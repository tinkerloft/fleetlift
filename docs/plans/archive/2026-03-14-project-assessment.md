# FleetLift Critical Project Assessment

**Date:** 2026-03-14
**Status:** Assessment

## Executive Summary

FleetLift is a well-architected agent workflow platform with strong security foundations and excellent Temporal workflow design. The core execution engine — DAG orchestration, sandbox isolation, HITL signals — is production-quality code. The weaknesses cluster around operational maturity (observability, health checks, migration tooling), frontend-backend contract consistency, and incomplete feature wiring rather than fundamental design problems.

**Overall: strong bones, needs finishing work.**

---

## What Works Well

### 1. Temporal Workflow Implementation — Excellent

The workflow engine is the strongest part of the codebase. Every Temporal determinism rule is followed correctly:

- `workflow.GetLogger(ctx)` used instead of `slog`/`fmt` throughout `internal/workflow/`
- Map iteration is collected to sorted slices before executing activities (`dag.go:97-100`)
- No `time.Now()`, `math/rand`, or other non-deterministic calls in workflow code

The DAG scheduler (`dag.go:360-377`) correctly identifies ready steps, returns deterministically sorted results, and detects deadlocks when no steps are ready but pending steps remain.

**Fan-out is well-designed.** Indexed child workflow IDs (`{runID}-{stepID}-{index}`), proper aggregation of results, and a documented guard that overrides HITL approval policy on fan-out steps to prevent signal routing hangs (`dag.go:272-278`).

**Error handling is thorough.** `NewDisconnectedContext()` ensures cleanup runs even after cancellation. Sandbox leak prevention iterates all provisioned sandboxes in a deferred block. Failed knowledge capture warns but doesn't fail the step.

### 2. Security Model — Strong

The codebase consistently follows its own security rules:

- **URL validation**: Only `https://` scheme allowed for repos, with test coverage for `file://`, `git://`, `ssh://` rejection (`execute_test.go:44-50`)
- **Credential naming**: Regex `^[A-Z][A-Z0-9_]{0,63}$` with reserved env var blacklist (PATH, LD_PRELOAD, etc.) — validates all names upfront before any DB calls
- **Shell injection prevention**: `shellQuote()` used on all user-controlled strings in shell commands — repo URLs, branch names, prompts, credential values
- **Encryption**: AES-256-GCM for credentials at rest, 32-byte hex key validated at init
- **CSRF protection**: OAuth state cookies are HttpOnly, SameSite=Lax
- **Multi-tenant isolation**: `teamIDFromRequest()` validates team membership from JWT claims — no implicit team routing via map iteration

### 3. Activity Architecture — Clean Separation

The `Activities` struct with dependency injection (`sandbox.Client`, `*sqlx.DB`, `knowledge.Store`, agent runners) is a clean pattern. Activities are methods on this struct, giving natural access to shared resources.

Credential resolution is done correctly: validate all names upfront, batch query for efficiency, fail fast on any invalid name before touching the database.

### 4. Workflow Template System — Well-Designed

The template registry with priority-based provider chain (builtins < DB) is elegant. Templates use standard Go text/template with useful custom functions (`toJSON`, `truncate`, `join`). The 10 builtin templates cover real use cases — bug fixes, fleet transforms, migrations, audits, incident response, PR review, triage.

YAML is parsed with `yaml.Unmarshal` (not `json.Unmarshal`) as the CLAUDE.md rules require.

### 5. CLI — Good Feature Parity

The CLI covers most backend operations with consistent patterns: table output via `tabwriter`, `--output-json` for scripting, SSE streaming for live logs, browser auto-open for OAuth login. Feature parity with the frontend is good — the only gaps are reports (frontend-only) and workflow creation (CLI-only), both by design.

---

## What Needs Improvement

### 1. Frontend-Backend Contract Consistency — Multiple Mismatches

This is the most impactful set of issues. The frontend and backend have drifted apart in several places:

**Missing endpoint:** Frontend calls `getReportArtifacts()` → `GET /api/reports/{runID}/artifacts` (`web/src/api/client.ts:84`), but this handler doesn't exist. Will 404 at runtime.

**Response format mismatches:**
- `/api/runs/{id}/output` — backend returns an array of `{step_id, output}`, frontend expects a `Record<string, unknown>` object
- `/api/runs/{id}/diff` — backend returns an array of `{step_id, diff}`, frontend expects `{diff: string}`

**Unregistered route:** `WorkflowsHandler.Update()` exists as a handler method, but `PUT /api/workflows/{id}` is never registered in the router. The handler is dead code.

**Error format inconsistency:** Handlers use `http.Error()` (returns `text/plain`), but the frontend expects JSON `{error: "..."}`. Works by accident because the frontend falls back to `res.statusText`, but violates REST conventions and makes error messages less useful.

**Recommendation:** Consider an OpenAPI/Swagger spec as the contract between frontend and backend. Generate types from it. This class of bug is the kind that only shows up at runtime and is easy to prevent structurally.

### 2. Operational Readiness — Missing Fundamentals

The codebase handles the happy path well but lacks the operational scaffolding needed for production:

**No health check endpoint.** No `/health` or `/readiness` for load balancers or Kubernetes probes. The server starts and either works or doesn't — no way for infrastructure to know.

**Inconsistent logging.** The worker uses `slog` with JSON output. The server uses `log.Printf`. These should be unified — structured logging with correlation IDs linking HTTP requests to Temporal workflow executions.

**No request tracing.** No correlation IDs, no distributed tracing (OpenTelemetry). When an agent fails inside a sandbox, there's no trace linking the HTTP request that started the run → the Temporal workflow → the specific activity → the sandbox execution.

**Metrics are scaffolded but empty.** `internal/metrics/` has a Prometheus interceptor registered, but no actual metrics are collected. No counters for runs started/completed/failed, no histograms for step duration, no gauges for active sandboxes.

**No schema migration system.** `internal/db/migrations/001_initial.sql` exists but is never executed. Schema changes are applied by running `schema.sql` directly via `Migrate()`. This is fine for development but will cause data loss in production — there's no `ALTER TABLE` path for existing databases.

### 3. Test Coverage — Adequate Core, Gaps at Boundaries

Test/source ratio is ~31% (2,837 test LOC / 9,164 source LOC). The critical paths are well-tested:

- Auth middleware (5 tests covering token validation, CSRF, cookie handling)
- Workflow DAG scheduling, condition evaluation, fan-out aggregation
- URL scheme enforcement, credential validation
- AES-GCM encryption round-trips

**Critical gaps per CLAUDE.md's own requirements:**

- **OAuth CSRF state validation** — `handlers/auth.go:43-57` handles state comparison but has zero tests. CLAUDE.md explicitly lists this as a must-test area.
- **Multi-tenant isolation** — handlers enforce team_id checks, but no test verifies that User A from Team A can't access Team B's runs.
- **SSE ticket lifecycle** — CLAUDE.md lists `auth/middleware.go` SSE ticket lifecycle as must-test. Tests exist for basic auth but not SSE-specific ticket validation.

**Other notable gaps:**
- No integration tests for full DAGWorkflow + real activities (only unit tests with mocks)
- No tests for the `DEV_NO_AUTH` bypass mode (`router.go:126-147`)
- GitHub and Slack action handlers are stubs — dispatched but mostly no-ops

### 4. Knowledge System — Wired But Not Connected

The CLAUDE.md notes this as a "v1 holdover — needs decision: wire in or remove." The current state is awkward:

- `internal/knowledge/` is fully implemented (DBStore + MemoryStore with complete CRUD)
- Server initializes a knowledge store (`cmd/server/main.go:73-74`)
- But the **worker never populates `KnowledgeStore`** in the Activities struct (`cmd/worker/main.go:62` — field exists, never assigned)
- `ExecuteStep` checks `if a.KnowledgeStore != nil` before enrichment (`execute.go:71`), so it silently skips knowledge features
- No workflow templates use `knowledge.capture: true` or `knowledge.enrich: true`

**This is a feature that's 90% built but 0% used.** Either wire it up (one line in `worker/main.go` + a template that uses it) or remove it to reduce confusion. The MCP design doc proposes replacing the file-based capture with MCP tools, which would be the right time to make this decision.

### 5. Incomplete Feature Implementations

**GitHub activities are stubs.** `internal/activity/github.go` says "TODO: Phase 9." The action dispatcher (`actions.go:16-34`) routes to handlers, but `actionGitHubAssignIssue`, `actionGitHubLabel`, and `actionGitHubPRReview` log and return without doing anything. Templates like `triage.yaml` reference these actions — they'll silently no-op.

**Slack notifications are similar.** The slack activity exists but the actual Slack API integration is minimal.

**Artifact collection is underused.** Only `fleet-research.yaml` declares `outputs.artifacts`. The audit template produces a report but doesn't collect it as an artifact. This suggests the feature works but wasn't prioritized in template design.

### 6. Code Quality Nits

**One panic in production code** (`server/router.go:168`):
```go
fsys, err := fs.Sub(web.DistFS, "dist")
if err != nil {
    panic("web: failed to sub dist: " + err.Error())
}
```
Should return a startup error instead of crashing.

**Duplicate `shellQuote` implementations** in `internal/activity/util.go:10-13` and `internal/agent/quote.go:6-9`. Identical logic, different packages. Should be consolidated to one location.

**Goroutine panic silencing** in `dag.go:346` — if a step goroutine panics, the DAG scheduler logs `continue // goroutine panicked` and skips the result. This should be surfaced as a step failure, not silently dropped.

**`KnowledgePage.tsx` uses raw `fetch()`** instead of the centralized API client that every other page uses. Inconsistent error handling and auth token management.

---

## Risk Assessment

### Low Risk (Code Quality)
The core engine works. Temporal workflows are deterministic. Security rules are followed. The architecture supports the roadmap (multi-agent, MCP, non-agentic steps). These are hard things to retrofit; FleetLift has them from the start.

### Medium Risk (Operational Gaps)
No health checks, no metrics, no tracing, no migration system. These are table-stakes for production but are all additive — they don't require rearchitecting anything. The risk is deploying without them, not the effort to add them.

### Medium Risk (Contract Drift)
Frontend-backend mismatches are the kind of bug that erodes trust. The missing artifacts endpoint, response format discrepancies, and unregistered routes suggest the frontend and backend evolved independently without integration testing. An OpenAPI spec or at least a shared types package would prevent this class of issue.

### Low Risk (Incomplete Features)
GitHub/Slack stubs and unwired knowledge are clearly marked as incomplete. They don't affect the core workflow engine. The MCP proposal provides a natural path to replace the knowledge file convention with something better.

---

## Prioritized Recommendations

### Immediate (Before Production)

1. **Fix frontend-backend contract issues** — implement missing artifacts endpoint, align response formats for output/diff endpoints, register the workflow update route
2. **Add health check endpoint** — `GET /health` returning 200 with basic status
3. **Wire knowledge store in worker** or remove the dead code path — the current half-wired state is confusing
4. **Fix the panic** in `router.go:168` — return error instead
5. **Add OAuth CSRF tests** — CLAUDE.md requires this

### Short-Term (Operational Readiness)

6. **Unify logging** — slog everywhere, with request correlation IDs
7. **Add schema migration system** — use golang-migrate or atlas instead of raw `schema.sql`
8. **Add missing database indexes** — `step_runs.run_id`, `knowledge_items.workflow_template_id`
9. **Add multi-tenant isolation tests** — verify cross-team access is denied
10. **Standardize error responses** — JSON `{"error": "..."}` from all handlers

### Medium-Term (Feature Completion)

11. **Implement GitHub/Slack integrations** — the stubs are referenced by templates
12. **Add artifact collection to more templates** — audit and migration should collect reports
13. **Add metrics collection** — run counts, step durations, active sandboxes, error rates
14. **Consolidate duplicate code** — `shellQuote`, potentially shared types between frontend/backend
15. **Add data retention/archival** — runs table will grow unbounded

### Long-Term (Platform Maturity)

16. **OpenAPI spec** — generate frontend types and backend validation from a single source
17. **Distributed tracing** — OpenTelemetry from HTTP handler through Temporal to sandbox
18. **Integration test suite** — full workflow execution with real (or containerized) Temporal and PostgreSQL
19. **MCP agent interface** — per the design doc, starting with read-only context tools

---

## Scorecard

| Dimension | Rating | Summary |
|-----------|--------|---------|
| **Architecture** | 9/10 | Clean separation: DAG orchestration, sandbox isolation, pluggable runners, template system. Supports the roadmap. |
| **Temporal Workflows** | 9/10 | Determinism rules followed perfectly. Fan-out, HITL, sandbox groups all work. |
| **Security** | 8/10 | Strong perimeter (URL validation, encryption, CSRF, tenant isolation). Missing: audit logging, production env guards. |
| **Code Quality** | 8/10 | Consistent patterns, good error handling. Minor: one panic, duplicate functions, silent goroutine failures. |
| **Test Coverage** | 6/10 | Critical paths covered. Gaps: OAuth CSRF, multi-tenant isolation, SSE lifecycle — all listed as must-test in CLAUDE.md. |
| **Frontend** | 7/10 | Modern React 19 + TypeScript. API client is clean. Missing: error boundaries, contract alignment with backend. |
| **Operations** | 5/10 | Graceful shutdown works. Missing: health checks, metrics, tracing, consistent logging, migration system. |
| **Feature Completeness** | 6/10 | Core engine complete. Knowledge half-wired. GitHub/Slack stubs. Artifact collection underused. |
| **Documentation** | 7/10 | CLAUDE.md, ARCHITECTURE.md, WORKFLOW_REFERENCE.md are solid. Some staleness in README. |

**Overall: 7/10** — strong foundation that needs operational polish and feature completion before production use at scale.
