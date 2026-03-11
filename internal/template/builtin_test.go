package template

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltinProviderLoadsAll(t *testing.T) {
	p, err := NewBuiltinProvider()
	require.NoError(t, err)
	templates, err := p.List(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, templates, 9)
	slugs := map[string]bool{}
	for _, tmpl := range templates {
		slugs[tmpl.Slug] = true
	}
	for _, expected := range []string{
		"fleet-research", "fleet-transform", "bug-fix", "dependency-update",
		"pr-review", "migration", "triage", "audit", "incident-response",
	} {
		assert.True(t, slugs[expected], "missing builtin: %s", expected)
	}
}

func TestBuiltinProviderGet(t *testing.T) {
	p, err := NewBuiltinProvider()
	require.NoError(t, err)

	tmpl, err := p.Get(context.Background(), "", "bug-fix")
	require.NoError(t, err)
	assert.Equal(t, "bug-fix", tmpl.Slug)
	assert.True(t, tmpl.Builtin)

	_, err = p.Get(context.Background(), "", "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestBuiltinProviderReadOnly(t *testing.T) {
	p, err := NewBuiltinProvider()
	require.NoError(t, err)
	assert.False(t, p.Writable())
	assert.Error(t, p.Save(context.Background(), "", nil))
	assert.Error(t, p.Delete(context.Background(), "", ""))
}
