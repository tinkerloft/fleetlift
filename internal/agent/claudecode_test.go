package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseClaudeEvent_Result(t *testing.T) {
	line := `{"type":"result","cost":0.05,"duration":12.3}`
	ev := parseClaudeEvent(line)
	assert.Equal(t, "complete", ev.Type)
	assert.Equal(t, "result", ev.Output["type"])
}

func TestParseClaudeEvent_NeedsInput(t *testing.T) {
	line := `{"type":"needs_input","message":"Please provide API key"}`
	ev := parseClaudeEvent(line)
	assert.Equal(t, "needs_input", ev.Type)
	assert.Equal(t, "Please provide API key", ev.Content)
}

func TestParseClaudeEvent_AssistantText(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"Working on it..."}]}}`
	ev := parseClaudeEvent(line)
	assert.Equal(t, "stdout", ev.Type)
	assert.Equal(t, "Working on it...", ev.Content)
}

func TestParseClaudeEvent_AssistantToolUse(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"ls -la","description":"List files"}}]}}`
	ev := parseClaudeEvent(line)
	assert.Equal(t, "stdout", ev.Type)
	assert.Equal(t, "[tool] Bash: List files", ev.Content)
}

func TestParseClaudeEvent_AssistantToolUseNoDesc(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"git status"}}]}}`
	ev := parseClaudeEvent(line)
	assert.Equal(t, "stdout", ev.Type)
	assert.Equal(t, "[tool] Bash: git status", ev.Content)
}

func TestParseClaudeEvent_AssistantThinking(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"Let me think..."}]}}`
	ev := parseClaudeEvent(line)
	assert.Equal(t, "", ev.Type, "thinking blocks should be filtered out")
}

func TestParseClaudeEvent_SystemInit(t *testing.T) {
	line := `{"type":"system","subtype":"init","cwd":"/workspace"}`
	ev := parseClaudeEvent(line)
	assert.Equal(t, "", ev.Type, "system events should be filtered out")
}

func TestParseClaudeEvent_RateLimit(t *testing.T) {
	line := `{"type":"rate_limit_event","rate_limit_info":{}}`
	ev := parseClaudeEvent(line)
	assert.Equal(t, "", ev.Type, "rate limit events should be filtered out")
}

func TestParseClaudeEvent_ToolResultError(t *testing.T) {
	line := `{"type":"user","message":{"content":[{"type":"tool_result","content":"command not found","is_error":true,"tool_use_id":"123"}]}}`
	ev := parseClaudeEvent(line)
	assert.Equal(t, "stderr", ev.Type)
	assert.Equal(t, "command not found", ev.Content)
}

func TestParseClaudeEvent_InvalidJSON(t *testing.T) {
	line := "not json at all"
	ev := parseClaudeEvent(line)
	assert.Equal(t, "stdout", ev.Type)
	assert.Equal(t, "not json at all", ev.Content)
}
