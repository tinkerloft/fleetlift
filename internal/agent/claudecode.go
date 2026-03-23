package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
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
	runID := uuid.NewString()
	promptPath := fmt.Sprintf("/tmp/fleetlift-prompt-%s.txt", runID)
	requestPath := fmt.Sprintf("/tmp/fleetlift-request-%s.json", runID)
	if err := r.sandbox.WriteFile(ctx, sandboxID, promptPath, opts.Prompt); err != nil {
		return nil, fmt.Errorf("write prompt file: %w", err)
	}

	workDir := opts.WorkDir
	if workDir == "" {
		workDir = "/workspace"
	}

	req := bridgeRequest{
		Version:    1,
		PromptFile: promptPath,
		WorkDir:    workDir,
		MaxTurns:   effectiveMaxTurns(opts.MaxTurns),
		MCP: bridgeMCPRequest{
			Enabled:    true,
			ConfigPath: "/workspace/.mcp.json",
		},
		PluginDirs: opts.EvalPluginDirs,
		Env:        opts.Environment,
	}
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal bridge request: %w", err)
	}
	if err := r.sandbox.WriteFile(ctx, sandboxID, requestPath, string(reqBytes)); err != nil {
		return nil, fmt.Errorf("write bridge request file: %w", err)
	}

	// If MCP sidecar is available, source profile to pick up FLEETLIFT_MCP_PORT
	// and write config so Claude discovers it via .mcp.json in the workspace.
	mcpSetup := `. /tmp/fleetlift-mcp-env.sh 2>/dev/null; ` +
		`if [ -n "$FLEETLIFT_MCP_PORT" ]; then ` +
		`printf '{"mcpServers":{"fleetlift":{"type":"sse","url":"http://localhost:%s/sse"}}}' "$FLEETLIFT_MCP_PORT" > /workspace/.mcp.json; ` +
		`fi`

	cmd := fmt.Sprintf("%s && node /agent/bridge.js %s", mcpSetup, shellquote.Quote(requestPath))

	ch := make(chan Event, 64)
	go func() {
		defer close(ch)
		err := r.sandbox.ExecStream(ctx, sandboxID, cmd, workDir, func(line string) {
			for _, event := range parseClaudeStreamChunk(line) {
				select {
				case ch <- event:
				case <-ctx.Done():
					return
				}
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

func parseClaudeStreamChunk(line string) []Event {
	var outer struct {
		Stream  string `json:"stream"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(line), &outer); err != nil || outer.Content == "" {
		event := parseClaudeEvent(line)
		if event.Type == "" && event.Content == "" {
			return nil
		}
		return []Event{event}
	}

	parts := strings.Split(outer.Content, "\n")
	if len(parts) == 1 {
		event := parseClaudeEvent(line)
		if event.Type == "" && event.Content == "" {
			return nil
		}
		return []Event{event}
	}

	events := make([]Event, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		wrapped, err := json.Marshal(map[string]string{
			"stream":  outer.Stream,
			"content": part,
		})
		if err != nil {
			events = append(events, Event{Type: outer.Stream, Content: part})
			continue
		}
		event := parseClaudeEvent(string(wrapped))
		if event.Type == "" && event.Content == "" {
			continue
		}
		events = append(events, event)
	}
	return events
}

type bridgeRequest struct {
	Version    int               `json:"version"`
	PromptFile string            `json:"prompt_file"`
	WorkDir    string            `json:"work_dir"`
	MaxTurns   int               `json:"max_turns"`
	MCP        bridgeMCPRequest  `json:"mcp,omitempty"`
	PluginDirs []string          `json:"plugin_dirs,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
}

type bridgeMCPRequest struct {
	Enabled    bool   `json:"enabled"`
	ConfigPath string `json:"config_path,omitempty"`
}

// effectiveMaxTurns returns the max_turns to pass to claude. A configured value of 0
// means "use the runner default" (100). Any explicitly-set value is used as-is.
func effectiveMaxTurns(configured int) int {
	if configured == 0 {
		return 100
	}
	return configured
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
	case "assistant_text":
		content, _ := raw["content"].(string)
		if content == "" {
			return Event{}
		}
		return Event{Type: "stdout", Content: content}
	case "tool_call":
		return parseNormalizedToolCall(raw)
	case "tool_result":
		content, _ := raw["content"].(string)
		if content == "" {
			return Event{}
		}
		stream, _ := raw["stream"].(string)
		if stream == "stderr" {
			return Event{Type: "stderr", Content: content}
		}
		return Event{Type: "stdout", Content: content}
	case "status":
		content, _ := raw["content"].(string)
		if content == "" {
			return Event{}
		}
		return Event{Type: "stdout", Content: content}
	case "complete":
		return Event{Type: "complete", Output: raw}
	case "error":
		content, _ := raw["content"].(string)
		if content == "" {
			content = "agent bridge error"
		}
		return Event{Type: "error", Content: content}
	case "needs_input":
		if content, _ := raw["content"].(string); content != "" {
			return Event{Type: "needs_input", Content: content}
		}
		return Event{Type: "needs_input", Content: fmt.Sprintf("%v", raw["message"])}
	default:
		return parseLegacyClaudeEvent(raw)
	}
}

func parseNormalizedToolCall(raw map[string]any) Event {
	name, _ := raw["name"].(string)
	desc, _ := raw["description"].(string)
	command, _ := raw["command"].(string)
	if desc != "" {
		if name != "" {
			return Event{Type: "stdout", Content: fmt.Sprintf("[tool] %s: %s", name, desc)}
		}
		return Event{Type: "stdout", Content: fmt.Sprintf("[tool] %s", desc)}
	}
	if command != "" {
		if len(command) > 120 {
			command = command[:120] + "…"
		}
		if name != "" {
			return Event{Type: "stdout", Content: fmt.Sprintf("[tool] %s: %s", name, command)}
		}
		return Event{Type: "stdout", Content: "[tool] " + command}
	}
	if name != "" {
		return Event{Type: "stdout", Content: "[tool] " + name}
	}
	return Event{}
}

func parseLegacyClaudeEvent(raw map[string]any) Event {
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
