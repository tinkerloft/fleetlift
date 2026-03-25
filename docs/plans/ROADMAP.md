# FleetLift Roadmap

**Last updated:** 2026-03-25

---

## Completed

### Tier 1 — Production Readiness ✅
Broken contracts fixed, dead code removed, test coverage added. See [`archive/2026-03-14-tier1-production-readiness.md`](archive/2026-03-14-tier1-production-readiness.md).

### Tier 2 — Product Quality ✅
- **Track C — Web Experience:** DAG graph, run detail, workflow pages, global polish, profile menu, inbox enhancements, DiffViewer/JsonViewer/StepTimeline. See [`archive/2026-03-14-track-c-web-experience.md`](archive/2026-03-14-track-c-web-experience.md).
- **Track D — OSS Positioning:** README, use cases, comparison, getting started, CONTRIBUTING, deployment guide, demo video.
- **Track H — Workflow Engine Reliability:** H1 workflow validation, H2 action step credentials, H3 output schema enforcement, H4 integration test harness, H5 action contract registry, H6 template rendering safety, H7 error handling audit.

### Tier 3 (partial) ✅
- **Track E — MCP Agent Interface:** E1 read-only context tools, E2 write tools (`artifact.create`, `progress.update`), E3 interactive tools (`inbox.request_input`, `inbox.notify`).
- **Track F (partial):** GitHub activities, Slack notifications, Prometheus metrics, run duration + cost tracking, dark mode, HITL inbox notifications (via MCP `inbox.request_input`).
- **Agent Profiles (PR #44/#45):** Workflows declare an `agent_profile` to inject skills, MCPs, and plugins into the sandbox before execution.
- **Fan-out reliability (PR #47, 2026-03-19):** Per-step input tracking, DAG visual redesign (IBM Plex, dark mode tokens, status glows, border animations), DAG fan-out collapse (threshold 6, collapsed node with status bar), partial fan-out failure → inbox item + proceed/terminate signal, stuck `running` step records fixed.
- **Artifact & output UX (PR #48, 2026-03-19):** `GET /api/artifacts/{id}/content` endpoint (auth, Content-Disposition, download mode, content-type allowlist); `ArtifactCard` component (expand/collapse, markdown rendering, download); `ReportViewer` redesigned artifact-first; `StepPanel` surfaces artifacts above output JSON; `RunDetail` hero panel auto-expands primary artifact above DAG; inbox `output_ready`/`notify` items get "View Report →" links; `artifact_id` stored on inbox items via `GetPrimaryRunArtifactID` activity. See [`2026-03-19-artifact-ux-plan.md`](2026-03-19-artifact-ux-plan.md).
- **Post-PR #47 bug fixes (2026-03-19):** DB CHECK constraint extended for `fan_out_partial_failure` kind (migration 007); `max_turns` default logic fixed (0 = runner default, not a floor); concurrent fan-out signal routing fixed (per-step channels, StepID in payload); `StepWorkflow` defer double-write on `finalizeStep` failure prevented (`stepRunFinalized` flag); MCP env file write verified in sandbox before continuing.
- **Platform fixes + ADS workflow (2026-03-20, PRs #49–#50):** `evalCondition` exposes step output; `pull_request` config fields template-rendered via `RenderPrompt`; `CreatePullRequest` skips commit/push/PR on clean working tree; `extractCostUSD` checks `total_cost_usd` then falls back to `cost_usd`; `auto-debt-slayer` builtin workflow added; ADS agent Dockerfile + superpowers agent profile (migration 010); improved Claude Code output handling (fenced JSON extraction, plain-text normalization partial).

---

## Outstanding

### Prioritized Improvement Plan

#### P0 — Reliability and Operator Trust

| Item | Why now |
|---|---|
| **Worker-restart-safe execution** | Highest production-risk gap. `ExecuteStep` can fail permanently if the worker restarts mid-run; add heartbeat-detail checkpointing and resume-or-skip behavior before expanding the platform surface. |
| **Per-step failure visibility** (F6) | Operators should see failures as soon as a non-optional step fails, not only when the full run completes. This shortens response time and makes HITL operations more credible. |
| **Output normalization — plain-text result** (F7) | `extractStructuredOutput` handles JSON results correctly but still returns the full raw Claude event map when `result` is a plain string. |

#### P1 — Workflow Product Completion

| Item | Why now |
|---|---|
| **Bring Your Own Workflow UI** | The backend CRUD/fork APIs exist already; the highest-leverage product gap is the missing frontend authoring flow. |
| **Validation-first workflow editing** | Surface workflow validation errors cleanly, support fork-from-builtin and import-from-YAML, and make custom workflow creation safe for operators. |
| **Frontend docs cleanup** | Replace the stock Vite README with real web setup and contributor guidance to match the maturity of the backend/docs experience. |

#### P1.5 — Platform Hardening

| Item | Why now |
|---|---|
| **OpenAPI / contract hardening** | Reduce frontend-backend drift as the API surface grows and improve confidence in future UI work. |
| **Correlation IDs + observability pass** | Make cross-service debugging easier across server, worker, SSE, and sandbox execution paths. |
| **Real integration coverage** | Add live Temporal/PostgreSQL integration coverage beyond SDK testsuite mocks to catch production-shaped regressions earlier. |

#### P2 — Capability Expansion

| Item | Why later |
|---|---|
| **GitHub integration consolidation** | Valuable cleanup, but less urgent than reliability and authoring completeness. |
| **Workflow expressiveness** | Conditional PR creation, templated PR fields, and per-repo fan-out filters improve ergonomics after the platform is more stable. |
| **Knowledge loop evolution** | Semantic memory, dedup, and decay are useful multipliers, but should follow core workflow and platform hardening. |

#### Recommended sequence

1. Reliability polish — worker-restart-safe execution, F6 per-step failure notifications, F7 plain-text output normalization.
2. Workflow product completion — BYOW UI (K1), validation UX, workflow import/fork/edit flows, frontend docs.
3. Platform hardening — OpenAPI, correlation IDs, real integration tests.
4. Capability expansion — GitHub cleanup, workflow expressiveness (J3/J4), richer knowledge systems.

### Track F — Feature Completion

| # | Item | Notes |
|---|------|-------|
| F5 | ~~**Cost tracking**~~ ✅ **Done** | `extractCostUSD` checks `total_cost_usd` first, falls back to `cost_usd`; `total_cost_usd` on runs computed as `SUM(cost_usd)` from step_runs. |
| F6 | **Per-step failure notifications** — create inbox item immediately when a non-optional step fails | `CreateInboxItemActivity` in `dag.go` only fires in the run-completion defer; operators see nothing until the whole run ends |
| F7 | **Agent output normalization (plain-text result)** — when `result` is a plain string with no JSON, store `{"result": "<string>"}` instead of the full raw Claude event map | `extractStructuredOutput` handles JSON-object and fenced/bare JSON-in-string cases; plain-text falls through to `return raw` |

### Track J — Workflow Expressiveness

Identified during `doc-assessment` workflow design. Full spec: [`2026-03-18-workflow-expressiveness-prd.md`](2026-03-18-workflow-expressiveness-prd.md).

| # | Item | Effort | Priority |
|---|------|--------|----------|
| J1 | ~~**Conditional PR creation**~~ ✅ **Done** — `CreatePullRequest` runs `git status --porcelain` after `git add -A` and returns early if tree is clean | Low | — |
| J2 | ~~**Template rendering in `pull_request` fields**~~ ✅ **Done** — `resolveStep` in `dag.go` renders `BranchPrefix`, `Title`, `Body` through `RenderPrompt` | Low | — |
| J3 | **Per-repo conditional fan-out (`filter` field)** — template expression evaluated per-repo against upstream fan-out outputs; only matching repos proceed | Medium | P2 |
| J4 | **Sandbox group reuse across fan-out steps** — same sandbox instance shared by sibling fan-out steps operating on the same repo; eliminates re-clone penalty | High | P3 |

### Track K — Bring Your Own Workflow + New Templates

#### K1 — Bring Your Own Workflow (BYOW)

Users upload, author, and manage their own workflow YAML alongside builtins. The backend is largely done — `POST /workflows`, `PUT /workflows/{id}`, `DELETE /workflows/{id}`, and `POST /workflows/{id}/fork` all exist in `handlers/workflows.go`. The gap is frontend only.

| Phase | Item | Notes |
|-------|------|-------|
| K1a | **API client methods** — add `createWorkflow`, `updateWorkflow`, `deleteWorkflow`, `forkWorkflow` to `client.ts` | Backend exists; frontend missing |
| K1b | **Fork builtin** — "Customise" button on builtin workflow cards opens the YAML in an editable copy owned by the team | Good entry point for non-technical users |
| K1c | **Create from scratch** — "New Workflow" button opens a CodeMirror editor pre-populated with a starter template; validates on save | `ValidateWorkflow()` already exists in Go; surface errors in UI |
| K1d | **Import from YAML** — drag-and-drop or paste raw YAML; validate and save | Handles the "I already have a YAML" case |
| K1e | **Edit + delete** — team-owned workflows show Edit and Delete actions; builtins show Fork only | Distinguish `builtin: true` vs team-owned in the UI |
| K1f | **Prompt customisation** — optional free-text field per-run to append additional instructions to any step's prompt | Small parameter UX; high operator value for tailoring PR review focus etc. |

#### K2 — New Workflow Templates

| # | Item | Notes |
|---|------|-------|
| K2a | **Background Document Assessor** — scheduled overnight run across repos with recent changes; report findings and/or raise fix PRs | Workflow YAML exists; needs scheduling trigger + delta filtering |
| K2b | **End-to-End Code Change Manager** — creation → CI → fix → review comments → CI → hand off | Depends on J3 (per-repo conditional fan-out) for the review-fix loop |

### Track G — Platform

| # | Item |
|---|------|
| G1 | OpenAPI spec for frontend-backend contract |
| G3 | Integration test suite against real Temporal + PostgreSQL — `dag_integration_test.go` uses Temporal `testsuite` mocks, not a live server |
| G4 | Unified slog logging with correlation IDs everywhere |
| G5 | Semantic memory (embeddings, dedup, decay) |

> Note: schema migrations (golang-migrate v4) are already fully implemented — auto-applied at startup, embedded via `iofs`, tested in `db_test.go`.

### Track L — Minion-Parity (individual dev task delegation)

Full spec: [`docs/superpowers/specs/2026-03-25-minion-parity-design.md`](../superpowers/specs/2026-03-25-minion-parity-design.md)

| Phase | Item | Status |
|---|---|---|
| L1 | Home page (prompt-first `/`), `quick-run` builtin workflow, `created_by=me` runs filter, Retry button, log search, model selection per run | Planned |
| L2 | Prompt improvement — `POST /api/prompt/improve` server endpoint + Minion-style side-by-side modal | Planned |
| L3 | Prompt presets (personal + team) + saved repo shortcuts | Planned |
| L4 | Co-author attribution — inject triggering user's GitHub identity into sandbox env vars | Planned |
| L5 | **Follow Up** — button on RunDetail that pre-populates Home prompt with completed run's context (output summary, repo, branch) | Planned |

### Track I — Future Enhancements

| # | Item |
|---|------|
| I1 | Notification preferences per-team/user |
| I2 | Data retention / archival for runs table |
| I3 | Run detail sequential step view (spinner on active step) |

---

## Open Questions

1. **Inbox auto-dismiss:** Should items auto-dismiss when their run reaches a terminal state?
2. **MCP `request_input` timeout:** Configurable per-step? Default 4 hours?
3. **Notification dispatch:** Email/Slack from inbox items — separate plan or part of F6?
4. **Fan-out knowledge sharing:** Should sibling agents see each other's `memory.add_learning` in real-time?
5. **Worker-restart-safe execution:** Checkpoint activity progress so retries resume rather than restart. Requires heartbeat-detail checkpointing + per-step `retry_on_worker_restart` YAML flag. See `ENHANCEMENTS.md` for full research. Workaround: don't restart the worker during active runs.
6. **Remove `mode: report|transform`** — the distinction is blurry and forces workflow authors to think in terms of implementation rather than intent. Tracked in the expressiveness PRD.
