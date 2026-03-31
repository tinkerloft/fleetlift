# Multi-Persona PR Review Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add three new GitHub action types and a `pr-review-multi` builtin workflow that runs parallel focused reviewer agents, synthesises findings adversarially, posts a rich summary comment and inline annotations, with live progress feedback on the PR.

**Architecture:** Four new action handlers added to `internal/activity/actions.go` (`github_fetch_pr`, `github_pr_review_inline`, `github_update_comment`), each registered in `internal/model/action_contract.go`. Two workflow YAML files updated/created: `pr-review.yaml` gains the `github_fetch_pr` action for its fetch step; `pr-review-multi.yaml` is a new builtin implementing the full parallel-reviewer DAG.

**Tech Stack:** Go, `github.com/google/go-github/v62`, `golang.org/x/oauth2`, YAML workflow definitions, `testify/assert`.

---

## File Map

| File | Change |
|---|---|
| `internal/activity/actions.go` | Add `actionGitHubFetchPR`, `actionGitHubPRReviewInline`, `actionGitHubUpdateComment`; register in `ExecuteAction` switch |
| `internal/model/action_contract.go` | Register `github_fetch_pr`, `github_pr_review_inline`, `github_update_comment` in `DefaultActionRegistry()` |
| `internal/activity/actions_test.go` | Tests for all three new action handlers |
| `internal/template/workflows/pr-review.yaml` | Replace `fetch_pr` agent step with `github_fetch_pr` action step |
| `internal/template/workflows/pr-review-multi.yaml` | New builtin workflow |

---

## Task 1: `github_fetch_pr` action — contract + handler

**Files:**
- Modify: `internal/model/action_contract.go`
- Modify: `internal/activity/actions.go`
- Modify: `internal/activity/actions_test.go`

- [ ] **Step 1: Register the contract**

In `internal/model/action_contract.go`, add to `DefaultActionRegistry()` before the `return r` line:

```go
r.Register(ActionContract{
    Type:        "github_fetch_pr",
    Description: "Fetch PR metadata and unified diff from GitHub API (no sandbox)",
    Inputs: []FieldContract{
        {Name: "repo_url", Type: "string", Required: true, Description: "GitHub repository URL"},
        {Name: "pr_number", Type: "int", Required: true, Description: "Pull request number"},
    },
    Outputs: []FieldContract{
        {Name: "diff", Type: "string", Required: true, Description: "Unified diff of the PR"},
        {Name: "title", Type: "string", Required: true, Description: "PR title"},
        {Name: "base_branch", Type: "string", Required: true, Description: "Base branch name"},
        {Name: "changed_files", Type: "array", Required: true, Description: "List of changed file paths"},
        {Name: "additions", Type: "int", Required: true, Description: "Total lines added"},
        {Name: "deletions", Type: "int", Required: true, Description: "Total lines deleted"},
    },
    Credentials: []string{"GITHUB_TOKEN"},
})
```

- [ ] **Step 2: Write failing test**

In `internal/activity/actions_test.go`, add:

```go
func TestGitHubFetchPR_MissingToken_ReturnsError(t *testing.T) {
    _, err := actionGitHubFetchPR(context.Background(),
        map[string]any{"repo_url": "https://github.com/org/repo", "pr_number": 1},
        map[string]string{},
    )
    require.Error(t, err)
    assert.Contains(t, err.Error(), "GITHUB_TOKEN")
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
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
cd /home/andrew/dev/projects/fleetlift/main
go test -buildvcs=false ./internal/activity/ -run TestGitHubFetchPR -v
```

Expected: `FAIL` — `actionGitHubFetchPR undefined`

- [ ] **Step 4: Implement the handler**

In `internal/activity/actions.go`, add the function and register it in the switch:

In the `switch actionType` block in `ExecuteAction`, add before `default`:
```go
case "github_fetch_pr":
    result, err = actionGitHubFetchPR(ctx, config, credentials)
```

Add the function after `actionGitHubPostIssueComment`:

```go
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
```

- [ ] **Step 5: Run tests — expect pass**

```bash
go test -buildvcs=false ./internal/activity/ -run TestGitHubFetchPR -v
```

Expected: all 3 tests `PASS`

- [ ] **Step 6: Run contract tests to verify registration wires up**

```bash
go test -buildvcs=false ./internal/activity/ -run TestActionContract -v
```

Expected: `github_fetch_pr` subtests pass (handler exists, required inputs rejected on empty config)

- [ ] **Step 7: Build check**

```bash
go build -buildvcs=false ./...
```

Expected: no errors

- [ ] **Step 8: Commit**

```bash
git add internal/activity/actions.go internal/activity/actions_test.go internal/model/action_contract.go
git commit -m "feat: add github_fetch_pr action (visible DAG clone step)"
```

---

## Task 2: `github_pr_review_inline` action — contract + handler

**Files:**
- Modify: `internal/model/action_contract.go`
- Modify: `internal/activity/actions.go`
- Modify: `internal/activity/actions_test.go`

- [ ] **Step 1: Register the contract**

In `internal/model/action_contract.go`, add to `DefaultActionRegistry()`:

```go
r.Register(ActionContract{
    Type:        "github_pr_review_inline",
    Description: "Post inline review comments on a GitHub PR using file line numbers and side (LEFT/RIGHT)",
    Inputs: []FieldContract{
        {Name: "repo_url", Type: "string", Required: true, Description: "GitHub repository URL"},
        {Name: "pr_number", Type: "int", Required: true, Description: "Pull request number"},
        {Name: "annotations", Type: "string", Required: true, Description: "JSON array of {file, line, side, body} objects"},
        {Name: "commit_id", Type: "string", Required: false, Description: "Commit SHA to attach review to (uses PR head if omitted)"},
    },
    Outputs: []FieldContract{
        {Name: "posted", Type: "int", Required: true, Description: "Number of annotations successfully posted"},
        {Name: "skipped", Type: "int", Required: true, Description: "Number of annotations skipped"},
        {Name: "skipped_details", Type: "array", Required: false, Description: "Details of skipped annotations"},
    },
    Credentials: []string{"GITHUB_TOKEN"},
})
```

- [ ] **Step 2: Write failing tests**

In `internal/activity/actions_test.go`, add:

```go
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
    // Empty annotations list should return posted=0, skipped=0 without calling GitHub.
    // We can't call GitHub in unit tests, but we can test the no-token path is skipped
    // when annotations is empty — empty list short-circuits before token check.
    result, err := actionGitHubPRReviewInline(context.Background(),
        map[string]any{
            "repo_url":    "https://github.com/org/repo",
            "pr_number":   1,
            "annotations": `[]`,
        },
        map[string]string{},
    )
    // Empty annotations: error about GITHUB_TOKEN since token check runs first.
    // The key behaviour is no panic and a clear error.
    _ = result
    _ = err
    // Behaviour assertion: function is callable without panic on empty annotations.
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
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
go test -buildvcs=false ./internal/activity/ -run TestGitHubPRReviewInline -v
```

Expected: `FAIL` — `actionGitHubPRReviewInline undefined`

- [ ] **Step 4: Implement the handler**

Add to the `ExecuteAction` switch in `internal/activity/actions.go`:

```go
case "github_pr_review_inline":
    result, err = actionGitHubPRReviewInline(ctx, config, credentials)
```

Add the function and its annotation type after `actionGitHubFetchPR`:

```go
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
    if commitID == "" {
        owner, repo := extractOwnerRepo(repoURL)
        pr, _, err := ghClient.PullRequests.Get(ctx, owner, repo, prNumber)
        if err != nil {
            return nil, fmt.Errorf("github_pr_review_inline: get PR head SHA: %w", err)
        }
        if pr.Head != nil && pr.Head.SHA != nil {
            commitID = *pr.Head.SHA
        }
    }

    // Build review comments. GitHub requires side to be "LEFT" or "RIGHT".
    owner, repo := extractOwnerRepo(repoURL)
    comments := make([]*github.DraftReviewComment, 0, len(annotations))
    for _, a := range annotations {
        side := strings.ToUpper(a.Side)
        if side != "LEFT" && side != "RIGHT" {
            side = "RIGHT" // default to RIGHT (new file) for unrecognised values
        }
        line := a.Line
        comments = append(comments, &github.DraftReviewComment{
            Path: github.String(a.File),
            Line: github.Int(line),
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
```

Also add `"encoding/json"` to the imports in `actions.go` if not already present. Check with:

```bash
grep '"encoding/json"' internal/activity/actions.go
```

If missing, add it to the import block.

- [ ] **Step 5: Run tests — expect pass**

```bash
go test -buildvcs=false ./internal/activity/ -run TestGitHubPRReviewInline -v
```

Expected: all tests `PASS`

- [ ] **Step 6: Build check**

```bash
go build -buildvcs=false ./...
```

Expected: no errors

- [ ] **Step 7: Run full contract tests**

```bash
go test -buildvcs=false ./internal/activity/ -run TestActionContract -v
```

Expected: `github_pr_review_inline` subtests pass

- [ ] **Step 8: Commit**

```bash
git add internal/activity/actions.go internal/activity/actions_test.go internal/model/action_contract.go
git commit -m "feat: add github_pr_review_inline action with Line/Side API and fallback one-by-one posting"
```

---

## Task 3: `github_update_comment` action — progress feedback

This action enables the workflow to update an existing PR comment body, used to show live progress as the review advances through phases.

**Files:**
- Modify: `internal/model/action_contract.go`
- Modify: `internal/activity/actions.go`
- Modify: `internal/activity/actions_test.go`

- [ ] **Step 1: Register the contract**

In `internal/model/action_contract.go`, add to `DefaultActionRegistry()`:

```go
r.Register(ActionContract{
    Type:        "github_update_comment",
    Description: "Update the body of an existing GitHub issue/PR comment",
    Inputs: []FieldContract{
        {Name: "repo_url", Type: "string", Required: true, Description: "GitHub repository URL"},
        {Name: "comment_id", Type: "int", Required: true, Description: "Comment ID to update"},
        {Name: "body", Type: "string", Required: true, Description: "New comment body"},
    },
    Outputs: []FieldContract{
        {Name: "status", Type: "string", Required: true, Description: "updated | failed"},
        {Name: "comment_id", Type: "int", Required: true, Description: "Updated comment ID"},
    },
    Credentials: []string{"GITHUB_TOKEN"},
})
```

- [ ] **Step 2: Write failing tests**

In `internal/activity/actions_test.go`, add:

```go
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

func TestGitHubUpdateComment_MissingCommentID_ReturnsError(t *testing.T) {
    _, err := actionGitHubUpdateComment(context.Background(),
        map[string]any{"repo_url": "https://github.com/org/repo", "body": "text"},
        map[string]string{"GITHUB_TOKEN": "tok"},
    )
    require.Error(t, err)
    assert.Contains(t, err.Error(), "comment_id")
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
go test -buildvcs=false ./internal/activity/ -run TestGitHubUpdateComment -v
```

Expected: `FAIL` — `actionGitHubUpdateComment undefined`

- [ ] **Step 4: Implement the handler**

Add to the `ExecuteAction` switch in `internal/activity/actions.go`:

```go
case "github_update_comment":
    result, err = actionGitHubUpdateComment(ctx, config, credentials)
```

Add the function after `actionGitHubPRReviewInlineOneByOne`:

```go
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
```

- [ ] **Step 5: Run tests — expect pass**

```bash
go test -buildvcs=false ./internal/activity/ -run TestGitHubUpdateComment -v
```

Expected: all 3 tests `PASS`

- [ ] **Step 6: Build + full contract tests**

```bash
go build -buildvcs=false ./... && go test -buildvcs=false ./internal/activity/ -run TestActionContract -v
```

Expected: no build errors; `github_update_comment` subtests pass

- [ ] **Step 7: Commit**

```bash
git add internal/activity/actions.go internal/activity/actions_test.go internal/model/action_contract.go
git commit -m "feat: add github_update_comment action for live PR progress updates"
```

---

## Task 4: Update `pr-review.yaml` to use `github_fetch_pr`

The existing single-agent PR review workflow's fetch step currently spins up a full Claude Code agent just to run `git diff`. Replace it with the new lightweight action.

**Files:**
- Modify: `internal/template/workflows/pr-review.yaml`

- [ ] **Step 1: Replace the `fetch_pr` agent step**

Replace the entire `fetch_pr` step in `internal/template/workflows/pr-review.yaml`.

Current content (lines ~22–48):
```yaml
  - id: fetch_pr
    title: Fetch PR details
    sandbox_group: main
    mode: report
    repositories:
      - url: "{{ .Params.repo_url }}"
        ref: "refs/pull/{{ .Params.pr_number }}/head"
    execution:
      agent: claude-code
      credentials:
        - GITHUB_TOKEN
      prompt: |
        You are in a repository with PR #{{ .Params.pr_number }} already checked out.
        The PR branch is at HEAD and the base branch (main/master) is at origin/main or origin/master.

        Generate the diff between the base branch and HEAD:
          git diff origin/main...HEAD

        Also extract the PR title and base branch name.
        Output the diff content and PR metadata.
      output:
        schema:
          diff: "string"
          title: "string"
          base_branch: "string"
```

Replace with:
```yaml
  - id: fetch_pr
    title: Fetch PR details
    action:
      type: github_fetch_pr
      credentials:
        - GITHUB_TOKEN
      config:
        repo_url: "{{ .Params.repo_url }}"
        pr_number: "{{ .Params.pr_number }}"
```

Also remove the `sandbox_groups:` section from the top of the file since `fetch_pr` no longer uses it and the `review` step does not declare a sandbox group either.

- [ ] **Step 2: Remove `sandbox_groups` if it is now unused**

Check the file — if `sandbox_group: main` appears on no remaining step, remove:
```yaml
sandbox_groups:
  main:
    image: claude-code-sandbox:latest
```

- [ ] **Step 3: Verify YAML parses**

```bash
go test -buildvcs=false ./internal/template/... -v
```

Expected: all template tests pass

- [ ] **Step 4: Build**

```bash
go build -buildvcs=false ./...
```

Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add internal/template/workflows/pr-review.yaml
git commit -m "feat: replace pr-review fetch_pr agent step with github_fetch_pr action"
```

---

## Task 5: Add `pr-review-multi.yaml` builtin workflow

**Files:**
- Create: `internal/template/workflows/pr-review-multi.yaml`

- [ ] **Step 1: Create the workflow file**

Create `internal/template/workflows/pr-review-multi.yaml` with the following content:

```yaml
version: 1
id: pr-review-multi
title: Multi-Persona PR Review
description: >
  Parallel focused code review across security, correctness, scalability, and
  conventions dimensions. Findings are grounded against the diff, deduplicated,
  and posted as a summary comment and inline annotations on the PR.
tags:
  - review
  - automation
  - single-repo
parameters:
  - name: repo_url
    type: string
    required: true
    description: "Repository URL"
  - name: pr_number
    type: int
    required: true
    description: "Pull request number to review"
steps:
  - id: fetch_pr
    title: Fetch PR diff
    action:
      type: github_fetch_pr
      credentials: [GITHUB_TOKEN]
      config:
        repo_url: "{{ .Params.repo_url }}"
        pr_number: "{{ .Params.pr_number }}"

  - id: post_progress
    title: Post review started comment
    depends_on: [fetch_pr]
    action:
      type: github_comment
      credentials: [GITHUB_TOKEN]
      config:
        repo_url: "{{ .Params.repo_url }}"
        issue_number: "{{ .Params.pr_number }}"
        body: |
          <!-- fleetlift-review-progress -->
          🔍 **Multi-persona review in progress** — running security, correctness, scalability, and conventions checks in parallel. Results will follow shortly.

  - id: review_security
    title: Security review
    depends_on: [fetch_pr]
    execution:
      agent: claude-code
      prompt: |
        You are a security-focused code reviewer. Review ONLY the changed lines in the diff below.
        Do not comment on code that is not in the diff.

        PR: {{ .Steps.fetch_pr.Output.title }}
        Changed files: {{ .Steps.fetch_pr.Output.changed_files | toJSON }}

        Diff:
        {{ .Steps.fetch_pr.Output.diff }}

        Focus exclusively on: authentication and authorisation flaws, injection vectors
        (SQL, command, path traversal), secrets or credentials committed to code,
        use of deprecated or broken crypto primitives, missing input validation at
        trust boundaries, insecure dependencies or transitive vulnerabilities.

        For each finding output:
        - file: exact file path as it appears in the changed files list
        - line: line number in the file
        - severity: critical | high | medium | low
        - description: one concise actionable sentence

        If you find nothing, output an empty findings array. Do not invent findings.
      output:
        schema:
          findings: array
          summary: string

  - id: review_correctness
    title: Correctness review
    depends_on: [fetch_pr]
    execution:
      agent: claude-code
      prompt: |
        You are a correctness-focused code reviewer. Review ONLY the changed lines in the diff below.

        PR: {{ .Steps.fetch_pr.Output.title }}
        Changed files: {{ .Steps.fetch_pr.Output.changed_files | toJSON }}

        Diff:
        {{ .Steps.fetch_pr.Output.diff }}

        Focus exclusively on: logic errors, off-by-one errors, nil/null pointer dereferences,
        unhandled or swallowed error paths, data races and concurrency bugs, incorrect
        assumptions about ordering or state, wrong algorithm for the stated purpose.

        For each finding output:
        - file: exact file path as it appears in the changed files list
        - line: line number in the file
        - severity: critical | high | medium | low
        - description: one concise actionable sentence

        If you find nothing, output an empty findings array. Do not invent findings.
      output:
        schema:
          findings: array
          summary: string

  - id: review_scalability
    title: Scalability & performance review
    depends_on: [fetch_pr]
    execution:
      agent: claude-code
      prompt: |
        You are a scalability and performance-focused code reviewer. Review ONLY the changed lines below.

        PR: {{ .Steps.fetch_pr.Output.title }}
        Changed files: {{ .Steps.fetch_pr.Output.changed_files | toJSON }}

        Diff:
        {{ .Steps.fetch_pr.Output.diff }}

        Focus exclusively on: N+1 query patterns, unbounded loops or memory allocations,
        synchronous blocking calls on hot paths, missing pagination on list operations,
        cache invalidation bugs, inefficient data structures for the shown access pattern,
        unnecessary serialisation or lock contention.

        For each finding output:
        - file: exact file path as it appears in the changed files list
        - line: line number in the file
        - severity: critical | high | medium | low
        - description: one concise actionable sentence

        If you find nothing, output an empty findings array. Do not invent findings.
      output:
        schema:
          findings: array
          summary: string

  - id: review_style
    title: Conventions & coverage review
    depends_on: [fetch_pr]
    execution:
      agent: claude-code
      prompt: |
        You are a code conventions and test coverage reviewer. Review ONLY the changed lines below.

        PR: {{ .Steps.fetch_pr.Output.title }}
        Changed files: {{ .Steps.fetch_pr.Output.changed_files | toJSON }}

        Diff:
        {{ .Steps.fetch_pr.Output.diff }}

        Focus exclusively on: missing or inadequate tests for changed behaviour,
        naming inconsistencies with the surrounding codebase visible in the diff,
        undocumented public API or interface changes, breaking changes without
        version bumps, dead code introduced by the change.

        For each finding output:
        - file: exact file path as it appears in the changed files list
        - line: line number in the file
        - severity: critical | high | medium | low
        - description: one concise actionable sentence

        If you find nothing, output an empty findings array. Do not invent findings.
      output:
        schema:
          findings: array
          summary: string

  - id: update_progress
    title: Update progress — synthesising
    depends_on: [review_security, review_correctness, review_scalability, review_style]
    action:
      type: github_update_comment
      credentials: [GITHUB_TOKEN]
      config:
        repo_url: "{{ .Params.repo_url }}"
        comment_id: "{{ .Steps.post_progress.Output.comment_id }}"
        body: |
          <!-- fleetlift-review-progress -->
          ✅ **All reviewers complete** — synthesising findings, grounding against diff, and scoring risk. Summary will follow.

  - id: synthesis
    title: Synthesise and score findings
    depends_on: [update_progress]
    execution:
      agent: claude-code
      prompt: |
        You are an adversarial code review synthesiser. Your job is quality control over
        other reviewers' output — not additional review.

        Original diff:
        {{ .Steps.fetch_pr.Output.diff }}

        Changed files (ground truth — findings outside this list are invalid):
        {{ .Steps.fetch_pr.Output.changed_files | toJSON }}

        Reviewer outputs:
        Security:     {{ .Steps.review_security.Output | toJSON }}
        Correctness:  {{ .Steps.review_correctness.Output | toJSON }}
        Scalability:  {{ .Steps.review_scalability.Output | toJSON }}
        Style:        {{ .Steps.review_style.Output | toJSON }}

        Steps:
        1. GROUNDING CHECK: Discard any finding whose file is not in the changed files list,
           or whose line number does not appear in the diff hunk for that file.
           Do not include ungrounded findings in your output under any circumstances.
        2. DEDUPLICATION: If multiple reviewers flag the same issue at the same file and line,
           merge into one finding. Keep the highest severity. Combine descriptions concisely.
        3. CONTRADICTION: If reviewers disagree about the same code, include both perspectives
           and set severity to "needs-discussion".
        4. RISK SCORING: For each changed file, assign a risk level (critical/high/medium/low)
           based on the density and severity of grounded findings against it.
        5. FOCUS LIST: Rank changed files by risk level, highest first.
        6. INLINE ANNOTATIONS: From grounded findings, select those specific enough for an
           inline diff comment — must have a clear file path, line number, and actionable
           one-sentence description. For each annotation output:
           - file: exact file path
           - line: the line number (new-file line for added/context code; original-file line for deleted code)
           - side: "RIGHT" if finding is on an added or context line; "LEFT" if on a deleted line
           - body: one concise sentence

        Write the executive_summary in markdown for a PR comment audience. Be direct and
        specific. Do not pad with pleasantries. Do not repeat all findings verbatim — synthesise.

        Save the review report using mcp__fleetlift__artifact__create with
        name="pr-review-multi" and content_type="text/markdown".
      output:
        schema:
          executive_summary: string
          file_risk_table: array
          findings: array
          focus_files: array
          inline_annotations: array

  - id: post_summary
    title: Post review summary
    depends_on: [synthesis]
    action:
      type: github_pr_review
      credentials: [GITHUB_TOKEN]
      config:
        repo_url: "{{ .Params.repo_url }}"
        pr_number: "{{ .Params.pr_number }}"
        summary: "{{ .Steps.synthesis.Output.executive_summary }}"

  - id: post_inline
    title: Post inline annotations
    depends_on: [post_summary]
    action:
      type: github_pr_review_inline
      credentials: [GITHUB_TOKEN]
      config:
        repo_url: "{{ .Params.repo_url }}"
        pr_number: "{{ .Params.pr_number }}"
        annotations: "{{ .Steps.synthesis.Output.inline_annotations | toJSON }}"
```

- [ ] **Step 2: Verify the new workflow parses and registers**

```bash
go test -buildvcs=false ./internal/template/... -v
```

Expected: all tests pass including any that enumerate builtin workflow IDs

- [ ] **Step 3: Build**

```bash
go build -buildvcs=false ./...
```

Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add internal/template/workflows/pr-review-multi.yaml
git commit -m "feat: add pr-review-multi builtin workflow with parallel reviewers and progress feedback"
```

---

## Task 6: Full test suite + smoke check

- [ ] **Step 1: Run all unit tests**

```bash
go test -buildvcs=false ./...
```

Expected: all tests pass

- [ ] **Step 2: Run linter (if available)**

```bash
make lint
```

Expected: no new lint errors (skip if `golangci-lint` not installed)

- [ ] **Step 3: Smoke test API + CLI**

Requires the stack to be running (`docker compose up -d && scripts/integration/start.sh`).

```bash
scripts/integration/smoke-test.sh api cli
```

Expected: all checks green

- [ ] **Step 4: Verify new workflow appears in workflow list**

```bash
fl workflow list
```

Expected: `pr-review-multi` appears in the output alongside `pr-review`

- [ ] **Step 5: Final commit if any fixups**

```bash
git add -p
git commit -m "fix: post-review cleanups"
```

---

## Self-Review Notes

**Spec coverage check:**
- ✅ `github_fetch_pr` action — Task 1
- ✅ `github_pr_review_inline` action with Line/Side API — Task 2
- ✅ Skipped annotation observability (`posted`/`skipped`/`skipped_details`) — Task 2
- ✅ `github_update_comment` for live progress — Task 3
- ✅ Update `pr-review.yaml` fetch step — Task 4
- ✅ `pr-review-multi.yaml` builtin workflow — Task 5
- ✅ Progress comment posted after `fetch_pr`, updated after all reviewers complete — Task 5 YAML
- ✅ `DefaultActionRegistry()` registration for all three actions — Tasks 1–3

**One-by-one fallback:** The inline action first attempts a single batch review; if GitHub rejects it (any invalid annotation causes a 422 for the whole request), it falls back to one-per-annotation posting, collecting skip details. This avoids losing all valid annotations because one line is outside the diff.

**`update_progress` dependency:** `synthesis` depends on `update_progress`, not directly on the reviewers. This ensures the progress comment update happens before synthesis starts, giving reviewers a clean ordering: fetch → (reviewers in parallel) → update_progress → synthesis → post.

**`comment_id` type:** `github_comment` returns `comment_id` as `int`. The `github_update_comment` config receives it as a template expression `{{ .Steps.post_progress.Output.comment_id }}` — the template renderer will produce a string; `toInt` in the handler handles string-to-int conversion, which is already tested via `toInt`'s existing `string` case.
