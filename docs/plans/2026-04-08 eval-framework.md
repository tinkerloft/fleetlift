# ADR-001: Pure Go Eval Framework for AI Workflow Testing

## Date

2026-04-08

## Context

FleetLift is a Go/Temporal DAG workflow orchestrator with Claude Code agents that post PR comments, create artifacts, and interact via GitHub API. We need to evaluate agent output quality across ~90 tests (1 suit per template ~ 10 suites, ~3-15 tests each), run ad-hoc or weekly.

Requirements:

- Gather agent outputs from GitHub PR comments, artifacts, and REST APIs
- Deterministic assertions + LLM-as-judge scoring
- Minimal operational friction in a Go codebase
- No new language toolchains

We evaluated five approaches from three perspectives (critical architect, pragmatic staff engineer, value-to-effort PM):

1. Custom Python framework (like Miro Digital Twin's ~2000 LOC eval suite)
2. DeepEval (Python, pytest-based, 60+ metrics, GEval)
3. Ragas (Python, RAG-focused evaluation)
4. Promptfoo (YAML-driven, language-agnostic, Go binary provider support)
5. Pure Go with `go test` + LLM-as-judge helper

## Decision

Use **Go `testing` + a custom LLM-as-judge helper** (~200 LOC) using `anthropic-sdk-go` and `go-github/v62`, both already in the project.

Architecture:

- `internal/eval/judge.go` — LLM-as-judge helper: structured prompt → Claude → JSON score + reasoning
- `internal/eval/github.go` — GitHub PR comment fetching + async polling
- `tests/eval/` — Test suites gated behind `GOEVALS=1` env var
- CI runs evals on weekly schedule or on-demand via `[run-evals]` commit message tag

Each test follows a two-layer pattern:

1. **Deterministic assertions** (fast, free): keyword presence, forbidden content, length checks via `testify`
2. **LLM judge** (nuanced, ~$0.02/test): Claude evaluates quality against a plain-English rubric

## Alternatives Considered

### Promptfoo (YAML + Go binary provider)

Language-agnostic eval platform with YAML test definitions and built-in LLM judge (`llm-rubric`). Used by OpenAI/Anthropic. Acquired by OpenAI (March 2026). $0 OSS, $50/mo team cloud.

**Rejected because:**

- **No async support** — FleetLift agents post PR comments asynchronously. Promptfoo providers must be synchronous, requiring polling workarounds in the provider binary.
- YAML conditional logic limited to Nunjucks templates; complex test logic requires escape to JS/Python.
- Adds **Node.js runtime dependency** to a backend Go project.
- **Post-acquisition uncertainty** — OpenAI committed to keeping OSS open, but long-term maintenance priority for standalone tool is unclear.
- Go provider **cannot import** `internal/` **packages** — FleetLift's code lives almost entirely in `internal/`. Would need code duplication or a thin HTTP API layer.

### DeepEval (Python)

Mature eval framework with 60+ metrics, GEval (1-line LLM judge), Confident AI dashboard. 13k+ GitHub stars, 3M+ monthly PyPI downloads.

**Rejected because:**

- **Python in a Go project** — requires separate virtualenv, pip, Python runtime in CI pipeline.
- Team context-switching between Go and Python test code.
- Dependency fragmentation: `requirements.txt` (or potery analog) alongside `go.mod`.
- **Would be the top choice if FleetLift were a Python project.** GEval's 1-line LLM judge and source-agnostic `LLMTestCase` are excellent.

### DeepEval in separate repo

Isolate Python friction in a dedicated `fleetlift-evals` repo.

**Rejected because:**

- Can't access FleetLift's `internal/` packages for test setup, Temporal client, GitHub client.
- Adds repo management overhead for a small team.
- Cross-repo CI coordination complexity.

### Ragas (Python)

RAG-focused evaluation framework with Faithfulness, Context Precision, Answer Relevancy metrics.

**Rejected because:**

- **Design mismatch** — core metrics assume a retrieval+generation pipeline. FleetLift workflows are agent orchestration + artifact creation. "Context precision" doesn't map to PR comment quality.
- Same Python friction as DeepEval with worse fit for the use case.
- No GEval equivalent — custom metrics require heavy dataclass boilerplate.

### Custom Python framework (Digital Twin approach)

Miro Digital Twin has a mature ~2000 LOC eval framework with hybrid evaluation (assertions + LLM judge via Strands Agent), Braintrust integration, answer-type-driven scoring.

**Rejected because:**

- **Overkill** — 2000 LOC framework for 90 weekly tests.
- Python-only, tightly coupled to HTTP gathering against a specific FastAPI service.
- Requires Strands Agent (Bedrock), Braintrust, CoreAI API — infrastructure FleetLift doesn't use.
- Answer-type-driven scoring (6 types) is not needed.

## Consequences

### Positive

- **Zero new dependencies** — uses `anthropic-sdk-go` and `go-github/v62` already in `go.mod`
- **Full access to `internal/` packages** for test setup, Temporal client, database fixtures
- **Native `go test` integration** — works with existing CI, IDE, debugger, and coverage tools
- **No cross-language toolchain friction** — team stays in Go
- **~630 LOC total** (vs ~2000 for custom Python framework)
- **Team owns everything** — no framework maintenance surprises or acquisition risk
- **Async-friendly** — Go tests can directly use goroutines + channels to poll for async agent outputs

### Negative

- **No pre-built metric library** (vs DeepEval's 60+ metrics) — we write judge prompts ourselves
- **No web dashboard** for result comparison — use `go test -v` output + CI logs
- **No built-in regression detection** — track scores over time manually or build a simple comparator
- **LLM-as-judge quality depends on our prompt engineering** — no GEval-style automatic rubric generation
- **LLM API costs** — ~$0.02/test × 90 tests = ~$1.80/run (negligible at weekly cadence)

### Trade-off accepted

We trade Python's richer eval ecosystem and Promptfoo's YAML convenience for Go-native simplicity, full `internal/` access, and zero operational friction. At our scale (90 tests, weekly), this is the right trade-off. If eval needs grow significantly (500+ tests, real-time monitoring, team dashboard), we can revisit Promptfoo or build a lightweight results DB.