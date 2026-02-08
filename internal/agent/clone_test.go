package agent

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
)

func testPipeline(fs *mockFS, exec *mockExecutor) *Pipeline {
	return NewPipeline("/tmp/test-fleetlift", fs, exec, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

func TestCloneRepos_GitCommandsIssued(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		TaskID: "task-1",
		Mode:   "transform",
		Repositories: []protocol.ManifestRepo{
			{Name: "svc", URL: "https://github.com/org/svc.git", Branch: "main"},
		},
		GitConfig: protocol.ManifestGitConfig{
			UserEmail:  "bot@test.com",
			UserName:   "Bot",
			CloneDepth: 10,
		},
	}

	// Set GITHUB_TOKEN to empty to skip credential setup
	t.Setenv("GITHUB_TOKEN", "")

	err := p.cloneRepos(context.Background(), manifest)
	require.NoError(t, err)

	calls := exec.getCalls()
	// Should have: git config email, git config name, git clone
	require.GreaterOrEqual(t, len(calls), 3)

	// First two are git config
	assert.Equal(t, "git", calls[0].Name)
	assert.Contains(t, calls[0].Args, "user.email")

	assert.Equal(t, "git", calls[1].Name)
	assert.Contains(t, calls[1].Args, "user.name")

	// Third is git clone
	assert.Equal(t, "git", calls[2].Name)
	assert.Equal(t, "clone", calls[2].Args[0])
	assert.Contains(t, calls[2].Args, "--depth")
	assert.Contains(t, calls[2].Args, "10")
}

func TestCloneRepos_CredentialStore(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		TaskID: "task-1",
		Mode:   "transform",
		Repositories: []protocol.ManifestRepo{
			{Name: "svc", URL: "https://github.com/org/svc.git"},
		},
		GitConfig: protocol.ManifestGitConfig{UserEmail: "bot@test.com", UserName: "Bot"},
	}

	t.Setenv("GITHUB_TOKEN", "ghp_test123")

	err := p.cloneRepos(context.Background(), manifest)
	require.NoError(t, err)

	// Check credential file was written
	homeDir, _ := os.UserHomeDir()
	credPath := homeDir + "/.git-credentials"
	credData, err := fs.ReadFile(credPath)
	require.NoError(t, err)
	assert.Contains(t, string(credData), "x-access-token:ghp_test123@github.com")

	// Check git config --global credential.helper store was called
	calls := exec.getCalls()
	found := false
	for _, c := range calls {
		if c.Name == "git" && len(c.Args) >= 3 && c.Args[2] == "credential.helper" {
			assert.Equal(t, "store", c.Args[3])
			found = true
		}
	}
	assert.True(t, found, "should call git config credential.helper store")
}

func TestCloneRepos_TransformationMode(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		TaskID: "task-1",
		Mode:   "transform",
		Transformation: &protocol.ManifestRepo{
			Name: "tools", URL: "https://github.com/org/tools.git", Branch: "main",
		},
		Targets: []protocol.ManifestRepo{
			{Name: "svc-a", URL: "https://github.com/org/svc-a.git", Branch: "main"},
		},
		GitConfig: protocol.ManifestGitConfig{UserEmail: "bot@test.com", UserName: "Bot"},
	}

	t.Setenv("GITHUB_TOKEN", "")

	err := p.cloneRepos(context.Background(), manifest)
	require.NoError(t, err)

	calls := exec.getCalls()
	// Find clone calls
	clones := 0
	for _, c := range calls {
		if c.Name == "git" && len(c.Args) > 0 && c.Args[0] == "clone" {
			clones++
		}
	}
	assert.Equal(t, 2, clones, "should clone transformation repo + 1 target")
}

func TestCloneRepos_SetupCommands(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		TaskID: "task-1",
		Mode:   "transform",
		Repositories: []protocol.ManifestRepo{
			{Name: "svc", URL: "https://github.com/org/svc.git", Setup: []string{"npm install"}},
		},
		GitConfig: protocol.ManifestGitConfig{UserEmail: "bot@test.com", UserName: "Bot"},
	}

	t.Setenv("GITHUB_TOKEN", "")

	err := p.cloneRepos(context.Background(), manifest)
	require.NoError(t, err)

	calls := exec.getCalls()
	found := false
	for _, c := range calls {
		if c.Name == "bash" && len(c.Args) == 2 && c.Args[1] == "npm install" {
			found = true
		}
	}
	assert.True(t, found, "should run setup command")
}
