package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
	"github.com/tinkerloft/fleetlift/internal/shellquote"
)

// ClaudeCodeRunner implements Runner by invoking Claude Code CLI inside a sandbox.
type ClaudeCodeRunner struct {
	sandbox sandbox.Client
}

// NewClaudeCodeRunner creates a new ClaudeCodeRunner backed by the given sandbox client.
func NewClaudeCodeRunner(sb sandbox.Client) *ClaudeCodeRunner {
	return &ClaudeCodeRunner{sandbox: sb}
}

func (r *ClaudeCodeRunner) Name() string { return "claude-code" }

func (r *ClaudeCodeRunner) SandboxEnv() map[string]string {
	env := make(map[string]string)
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		env["ANTHROPIC_API_KEY"] = key
	}
	if token := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); token != "" {
		env["CLAUDE_CODE_OAUTH_TOKEN"] = token
	}
	return env
}

func (r *ClaudeCodeRunner) Run(ctx context.Context, sandboxID string, opts RunOpts) (<-chan Event, error) {
	// If MCP sidecar is available, source profile to pick up FLEETLIFT_MCP_PORT
	// and write config so Claude discovers it via .mcp.json in the workspace.
	mcpSetup := `. /tmp/fleetlift-mcp-env.sh 2>/dev/null; ` +
		`if [ -n "$FLEETLIFT_MCP_PORT" ]; then ` +
		`printf '{"mcpServers":{"fleetlift":{"type":"sse","url":"http://localhost:%s/sse"}}}' "$FLEETLIFT_MCP_PORT" > /workspace/.mcp.json; ` +
		`fi`

	pluginDirFlags := ""
	for _, dir := range opts.EvalPluginDirs {
		pluginDirFlags += " --plugin-dir " + shellquote.Quote(dir)
	}

	cmd := fmt.Sprintf("%s && cd %s && claude -p %s%s --output-format stream-json --verbose --dangerously-skip-permissions --max-turns %d",
		mcpSetup, shellquote.Quote(opts.WorkDir), shellquote.Quote(opts.Prompt), pluginDirFlags, max(opts.MaxTurns, 100))

	ch := make(chan Event, 64)
	go func() {
		defer close(ch)
		err := r.sandbox.ExecStream(ctx, sandboxID, cmd, opts.WorkDir, func(line string) {
			event := parseClaudeEvent(line)
			select {
			case ch <- event:
			case <-ctx.Done():
			}
		})
		if err != nil {
			select {
			case ch <- Event{Type: "error", Content: err.Error()}:
			case <-ctx.Done():
			}
		}
	}()
	return ch, nil
}

func (r *ClaudeCodeRunner) Interrupt(ctx context.Context, sandboxID string) error {
	_, _, err := r.sandbox.Exec(ctx, sandboxID, "pkill -INT -f 'claude'", "/")
	return err
}

// parseClaudeEvent parses a single line of claude --output-format stream-json output
// and extracts human-readable content, filtering out noise like system init, rate limits,
// thinking blocks, and raw tool results.
//
// Lines arrive from ExecStream in the normalized format {"stream":"stdout","content":"..."}
// where content is the actual Claude JSON. We unwrap that first.
func parseClaudeEvent(line string) Event {
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return Event{Type: "stdout", Content: line}
	}

	// Unwrap the normalized ExecStream format: {"stream":"...","content":"..."}
	if _, hasStream := raw["stream"]; hasStream {
		content, _ := raw["content"].(string)
		if content == "" {
			return Event{}
		}
		// Re-parse the inner content as Claude JSON.
		var inner map[string]any
		if err := json.Unmarshal([]byte(content), &inner); err != nil {
			// Not JSON — plain text output from the command.
			stream, _ := raw["stream"].(string)
			evType := "stdout"
			if stream == "stderr" {
				evType = "stderr"
			}
			return Event{Type: evType, Content: content}
		}
		raw = inner
	}

	typ, _ := raw["type"].(string)
	switch typ {
	case "result":
		return Event{Type: "complete", Output: raw}
	case "needs_input":
		return Event{Type: "needs_input", Content: fmt.Sprintf("%v", raw["message"])}
	case "system", "rate_limit_event":
		// Noise — skip entirely.
		return Event{}
	case "user":
		// Tool results — extract a brief summary if present.
		return parseToolResult(raw)
	case "assistant":
		return parseAssistantMessage(raw)
	default:
		content, _ := raw["content"].(string)
		if content == "" {
			return Event{}
		}
		return Event{Type: "stdout", Content: content}
	}
}

// parseAssistantMessage extracts readable content from assistant message events.
func parseAssistantMessage(raw map[string]any) Event {
	msg, ok := raw["message"].(map[string]any)
	if !ok {
		return Event{}
	}
	content, _ := msg["content"].([]any)
	var parts []string
	for _, block := range content {
		b, ok := block.(map[string]any)
		if !ok {
			continue
		}
		blockType, _ := b["type"].(string)
		switch blockType {
		case "text":
			if text, ok := b["text"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		case "tool_use":
			name, _ := b["name"].(string)
			input, _ := b["input"].(map[string]any)
			desc, _ := input["description"].(string)
			if desc != "" {
				parts = append(parts, fmt.Sprintf("[tool] %s: %s", name, desc))
			} else if cmd, ok := input["command"].(string); ok {
				// Bash commands — show the command itself.
				if len(cmd) > 120 {
					cmd = cmd[:120] + "…"
				}
				parts = append(parts, fmt.Sprintf("[tool] %s: %s", name, cmd))
			} else {
				parts = append(parts, fmt.Sprintf("[tool] %s", name))
			}
			// Skip "thinking" blocks — they're noise in logs.
		}
	}
	if len(parts) == 0 {
		return Event{}
	}
	return Event{Type: "stdout", Content: strings.Join(parts, "\n")}
}

// parseToolResult extracts a brief summary from tool result events.
func parseToolResult(raw map[string]any) Event {
	msg, ok := raw["message"].(map[string]any)
	if !ok {
		return Event{}
	}
	content, _ := msg["content"].([]any)
	for _, block := range content {
		b, ok := block.(map[string]any)
		if !ok {
			continue
		}
		if b["type"] == "tool_result" {
			result, _ := b["content"].(string)
			isError, _ := b["is_error"].(bool)
			if isError && result != "" {
				if len(result) > 200 {
					result = result[:200] + "…"
				}
				return Event{Type: "stderr", Content: result}
			}
		}
	}
	// Most tool results are verbose — skip them.
	return Event{}
}
