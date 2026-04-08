# Eval Framework: Pure Go Agent Output Testing

**Date:** 2026-04-08
**Status:** Draft
**Decision Record:** [ADR-001: Eval Framework](./2026-04-08%20eval-framework.md)

---

## Background

FleetLift workflows produce PR comments, artifacts, structured JSON output, and GitHub interactions. There is no systematic way to evaluate whether these outputs meet quality expectations. Manual spot-checking does not scale across ~90 tests (1 suite per workflow template, ~10 suites, 3-15 tests each), run ad-hoc or weekly.

The [previous ADR](./2026-04-08%20eval-framework.md) evaluated five approaches (custom Python, DeepEval, Ragas, Promptfoo, pure Go) and selected **Go `testing` + a custom LLM-as-judge helper** using `anthropic-sdk-go` and `go-github/v62`, both already in `go.mod`. Zero new dependencies, full `internal/` access, native async support. 
This document is focused on the implementation details.

---

## Goals

- Deterministic assertions + LLM-as-judge scoring for every workflow template
- Run via `go test` — works with existing IDE, debugger, coverage tooling
- Gated via `//go:build eval` tag — never fires on regular `go test ./...`
- Two-layer test pattern: fast/free deterministic checks first, then nuanced LLM judge
- Retrieve agent outputs from workflow run results (REST API), not directly from GitHub

## Non-Goals (v1)

- Web dashboard for result comparison (use `go test -v` + CI logs)
- Automated regression detection / score trending over time
- Pre-built metric library (we write judge rubrics ourselves)
- GEval-style automatic rubric generation
- CI integration (GitHub Actions workflow, scheduled runs)

---

## Architecture

```
tests/eval/
  pr_review_test.go          # per-template test suite
  bug_fix_test.go
  triage_test.go
  doc_assessment_test.go
  helpers_test.go            # shared test fixtures + polling helper
  ...

internal/eval/
  judge.go                   # LLM-as-judge helper (~100 LOC)
  run.go                     # workflow run output fetching + polling (~100 LOC)
```

Each test follows a two-layer pattern:

```
Layer 1 — Deterministic (fast, free)
  keyword presence, forbidden content, length bounds,
  structured output field validation via testify

Layer 2 — LLM Judge (nuanced, ~$0.02/test)
  Claude evaluates quality against a plain-English rubric
  returns structured score + reasoning
```

---

## Component Design

### `internal/eval/judge.go` — LLM-as-Judge Helper

Accepts a plain-English rubric and agent output, calls Claude, returns a structured score.

```go
// JudgeResult holds the LLM judge's evaluation of agent output.
type JudgeResult struct {
    Score     int    `json:"score"`     // 1-5
    Reasoning string `json:"reasoning"`
    Pass      bool   // score >= threshold
}

// JudgeOption configures a Judge call.
type JudgeOption func(*judgeConfig)

func WithThreshold(n int) JudgeOption          // default: 3
func WithModel(model string) JudgeOption       // default: claude-sonnet
func WithContext(key, value string) JudgeOption // additional context fields

// Judge evaluates output against rubric using Claude as an LLM judge.
// Returns a structured score with reasoning. Uses anthropic-sdk-go with
// JSON mode — no regex parsing of LLM responses.
func Judge(ctx context.Context, rubric, output string, opts ...JudgeOption) (*JudgeResult, error)
```

The prompt template instructs Claude to:
1. Read the rubric criteria
2. Evaluate the output against each criterion
3. Return a JSON object with `score` (1-5) and `reasoning`. The tests can be extended with other metrics in the future.

---

### `internal/eval/run.go` — Workflow Run Output Fetching

Eval tests retrieve agent outputs from FleetLift's REST API rather than directly from GitHub. This uses the existing endpoints:

- `GET /api/runs/{id}` — run details with all step runs (including `Output`, `Diff`, `PRUrl`)
- `GET /api/runs/{id}/output` — structured outputs from all completed steps

```go
// Client wraps the FleetLift REST API for eval test usage.
type Client struct {
    base   string
    token  string
    client *http.Client
}

// NewClient returns an eval client targeting the given FleetLift API URL.
func NewClient(baseURL, token string) *Client

// RunResult holds a completed run with its step outputs.
type RunResult struct {
    Run   model.Run       `json:"run"`
    Steps []model.StepRun `json:"steps"`
}

// GetRun fetches a run and all its step runs by ID.
func (c *Client) GetRun(ctx context.Context, runID string) (*RunResult, error)

// StepOutput is a step_id → output pair from GET /api/runs/{id}/output.
type StepOutput struct {
    StepID string         `json:"step_id"`
    Output map[string]any `json:"output"`
}

// GetOutputs fetches structured outputs for all completed steps of a run.
func (c *Client) GetOutputs(ctx context.Context, runID string) ([]StepOutput, error)

// PollRun polls until the run reaches a terminal status or the context expires.
// Returns the final RunResult. Callers should use context.WithTimeout to bound the wait.
func (c *Client) PollRun(ctx context.Context, runID string, interval time.Duration) (*RunResult, error)
```

This approach:
- Tests against the same interface operators use — validates the full pipeline
- Handles async execution via `PollRun` with context-based timeout (idiomatic Go)
- Reuses existing `model.Run` and `model.StepRun` types from `internal/model/`
- Works for all workflow types (PR comments, artifacts, structured output, diffs)

---

### `tests/eval/` — Test Suites

All eval test files use a `//go:build eval` build tag — the idiomatic Go mechanism for gating expensive tests. Unlike env var checks, build tags are enforced by the compiler: `go test ./...` won't even compile eval files.

```go
//go:build eval

package eval_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/tinkerloft/fleetlift/internal/eval"
)

func TestPRReviewQuality(t *testing.T) {
    t.Parallel()

    client := eval.NewClient(apiURL(t), apiToken(t))
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()

    result, err := client.PollRun(ctx, runID, 5*time.Second)
    require.NoError(t, err)

    reviewStep := findStep(t, result.Steps, "review")
    body := stepOutputString(t, reviewStep, "comment_body")

    // Layer 1: deterministic assertions
    assert.Contains(t, body, "## Security")
    assert.NotContains(t, body, "LGTM")
    assert.Greater(t, len(body), 200)

    // Layer 2: LLM judge
    judge, err := eval.Judge(ctx,
        "The review should identify at least one security concern "+
            "and provide a concrete fix suggestion with a code example.",
        body,
    )
    require.NoError(t, err)
    assert.True(t, judge.Pass, "judge score %d/5: %s", judge.Score, judge.Reasoning)
}

// apiURL returns the FleetLift API URL from FLEETLIFT_API_URL or skips the test.
func apiURL(t *testing.T) string {
    t.Helper()
    u := os.Getenv("FLEETLIFT_API_URL")
    if u == "" {
        t.Skip("FLEETLIFT_API_URL not set")
    }
    return u
}
```

**Go best practices applied:**
- `//go:build eval` — compiler-enforced gating, not runtime env checks
- `t.Parallel()` — suites run concurrently where safe
- `t.Helper()` — on all helper functions for accurate test failure line numbers
- `context.WithTimeout` — bounded waits, no unbounded polling
- `require` for fatal preconditions, `assert` for non-fatal checks
- Table-driven subtests for parameterised test cases (e.g., multiple rubrics per workflow)

Suites planned for v1:

| Suite | Template | Tests | Notes |
|-------|----------|-------|-------|
| `pr_review_test.go` | pr-review, pr-review-multi | ~15 | Comment structure, finding quality, inline annotations |
| `triage_test.go` | triage | ~10 | Label accuracy, priority assignment, summary quality |
| `bug_fix_test.go` | bug-fix | ~10 | Fix correctness, PR quality, test coverage |
| `doc_assessment_test.go` | doc-assessment | ~10 | Score calibration, finding specificity |
| `sandbox_test.go` | sandbox-test | ~5 | Output passing, step chaining |
| `clone_test.go` | clone-test | ~5 | Clone verification, branch creation |
| Remaining templates | ~5 more suites | ~35 | As templates stabilise |

---

## Commands

```bash
# Run all eval tests (build tag required)
go test -tags eval -v -timeout 30m ./tests/eval/...

# Run a single suite
go test -tags eval -v -timeout 10m ./tests/eval/ -run TestPRReview

# Via Makefile
make evals

# Build (verify compilation — excludes eval files without the tag)
go build -buildvcs=false ./...

# Unit tests (excludes evals — no -tags eval)
go test -buildvcs=false ./...
```

Makefile target:
```makefile
evals:
	go test -tags eval -v -timeout 30m ./tests/eval/...
```

---

## Credential & Environment Requirements

| Name | Purpose |
|------|---------|
| `ANTHROPIC_API_KEY` | LLM judge calls via `anthropic-sdk-go` |
| `FLEETLIFT_API_URL` | FleetLift API base URL for run output fetching |
| `FLEETLIFT_API_TOKEN` | Bearer token for FleetLift API auth |

Tests skip gracefully (via `t.Skip`) if required env vars are absent.

---

## Boundaries

- **Always:** use `//go:build eval` on every eval test file; use the two-layer pattern (deterministic first, then judge); use `t.Helper()` on helper functions; use `context.WithTimeout` for all polling; run `go build ./...` and `go test ./...` before committing eval code
- **Ask first:** adding a new dependency beyond `anthropic-sdk-go`; changing the judge prompt template; adding a third evaluation layer
- **Never:** omit the build tag on eval test files; hardcode API keys or tokens; commit test fixtures containing real PR content without redaction

---

## Success Criteria

- [ ] `go test -tags eval -v ./tests/eval/...` runs all suites and produces pass/fail results
- [ ] `go test ./...` (without `-tags eval`) compiles and runs with zero eval-related test execution
- [ ] At least 3 workflow template suites implemented with both deterministic and judge layers
- [ ] `make evals` target works
- [ ] LLM judge returns structured `JudgeResult` with score, reasoning, and pass/fail
- [ ] Run output polling handles async workflow execution with context-based timeout
- [ ] Total framework code (excluding test suites) is under 300 LOC

---

## Open Questions

1. Creation of golden set PRs for the review.
2. How to include Miro Claude Code PR review into the comparation? Maybe add github_commit and github_post_comment actions?
3. **Adressing flaky tests** — LLM judge scores are non-deterministic.
4. **Eval for fan-out workflows** — doc-assessment and pr-review-multi fan out across repos/personas. Should eval tests cover a single child, or assert on the aggregated synthesis output?

---

## Priority

| # | Component | Effort | Impact | Priority |
|---|-----------|--------|--------|----------|
| 1 | LLM-as-judge helper (`judge.go`) | Low (~100 LOC) | High — core scoring primitive | P1 |
| 2 | Run output fetching + polling (`run.go`) | Low (~100 LOC) | High — enables async output testing | P1 |
| 3 | Test suite structure + first 3 suites | Medium | High — proves the framework works | P1 |

Requirements 1-3 are the minimal set to write and run eval suites. Total estimated code: ~630 LOC including first test suites.
