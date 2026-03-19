package template

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestBuiltinProviderLoadsAll(t *testing.T) {
	p, err := NewBuiltinProvider()
	require.NoError(t, err)
	templates, err := p.List(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, templates, 14)
	slugs := map[string]bool{}
	for _, tmpl := range templates {
		slugs[tmpl.Slug] = true
	}
	for _, expected := range []string{
		"fleet-research", "fleet-transform", "bug-fix", "dependency-update",
		"pr-review", "migration", "triage", "audit", "incident-response",
		"sandbox-test", "mcp-test", "clone-test", "doc-assessment",
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

func TestSandboxTestWorkflowTemplate_Parses(t *testing.T) {
	p, err := NewBuiltinProvider()
	require.NoError(t, err)

	tmpl, err := p.Get(context.Background(), "", "sandbox-test")
	require.NoError(t, err)
	assert.Equal(t, "Sandbox Test", tmpl.Title)

	var def model.WorkflowDef
	require.NoError(t, model.ParseWorkflowYAML([]byte(tmpl.YAMLBody), &def))
	assert.Len(t, def.Steps, 2)
	assert.Equal(t, "shell", def.Steps[0].Execution.Agent)
	assert.Equal(t, "run_command", def.Steps[0].ID)
	assert.Equal(t, []string{"run_command"}, def.Steps[1].DependsOn)
	assert.Equal(t, "test", def.Steps[0].SandboxGroup)
	assert.Equal(t, "test", def.Steps[1].SandboxGroup)
}

func TestBuiltinProviderReadOnly(t *testing.T) {
	p, err := NewBuiltinProvider()
	require.NoError(t, err)
	assert.False(t, p.Writable())
	assert.Error(t, p.Save(context.Background(), "", nil))
	assert.Error(t, p.Delete(context.Background(), "", ""))
}
