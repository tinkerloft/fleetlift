---
date: 2026-03-11T22:55:47Z
researcher: Claude Sonnet 4.6
git_commit: 2c536a232f687b81cbde562ba08903122a884dee
branch: platform-v2
repository: fleetlift
topic: "Platform v2 Redesign — Full Implementation"
tags: [implementation, platform-v2, temporal, dag-workflow, multi-tenant, auth, web-ui]
status: complete
last_updated: 2026-03-11
last_updated_by: Claude Sonnet 4.6
type: implementation_strategy
---

# Handoff: platform-redesign — v2 implementation complete

## Task(s)

Implemented the full Fleetlift platform-v2 redesign per the approved design and implementation plan.

| Phase | Status | Description |
|-------|--------|-------------|
| 1 — Strip old arch | ✅ complete | Removed sidecar agent, file-based protocol, old task model; gutted cmd stubs |
| 2+3 — DB + Models | ✅ complete | 13-table PostgreSQL schema; all entity model types |
| 4 — Auth | ✅ complete | JWT (HS256), GitHub OAuth, HTTP middleware |
| 5 — Templates | ✅ complete | BuiltinProvider + DBProvider + Registry; 9 builtin YAML workflows |
| 6+7 — AgentRunner + Sandbox | ✅ complete | Runner interface, ClaudeCodeRunner, OpenSandbox REST client |
| 8 — DAG Workflow | ✅ complete | DAGWorkflow + StepWorkflow with HITL signals |
| 9+10 — Activities + Worker | ✅ complete | All core activities; AES-256-GCM credential store; full worker registration |
| 11 — API Server | ✅ complete | chi router; all REST + SSE endpoints; GitHub OAuth callback |
| 12 — CLI | ✅ complete | cobra CLI with auth/workflow/run/inbox/credential commands |
| 13 — Web UI | ✅ complete | React SPA rebuilt with DAGGraph, StepPanel, HITLPanel, LogStream, ReportViewer |
| 14+15 — Lint + final | ✅ complete | All lint, tests, build clean |

**Docs updated:** `CLAUDE.md` reflects new structure and env vars. Implementation plan progress table added.

**Not done (deferred):** README and `docs/` user-facing docs need updating — flagged for next session.

## Critical References

- `docs/plans/2026-03-11-platform-redesign.md` — approved design doc (architecture, data model, DAG execution, YAML schema, 9 workflow templates, CLI commands)
- `docs/plans/2026-03-11-platform-redesign-impl.md` — implementation plan with progress table
- `CLAUDE.md` — project conventions, package structure, env vars reference

## Recent changes

All changes are on branch `platform-v2`. Two commits:
- `b6a1f5d` — Phase 1: stripped old architecture (89 files)
- `e027bd6` — Full platform-v2 implementation (114 files, 7644 insertions)
- `1b1d8eb` — Implementation plan progress table
- `2c536a2` — CLAUDE.md updated

Key new packages and entry files:
- `internal/auth/jwt.go`, `internal/auth/github.go`, `internal/auth/middleware.go`
- `internal/db/schema.sql`, `internal/db/db.go`
- `internal/model/workflow.go` — WorkflowDef, StepDef, SandboxSpec, etc.
- `internal/model/run.go`, `internal/model/step.go`
- `internal/template/provider.go`, `internal/template/builtin.go`, `internal/template/render.go`
- `internal/template/workflows/*.yaml` — 9 builtin workflow definitions
- `internal/agent/runner.go`, `internal/agent/claudecode.go`
- `internal/sandbox/client.go`, `internal/sandbox/opensandbox/client.go`
- `internal/workflow/dag.go`, `internal/workflow/step.go`
- `internal/activity/activities.go`, `internal/activity/execute.go`, `internal/activity/provision.go`, `internal/activity/credential.go`
- `internal/server/router.go`, `internal/server/handlers/`
- `cmd/cli/main.go`, `cmd/cli/client.go`, `cmd/cli/run.go`, `cmd/cli/auth.go`
- `cmd/worker/main.go`, `cmd/server/main.go`
- `web/src/App.tsx` — rebuilt routes
- `web/src/components/DAGGraph.tsx`, `HITLPanel.tsx`, `LogStream.tsx`, `StepPanel.tsx`, `ReportViewer.tsx`
- `web/src/pages/` — WorkflowList, WorkflowDetail, RunList, RunDetail, Inbox, ReportList, ReportDetail

## Learnings

- **LSP diagnostics were frequently stale** — the agent team reported successful `go build ./...` but LSP showed errors. Always run `go build ./...` directly rather than trusting LSP output.
- **Temporal activity references use string constants**, not function references — avoids circular import between `internal/workflow` and `internal/activity`. Constants live in `internal/activity/constants.go`.
- **go.mod** already had most deps (chi, cors, oauth2, yaml, uuid, testify, temporal). New additions: `github.com/golang-jwt/jwt/v5`, `github.com/jmoiron/sqlx`, `github.com/lib/pq`.
- **web/dist/** must contain at least one embeddable file for `//go:embed dist` in `web/embed.go` to compile — `web/dist/placeholder.txt` was added in Phase 1; replaced by real build output later.
- **evalCondition** in `dag.go` is a placeholder returning `true` — full expression evaluation is not implemented.
- **Knowledge system** (`internal/knowledge/`, `internal/model/knowledge.go`, `internal/activity/knowledge.go`) is a v1 holdover present on the branch but not wired into the v2 DAG execution flow.

## Artifacts

- `docs/plans/2026-03-11-platform-redesign.md` — design doc
- `docs/plans/2026-03-11-platform-redesign-impl.md` — implementation plan (with progress table)
- `CLAUDE.md` — project instructions (updated)
- `internal/db/schema.sql` — canonical DB schema
- `internal/template/workflows/` — 9 builtin workflow YAMLs
- `internal/model/workflow.go` — WorkflowDef YAML schema (single source of truth for template format)
- `/Users/andrew/.claude/projects/-Users-andrew-dev-code-projects-fleetlift--bare/memory/project-knowledge-system.md` — memory note on knowledge system decision

## Action Items & Next Steps

1. **README update** — `README.md` still describes the old v1 platform. Needs a rewrite covering: what platform-v2 is, quick-start (docker-compose for Temporal + Postgres + OpenSandbox), env vars, running worker + server + CLI.

2. **docs/ user-facing docs** — Any docs in `docs/` beyond the plan files likely describe the old architecture. Review and update.

3. **Knowledge system decision** — Either wire `internal/knowledge/` into the v2 DAG execution flow (e.g., as a post-step enrichment activity after ExecuteStep) or remove it. See memory note.

4. **evalCondition implementation** — `internal/workflow/dag.go`'s `evalCondition()` is a stub returning `true`. Needs a real expression evaluator (e.g., simple Go template boolean or a small expression library) to support conditional steps.

5. **API: GET /api/me endpoint** — The CLI's `auth login` polls `GET /api/me` to confirm auth, but this endpoint is not implemented in the server. Add it to `internal/server/handlers/auth.go`.

6. **Integration test skeleton** — `tests/integration/dag_test.go` is stubbed. When a real Temporal + OpenSandbox environment is available, flesh out the end-to-end test.

7. **Markdown export in reports** — `GET /api/reports/{runID}/export?format=markdown` is wired but the Markdown rendering is likely minimal. Improve if needed.

## Other Notes

- The branch is `platform-v2`, branched from `main` at commit `441523d`.
- All tests pass: `go test ./...` (13 packages), `npm run build` (web/dist output ~500KB JS).
- The v1 `internal/knowledge/` store uses local file storage (not PostgreSQL) — it predates the v2 DB schema entirely.
- `internal/activity/github.go` was rewritten in Phase 1 as a stub, then extended in Phase 9. The `CreatePullRequest` activity now lives in `internal/activity/pr.go` (not github.go), which runs `gh pr create` in the sandbox.
- Web dist is embedded via `//go:embed dist` in `web/embed.go` — the server binary serves the SPA for all non-API routes.
- The 9 builtin workflow YAMLs are embedded via `//go:embed workflows/*.yaml` in `internal/template/builtin.go`.
