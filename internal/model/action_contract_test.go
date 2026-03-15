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
	assert.Contains(t, types, "github_assign")
	assert.Contains(t, types, "github_label")
	assert.Contains(t, types, "github_comment")
	assert.Contains(t, types, "create_pr")
}
