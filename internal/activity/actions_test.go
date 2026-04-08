package activity

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockCredStore struct {
	data        map[string]string
	batchCalled bool
}

func (m *mockCredStore) Get(_ context.Context, _, name string) (string, error) {
	if v, ok := m.data[name]; ok {
		return v, nil
	}
	return "", fmt.Errorf("not found")
}

func (m *mockCredStore) GetBatch(_ context.Context, _ string, names []string) (map[string]string, error) {
	m.batchCalled = true
	out := map[string]string{}
	for _, n := range names {
		if v, ok := m.data[n]; ok {
			out[n] = v
		}
	}
	return out, nil
}

type errCredStore struct{}

func (e *errCredStore) Get(_ context.Context, _, _ string) (string, error) {
	return "", fmt.Errorf("store error")
}

func (e *errCredStore) GetBatch(_ context.Context, _ string, _ []string) (map[string]string, error) {
	return nil, fmt.Errorf("store unavailable")
}

func TestExecuteAction_UnknownType(t *testing.T) {
	a := &Activities{CredStore: &mockCredStore{}}
	_, err := a.ExecuteAction(context.Background(), "step-1", "bad_type", nil, "team-1", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action type")
}

func TestExecuteAction_SlackNotify_MissingChannelReturnsError(t *testing.T) {
	a := &Activities{CredStore: &mockCredStore{}}
	_, err := a.ExecuteAction(context.Background(), "", "slack_notify",
		map[string]any{"channel": "", "message": ""}, "team-1", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

func TestExecuteAction_CredentialsFetched(t *testing.T) {
	store := &mockCredStore{data: map[string]string{"MY_TOKEN": "secret"}}
	a := &Activities{CredStore: store}
	// create_pr is a no-op action that returns skipped — verifies creds are fetched before dispatch
	result, err := a.ExecuteAction(context.Background(), "", "create_pr",
		map[string]any{},
		"team-1", []string{"MY_TOKEN"})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, store.batchCalled, "expected GetBatch to be called")
}

func TestExecuteAction_CredentialFetchError(t *testing.T) {
	a := &Activities{CredStore: &errCredStore{}}
	_, err := a.ExecuteAction(context.Background(), "", "slack_notify",
		map[string]any{}, "team-1", []string{"SOME_CRED"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetch credentials")
}

// Fix 1: missing GITHUB_TOKEN in credential store must return error, not fall back to env.
func TestGitHubPRReview_MissingToken_ReturnsError_NoEnvFallback(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "operator-token-must-not-be-used")
	// Empty credentials map — no GITHUB_TOKEN in store.
	_, err := actionGitHubPostReviewComment(context.Background(),
		map[string]any{
			"repo_url":  "https://github.com/org/repo",
			"pr_number": 1,
			"summary":   "looks good",
		},
		map[string]string{}, // no creds in store
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GITHUB_TOKEN")
}

// --- github_fetch_pr tests ---

func TestGitHubFetchPR_MissingToken_ReturnsError(t *testing.T) {
	_, err := actionGitHubFetchPR(context.Background(),
		map[string]any{"repo_url": "https://github.com/org/repo", "pr_number": 1},
		map[string]string{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GITHUB_TOKEN")
}

// toInt must handle scientific notation strings produced when large float64
// values (e.g. GitHub comment IDs after JSON round-trip) are template-rendered.
func TestToInt_ScientificNotationString(t *testing.T) {
	// 4165804618 → JSON float64 → template renders as "4.165804618e+09"
	assert.Equal(t, 4165804618, toInt("4.165804618e+09"))
	assert.Equal(t, 42, toInt("42"))
	assert.Equal(t, 0, toInt("not-a-number"))
	assert.Equal(t, 100, toInt(float64(100)))
	assert.Equal(t, 99, toInt(int64(99)))
}

func TestGitHubFetchPR_MissingRepoURL_ReturnsError(t *testing.T) {
	_, err := actionGitHubFetchPR(context.Background(),
		map[string]any{"pr_number": 1},
		map[string]string{"GITHUB_TOKEN": "tok"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repo_url")
}

func TestGitHubFetchPR_MissingPRNumber_ReturnsError(t *testing.T) {
	_, err := actionGitHubFetchPR(context.Background(),
		map[string]any{"repo_url": "https://github.com/org/repo"},
		map[string]string{"GITHUB_TOKEN": "tok"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pr_number")
}

// --- github_pr_review_inline tests ---

func TestGitHubPRReviewInline_MissingToken_ReturnsError(t *testing.T) {
	_, err := actionGitHubPRReviewInline(context.Background(),
		map[string]any{
			"repo_url":    "https://github.com/org/repo",
			"pr_number":   1,
			"annotations": `[]`,
		},
		map[string]string{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GITHUB_TOKEN")
}

func TestGitHubPRReviewInline_EmptyAnnotations_IsNoop(t *testing.T) {
	// Empty annotations with valid token should short-circuit to posted=0.
	result, err := actionGitHubPRReviewInline(context.Background(),
		map[string]any{
			"repo_url":    "https://github.com/org/repo",
			"pr_number":   1,
			"annotations": `[]`,
		},
		map[string]string{"GITHUB_TOKEN": "tok"},
	)
	require.NoError(t, err)
	assert.Equal(t, 0, result["posted"])
	assert.Equal(t, 0, result["skipped"])
}

func TestGitHubPRReviewInline_MissingRepoURL_ReturnsError(t *testing.T) {
	_, err := actionGitHubPRReviewInline(context.Background(),
		map[string]any{"pr_number": 1, "annotations": `[]`},
		map[string]string{"GITHUB_TOKEN": "tok"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repo_url")
}

func TestGitHubPRReviewInline_InvalidAnnotationsJSON_ReturnsError(t *testing.T) {
	_, err := actionGitHubPRReviewInline(context.Background(),
		map[string]any{
			"repo_url":    "https://github.com/org/repo",
			"pr_number":   1,
			"annotations": `not-json`,
		},
		map[string]string{"GITHUB_TOKEN": "tok"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse annotations")
}

// --- github_update_comment tests ---

func TestGitHubUpdateComment_MissingToken_ReturnsError(t *testing.T) {
	_, err := actionGitHubUpdateComment(context.Background(),
		map[string]any{
			"repo_url":   "https://github.com/org/repo",
			"comment_id": 42,
			"body":       "updated text",
		},
		map[string]string{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GITHUB_TOKEN")
}

func TestGitHubUpdateComment_MissingRepoURL_ReturnsError(t *testing.T) {
	_, err := actionGitHubUpdateComment(context.Background(),
		map[string]any{"comment_id": 42, "body": "text"},
		map[string]string{"GITHUB_TOKEN": "tok"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repo_url")
}

func TestGitHubUpdateComment_MissingBody_ReturnsError(t *testing.T) {
	_, err := actionGitHubUpdateComment(context.Background(),
		map[string]any{"repo_url": "https://github.com/org/repo", "comment_id": 42},
		map[string]string{"GITHUB_TOKEN": "tok"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "body")
}

func TestGitHubUpdateComment_MissingCommentID_ReturnsError(t *testing.T) {
	_, err := actionGitHubUpdateComment(context.Background(),
		map[string]any{"repo_url": "https://github.com/org/repo", "body": "text"},
		map[string]string{"GITHUB_TOKEN": "tok"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "comment_id")
}

// --- URL scheme validation tests ---

func TestGitHubFetchPR_NonHTTPS_ReturnsError(t *testing.T) {
	_, err := actionGitHubFetchPR(context.Background(),
		map[string]any{"repo_url": "git://github.com/org/repo", "pr_number": 1},
		map[string]string{"GITHUB_TOKEN": "tok"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https://")
}

func TestGitHubPRReviewInline_NonHTTPS_ReturnsError(t *testing.T) {
	_, err := actionGitHubPRReviewInline(context.Background(),
		map[string]any{
			"repo_url":    "ssh://git@github.com/org/repo",
			"pr_number":   1,
			"annotations": `[{"file":"f.go","line":1,"side":"RIGHT","body":"bug"}]`,
		},
		map[string]string{"GITHUB_TOKEN": "tok"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https://")
}

func TestGitHubUpdateComment_NonHTTPS_ReturnsError(t *testing.T) {
	_, err := actionGitHubUpdateComment(context.Background(),
		map[string]any{
			"repo_url":   "file:///etc/passwd",
			"comment_id": 42,
			"body":       "text",
		},
		map[string]string{"GITHUB_TOKEN": "tok"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https://")
}

// --- toInt64 tests ---

func TestToInt64_Precision(t *testing.T) {
	// Verify int64 precision is preserved for large values.
	assert.Equal(t, int64(4165804618), toInt64("4165804618"))
	assert.Equal(t, int64(4165804618), toInt64(int64(4165804618)))
	assert.Equal(t, int64(42), toInt64(42))
	assert.Equal(t, int64(100), toInt64(float64(100)))
	assert.Equal(t, int64(0), toInt64("not-a-number"))
	// Scientific notation fallback.
	assert.Equal(t, int64(4165804618), toInt64("4.165804618e+09"))
}

// Fix 5: empty summary with missing token must return error, not a success skip.
func TestGitHubPRReview_EmptySummaryWithMissingToken_ReturnsError(t *testing.T) {
	_, err := actionGitHubPostReviewComment(context.Background(),
		map[string]any{
			"repo_url":  "https://github.com/org/repo",
			"pr_number": 1,
			"summary":   "", // empty summary
		},
		map[string]string{}, // no GITHUB_TOKEN
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GITHUB_TOKEN")
}
