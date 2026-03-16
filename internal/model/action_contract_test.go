package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionRegistry_Get_Known(t *testing.T) {
	r := DefaultActionRegistry()
	c, ok := r.Get("slack_notify")
	require.True(t, ok)
	assert.Equal(t, "slack_notify", c.Type)
	assert.NotEmpty(t, c.Description)
}

func TestActionRegistry_Get_Unknown(t *testing.T) {
	r := DefaultActionRegistry()
	_, ok := r.Get("send_email")
	assert.False(t, ok)
}

func TestActionRegistry_Types(t *testing.T) {
	r := DefaultActionRegistry()
	types := r.Types()
	assert.Contains(t, types, "slack_notify")
	assert.Contains(t, types, "github_pr_review")
	assert.Contains(t, types, "github_label")
	assert.Contains(t, types, "github_comment")
	assert.Contains(t, types, "create_pr")
}

func TestValidateConfig_Valid(t *testing.T) {
	r := DefaultActionRegistry()
	errs := r.ValidateConfig("slack_notify", map[string]any{
		"channel": "#general",
		"message": "hello",
	})
	assert.Empty(t, errs)
}

func TestValidateConfig_MissingRequired(t *testing.T) {
	r := DefaultActionRegistry()
	errs := r.ValidateConfig("slack_notify", map[string]any{
		"channel": "#general",
		// "message" is missing
	})
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "message")
	assert.Contains(t, errs[0], "required")
}

func TestValidateConfig_UnknownKey(t *testing.T) {
	r := DefaultActionRegistry()
	errs := r.ValidateConfig("slack_notify", map[string]any{
		"channel":  "#general",
		"message":  "hello",
		"chanel_2": "bad",
	})
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "chanel_2")
	assert.Contains(t, errs[0], "unknown")
}

func TestValidateConfig_TemplateValueSkipsTypeCheck(t *testing.T) {
	r := DefaultActionRegistry()
	// pr_number expects int, but template value should skip type check
	errs := r.ValidateConfig("github_pr_review", map[string]any{
		"repo_url":  "https://github.com/org/repo",
		"pr_number": "{{ .Params.pr_number }}",
		"summary":   "looks good",
	})
	assert.Empty(t, errs)
}

func TestValidateConfig_WrongType(t *testing.T) {
	r := DefaultActionRegistry()
	// pr_number expects int, passing a plain string (not template)
	errs := r.ValidateConfig("github_pr_review", map[string]any{
		"repo_url":  "https://github.com/org/repo",
		"pr_number": "not-a-number",
		"summary":   "looks good",
	})
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "pr_number")
	assert.Contains(t, errs[0], "int")
}

func TestValidateConfig_UnknownActionType(t *testing.T) {
	r := DefaultActionRegistry()
	errs := r.ValidateConfig("send_email", map[string]any{})
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "unknown")
}

func TestValidateConfig_NilConfig(t *testing.T) {
	r := DefaultActionRegistry()
	errs := r.ValidateConfig("slack_notify", nil)
	// Should report missing required fields
	assert.NotEmpty(t, errs)
}

func TestValidateConfig_ArrayTypeAcceptsSlice(t *testing.T) {
	r := DefaultActionRegistry()
	errs := r.ValidateConfig("github_label", map[string]any{
		"repo_url":     "https://github.com/org/repo",
		"issue_number": 42,
		"labels":       []any{"bug", "critical"},
	})
	assert.Empty(t, errs)
}
