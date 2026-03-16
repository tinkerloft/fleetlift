package activity

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

// execRecordingSandbox records Exec calls for assertion in tests.
type execRecordingSandbox struct {
	noopSandbox
	execCmds []string
}

func (s *execRecordingSandbox) Create(_ context.Context, _ sandbox.CreateOpts) (string, error) {
	return "sb-test", nil
}

func (s *execRecordingSandbox) Exec(_ context.Context, _, cmd, _ string) (string, string, error) {
	s.execCmds = append(s.execCmds, cmd)
	return "", "", nil
}

func TestProvisionSandbox_RejectsInvalidCredentialName(t *testing.T) {
	a := &Activities{
		Sandbox:   &noopSandbox{},
		CredStore: &stubCredStore{},
	}

	invalidNames := []string{
		"lower_case",
		"123START",
		"HAS SPACE",
		"HAS-DASH",
		"has.dot",
		"",
		"A" + string(make([]byte, 64)), // 65 chars total — too long
	}

	for _, name := range invalidNames {
		t.Run(name, func(t *testing.T) {
			input := workflow.StepInput{
				TeamID: "team-1",
				ResolvedOpts: workflow.ResolvedStepOpts{
					Credentials: []string{name},
					Agent:       "claude-code",
				},
			}
			_, err := a.ProvisionSandbox(context.Background(), input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid credential name")
		})
	}
}

func TestProvisionSandbox_RejectsReservedEnvVar(t *testing.T) {
	a := &Activities{
		Sandbox:   &noopSandbox{},
		CredStore: &stubCredStore{},
	}

	reserved := []string{"PATH", "LD_PRELOAD", "LD_LIBRARY_PATH", "HOME", "USER", "SHELL"}
	for _, name := range reserved {
		t.Run(name, func(t *testing.T) {
			input := workflow.StepInput{
				TeamID: "team-1",
				ResolvedOpts: workflow.ResolvedStepOpts{
					Credentials: []string{name},
					Agent:       "claude-code",
				},
			}
			_, err := a.ProvisionSandbox(context.Background(), input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "reserved environment variable")
		})
	}
}

func TestProvisionSandbox_AcceptsValidCredentialName(t *testing.T) {
	a := &Activities{
		Sandbox:   &noopSandbox{},
		CredStore: &stubCredStore{val: "secret"},
	}

	validNames := []string{
		"API_KEY",
		"MY_TOKEN",
		"A",
		"GITHUB_TOKEN",
		"X123",
	}

	for _, name := range validNames {
		t.Run(name, func(t *testing.T) {
			input := workflow.StepInput{
				TeamID: "team-1",
				ResolvedOpts: workflow.ResolvedStepOpts{
					Credentials: []string{name},
					Agent:       "claude-code",
				},
			}
			_, err := a.ProvisionSandbox(context.Background(), input)
			// No validation error; may succeed or fail for other reasons (e.g. DB).
			if err != nil {
				assert.NotContains(t, err.Error(), "invalid credential name")
				assert.NotContains(t, err.Error(), "reserved environment variable")
			}
		})
	}
}

// stubCredStore returns a fixed value for any credential lookup.
type stubCredStore struct {
	val string
}

func (s *stubCredStore) Get(_ context.Context, _, _ string) (string, error) {
	return s.val, nil
}

func (s *stubCredStore) GetBatch(_ context.Context, _ string, names []string) (map[string]string, error) {
	result := make(map[string]string, len(names))
	for _, name := range names {
		result[name] = s.val
	}
	return result, nil
}

func TestProvisionSandbox_CreatesWorkspace(t *testing.T) {
	rec := &execRecordingSandbox{}
	a := &Activities{Sandbox: rec}

	input := workflow.StepInput{
		TeamID:       "team-1",
		ResolvedOpts: workflow.ResolvedStepOpts{Agent: "shell"},
	}
	_, err := a.ProvisionSandbox(context.Background(), input)
	require.NoError(t, err)

	assert.Contains(t, rec.execCmds, "mkdir -p /workspace",
		"ProvisionSandbox must create /workspace so agent commands can use it as cwd")
}

// mcpSandbox records exec calls and returns "ok" for health check requests.
type mcpSandbox struct {
	noopSandbox
	execCmds     []string
	writtenFiles map[string][]byte
	failExec     string // if set, Exec returns error when cmd contains this string
}

func newMCPSandbox() *mcpSandbox {
	return &mcpSandbox{writtenFiles: make(map[string][]byte)}
}

func (m *mcpSandbox) Create(_ context.Context, _ sandbox.CreateOpts) (string, error) {
	return "sb-mcp", nil
}

func (m *mcpSandbox) Exec(_ context.Context, _, cmd, _ string) (string, string, error) {
	m.execCmds = append(m.execCmds, cmd)
	if m.failExec != "" && strings.Contains(cmd, m.failExec) {
		return "", "", fmt.Errorf("exec failed: %s", m.failExec)
	}
	// Architecture detection
	if cmd == "uname -m" {
		return "x86_64\n", "", nil
	}
	// Health check returns "ok"
	if strings.Contains(cmd, "/health") {
		return `{"status":"ok"}`, "", nil
	}
	return "", "", nil
}

func (m *mcpSandbox) WriteBytes(_ context.Context, _, path string, data []byte) error {
	m.writtenFiles[path] = data
	return nil
}

func (m *mcpSandbox) Kill(_ context.Context, _ string) error { return nil }

func TestProvisionSandbox_MCPSetup(t *testing.T) {
	// Create a temp file to act as the MCP binary (arch-suffixed).
	tmpPrefix := t.TempDir() + "/fleetlift-mcp"
	require.NoError(t, os.WriteFile(tmpPrefix+"-amd64", []byte("fake-binary"), 0o755))

	t.Setenv("FLEETLIFT_MCP_BINARY_PATH", tmpPrefix)
	t.Setenv("JWT_SECRET", "test-secret-key-32bytes-minimum!")

	sb := newMCPSandbox()
	a := &Activities{Sandbox: sb}

	input := workflow.StepInput{
		TeamID:       "team-1",
		RunID:        "run-1",
		ResolvedOpts: workflow.ResolvedStepOpts{Agent: "claude-code"},
	}
	sandboxID, err := a.ProvisionSandbox(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, "sb-mcp", sandboxID)

	// Verify binary was uploaded
	assert.Contains(t, sb.writtenFiles, "/usr/local/bin/fleetlift-mcp")

	// Verify chmod, test -x, start cmd, health check, and profile.d write were called
	assert.True(t, len(sb.execCmds) >= 5, "expected at least 5 exec calls, got %d", len(sb.execCmds))

	// Verify the nohup command does NOT contain --token flag
	for _, cmd := range sb.execCmds {
		if strings.Contains(cmd, "nohup") {
			assert.NotContains(t, cmd, "--token", "token should not be in CLI args")
			assert.Contains(t, cmd, "FLEETLIFT_MCP_TOKEN=", "token should be in env var")
		}
	}

	// Verify profile.d write includes both PORT and TOKEN
	foundTokenExport := false
	for _, cmd := range sb.execCmds {
		if strings.Contains(cmd, "FLEETLIFT_MCP_TOKEN") && strings.Contains(cmd, "profile.d") {
			foundTokenExport = true
		}
	}
	assert.True(t, foundTokenExport, "expected profile.d write to include FLEETLIFT_MCP_TOKEN export")
}

func TestProvisionSandbox_MCPSkippedWhenBinaryMissing(t *testing.T) {
	// Don't set FLEETLIFT_MCP_BINARY_PATH — MCP should be skipped entirely.
	t.Setenv("FLEETLIFT_MCP_BINARY_PATH", "")

	rec := &execRecordingSandbox{}
	a := &Activities{Sandbox: rec}

	input := workflow.StepInput{
		TeamID:       "team-1",
		ResolvedOpts: workflow.ResolvedStepOpts{Agent: "claude-code"},
	}
	sandboxID, err := a.ProvisionSandbox(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, "sb-test", sandboxID)

	// Only the workspace creation command should have been executed.
	assert.Equal(t, []string{"mkdir -p /workspace"}, rec.execCmds)
}

func TestProvisionSandbox_MCPFailsWhenBinaryUnreadable(t *testing.T) {
	t.Setenv("FLEETLIFT_MCP_BINARY_PATH", "/nonexistent/path/fleetlift-mcp")

	a := &Activities{Sandbox: newMCPSandbox()}
	input := workflow.StepInput{
		TeamID:       "team-1",
		ResolvedOpts: workflow.ResolvedStepOpts{Agent: "claude-code"},
	}
	_, err := a.ProvisionSandbox(context.Background(), input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP binary not found")
}

func TestProvisionSandbox_MCPFailsWhenJWTSecretEmpty(t *testing.T) {
	tmpPrefix := t.TempDir() + "/fleetlift-mcp"
	require.NoError(t, os.WriteFile(tmpPrefix+"-amd64", []byte("fake"), 0o755))

	t.Setenv("FLEETLIFT_MCP_BINARY_PATH", tmpPrefix)
	t.Setenv("JWT_SECRET", "")

	a := &Activities{Sandbox: newMCPSandbox()}
	input := workflow.StepInput{
		TeamID:       "team-1",
		ResolvedOpts: workflow.ResolvedStepOpts{Agent: "claude-code"},
	}
	_, err := a.ProvisionSandbox(context.Background(), input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT_SECRET is required")
}

func TestProvisionSandbox_MCPFailsOnHealthCheckTimeout(t *testing.T) {
	tmpPrefix := t.TempDir() + "/fleetlift-mcp"
	require.NoError(t, os.WriteFile(tmpPrefix+"-amd64", []byte("fake"), 0o755))

	t.Setenv("FLEETLIFT_MCP_BINARY_PATH", tmpPrefix)
	t.Setenv("JWT_SECRET", "test-secret-key-32bytes-minimum!")

	// Sandbox that never returns "ok" for health checks.
	sb := newMCPSandbox()
	sb.failExec = "/health"
	a := &Activities{Sandbox: sb}

	input := workflow.StepInput{
		TeamID:       "team-1",
		RunID:        "run-1",
		ResolvedOpts: workflow.ResolvedStepOpts{Agent: "claude-code"},
	}
	_, err := a.ProvisionSandbox(context.Background(), input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "health check failed")
}
