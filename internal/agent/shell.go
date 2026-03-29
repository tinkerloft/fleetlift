package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
	"github.com/tinkerloft/fleetlift/internal/shellquote"
)

const exitCodeSentinel = "__FLEETLIFT_EXIT_CODE__="

// ShellRunner implements Runner by executing shell commands inside a sandbox.
type ShellRunner struct {
	sandbox sandbox.Client
}

// NewShellRunner creates a new ShellRunner backed by the given sandbox client.
func NewShellRunner(sb sandbox.Client) *ShellRunner {
	return &ShellRunner{sandbox: sb}
}

func (r *ShellRunner) Name() string { return "shell" }

func (r *ShellRunner) Run(ctx context.Context, sandboxID string, opts RunOpts) (<-chan Event, error) {
	// Append exit code sentinel. OpenSandbox's execd already runs commands via
	// bash -c, so we pass the prompt directly — no extra /bin/sh -c wrapper needed.
	cmd := opts.Prompt + "\necho " + exitCodeSentinel + "$?"

	ch := make(chan Event, 64)
	go func() {
		defer close(ch)

		var stdoutBuf, stderrBuf strings.Builder
		exitCode := -1

		err := r.sandbox.ExecStream(ctx, sandboxID, cmd, opts.WorkDir, func(line string) {
			stream, content, ok := parseSSELine(line)
			if !ok {
				// Non-JSON line: emit as stdout.
				select {
				case ch <- Event{Type: "stdout", Content: line}:
					stdoutBuf.WriteString(line)
					stdoutBuf.WriteByte('\n')
				case <-ctx.Done():
				}
				return
			}

			// Check for sentinel.
			if codeStr, found := strings.CutPrefix(content, exitCodeSentinel); found {
				code, parseErr := strconv.Atoi(codeStr)
				if parseErr == nil {
					exitCode = code
				}
				return
			}

			evType := "stdout"
			if stream == "stderr" {
				evType = "stderr"
				stderrBuf.WriteString(content)
				stderrBuf.WriteByte('\n')
			} else {
				stdoutBuf.WriteString(content)
				stdoutBuf.WriteByte('\n')
			}

			select {
			case ch <- Event{Type: evType, Content: content}:
			case <-ctx.Done():
			}
		})

		if err != nil {
			select {
			case ch <- Event{Type: "error", Content: err.Error()}:
			case <-ctx.Done():
			}
			return
		}

		if exitCode != 0 && exitCode != -1 {
			// Include captured stdout/stderr for debugging.
			errMsg := fmt.Sprintf("command exited with code %d", exitCode)
			if stderr := stderrBuf.String(); stderr != "" {
				errMsg += "\nstderr: " + stderr
			}
			if stdout := stdoutBuf.String(); stdout != "" && len(stdout) < 2000 {
				errMsg += "\nstdout: " + stdout
			}
			select {
			case ch <- Event{Type: "error", Content: errMsg}:
			case <-ctx.Done():
			}
			return
		}

		select {
		case ch <- Event{
			Type: "complete",
			Output: map[string]any{
				"stdout":    stdoutBuf.String(),
				"stderr":    stderrBuf.String(),
				"exit_code": 0,
			},
		}:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

func (r *ShellRunner) Interrupt(ctx context.Context, sandboxID string) error {
	// Use the sentinel to uniquely identify the process started by this runner,
	// avoiding killing unrelated /bin/sh processes in the sandbox.
	_, _, err := r.sandbox.Exec(ctx, sandboxID, "pkill -INT -f "+shellquote.Quote(exitCodeSentinel), "/")
	return err
}

// parseSSELine attempts to parse a JSON line with "stream" and "content" fields.
// Returns the stream name, content, and whether parsing succeeded.
func parseSSELine(line string) (stream, content string, ok bool) {
	var parsed struct {
		Stream  string `json:"stream"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		return "", "", false
	}
	return parsed.Stream, parsed.Content, true
}
