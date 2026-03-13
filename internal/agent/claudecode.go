package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
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

func (r *ClaudeCodeRunner) Run(ctx context.Context, sandboxID string, opts RunOpts) (<-chan Event, error) {
	cmd := fmt.Sprintf("claude -p %s --output-format stream-json --max-turns %d",
		shellQuote(opts.Prompt), max(opts.MaxTurns, 20))

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

// parseClaudeEvent parses a single line of claude --output-format stream-json output.
func parseClaudeEvent(line string) Event {
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return Event{Type: "stdout", Content: line}
	}
	typ, _ := raw["type"].(string)
	switch typ {
	case "result":
		return Event{Type: "complete", Output: raw}
	case "needs_input":
		return Event{Type: "needs_input", Content: fmt.Sprintf("%v", raw["message"])}
	default:
		content, _ := raw["content"].(string)
		return Event{Type: "stdout", Content: content}
	}
}
