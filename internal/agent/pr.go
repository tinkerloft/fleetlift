package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
)

// sensitivePatterns are .gitignore patterns to exclude secrets from git add -A (H2 fix).
var sensitivePatterns = []string{
	".env",
	".env.*",
	"*.key",
	"*.pem",
	"credentials*",
	".git-credentials",
	".git-credential-helper",
	"*.secret",
}

// createPullRequests creates PRs for all repos with changes.
func (p *Pipeline) createPullRequests(ctx context.Context, manifest *protocol.TaskManifest, repoResults []protocol.RepoResult) []protocol.RepoResult {
	if manifest.PullRequest == nil {
		return repoResults
	}

	repos := manifest.EffectiveRepos()

	for i, result := range repoResults {
		if len(result.FilesModified) == 0 {
			continue
		}

		// Find the matching repo config
		var repo *protocol.ManifestRepo
		for _, r := range repos {
			if r.Name == result.Name {
				repo = &r
				break
			}
		}
		if repo == nil {
			continue
		}

		repoPath := manifest.RepoPath(repo.Name)

		pr, err := p.createPR(ctx, manifest, repo, repoPath)
		if err != nil {
			p.logger.Error("Failed to create PR", "repo", repo.Name, "error", err)
			errMsg := fmt.Sprintf("PR creation failed: %v", err)
			repoResults[i].Error = &errMsg
			continue
		}

		repoResults[i].PullRequest = pr
	}

	return repoResults
}

func (p *Pipeline) createPR(ctx context.Context, manifest *protocol.TaskManifest, repo *protocol.ManifestRepo, repoPath string) (*protocol.PRInfo, error) {
	prCfg := manifest.PullRequest

	// Determine branch name
	branchPrefix := "auto/" + manifest.TaskID
	if prCfg.BranchPrefix != "" {
		branchPrefix = prCfg.BranchPrefix
	}
	branchName := branchPrefix + "-" + repo.Name

	// Create branch
	if err := p.gitExec(ctx, repoPath, "checkout", "-b", branchName); err != nil {
		return nil, fmt.Errorf("create branch: %w", err)
	}

	// H2 fix: inject .gitignore to block sensitive patterns before git add -A
	gitignorePath := filepath.Join(repoPath, ".gitignore")
	preAddContent, _ := p.fs.ReadFile(gitignorePath) // capture current state (includes transformation changes)
	marker := "\n# fleetlift-agent: sensitive pattern injection\n"
	gitignoreContent := string(preAddContent) + marker + strings.Join(sensitivePatterns, "\n") + "\n"
	if err := p.fs.WriteFile(gitignorePath, []byte(gitignoreContent), 0644); err != nil {
		p.logger.Warn("Failed to write .gitignore for secret exclusion", "error", err)
	}

	// Stage all changes (respects .gitignore)
	if err := p.gitExec(ctx, repoPath, "add", "-A"); err != nil {
		return nil, fmt.Errorf("git add: %w", err)
	}

	// Restore the pre-injection .gitignore content (preserving transformation changes, removing injection)
	if len(preAddContent) > 0 {
		if err := p.fs.WriteFile(gitignorePath, preAddContent, 0644); err != nil {
			p.logger.Warn("Failed to restore .gitignore", "error", err)
		}
		// Re-stage the restored .gitignore so the commit has the correct version
		_ = p.gitExec(ctx, repoPath, "add", ".gitignore")
	} else {
		// No .gitignore existed before the transformation or injection -- unstage it
		_ = p.gitExec(ctx, repoPath, "reset", "HEAD", ".gitignore")
		_ = p.fs.Remove(gitignorePath)
	}

	// Commit
	commitMsg := prCfg.Title
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("fix: %s", manifest.Title)
	}

	if err := p.gitExec(ctx, repoPath, "commit", "-m", commitMsg); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}

	// Push
	if err := p.gitExec(ctx, repoPath, "push", "origin", branchName); err != nil {
		return nil, fmt.Errorf("git push: %w", err)
	}

	// Create PR via gh CLI
	ghArgs := []string{"pr", "create",
		"--title", prCfg.Title,
		"--body", prCfg.Body,
		"--head", branchName,
	}

	for _, label := range prCfg.Labels {
		ghArgs = append(ghArgs, "--label", label)
	}
	for _, reviewer := range prCfg.Reviewers {
		ghArgs = append(ghArgs, "--reviewer", reviewer)
	}

	result, err := p.exec.Run(ctx, CommandOpts{
		Name: "gh",
		Args: ghArgs,
		Dir:  repoPath,
	})
	if err != nil {
		return nil, fmt.Errorf("gh pr create: %w", err)
	}

	prURL := ""
	if result != nil {
		prURL = strings.TrimSpace(result.Stdout)
	}

	return &protocol.PRInfo{
		URL:        prURL,
		BranchName: branchName,
		Title:      prCfg.Title,
	}, nil
}

func (p *Pipeline) gitExec(ctx context.Context, repoPath string, args ...string) error {
	result, err := p.exec.Run(ctx, CommandOpts{
		Name: "git",
		Args: args,
		Dir:  repoPath,
	})
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("%s: %s", strings.Join(args, " "), stderr)
	}
	return nil
}
