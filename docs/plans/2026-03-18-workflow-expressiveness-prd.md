# PRD: Workflow Expressiveness Improvements

**Date:** 2026-03-18
**Status:** Draft
**Source:** Design friction encountered building the `doc-assessment` builtin workflow
**Also required by:** [Auto-Debt-Slayer workflow plan](./auto-debt-slayer-workflow.md) — Improvements 1 and 2 are hard blockers for that workflow

---

## Background

During design of the `doc-assessment` workflow, four platform limitations forced workarounds that complicated the design, reduced observability, or required implementation compromises. This PRD documents those gaps and proposes targeted improvements.

---

## Improvement 1: Conditional PR Creation (Only Create PRs When There Are Changes)

### Problem

The platform unconditionally attempts PR creation for every fan-out child in a `mode: transform` step that has a `pull_request` config. It runs `git add -A && git commit` regardless of whether the agent modified anything. When the agent makes no changes (e.g., because the repo scored above the fix threshold), the `git commit` fails with "nothing to commit." This produces a soft error on every no-op child — spurious noise at fleet scale.

In `doc-assessment`, this forced us to abandon the platform's `pull_request` step config entirely and have the agent create PRs via `gh pr create` inside the prompt. This works but has a cost: PR URLs are tracked only in the agent's structured JSON output, not in `step_runs.pr_url` / `step_runs.branch_name` — losing platform-level PR observability (UI links, reporting).

### Proposed Change

Before executing `git add -A && git commit`, the `CreatePullRequest` activity should check whether the working tree has any staged or unstaged changes. If the working tree is clean, skip PR creation silently (not as an error). No branch is pushed; `step_runs.pr_url` and `step_runs.branch_name` are left empty.

```
git status --porcelain  →  empty  →  skip PR creation (not an error)
git status --porcelain  →  changes  →  proceed with commit + push + PR
```

### Impact

- Removes spurious errors from report-mode fan-out runs at fleet scale
- Restores platform-level PR tracking for conditional fix workflows
- `doc-assessment` can use `mode: transform` with `pull_request` config rather than agent-driven `gh pr create`

---

## Improvement 2: Template Rendering for `pull_request` Config Fields

### Problem

Go template rendering is applied only to `Execution.Prompt` and `Repositories` fields. The `PullRequest` struct fields (`Draft`, `Title`, `Body`, `BranchPrefix`, `Labels`) are passed through to the `CreatePullRequest` activity without rendering. This means workflow parameters cannot flow into PR config.

In `doc-assessment`, the `draft_prs` parameter (default: `true`) could not be wired into `pull_request.draft: {{ .Params.draft_prs }}` — the YAML parser would try to parse the template string as a Go bool and fail. The workaround was to inject `draft_prs` into the prompt and have the agent interpret it as a `gh pr create --draft` flag — indirect and fragile.

### Proposed Change

Apply `RenderPrompt` (or a lightweight equivalent) to all string and bool fields of `PRDef` before the step executes. Specifically: `Title`, `Body`, `BranchPrefix` (strings), and `Draft` (bool — render the template, then parse the result as a bool).

Example YAML that should work after this change:

```yaml
pull_request:
  branch_prefix: "docs/fleetlift-assessment-"
  title: "docs: fix documentation issues (score: {{ .Output.scores.overall }}/5)"
  body: |
    Automated fixes by Fleetlift doc-assessment.
    Files changed: {{ .Output.files_modified | join ", " }}
  draft: "{{ .Params.draft_prs }}"
  labels: ["documentation", "automated"]
```

### Impact

- Enables dynamic PR titles/bodies from step structured output (e.g., include repo score, list of changed files)
- Makes `draft_prs`-style parameters work correctly without prompt injection
- Unlocks more expressive PR workflows in general (migration summaries, audit findings in PR body, etc.)

---

## Improvement 3: Per-Repo Conditional Fan-Out (Filter Fan-Out Based on Upstream Per-Repo Output)

### Problem

The condition system operates at the step level: a condition can skip an entire step (e.g., `{{ eq .Params.mode "fix" }}`), but cannot filter the fan-out list based on per-repo structured output from a preceding fan-out step.

In `doc-assessment`, the natural design was two steps:

```
assess (fan-out: all repos)  →  create-prs (fan-out: only repos where fix_applied=true)
```

But the platform has no mechanism to express "fan out only over repos where the upstream per-repo output has `fix_applied=true`." The condition field on `create-prs` can only check step-level status. This forced us to collapse both phases into the `assess` step, losing the clean separation between "evaluate" and "act."

### Proposed Change

Add a `filter` field to step definitions alongside `condition`. Where `condition` skips the entire step, `filter` is a Go template expression evaluated **per repo** against the corresponding entry in the upstream fan-out step's `Outputs` array. Only repos for which the filter evaluates true are included in this step's fan-out.

```yaml
steps:
  - id: assess
    repositories: "{{ .Params.repos }}"
    execution:
      ...

  - id: create-prs
    depends_on: [assess]
    repositories: "{{ .Params.repos }}"
    filter: "{{ index .Steps.assess.OutputsByRepo .Repo.Name \"fix_applied\" }}"
    pull_request:
      ...
```

This requires:
1. A new `OutputsByRepo` map on step outputs (keyed by repo URL or name) computed from the fan-out `Outputs` array
2. A `filter` field on `StepDef` evaluated per-repo during fan-out construction
3. Template context for filter includes `.Repo` (the current repo being considered) and `.Steps` (prior step outputs)

### Impact

- Enables "assess then selectively act" patterns without collapsing two logical steps into one
- Makes fleet workflows with conditional downstream actions (PR creation, notifications, labeling) natural to express
- The `doc-assessment` workflow becomes a clean two-step fan-out: `assess → create-prs` with filtered repos

---

## Improvement 4: Sandbox Group Reuse Across Fan-Out Children

### Problem

`sandbox_group` allows steps within the same parent DAG to share a provisioned sandbox, avoiding re-provisioning costs. However, this only works for non-fan-out steps. Fan-out steps spawn child `StepWorkflow` instances, and sandbox group sharing is not propagated to children. Each fan-out child always provisions a fresh sandbox and re-clones its repository.

In `doc-assessment`, this meant the `assess` step (clone + analyze + fix) and a potential `create-prs` step (clone + create PR) for the same repo would each clone the repository from scratch. For large repos, or at fleet scale, this doubles clone time and network egress per repo.

The ideal flow — once a repo is cloned for assessment, reuse that sandbox for PR creation — was unachievable. This was a secondary factor in preferring the single-step approach.

### Proposed Change

Extend sandbox group semantics to work across fan-out steps. When two fan-out steps share the same `sandbox_group` name and fan out over the same repo list, the system should:

1. Use the same sandbox instance for both steps' children operating on the same repo
2. Manage sandbox lifecycle based on the last step in the group that references it (not the first step to complete)
3. Clean up unused group sandboxes (where the repo was filtered out of downstream steps)

Implementation consideration: fan-out child IDs are `{runID}-{stepID}-{index}`. For sandbox group reuse, the sandbox would need to be keyed by `{runID}-{sandboxGroup}-{repoIndex}` rather than by step ID, and the group sandbox provisioned once per repo on first step, reused on subsequent steps, destroyed on last step completion.

### Impact

- Eliminates repo re-cloning for multi-step fan-out workflows that operate on the same repos
- Warm sandbox context: the agent in a follow-up step inherits file state from the prior step (no re-checkout, no re-install of dependencies)
- Meaningful cost and latency reduction at fleet scale (hundreds of repos × two steps each)
- Unlocks more granular workflow decomposition without paying a re-clone penalty

---

## Priority

| # | Improvement | Effort | Impact | Priority |
|---|-------------|--------|--------|----------|
| 1 | Conditional PR creation (clean working tree = skip) | Low | High | P1 |
| 2 | Template rendering for `pull_request` fields | Low | Medium | P1 |
| 3 | Per-repo conditional fan-out (`filter` field) | Medium | High | P2 |
| 4 | Sandbox group reuse across fan-out children | High | Medium | P3 |

Improvements 1 and 2 are small, well-contained changes to existing activities and template rendering. They unblock the most common patterns (conditional fixes, parameterized PR titles/drafts) without architectural changes. Improvement 3 requires DAG and template engine changes but unlocks a whole class of "assess then selectively act" workflows. Improvement 4 is the most complex but pays off most at fleet scale.
