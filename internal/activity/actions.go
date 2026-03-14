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

// ExecuteAction dispatches an action step to the appropriate handler based on action type.
func (a *Activities) ExecuteAction(ctx context.Context, actionType string, config map[string]any) (map[string]any, error) {
	switch actionType {
	case "slack_notify":
		return nil, a.actionNotifySlack(ctx, config)
	case "github_pr_review":
		return nil, a.actionGitHubPostReviewComment(ctx, config)
	case "github_assign":
		return nil, a.actionGitHubAssignIssue(ctx, config)
	case "github_label":
		return nil, a.actionGitHubAddLabel(ctx, config)
	case "github_comment":
		return nil, a.actionGitHubPostIssueComment(ctx, config)
	case "create_pr":
		// PR creation is handled by CreatePullRequest activity directly
		return map[string]any{"status": "skipped_in_action"}, nil
	default:
		return nil, fmt.Errorf("unknown action type: %s", actionType)
	}
}

func (a *Activities) actionNotifySlack(ctx context.Context, config map[string]any) error {
	channel, _ := config["channel"].(string)
	message, _ := config["message"].(string)
	if channel == "" || message == "" {
		activity.GetLogger(ctx).Warn("slack_notify: missing channel or message")
		return nil
	}

	slackActs := NewSlackActivities()
	_, err := slackActs.NotifySlack(ctx, channel, message, nil)
	return err
}

func (a *Activities) actionGitHubPostReviewComment(ctx context.Context, config map[string]any) error {
	repoURL, _ := config["repo_url"].(string)
	prNumber := toInt(config["pr_number"])
	summary, _ := config["summary"].(string)

	if repoURL == "" || prNumber == 0 {
		return fmt.Errorf("github_pr_review: missing repo_url or pr_number")
	}

	ghClient := newGitHubClient(ctx)
	if ghClient == nil {
		activity.GetLogger(ctx).Warn("github_pr_review: GITHUB_TOKEN not set, skipping PR comment")
		return nil
	}

	if summary == "" {
		activity.GetLogger(ctx).Warn("github_pr_review: empty summary, skipping")
		return nil
	}

	owner, repo := extractOwnerRepo(repoURL)
	_, _, err := ghClient.PullRequests.CreateReview(ctx, owner, repo, prNumber, &github.PullRequestReviewRequest{
		Body:  github.String(summary),
		Event: github.String("COMMENT"),
	})
	return err
}

func (a *Activities) actionGitHubAssignIssue(ctx context.Context, config map[string]any) error {
	repoURL, _ := config["repo_url"].(string)
	issueNumber := toInt(config["issue_number"])

	if repoURL == "" || issueNumber == 0 {
		return fmt.Errorf("github_assign: missing repo_url or issue_number")
	}

	// Assignment logic would look up CODEOWNERS or a team map; for now, log and skip
	activity.GetLogger(ctx).Info("github_assign: auto-assignment not yet configured",
		"repo", repoURL, "issue", issueNumber)
	return nil
}

func (a *Activities) actionGitHubAddLabel(ctx context.Context, config map[string]any) error {
	repoURL, _ := config["repo_url"].(string)
	issueNumber := toInt(config["issue_number"])
	labels := toStringSlice(config["labels"])

	if repoURL == "" || issueNumber == 0 || len(labels) == 0 {
		return fmt.Errorf("github_label: missing repo_url, issue_number, or labels")
	}

	ghClient := newGitHubClient(ctx)
	if ghClient == nil {
		return fmt.Errorf("GITHUB_TOKEN not set")
	}

	owner, repo := extractOwnerRepo(repoURL)
	_, _, err := ghClient.Issues.AddLabelsToIssue(ctx, owner, repo, issueNumber, labels)
	return err
}

func (a *Activities) actionGitHubPostIssueComment(ctx context.Context, config map[string]any) error {
	repoURL, _ := config["repo_url"].(string)
	issueNumber := toInt(config["issue_number"])
	body, _ := config["body"].(string)

	if repoURL == "" || issueNumber == 0 || body == "" {
		return fmt.Errorf("github_comment: missing repo_url, issue_number, or body")
	}

	ghActs := NewGitHubActivities()
	return ghActs.PostIssueComment(ctx, repoURL, issueNumber, body)
}

func newGitHubClient(ctx context.Context) *github.Client {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc)
}

func toInt(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case int64:
		return int(val)
	case string:
		// Simple string-to-int; template rendering may produce strings
		var n int
		_, _ = fmt.Sscanf(val, "%d", &n)
		return n
	}
	return 0
}

func toStringSlice(v any) []string {
	switch val := v.(type) {
	case []string:
		return val
	case []any:
		var out []string
		for _, item := range val {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if val != "" {
			return strings.Split(val, ",")
		}
	}
	return nil
}

