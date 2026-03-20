package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-github/v62/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

// scriptingSandbox records Exec calls and returns pre-configured responses.
// It embeds noopSandbox (defined in execute_test.go) to satisfy sandbox.Client.
type scriptingSandbox struct {
	noopSandbox
	responses []sandboxResponse
}

type sandboxResponse struct {
	match  string // substring to match against cmd; empty matches anything
	stdout string
	stderr string
	err    error
}

func (s *scriptingSandbox) Exec(_ context.Context, _, cmd, _ string) (string, string, error) {
	for _, r := range s.responses {
		if r.match == "" || strings.Contains(cmd, r.match) {
			return r.stdout, r.stderr, r.err
		}
	}
	return "", "", nil
}

func makePRTestInput(repoURL string) workflow.StepInput {
	return workflow.StepInput{
		RunID:     "run-abc",
		StepRunID: "steprun-1",
		StepDef: model.StepDef{
			ID:   "execute",
			Mode: "transform",
			PullRequest: &model.PRDef{
				BranchPrefix: "agent",
				Title:        "fix: test PR",
				Body:         "automated",
			},
		},
		ResolvedOpts: workflow.ResolvedStepOpts{
			Repos: []model.RepoRef{{URL: repoURL}},
		},
	}
}

// newTestGitHubClient returns a *github.Client pointed at a test HTTP server.
func newTestGitHubClient(t *testing.T, handler http.Handler) *github.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := github.NewClient(nil).WithAuthToken("test-token")
	var err error
	client.BaseURL, err = client.BaseURL.Parse(srv.URL + "/")
	require.NoError(t, err)
	client.UploadURL, err = client.UploadURL.Parse(srv.URL + "/")
	require.NoError(t, err)
	return client
}

// TestCreatePullRequest_CleanTree verifies that when git status --porcelain
// returns empty output, CreatePullRequest returns ("", nil) and never calls GitHub.
// Uses context.Background() directly because RecordHeartbeat is only reached
// after the clean-tree check (so it is never called on this path).
func TestCreatePullRequest_CleanTree(t *testing.T) {
	githubCalled := false
	ghClient := newTestGitHubClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		githubCalled = true
		w.WriteHeader(http.StatusInternalServerError)
	}))

	sb := &scriptingSandbox{
		responses: []sandboxResponse{
			{match: "checkout -b"},
			{match: "add -A"},
			{match: "status --porcelain", stdout: ""}, // empty = clean
		},
	}

	a := &Activities{Sandbox: sb, GitHubClient: ghClient}
	prURL, err := a.CreatePullRequest(context.Background(), "sb-1", makePRTestInput("https://github.com/acme/repo"))

	require.NoError(t, err)
	assert.Empty(t, prURL)
	assert.False(t, githubCalled, "GitHub API must not be called for a clean working tree")
}

// TestCreatePullRequest_DirtyTree verifies that when git status --porcelain
// returns output, the full commit/push/PR flow runs and the PR URL is returned.
// Uses TestActivityEnvironment to provide a valid Temporal activity context
// (required by activity.RecordHeartbeat).
func TestCreatePullRequest_DirtyTree(t *testing.T) {
	prCreated := false
	ghClient := newTestGitHubClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/repos/acme/repo/pulls" {
			prCreated = true
			pr := github.PullRequest{
				Number:  github.Int(42),
				HTMLURL: github.String("https://github.com/acme/repo/pull/42"),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(pr)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "[]")
	}))

	sb := &scriptingSandbox{
		responses: []sandboxResponse{
			{match: "checkout -b"},
			{match: "add -A"},
			{match: "status --porcelain", stdout: "M  some/file.go\n"}, // dirty
			{match: "commit"},
			{match: "push"},
		},
	}

	a := &Activities{Sandbox: sb, GitHubClient: ghClient}

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivity(a.CreatePullRequest)

	val, err := env.ExecuteActivity(a.CreatePullRequest, "sb-1", makePRTestInput("https://github.com/acme/repo"))
	require.NoError(t, err)

	var prURL string
	require.NoError(t, val.Get(&prURL))
	assert.Equal(t, "https://github.com/acme/repo/pull/42", prURL)
	assert.True(t, prCreated)
}
