# Eval Orchestration: Four-File Architecture

**Date:** 2026-04-10
**Status:** Draft
**Context:** [Eval Framework PRD](./2026-04-08-eval-framework-prd.md), [ADR-001](./2026-04-08%20eval-framework.md)
**Alternatives:** [Design Options Explored](./2026-04-10-multi-step-eval-orchestration-alternatives.md)

---

## Problem

The eval framework PRD describes a judge helper and output fetching, but does not address three harder problems:

1. **Multi-step eval logic** — some evals need setup before the workflow runs (reset repo to a pinned commit) and teardown after (clean up branches). The eval is not just "run workflow → check output" — it's "prepare state → run workflow → check output → clean up."
2. **Side-effect suppression** — workflows like `pr-review` post comments to GitHub, `auto-debt-slayer` creates real PRs, `triage` applies labels. During eval runs, these side effects must not fire — but the agent steps that produce the reviewable output must still run.
3. **Golden set as input data** — evals run against a predefined golden set: an array of inputs (repo, commit, rubric text, parameters) fed to an eval template. The system must support flexible text data, composite scoring (deterministic + LLM judge), and extensible rubric types.

### Concrete examples


| Workflow           | Setup needed                              | Side effects to suppress                     | What to evaluate                                              |
| ------------------ | ----------------------------------------- | -------------------------------------------- | ------------------------------------------------------------- |
| `pr-review`        | PR exists at known state                  | `post-comments` action (GitHub comment)      | `review` step structured output (summary, findings, approval) |
| `auto-debt-slayer` | Repo at pinned commit, Jira ticket exists | `notify` action (Slack), PR creation         | `assess` decision accuracy, `execute` code quality            |
| `triage`           | GitHub issue at known state               | `classify` action (labels), `comment` action | `analyze` step output (type, severity, component)             |
| `doc-assessment`   | Repos at known states                     | PR creation (in fix mode)                    | Per-repo scores, finding quality                              |


---

## Architecture: Four-File Separation

Clean four-way separation of concerns. Each file type has a single responsibility:


| File type         | Responsibility                                                                  | Changes when...          |
| ----------------- | ------------------------------------------------------------------------------- | ------------------------ |
| **Workflow**      | Production workflow logic                                                       | Business logic changes   |
| **Eval Template** | How to test — setup/teardown steps, suppress_steps, rubric evaluation           | Test methodology changes |
| **Golden Set**    | Pure input data — array of test case inputs (repo, commit, rubric text, params) | Test cases added/updated |
| **Eval Suite**    | Binding + config — references eval template + golden set(s) + overrides         | Test composition changes |


```
Eval Suite ──references──→ Eval Template ──references──→ Workflow (production)
     │                          │
     └──references──→ Golden Set(s) ──feeds inputs──→ Eval Template
```

### File structure

```
internal/template/workflows/
  pr-review.yaml                        # Workflow (production, untouched)
  auto-debt-slayer.yaml
  triage.yaml

tests/eval/
  templates/                            # Eval Templates
    pr-review.eval.yaml
    auto-debt-slayer.eval.yaml
    triage.eval.yaml
  golden/                               # Golden Sets (pure input data)
    pr-review-security.golden.yaml
    pr-review-style.golden.yaml
    ads-known-bugs.golden.yaml
    triage-issues.golden.yaml
  suites/                               # Eval Suites (binding + config)
    pr-review-full.suite.yaml
    pr-review-quick.suite.yaml
    ads-regression.suite.yaml
    nightly.suite.yaml
  runner_test.go                        # Go test driver (~300 LOC)
  rubrics/                              # Rubric evaluators (Go)
    deterministic.go
    llm_judge.go
```

---

## Eval Template

Defines **how** to test a workflow — setup/teardown steps (reusing workflow step syntax), which steps to suppress, and what rubric types apply. Does NOT contain test case data.

```yaml
# tests/eval/templates/pr-review.eval.yaml
version: 1
id: eval-pr-review
workflow_id: pr-review                  # target workflow under test

# Setup steps run BEFORE the target workflow, per test case.
# Uses standard workflow step syntax — reuses the existing engine.
# Template context: {{ .Input.<field> }} from golden set entry.
setup:
  - id: prepare-fixture
    execution:
      agent: shell
      prompt: |
        cd /workspace
        git clone {{ .Input.repo_url }} repo
        cd repo
        {{ if .Input.commit }}git checkout {{ .Input.commit }}{{ end }}
        {{ if .Input.branch }}
        git checkout -b {{ .Input.branch }}
        git push origin {{ .Input.branch }}
        {{ end }}

# Teardown steps run AFTER evaluation completes (success or failure).
# This is optional example as we can also leverage sturctured output and not push changes into the repo.
teardown:
  - id: cleanup-fixture
    execution:
      agent: shell
      prompt: |
        {{ if .Input.branch }}
        cd /workspace/repo
        git push origin --delete {{ .Input.branch }} || true
        {{ end }}

# Steps in the target workflow to suppress during eval runs.
suppress_steps:
  - post-comments
```

**Key design choice:** setup and teardown steps use the same `StepDef` syntax as production workflows. This means git operations, API calls, and any sandbox-compatible command work without reimplementation in Go.

---

## Golden Set

**Pure input data** — an array of test case inputs fed to the eval template. No logic, no step definitions. Each entry provides inputs to the eval template and rubrics to evaluate outputs.

```yaml
# tests/eval/golden/pr-review-security.golden.yaml
version: 1
description: "Security-focused PR review test cases"

inputs:
  - id: sql-injection-detection
    description: "PR introducing SQL injection should be flagged"
    # Inputs available to eval template as {{ .Input.<field> }}
    repo_url: "https://github.com/tinkerloft/eval-fixtures"
    commit: "abc123"
    branch: "eval/pr-review-sqli"
    pr_number: "42"
    # Workflow parameters (passed to target workflow as-is)
    parameters:
      repo_url: "https://github.com/tinkerloft/eval-fixtures"
      pr_number: "42"
    # Rubrics — how to evaluate this specific case's outputs
    rubrics:
      - step_id: review
        type: deterministic
        checks:
          - field: "output.findings"
            op: contains_match
            match: { category: "security" }
          - field: "output.approval"
            op: not_equals
            value: "approve"
      - step_id: review
        type: llm_judge
        prompt: |
          The PR introduces an unsanitized SQL query in handlers/users.go.
          The review should identify the SQL injection vulnerability and
          suggest parameterized queries. Rate compliance 0 to 5.
        threshold: 4

  - id: xss-detection
    description: "PR introducing XSS should be flagged"
    repo_url: "https://github.com/tinkerloft/eval-fixtures"
    commit: "def456"
    pr_number: "44"
    parameters:
      repo_url: "https://github.com/tinkerloft/eval-fixtures"
      pr_number: "44"
    rubrics:
      - step_id: review
        type: deterministic
        checks:
          - field: "output.findings"
            op: contains_match
            match: { category: "security" }
      - step_id: review
        type: llm_judge
        prompt: |
          The PR renders user input without escaping in a template.
          The review should identify the XSS vulnerability. Rate 0 to 5.
        threshold: 4
```

---

## Eval Suite

The **binding file** — references an eval template and one or more golden sets, plus configuration overrides. This is what the Go test driver reads as its entry point.

```yaml
# tests/eval/suites/pr-review-full.suite.yaml
version: 1
id: pr-review-full
description: "Full PR review eval — security + style golden sets"

eval_template: pr-review.eval.yaml

golden_sets:
  - pr-review-security.golden.yaml
  - pr-review-style.golden.yaml

config:
  timeout: 15m
  max_parallel: 3
  model: claude-sonnet-4-5-20250514
  llm_judge_model: claude-haiku-4-5-20251001
```

**Quick suite** (subset for smoke testing):

```yaml
# tests/eval/suites/pr-review-quick.suite.yaml
version: 1
id: pr-review-quick
description: "Quick PR review smoke — 2 cases only"

eval_template: pr-review.eval.yaml
golden_sets:
  - pr-review-security.golden.yaml

config:
  timeout: 5m
  max_parallel: 1
  max_cases: 2                          # only run first 2 inputs per golden set
```

**Nightly suite** (combines multiple eval templates):

```yaml
# tests/eval/suites/nightly.suite.yaml
version: 1
id: nightly
description: "Nightly regression — all workflow evals"

evals:
  - eval_template: pr-review.eval.yaml
    golden_sets:
      - pr-review-security.golden.yaml
      - pr-review-style.golden.yaml
  - eval_template: auto-debt-slayer.eval.yaml
    golden_sets:
      - ads-known-bugs.golden.yaml
  - eval_template: triage.eval.yaml
    golden_sets:
      - triage-issues.golden.yaml

config:
  timeout: 30m
  max_parallel: 5
```

---

## Go Test Driver

The driver reads suite files as its entry point — generic, works for any suite without per-workflow code:

#TODO: Verify and review code example here: 

```go
//go:build eval

func TestEvalSuites(t *testing.T) {
    suites := loadAllSuites(t, "suites/*.suite.yaml")
    client := eval.NewClient(apiURL(t), apiToken(t))

    for _, suite := range suites {
        t.Run(suite.ID, func(t *testing.T) {
            for _, evalRef := range suite.Evals {
                tmpl := loadEvalTemplate(t, evalRef.EvalTemplate)
                for _, gsPath := range evalRef.GoldenSets {
                    gs := loadGoldenSet(t, gsPath)
                    t.Run(gs.Description, func(t *testing.T) {
                        for i, input := range gs.Inputs {
                            if suite.Config.MaxCases > 0 && i >= suite.Config.MaxCases {
                                break
                            }
                            t.Run(input.ID, func(t *testing.T) {
                                t.Parallel()
                                runEvalCase(t, client, tmpl, input, suite.Config)
                            })
                        }
                    })
                }
            }
        })
    }
}

func runEvalCase(t *testing.T, c *eval.Client, tmpl EvalTemplate, input GoldenInput, cfg Config) {
    t.Helper()
    ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
    defer cancel()

    // 1. Setup (workflow engine runs setup steps from eval template)
    if len(tmpl.Setup) > 0 {
        setupID := c.RunSetupSteps(t, ctx, tmpl.Setup, input)
        t.Cleanup(func() {
            c.RunTeardownSteps(t, context.Background(), tmpl.Teardown, input)
        })
        c.PollRun(t, ctx, setupID)
    }

    // 2. Run target workflow with suppress_steps
    runID := c.StartRun(t, ctx, tmpl.WorkflowID, eval.RunOpts{
        Parameters:    input.Parameters,
        SuppressSteps: tmpl.SuppressSteps,
        Model:         cfg.Model,
    })
    result := c.PollRun(t, ctx, runID)
    require.Equal(t, "complete", string(result.Run.Status))

    // 3. Evaluate rubrics from golden set input
    for _, rubric := range input.Rubrics {
        stepOutput := findStepOutput(t, result, rubric.StepID)
        evaluateRubric(t, ctx, stepOutput, rubric, cfg)
    }
}
```

**Running evals:**

```bash
# Run all suites
go test -tags eval -v -timeout 30m ./tests/eval/...

# Run only the quick PR review suite
go test -tags eval -v -run TestEvalSuites/pr-review-quick ./tests/eval/...

# Run nightly suite
go test -tags eval -v -timeout 60m -run TestEvalSuites/nightly ./tests/eval/...

# Via Makefile
make evals                              # all suites
make evals-quick                        # quick suites only
```

---

## Step Suppression (Platform Change Required)

The eval template declares `suppress_steps` — step IDs in the target workflow that should be skipped during eval runs. This requires a platform change.

**Recommended approach:** `suppress_steps` field on `POST /api/runs`

```json
POST /api/runs
{
  "workflow_id": "pr-review",
  "parameters": { "repo_url": "...", "pr_number": "42" },
  "suppress_steps": ["post-comments"]
}
```

The DAG engine checks `suppress_steps` before executing each step. Suppressed steps are marked `skipped` with reason `eval_suppressed`. Downstream steps that depend on suppressed steps still receive prior step outputs — only the suppressed step's execution is skipped.

**Why not alternatives:**

- Condition override (`eval_mode` parameter + `{{ not .Params.eval_mode }}` on each step) — pollutes production workflow YAML with eval concerns, easy to forget on new workflows
- Step-level `eval.suppress: true` annotation — mixes eval concerns into workflow definition, still needs an `eval_mode` flag to activate

---

## Rubric Types

### Deterministic (fast, free)

```yaml
rubrics:
  - step_id: review
    type: deterministic
    checks:
      - field: "output.approval"
        op: not_equals
        value: "approve"
      - field: "output.findings"
        op: contains_match
        match: { category: "security" }
      - field: "output.summary"
        op: min_length
        value: 200
```

**Supported operators:** `equals`, `not_equals`, `contains`, `not_contains`, `one_of`, `contains_match`, `min_length`, `max_length`, `gt`, `lt`, `regex`.

### LLM Judge (nuanced, ~$0.2/call)

```yaml
rubrics:

  - step_id: review
    type: llm_judge
    prompt: |
      The PR introduces an unsanitized SQL query. The review should
      identify the vulnerability and suggest parameterized queries.
      Rate compliance from 0 to 5.
    threshold: 4
```

Uses the `Judge` helper from the eval framework PRD. Model configurable per-suite via `config.llm_judge_model`.

### Extensibility

Rubric types are implemented as a Go interface:

```go
type RubricEvaluator interface {
    Evaluate(ctx context.Context, stepOutput map[string]any, rubric RubricDef) (RubricResult, error)
}

type RubricResult struct {
    Pass      bool
    Score     *int       // nil for deterministic (pass/fail only)
    Reasoning string     // empty for deterministic
    Details   []CheckResult
}
```

New rubric types (e.g., `regex_match`, `json_schema`, `similarity`) can be added by implementing the interface and registering in a type map.

---

## Alternatives Considered

Four alternative approaches were evaluated before arriving at the four-file architecture. Full analysis with code examples, pros/cons, and comparison matrix: **[Design Options Explored](./2026-04-10-multi-step-eval-orchestration-alternatives.md)**.


| Option                                                                                                                                               | Core idea                                                                              | Why not                                                                                                               |
| ---------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| **[A: Wrapper Workflow](./2026-04-10-multi-step-eval-orchestration-alternatives.md#option-a-eval-wrapper-workflow)**                                 | Eval is a separate workflow YAML that nests the target via a new `run_workflow` action | High platform complexity (nested workflows), golden set data embedded in YAML params, Temporal child workflow opacity |
| **[B: Go Test Driver](./2026-04-10-multi-step-eval-orchestration-alternatives.md#option-b-go-test-driver-with-golden-set-files)**                    | Go tests read golden YAML, call REST API, setup/teardown in Go helpers                 | Setup/teardown reimplemented in Go instead of reusing workflow engine step syntax                                     |
| **[C: Annotated YAML](./2026-04-10-multi-step-eval-orchestration-alternatives.md#option-c-eval-annotated-workflow-yaml)**                            | `eval` section embedded in production workflow YAML                                    | Pollutes workflow definitions, awkward with 15+ test cases, breaks separation of concerns                             |
| **[D: Standalone Schema](./2026-04-10-multi-step-eval-orchestration-alternatives.md#option-d-standalone-eval-schema-files-recommended-exploration)** | Dedicated eval YAML with golden set data inline                                        | Golden set not purely data — mixed with eval orchestration. Same setup reimplementation issue as B                    |


The four-file architecture combines the best of A (workflow steps for setup/teardown) and D (separate golden set files) while adding the suite layer for composability. See also the [step suppression approaches](./2026-04-10-multi-step-eval-orchestration-alternatives.md#cross-cutting-concern-step-suppression) and [comparison matrix](./2026-04-10-multi-step-eval-orchestration-alternatives.md#comparison-matrix) in the alternatives document.