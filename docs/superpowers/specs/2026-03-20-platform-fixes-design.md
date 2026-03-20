# Design: Platform Fixes for Workflow Expressiveness

**Date:** 2026-03-20
**Status:** Approved
**Context:** Required before the auto-debt-slayer workflow can be implemented. See [auto-debt-slayer-workflow.md](../../plans/auto-debt-slayer-workflow.md) and [workflow-expressiveness PRD](../../plans/2026-03-18-workflow-expressiveness-prd.md) (Improvements 1 & 2).

---

## Fix 1: `evalCondition` must expose step output

**File:** `internal/workflow/dag.go`

`evalCondition` builds its per-step context with only `status` and `error`. Step `output` is absent, so any condition referencing structured step output silently evaluates to false.

**Change:** In `evalCondition`, inside the `for id, out := range outputs` loop, add `"output": out.Output` to the per-step map:

```go
steps[id] = map[string]any{
    "status": string(out.Status),
    "error":  out.Error,
    "output": out.Output,   // add this
}
```

**Casing note:** `evalCondition` uses lowercase data keys (`.steps`, `.params`), while `RenderPrompt` uses the exported `RenderContext` struct (`.Steps`, `.Params`). Condition expressions must use lowercase: `{{ eq .steps.assess.output.decision "execute" }}`.

**Tests:** Add cases to `dag_test.go` covering conditions that read from step output — both truthy and falsy paths.

---

## Fix 2: `pull_request` config fields must be template-rendered

**Files:** `internal/workflow/dag.go` (`resolveStep`)

`resolveStep` assigns `opts.PRConfig = step.PullRequest` with no rendering. String fields `BranchPrefix`, `Title`, and `Body` cannot reference params or prior step outputs.

**Change:** In `resolveStep`, move the `pull_request` rendering block **outside** the `step.Execution == nil` early-return guard so it applies to all steps regardless of whether they have an `execution` block. Render the three string fields through `RenderPrompt` on a shallow copy of `PRDef`:

```go
// After the Execution == nil guard and all Execution rendering:
if step.PullRequest != nil {
    pr := *step.PullRequest  // shallow copy — don't mutate the original StepDef
    ctx := fltemplate.RenderContext{Params: params, Steps: outputs}
    var err error
    if pr.BranchPrefix, err = fltemplate.RenderPrompt(pr.BranchPrefix, ctx); err != nil {
        return opts, fmt.Errorf("render pull_request.branch_prefix for step %s: %w", step.ID, err)
    }
    if pr.Title, err = fltemplate.RenderPrompt(pr.Title, ctx); err != nil {
        return opts, fmt.Errorf("render pull_request.title for step %s: %w", step.ID, err)
    }
    if pr.Body, err = fltemplate.RenderPrompt(pr.Body, ctx); err != nil {
        return opts, fmt.Errorf("render pull_request.body for step %s: %w", step.ID, err)
    }
    opts.PRConfig = &pr
}
```

`Draft` (bool) and `Labels` ([]string) are parsed directly from YAML and need no rendering.

**Tests:** Add unit tests for `resolveStep` covering template rendering in `BranchPrefix`, `Title`, and `Body` — including references to params and prior step outputs.

---

## Fix 3: Skip PR creation when working tree is clean

**File:** `internal/activity/pr.go` (`CreatePullRequest`)

The current implementation runs four git commands in a sequential loop:

```
git checkout -b <branch>
git add -A
git commit -m <title>
git push origin <branch>
```

When the agent makes no file changes, `git commit` fails with "nothing to commit." This surfaces as an error on no-op agent runs.

**Change:** Break the command loop into individual sequential exec calls. After `git checkout -b` and `git add -A`, run `git status --porcelain`. If the output is empty, return `("", nil)` immediately — no commit, no push, no GitHub API call. The branch creation (`git checkout -b`) must still run before the status check so any commit that does proceed lands on the correct branch.

Requires adding `"strings"` to the import block in `pr.go`.

Pseudocode:
```
exec: git checkout -b <branch>
exec: git add -A
exec: git status --porcelain  →  if empty, return ("", nil)
exec: git commit -m <title>
exec: git push origin <branch>
// GitHub PR creation proceeds as before
```

**Tests:** `CreatePullRequest` currently constructs its GitHub client inline via `os.Getenv("GITHUB_TOKEN")`, making it untestable without the env var. Before writing the clean-tree test, refactor the GitHub client to be injectable (e.g., accept a `*github.Client` parameter, or add a `GitHubClientFunc` field to `Activities`). With that in place, add a test covering:
- Clean working tree → returns `("", nil)`, GitHub client never called
- Dirty working tree → proceeds normally through to PR creation

---

## Implementation Order

1. Fix 3 (clean working tree) — touches only `pr.go`, includes GitHub client refactor for testability
2. Fix 1 (evalCondition output) — touches only `dag.go`
3. Fix 2 (pull_request rendering) — touches `dag.go`, logically after Fix 1

All three ship in a single PR.
