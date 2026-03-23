package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

type runnerSandbox struct {
	lines       []string
	err         error
	execCmd     string
	execWorkDir string
	writes      map[string]string
}

func (s *runnerSandbox) ExecStream(_ context.Context, _, cmd, workDir string, onLine func(string)) error {
	s.execCmd = cmd
	s.execWorkDir = workDir
	for _, line := range s.lines {
		onLine(line)
	}
	return s.err
}

func (s *runnerSandbox) Exec(_ context.Context, _, _, _ string) (string, string, error) {
	return "", "", nil
}

func (s *runnerSandbox) WriteFile(_ context.Context, _, path, content string) error {
	if s.writes == nil {
		s.writes = make(map[string]string)
	}
	s.writes[path] = content
	return nil
}

func (s *runnerSandbox) Create(context.Context, sandbox.CreateOpts) (string, error) { return "", nil }
func (s *runnerSandbox) WriteBytes(context.Context, string, string, []byte) error   { return nil }
func (s *runnerSandbox) ReadFile(context.Context, string, string) (string, error)   { return "", nil }
func (s *runnerSandbox) ReadBytes(context.Context, string, string) ([]byte, error)  { return nil, nil }
func (s *runnerSandbox) Kill(context.Context, string) error                         { return nil }
func (s *runnerSandbox) RenewExpiration(context.Context, string) error              { return nil }

func wrapped(stream string, event map[string]any) string {
	inner, _ := json.Marshal(event)
	outer, _ := json.Marshal(map[string]string{"stream": stream, "content": string(inner)})
	return string(outer)
}

func wrappedContent(stream, content string) string {
	outer, _ := json.Marshal(map[string]string{"stream": stream, "content": content})
	return string(outer)
}

func TestClaudeCodeRunner_RunUsesBridgeRequestFile(t *testing.T) {
	sb := &runnerSandbox{
		lines: []string{wrapped("stdout", map[string]any{"type": "complete", "result": "ok", "is_error": false})},
	}
	r := NewClaudeCodeRunner(sb)

	ch, err := r.Run(context.Background(), "sb-1", RunOpts{
		Prompt:         "check status",
		WorkDir:        "/workspace/repo",
		MaxTurns:       5,
		EvalPluginDirs: []string{"/agent/plugins/foo"},
		Environment:    map[string]string{"FOO": "bar"},
	})
	require.NoError(t, err)
	_ = collectEvents(ch, 2*time.Second)

	assert.Contains(t, sb.execCmd, "node /agent/bridge.js")
	assert.NotContains(t, sb.execCmd, "claude -p")
	assert.Equal(t, "/workspace/repo", sb.execWorkDir)

	var promptPath string
	var requestPath string
	for path := range sb.writes {
		if strings.HasPrefix(path, "/tmp/fleetlift-prompt-") {
			promptPath = path
		}
		if strings.HasPrefix(path, "/tmp/fleetlift-request-") {
			requestPath = path
		}
	}
	require.NotEmpty(t, promptPath)
	require.NotEmpty(t, requestPath)
	require.Contains(t, sb.execCmd, requestPath)
	require.Equal(t, "check status", sb.writes[promptPath])

	var req map[string]any
	require.NoError(t, json.Unmarshal([]byte(sb.writes[requestPath]), &req))
	assert.Equal(t, float64(1), req["version"])
	assert.Equal(t, promptPath, req["prompt_file"])
	assert.Equal(t, "/workspace/repo", req["work_dir"])
	assert.Equal(t, float64(5), req["max_turns"])
	assert.Equal(t, []any{"/agent/plugins/foo"}, req["plugin_dirs"])
	mcp, ok := req["mcp"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, mcp["enabled"])
	assert.Equal(t, "/workspace/.mcp.json", mcp["config_path"])
}

func TestParseClaudeEvent_AssistantText(t *testing.T) {
	ev := parseClaudeEvent(`{"type":"assistant_text","content":"Working on it..."}`)
	assert.Equal(t, "stdout", ev.Type)
	assert.Equal(t, "Working on it...", ev.Content)
}

func TestParseClaudeEvent_ToolCall(t *testing.T) {
	ev := parseClaudeEvent(`{"type":"tool_call","name":"Bash","description":"List files"}`)
	assert.Equal(t, "stdout", ev.Type)
	assert.Equal(t, "[tool] Bash: List files", ev.Content)
}

func TestParseClaudeEvent_ToolResultStdout(t *testing.T) {
	ev := parseClaudeEvent(`{"type":"tool_result","stream":"stdout","chunk_index":1,"chunk_total":2,"content":"part-1"}`)
	assert.Equal(t, "stdout", ev.Type)
	assert.Equal(t, "part-1", ev.Content)
}

func TestParseClaudeEvent_ToolResultStderr(t *testing.T) {
	ev := parseClaudeEvent(`{"type":"tool_result","stream":"stderr","chunk_index":1,"chunk_total":1,"content":"failed"}`)
	assert.Equal(t, "stderr", ev.Type)
	assert.Equal(t, "failed", ev.Content)
}

func TestParseClaudeEvent_Status(t *testing.T) {
	ev := parseClaudeEvent(`{"type":"status","content":"Running tests"}`)
	assert.Equal(t, "stdout", ev.Type)
	assert.Equal(t, "Running tests", ev.Content)
}

func TestParseClaudeEvent_NeedsInput(t *testing.T) {
	ev := parseClaudeEvent(`{"type":"needs_input","content":"Approve deployment?"}`)
	assert.Equal(t, "needs_input", ev.Type)
	assert.Equal(t, "Approve deployment?", ev.Content)
}

func TestParseClaudeEvent_Complete(t *testing.T) {
	ev := parseClaudeEvent(`{"type":"complete","result":"Done","is_error":false,"cost_usd":0.04}`)
	assert.Equal(t, "complete", ev.Type)
	assert.Equal(t, "Done", ev.Output["result"])
	assert.Equal(t, false, ev.Output["is_error"])
}

func TestParseClaudeEvent_Error(t *testing.T) {
	ev := parseClaudeEvent(`{"type":"error","content":"API rate limit"}`)
	assert.Equal(t, "error", ev.Type)
	assert.Equal(t, "API rate limit", ev.Content)
}

func TestParseClaudeEvent_ChunkedToolResultOrder(t *testing.T) {
	lines := []string{
		`{"type":"tool_result","stream":"stdout","chunk_index":1,"chunk_total":2,"content":"hello "}`,
		`{"type":"tool_result","stream":"stdout","chunk_index":2,"chunk_total":2,"content":"world"}`,
	}
	var out strings.Builder
	for _, line := range lines {
		ev := parseClaudeEvent(line)
		assert.Equal(t, "stdout", ev.Type)
		out.WriteString(ev.Content)
	}
	assert.Equal(t, "hello world", out.String())
}

func TestParseClaudeEvent_WrappedNormalized(t *testing.T) {
	line := wrapped("stdout", map[string]any{"type": "assistant_text", "content": "hello"})
	ev := parseClaudeEvent(line)
	assert.Equal(t, "stdout", ev.Type)
	assert.Equal(t, "hello", ev.Content)
}

func TestClaudeCodeRunner_RunSplitsCombinedBridgeChunk(t *testing.T) {
	sb := &runnerSandbox{
		lines: []string{wrappedContent("stdout", strings.Join([]string{
			`{"type":"assistant_text","content":"hello"}`,
			`{"type":"tool_call","name":"Bash","description":"List files"}`,
			`{"type":"complete","result":"Done","is_error":false}`,
		}, "\n"))},
	}
	r := NewClaudeCodeRunner(sb)

	ch, err := r.Run(context.Background(), "sb-1", RunOpts{Prompt: "hello", WorkDir: "/workspace"})
	require.NoError(t, err)
	got := collectEvents(ch, 2*time.Second)

	require.Len(t, got, 3)
	assert.Equal(t, Event{Type: "stdout", Content: "hello"}, got[0])
	assert.Equal(t, Event{Type: "stdout", Content: "[tool] Bash: List files"}, got[1])
	assert.Equal(t, "complete", got[2].Type)
	assert.Equal(t, "Done", got[2].Output["result"])
}

func TestParseClaudeEvent_LegacyResultStillWorks(t *testing.T) {
	ev := parseClaudeEvent(`{"type":"result","subtype":"success","cost_usd":0.05,"session_id":"abc"}`)
	assert.Equal(t, "complete", ev.Type)
	assert.Equal(t, "result", ev.Output["type"])
	assert.Equal(t, 0.05, ev.Output["cost_usd"])
}

func TestEffectiveMaxTurns_ZeroReturnsDefault(t *testing.T) {
	assert.Equal(t, 100, effectiveMaxTurns(0))
}

func TestEffectiveMaxTurns_ExplicitSmallValueRespected(t *testing.T) {
	assert.Equal(t, 5, effectiveMaxTurns(5))
}
