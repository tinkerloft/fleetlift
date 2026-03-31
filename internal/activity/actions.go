package activity

import (
	"context"
	"encoding/json"
	"fmt"
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

	var seq int64
	logAction(ctx, a, stepRunID, seq, fmt.Sprintf("Executing action: %s", actionType))
	seq++

	var result map[string]any
	var err error

	switch actionType {
	case "slack_notify":
		result, err = actionNotifySlack(ctx, config, credentials)
	case "github_pr_review":
		result, err = actionGitHubPostReviewComment(ctx, config, credentials)
	case "github_label":
		result, err = actionGitHubAddLabel(ctx, config, credentials)
	case "github_comment":
		result, err = actionGitHubPostIssueComment(ctx, config, credentials)
	case "github_fetch_pr":
		result, err = actionGitHubFetchPR(ctx, config, credentials)
	case "github_pr_review_inline":
		result, err = actionGitHubPRReviewInline(ctx, config, credentials)
	case "github_update_comment":
		result, err = actionGitHubUpdateComment(ctx, config, credentials)
	case "create_pr":
		// PR creation is handled by CreatePullRequest activity directly
		return map[string]any{"status": "skipped_in_action"}, nil
	default:
		return nil, fmt.Errorf("unknown action type: %s", actionType)
	}

	if err != nil {
		logAction(ctx, a, stepRunID, seq, fmt.Sprintf("Action failed: %v", err))
		return nil, err
	}
	logAction(ctx, a, stepRunID, seq, "Action completed successfully")
	return result, nil
}

// logAction writes a single log line for an action step run.
func logAction(ctx context.Context, a *Activities, stepRunID string, seq int64, msg string) {
	if a.DB == nil || stepRunID == "" {
		return
	}
	_ = batchInsertLogs(ctx, a, stepRunID, []logLine{{Seq: seq, Stream: "stdout", Content: msg}})
}

func actionNotifySlack(ctx context.Context, config map[string]any, _ map[string]string) (map[string]any, error) {
	channel, _ := config["channel"].(string)
	message, _ := config["message"].(string)
	if channel == "" || message == "" {
		return nil, fmt.Errorf("slack_notify: missing required config (channel=%q, message=%q)", channel, message)
	}

	var threadTS *string
	if ts, _ := config["thread_ts"].(string); ts != "" {
		threadTS = &ts
	}

	slackActs := NewSlackActivities()
	_, err := slackActs.NotifySlack(ctx, channel, message, threadTS)
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

	token := credentials["GITHUB_TOKEN"]
	ghClient := newGitHubClientWithToken(ctx, token)
	if ghClient == nil {
		return nil, fmt.Errorf("github_pr_review: GITHUB_TOKEN is not set")
	}

	if summary == "" {
		return map[string]any{"status": "skipped", "reason": "empty summary"}, nil
	}

	owner, repo := extractOwnerRepo(repoURL)
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("github_pr_review: could not parse owner/repo from %s", repoURL)
	}
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

func actionGitHubAddLabel(ctx context.Context, config map[string]any, credentials map[string]string) (map[string]any, error) {
	repoURL, _ := config["repo_url"].(string)
	issueNumber := toInt(config["issue_number"])
	labels := toStringSlice(config["labels"])

	if repoURL == "" || issueNumber == 0 || len(labels) == 0 {
		return nil, fmt.Errorf("github_label: missing repo_url, issue_number, or labels")
	}

	token := credentials["GITHUB_TOKEN"]
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

	token := credentials["GITHUB_TOKEN"]
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

func actionGitHubFetchPR(ctx context.Context, config map[string]any, credentials map[string]string) (map[string]any, error) {
	repoURL, _ := config["repo_url"].(string)
	prNumber := toInt(config["pr_number"])

	if repoURL == "" {
		return nil, fmt.Errorf("github_fetch_pr: missing repo_url")
	}
	if prNumber == 0 {
		return nil, fmt.Errorf("github_fetch_pr: missing pr_number")
	}

	token := credentials["GITHUB_TOKEN"]
	ghClient := newGitHubClientWithToken(ctx, token)
	if ghClient == nil {
		return nil, fmt.Errorf("github_fetch_pr: GITHUB_TOKEN is not set")
	}

	owner, repo := extractOwnerRepo(repoURL)
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("github_fetch_pr: could not parse owner/repo from %s", repoURL)
	}

	// Fetch PR metadata.
	pr, _, err := ghClient.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("github_fetch_pr: get PR: %w", err)
	}

	// Fetch unified diff.
	diff, _, err := ghClient.PullRequests.GetRaw(ctx, owner, repo, prNumber, github.RawOptions{Type: github.Diff})
	if err != nil {
		return nil, fmt.Errorf("github_fetch_pr: get diff: %w", err)
	}

	// Collect changed file paths (paginated).
	changedFiles := make([]string, 0)
	opts := &github.ListOptions{PerPage: 100}
	for {
		files, resp, err := ghClient.PullRequests.ListFiles(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("github_fetch_pr: list files: %w", err)
		}
		for _, f := range files {
			if f.Filename != nil {
				changedFiles = append(changedFiles, *f.Filename)
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	title := ""
	if pr.Title != nil {
		title = *pr.Title
	}
	baseBranch := ""
	if pr.Base != nil && pr.Base.Ref != nil {
		baseBranch = *pr.Base.Ref
	}
	additions := 0
	if pr.Additions != nil {
		additions = *pr.Additions
	}
	deletions := 0
	if pr.Deletions != nil {
		deletions = *pr.Deletions
	}

	return map[string]any{
		"diff":          diff,
		"title":         title,
		"base_branch":   baseBranch,
		"changed_files": changedFiles,
		"additions":     additions,
		"deletions":     deletions,
	}, nil
}

// inlineAnnotation is a single inline review comment from the synthesis step.
type inlineAnnotation struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Side string `json:"side"` // "LEFT" (deleted lines) or "RIGHT" (added/context lines)
	Body string `json:"body"`
}

func actionGitHubPRReviewInline(ctx context.Context, config map[string]any, credentials map[string]string) (map[string]any, error) {
	repoURL, _ := config["repo_url"].(string)
	prNumber := toInt(config["pr_number"])
	annotationsJSON, _ := config["annotations"].(string)
	commitID, _ := config["commit_id"].(string)

	if repoURL == "" {
		return nil, fmt.Errorf("github_pr_review_inline: missing repo_url")
	}
	if prNumber == 0 {
		return nil, fmt.Errorf("github_pr_review_inline: missing pr_number")
	}

	token := credentials["GITHUB_TOKEN"]
	ghClient := newGitHubClientWithToken(ctx, token)
	if ghClient == nil {
		return nil, fmt.Errorf("github_pr_review_inline: GITHUB_TOKEN is not set")
	}

	// Parse annotations JSON.
	var annotations []inlineAnnotation
	if annotationsJSON != "" && annotationsJSON != "null" {
		if err := json.Unmarshal([]byte(annotationsJSON), &annotations); err != nil {
			return nil, fmt.Errorf("github_pr_review_inline: parse annotations: %w", err)
		}
	}
	if len(annotations) == 0 {
		return map[string]any{"posted": 0, "skipped": 0, "skipped_details": []any{}}, nil
	}

	// Resolve commit ID to the PR head SHA if not provided.
	owner, repo := extractOwnerRepo(repoURL)
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("github_pr_review_inline: could not parse owner/repo from %s", repoURL)
	}
	if commitID == "" {
		pr, _, err := ghClient.PullRequests.Get(ctx, owner, repo, prNumber)
		if err != nil {
			return nil, fmt.Errorf("github_pr_review_inline: get PR head SHA: %w", err)
		}
		if pr.Head != nil && pr.Head.SHA != nil {
			commitID = *pr.Head.SHA
		}
	}

	// Build review comments. GitHub requires side to be "LEFT" or "RIGHT".
	comments := make([]*github.DraftReviewComment, 0, len(annotations))
	for _, a := range annotations {
		side := strings.ToUpper(a.Side)
		if side != "LEFT" && side != "RIGHT" {
			side = "RIGHT" // default to RIGHT (new file) for unrecognised values
		}
		comments = append(comments, &github.DraftReviewComment{
			Path: github.String(a.File),
			Line: github.Int(a.Line),
			Side: github.String(side),
			Body: github.String(a.Body),
		})
	}

	// Post as a single review to avoid per-comment notification spam.
	req := &github.PullRequestReviewRequest{
		CommitID: github.String(commitID),
		Event:    github.String("COMMENT"),
		Comments: comments,
	}
	_, _, err := ghClient.PullRequests.CreateReview(ctx, owner, repo, prNumber, req)
	if err != nil {
		// GitHub rejects the entire review if any single comment is invalid
		// (e.g., line not in diff). Fall back to posting annotations one by one
		// so valid ones still land.
		return actionGitHubPRReviewInlineOneByOne(ctx, ghClient, owner, repo, prNumber, commitID, annotations)
	}

	return map[string]any{
		"posted":          len(annotations),
		"skipped":         0,
		"skipped_details": []any{},
	}, nil
}

// actionGitHubPRReviewInlineOneByOne posts each annotation individually, collecting
// skipped items rather than failing the whole step when individual comments are rejected.
func actionGitHubPRReviewInlineOneByOne(ctx context.Context, ghClient *github.Client, owner, repo string, prNumber int, commitID string, annotations []inlineAnnotation) (map[string]any, error) {
	posted := 0
	skippedDetails := make([]any, 0)

	for _, a := range annotations {
		side := strings.ToUpper(a.Side)
		if side != "LEFT" && side != "RIGHT" {
			side = "RIGHT"
		}
		req := &github.PullRequestReviewRequest{
			CommitID: github.String(commitID),
			Event:    github.String("COMMENT"),
			Comments: []*github.DraftReviewComment{{
				Path: github.String(a.File),
				Line: github.Int(a.Line),
				Side: github.String(side),
				Body: github.String(a.Body),
			}},
		}
		_, _, err := ghClient.PullRequests.CreateReview(ctx, owner, repo, prNumber, req)
		if err != nil {
			skippedDetails = append(skippedDetails, map[string]any{
				"file":   a.File,
				"line":   a.Line,
				"reason": err.Error(),
			})
		} else {
			posted++
		}
	}

	return map[string]any{
		"posted":          posted,
		"skipped":         len(skippedDetails),
		"skipped_details": skippedDetails,
	}, nil
}

func actionGitHubUpdateComment(ctx context.Context, config map[string]any, credentials map[string]string) (map[string]any, error) {
	repoURL, _ := config["repo_url"].(string)
	commentID := int64(toInt(config["comment_id"]))
	body, _ := config["body"].(string)

	if repoURL == "" {
		return nil, fmt.Errorf("github_update_comment: missing repo_url")
	}
	if commentID == 0 {
		return nil, fmt.Errorf("github_update_comment: missing comment_id")
	}
	if body == "" {
		return nil, fmt.Errorf("github_update_comment: missing body")
	}

	token := credentials["GITHUB_TOKEN"]
	ghClient := newGitHubClientWithToken(ctx, token)
	if ghClient == nil {
		return nil, fmt.Errorf("github_update_comment: GITHUB_TOKEN is not set")
	}

	owner, repo := extractOwnerRepo(repoURL)
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("github_update_comment: could not parse owner/repo from %s", repoURL)
	}
	updated, _, err := ghClient.Issues.EditComment(ctx, owner, repo, commentID, &github.IssueComment{
		Body: github.String(body),
	})
	if err != nil {
		return nil, fmt.Errorf("github_update_comment: %w", err)
	}

	updatedID := int64(0)
	if updated != nil && updated.ID != nil {
		updatedID = *updated.ID
	}
	return map[string]any{"status": "updated", "comment_id": updatedID}, nil
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
