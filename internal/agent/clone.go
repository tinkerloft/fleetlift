package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
)

// cloneRepos clones all repositories defined in the manifest.
func (p *Pipeline) cloneRepos(ctx context.Context, manifest *protocol.TaskManifest) error {
	// Configure git identity
	if err := p.configureGit(ctx, manifest.GitConfig); err != nil {
		return fmt.Errorf("git config failed: %w", err)
	}

	// Configure git credentials if GITHUB_TOKEN is set
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		if err := p.configureGitCredentials(ctx, token); err != nil {
			p.logger.Warn("Failed to configure git credentials", "error", err)
		}
	}

	// Clone transformation repo at /workspace if set
	if manifest.Transformation != nil {
		if err := p.cloneRepo(ctx, manifest.Transformation, protocol.WorkspacePath, manifest.GitConfig.CloneDepth); err != nil {
			return fmt.Errorf("clone transformation repo: %w", err)
		}
		// Run setup commands
		for _, cmd := range manifest.Transformation.Setup {
			if err := p.runShell(ctx, protocol.WorkspacePath, cmd); err != nil {
				return fmt.Errorf("transformation setup %q: %w", cmd, err)
			}
		}
	}

	// Determine target base path
	basePath := manifest.RepoBasePath()
	if manifest.Transformation != nil {
		if err := p.fs.MkdirAll(basePath, 0755); err != nil {
			return fmt.Errorf("create targets dir: %w", err)
		}
	}

	// Determine repos to clone
	repos := manifest.EffectiveRepos()

	for _, repo := range repos {
		repoPath := manifest.RepoPath(repo.Name)
		if err := p.cloneRepo(ctx, &repo, repoPath, manifest.GitConfig.CloneDepth); err != nil {
			return fmt.Errorf("clone %s: %w", repo.Name, err)
		}
		// Run setup commands
		for _, cmd := range repo.Setup {
			if err := p.runShell(ctx, repoPath, cmd); err != nil {
				return fmt.Errorf("setup %s %q: %w", repo.Name, cmd, err)
			}
		}
	}

	// Write AGENTS.md
	agentsMD := generateAgentsMD(manifest)
	if err := p.fs.WriteFile(filepath.Join(protocol.WorkspacePath, "AGENTS.md"), []byte(agentsMD), 0644); err != nil {
		p.logger.Warn("Failed to write AGENTS.md", "error", err)
	}

	return nil
}

func (p *Pipeline) cloneRepo(ctx context.Context, repo *protocol.ManifestRepo, destPath string, depth int) error {
	branch := repo.Branch
	if branch == "" {
		branch = "main"
	}
	if depth <= 0 {
		depth = DefaultCloneDepth
	}

	p.logger.Info("Cloning repository", "url", repo.URL, "branch", branch, "dest", destPath)

	_, err := p.exec.Run(ctx, CommandOpts{
		Name: "git",
		Args: []string{"clone", "--branch", branch, "--depth", strconv.Itoa(depth), repo.URL, destPath},
	})
	return err
}

func (p *Pipeline) configureGit(ctx context.Context, cfg protocol.ManifestGitConfig) error {
	commands := [][]string{
		{"git", "config", "--global", "user.email", cfg.UserEmail},
		{"git", "config", "--global", "user.name", cfg.UserName},
	}
	for _, args := range commands {
		result, err := p.exec.Run(ctx, CommandOpts{Name: args[0], Args: args[1:]})
		if err != nil {
			stderr := ""
			if result != nil {
				stderr = result.Stderr
			}
			return fmt.Errorf("%s: %s", strings.Join(args, " "), stderr)
		}
	}
	return nil
}

// configureGitCredentials sets up git credential.store to avoid exposing tokens (C4 fix).
func (p *Pipeline) configureGitCredentials(ctx context.Context, token string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/home/agent"
	}

	// Write credentials file (plain format, no shell interpretation)
	credPath := filepath.Join(homeDir, ".git-credentials")
	credContent := fmt.Sprintf("https://x-access-token:%s@github.com\n", token)
	if err := p.fs.WriteFile(credPath, []byte(credContent), 0600); err != nil {
		return err
	}

	_, err = p.exec.Run(ctx, CommandOpts{
		Name: "git",
		Args: []string{"config", "--global", "credential.helper", "store"},
	})
	return err
}

func (p *Pipeline) runShell(ctx context.Context, dir string, command string) error {
	p.logger.Info("Running setup command", "command", command, "dir", dir)
	_, err := p.exec.Run(ctx, CommandOpts{
		Name: "bash",
		Args: []string{"-c", command},
		Dir:  dir,
	})
	return err
}

func generateAgentsMD(manifest *protocol.TaskManifest) string {
	var sb strings.Builder
	sb.WriteString("# Agent Instructions\n\n")
	sb.WriteString(fmt.Sprintf("## Task: %s\n\n", manifest.Title))
	sb.WriteString(fmt.Sprintf("**Task ID:** %s\n\n", manifest.TaskID))

	if manifest.Execution.Prompt != "" {
		sb.WriteString(fmt.Sprintf("**Prompt:**\n%s\n\n", manifest.Execution.Prompt))
	}

	if manifest.Transformation != nil {
		sb.WriteString("## Transformation Repository\n\n")
		sb.WriteString(fmt.Sprintf("- `%s` (branch: %s)\n\n", manifest.Transformation.Name, manifest.Transformation.Branch))
	}

	sb.WriteString("## Repositories\n\n")
	repos := manifest.EffectiveRepos()
	for _, repo := range repos {
		if manifest.Transformation != nil {
			sb.WriteString(fmt.Sprintf("- `%s` (branch: %s) at `%s`\n", repo.Name, repo.Branch, manifest.RepoPath(repo.Name)))
		} else {
			sb.WriteString(fmt.Sprintf("- `%s` (branch: %s)\n", repo.Name, repo.Branch))
		}
	}

	sb.WriteString("\n## Guidelines\n\n")
	sb.WriteString("1. Focus on the specific task described above\n")
	sb.WriteString("2. Make minimal, targeted changes\n")
	sb.WriteString("3. Follow existing code style and patterns\n")
	sb.WriteString("4. Add tests if applicable\n")
	sb.WriteString("5. Do not modify unrelated files\n")

	return sb.String()
}
