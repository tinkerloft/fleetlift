package activity

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"
)

// ExecuteAction dispatches an action step to the appropriate handler based on action type.
func (a *Activities) ExecuteAction(ctx context.Context, stepRunID string, actionType string, config map[string]any, teamID string, credNames []string) (map[string]any, error) {
	// Fetch credentials if requested.
	var credentials map[string]string
	if len(credNames) > 0 && a.CredStore != nil {
		var err error
		credentials, err = a.CredStore.GetBatch(ctx, teamID, credNames)
		if err != nil {
			return nil, fmt.Errorf("fetch credentials: %w", err)
		}
	}

	logAction(ctx, a, stepRunID, fmt.Sprintf("Executing action: %s", actionType))

	var result map[string]any
	var err error

	switch actionType {
	case "slack_notify":
		result, err = actionNotifySlack(ctx, config, credentials)
	case "github_pr_review":
		result, err = actionGitHubPostReviewComment(ctx, config, credentials)
	case "github_assign":
		result, err = actionGitHubAssignIssue(ctx, config, credentials)
	case "github_label":
		result, err = actionGitHubAddLabel(ctx, config, credentials)
	case "github_comment":
		result, err = actionGitHubPostIssueComment(ctx, config, credentials)
	case "create_pr":
		// PR creation is handled by CreatePullRequest activity directly
		return map[string]any{"status": "skipped_in_action"}, nil
	default:
		return nil, fmt.Errorf("unknown action type: %s", actionType)
	}

	if err != nil {
		logAction(ctx, a, stepRunID, fmt.Sprintf("Action failed: %v", err))
		return nil, err
	}
	logAction(ctx, a, stepRunID, "Action completed successfully")
	return result, nil
}

// logAction writes a single log line for an action step run.
func logAction(ctx context.Context, a *Activities, stepRunID, msg string) {
	if a.DB == nil || stepRunID == "" {
		return
	}
	_ = batchInsertLogs(ctx, a, stepRunID, []logLine{{Seq: 0, Stream: "stdout", Content: msg}})
}

// credOrEnv returns the credential value from the map, falling back to os.Getenv.
func credOrEnv(credentials map[string]string, name string) string {
	if v, ok := credentials[name]; ok && v != "" {
		return v
	}
	return os.Getenv(name)
}

func actionNotifySlack(ctx context.Context, config map[string]any, credentials map[string]string) (map[string]any, error) {
	channel, _ := config["channel"].(string)
	message, _ := config["message"].(string)
	if channel == "" || message == "" {
		return map[string]any{"status": "skipped", "reason": "missing channel or message"}, nil
	}

	slackActs := NewSlackActivities()
	_, err := slackActs.NotifySlack(ctx, channel, message, nil)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "sent", "channel": channel}, nil
}

func actionGitHubPostReviewComment(ctx context.Context, config map[string]any, credentials map[string]string) (map[string]any, error) {
	repoURL, _ := config["repo_url"].(string)
	prNumber := toInt(config["pr_number"])
	summary, _ := config["summary"].(string)

	if repoURL == "" || prNumber == 0 {
		return nil, fmt.Errorf("github_pr_review: missing repo_url or pr_number")
	}

	if summary == "" {
		return map[string]any{"status": "skipped", "reason": "empty summary"}, nil
	}

	token := credOrEnv(credentials, "GITHUB_TOKEN")
	ghClient := newGitHubClientWithToken(ctx, token)
	if ghClient == nil {
		return nil, fmt.Errorf("github_pr_review: GITHUB_TOKEN is not set")
	}

	owner, repo := extractOwnerRepo(repoURL)
	review, _, err := ghClient.PullRequests.CreateReview(ctx, owner, repo, prNumber, &github.PullRequestReviewRequest{
		Body:  github.String(summary),
		Event: github.String("COMMENT"),
	})
	if err != nil {
		return nil, err
	}
	reviewID := int64(0)
	if review != nil && review.ID != nil {
		reviewID = *review.ID
	}
	return map[string]any{"status": "posted", "review_id": reviewID}, nil
}

func actionGitHubAssignIssue(ctx context.Context, config map[string]any, _ map[string]string) (map[string]any, error) {
	repoURL, _ := config["repo_url"].(string)
	issueNumber := toInt(config["issue_number"])

	if repoURL == "" || issueNumber == 0 {
		return map[string]any{"status": "skipped", "reason": "not configured"}, nil
	}

	// Assignment logic would look up CODEOWNERS or a team map; for now, skip
	return map[string]any{"status": "skipped", "reason": "not configured"}, nil
}

func actionGitHubAddLabel(ctx context.Context, config map[string]any, credentials map[string]string) (map[string]any, error) {
	repoURL, _ := config["repo_url"].(string)
	issueNumber := toInt(config["issue_number"])
	labels := toStringSlice(config["labels"])

	if repoURL == "" || issueNumber == 0 || len(labels) == 0 {
		return nil, fmt.Errorf("github_label: missing repo_url, issue_number, or labels")
	}

	token := credOrEnv(credentials, "GITHUB_TOKEN")
	ghClient := newGitHubClientWithToken(ctx, token)
	if ghClient == nil {
		return nil, fmt.Errorf("GITHUB_TOKEN not set")
	}

	owner, repo := extractOwnerRepo(repoURL)
	applied, _, err := ghClient.Issues.AddLabelsToIssue(ctx, owner, repo, issueNumber, labels)
	if err != nil {
		return nil, err
	}
	appliedNames := make([]string, 0, len(applied))
	for _, l := range applied {
		if l.Name != nil {
			appliedNames = append(appliedNames, *l.Name)
		}
	}
	return map[string]any{"status": "labeled", "labels": appliedNames}, nil
}

func actionGitHubPostIssueComment(ctx context.Context, config map[string]any, credentials map[string]string) (map[string]any, error) {
	repoURL, _ := config["repo_url"].(string)
	issueNumber := toInt(config["issue_number"])
	body, _ := config["body"].(string)

	if repoURL == "" || issueNumber == 0 || body == "" {
		return nil, fmt.Errorf("github_comment: missing repo_url, issue_number, or body")
	}

	token := credOrEnv(credentials, "GITHUB_TOKEN")
	ghClient := newGitHubClientWithToken(ctx, token)
	if ghClient == nil {
		return nil, fmt.Errorf("GITHUB_TOKEN not set")
	}

	owner, repo := extractOwnerRepo(repoURL)
	comment, _, err := ghClient.Issues.CreateComment(ctx, owner, repo, issueNumber, &github.IssueComment{
		Body: github.String(body),
	})
	if err != nil {
		return nil, err
	}
	commentID := int64(0)
	if comment != nil && comment.ID != nil {
		commentID = *comment.ID
	}
	return map[string]any{"status": "posted", "comment_id": commentID}, nil
}

func newGitHubClientWithToken(ctx context.Context, token string) *github.Client {
	if token == "" {
		return nil
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	return github.NewClient(oauth2.NewClient(ctx, ts))
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
