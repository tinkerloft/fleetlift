package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

// sseJSON marshals a {"stream": stream, "content": content} JSON line.
func sseJSON(stream, content string) string {
	b, _ := json.Marshal(map[string]string{"stream": stream, "content": content})
	return string(b)
}

// mockSandbox implements sandbox.Client with a configurable ExecStream.
type mockSandbox struct {
	sandbox.Client
	lines []string // lines to deliver via onLine
	err   error    // error to return from ExecStream
}

func (m *mockSandbox) ExecStream(ctx context.Context, _, _, _ string, onLine func(string)) error {
	for _, line := range m.lines {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		onLine(line)
	}
	return m.err
}

func (m *mockSandbox) Exec(_ context.Context, _, _, _ string) (string, string, error) {
	return "", "", nil
}

// collectEvents drains the channel with a timeout.
func collectEvents(ch <-chan Event, timeout time.Duration) []Event {
	var events []Event
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, ev)
		case <-timer.C:
			return events
		}
	}
}

func TestShellRunner_Name(t *testing.T) {
	r := NewShellRunner(nil)
	if r.Name() != "shell" {
		t.Fatalf("expected name 'shell', got %q", r.Name())
	}
}

func TestShellRunner_StdoutEvents(t *testing.T) {
	mock := &mockSandbox{
		lines: []string{
			sseJSON("stdout", "hello"),
			sseJSON("stdout", "world"),
			sseJSON("stdout", "__FLEETLIFT_EXIT_CODE__=0"),
		},
	}
	r := NewShellRunner(mock)
	ch, err := r.Run(context.Background(), "sb-1", RunOpts{Prompt: "echo hello && echo world"})
	if err != nil {
		t.Fatal(err)
	}
	events := collectEvents(ch, 2*time.Second)
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != "stdout" || events[0].Content != "hello" {
		t.Errorf("event[0] = %+v", events[0])
	}
	if events[1].Type != "stdout" || events[1].Content != "world" {
		t.Errorf("event[1] = %+v", events[1])
	}
	if events[2].Type != "complete" {
		t.Errorf("expected complete event, got %+v", events[2])
	}
	exitCode, _ := events[2].Output["exit_code"].(int)
	if exitCode != 0 {
		t.Errorf("expected exit_code 0, got %v", events[2].Output["exit_code"])
	}
	stdout, _ := events[2].Output["stdout"].(string)
	if stdout != "hello\nworld\n" {
		t.Errorf("expected aggregated stdout, got %q", stdout)
	}
}

func TestShellRunner_StderrEvents(t *testing.T) {
	mock := &mockSandbox{
		lines: []string{
			sseJSON("stdout", "out1"),
			sseJSON("stderr", "err1"),
			sseJSON("stdout", "out2"),
			sseJSON("stdout", "__FLEETLIFT_EXIT_CODE__=0"),
		},
	}
	r := NewShellRunner(mock)
	ch, err := r.Run(context.Background(), "sb-1", RunOpts{Prompt: "cmd"})
	if err != nil {
		t.Fatal(err)
	}
	events := collectEvents(ch, 2*time.Second)
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != "stdout" {
		t.Errorf("event[0] type = %q", events[0].Type)
	}
	if events[1].Type != "stderr" || events[1].Content != "err1" {
		t.Errorf("event[1] = %+v", events[1])
	}
	if events[2].Type != "stdout" {
		t.Errorf("event[2] type = %q", events[2].Type)
	}
	if events[3].Type != "complete" {
		t.Errorf("expected complete, got %+v", events[3])
	}
	stderr, _ := events[3].Output["stderr"].(string)
	if stderr != "err1\n" {
		t.Errorf("expected stderr buffer 'err1\\n', got %q", stderr)
	}
	stdout, _ := events[3].Output["stdout"].(string)
	if stdout != "out1\nout2\n" {
		t.Errorf("expected stdout buffer 'out1\\nout2\\n', got %q", stdout)
	}
}

func TestShellRunner_NonZeroExitCode(t *testing.T) {
	mock := &mockSandbox{
		lines: []string{
			sseJSON("stderr", "not found"),
			sseJSON("stdout", "__FLEETLIFT_EXIT_CODE__=127"),
		},
	}
	r := NewShellRunner(mock)
	ch, err := r.Run(context.Background(), "sb-1", RunOpts{Prompt: "badcmd"})
	if err != nil {
		t.Fatal(err)
	}
	events := collectEvents(ch, 2*time.Second)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != "stderr" {
		t.Errorf("event[0] type = %q", events[0].Type)
	}
	if events[1].Type != "error" {
		t.Fatalf("expected error event, got %+v", events[1])
	}
	if events[1].Content != "command exited with code 127" {
		t.Errorf("unexpected error content: %q", events[1].Content)
	}
}

func TestShellRunner_ExecStreamError(t *testing.T) {
	mock := &mockSandbox{
		lines: []string{
			sseJSON("stdout", "partial"),
		},
		err: fmt.Errorf("connection lost"),
	}
	r := NewShellRunner(mock)
	ch, err := r.Run(context.Background(), "sb-1", RunOpts{Prompt: "cmd"})
	if err != nil {
		t.Fatal(err)
	}
	events := collectEvents(ch, 2*time.Second)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != "stdout" || events[0].Content != "partial" {
		t.Errorf("event[0] = %+v", events[0])
	}
	if events[1].Type != "error" || events[1].Content != "connection lost" {
		t.Errorf("event[1] = %+v", events[1])
	}
}

func TestShellRunner_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	mock := &mockSandbox{
		lines: []string{
			sseJSON("stdout", "line1"),
			sseJSON("stdout", "line2"),
			sseJSON("stdout", "line3"),
			sseJSON("stdout", "__FLEETLIFT_EXIT_CODE__=0"),
		},
	}
	r := NewShellRunner(mock)
	ch, err := r.Run(ctx, "sb-1", RunOpts{Prompt: "cmd"})
	if err != nil {
		t.Fatal(err)
	}
	events := collectEvents(ch, 1*time.Second)
	if len(events) > 2 {
		t.Errorf("expected ≤2 events with cancelled context, got %d", len(events))
	}
}

func TestShellRunner_NonJSONLine(t *testing.T) {
	mock := &mockSandbox{
		lines: []string{
			"plain text output",
			sseJSON("stdout", "__FLEETLIFT_EXIT_CODE__=0"),
		},
	}
	r := NewShellRunner(mock)
	ch, err := r.Run(context.Background(), "sb-1", RunOpts{Prompt: "cmd"})
	if err != nil {
		t.Fatal(err)
	}
	events := collectEvents(ch, 2*time.Second)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != "stdout" || events[0].Content != "plain text output" {
		t.Errorf("event[0] = %+v", events[0])
	}
	if events[1].Type != "complete" {
		t.Errorf("expected complete, got %+v", events[1])
	}
}
