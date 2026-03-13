# Platform v2 Completion Design

**Date:** 2026-03-12
**Branch:** platform-v2
**Status:** Approved

## Overview

Four work streams to complete the platform-v2 implementation:

1. Knowledge system (migrate to PostgreSQL, wire into DAG)
2. Documentation (archive v1, write v2 docs)
3. Outstanding action items (GET /api/me, evalCondition, integration tests, markdown export)

---

## 1. Knowledge System

### Goal

Agents that run repetitive workflow steps accumulate insights. Those insights should be captured, reviewed, and injected into future runs of the same workflow to improve agent performance over time.

### Data Model

New `knowledge_items` table in PostgreSQL:

```sql
CREATE TABLE knowledge_items (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id         UUID NOT NULL REFERENCES teams(id),
    workflow_template_id UUID REFERENCES workflow_templates(id),
    step_run_id     UUID REFERENCES step_runs(id),
    type            TEXT NOT NULL,   -- pattern | correction | gotcha | context
    summary         TEXT NOT NULL,
    details         TEXT,
    source          TEXT NOT NULL,   -- auto_captured | manual
    tags            TEXT[],
    confidence      FLOAT DEFAULT 1.0,
    status          TEXT NOT NULL DEFAULT 'pending',  -- pending | approved | rejected
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

`internal/model/knowledge.go` is updated to match (drop `KnowledgeConfig` as a standalone struct — move capture/enrich config into `StepDef`).

### Workflow Template Schema Changes

Add optional `knowledge` block to `StepDef`:

```yaml
steps:
  - id: migrate-controllers
    prompt: "..."
    knowledge:
      capture: true       # instruct agent to write fleetlift-knowledge.json
      enrich: true        # inject approved knowledge into prompt
      max_items: 10       # cap on injected items
      tags: [controllers] # filter for relevant items
```

### Capture Flow

1. If `step.knowledge.capture == true`, `ExecuteStep` activity prepends a `KNOWLEDGE CAPTURE` section to the agent prompt instructing it to write `fleetlift-knowledge.json` before exiting. Format:
   ```json
   [{"type": "pattern", "summary": "...", "details": "...", "tags": [], "confidence": 0.9}]
   ```
2. After step completes, `CaptureKnowledge` activity reads `fleetlift-knowledge.json` from the sandbox via `sandbox.ReadFile`, inserts rows into `knowledge_items` with `status=pending`.
3. `CaptureKnowledge` is called by `StepWorkflow` after `ExecuteStep` completes (if capture enabled).

### Enrichment Flow

1. If `step.knowledge.enrich == true`, `ExecuteStep` activity queries `knowledge_items` WHERE `team_id = $team_id AND (workflow_template_id = $id OR workflow_template_id IS NULL) AND status = 'approved'`, ordered by confidence DESC, limited by `max_items`.
2. Formats items as a `KNOWLEDGE BASE` context block prepended to the step prompt.

### Review/Curation

- `GET /api/knowledge` — list items (filterable by status, workflow, tags)
- `GET /api/knowledge/{id}` — get item
- `PATCH /api/knowledge/{id}` — update status (approve/reject)
- `DELETE /api/knowledge/{id}` — delete item
- Web UI: new "Knowledge" page (list with approve/reject actions, filter by status)
- CLI: `fleetlift knowledge list`, `fleetlift knowledge approve <id>`, `fleetlift knowledge reject <id>`

### Drop File-Based Store

Remove `internal/knowledge/store.go` and `internal/knowledge/store_test.go`. Replace with a DB-backed `KnowledgeStore` using sqlx (mirrors pattern of other DB operations in the codebase). Keep `internal/model/knowledge.go` (updated). Replace `internal/activity/knowledge.go` stub with real implementation.

---

## 2. Documentation

### Archive

Move to `docs/archive/v1/` (no content changes):
- `docs/DESIGN.md` → `docs/plans/DESIGN.md`
- `docs/GROUPED_EXECUTION.md`
- `docs/TASK_FILE_REFERENCE.md`
- `docs/SIDECAR_AGENT.md` (already in plans/)
- `docs/examples/` (entire directory)
- `docs/GITHUB_ACTIONS_DISCOVERY.md` and `docs/INTEGRATION_NOTES.md` (after reading to confirm v1-only)

### Rewrite

**`README.md`** — covers:
- What Fleetlift is (multi-tenant agentic workflow platform)
- Quick-start: prerequisites, docker-compose for Temporal + Postgres + OpenSandbox, run worker + server, CLI login
- Env vars table (mirrors CLAUDE.md)
- Pointer to docs/

**`docs/CLI_REFERENCE.md`** — all v2 commands: `auth`, `workflow`, `run`, `inbox`, `credential`, `knowledge`

**`docs/TROUBLESHOOTING.md`** — v2 symptoms: worker not connecting to Temporal, sandbox provisioning failures, JWT errors, missing env vars

### New

**`docs/WORKFLOW_REFERENCE.md`** — complete YAML schema for workflow templates: top-level fields, `steps[]`, `StepDef` fields, `sandbox` spec, `knowledge` config, `condition` syntax, `inputs`/`outputs`, `on_failure`

**`docs/ARCHITECTURE.md`** — system overview: components (server, worker, CLI, web), Temporal DAG execution model, OpenSandbox integration, multi-tenancy model, auth (JWT + GitHub OAuth), knowledge loop diagram

---

## 3. Outstanding Action Items

### GET /api/me

Add to `internal/server/handlers/auth.go`. Reads JWT claims already decoded by middleware from request context. Returns:
```json
{"id": "...", "email": "...", "name": "...", "team_id": "..."}
```
No additional DB query required.

### evalCondition

Replace stub in `internal/workflow/dag.go` with a Go template evaluator:
- Parse `condition` string as a Go template with a custom FuncMap
- Execute against a data map: `{"outputs": stepOutputs, "status": lastStatus}`
- Return `true` if rendered string equals `"true"`, false otherwise
- Support simple expressions via template syntax: `{{eq .outputs.exit_code "0"}}`
- No external dependency needed (stdlib `text/template`)

### Integration Tests

In `tests/integration/dag_test.go`:
- Add `//go:build integration` build tag (skipped in normal `go test ./...`)
- Use Temporal Go SDK `testsuite.WorkflowTestSuite` for in-process Temporal testing (no external server)
- Cover: DAGWorkflow happy path, HITL signal handling, step failure propagation
- Mock sandbox client for integration tests

### Markdown Export

In `internal/server/handlers/` (report handler):
- `GET /api/reports/{runID}/export?format=markdown` renders a structured Markdown document
- Sections: run summary (status, duration, template), step results table, per-step output, artifacts list
- Use `text/template` with a hardcoded Markdown template

---

## Sequencing

1. Knowledge DB migration + model + store
2. Knowledge capture/enrich wiring (StepWorkflow + ExecuteStep)
3. Knowledge API + CLI + Web UI
4. GET /api/me + evalCondition + markdown export (small, can be parallelized)
5. Integration tests
6. Documentation (archive + rewrite + new)
