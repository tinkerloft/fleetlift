# FleetLift Roadmap

**Last updated:** 2026-03-19

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
- **Fan-out reliability (2026-03-19):** Per-step input tracking, DAG visual redesign (IBM Plex, dark mode tokens, status glows, border animations), DAG fan-out collapse (threshold 6, collapsed node with status bar), partial fan-out failure → inbox item + proceed/terminate signal, stuck `running` step records fixed.

---

## Outstanding

### Track F — Feature Completion

| # | Item | Notes |
|---|------|-------|
| F5 | **Cost tracking broken** — `cost_usd` is NULL for all steps despite `extractCostUSD` being called | `extractCostUSD` looks for `cost_usd` in the raw result event but the field is absent; Claude Code CLI may use a different field name (`total_cost_usd`, `usage.cost_usd`, etc.) — needs inspection of actual CLI output |
| F6 | **Per-step failure notifications** — create inbox item immediately when a non-optional step fails | `CreateInboxItemActivity` in `dag.go` only fires in the run-completion defer; operators see nothing until the whole run ends |
| F7 | **Agent output normalisation (text result)** — when no schema declared and `result` is a string, store it cleanly instead of the full raw Claude event map (`session_id`, `usage`, `modelUsage`, etc.) | `extractStructuredOutput` already handles the JSON-object case; only the text-string case remains |
| F8 | **Artifact & output UX** — full plan at [`2026-03-19-artifact-ux-plan.md`](2026-03-19-artifact-ux-plan.md) | Artifacts stored correctly but unviewable; no content endpoint; no markdown rendering; no hero result panel on run detail; inbox links don't reach artifacts |

### Track J — Workflow Expressiveness

Identified during `doc-assessment` workflow design. Full spec: [`2026-03-18-workflow-expressiveness-prd.md`](2026-03-18-workflow-expressiveness-prd.md).

| # | Item | Effort | Priority |
|---|------|--------|----------|
| J1 | **Conditional PR creation** — skip `git commit + push + PR` silently when working tree is clean | Low | P1 |
| J2 | **Template rendering in `pull_request` fields** — apply `RenderPrompt` to `Title`, `Body`, `BranchPrefix`, `Draft` so params and step outputs can flow into PR config | Low | P1 |
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
