# Design Options: Multi-Step Eval Orchestration Alternanives

**Date:** 2026-04-10
**Status:** Draft — exploring alternanitve approaches,  
**Selected approach:** [Four-File Architecture (main plan)](./2026-04-10-multi-step-eval-orchestration.md)
**Context:** [Eval Framework PRD](./2026-04-08-eval-framework-prd.md), [ADR-001](./2026-04-08%20eval-framework.md)

---

## Problem

The eval framework PRD describes a judge helper and output fetching, but does not address three harder problems:

1. **Multi-step eval logic** — some evals need setup before the workflow runs (reset repo to a pinned commit) and teardown after (clean up branches). The eval is not just "run workflow → check output" — it's "prepare state → run workflow → check output → clean up."
2. **Side-effect suppression** — workflows like `pr-review` post comments to GitHub, `auto-debt-slayer` creates real PRs, `triage` applies labels. During eval runs, these side effects must not fire — but the agent steps that produce the reviewable output must still run.
3. **Golden set as the source of truth** — evals run against a predefined golden set (YAML file + validation schema) that defines test cases: inputs, repo states, rubrics, expected outputs. The system must support flexible text data, composite scoring (deterministic + LLM judge), and extensible rubric types.

### Concrete examples


| Workflow           | Setup needed                              | Side effects to suppress                     | What to evaluate                                              |
| ------------------ | ----------------------------------------- | -------------------------------------------- | ------------------------------------------------------------- |
| `pr-review`        | PR exists at known state                  | `post-comments` action (GitHub comment)      | `review` step structured output (summary, findings, approval) |
| `auto-debt-slayer` | Repo at pinned commit, Jira ticket exists | `notify` action (Slack), PR creation         | `assess` decision accuracy, `execute` code quality            |
| `triage`           | GitHub issue at known state               | `classify` action (labels), `comment` action | `analyze` step output (type, severity, component)             |
| `doc-assessment`   | Repos at known states                     | PR creation (in fix mode)                    | Per-repo scores, finding quality                              |


---

## Golden Set Schema

All approaches below share the same golden set format. A golden set is a YAML file defining test cases with inputs, expected states, and evaluation rubrics.

```yaml
# tests/eval/golden/pr-review.golden.yaml
version: 1
workflow_id: pr-review
description: "PR review quality evaluation suite"

# Shared defaults for all cases in this file
defaults:
  timeout: 10m
  parameters:
    model: claude-sonnet-4-5-20250514

cases:
  - id: security-vuln-detection
    description: "PR introducing SQL injection should be flagged"

    # Inputs passed as workflow parameters
    parameters:
      repo_url: "https://github.com/tinkerloft/eval-fixtures"
      pr_number: "42"
      # repo state: the fixture repo has PR #42 frozen at a known diff

    # Steps to suppress during eval (no GitHub side effects)
    suppress_steps:
      - post-comments

    # Composite rubrics — evaluated against step outputs
    rubrics:
      - step_id: review
        type: deterministic
        checks:
          - field: "output.findings"
            op: contains_match
            match: { category: "security" }
          - field: "output.summary"
            op: contains
            value: "SQL injection"
          - field: "output.approval"
            op: not_equals
            value: "approve"

      - step_id: review
        type: llm_judge
        prompt: |
          The PR introduces an unsanitized SQL query in handlers/users.go.
          The review should:
          1. Identify the SQL injection vulnerability
          2. Suggest parameterized queries as a fix
          3. Not approve the PR.
          Rate the complience of the PR from 0 to 5.
        threshold: 4

  - id: clean-pr-approval
    description: "Well-written PR should be approved with minor style notes"
    parameters:
      repo_url: "https://github.com/tinkerloft/eval-fixtures"
      pr_number: "43"
    suppress_steps:
      - post-comments
    rubrics:
      - step_id: review
        type: deterministic
        checks:
          - field: "output.approval"
            op: one_of
            values: ["approve", "approve_with_comments"]
      - step_id: review
        type: llm_judge
        prompt: |
          This is a clean, well-tested PR. The review should approve it,
          possibly with minor style suggestions. It should NOT flag false
          security or correctness issues. Rate the complience of the PR from 0 to 5.
        threshold: 3
```

**Golden set validation schema** — a JSON Schema document that validates the golden YAML structure, ensuring required fields exist and rubric types are valid. Lives alongside the golden files.

---

## Option A: Eval Wrapper Workflow

**Idea:** Create a separate "eval workflow" YAML that wraps the target workflow. The eval workflow has setup steps, runs the target, then has assertion steps.

```yaml
version: 1
id: eval-pr-review
title: "Eval: PR Review Quality"
tags: [eval]

parameters:
  - name: golden_set_path
    type: string
    required: true

steps:
  # Setup: prepare repo state
  - id: setup
    mode: report
    execution:
      agent: shell
      prompt: |
        git clone https://github.com/tinkerloft/eval-fixtures /workspace/repo
        cd /workspace/repo && git checkout {{ .Params.pinned_commit }}

  # Run the actual workflow under test
  - id: run-target
    depends_on: [setup]
    action:
      type: run_workflow    # NEW action type
      config:
        workflow_id: pr-review
        parameters:
          repo_url: "{{ .Steps.setup.Output.repo_url }}"
          pr_number: "{{ .Params.pr_number }}"
        suppress_steps: [post-comments]  # skip side-effect steps
        wait: true

  # Evaluate outputs
  - id: evaluate
    depends_on: [run-target]
    mode: report
    execution:
      agent: claude-code
      prompt: |
        Evaluate the PR review output against the golden set rubric.
        Review output: {{ .Steps.run-target.Output | toJSON }}
        Rubric: {{ .Params.rubric }}
```

### Pros

- Uses existing workflow YAML primitives — no new schema language
- Setup/teardown are regular steps with full sandbox access
- Visible in the UI as a normal run with DAG visualization
- Can reuse existing step types (shell, claude-code, actions)

### Cons

- **Requires a new `run_workflow` action type** — non-trivial platform change to nest workflow execution
- Proliferates workflow files (1 eval workflow per target workflow)
- Eval logic split between YAML (orchestration) and Go (rubric evaluation)
- Golden set data embedded in YAML parameters becomes unwieldy for many test cases
- **Temporal child workflow complexity** — nested workflow-within-workflow adds operational opacity

---

## Option B: Go Test Driver with Golden Set Files

**Idea:** Eval orchestration lives entirely in Go tests. A test driver reads the golden set YAML, triggers the workflow via REST API with appropriate parameters, polls for completion, then evaluates outputs against rubrics. Setup/teardown are Go test helpers.

```go
//go:build eval

package eval_test

func TestPRReview(t *testing.T) {
    golden := loadGoldenSet(t, "testdata/pr-review.golden.yaml")
    client := eval.NewClient(apiURL(t), apiToken(t))

    for _, tc := range golden.Cases {
        t.Run(tc.ID, func(t *testing.T) {
            t.Parallel()

            // Trigger workflow with suppress_steps parameter
            runID := client.StartRun(t, tc.WorkflowID, eval.RunOpts{
                Parameters:    tc.Parameters,
                SuppressSteps: tc.SuppressSteps,
            })

            // Poll until terminal
            result := client.PollRun(t, runID, tc.Timeout)
            require.Equal(t, "complete", result.Run.Status)

            // Evaluate each rubric
            for _, rubric := range tc.Rubrics {
                stepOutput := findStepOutput(t, result, rubric.StepID)
                switch rubric.Type {
                case "deterministic":
                    evalDeterministic(t, stepOutput, rubric.Checks)
                case "llm_judge":
                    evalLLMJudge(t, stepOutput, rubric)
                }
            }
        })
    }
}
```

**Setup/teardown** for cases requiring repo state:

```yaml
# In the golden set:
cases:
  - id: ads-known-bug
    setup:
      type: git_checkout
      repo: "https://github.com/tinkerloft/eval-fixtures"
      commit: "abc123def"
      branch: "eval/ads-known-bug"   # ephemeral branch created by setup
    teardown:
      type: git_delete_branch
      branch: "eval/ads-known-bug"
    parameters:
      github_repo: "https://github.com/tinkerloft/eval-fixtures"
      ticket_key: "TEST-001"
    # ...
```

The Go test driver executes `setup` before `StartRun` and `teardown` after evaluation:

```go
func (c *Client) StartRun(t *testing.T, workflowID string, opts RunOpts) string {
    t.Helper()
    if opts.Setup != nil {
        runSetup(t, opts.Setup)       // git checkout, create branch, etc.
        t.Cleanup(func() {
            runTeardown(t, opts.Setup) // delete branch, etc.
        })
    }
    // POST /api/runs with suppress_steps in parameters
    // ...
}
```

### Pros

- **Golden set is the single source of truth** — all test cases, rubrics, setup, teardown in YAML
- Go test driver is thin (~200 LOC) — reads YAML, calls REST API, evaluates rubrics
- No new workflow YAML files — the target workflow runs as-is
- `t.Parallel()`, `t.Cleanup()`, `t.Run()` give proper test lifecycle
- Composite scoring is natural — deterministic checks + LLM judge in sequence
- Extensible rubric types — add a new `case` in the `switch` statement
- Standard `go test` output, IDE integration, `-run` filtering

### Cons

- **Requires `suppress_steps` support in the platform** — the REST API or workflow engine must accept a list of steps to skip
- Setup/teardown in Go means additional Go code for each setup type (git_checkout, create_issue, etc.)
- Two languages: YAML (golden set) + Go (driver) — but this is already the pattern for workflow YAML + Go engine

---

## Option C: Eval-Annotated Workflow YAML

**Idea:** Add an `eval` section directly to existing workflow YAML files. The eval section defines test cases, rubrics, and step annotations inline. A test runner reads the workflow YAML, extracts the eval section, and executes.

```yaml
version: 1
id: pr-review
title: Automated PR Review
# ... normal workflow definition ...

steps:
  - id: fetch_pr
    # ...
  - id: review
    # ...
  - id: post-comments
    action:
      type: github_pr_review
    eval:
      suppress: true    # <-- annotation: skip this step during evals

# Eval section — ignored by the workflow engine, read by eval runner
eval:
  golden_set:
    - id: security-vuln
      parameters:
        repo_url: "https://github.com/tinkerloft/eval-fixtures"
        pr_number: "42"
      rubrics:
        - step_id: review
          type: deterministic
          checks:
            - field: "output.approval"
              op: not_equals
              value: "approve"
        - step_id: review
          type: llm_judge
          prompt: "Should flag SQL injection..."
          threshold: 4
```

### Pros

- **Eval lives next to the workflow it tests** — co-location reduces drift
- Step-level `eval.suppress: true` is clean and declarative
- No separate golden set files to maintain
- Workflow authors write both the workflow and its eval criteria

### Cons

- **Pollutes workflow YAML with test concerns** — workflow YAML becomes larger and harder to read
- Golden set cases are embedded in the workflow definition — awkward when you have 15 test cases for one workflow
- Builtin workflows are embedded in the Go binary — editing eval sections requires rebuilding
- Schema validation becomes complex (workflow schema + eval schema in one file)
- **Breaks separation of concerns** — workflow definition and test definition are different audiences

---

## Option D: Standalone Eval Schema Files (Recommended exploration)

**Idea:** A dedicated `eval.yaml` schema that references target workflows and golden sets. Neither embedded in Go tests nor in workflow YAML. The eval runner is a Go binary or test suite that reads these files.

```
tests/eval/
  runner_test.go              # Go test driver (~200 LOC)
  rubrics/                    # Reusable rubric definitions
    deterministic.go
    llm_judge.go
  setup/                      # Setup/teardown handlers
    git.go                    # git_checkout, git_delete_branch
    github.go                 # create_issue, create_pr (fixture)
  golden/                     # Golden set files
    pr-review.golden.yaml
    auto-debt-slayer.golden.yaml
    triage.golden.yaml
    doc-assessment.golden.yaml
```

**Golden set file** (same as shown in the Golden Set Schema section above):

```yaml
version: 1
workflow_id: pr-review
description: "PR review quality evaluation"

defaults:
  timeout: 10m

cases:
  - id: security-vuln-detection
    parameters: { repo_url: "...", pr_number: "42" }
    suppress_steps: [post-comments]
    rubrics:
      - step_id: review
        type: deterministic
        checks:
          - { field: "output.findings", op: contains_match, match: { category: "security" } }
      - step_id: review
        type: llm_judge
        prompt: "Should identify SQL injection..."
        threshold: 4

  - id: clean-pr-approval
    parameters: { repo_url: "...", pr_number: "43" }
    suppress_steps: [post-comments]
    rubrics:
      - step_id: review
        type: deterministic
        checks:
          - { field: "output.approval", op: one_of, values: ["approve"] }
```

**Go test driver** reads all `*.golden.yaml` files and generates subtests:

```go
//go:build eval

func TestGoldenSets(t *testing.T) {
    files, _ := filepath.Glob("golden/*.golden.yaml")
    for _, f := range files {
        gs := loadGoldenSet(t, f)
        t.Run(gs.WorkflowID, func(t *testing.T) {
            for _, tc := range gs.Cases {
                t.Run(tc.ID, func(t *testing.T) {
                    t.Parallel()
                    runAndEvaluate(t, gs.WorkflowID, tc)
                })
            }
        })
    }
}
```

### Pros

- **Clean separation**: workflow YAML, golden set YAML, and Go driver are each independent
- Golden set is the single source of truth — adding a test case = adding YAML, no Go code
- Go driver is generic (~200 LOC) — works for any workflow without per-workflow test code
- Extensible rubric types via Go interface: `type Rubric interface { Evaluate(output, config) Result }`
- Composite scoring is first-class — deterministic + LLM judge per step, aggregated per case
- Standard `go test` with `//go:build eval` gating
- Golden set schema can be validated separately (CI can check golden YAML without running evals)

### Cons

- **Still requires `suppress_steps` in the platform** (shared with Option B)
- Three moving parts: golden YAML + Go driver + platform support
- Setup/teardown types need Go handlers (same as Option B)

---

## Cross-Cutting Concern: Step Suppression

All options except A need a mechanism to skip side-effect steps during eval runs. Three implementation approaches:

### S1: `suppress_steps` parameter on `POST /api/runs`

```json
POST /api/runs
{
  "workflow_id": "pr-review",
  "parameters": { "repo_url": "...", "pr_number": "42" },
  "suppress_steps": ["post-comments"]
}
```

The DAG engine checks `suppress_steps` before executing each step. Suppressed steps are marked `skipped` with reason `suppressed`. Downstream steps that depend on suppressed steps use the suppressed step's *would-have-been* inputs (prior step outputs are still available).

**Pros:** Clean API, no workflow YAML changes, suppression is caller-controlled.
**Cons:** New API field, DAG engine change.

### S2: Condition override via parameters

Add a conventional `eval_mode` parameter to workflows:

```yaml
parameters:
  - name: eval_mode
    type: bool
    default: false

steps:
  - id: post-comments
    condition: "{{ not .Params.eval_mode }}"
    action:
      type: github_pr_review
```

**Pros:** No platform changes — pure YAML convention.
**Cons:** Every workflow must add the parameter and conditions manually. Pollutes production workflow definitions with eval concerns. Easy to forget on new workflows.

### S3: Step-level `eval.suppress` annotation (from Option C)

```yaml
steps:
  - id: post-comments
    eval:
      suppress: true
```

The DAG engine skips steps with `eval.suppress: true` when the run is started with `eval_mode: true`.

**Pros:** Annotation lives on the step that has side effects — discoverable by reading the workflow.
**Cons:** Mixes eval concerns into workflow YAML. Requires `eval_mode` flag anyway to activate.

**Recommendation:** S1 (`suppress_steps` on the API) is cleanest — eval logic stays in the eval layer, workflow YAML stays clean.

---

## Cross-Cutting Concern: Setup & Teardown

Evals that need repo state (ADS at pinned commit, PR review with known PR) require setup before the workflow runs.

### Setup types needed


| Type                  | What it does                                             | Used by               |
| --------------------- | -------------------------------------------------------- | --------------------- |
| `git_checkout`        | Create ephemeral branch at pinned commit in fixture repo | ADS, bug-fix          |
| `git_create_pr`       | Create a PR with known diff in fixture repo              | pr-review             |
| `github_create_issue` | Create issue with known content                          | triage                |
| `noop`                | No setup — workflow params are sufficient                | doc-assessment, audit |


### Where setup lives

In the golden set YAML:

```yaml
cases:
  - id: ads-known-bug
    setup:
      - type: git_checkout
        repo: "https://github.com/tinkerloft/eval-fixtures"
        commit: "abc123"
        branch: "eval/ads-{{ .CaseID }}"
    teardown:
      - type: git_delete_branch
        repo: "https://github.com/tinkerloft/eval-fixtures"
        branch: "eval/ads-{{ .CaseID }}"
```

Go handlers in `tests/eval/setup/` implement each type. `t.Cleanup()` handles teardown automatically.

---

## Comparison Matrix


| Dimension                 | A: Wrapper Workflow       | B: Go Test Driver    | C: Annotated YAML          | D: Standalone Schema |
| ------------------------- | ------------------------- | -------------------- | -------------------------- | -------------------- |
| Golden set location       | Workflow params           | Separate YAML        | Inline in workflow         | Separate YAML        |
| Setup/teardown            | Workflow steps            | Go helpers           | Workflow steps             | Go helpers           |
| Side-effect suppression   | Skip step in wrapper      | `suppress_steps` API | `eval.suppress` annotation | `suppress_steps` API |
| New platform changes      | `run_workflow` action     | `suppress_steps` API | `eval` YAML field + engine | `suppress_steps` API |
| Rubric extensibility      | Agent prompt in eval step | Go interface         | YAML + agent               | Go interface         |
| Separation of concerns    | Medium                    | High                 | Low                        | **High**             |
| Adding a test case        | New YAML or params        | Add YAML case        | Edit workflow YAML         | **Add YAML case**    |
| Workflow YAML pollution   | None (separate file)      | None                 | **Yes**                    | None                 |
| Go code per workflow      | 1 workflow YAML each      | Shared driver        | Shared driver              | **Shared driver**    |
| UI visibility of eval run | Yes (normal run)          | External (go test)   | Yes (normal run)           | External (go test)   |
| Complexity                | High (nested workflows)   | Medium               | Medium                     | **Medium**           |


---

## Open Questions

1. **Fixture repo strategy** — single `tinkerloft/eval-fixtures` repo with branches per test case, or multiple fixture repos?
2. **Rubric type extensibility** — start with `deterministic` + `llm_judge` only, or design the interface for custom types from day one?
3. **Golden set versioning** — should golden sets be versioned independently from the workflow they test? (Useful when workflow changes but expected outputs haven't been updated yet.)
4. **Eval result persistence** — should eval results be written back to the DB (new `eval_results` table) for trend tracking, or is `go test -v` output sufficient for v1?
5. **Parallel eval runs** — how many concurrent workflow runs can the dev stack handle? This determines whether `t.Parallel()` needs throttling.

