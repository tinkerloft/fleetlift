package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
)

func TestCreatePR_BranchNaming(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	exec.runFunc = func(ctx context.Context, opts CommandOpts) (*CommandResult, error) {
		if opts.Name == "gh" {
			return &CommandResult{Stdout: "https://github.com/org/svc/pull/1\n"}, nil
		}
		return &CommandResult{}, nil
	}
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		TaskID: "migration-123",
		PullRequest: &protocol.ManifestPRConfig{
			Title: "Fix bug",
			Body:  "Fixes #42",
		},
	}
	repo := &protocol.ManifestRepo{Name: "svc"}

	pr, err := p.createPR(context.Background(), manifest, repo, "/workspace/svc")
	require.NoError(t, err)

	assert.Equal(t, "auto/migration-123-svc", pr.BranchName)
	assert.Equal(t, "https://github.com/org/svc/pull/1", pr.URL)
	assert.Equal(t, "Fix bug", pr.Title)
}

func TestCreatePR_CustomBranchPrefix(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	exec.runFunc = func(ctx context.Context, opts CommandOpts) (*CommandResult, error) {
		if opts.Name == "gh" {
			return &CommandResult{Stdout: "https://github.com/org/svc/pull/1\n"}, nil
		}
		return &CommandResult{}, nil
	}
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		TaskID: "task-1",
		PullRequest: &protocol.ManifestPRConfig{
			BranchPrefix: "feat/slog",
			Title:        "Slog migration",
		},
	}
	repo := &protocol.ManifestRepo{Name: "svc"}

	pr, err := p.createPR(context.Background(), manifest, repo, "/workspace/svc")
	require.NoError(t, err)

	assert.Equal(t, "feat/slog-svc", pr.BranchName)
}

func TestCreatePR_GitIgnoreInjection(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	exec.runFunc = func(ctx context.Context, opts CommandOpts) (*CommandResult, error) {
		if opts.Name == "gh" {
			return &CommandResult{Stdout: "https://github.com/org/svc/pull/1\n"}, nil
		}
		return &CommandResult{}, nil
	}
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		TaskID:      "task-1",
		PullRequest: &protocol.ManifestPRConfig{Title: "Fix"},
	}
	repo := &protocol.ManifestRepo{Name: "svc"}

	_, err := p.createPR(context.Background(), manifest, repo, "/workspace/svc")
	require.NoError(t, err)

	// Verify .gitignore was written with sensitive patterns
	data, err := fs.ReadFile("/workspace/svc/.gitignore")
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, ".env")
	assert.Contains(t, content, "*.key")
	assert.Contains(t, content, "*.pem")
	assert.Contains(t, content, "credentials*")
	assert.Contains(t, content, ".git-credentials")
}

func TestCreatePR_GitIgnorePreservesExisting(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	exec.runFunc = func(ctx context.Context, opts CommandOpts) (*CommandResult, error) {
		if opts.Name == "gh" {
			return &CommandResult{Stdout: "https://github.com/org/svc/pull/1\n"}, nil
		}
		return &CommandResult{}, nil
	}
	p := testPipeline(fs, exec)

	// Pre-populate existing .gitignore
	existingContent := "node_modules/\nbuild/\n*.log\n"
	_ = fs.WriteFile("/workspace/svc/.gitignore", []byte(existingContent), 0644)

	manifest := &protocol.TaskManifest{
		TaskID:      "task-1",
		PullRequest: &protocol.ManifestPRConfig{Title: "Fix"},
	}
	repo := &protocol.ManifestRepo{Name: "svc"}

	_, err := p.createPR(context.Background(), manifest, repo, "/workspace/svc")
	require.NoError(t, err)

	// The gitignore written during staging should contain both original and injected patterns
	data, err := fs.ReadFile("/workspace/svc/.gitignore")
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "node_modules/")
	assert.Contains(t, content, "build/")
	assert.Contains(t, content, "fleetlift-agent: sensitive pattern injection")
	assert.Contains(t, content, ".env")
	assert.Contains(t, content, "*.key")
}

func TestCreatePR_GitCommands(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	exec.runFunc = func(ctx context.Context, opts CommandOpts) (*CommandResult, error) {
		if opts.Name == "gh" {
			return &CommandResult{Stdout: "https://github.com/org/svc/pull/1\n"}, nil
		}
		return &CommandResult{}, nil
	}
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		TaskID:      "task-1",
		PullRequest: &protocol.ManifestPRConfig{Title: "Fix bug"},
	}
	repo := &protocol.ManifestRepo{Name: "svc"}

	_, err := p.createPR(context.Background(), manifest, repo, "/workspace/svc")
	require.NoError(t, err)

	calls := exec.getCalls()
	// Verify key git commands: checkout -b, add -A, reset, checkout --, commit, push, gh pr create
	gitCmds := make([]string, 0)
	for _, c := range calls {
		if c.Name == "git" && len(c.Args) > 0 {
			gitCmds = append(gitCmds, c.Args[0])
		}
	}
	assert.Contains(t, gitCmds, "checkout")
	assert.Contains(t, gitCmds, "add")
	assert.Contains(t, gitCmds, "commit")
	assert.Contains(t, gitCmds, "push")
}

func TestCreatePullRequests_SkipsNoChanges(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		TaskID:       "task-1",
		Repositories: []protocol.ManifestRepo{{Name: "svc"}},
		PullRequest:  &protocol.ManifestPRConfig{Title: "Fix"},
	}

	results := []protocol.RepoResult{
		{Name: "svc", Status: "success", FilesModified: nil},
	}

	out := p.createPullRequests(context.Background(), manifest, results)
	assert.Nil(t, out[0].PullRequest, "should skip PR when no files modified")
}

func TestCreatePR_Labels_Reviewers(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	var ghArgs []string
	exec.runFunc = func(ctx context.Context, opts CommandOpts) (*CommandResult, error) {
		if opts.Name == "gh" {
			ghArgs = opts.Args
			return &CommandResult{Stdout: "https://github.com/org/svc/pull/1\n"}, nil
		}
		return &CommandResult{}, nil
	}
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		TaskID: "task-1",
		PullRequest: &protocol.ManifestPRConfig{
			Title:     "Fix",
			Labels:    []string{"automated", "review-needed"},
			Reviewers: []string{"alice", "bob"},
		},
	}
	repo := &protocol.ManifestRepo{Name: "svc"}

	_, err := p.createPR(context.Background(), manifest, repo, "/workspace/svc")
	require.NoError(t, err)

	// Check that labels and reviewers are passed
	ghStr := ""
	for _, a := range ghArgs {
		ghStr += a + " "
	}
	assert.Contains(t, ghStr, "--label")
	assert.Contains(t, ghStr, "--reviewer")
}
