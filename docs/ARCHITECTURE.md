# Architecture

## System overview

Fleetlift has four deployed processes and one SPA served from the API server:

| Component | Binary | Responsibility |
|-----------|--------|---------------|
| **API server** | `cmd/server` | REST API, GitHub OAuth, JWT auth, SSE streaming, serves embedded React SPA |
| **Worker** | `cmd/worker` | Temporal worker — registers and executes DAGWorkflow, StepWorkflow, and all activities |
| **CLI** | `cmd/cli` (binary: `fleetlift`) | Local developer interface — submit runs, inspect status, approve/steer steps |
| **Web UI** | `web/` (embedded in server) | React 19 SPA; real-time updates via SSE |
| **MCP sidecar** *(optional)* | `FLEETLIFT_MCP_BINARY_PATH` | Structured tool API between the worker and agent during step execution |

External dependencies:

- **Temporal** — durable workflow engine (gRPC, default `localhost:7233`)
- **PostgreSQL** — persistent state for runs, steps, inbox, reports, credentials
- **OpenSandbox** — on-demand container sandboxes where agents execute
- **GitHub** — OAuth provider; PR destination
- **MCP sidecar** *(optional)* — structured tool interface between worker and agent; enabled by setting `FLEETLIFT_MCP_BINARY_PATH` to the binary prefix (arch suffix appended at runtime)

---

## Component diagram

```
  ┌──────────┐   REST/SSE   ┌──────────────────────────────────────────┐
  │   CLI    │◄────────────►│               API Server                  │
  └──────────┘              │  chi router · JWT middleware · handlers   │
                            │  /api/workflows /api/runs /api/inbox ...  │
  ┌──────────┐   REST/SSE   │  /auth/github  /auth/github/callback     │
  │  Web UI  │◄────────────►│  /* (embedded React SPA)                  │
  │ (React)  │              └──────────────┬───────────────────────────┘
  └──────────┘                             │ Temporal SDK (start/signal)
                                           ▼
                            ┌──────────────────────────┐
                            │       Temporal Server     │
                            │   (workflow state store)  │
                            └──────────────┬────────────┘
                                           │ task queue: fleetlift
                                           ▼
                            ┌──────────────────────────┐
                            │          Worker           │
                            │  DAGWorkflow              │
                            │  StepWorkflow             │
                            │  Activities (agent, PR,   │
                            │   sandbox, slack, github) │
                            └───────┬──────────┬────────┘
                                    │ REST      │ REST (optional)
                                    ▼           ▼
                    ┌───────────────────┐  ┌──────────────────────┐
                    │   OpenSandbox     │  │    MCP Sidecar       │
                    │  Ephemeral        │  │  Structured tool API │
                    │  containers       │  │  for agents          │
                    │  per step;        │  │  (enabled via        │
                    │  agent runs       │  │  FLEETLIFT_MCP_      │
                    │  inside           │  │  BINARY_PATH)        │
                    └───────────────────┘  └──────────────────────┘
```

State storage:

```
PostgreSQL
  ├── teams           (multi-tenancy)
  ├── users           (GitHub identity + team membership)
  ├── workflow_templates (team-owned YAML definitions)
  ├── runs            (top-level run state)
  ├── step_runs       (per-step state, logs, diff, PR URL)
  ├── inbox_items     (HITL notifications)
  ├── reports         (structured step output)
  └── credentials     (AES-256-GCM encrypted secrets)
```

---

## DAGWorkflow execution model

`DAGWorkflow` (in `internal/workflow/dag.go`) implements a topological scheduler:

1. **Inputs**: `RunID`, `WorkflowDef` (parsed YAML), `Parameters` (map supplied by caller)
2. **Loop**: Find all steps whose `depends_on` are satisfied and not yet started → launch them in parallel as goroutines via Temporal's `workflow.Go`
3. **Sandbox groups**: If a step declares `sandbox_group`, provision one shared sandbox for all steps in that group before executing them
4. **Template resolution**: Field values in `StepDef` that contain `{{ ... }}` are rendered with Go `text/template` using `.Params` and `.Steps` (completed step outputs)
5. **Conditions**: Steps with a `condition` field are skipped if the expression evaluates to non-`true`
6. **StepWorkflow**: Each ready step is dispatched as a child `StepWorkflow`, which executes the actual agent activity
7. **Deadlock detection**: If no steps are ready but some remain pending, the workflow fails with a deadlock error

`StepWorkflow` handles:
- Provisioning / reusing sandbox
- Running the agent (ClaudeCodeRunner → OpenSandbox REST API)
- Waiting for HITL approval signals (`approve`, `reject`, `steer`)
- Persisting logs, diffs, and structured output to PostgreSQL

---

## Request flow

### Triggering a run

```
CLI: fleetlift run start <workflow-id> --param k=v
  → POST /api/runs   (JWT auth header)
  → RunsHandler.Create
    → resolve WorkflowDef from template registry
    → insert run row in PostgreSQL
    → temporal.ExecuteWorkflow(DAGWorkflow, {RunID, WorkflowDef, Params})
  ← 201 {id: "<run-id>"}
```

### Streaming logs

```
CLI: fleetlift run logs <id>   (or Web UI SSE subscription)
  → GET /api/runs/{id}/events
  → RunsHandler.Stream
    → polls step_runs table
    → pushes SSE events: log lines, status changes
  ← text/event-stream
```

### HITL approval

```
CLI: fleetlift run approve <id>
  → POST /api/runs/{id}/approve
  → RunsHandler.Approve
    → temporal.SignalWorkflow("approve", ...)
  ← 200

Worker (StepWorkflow):
  → waiting on workflow.GetSignalChannel("approve")
  → receives signal → proceeds to PR creation
```

### MCP tool calls (agent → worker)

When `FLEETLIFT_MCP_BINARY_PATH` is set, the worker provisions an MCP sidecar alongside each sandbox. The agent calls structured tools through the sidecar, which routes to the server's `/api/mcp/*` endpoints:

```
Agent (in sandbox) → MCP sidecar → POST /api/mcp/progress
                                  → POST /api/mcp/artifacts
                                  → GET  /api/mcp/knowledge
                                  → POST /api/mcp/inbox/notify
                                  → POST /api/mcp/inbox/request_input
```

MCP endpoints use a run-scoped JWT (separate from user JWTs) validated by `auth.MCPAuth`.

---

## Multi-tenancy

Every API resource is scoped to a `team_id`. The JWT payload carries `team_id` and `user_id`. The auth middleware extracts and injects these into the request context; all handlers enforce team isolation.

Teams are created by an internal admin flow (not exposed via public API in v2). Users join teams via GitHub OAuth — the callback handler looks up or creates a user and associates them with their team.

---

## Auth

- **GitHub OAuth**: user visits `/auth/github` → redirected to GitHub → callback at `/auth/github/callback` → server exchanges code for GitHub token, fetches user profile, upserts user row, issues JWT
- **JWT**: HS256, signed with `JWT_SECRET`, carries `user_id` and `team_id`. Short-lived; refreshed via `POST /auth/refresh`
- **Middleware**: `auth.Middleware` validates the `Authorization: Bearer <token>` header on all `/api/*` routes

---

## Knowledge loop

The knowledge system captures agent-generated insights and injects them into future runs:

```
Agent run completes
  → StepWorkflow extracts lessons (if knowledge.capture = true)
  → Inserts knowledge items with status = "pending"

Operator reviews via Inbox / CLI
  → Approves or rejects items

Future step with knowledge.inject = true
  → ExecutionDef prompt is enriched with approved knowledge items
     matching the step's knowledge.tags
```

---

## SSE streaming

The server pushes two event types on `GET /api/runs/{id}/events`:

| Event type | Payload | When |
|-----------|---------|------|
| `log` | `{content: string, stream: "stdout"|"stderr"}` | Each log line from the agent |
| `status` | `{status: "pending"|"running"|"awaiting_input"|"complete"|"failed"|"cancelled"}` | On step state change |

The CLI's `run logs` command and the web UI both consume this stream. The stream ends when the run reaches a terminal state (`complete`, `failed`, or `cancelled`).
