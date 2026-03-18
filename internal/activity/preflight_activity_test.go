package activity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"

	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

// preflightRecordingSandbox records Exec calls for assertion.
type preflightRecordingSandbox struct {
	execCmds []string
}

func (s *preflightRecordingSandbox) Create(_ context.Context, _ sandbox.CreateOpts) (string, error) {
	return "sb-test", nil
}
func (s *preflightRecordingSandbox) Exec(_ context.Context, _, cmd, _ string) (string, string, error) {
	s.execCmds = append(s.execCmds, cmd)
	return "", "", nil
}
func (s *preflightRecordingSandbox) ExecStream(_ context.Context, _, _, _ string, _ func(string)) error {
	return nil
}
func (s *preflightRecordingSandbox) WriteFile(_ context.Context, _, _, _ string) error   { return nil }
func (s *preflightRecordingSandbox) WriteBytes(_ context.Context, _, _ string, _ []byte) error {
	return nil
}
func (s *preflightRecordingSandbox) ReadFile(_ context.Context, _, _ string) (string, error) {
	return "", nil
}
func (s *preflightRecordingSandbox) ReadBytes(_ context.Context, _, _ string) ([]byte, error) {
	return nil, nil
}
func (s *preflightRecordingSandbox) Kill(_ context.Context, _ string) error            { return nil }
func (s *preflightRecordingSandbox) RenewExpiration(_ context.Context, _ string) error { return nil }

func TestRunPreflight_ExecutesScriptInSandbox(t *testing.T) {
	sb := &preflightRecordingSandbox{}
	acts := &Activities{Sandbox: sb}
	out, err := acts.RunPreflight(context.Background(), workflow.RunPreflightInput{
		SandboxID: "sb-1",
		TeamID:    "team-1",
		Profile: model.AgentProfileBody{
			MCPs: []model.MCPConfig{{Name: "test-mcp", Transport: "sse", URL: "https://mcp.example.com/sse"}},
		},
	})
	require.NoError(t, err)
	require.Empty(t, out.EvalPluginDirs)
	require.Len(t, sb.execCmds, 1)
	assert.Contains(t, sb.execCmds[0], "claude mcp add")
	assert.Contains(t, sb.execCmds[0], "test-mcp")
}

func TestRunPreflight_ClonesEvalPlugins(t *testing.T) {
	sb := &preflightRecordingSandbox{}
	acts := &Activities{Sandbox: sb}
	out, err := acts.RunPreflight(context.Background(), workflow.RunPreflightInput{
		SandboxID:      "sb-1",
		TeamID:         "team-1",
		Profile:        model.AgentProfileBody{},
		EvalPluginURLs: []string{"https://github.com/org/repo/tree/main/plugins/foo"},
	})
	require.NoError(t, err)
	require.Len(t, out.EvalPluginDirs, 1)
	assert.Equal(t, "/tmp/eval-plugin-0/plugins/foo", out.EvalPluginDirs[0])
	// Should have one exec call for the git clone
	require.Len(t, sb.execCmds, 1)
	assert.Contains(t, sb.execCmds[0], "git clone")
	assert.Contains(t, sb.execCmds[0], "sparse-checkout set")
}

func TestRunPreflight_EmptyProfileSkipsExec(t *testing.T) {
	sb := &preflightRecordingSandbox{}
	acts := &Activities{Sandbox: sb}
	out, err := acts.RunPreflight(context.Background(), workflow.RunPreflightInput{
		SandboxID: "sb-1",
		TeamID:    "team-1",
		Profile:   model.AgentProfileBody{},
	})
	require.NoError(t, err)
	require.Empty(t, out.EvalPluginDirs)
	assert.Empty(t, sb.execCmds, "expected no Exec calls for empty profile")
}

func TestRunPreflight_ProfileWithPluginsAndMCPs(t *testing.T) {
	sb := &preflightRecordingSandbox{}
	acts := &Activities{Sandbox: sb}
	_, err := acts.RunPreflight(context.Background(), workflow.RunPreflightInput{
		SandboxID: "sb-1",
		TeamID:    "team-1",
		Profile: model.AgentProfileBody{
			Plugins: []model.PluginSource{{Plugin: "plugins/helm-doctor"}},
			MCPs:    []model.MCPConfig{{Name: "dt", Transport: "sse", URL: "https://dt.example.com/sse"}},
		},
	})
	require.NoError(t, err)
	// Should have exactly one Exec call containing the full script
	require.Len(t, sb.execCmds, 1)
	script := sb.execCmds[0]
	assert.True(t, strings.Contains(script, "claude plugin install") && strings.Contains(script, "claude mcp add"),
		"script should contain both plugin install and mcp add commands")
}

func TestRunPreflight_MarketplaceDBError_ReturnsError(t *testing.T) {
	// sqlx.Open doesn't connect, so use a bad DSN to force errors on first query.
	db, err := sqlx.Open("postgres", "postgres://invalid@localhost:5999/bad")
	if err != nil {
		t.Fatal(err)
	}
	acts := &Activities{
		DB:      db,
		Sandbox: &preflightRecordingSandbox{},
	}
	_, err = acts.RunPreflight(context.Background(), workflow.RunPreflightInput{
		SandboxID: "s1",
		TeamID:    "team-1",
		Profile:   model.AgentProfileBody{},
	})
	if err == nil {
		t.Fatal("expected error from DB failure, got nil")
	}
}

func TestRunPreflight_InvalidURLScheme_IsNonRetryable(t *testing.T) {
	acts := &Activities{
		Sandbox: &preflightRecordingSandbox{},
	}
	_, err := acts.RunPreflight(context.Background(), workflow.RunPreflightInput{
		SandboxID:      "s1",
		TeamID:         "team-1",
		Profile:        model.AgentProfileBody{},
		EvalPluginURLs: []string{"git://github.com/org/repo/tree/main/plugins/foo"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || !appErr.NonRetryable() {
		t.Errorf("expected NonRetryableApplicationError, got: %T %v", err, err)
	}
}
