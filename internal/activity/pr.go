package activity

import (
	"context"
	"fmt"
	"os"

	"github.com/google/go-github/v62/github"
	"go.temporal.io/sdk/activity"
	"golang.org/x/oauth2"

	"github.com/tinkerloft/fleetlift/internal/shellquote"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

// CreatePullRequest creates a PR from the changes in a sandbox.
// It pushes the branch from the sandbox and creates a GitHub PR.
func (a *Activities) CreatePullRequest(ctx context.Context, sandboxID string, input workflow.StepInput) (string, error) {
	activity.RecordHeartbeat(ctx, "creating PR")

	prDef := input.StepDef.PullRequest
	if prDef == nil {
		return "", fmt.Errorf("no PR configuration for step %s", input.StepDef.ID)
	}

	// Generate branch name
	branchName := fmt.Sprintf("%s/%s", prDef.BranchPrefix, input.RunID)

	// Create GitHub PR
	if len(input.ResolvedOpts.Repos) == 0 {
		return "", fmt.Errorf("no repos configured for PR creation")
	}

	repoDir := "/workspace/" + repoName(input.ResolvedOpts.Repos[0])

	// Create branch and push from sandbox
	cmds := []string{
		fmt.Sprintf("git -C %s checkout -b %s", shellquote.Quote(repoDir), shellquote.Quote(branchName)),
		fmt.Sprintf("git -C %s add -A", shellquote.Quote(repoDir)),
		fmt.Sprintf("git -C %s commit -m %s", shellquote.Quote(repoDir), shellquote.Quote(prDef.Title)),
		fmt.Sprintf("git -C %s push origin %s", shellquote.Quote(repoDir), shellquote.Quote(branchName)),
	}

	for _, cmd := range cmds {
		stdout, stderr, err := a.Sandbox.Exec(ctx, sandboxID, cmd, "/")
		if err != nil {
			return "", fmt.Errorf("git command %q: %w (stdout: %s, stderr: %s)", cmd, err, stdout, stderr)
		}
	}

	// Create GitHub PR
	repoURL := input.ResolvedOpts.Repos[0].URL
	owner, repo := extractOwnerRepo(repoURL)
	if owner == "" || repo == "" {
		return "", fmt.Errorf("could not parse owner/repo from %s", repoURL)
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return "", fmt.Errorf("GITHUB_TOKEN not set")
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	ghClient := github.NewClient(tc)

	baseBranch := DefaultBranch
	if len(input.ResolvedOpts.Repos) > 0 && input.ResolvedOpts.Repos[0].Branch != "" {
		baseBranch = input.ResolvedOpts.Repos[0].Branch
	}

	pr, _, err := ghClient.PullRequests.Create(ctx, owner, repo, &github.NewPullRequest{
		Title: github.String(prDef.Title),
		Body:  github.String(prDef.Body),
		Head:  github.String(branchName),
		Base:  github.String(baseBranch),
		Draft: github.Bool(prDef.Draft),
	})
	if err != nil {
		return "", fmt.Errorf("create PR: %w", err)
	}

	// Add labels if configured
	if len(prDef.Labels) > 0 {
		_, _, err = ghClient.Issues.AddLabelsToIssue(ctx, owner, repo, pr.GetNumber(), prDef.Labels)
		if err != nil {
			activity.GetLogger(ctx).Warn("failed to add labels to PR", "error", err)
		}
	}

	// Update step run with PR URL and branch
	if _, err := a.DB.ExecContext(ctx,
		`UPDATE step_runs SET pr_url = $1, branch_name = $2 WHERE id = $3`,
		pr.GetHTMLURL(), branchName, input.StepRunID); err != nil {
		activity.GetLogger(ctx).Warn("failed to record PR URL in step_run", "pr_url", pr.GetHTMLURL(), "error", err)
	}

	return pr.GetHTMLURL(), nil
}
