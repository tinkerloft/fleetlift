package activity

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"

	"github.com/tinkerloft/fleetlift/internal/model"
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

	// Verify binary was uploaded to the staging path before install
	assert.Contains(t, sb.writtenFiles, "/tmp/fleetlift-mcp-upload")

	// Verify install cmd, test -x, start cmd, health check, and profile.d write were called
	assert.True(t, len(sb.execCmds) >= 5, "expected at least 5 exec calls, got %d", len(sb.execCmds))

	// Verify the nohup command does NOT contain --token flag
	for _, cmd := range sb.execCmds {
		if strings.Contains(cmd, "nohup") {
			assert.NotContains(t, cmd, "--token", "token should not be in CLI args")
			assert.Contains(t, cmd, "FLEETLIFT_MCP_TOKEN=", "token should be in env var")
		}
	}

	// Verify env file write includes both PORT and TOKEN
	foundTokenExport := false
	for _, cmd := range sb.execCmds {
		if strings.Contains(cmd, "FLEETLIFT_MCP_TOKEN") && strings.Contains(cmd, "fleetlift-mcp-env.sh") {
			foundTokenExport = true
		}
	}
	assert.True(t, foundTokenExport, "expected fleetlift-mcp-env.sh write to include FLEETLIFT_MCP_TOKEN export")

	// Verify health check command has correct port syntax (not shell-quoted inside perl string)
	for _, cmd := range sb.execCmds {
		if strings.Contains(cmd, "perl") {
			assert.Contains(t, cmd, `PeerAddr=>"localhost:8081"`, "port must not be shell-quoted inside perl single-quoted string")
			assert.NotContains(t, cmd, `localhost:'8081'`, "shellquote must not nest quotes inside perl string")
		}
	}
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
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	assert.True(t, appErr.NonRetryable(), "binary-not-found error must be non-retryable")
}

func TestProvisionSandbox_MCPFailsIfEnvFileNotCreated(t *testing.T) {
	tmpPrefix := t.TempDir() + "/fleetlift-mcp"
	require.NoError(t, os.WriteFile(tmpPrefix+"-amd64", []byte("fake-binary"), 0o755))

	t.Setenv("FLEETLIFT_MCP_BINARY_PATH", tmpPrefix)
	t.Setenv("JWT_SECRET", "test-secret-key-32bytes-minimum!")

	sb := newMCPSandbox()
	sb.failExec = "test -f /tmp/fleetlift-mcp-env.sh"
	a := &Activities{Sandbox: sb}

	input := workflow.StepInput{
		TeamID:       "team-1",
		RunID:        "run-1",
		ResolvedOpts: workflow.ResolvedStepOpts{Agent: "claude-code"},
	}
	_, err := a.ProvisionSandbox(context.Background(), input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP env file")
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
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	assert.True(t, appErr.NonRetryable(), "missing JWT_SECRET error must be non-retryable")
}

// mcpSandboxWithArch is like mcpSandbox but returns a custom arch string from uname -m.
type mcpSandboxWithArch struct {
	mcpSandbox
	arch string
}

func newMCPSandboxWithArch(arch string) *mcpSandboxWithArch {
	return &mcpSandboxWithArch{
		mcpSandbox: mcpSandbox{writtenFiles: make(map[string][]byte)},
		arch:       arch,
	}
}

func (m *mcpSandboxWithArch) Exec(ctx context.Context, sandboxID, cmd, dir string) (string, string, error) {
	if cmd == "uname -m" {
		m.execCmds = append(m.execCmds, cmd)
		return m.arch + "\n", "", nil
	}
	return m.mcpSandbox.Exec(ctx, sandboxID, cmd, dir)
}

func TestProvisionSandbox_MCPFailsOnUnsupportedArch(t *testing.T) {
	tmpPrefix := t.TempDir() + "/fleetlift-mcp"
	t.Setenv("FLEETLIFT_MCP_BINARY_PATH", tmpPrefix)
	t.Setenv("JWT_SECRET", "test-secret-key-32bytes-minimum!")

	a := &Activities{Sandbox: newMCPSandboxWithArch("mips")}
	input := workflow.StepInput{
		TeamID:       "team-1",
		ResolvedOpts: workflow.ResolvedStepOpts{Agent: "claude-code"},
	}
	_, err := a.ProvisionSandbox(context.Background(), input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported sandbox architecture")
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	assert.True(t, appErr.NonRetryable(), "unsupported arch error must be non-retryable")
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

// gitCredSandbox records WriteFile and Exec calls for git credential assertions.
type gitCredSandbox struct {
	noopSandbox
	execCmds     []string
	writtenFiles map[string]string
}

func newGitCredSandbox() *gitCredSandbox {
	return &gitCredSandbox{writtenFiles: make(map[string]string)}
}

func (s *gitCredSandbox) Create(_ context.Context, _ sandbox.CreateOpts) (string, error) {
	return "sb-cred", nil
}

func (s *gitCredSandbox) Exec(_ context.Context, _, cmd, _ string) (string, string, error) {
	s.execCmds = append(s.execCmds, cmd)
	return "", "", nil
}

func (s *gitCredSandbox) WriteFile(_ context.Context, _, path, content string) error {
	s.writtenFiles[path] = content
	return nil
}

func TestProvisionSandbox_ConfiguresGitCredentialsWhenGithubTokenPresent(t *testing.T) {
	sb := newGitCredSandbox()
	a := &Activities{
		Sandbox:   sb,
		CredStore: &stubCredStore{val: "ghp_testtoken"},
	}
	input := workflow.StepInput{
		TeamID: "team-1",
		ResolvedOpts: workflow.ResolvedStepOpts{
			Credentials: []string{"GITHUB_TOKEN"},
			Agent:       "shell",
		},
	}
	_, err := a.ProvisionSandbox(context.Background(), input)
	require.NoError(t, err)

	// No file should be written — credentials are configured via inline helper only
	_, fileWritten := sb.writtenFiles["/root/.git-credentials"]
	assert.False(t, fileWritten, "should not write .git-credentials file")

	// git config must be called with an inline credential helper that reads $GITHUB_TOKEN
	found := false
	for _, cmd := range sb.execCmds {
		if strings.Contains(cmd, "credential.helper") && strings.Contains(cmd, "GITHUB_TOKEN") {
			found = true
		}
	}
	assert.True(t, found, "expected git credential.helper config referencing GITHUB_TOKEN")
}

func TestProvisionSandbox_SkipsGitCredentialsWhenNoGithubToken(t *testing.T) {
	sb := newGitCredSandbox()
	a := &Activities{Sandbox: sb}

	input := workflow.StepInput{
		TeamID:       "team-1",
		ResolvedOpts: workflow.ResolvedStepOpts{Agent: "shell"},
	}
	_, err := a.ProvisionSandbox(context.Background(), input)
	require.NoError(t, err)

	_, ok := sb.writtenFiles["/root/.git-credentials"]
	assert.False(t, ok, "should not write .git-credentials when GITHUB_TOKEN is absent")

	for _, cmd := range sb.execCmds {
		assert.NotContains(t, cmd, "credential.helper", "should not configure git credential helper when GITHUB_TOKEN is absent")
	}
}

func TestCleanupCheckpointBranch_EmptyBranch(t *testing.T) {
	acts := &Activities{}
	err := acts.CleanupCheckpointBranch(context.Background(), model.CleanupCheckpointInput{Branch: ""})
	require.NoError(t, err) // empty branch = no-op
}

func TestCleanupCheckpointBranch_MissingCredential(t *testing.T) {
	acts := &Activities{}
	err := acts.CleanupCheckpointBranch(context.Background(), model.CleanupCheckpointInput{
		RepoURL:        "https://github.com/example/repo",
		Branch:         "fleetlift/checkpoint/run-abc-step-fix",
		CredentialName: "",
		TeamID:         "team-1",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "credential")
}

func TestInjectGitToken(t *testing.T) {
	result, err := injectGitToken("https://github.com/org/repo.git", "ghp_abc123")
	require.NoError(t, err)
	assert.Contains(t, result, "x-access-token:ghp_abc123@github.com")
	assert.True(t, strings.HasPrefix(result, "https://"))
}
