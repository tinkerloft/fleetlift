# MCP Sidecar Integration Test

## Goal

Verify the full MCP sidecar round-trip in a real OpenSandbox container: binary upload, sidecar startup, health check, all 7 backend API endpoints, and optionally Claude Code MCP discovery + tool usage.

## Approach

A new `mcp-test` builtin workflow with two steps:

1. **Shell step** — curls the 7 MCP backend endpoints directly from inside the sandbox using the injected MCP JWT. Deterministic, no AI tokens, verifies the full provisioning + API chain.
2. **Agent step (optional)** — prompts Claude Code to use `progress.update` and `artifact.create` via MCP. Verifies MCP discovery config injection and end-to-end agent→sidecar→backend flow.

A test runner script (`scripts/integration/run-mcp-test.sh`) submits the workflow, polls Temporal, and asserts on DB state.

## Prerequisite: Token Injection

Currently `ProvisionSandbox` passes `FLEETLIFT_MCP_TOKEN` only as an inline env var on the sidecar's `nohup` command — it is NOT available to other processes in the sandbox. The shell test step needs the token to call the backend API directly.

**Required change:** Extend the `/etc/profile.d/fleetlift-mcp.sh` write in `ProvisionSandbox` to also export `FLEETLIFT_MCP_TOKEN`:

```bash
echo 'export FLEETLIFT_MCP_PORT=8081' >> /etc/profile.d/fleetlift-mcp.sh
echo 'export FLEETLIFT_MCP_TOKEN=<token>' >> /etc/profile.d/fleetlift-mcp.sh
```

This makes the token available to any process that sources the profile (shell steps, agent runners).

## Components

### 1. Workflow Template: `mcp-test.yaml`

Location: `internal/template/workflows/mcp-test.yaml`

Parameters:
- `include_agent_step` (bool, default: false) — whether to run the Claude Code step

Steps:

**Step 1: `verify_mcp_endpoints`**
- Agent: `shell`
- Mode: `report`
- Prompt: a self-contained bash script that:
  - Sources `/etc/profile.d/fleetlift-mcp.sh` to get `FLEETLIFT_MCP_PORT` and `FLEETLIFT_MCP_TOKEN`
  - Validates both vars are non-empty (exits with diagnostic if not)
  - Sets `AUTH="Authorization: Bearer $FLEETLIFT_MCP_TOKEN"` and `API="http://host.docker.internal:8080/api/mcp"` (note: `host.docker.internal` works on Docker Desktop; on Linux Docker, set `FLEETLIFT_API_URL` to `http://172.17.0.1:8080` or use `--add-host=host.docker.internal:host-gateway`)
  - Hits sidecar health: `curl -sf http://localhost:$FLEETLIFT_MCP_PORT/health` (retries 5x, 1s sleep)
  - Calls all 7 backend endpoints with `-H "$AUTH"`. Progress and artifact calls are placed early (before any long-running work) to avoid a timing race where the step transitions to `complete` between the MCPAuth middleware check and the `activeStepRunID` lookup:
    1. `POST $API/progress` with `{"percentage":25,"message":"mcp-test-shell"}` → assert 200
    2. `POST $API/artifacts` with `{"name":"mcp-test-shell.txt","content":"hello from shell"}` → assert 201, body contains `artifact_id`
    3. `GET $API/run` → assert HTTP 200, body contains `run_id`
    4. `POST $API/knowledge` with `{"type":"pattern","summary":"mcp-test-learning"}` → assert 201
    5. `GET $API/knowledge/search?q=mcp-test` → assert 200 (note: returns empty items since knowledge is `pending`, not `approved` — this only validates auth + routing)
    6. `GET $API/knowledge?max=5` → assert 200 (same caveat: needs workflow_id from run, returns approved items only)
    7. `GET $API/steps/nonexistent/output` → assert 404 (expected, no prior step)
  - Prints `PASS`/`FAIL` per endpoint
  - Exits non-zero on any unexpected status

**Step 2: `agent_uses_mcp`**
- Agent: `shell` (wrapper — see Conditional Step section below)
- Mode: `report`
- `depends_on: [verify_mcp_endpoints]`
- When `include_agent_step` is true: runs Claude Code via `claude` CLI with a prompt instructing it to use MCP tools
- When false: prints "skipped" and exits 0

Prompt for the agent (when enabled): "You have MCP tools available from the fleetlift server. Do the following in order: (1) Use the progress.update tool to report 75% progress with message 'agent-mcp-test'. (2) Use the artifact.create tool to create an artifact named 'mcp-test-agent.txt' with content 'hello from agent mcp'. (3) Print DONE."

### 2. Test Runner Script: `run-mcp-test.sh`

Location: `scripts/integration/run-mcp-test.sh`

Flow:
1. Source `dev-env.sh`
2. Check prerequisites:
   - `bin/fleetlift-mcp` exists and is an ELF binary (`file bin/fleetlift-mcp | grep -q ELF`), not a macOS Mach-O. If missing or wrong arch, print: `Run: GOOS=linux GOARCH=amd64 go build -o bin/fleetlift-mcp ./cmd/mcp-sidecar`
   - `FLEETLIFT_MCP_BINARY_PATH` is set in the environment (the worker reads this at activity time, not startup). Print a reminder to export it.
   - Worker and server are running (check PID files)
3. Generate user JWT, get team/user IDs from DB
4. Parse `--with-agent` flag to set `include_agent_step`
5. Submit `mcp-test` workflow via `POST /api/runs`
6. Poll Temporal for completion:
   - 60s timeout for shell-only
   - 120s timeout with `--with-agent`
7. Query DB to verify shell step:
   - `step_runs WHERE step_id = 'verify_mcp_endpoints'`: status = `complete`
   - `artifacts` table: row with `name = 'mcp-test-shell.txt'` for this run's step_run
   - `knowledge_items` table: row with `summary = 'mcp-test-learning'` for this team (note: `knowledge_items` is scoped by `team_id`/`workflow_template_id`, not `run_id`)
   - Note: progress (`percentage: 25`) is stored via `jsonb_set` on step_runs.output under the `progress` key. However, the final activity output write may overwrite this. The test should check `step_runs.output->'progress'->>'percentage'` and accept either `25` or NULL (if overwritten). The primary progress verification is that `POST /progress` returned 200 during execution.
8. If `--with-agent`:
   - `step_runs WHERE step_id = 'agent_uses_mcp'`: status = `complete`
   - `artifacts` table: row with `name = 'mcp-test-agent.txt'`
   - Same caveat for progress check
9. Print summary: PASSED / FAILED with per-check details

### 3. Environment Requirements

No new infra. Uses existing stack:
- Docker Compose: Temporal + Postgres + OpenSandbox
- Worker env vars needed at activity runtime:
  - `FLEETLIFT_MCP_BINARY_PATH=$PROJECT_ROOT/bin/fleetlift-mcp`
  - `JWT_SECRET` (already in dev-env.sh)
- Build command: `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/fleetlift-mcp ./cmd/mcp-sidecar`
- The existing `make build` target already cross-compiles for linux/amd64. Verify with `file bin/fleetlift-mcp` → should show `ELF 64-bit`.

### 4. Conditional Step Implementation

The agent step uses `agent: shell` as a wrapper. The prompt is a bash script that checks the `include_agent_step` parameter:

```bash
if [ "{{ .Params.include_agent_step }}" != "true" ]; then
  echo "agent step skipped (include_agent_step != true)"
  exit 0
fi

# Source MCP env for claude to discover
. /etc/profile.d/fleetlift-mcp.sh 2>/dev/null

# Write MCP config for Claude Code discovery
if [ -n "$FLEETLIFT_MCP_PORT" ]; then
  printf '{"mcpServers":{"fleetlift":{"type":"sse","url":"http://localhost:%s/sse"}}}' "$FLEETLIFT_MCP_PORT" > /workspace/.mcp.json
fi

cd /workspace
claude -p "You have MCP tools available... (prompt)" \
  --output-format stream-json --dangerously-skip-permissions --max-turns 10
```

This avoids sending AI prompts to `claude-code` agent type (which would try to parse the if-statement as an instruction). The shell wrapper gives us full control over the skip logic and MCP config setup.

**Why not `agent: claude-code`:** The template engine sends the prompt as-is to the agent runner. A bash if-guard would be interpreted as text by Claude Code, not executed. Using `agent: shell` with explicit `claude` CLI invocation gives deterministic skip behavior.

### 5. Error Handling

- Shell step: `set -euo pipefail` ensures any curl failure stops execution
- Each curl call captures HTTP status via `curl -w '%{http_code}' -o /tmp/response.json -s` and asserts explicitly
- Sidecar health check retries 5 times with 1s sleep before giving up
- If `FLEETLIFT_MCP_TOKEN` is empty after sourcing profile.d, step fails immediately: `echo "FAIL: FLEETLIFT_MCP_TOKEN not set — ProvisionSandbox did not inject token"; exit 1`
- If `FLEETLIFT_MCP_PORT` is empty, step fails with: `echo "FAIL: FLEETLIFT_MCP_PORT not set — profile.d write failed"; exit 1`
- Test script timeout: hard kill after 60s/120s, reports TIMEOUT

### 6. What This Verifies

| Layer | Verified By |
|-------|-------------|
| ProvisionSandbox binary upload | Step 1 health check (sidecar is running) |
| ProvisionSandbox token injection | Step 1 reads `$FLEETLIFT_MCP_TOKEN` from profile.d |
| ProvisionSandbox port injection | Step 1 reads `$FLEETLIFT_MCP_PORT` from profile.d |
| MCPAuth middleware | All 7 curl calls include Bearer token, get non-401 responses |
| Run-liveness check | Middleware allows calls during active run |
| HandleGetRun | Step 1 curl #1 (200 + run_id) |
| HandleUpdateProgress | Step 1 curl #2 (200) |
| HandleCreateArtifact | Step 1 curl #3 (201 + artifact_id) + DB row |
| HandleAddLearning | Step 1 curl #4 (201) + DB row |
| HandleSearchKnowledge | Step 1 curl #5 (200, auth/routing only — no approved items) |
| HandleGetKnowledge | Step 1 curl #6 (200, auth/routing only) |
| HandleGetStepOutput | Step 1 curl #7 (404 expected) |
| Claude Code MCP discovery | Step 2: agent finds `.mcp.json`, connects to sidecar |
| Agent → sidecar → backend | Step 2: artifact + progress verified in DB |
