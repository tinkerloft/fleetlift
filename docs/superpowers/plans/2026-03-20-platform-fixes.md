# Platform Fixes Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix three platform bugs that block the auto-debt-slayer workflow: `evalCondition` not exposing step output, `pull_request` config fields not being template-rendered, and `git commit` failing on clean working trees.

**Architecture:** Three independent, targeted changes to `dag.go` and `pr.go`. No new files. The GitHub client in `pr.go` is made injectable (field on `Activities`) so the clean-tree behaviour can be tested without hitting the GitHub API. `activity.RecordHeartbeat` is moved to after the clean-tree early return so tests that exercise the no-op path can use a plain `context.Background()`. The dirty-tree test uses `testsuite.TestActivityEnvironment` to supply a real Temporal activity context.

**Tech Stack:** Go, Temporal SDK, go-github v62, testify, httptest

**Spec:** `docs/superpowers/specs/2026-03-20-platform-fixes-design.md`

---

## Chunk 1: Fix 3 — Clean working tree + GitHub client injection

### Task 1: Make the GitHub client injectable in `Activities`

**Files:**
- Modify: `internal/activity/activities.go`
- Modify: `internal/activity/pr.go`

- [ ] **Step 1.1: Add `GitHubClient` field to `Activities`**

Open `internal/activity/activities.go`. Add one field and its import:

```go
import (
    "context"

    "github.com/google/go-github/v62/github"
    "github.com/jmoiron/sqlx"
    "github.com/tinkerloft/fleetlift/internal/agent"
    "github.com/tinkerloft/fleetlift/internal/sandbox"
)

type Activities struct {
    Sandbox      sandbox.Client
    DB           *sqlx.DB
    CredStore    CredentialStore
    AgentRunners map[string]agent.Runner
    ProfileStore ProfileStore
    GitHubClient *github.Client // if nil, constructed from GITHUB_TOKEN env var at call time
}
```

- [ ] **Step 1.2: Use injectable client and add DB nil guard in `CreatePullRequest`**

In `internal/activity/pr.go`, make two changes:

**a) Replace the inline client construction block** (the `token := os.Getenv...` block):

```go
// OLD:
token := os.Getenv("GITHUB_TOKEN")
if token == "" {
    return "", fmt.Errorf("GITHUB_TOKEN not set")
}
ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
tc := oauth2.NewClient(ctx, ts)
ghClient := github.NewClient(tc)
```

```go
// NEW:
ghClient := a.GitHubClient
if ghClient == nil {
    token := os.Getenv("GITHUB_TOKEN")
    if token == "" {
        return "", fmt.Errorf("GITHUB_TOKEN not set")
    }
    ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
    tc := oauth2.NewClient(ctx, ts)
    ghClient = github.NewClient(tc)
}
```

**b) Add a nil guard before `a.DB.ExecContext`** (near the end of the function):

```go
// OLD:
if _, err := a.DB.ExecContext(ctx,
    `UPDATE step_runs SET pr_url = $1, branch_name = $2 WHERE id = $3`,
    pr.GetHTMLURL(), branchName, input.StepRunID); err != nil {
    activity.GetLogger(ctx).Warn("failed to record PR URL in step_run", ...)
}
```

```go
// NEW:
if a.DB != nil {
    if _, err := a.DB.ExecContext(ctx,
        `UPDATE step_runs SET pr_url = $1, branch_name = $2 WHERE id = $3`,
        pr.GetHTMLURL(), branchName, input.StepRunID); err != nil {
        activity.GetLogger(ctx).Warn("failed to record PR URL in step_run", "pr_url", pr.GetHTMLURL(), "error", err)
    }
}
```

- [ ] **Step 1.3: Build to verify**

```bash
cd /Users/andrew/dev/projects/fleetlift && go build ./...
```

Expected: no errors.

- [ ] **Step 1.4: Run existing tests**

```bash
go test ./internal/activity/... -count=1
```

Expected: all pass (no behaviour change yet).

- [ ] **Step 1.5: Commit**

```bash
git add internal/activity/activities.go internal/activity/pr.go
git commit -m "refactor: make GitHub client injectable in Activities for testability"
```

---

### Task 2: Break the git command loop and add clean-tree check

**Files:**
- Modify: `internal/activity/pr.go`

The current implementation runs four git commands in a single loop and records a heartbeat at the very top. To (a) insert a status check between `git add -A` and `git commit`, and (b) avoid panicking in tests that take the clean-tree early-return path, replace the loop with sequential exec calls and move `RecordHeartbeat` to after the clean-tree check.

- [ ] **Step 2.1: Add `"strings"` import to `pr.go`**

```go
import (
    "context"
    "fmt"
    "os"
    "strings"   // add this

    "github.com/google/go-github/v62/github"
    "go.temporal.io/sdk/activity"
    "golang.org/x/oauth2"

    "github.com/tinkerloft/fleetlift/internal/shellquote"
    "github.com/tinkerloft/fleetlift/internal/workflow"
)
```

- [ ] **Step 2.2: Replace the heartbeat call and command loop**

The full rewritten body of `CreatePullRequest` from the `prDef` nil check through to the GitHub PR creation (replacing lines 19–49 in the original):

```go
func (a *Activities) CreatePullRequest(ctx context.Context, sandboxID string, input workflow.StepInput) (string, error) {
    prDef := input.StepDef.PullRequest
    if prDef == nil {
        return "", fmt.Errorf("no PR configuration for step %s", input.StepDef.ID)
    }

    if len(input.ResolvedOpts.Repos) == 0 {
        return "", fmt.Errorf("no repos configured for PR creation")
    }

    branchName := fmt.Sprintf("%s/%s", prDef.BranchPrefix, input.RunID)
    repoDir := "/workspace/" + repoName(input.ResolvedOpts.Repos[0])

    execGit := func(cmd string) error {
        stdout, stderr, err := a.Sandbox.Exec(ctx, sandboxID, cmd, "/")
        if err != nil {
            return fmt.Errorf("git command %q: %w (stdout: %s, stderr: %s)", cmd, err, stdout, stderr)
        }
        return nil
    }

    if err := execGit(fmt.Sprintf("git -C %s checkout -b %s", shellquote.Quote(repoDir), shellquote.Quote(branchName))); err != nil {
        return "", err
    }
    if err := execGit(fmt.Sprintf("git -C %s add -A", shellquote.Quote(repoDir))); err != nil {
        return "", err
    }

    // Skip commit/push/PR if there are no changes.
    statusOut, _, statusErr := a.Sandbox.Exec(ctx, sandboxID, fmt.Sprintf("git -C %s status --porcelain", shellquote.Quote(repoDir)), "/")
    if statusErr != nil {
        return "", fmt.Errorf("git status: %w", statusErr)
    }
    if strings.TrimSpace(statusOut) == "" {
        return "", nil // clean working tree — nothing to commit
    }

    // Record heartbeat now that we know there is real work to do.
    activity.RecordHeartbeat(ctx, "creating PR")

    if err := execGit(fmt.Sprintf("git -C %s commit -m %s", shellquote.Quote(repoDir), shellquote.Quote(prDef.Title))); err != nil {
        return "", err
    }
    if err := execGit(fmt.Sprintf("git -C %s push origin %s", shellquote.Quote(repoDir), shellquote.Quote(branchName))); err != nil {
        return "", err
    }

    // ... rest of function unchanged (GitHub client construction, PR creation, label add, DB update)
```

- [ ] **Step 2.3: Build to verify**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 2.4: Commit**

```bash
git add internal/activity/pr.go
git commit -m "fix: skip PR creation when working tree is clean after agent run"
```

---

### Task 3: Test the clean-tree behaviour

**Files:**
- Create: `internal/activity/pr_test.go`

- [ ] **Step 3.1: Write the failing tests**

Create `internal/activity/pr_test.go`:

```go
package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
```

Note: `scriptingSandbox.Exec` references `strings.Contains` — add `"strings"` to the import block of `pr_test.go`.

- [ ] **Step 3.2: Run tests to verify they fail**

```bash
go test ./internal/activity/ -run "TestCreatePullRequest" -v -count=1
```

Expected: FAIL or compile error — the tests reference the new `scriptingSandbox` type and the clean-tree logic that isn't in place yet.

- [ ] **Step 3.3: Run tests after Tasks 1 and 2 are in place**

```bash
go test ./internal/activity/ -run "TestCreatePullRequest" -v -count=1
```

Expected: both tests PASS.

- [ ] **Step 3.4: Run full activity test suite**

```bash
go test ./internal/activity/... -count=1
```

Expected: all pass.

- [ ] **Step 3.5: Commit**

```bash
git add internal/activity/pr_test.go
git commit -m "test: add CreatePullRequest tests for clean and dirty working tree"
```

---

## Chunk 2: Fix 1 — `evalCondition` must expose step output

### Task 4: Expose `output` in `evalCondition` context

**Files:**
- Modify: `internal/workflow/dag.go`
- Modify: `internal/workflow/dag_test.go`

- [ ] **Step 4.1: Write the failing tests**

In `internal/workflow/dag_test.go`, add after `TestEvalCondition_InvalidTemplate`:

```go
func TestEvalCondition_StepOutput(t *testing.T) {
	outputs := map[string]*model.StepOutput{
		"assess": {
			Status: model.StepStatusComplete,
			Output: map[string]any{"decision": "execute"},
		},
	}
	// Truthy: output field matches expected value
	assert.True(t, runEvalCondition(t,
		`{{eq (index .steps "assess").output.decision "execute"}}`,
		outputs,
	))
	// Falsy: output field has different value
	assert.False(t, runEvalCondition(t,
		`{{eq (index .steps "assess").output.decision "manual_needed"}}`,
		outputs,
	))
}

func TestEvalCondition_MissingStepOutput(t *testing.T) {
	// Step with no Output set — should not panic, condition evaluates to false
	outputs := map[string]*model.StepOutput{
		"assess": {Status: model.StepStatusComplete},
	}
	assert.False(t, runEvalCondition(t,
		`{{eq (index .steps "assess").output.decision "execute"}}`,
		outputs,
	))
}
```

- [ ] **Step 4.2: Run to verify failure**

```bash
go test ./internal/workflow/ -run "TestEvalCondition_StepOutput" -v -count=1
```

Expected: FAIL — the condition evaluates to false even when `decision` is "execute" (because `output` is absent from the context).

- [ ] **Step 4.3: Add `output` to the per-step context map**

In `internal/workflow/dag.go`, inside `evalCondition`, find the map literal inside the `for id, out := range outputs` loop and add `"output": out.Output`:

```go
// BEFORE:
steps[id] = map[string]any{
    "status": string(out.Status),
    "error":  out.Error,
}

// AFTER:
steps[id] = map[string]any{
    "status": string(out.Status),
    "error":  out.Error,
    "output": out.Output,
}
```

- [ ] **Step 4.4: Run tests to verify they pass**

```bash
go test ./internal/workflow/ -run "TestEvalCondition" -v -count=1
```

Expected: all `TestEvalCondition_*` tests pass, including the two new ones.

- [ ] **Step 4.5: Run full workflow test suite**

```bash
go test ./internal/workflow/... -count=1
```

Expected: all pass.

- [ ] **Step 4.6: Commit**

```bash
git add internal/workflow/dag.go internal/workflow/dag_test.go
git commit -m "fix: expose step output in evalCondition template context"
```

---

## Chunk 3: Fix 2 — `pull_request` config must be template-rendered

### Task 5: Render `pull_request` fields in `resolveStep`

**Files:**
- Modify: `internal/workflow/dag.go`
- Modify: `internal/workflow/dag_test.go`

- [ ] **Step 5.1: Write the failing tests**

In `internal/workflow/dag_test.go`, add:

```go
func TestResolveStep_PRConfig_TemplateRendered(t *testing.T) {
	step := model.StepDef{
		ID: "execute",
		Execution: &model.ExecutionDef{
			Agent:  "claude-code",
			Prompt: "Fix the issue",
		},
		PullRequest: &model.PRDef{
			BranchPrefix: "agent/{{ .Params.ticket_key }}",
			Title:        "fix: {{ .Params.ticket_key }} automated fix",
			Body:         "Fixes {{ .Params.ticket_key }}",
		},
	}
	params := map[string]any{"ticket_key": "AFX-1234"}

	opts, err := resolveStep(step, params, map[string]*model.StepOutput{})
	require.NoError(t, err)
	require.NotNil(t, opts.PRConfig)
	assert.Equal(t, "agent/AFX-1234", opts.PRConfig.BranchPrefix)
	assert.Equal(t, "fix: AFX-1234 automated fix", opts.PRConfig.Title)
	assert.Equal(t, "Fixes AFX-1234", opts.PRConfig.Body)
}

func TestResolveStep_PRConfig_FromPriorStepOutput(t *testing.T) {
	step := model.StepDef{
		ID: "execute",
		Execution: &model.ExecutionDef{
			Agent:  "claude-code",
			Prompt: "Fix the issue",
		},
		PullRequest: &model.PRDef{
			BranchPrefix: "agent/fix",
			Title:        `{{ (index .Steps "assess").Output.pr_title_hint }}`,
			Body:         `{{ (index .Steps "assess").Output.pr_body_draft }}`,
		},
	}
	outputs := map[string]*model.StepOutput{
		"assess": {
			Status: model.StepStatusComplete,
			Output: map[string]any{
				"pr_title_hint": "fix(AFX-1234): null pointer in auth handler",
				"pr_body_draft": "Automated fix for AFX-1234",
			},
		},
	}

	opts, err := resolveStep(step, map[string]any{}, outputs)
	require.NoError(t, err)
	require.NotNil(t, opts.PRConfig)
	assert.Equal(t, "fix(AFX-1234): null pointer in auth handler", opts.PRConfig.Title)
	assert.Equal(t, "Automated fix for AFX-1234", opts.PRConfig.Body)
}

func TestResolveStep_PRConfig_NoExecution(t *testing.T) {
	// A step with pull_request but no execution block — PRConfig must still be rendered.
	step := model.StepDef{
		ID: "action-with-pr",
		PullRequest: &model.PRDef{
			BranchPrefix: "fix/{{ .Params.ticket_key }}",
			Title:        "fix: {{ .Params.ticket_key }}",
		},
	}
	params := map[string]any{"ticket_key": "AFX-9999"}

	opts, err := resolveStep(step, params, map[string]*model.StepOutput{})
	require.NoError(t, err)
	require.NotNil(t, opts.PRConfig)
	assert.Equal(t, "fix/AFX-9999", opts.PRConfig.BranchPrefix)
	assert.Equal(t, "fix: AFX-9999", opts.PRConfig.Title)
}

func TestResolveStep_PRConfig_OriginalNotMutated(t *testing.T) {
	original := &model.PRDef{
		BranchPrefix: "agent/{{ .Params.ticket_key }}",
		Title:        "fix: {{ .Params.ticket_key }}",
	}
	step := model.StepDef{
		ID: "execute",
		Execution: &model.ExecutionDef{
			Agent:  "claude-code",
			Prompt: "Fix it",
		},
		PullRequest: original,
	}
	params := map[string]any{"ticket_key": "AFX-1234"}

	_, err := resolveStep(step, params, map[string]*model.StepOutput{})
	require.NoError(t, err)
	// Original PRDef must be unchanged
	assert.Equal(t, "agent/{{ .Params.ticket_key }}", original.BranchPrefix)
	assert.Equal(t, "fix: {{ .Params.ticket_key }}", original.Title)
}
```

- [ ] **Step 5.2: Run to verify failure**

```bash
go test ./internal/workflow/ -run "TestResolveStep_PRConfig" -v -count=1
```

Expected: `TestResolveStep_PRConfig_TemplateRendered`, `TestResolveStep_PRConfig_FromPriorStepOutput`, and `TestResolveStep_PRConfig_NoExecution` fail — template expressions appear literally in output instead of being rendered.

- [ ] **Step 5.3: Add PR config rendering to `resolveStep`**

In `internal/workflow/dag.go`, restructure `resolveStep` to move the `pull_request` block outside the `step.Execution == nil` early-return guard. Replace the entire function with:

```go
func resolveStep(step model.StepDef, params map[string]any, outputs map[string]*model.StepOutput) (ResolvedStepOpts, error) {
	var opts ResolvedStepOpts

	if step.Execution != nil {
		renderCtx := fltemplate.RenderContext{Params: params, Steps: outputs}

		prompt, err := fltemplate.RenderPrompt(step.Execution.Prompt, renderCtx)
		if err != nil {
			return opts, fmt.Errorf("render prompt for step %s: %w", step.ID, err)
		}

		opts.Prompt = prompt
		opts.Agent = step.Execution.Agent
		if opts.Agent == "" {
			opts.Agent = "claude-code"
		}
		opts.Verifiers = step.Execution.Verifiers
		opts.Credentials = step.Execution.Credentials
		opts.MaxTurns = step.Execution.MaxTurns

		if step.Repositories != nil {
			repos, err := resolveRepos(step.Repositories, params, outputs)
			if err != nil {
				return opts, fmt.Errorf("resolve repos for step %s: %w", step.ID, err)
			}
			opts.Repos = repos
		}
	}

	// Render pull_request config fields — applies regardless of whether Execution is set.
	if step.PullRequest != nil {
		renderCtx := fltemplate.RenderContext{Params: params, Steps: outputs}
		pr := *step.PullRequest // shallow copy — do not mutate the original StepDef
		var err error
		if pr.BranchPrefix, err = fltemplate.RenderPrompt(pr.BranchPrefix, renderCtx); err != nil {
			return opts, fmt.Errorf("render pull_request.branch_prefix for step %s: %w", step.ID, err)
		}
		if pr.Title, err = fltemplate.RenderPrompt(pr.Title, renderCtx); err != nil {
			return opts, fmt.Errorf("render pull_request.title for step %s: %w", step.ID, err)
		}
		if pr.Body, err = fltemplate.RenderPrompt(pr.Body, renderCtx); err != nil {
			return opts, fmt.Errorf("render pull_request.body for step %s: %w", step.ID, err)
		}
		opts.PRConfig = &pr
	}

	return opts, nil
}
```

> **Note on eval_plugins:** The eval_plugins rendering loop lives in the DAGWorkflow caller (not inside `resolveStep`), so it is unaffected by this restructure.

- [ ] **Step 5.4: Run tests to verify they pass**

```bash
go test ./internal/workflow/ -run "TestResolveStep_PRConfig" -v -count=1
```

Expected: all four new tests pass.

- [ ] **Step 5.5: Run full workflow test suite**

```bash
go test ./internal/workflow/... -count=1
```

Expected: all pass — including the existing `TestResolveStep_NilExecution` which must still return no error and empty `opts.Prompt`.

- [ ] **Step 5.6: Commit**

```bash
git add internal/workflow/dag.go internal/workflow/dag_test.go
git commit -m "fix: render pull_request config fields as Go templates in resolveStep"
```

---

## Chunk 4: Final verification

### Task 6: Full test run and lint

- [ ] **Step 6.1: Run all tests**

```bash
go test ./... -count=1
```

Expected: all pass.

- [ ] **Step 6.2: Run linter**

```bash
make lint
```

Expected: no errors.

- [ ] **Step 6.3: Build**

```bash
go build ./...
```

Expected: no errors.
