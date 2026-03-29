# Fleetlift E2E Smoke Test Suite — Spec

## Purpose

Validate that a running local fleetlift stack is fully functional after code changes. The suite is designed to catch regressions across the full surface area: API, CLI, workflow execution, and web UI.

## Prerequisites

- `docker compose up -d` (Temporal, Postgres, OpenSandbox)
- `scripts/integration/start.sh --build` (worker + server)
- `claude-code-sandbox:latest` Docker image built (`make sandbox-build`)
- Database seeded with dev team/user (via `fleetlift init-local` or prior test runs)

## Running

```bash
# Full suite
scripts/integration/smoke-test.sh

# Individual layers
scripts/integration/smoke-test.sh api
scripts/integration/smoke-test.sh cli
scripts/integration/smoke-test.sh workflows
scripts/integration/smoke-test.sh web

# Include expensive multi-repo workflows
SMOKE_TIER3=1 scripts/integration/smoke-test.sh workflows
```

## Architecture

```
smoke-test.sh                  ← top-level runner, selects layers
  ├── smoke-lib.sh             ← shared: auth, HTTP, assertions, polling
  ├── smoke/01-api.sh          ← API endpoint tests
  ├── smoke/02-cli.sh          ← CLI binary tests
  ├── smoke/03-workflows.sh    ← workflow execution E2E
  └── smoke/04-web-ui.sh       ← Playwright browser tests

run-sse-test.sh                ← standalone SSE streaming diagnostic (no Playwright dependency)
```

Each layer is independently runnable and reports pass/fail/skip counts. The runner collects results and exits non-zero if any layer has failures.

All layers share authentication via `smoke-lib.sh` which generates a dev JWT (same approach as existing `run-sandbox-test.sh`). No GitHub OAuth required — tests run against `DEV_NO_AUTH=1`.

---

## Layer 1: API Endpoints

Tests every major API resource via HTTP. Each test makes a request and asserts the HTTP status code and (where applicable) response body contents. Resources that support CRUD are tested with create → read → update → delete, with cleanup on exit.

| Area | What's Tested |
|------|--------------|
| **Health** | `GET /health` returns 200 |
| **Config** | `GET /api/config` returns `dev_no_auth` flag |
| **Identity** | `GET /api/me` returns current user |
| **Workflows** | List all templates; get a specific builtin (`sandbox-test`); get a non-existent slug returns 404; fork a builtin template; cleanup the fork |
| **Runs** | List runs returns 200; get a non-existent run returns 404 |
| **Credentials** | Create a credential; list includes it; delete it; list no longer includes it |
| **Knowledge** | List all items; list filtered by status (`?status=approved`) |
| **Inbox** | List inbox returns 200 |
| **Reports** | List reports returns 200 |
| **Agent Profiles** | Create profile; get by ID; update; delete |
| **Action Types** | List available action types |
| **SSE** | Connect to run event stream endpoint — verify it responds (200 or 404), not 500 |

**Not tested here:** Workflow submission, step execution, MCP endpoints (covered by workflow layer). OAuth login flow (requires GitHub).

---

## Layer 2: CLI Commands

Tests the `fleetlift` CLI binary against the running server. Uses `--server http://localhost:8080` and a dev auth token written to `~/.fleetlift/auth.json`.

| Command | What's Tested |
|---------|--------------|
| `workflow list` | Output includes known builtins: `sandbox-test`, `pr-review`, `bug-fix` |
| `workflow get sandbox-test` | Output includes title "Sandbox Test" |
| `workflow get nonexistent-wf` | Exits with non-zero status |
| `credential set` | Creates a credential; `credential list` shows it |
| `credential delete` | Removes it; `credential list` no longer shows it |
| `run start sandbox-test -p duration=3` | Starts a run; output includes run ID |
| `run list` | Succeeds (exit 0) |
| `run get <id>` | Shows run status for the run we just started |
| `knowledge list` | Succeeds (exit 0), even if empty |

**Not tested here:** `auth login/logout` (requires GitHub OAuth), `run logs` (SSE streaming — tested separately), `run approve/reject/steer` (requires HITL workflow).

---

## Layer 3: Workflow Execution E2E

Submits each built-in workflow via the API, waits for Temporal completion, and verifies step results in the database. Workflows are grouped into tiers based on credential requirements and cost.

### Tier 1 — No external credentials (always runs)

| Workflow | Parameters | Verifications |
|----------|-----------|---------------|
| **sandbox-test** | `duration=5` | Both steps complete; step 2 output contains step 1 data (output passing works) |
| **profile-test** | (none) | Workflow completes; `ResolveAgentProfile` and `RunPreflight` activities ran |
| **mcp-test** | (none) | Shell step completes (8/8 endpoint checks pass); agent step completes; artifacts created; knowledge item created (then cleaned up) |

### Tier 2 — Requires GITHUB_TOKEN credential (auto-detected)

These test real GitHub integration. If the team has no `GITHUB_TOKEN` credential stored, all are skipped with a clear message.

| Workflow | Parameters | Verifications |
|----------|-----------|---------------|
| **clone-test** | `repo_url=tinkerloft/fleetlift` | Step completes; output contains "clone-ok" (proves git credential injection works) |
| **pr-review** | `repo_url=tinkerloft/fleetlift, pr_number=52` | All 3 steps complete (fetch_pr, review, post-comments) |
| **triage** | `repo_url=tinkerloft/fleetlift, issue_number=1, issue_body=...` | At minimum the `analyze` step completes (classify/comment may fail if issue doesn't exist — that's OK) |
| **bug-fix** | `repo_url=tinkerloft/fleetlift, issue_body=...` | All steps complete (analyze, fix, self_review); uses a benign synthetic issue |

### Tier 3 — Expensive / multi-repo (opt-in via `SMOKE_TIER3=1`)

Skipped by default. These use real Claude Code API credits and take minutes per workflow.

| Workflow | What It Exercises |
|----------|------------------|
| **fleet-research** | Fan-out across repos; parallel step execution |
| **fleet-transform** | Fan-out + code changes + optional PR creation |
| **audit** | Multi-repo analysis with custom audit criteria |
| **incident-response** | Single-repo incident analysis with log correlation |
| **migration** | Multi-step code migration with verification |
| **dependency-update** | Targeted dependency version bump across repos |
| **doc-assessment** | Documentation quality analysis |

### Always skipped

| Workflow | Reason |
|----------|--------|
| **auto-debt-slayer** | Requires Jira credentials (`JIRA_API_TOKEN`) not available in standard dev setup |

### Credential auto-detection

The workflow layer checks the database for stored credentials before deciding what to run:

```
GITHUB_TOKEN exists?
  ├── yes → run Tier 1 + Tier 2
  └── no  → run Tier 1, skip Tier 2 with message

SMOKE_TIER3=1 set?
  ├── yes + GITHUB_TOKEN → run Tier 3
  └── no → skip Tier 3 with message
```

---

## Layer 4: Web UI (Playwright)

Browser tests using Playwright (Chromium). The app runs in dev mode where `DEV_NO_AUTH=1` causes automatic login — no GitHub OAuth needed. Tests interact with the production build served by the fleetlift server at `http://localhost:8080`.

### Navigation tests

Verify every page loads and renders its primary content:

| Page | URL | What's Checked |
|------|-----|---------------|
| Workflow Library | `/workflows` | Heading visible; builtin workflows listed (Sandbox Test, PR Review, Bug Fix) |
| Runs | `/runs` | Heading visible |
| Inbox | `/inbox` | Heading visible |
| Reports | `/reports` | Heading visible |
| Knowledge | `/knowledge` | Heading visible |
| Settings | `/settings` | Credentials section visible |
| System Health | `/system` | Page renders |

### Workflow launch test

1. Navigate to `/workflows/sandbox-test`
2. Fill in duration parameter
3. Click run button
4. Verify redirect to `/runs/<uuid>` (run detail page)
5. Verify workflow title appears on run detail page

### Credentials test

1. Navigate to `/settings`
2. Create a credential (`PLAYWRIGHT_SMOKE_CRED`)
3. Verify it appears in the list
4. Delete it
5. Verify it's gone

### SSE log streaming test

This test is specifically designed to verify — and help debug — the known issue where the `LogStream` component shows "Waiting for logs..." during execution and only renders logs after step completion. The backend SSE endpoint is confirmed working (curl receives real-time events), so this test isolates the browser-side behavior.

1. Submit a `sandbox-test` with `duration=20` via the API (long enough to observe streaming)
2. Navigate to the run detail page (`/runs/<id>`)
3. Wait for the first step (`run_command`) to appear in the step list with status `running`
4. Click on the running step to mount the `LogStream` component
5. **Diagnostic captures** (taken regardless of pass/fail, to aid debugging):
   - Screenshot of the log panel immediately after selecting the step
   - Browser console logs (capture any EventSource errors, 401s, connection failures)
   - Network tab: capture the request to `/api/runs/steps/<id>/logs` — record HTTP status, whether the connection stays open (SSE) or closes immediately, and any response headers
   - Check if the `fl_token` cookie is present in the browser's cookie jar
   - Check if `localStorage.token` is set
6. **Assertions:**
   - The EventSource request to `/api/runs/steps/<id>/logs` returns HTTP 200 (not 401 or 404)
   - Within 10 seconds of selecting the step, at least one log line containing "tick" appears in the `.font-mono` log container (the LogStream renders into a `div` with `font-mono text-xs text-green-400`)
   - The log count increases over a 5-second observation window (proving real-time streaming, not a single dump)
7. Wait for the workflow to complete
8. Take a final screenshot showing the full log output

**Expected current behavior (before fix):** Step 6 assertion fails — no "tick" lines appear while the step is running. The diagnostic captures should reveal why (missing cookie, 401 response, EventSource error, etc.).

**Expected behavior after fix:** Log lines appear incrementally as the step runs. The "streaming" indicator is visible.

**Not tested via Playwright:** HITL approval flows (requires a workflow paused at an approval gate), report export.

---

## Standalone SSE Test (`run-sse-test.sh`)

A focused, standalone script for SSE debugging. No Playwright dependency. Tests the two backend layers independently, and optionally runs the Playwright SSE test as a third layer.

```bash
scripts/integration/run-sse-test.sh              # Backend only
scripts/integration/run-sse-test.sh --playwright  # Backend + browser
```

### Test 1: Worker → DB

Submits a `sandbox-test` with `duration=20`. While the step is running, polls the `step_run_logs` table every 2 seconds for 12 seconds. Verifies the row count increases monotonically — proving the worker's `logBuffer` flushes incrementally, not in a single batch at the end.

**Passes when:** Each poll shows more rows than the previous one.
**Fails when:** Row count stays flat during the observation window, meaning the buffer only flushes at completion.

### Test 2: DB → SSE

Connects to the SSE endpoint `/api/runs/steps/<id>/logs` via `curl -N` for 8 seconds while the step is still running. Counts the number of `data:` lines and `tick` lines received.

**Passes when:** Multiple SSE events arrive containing tick output, and the SSE handshake comment (`: connected`) is present.
**Fails when:** Zero events arrive (SSE endpoint blocked, or pg_notify not firing), or handshake is missing.

### Test 3: Browser (optional, `--playwright`)

Runs only the `sse-streaming.spec.ts` Playwright test. Produces screenshots and console/network diagnostics in `scripts/integration/smoke/playwright/test-results/`.

### Output

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  SSE Test Results:
    Worker → DB:  PASS
    DB → SSE:     PASS
    Browser SSE:  FAIL
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

This output immediately tells you which layer is broken: if Test 1 and 2 pass but Test 3 fails, the issue is frontend. If Test 2 fails, check pg_notify triggers. If Test 1 fails, check the worker's log buffer.

---

## Error Reporting

Every failure must include enough context to diagnose the issue without re-running the test or manually querying systems. The goal: you read the failure output and immediately know which part of the stack is broken and why.

### Principle: show the actual error, not just the expectation

Bad: `FAIL: POST /api/credentials — expected HTTP 201, got 500`
Good:
```
FAIL: POST /api/credentials — expected HTTP 201, got 500
  Response: {"error":"credential name SMOKE_TEST_CRED already exists"}
```

Bad: `FAIL: pr-review — FAILED`
Good:
```
FAIL: pr-review — workflow FAILED (run=abc123, temporal=fl-pr-review-abc123)
  Step: fetch_pr — failed
  Error: create sandbox: opensandbox: create returned 500: image not found
  Hint: run 'make sandbox-build' to build the claude-code-sandbox image
```

### Per-layer error context

| Layer | On failure, include: |
|-------|---------------------|
| **API** | HTTP status code, response body (truncated to 500 chars), request method + path |
| **CLI** | Exit code, full stdout+stderr of the failed command |
| **Workflows** | Which step failed, Temporal failure message (from `temporal workflow show`), step error_message from DB if available |
| **Playwright** | Screenshot path, browser console errors, network request status for the relevant endpoint |
| **SSE** | Which layer failed (Worker→DB, DB→SSE, Browser), log counts at each checkpoint, SSE response status/headers |

### API assertion pattern

`assert_status` on failure should print the response body:

```
FAIL: POST /api/credentials (create) — expected HTTP 201, got 500
  Body: {"error":"pq: duplicate key value violates unique constraint"}
```

### Workflow assertion pattern

`wait_for_run` on failure should query the Temporal history and DB for the failure reason:

```
FAIL: pr-review — workflow FAILED (run=9818afc7, temporal=fl-pr-review-9818afc7)
  Failure: step fetch_pr failed: create sandbox: opensandbox: create returned 500: ...
  Step statuses:
    fetch_pr: failed
    review: pending
    post-comments: pending
```

### CLI assertion pattern

`assert_contains` on failure should show what the CLI actually output:

```
FAIL: workflow list shows sandbox-test — output missing 'sandbox-test'
  Actual output:
    Error: unauthorized — check your auth token
```

### Output format

Passing tests are one line. Failing tests expand with indented context:

```
── Workflows: Tier 1 ──
  PASS: sandbox-test — submitted (run=abc123)
  PASS: sandbox-test — completed
  PASS: sandbox-test — all steps complete
  FAIL: mcp-test — workflow FAILED (run=def456, temporal=fl-mcp-test-def456)
    Failure: step verify_mcp_endpoints failed: agent error: command exited with code 1
    Step statuses:
      verify_mcp_endpoints: failed
      agent_uses_mcp: pending
    Hint: check 'temporal workflow show --workflow-id fl-mcp-test-def456'

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Results: 3 passed, 1 failed, 0 skipped

  Failures:
    - mcp-test — workflow FAILED
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

The top-level runner reports which layers passed or failed:

```
╔══════════════════════════════════════════╗
║  SMOKE TEST SUMMARY
╚══════════════════════════════════════════╝
  Failed layers:
    - Workflow E2E
```

Exit code is 0 if all layers pass, 1 otherwise.

---

## Extending the Suite

**Adding a new workflow test:** Add a `run_workflow` call in the appropriate tier section of `03-workflows.sh`. The helper submits the run, polls Temporal, and reports pass/fail.

**Adding a new API test:** Add `api_call` + `assert_status` calls in the appropriate section of `01-api.sh`.

**Adding a new Playwright test:** Create a new `.spec.ts` file in `smoke/playwright/tests/`. It will be auto-discovered.

**Adding a new CLI test:** Add commands + assertions in `02-cli.sh`.

---

## Relationship to Existing Scripts

The existing `run-sandbox-test.sh`, `run-profile-test.sh`, and `run-mcp-test.sh` are **not replaced**. They remain as quick standalone checks. The smoke suite provides broader coverage using the same patterns but with shared infrastructure and unified reporting.
