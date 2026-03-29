# Claude Code Instructions

Project-specific instructions for Claude Code when working on this repository.

## Pre-Commit Checks

When fixing bugs or implementing features, always run the full test suite and ensure compilation passes before committing. For Go projects, run `go build -buildvcs=false ./...` and `go test -buildvcs=false ./...` before every commit. After changes that affect the running stack, also run `scripts/integration/smoke-test.sh api cli` as a quick sanity check.

## Local Server Operations

Use the scripts in `scripts/integration/` to manage the local dev environment:

- `scripts/integration/start.sh [--build]` — start worker + server (build first if `--build`)
- `scripts/integration/restart.sh` — rebuild binaries and restart both processes
- `scripts/integration/stop.sh` — stop worker and server
- `scripts/integration/logs.sh` — tail worker and server logs
- `scripts/integration/status.sh` — check if processes are running

Prerequisites: `docker compose up -d` (Temporal + Postgres + OpenSandbox) must be running.

Logs are written to `/tmp/fleetlift-worker.log` and `/tmp/fleetlift-server.log`.

## Smoke Tests

After any significant change, run the smoke test suite to verify the stack is functional:

```bash
scripts/integration/smoke-test.sh              # all layers (api, cli, workflows, web)
scripts/integration/smoke-test.sh api cli      # quick: API + CLI only (~30s)
scripts/integration/smoke-test.sh workflows    # workflow E2E (Tier 1 + Tier 2 if GITHUB_TOKEN exists)
scripts/integration/run-sse-test.sh            # SSE streaming diagnostic (Worker→DB, DB→SSE)
```

Individual quick checks (standalone, predate the smoke suite):
- `scripts/integration/run-sandbox-test.sh` — sandbox lifecycle + step output passing
- `scripts/integration/run-mcp-test.sh` — MCP sidecar endpoints + agent tool calls
- `scripts/integration/run-profile-test.sh` — agent profile CRUD + workflow E2E

Workflow test tiers:
- **Tier 1** (always): sandbox-test, profile-test, mcp-test — no external credentials
- **Tier 2** (auto-detected): clone-test, triage — requires `GITHUB_TOKEN` in credential store
- **Tier 2 opt-in**: `SMOKE_PR_REVIEW=1` (pr-review, slow), `SMOKE_BUG_FIX=1` (bug-fix, slow)
- **Tier 3** (opt-in): `SMOKE_TIER3=1` — fleet-research, fleet-transform, audit, etc.

## Before Completing Any Task

**Required checks before marking work complete:**

0. **Simplify**: Run `/simplify` to review changed code for reuse, quality, and efficiency
1. **Build**: `go build -buildvcs=false ./...` (use `-buildvcs=false` in worktrees)
2. **Unit tests**: `go test -buildvcs=false ./...`
3. **Linter**: `make lint` (requires `golangci-lint` — skip if not installed but don't suppress real issues)
4. **Smoke tests**: `scripts/integration/smoke-test.sh api cli` (quick sanity check against running stack)

## Workflow & Activity Pre-merge Checklist

For any new or modified workflow, activity, or sandbox integration code, verify all four:

- [ ] **Failure propagation** — every failure path sets an explicit error return or non-success status. No function falls through to a default "success" result when subordinate work has failed. Step failed → run must fail. Clone failed → activity must error.
- [ ] **Bounded retries** — every `workflow.ActivityOptions` that touches external state (DB, APIs, sandbox) has an explicit `RetryPolicy` with `MaximumAttempts`. Use `dbRetry` (5) for DB writes, 2–3 for execution steps. Unlimited retries on a permanent error (type mismatch, wrong field name, missing resource) means the workflow never terminates.
- [ ] **Integration contract verification** — never trust HTTP 200 = success for external calls. Check that fields sent to external APIs are actually the names those APIs expect (consult the spec/source). Verify side effects (e.g. `.git/HEAD` after clone) rather than assuming the call succeeded.
- [ ] **Streaming buffer sizes** — any `bufio.Scanner` or similar reader over agent/tool output must set an explicit buffer size (`scanner.Buffer(..., 4*1024*1024)`). The 64 KB default is too small for large file reads or diffs output as single JSON lines.

## Project Structure

- `cmd/cli/` - cobra CLI (`fleetlift` binary)
- `cmd/worker/` - Temporal worker (registers DAGWorkflow, StepWorkflow, all activities)
- `cmd/server/` - REST API + SSE server (chi, port 8080)
- `internal/activity/` - Temporal activity implementations
- `internal/agent/` - AgentRunner interface + ClaudeCodeRunner
- `internal/auth/` - JWT, GitHub OAuth, HTTP middleware
- `internal/db/` - PostgreSQL connection helper + schema
- `internal/knowledge/` - Knowledge store (DB-backed, wired into MCP sidecar endpoints)
- `internal/logging/` - slog adapter
- `internal/metrics/` - Prometheus interceptor
- `internal/model/` - All entity types (Run, StepRun, WorkflowTemplate, etc.)
- `internal/sandbox/` - sandbox.Client interface + opensandbox/ REST implementation
- `internal/server/` - chi router + handlers (auth, workflows, runs, inbox, reports, credentials)
- `internal/template/` - BuiltinProvider, DBProvider, Registry, RenderPrompt; 15 builtin YAML workflows
- `internal/workflow/` - DAGWorkflow + StepWorkflow (Temporal)
- `web/` - React 19 + TypeScript + Vite SPA (embedded in server binary via web/embed.go)
- `docs/plans/` - Design doc and implementation plan

## Credential Management

Claude auth (`ANTHROPIC_API_KEY` or `CLAUDE_CODE_OAUTH_TOKEN`) and `GITHUB_TOKEN` are stored as **encrypted team credentials in the database**, not as host environment variables.

- `ProvisionSandbox` always calls `resolveClaudeAuth` to inject auth into every sandbox — no agent-type or image-name gating
- `init-local` wizard prompts for credentials and stores them via `seedCredential()` in the DB
- To add/update a credential manually: `POST /api/credentials` with `{"name":"GITHUB_TOKEN","value":"ghp_..."}`
- Host env vars like `CLAUDE_CODE_OAUTH_TOKEN` in `~/.fleetlift/local.env` are **not** used by the worker — only the DB credential store is checked

## Sandbox Provisioning

- `ProvisionSandbox` runs **once per sandbox group** at the DAG level, using the agent/credentials of the *first step* that triggers it. If step 1 is `agent: shell` and step 2 is `agent: claude-code` in the same `sandbox_group`, auth must already be present from provisioning time.
- The `claude-code-sandbox:latest` image must be built locally: `make sandbox-build`. It is not pulled from a registry.
- `docker/Dockerfile.sandbox` build context is `docker/` — `COPY` paths are relative to that directory, not the repo root.
- The `profile-test` workflow requires an `e2e-profile-test` agent profile to exist before dispatch — it is not self-contained. The smoke test suite and `run-profile-test.sh` both create this profile as setup.

## Docker & Sandbox Guidelines

When working with Docker containers and sandboxes:

1. Never assume tools like `git`, `curl`, or `psql` are installed in a container — check first with `command -v`
2. Containers may run as non-root (e.g. the `agent` user in `claude-code-sandbox`) — don't write to `/root/`
3. Verify container names from `docker compose` config rather than guessing — use `docker ps` or the helpers in `dev-env.sh`
4. Wait for service readiness before proceeding — use health checks or polling loops, not fixed `sleep` delays

## Docker Networking (Linux)

- `host.docker.internal` does **not** resolve inside containers on Linux (only Docker Desktop on macOS/Windows)
- `dev-env.sh` auto-detects the Docker bridge gateway IP and sets `FLEETLIFT_API_URL` so the MCP sidecar can reach the host server
- The MCP test workflow reads `FLEETLIFT_API_URL` from `/tmp/fleetlift-mcp-env.sh` inside the sandbox

## OpenSandbox Version Pinning

The `docker-compose.yaml` pins specific OpenSandbox versions. Do not change these without testing streaming behavior:

- `opensandbox/server:v0.1.9` — has `networkPolicy` REST field used by egress policy
- `opensandbox/execd:v1.0.9` — fixes ExecStream framing; different versions change output buffering
- `opensandbox/egress:v1.0.4` — matches execd compatibility

If you change execd versions, verify with: `scripts/integration/run-sse-test.sh` (tests incremental log delivery).

## Shell Script Safety (`set -euo pipefail`)

All integration scripts use `set -euo pipefail`. This has critical implications:

- Any function that can return non-zero (e.g. `fl_sql` when no rows match) **must** have `|| true` when called inside `$()` substitutions, or `set -e` silently kills the script with no output
- `dev-env.sh` is sourced by every script — any failing command in it kills the caller. Always use `|| true` for optional commands like `docker network inspect`
- When writing new integration scripts, test them from a clean shell (not your interactive session which may have different env vars set)

## Development Workflow

When making changes to fleetlift:

1. **Start the stack**: `docker compose up -d && scripts/integration/start.sh --build`
2. **Make changes** to Go code, workflow YAMLs, or frontend
3. **Rebuild + restart**: `scripts/integration/restart.sh` (rebuilds all binaries + web, restarts worker + server)
4. **Quick validation**: `scripts/integration/smoke-test.sh api cli` (~30s, tests API endpoints + CLI commands)
5. **Workflow validation**: `scripts/integration/smoke-test.sh workflows` (runs Tier 1 diagnostic workflows + Tier 2 if GITHUB_TOKEN exists)
6. **After sandbox/streaming changes**: `scripts/integration/run-sse-test.sh` (verifies log insertion + SSE delivery)
7. **After frontend changes**: `scripts/integration/smoke-test.sh web` (Playwright navigation + interaction tests)

For changes to `ProvisionSandbox`, credential handling, or MCP sidecar code, always run the full workflow tier:
```bash
scripts/integration/smoke-test.sh workflows
```

For changes to `dev-env.sh` or other shell scripts, test from a fresh shell — your interactive session may mask `set -e` failures due to env vars already being set.

## Code Review

When doing code reviews, verify claims against actual code before presenting findings. If uncertain about error handling or behavior, read the relevant code paths rather than guessing. Do not report issues that don't exist.

## Git Workflow

When working with worktrees, always verify you are on the correct branch and in the correct worktree directory before committing. Run `git branch --show-current` and `pwd` to confirm.

## Known Issues

- `POST /api/workflows/{id}/fork` returns 500 — fork endpoint is broken
- CLI `credential list` has a JSON unmarshalling bug (expects `{items:[]}`, server returns `[]`)
- Frontend SSE log streaming: `LogStream` component shows "Waiting for logs..." during execution — backend SSE is confirmed working (verified via curl + `run-sse-test.sh`). SSE auth is handled via the `fl_token` HttpOnly cookie set during OAuth callback and dev-login; the auth middleware (`internal/auth/middleware.go`) accepts cookie auth for GET/HEAD/OPTIONS requests, which covers EventSource connections. If logs still don't appear, check that the `fl_token` cookie is being set (OAuth flow or dev-login must complete before SSE connections will authenticate).
- `HeartbeatTimeout: 2m` on `ExecuteStep` can be too short for large PRs where Claude thinks for extended periods without producing output events

## Key Conventions

- Use Temporal SDK patterns for activities and workflows
- Register new activities/workflows in `cmd/worker/main.go`
- Add activity name constants to `internal/activity/constants.go`
- Update the current implementation plan document when completing phases

## Naming Conventions

For MCP tool references in workflow YAML prompts, always use the full prefixed name format `mcp__fleetlift__<category>__<tool>` (e.g. `mcp__fleetlift__memory__search`, `mcp__fleetlift__artifact__create`), never short names. Check existing usage in `internal/template/workflows/*.yaml` for consistency before adding new references.

## Temporal Workflow Rules (STRICT)

**Never use these in `internal/workflow/*.go` workflow functions** — they cause non-determinism on replay:
- `slog.*`, `fmt.Print*`, `log.*` → use `workflow.GetLogger(ctx)` instead
- `time.Now()` → use `workflow.Now(ctx)`
- `math/rand` → use `workflow.SideEffect`
- Iterating a `map` while calling `workflow.ExecuteActivity` or `workflow.Go` → collect keys into a slice, `sort.Strings()`, then iterate

Activities (`internal/activity/`) are exempt — they run outside the determinism sandbox.

## Security Rules

- **Input validation:** Validate all user-supplied values at trust boundaries before use in shell commands, SQL, or file paths:
  - Repo URLs must use `https://` scheme only — reject `file://`, `git://`, `ssh://`
  - Credential names used as env vars must match `^[A-Z][A-Z0-9_]*$` and must not be reserved names (`PATH`, `LD_PRELOAD`, etc.)
  - Artifact/file paths must be validated to stay within `/workspace/` (no `..`)
- **No hardcoded credentials:** Never add fallback values for `DATABASE_URL` or other secrets. Fail fast with a clear error if required env vars are absent.
- **Multi-tenant isolation:** Never use Go map iteration to select a team ID. Always require an explicit `X-Team-ID` header or `?team_id=` param, validated against JWT claims.
- **State ownership:** Run/step status transitions must be performed by Temporal activities inside the workflow — not by HTTP handlers after `ExecuteWorkflow()` returns.

## Test Coverage Requirements

Beyond general unit tests, these specific areas **must** have tests before merge:
- `internal/auth/middleware.go` — auth middleware, SSE ticket lifecycle
- `internal/server/handlers/auth.go` — OAuth CSRF state validation
- Any new encryption or credential handling code
- New Temporal workflows must have at least one `go.temporal.io/sdk/testsuite` test

## Frontend Rules

- All `fetch`/promise chains must have a `.catch()` handler or be wrapped in `try/catch` — no silent failures
- Never call `res.json()` without first checking `res.status !== 204` and `res.headers.get('content-length') !== '0'`
- DELETE endpoints return 204 No Content — callers must not attempt to parse the response body

## Database Migrations

All schema changes **must** be encoded as versioned migration files — never applied manually or via raw SQL in application code.

- Migration files live in `internal/db/migrations/` and follow the pattern `NNN_description.up.sql` (and optionally `.down.sql`)
- golang-migrate v4 applies them automatically at server and worker startup via `db.Migrate()` in `internal/db/db.go`
- To add a new migration: create `NNN_description.up.sql` with the next version number, rebuild — no other changes required
- `internal/db/schema.sql` is a **reference only** (migration 001 baseline); it is not executed at runtime

## Go Conventions

- Always initialize slices with `make([]Type, 0)` instead of `var x []Type` — nil slices marshal as JSON `null` which crashes the frontend
- Nullable database columns (`UUID REFERENCES ... ON DELETE SET NULL`) must use pointer types (`*string`) in model structs, not bare `string` — sqlx cannot scan `NULL` into a `string`
- Use `go build -buildvcs=false ./...` in worktrees (VCS stamping fails outside the main repo)

## Go + PostgreSQL Type Rules

- `[]string` fields **cannot** scan PostgreSQL `TEXT[]` columns — use `pq.StringArray` (from `github.com/lib/pq`)
- `map[string]any` fields **cannot** scan `JSONB` columns — use the project's `JSONMap` type from `internal/model/types.go`
- Nullable `UUID` columns must map to `*string` in Go model structs (not `string`) — see `KnowledgeItem.WorkflowTemplateID` for an example
- When in doubt, check `internal/model/types.go` for the canonical scan-safe types before adding new model fields

## Serialization Rules

- **Never** use `json.Unmarshal` to parse YAML content (workflow template bodies, config files). Use `yaml.Unmarshal` from `gopkg.in/yaml.v3`
- The two formats are not interchangeable — YAML parses successfully as JSON only for trivial inputs

## Shell Command Construction

- **Every** user-controlled string interpolated into a shell command must be wrapped with `shellQuote()` — repo URLs, branch names, commit messages, file paths, credential values, prompt text
- `shellQuote` is defined in `internal/agent/quote.go` and `internal/activity/util.go`
- Run `git grep shellQuote` before committing any file that constructs shell command strings

## Temporal Parent/Child Signal Routing

- HITL signals (`approve`, `reject`, `steer`) are registered on **child StepWorkflow** instances, not the parent DAGWorkflow
- Child workflow IDs: `{runID}-{stepID}` (single), `{runID}-{stepID}-{index}` (fan-out)
- When routing HITL signals: look up `step_id` from `step_runs WHERE status = 'awaiting_input'`, construct child ID — never signal the parent `temporal_id` directly for HITL
- Cancel signals target the parent DAGWorkflow (`temporal_id` on `runs`)

## Environment Variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `DATABASE_URL` | PostgreSQL DSN | `postgres://fleetlift:fleetlift@localhost:5432/fleetlift` |
| `TEMPORAL_ADDRESS` | Temporal server | `localhost:7233` |
| `OPENSANDBOX_DOMAIN` | OpenSandbox API base URL | — |
| `OPENSANDBOX_API_KEY` | OpenSandbox auth key | — |
| `AGENT_IMAGE` | Default sandbox image (Claude Code) | `claude-code-sandbox:latest` |
| `JWT_SECRET` | Server JWT signing key | — |
| `CREDENTIAL_ENCRYPTION_KEY` | 32-byte hex key for AES-256-GCM | — |
| `GITHUB_CLIENT_ID` | OAuth app client ID | — |
| `GITHUB_CLIENT_SECRET` | OAuth app client secret | — |
| `GIT_USER_EMAIL` | Git commit identity for agent | `claude-agent@noreply.localhost` |
| `GIT_USER_NAME` | Git commit identity for agent | `Claude Code Agent` |
| `FLEETLIFT_MCP_BINARY_PATH` | MCP sidecar binary path prefix (arch suffix appended at runtime, e.g. `-amd64`) | — |
| `FLEETLIFT_API_URL` | API URL reachable from inside sandbox containers | auto-detected by `dev-env.sh` |
