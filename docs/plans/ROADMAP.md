# FleetLift Roadmap

**Date:** 2026-03-14
**Status:** Active

FleetLift has strong architecture and a working core engine. This roadmap covers what's needed to go from "internal tool" to "production-ready OSS product."

---

## Tier 1: Production Readiness

Fix broken contracts, remove dead code, add missing tests. **Do first — blocks everything else.**

See: [`2026-03-14-tier1-production-readiness.md`](2026-03-14-tier1-production-readiness.md) for the full implementation plan.

### Track A: Fix What's Broken

| # | Item | Files | Status |
|---|------|-------|--------|
| 1 | Fix `spaHandler` panic → return error | `router.go` | ⬜ |
| 2 | Add `GET /health` endpoint | `router.go` | ⬜ |
| 3 | Standardize error responses to JSON (`writeJSONError`) | All handler files | ⬜ |
| 4 | Fix frontend-backend contract mismatches (diff, output, artifacts) | `runs.go`, `reports.go`, `client.ts` | ⬜ |
| 5 | Consolidate duplicate `shellQuote` → `internal/shellquote/` | `activity/`, `agent/` | ⬜ |
| 6 | Surface goroutine panics in DAG as step failures | `dag.go` | ⬜ |
| 7 | Remove dead knowledge-capture code from worker/activities | `activities.go`, `execute.go`, `step.go`, `knowledge.go` | ⬜ |

### Track B: Test Coverage Gaps

| # | Item | Files | Status |
|---|------|-------|--------|
| 8 | OAuth CSRF state validation tests | `handlers/auth_test.go` | ⬜ |
| 9 | Multi-tenant isolation tests | `handlers/isolation_test.go` | ⬜ |
| 10 | SSE event stream auth guard tests | `handlers/runs_test.go` | ⬜ |

---

## Tier 2: Product Quality

Makes FleetLift feel finished. Tracks C and D can run in parallel.

### Track C: Web Experience

Merges the web interface enrichment remaining phases, visual polish plan, profile menu, and inbox notifications into a single track.

#### C1: DAG Graph Overhaul (from Visual Polish Phase 1)
- Custom node component: status dot + title + mode badge + duration
- Left accent bar in status color, white/dark background
- Pulsing dot for `running` nodes
- `smoothstep` edges, animated dashed stroke for active edges
- Dynamic height: `min(400px, levels * 140 + 100)`
- **Files:** `DAGGraph.tsx` (rewrite), new `DAGNode.tsx`

#### C2: Run Detail — The Whoa Moment (from Visual Polish Phase 3)
- Live duration counter next to status badge
- Progress indicator: `3 of 7 steps` with segmented bar
- Step list → vertical timeline component
- Fan-out visualization (one node → parallel lanes)
- Data flow animation on edges when step completes
- Diff syntax coloring (`+` green, `-` red, `@@` blue)
- JSON output syntax coloring
- **Files:** `RunDetail.tsx`, `StepPanel.tsx`, `LogStream.tsx`, new `StepTimeline.tsx`

#### C3: Workflow Pages — Color & Identity (from Visual Polish Phase 2)
- Category color per workflow (top border accent + icon tint)
- Lucide icon per category (Shield, Bug, GitBranch, etc.)
- Step count + mode chips on cards
- Sort workflow list alphabetically (fix backend `ORDER BY title` too)
- Workflow detail: hero section with icon, CodeMirror YAML view
- **Files:** `WorkflowList.tsx`, `WorkflowDetail.tsx`, new `workflow-colors.ts`

#### C4: Global Polish (from Visual Polish Phase 4)
- Skeleton loading states (replace "Loading..." text)
- Empty states with muted icons + CTAs
- Enhanced status badges (pulsing dot for `running`, amber for `awaiting_input`)
- Monospace text backgrounds, section separators
- **Files:** `badge.tsx`, `index.css`, multiple pages

#### C5: Profile Menu (from Profile Menu plan)
- Radix DropdownMenu ui primitive
- Enrich `/api/me` with user name, email, team details
- `UserMenu` component with initials avatar, team list, sign out
- Header bar in Layout.tsx
- **Files:** `dropdown-menu.tsx`, `UserMenu.tsx`, `Layout.tsx`, `auth.go`

#### C6: Inbox Enhancements (from Inbox Notifications + Web Enrichment Phase 12)
- HITL inbox notifications (create item when step enters `awaiting_input`)
- Per-step failure notifications (non-optional step fails → inbox item)
- Inline approve/reject/steer buttons in inbox
- Unread count badge on Inbox nav link
- **Files:** `step.go`, `dag.go`, `Inbox.tsx`, `Sidebar.tsx`

#### C7: Enhanced Components (from Web Enrichment Phase 11)
- Syntax-highlighted diff viewer (Prism or CSS-only)
- CodeMirror YAML editor with real-time validation
- Execution timeline component
- **Files:** new components, `TaskDetail.tsx`

#### C8: System Health Page (from Web Enrichment Phase 12)
- Worker status, task queue depth (via Temporal API)
- Recent workflow failure rate
- Links to full Temporal UI

### Track D: OSS Positioning

Independent of Track C. Can run in parallel.

| Phase | Item | File | Impact | Status |
|-------|------|------|--------|--------|
| D1 | README rewrite | `README.md` | Very High | ⬜ |
| D2 | Use cases document | `docs/USE_CASES.md` | High | ⬜ |
| D3 | Comparison page | `docs/COMPARISON.md` | High | ⬜ |
| D4 | Getting started tutorial | `docs/GETTING_STARTED.md` | High | ⬜ |
| D5 | CONTRIBUTING.md | `CONTRIBUTING.md` | Medium | ⬜ |
| D6 | Production deployment guide | `docs/DEPLOYMENT.md` | Medium | ⬜ |
| D7 | Example READMEs | `examples/README.md` | Medium | ⬜ |
| D8 | Demo video (after C1+C2) | YouTube + README embed | High | ⬜ |
| D9 | Web landing route (optional) | `web/` public route | Low | ⬜ |

---

## Tier 3: Platform Capability

Major new features. Do after Tiers 1-2.

### Track E: MCP Agent Interface

See: [`archive/2026-03-14-mcp-agent-interface.md`](archive/2026-03-14-mcp-agent-interface.md) for the full design.

| Phase | Scope | Key Tools | Status |
|-------|-------|-----------|--------|
| E1 | Read-only context tools | `context.get_run`, `context.get_step_output`, `context.get_knowledge` | ⬜ |
| E2 | Write tools | `artifact.create`, `memory.add_learning`, `memory.search`, `progress.update` | ⬜ |
| E3 | Interactive tools | `inbox.request_input`, `inbox.notify` | ⬜ |

### Track F: Feature Completion

| # | Item | Status |
|---|------|--------|
| 1 | Implement GitHub activity stubs (assign, label, PR review) | ⬜ |
| 2 | Implement Slack notification integration | ⬜ |
| 3 | Add artifact collection to more templates (audit, migration) | ⬜ |
| 4 | Add Prometheus metrics (run counts, step durations, active sandboxes) | ⬜ |
| 5 | Notification preferences per-team/user | ⬜ |
| 6 | Data retention/archival for runs table | ⬜ |

### Track G: Long-Term Platform

| # | Item | Status |
|---|------|--------|
| 1 | OpenAPI spec for frontend-backend contract | ⬜ |
| 2 | Distributed tracing (OpenTelemetry) | ⬜ |
| 3 | Integration test suite (real Temporal + PostgreSQL) | ⬜ |
| 4 | Schema migration system (golang-migrate or atlas) | ⬜ |
| 5 | Unify logging to slog everywhere with correlation IDs | ⬜ |
| 6 | Semantic memory (embeddings, dedup, decay) | ⬜ |

---

## Dependency Graph

```
Tier 1 (A, B) ─────────────────────────────────┐
                                                ▼
Tier 2: C1 (DAG) ──▶ C2 (Run detail) ──▶ D8 (Demo video)
        C3-C8 (parallel with C1/C2)
        D1-D7 (independent, parallel)
                                                │
Tier 3: E1 ──▶ E2 ──▶ E3                       │
        F (independent, parallel)               │
        G (after E+F)                           ▼
```

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
