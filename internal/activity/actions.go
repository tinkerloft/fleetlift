package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"
)

// maxDiffSize is the maximum diff size (in bytes) returned by github_fetch_pr.
// Larger diffs are truncated with a warning to avoid excessive memory when the
// diff is fanned out to multiple parallel reviewer agents.
const maxDiffSize = 1024 * 1024 // 1 MB

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

	owner, repo, err := parseGitHubRepo(repoURL, "github_pr_review")
	if err != nil {
		return nil, err
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

	owner, repo, err := parseGitHubRepo(repoURL, "github_label")
	if err != nil {
		return nil, err
	}
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

	owner, repo, err := parseGitHubRepo(repoURL, "github_comment")
	if err != nil {
		return nil, err
	}
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

	owner, repo, err := parseGitHubRepo(repoURL, "github_fetch_pr")
	if err != nil {
		return nil, err
	}

	// Fetch PR metadata.
	pr, _, err := ghClient.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("github_fetch_pr: get PR: %w", err)
	}

	// Fetch unified diff. Truncate to maxDiffSize to bound memory when the
	// diff is fanned out to multiple parallel reviewer agents.
	diff, _, err := ghClient.PullRequests.GetRaw(ctx, owner, repo, prNumber, github.RawOptions{Type: github.Diff})
	if err != nil {
		return nil, fmt.Errorf("github_fetch_pr: get diff: %w", err)
	}
	diffTruncated := false
	if len(diff) > maxDiffSize {
		cutAt := maxDiffSize
		if idx := strings.LastIndex(diff[:maxDiffSize], "\n"); idx > 0 {
			cutAt = idx
		}
		diff = diff[:cutAt] + "\n\n[... diff truncated at 1 MB ...]"
		diffTruncated = true
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
		"diff":           diff,
		"diff_truncated": diffTruncated,
		"title":          title,
		"base_branch":    baseBranch,
		"changed_files":  changedFiles,
		"additions":      additions,
		"deletions":      deletions,
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
	annotations := make([]inlineAnnotation, 0)
	if annotationsJSON != "" && annotationsJSON != "null" {
		if err := json.Unmarshal([]byte(annotationsJSON), &annotations); err != nil {
			return nil, fmt.Errorf("github_pr_review_inline: parse annotations: %w", err)
		}
	}
	if len(annotations) == 0 {
		return map[string]any{"posted": 0, "skipped": 0, "skipped_details": []any{}}, nil
	}

	// Resolve commit ID to the PR head SHA if not provided.
	owner, repo, err := parseGitHubRepo(repoURL, "github_pr_review_inline")
	if err != nil {
		return nil, err
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
	if commitID == "" {
		return nil, fmt.Errorf("github_pr_review_inline: could not resolve PR head SHA (branch may have been deleted)")
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
	_, _, err = ghClient.PullRequests.CreateReview(ctx, owner, repo, prNumber, req)
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

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for i, a := range annotations {
		// Brief pause between API calls to avoid triggering GitHub's secondary rate limit.
		if i > 0 {
			select {
			case <-ctx.Done():
				result := map[string]any{
					"posted":          posted,
					"skipped":         len(skippedDetails),
					"skipped_details": skippedDetails,
				}
				return result, ctx.Err()
			case <-ticker.C:
			}
		}
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

	result := map[string]any{
		"posted":          posted,
		"skipped":         len(skippedDetails),
		"skipped_details": skippedDetails,
	}
	if posted == 0 && len(annotations) > 0 {
		return result, fmt.Errorf("github_pr_review_inline: all %d annotations were rejected", len(annotations))
	}
	return result, nil
}

func actionGitHubUpdateComment(ctx context.Context, config map[string]any, credentials map[string]string) (map[string]any, error) {
	repoURL, _ := config["repo_url"].(string)
	commentID := toInt64(config["comment_id"])
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

	owner, repo, err := parseGitHubRepo(repoURL, "github_update_comment")
	if err != nil {
		return nil, err
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
	return int(toInt64(v))
}

// toInt64 converts a value to int64 with full precision (no float64 intermediate).
func toInt64(v any) int64 {
	switch val := v.(type) {
	case int:
		return int64(val)
	case float64:
		return int64(val)
	case int64:
		return val
	case string:
		if n, err := strconv.ParseInt(val, 10, 64); err == nil {
			return n
		}
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return int64(f)
		}
		return 0
	}
	return 0
}

func toStringSlice(v any) []string {
	switch val := v.(type) {
	case []string:
		return val
	case []any:
		out := make([]string, 0, len(val))
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
	return make([]string, 0)
}
