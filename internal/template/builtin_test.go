package template

import (
	"context"
	"strings"
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
	assert.Len(t, templates, 15)
	slugs := map[string]bool{}
	for _, tmpl := range templates {
		slugs[tmpl.Slug] = true
	}
	for _, expected := range []string{
		"fleet-research", "fleet-transform", "bug-fix", "dependency-update",
		"pr-review", "migration", "triage", "audit", "incident-response",
		"sandbox-test", "mcp-test", "clone-test", "doc-assessment",
		"auto-debt-slayer",
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

func TestAutoDebtSlayerWorkflowTemplate_Parses(t *testing.T) {
	p, err := NewBuiltinProvider()
	require.NoError(t, err)

	tmpl, err := p.Get(context.Background(), "", "auto-debt-slayer")
	require.NoError(t, err)
	assert.Equal(t, "Auto Debt Slayer", tmpl.Title)

	var def model.WorkflowDef
	require.NoError(t, model.ParseWorkflowYAML([]byte(tmpl.YAMLBody), &def))

	// 4 steps in correct order
	require.Len(t, def.Steps, 4)
	assert.Equal(t, "enrich", def.Steps[0].ID)
	assert.Equal(t, "assess", def.Steps[1].ID)
	assert.Equal(t, "execute", def.Steps[2].ID)
	assert.Equal(t, "notify", def.Steps[3].ID)

	// enrich: report mode, has execution and repositories
	assert.Equal(t, "report", def.Steps[0].Mode)
	assert.NotNil(t, def.Steps[0].Execution)

	// assess: report mode, depends on enrich
	assert.Equal(t, "report", def.Steps[1].Mode)
	assert.Contains(t, def.Steps[1].DependsOn, "enrich")

	// execute: transform, has condition and pull_request
	assert.Equal(t, "transform", def.Steps[2].Mode)
	assert.NotEmpty(t, def.Steps[2].Condition)
	assert.NotNil(t, def.Steps[2].PullRequest)
	assert.NotEmpty(t, def.Steps[2].PullRequest.BranchPrefix)
	assert.Contains(t, def.Steps[2].DependsOn, "assess")

	// notify: optional action step
	assert.True(t, def.Steps[3].Optional)
	assert.NotNil(t, def.Steps[3].Action)
	assert.Equal(t, "slack_notify", def.Steps[3].Action.Type)

	// required parameters
	paramNames := make([]string, len(def.Parameters))
	for i, p := range def.Parameters {
		paramNames[i] = p.Name
	}
	assert.Contains(t, paramNames, "ticket_key")
	assert.Contains(t, paramNames, "jira_base_url")
	assert.Contains(t, paramNames, "github_repo")
}

func TestBuiltinProviderReadOnly(t *testing.T) {
	p, err := NewBuiltinProvider()
	require.NoError(t, err)
	assert.False(t, p.Writable())
	assert.Error(t, p.Save(context.Background(), "", nil))
	assert.Error(t, p.Delete(context.Background(), "", ""))
}

func TestBuiltinClaudeWorkflowsDoNotUseDeprecatedClaudeCodeImage(t *testing.T) {
	p, err := NewBuiltinProvider()
	require.NoError(t, err)

	templates, err := p.List(context.Background(), "")
	require.NoError(t, err)

	for _, tmpl := range templates {
		if !strings.Contains(tmpl.YAMLBody, "agent: claude-code") {
			continue
		}
		assert.NotContainsf(t, tmpl.YAMLBody, "image: claude-code:latest",
			"builtin %q should not pin deprecated claude-code image", tmpl.Slug)
	}
}
