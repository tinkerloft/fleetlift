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

func TestParseClaudeEvent_Default(t *testing.T) {
	line := `{"type":"assistant","content":"Working on it..."}`
	ev := parseClaudeEvent(line)
	assert.Equal(t, "stdout", ev.Type)
	assert.Equal(t, "Working on it...", ev.Content)
}

func TestParseClaudeEvent_InvalidJSON(t *testing.T) {
	line := "not json at all"
	ev := parseClaudeEvent(line)
	assert.Equal(t, "stdout", ev.Type)
	assert.Equal(t, "not json at all", ev.Content)
}
