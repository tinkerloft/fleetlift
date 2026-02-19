# Web UI Design

**Date**: 2026-02-18
**Status**: Approved
**Phase**: 9.5

---

## Goal

A shared dashboard for operators running Fleetlift tasks and developers whose repos are being transformed. Primary use case: reviewing agent-produced diffs, approving/rejecting changes, and steering stuck agents — replacing the need to run CLI commands for human-in-the-loop interactions.

---

## Architecture

### New binary: `cmd/server`

A Go binary that serves:
1. A REST+SSE API at `/api/v1/...` wrapping Temporal SDK calls (queries, signals, workflow listing)
2. A pre-built React+TypeScript SPA as embedded static files (`//go:embed web/dist`)

Connects to Temporal via `TEMPORAL_ADDRESS` env var — same pattern as the worker. No new persistence layer; Temporal is the source of truth.

```
fleetlift/
├── cmd/server/          # Go API server + static file server
├── web/                 # React + TypeScript SPA (Vite)
│   ├── src/
│   │   ├── pages/       # Inbox, TaskDetail, TaskList, Reports
│   │   ├── components/  # DiffViewer, SteeringPanel, StatusBadge, GroupProgress
│   │   └── api/         # Typed fetch wrappers
│   ├── package.json
│   └── vite.config.ts
└── internal/
    └── server/          # HTTP handlers, SSE, Temporal client wrappers
```

`make build` builds the frontend first, then the Go binary embeds `web/dist/`.

---

## Pages

### 1. Inbox (`/`) — landing page

A prioritized list of tasks needing human action. Inbox types:

| Priority | Type | Trigger |
|----------|------|---------|
| 1 | Awaiting approval | Agent finished; diff ready to review |
| 2 | Paused on threshold | Grouped execution exceeded failure % |
| 3 | Steering requested | Agent stuck or failed verifiers |
| 4 | Completed, needs review | Finished tasks with PRs/reports |

Each item shows: task title, status badge, repo count, time waiting. Click → Task Detail.

### 2. Task Detail (`/tasks/:workflowId`)

The core screen. Tabs:

- **Diff**: Unified diff per repo/file (`react-diff-viewer-continued`). Approve / Reject / Steer actions inline.
- **Verifier logs**: Pass/fail per verifier with collapsible stdout.
- **Group progress** (grouped tasks): list of groups with status, failure %, continue/abort controls.
- **Reports** (report-mode tasks): frontmatter as structured table + markdown body.

**Steering panel** (when awaiting approval): text input to send a steering prompt. Steering state (iteration count, history) shown alongside.

### 3. Task list (`/tasks`)

All tasks with status filter (running / awaiting approval / completed / failed) and search. Not just inbox items.

### 4. Reports (`/reports/:workflowId`)

Dedicated view for report-mode tasks. Table of repos/targets with frontmatter data columns, expandable markdown body per row. JSON export button.

---

## API

All endpoints under `/api/v1/`.

### Query endpoints (Temporal SDK queries)

| Method | Path | Temporal call |
|--------|------|---------------|
| `GET` | `/api/v1/tasks` | `ListWorkflows` |
| `GET` | `/api/v1/tasks/inbox` | `ListWorkflows` + `QueryStatus` per result |
| `GET` | `/api/v1/tasks/:id` | `DescribeWorkflow` + `QueryStatus` |
| `GET` | `/api/v1/tasks/:id/diff` | `QueryWorkflow("query_diff")` |
| `GET` | `/api/v1/tasks/:id/logs` | `QueryWorkflow("query_verifier_logs")` |
| `GET` | `/api/v1/tasks/:id/steering` | `QueryWorkflow("query_steering_state")` |
| `GET` | `/api/v1/tasks/:id/progress` | `QueryWorkflow("query_execution_progress")` |
| `GET` | `/api/v1/tasks/:id/reports` | `QueryWorkflow` + report data from result |

### Signal endpoints (Temporal SDK signals)

| Method | Path | Temporal call |
|--------|------|---------------|
| `POST` | `/api/v1/tasks/:id/approve` | `SignalWorkflow("approve")` |
| `POST` | `/api/v1/tasks/:id/reject` | `SignalWorkflow("reject")` |
| `POST` | `/api/v1/tasks/:id/cancel` | `SignalWorkflow("cancel")` |
| `POST` | `/api/v1/tasks/:id/steer` | `SignalWorkflow("steer", {prompt})` |
| `POST` | `/api/v1/tasks/:id/continue` | `SignalWorkflow("continue", {skip_remaining})` |

### Live updates

`GET /api/v1/tasks/:id/events` — Server-Sent Events stream. Server polls Temporal queries every 2s and pushes `status`, `progress`, and `steering_state` changes. Native `EventSource` reconnect handles drops.

### Auth

Out of scope for now (trusted internal network assumed). Optional: `FLEETLIFT_API_TOKEN` bearer token env var as a thin gate.

---

## Data Flow

### Inbox

```
Browser → GET /api/v1/tasks/inbox
Server  → Temporal.ListWorkflows (filter: running/paused/awaiting_approval)
        → QueryStatus per workflow to classify inbox type
        → Return sorted list (priority: approval > paused > steering > review)
```

### Diff review + approval

```
User opens task → GET /api/v1/tasks/:id/diff
               → Temporal QueryDiff → []model.DiffOutput
DiffViewer renders per-file unified diffs
User clicks Approve → POST /api/v1/tasks/:id/approve
                    → Temporal SignalWorkflow("approve")
                    → UI: "Approved — PRs being created..."
SSE stream picks up status change, updates header badge
```

### Steering

```
User types prompt → POST /api/v1/tasks/:id/steer {prompt: "..."}
                 → Temporal SignalWorkflow("steer", prompt)
                 → UI switches to "Agent running..." loading state
SSE stream pushes status change + new steering iteration when agent completes
User can review new diff, steer again, or approve
```

---

## Frontend Stack

| Concern | Library |
|---------|---------|
| Framework | React 18 + TypeScript |
| Build | Vite |
| Routing | react-router-dom v6 |
| Data fetching | @tanstack/react-query |
| UI components | shadcn/ui + Tailwind CSS |
| Diff viewer | react-diff-viewer-continued |
| SSE client | Native `EventSource` |

---

## Error Handling

- Temporal errors → `{error: "..."}` JSON with appropriate HTTP status
- SSE disconnect → `EventSource` auto-reconnects
- Optimistic UI for approve/reject with rollback on HTTP error
- Inbox polling degrades gracefully if Temporal is unreachable (shows last-known state + banner)

---

## Out of Scope

- Authentication / authorization (beyond optional bearer token gate)
- Task submission form (Phase 11 / NL task creation handles this)
- Knowledge management UI (Phase 10)
- Mobile layout
