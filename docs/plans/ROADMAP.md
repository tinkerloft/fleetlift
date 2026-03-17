# FleetLift Roadmap

**Date:** 2026-03-14
**Status:** Active (Tier 1 complete, Tier 2 C+D+H done — ready for Tier 3)

FleetLift has strong architecture and a working core engine. This roadmap covers what's needed to go from "internal tool" to "production-ready OSS product."

---

## Tier 1: Production Readiness ✅

Fix broken contracts, remove dead code, add missing tests. **Complete — all tasks done and verified.**

See: [`2026-03-14-tier1-production-readiness.md`](2026-03-14-tier1-production-readiness.md) for the full implementation plan.

### Track A: Fix What's Broken ✅

| # | Item | Files | Status |
|---|------|-------|--------|
| 1 | Fix `spaHandler` panic → return error | `router.go` | ✅ |
| 2 | Add `GET /health` endpoint | `router.go` | ✅ |
| 3 | Standardize error responses to JSON (`writeJSONError`) | All handler files | ✅ |
| 4 | Fix frontend-backend contract mismatches (diff, output, artifacts) | `runs.go`, `reports.go`, `client.ts` | ✅ |
| 5 | Consolidate duplicate `shellQuote` → `internal/shellquote/` | `activity/`, `agent/` | ✅ |
| 6 | Surface goroutine panics in DAG as step failures | `dag.go` | ✅ |
| 7 | Remove dead knowledge-capture code from worker/activities | `activities.go`, `execute.go`, `step.go`, `knowledge.go` | ✅ |

### Track B: Test Coverage Gaps ✅

| # | Item | Files | Status |
|---|------|-------|--------|
| 8 | OAuth CSRF state validation tests | `handlers/auth_test.go` | ✅ |
| 9 | Multi-tenant isolation tests | `handlers/isolation_test.go` | ✅ |
| 10 | SSE event stream auth guard tests | `handlers/runs_test.go` | ✅ |

---

## Tier 2: Product Quality

Makes FleetLift feel finished. Tracks C and D can run in parallel.

### Track C: Web Experience ✅

Merges the web interface enrichment remaining phases, visual polish plan, profile menu, and inbox notifications into a single track. **Complete — all phases implemented. See code review follow-ups below.**

See: [`2026-03-14-track-c-web-experience.md`](2026-03-14-track-c-web-experience.md) for the full implementation plan.

| Phase | Item | Status |
|-------|------|--------|
| C1 | DAG Graph Overhaul — custom nodes, smoothstep edges, dynamic height | ✅ |
| C2 | Run Detail — live duration, progress bar, step timeline, diff/JSON coloring | ✅ |
| C3 | Workflow Pages — category colors, icons, CodeMirror YAML, alphabetical sort | ✅ |
| C4 | Global Polish — skeleton loaders, empty states, enhanced badges | ✅ |
| C5 | Profile Menu — /api/me enrichment, Radix dropdown, UserMenu | ✅ |
| C6 | Inbox Enhancements — filter tabs, inline approve/reject/steer | ✅ |
| C7 | Enhanced Components — DiffViewer, JsonViewer, StepTimeline | ✅ |
| C8 | System Health Page — placeholder UI (backend endpoint deferred) | ✅ |

#### Code Review Follow-ups (pre-merge) — ✅ All resolved
- [x] **P1:** Fix XSS in `JsonViewer.tsx` — HTML-escape before regex colorization
- [x] **P1:** Add `rows.Err()` check in `auth.go` `HandleMe` after SQL iteration
- [x] **P2:** Log DB errors on user profile lookup in `HandleMe`
- [x] **P2:** Use `replaceAll('_', ' ')` in `StatusBadge` and `StepTimeline`
- [x] **P2:** Track EventSource connection state in `LogStream` for streaming indicator
- [x] **P2:** Clear steer text when switching inbox items
- [x] **P2:** Mark SystemHealth as placeholder (no hardcoded green badges)
- [x] **P2:** Add component-level tests for StatusBadge, DiffViewer, UserMenu

### Track H: Workflow Engine Reliability — ✅ Complete

The PR Review workflow exposed 8 platform-level bugs and 2 template-level bugs during live testing. 80% were platform issues that would affect every workflow. This track hardens the engine so users can compose workflows from activities that work reliably in isolation.

**Motivation:** Currently, every new workflow requires manual end-to-end debugging against a live stack. The goal is: a user writes YAML, validation catches mistakes before execution, activities have clear contracts, and failures are loud and actionable.

#### Infrastructure Hardening (2026-03-14, PR #28)

Bounded retries, buffer sizes, and error propagation across the workflow/activity layer:

- All external-state activities now have explicit `RetryPolicy` with `MaximumAttempts` (ProvisionSandbox: 3, ExecuteAction: 2, CreatePullRequest: 2, CleanupSandbox: 3, VerifyStep: 2)
- `finalizeStep()` returns errors instead of silently swallowing DB failures
- `aggregateFanOut()` handles nil goroutine results as failures instead of dropping them
- CLI SSE scanner uses 4MB buffer (matches agent output buffer)
- Slack/action activities return errors instead of `return nil` on failure
- Sandbox creation validates non-empty sandbox ID
- New tests: `dag_test.go` (fan-out nil handling), `step_test.go` (provision/PR/finalize failure propagation), `client_test.go` (sandbox ID validation)

#### Runtime Bugs Fixed (2026-03-14)

| Bug | Root Cause | Fix | Scope |
|-----|-----------|-----|-------|
| OpenSandbox `workdir` ignored | Wrong JSON field (`workdir` → `cwd`) | `opensandbox/client.go` | Platform |
| `bufio.Scanner: token too long` | Default 64KB buffer; agent output exceeds it | 4MB scanner buffer | Platform |
| Failed runs show "completed" | `DAGWorkflow` returned `nil` on step failure | Return collected errors | Platform |
| Steps run before deps complete | `json:` tags on `WorkflowDef` broke Temporal replay | Revert tags; use raw YAML for API | Platform |
| Template crash on skipped deps | `resolveStep` called before checking dep status | Skip propagation before render | Platform |
| `/api/inbox` 500 | Nullable DB columns scanned into `string` | Use `*string` for nullable fields | Platform |
| Action steps silently skip | `return nil` when `GITHUB_TOKEN` missing | Return error; fail loudly | Platform |
| Redundant clone in shared sandbox | No check for existing `.git/HEAD` | Skip clone if already present | Platform |
| Review step wrong workdir | Step missing `repositories` declaration | Add repos; skip-if-cloned logic | Template |
| Post-comments empty summary | Wrong output key in template | Template-specific fix | Template |

#### Reliability Work Items

| # | Item | Key Files | Priority | Status |
|---|------|-----------|----------|--------|
| H1 | **Workflow validation before execution** — check dep references, circular deps, template step/param refs, unknown action types, unreachable steps | `workflow/validate.go` (new) | P0 | ✅ |
| H2 | **Action step credential access + logging** — action steps get credential store access (not just worker env), log what they do, store results as step output | `activity/actions.go`, `workflow/dag.go` | P0 | ✅ |
| H3 | **Output schema enforcement** — extract agent output into declared schema fields, fail if required fields missing; downstream steps get predictable data | `activity/execute.go`, `workflow/dag.go` | P0 | ✅ |
| H4 | **Workflow integration test harness** — Temporal test environment + mock sandbox/agent; validate DAG orchestration, template rendering, credential flow, action dispatch for each builtin workflow | `workflow/dag_integration_test.go` (new) | P1 | ✅ |
| H5 | **Activity/action contract registry** — declared input/output schemas per action type; enables validation at parse time and future UI workflow builder | `model/action_contract.go`, `handlers/actions.go` | P1 | ✅ |
| H6 | **Template rendering safety** — validate all referenced step IDs and output keys exist before rendering; clear error messages ("step 'revew' does not exist, did you mean 'review'?") | `template/render.go` | P2 | ✅ (implemented in `workflow/validate.go` `validateTemplateRefs()`) |
| H7 | **Error handling audit** — grep for `return nil` after error conditions, `Warn` used for fatal conditions, `writeJSONError` without logging; enforce fail-loud policy | All handlers, activities | P2 | ✅ (all handlers log before 500 responses; log.Printf migrated to slog in constants.go and auth.go) |

**H1–H3** are the minimum for "users can compose workflows that work." **H4–H5** prevent regressions and enable self-service. **H6–H7** improve the authoring and debugging experience.

#### H1: Workflow Validation — Detail

A `ValidateWorkflow(def WorkflowDef, registry ActionRegistry)` function that runs before `ExecuteWorkflow`. Catches at parse time:

- **Structural:** undefined step IDs in `depends_on`, circular dependencies, unreachable steps
- **Template refs:** `{{ .Steps.X.Output.Y }}` where step `X` doesn't exist or isn't an upstream dependency
- **Param refs:** `{{ .Params.Z }}` where `Z` isn't declared in `parameters`
- **Action types:** unknown `action.type` values not in the registry
- **Credential refs:** credentials referenced but not declared, or action steps needing credentials that aren't configured

Current failure mode: all of these crash at runtime with Go template panics or missing-key errors deep inside a Temporal activity.

#### H2: Action Step Promotion — Detail

Currently, action steps vs agent steps have very different capabilities:

| Capability | Agent Steps | Action Steps |
|------------|-------------|--------------|
| Credential store access | ✅ via ProvisionSandbox | ❌ worker env only |
| Log streaming | ✅ real-time to UI | ❌ no logs |
| Step output stored | ✅ | ❌ |
| Retries with heartbeat | ✅ | ❌ |
| Status transitions | ✅ cloning→running→verifying | ❌ instant complete |

Minimum fix: pass `teamID` + credential names to `ExecuteAction` activity; fetch from `CredStore`; log action execution; store action result as step output. This requires:
- Adding `credentials` field to `ActionDef` struct
- Updating `executeAction` in `dag.go` to pass team context
- Updating `ExecuteAction` activity signature to accept team ID + credential names

#### H3: Output Schema Enforcement — Detail

Current flow: agent runs → raw Claude Code JSON result stored as `output` → downstream steps access `{{ .Steps.X.Output.Y }}`.

Problem: the agent output is Claude Code metadata (`result`, `session_id`, `usage`, `modelUsage`, etc.), not the declared schema (`comments`, `summary`, `approval`). The `output.schema` in the YAML is documentation only — nothing extracts or validates it.

Fix: after agent execution, extract declared schema fields from the raw output. The agent's `result` text needs to be parsed (likely JSON within the text) or the prompt needs to instruct the agent to produce structured output. If declared fields are missing, fail the step rather than passing empty data downstream.

#### H7: Error Handling Patterns Found

Patterns discovered during debugging that should be audited codebase-wide:

| Pattern | Example | Fix |
|---------|---------|-----|
| `return nil` after error | `actionGitHubPostReviewComment` returning nil when GITHUB_TOKEN missing | Return `fmt.Errorf(...)` |
| `writeJSONError` without logging | `inbox.go` List handler | Add `slog.Error(...)` before every `writeJSONError` with 5xx status |
| `Warn` for fatal conditions | "GITHUB_TOKEN not set, skipping PR comment" | If the step can't do its job, return error, don't warn |
| Swallowed DB errors | Nullable columns → scan failure → generic "failed to list" | Use correct Go types (`*string`, `pq.StringArray`, `JSONMap`) |

### Track D: OSS Positioning — ✅ Docs Complete (D1–D7)

Independent of Track C. Can run in parallel. **D1–D7 completed in PR #22.**

| Phase | Item | File | Impact | Status |
|-------|------|------|--------|--------|
| D1 | README rewrite | `README.md` | Very High | ✅ |
| D2 | Use cases document | `docs/USE_CASES.md` | High | ✅ |
| D3 | Comparison page | `docs/COMPARISON.md` | High | ✅ |
| D4 | Getting started tutorial | `docs/GETTING_STARTED.md` | High | ✅ |
| D5 | CONTRIBUTING.md | `CONTRIBUTING.md` | Medium | ✅ |
| D6 | Production deployment guide | `docs/DEPLOYMENT.md` | Medium | ✅ |
| D7 | Example READMEs | `examples/README.md` | Medium | ✅ |
| D8 | Demo video (after C1+C2) | YouTube + README embed | High | ✅ |

---

## Tier 3: Platform Capability

Major new features. **Requires H1–H3 (workflow engine reliability) before adding new workflows or action types.** Without validation and contracts, each new template compounds debugging burden.

### Track E: MCP Agent Interface

See: [`archive/2026-03-14-mcp-agent-interface.md`](archive/2026-03-14-mcp-agent-interface.md) for the full design.

| Phase | Scope | Key Tools | Status |
|-------|-------|-----------|--------|
| E1 | Read-only context tools | `context.get_run`, `context.get_step_output`, `context.get_knowledge` | ✅ |
| E2 | Write tools | `artifact.create`, `memory.add_learning`, `memory.search`, `progress.update` | ✅ |
| E3 | Interactive tools | `inbox.request_input`, `inbox.notify` | ✅ |

### Track F: Feature Completion

| # | Item | Status |
|---|------|--------|
| 1 | Implement GitHub activity stubs (assign, label, PR review) | ✅ (assign removed; PR creation, review comments, labels, issue comments all implemented) |
| 2 | Implement Slack notification integration | ✅ |
| 3 | Add artifact collection to more templates (audit, migration) | ⬜ superseded by E2 (`artifact.create` MCP tool — agents collect their own artifacts) |
| 4 | Add Prometheus metrics (run counts, step durations, active sandboxes) | ✅ (activity duration/count, PRs created, sandbox provisioning) |
| 5 | **HITL inbox notifications** — create inbox item when step enters `awaiting_input`; include step title and approval context | ⬜ |
| 6 | **Per-step failure notifications** — create inbox item immediately when a non-optional step fails, don't wait for DAG completion | ⬜ |
| 7 | **Agent output normalization** — when no schema declared, extract `result` text and agent metadata into separate fields rather than storing raw Claude event | ⬜ |

### Track G: Long-Term Platform

| # | Item | Status |
|---|------|--------|
| 1 | OpenAPI spec for frontend-backend contract | ⬜ |
| 2 | Distributed tracing (OpenTelemetry) | ⬜ |
| 3 | Integration test suite (real Temporal + PostgreSQL) — see also H4 | ⬜ |
| 4 | Schema migration system (golang-migrate or atlas) | ⬜ |
| 5 | Unify logging to slog everywhere with correlation IDs — see also H7 | ⬜ |
| 6 | Semantic memory (embeddings, dedup, decay) | ⬜ |

### Track I: Future Enhancements

| # | Item | Status |
|---|------|--------|
| 1 | Notification preferences per-team/user | ⬜ |
| 2 | Data retention/archival for runs table | ⬜ |
| 3 | Run duration column in runs list | ✅ |
| 4 | Run detail as sequential step view (spinner on active step) | ⬜ |
| 5 | Run cost tracking — aggregate `total_cost_usd` across steps, new DB column, display in runs list and run detail | ✅ |
| 6 | Dark mode toggle | ✅ |

---

## Dependency Graph

```
Tier 1 (A, B) ✅
Tier 2: C (Web) ✅
        D (OSS Docs) ✅
        H (Engine Reliability) ✅ — H1-H7 all complete

Tier 3: E1 ──▶ E2 ──▶ E3 ✅
        F (parallel with E, benefits from H5)
        G (after E+F)
```

**Critical path cleared:** H1–H7 complete. Tier 3 work can begin.

---

## Open Questions

1. **Inbox failure items:** Should failed runs create "action_required" or just "output_ready"?
2. **Inbox auto-dismiss:** Should items auto-dismiss when their run reaches terminal state?
3. **MCP transport:** Stdio for AI agents + HTTP for shell steps (both)?
4. **MCP `request_input` timeout:** Configurable per-step? Default 4 hours?
5. **MCP server binary:** Separate binary in sandbox image vs bundled into agent runner?
6. **Notification dispatch:** Email/Slack notifications — separate plan or part of inbox work?
7. **Fan-out knowledge sharing:** Should sibling agents see each other's `memory.add_learning` in real-time?

---

## Archived Plans

The following plans were consolidated into this roadmap and moved to `archive/`. They contain detailed implementation specs (code snippets, component designs, API schemas) referenced by the tracks above.

| Archive File | Roadmap Track | Content |
|---|---|---|
| `archive/2026-03-13-profile-menu.md` | C5 | Full code for dropdown, /api/me enrichment, Layout header |
| `archive/2026-03-14-project-assessment.md` | A, B, F, G | Assessment findings, scorecard, prioritized recs |
| `archive/web-interface-enrichment.md` | C6, C7, C8 | API specs, component list, phases 11-12 detail (1-10 done) |
| `archive/inbox-notifications.md` | C6 | HITL/failure notification impl tasks |
| `archive/2026-03-14-oss-positioning.md` | D | README structure, use case scenarios, comparison matrix |
| `archive/2026-03-14-web-interface-polish.md` | C1, C2, C3, C4 | DAG node design, run detail layout, workflow card CSS |
| `archive/2026-03-14-mcp-agent-interface.md` | E | Full MCP design: architecture, phases, tool specs, memory assessment |
| `archive/2026-03-14-tier1-production-readiness.md` | A, B | Full Tier 1 implementation plan (completed) |
| `archive/2026-03-14-track-c-web-experience.md` | C1–C8 | Web experience implementation plan (completed) |
