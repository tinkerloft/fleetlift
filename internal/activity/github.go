// Package activity contains Temporal activity implementations.
// TODO: GitHub activities will be implemented/extended in Phase 9.
package activity

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-github/v62/github"
	"go.temporal.io/sdk/activity"
	"golang.org/x/oauth2"
)

// GitHubActivities contains activities for GitHub operations.
type GitHubActivities struct{}

// NewGitHubActivities creates a new GitHubActivities instance.
func NewGitHubActivities() *GitHubActivities {
	return &GitHubActivities{}
}

// extractOwnerRepo extracts owner and repo name from a GitHub URL.
func extractOwnerRepo(url string) (string, string) {
	url = strings.TrimSuffix(url, "/")
	url = strings.TrimSuffix(url, ".git")
	parts := strings.Split(url, "/")
	if len(parts) < 2 {
		return "", ""
	}
	return parts[len(parts)-2], parts[len(parts)-1]
}

// PostIssueComment posts a comment on a GitHub issue.
func (a *GitHubActivities) PostIssueComment(ctx context.Context, repoURL string, issueNumber int, body string) error {
	_ = activity.GetLogger(ctx)
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fmt.Errorf("GITHUB_TOKEN not set")
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)
	owner, repo := extractOwnerRepo(repoURL)
	comment := &github.IssueComment{Body: github.String(body)}
	_, _, err := client.Issues.CreateComment(ctx, owner, repo, issueNumber, comment)
	return err
}
