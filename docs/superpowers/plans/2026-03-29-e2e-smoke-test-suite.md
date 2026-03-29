# E2E Smoke Test Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a comprehensive, extensible smoke test suite that validates all major fleetlift functionality against a running local stack — API endpoints, CLI commands, built-in workflows, and web UI.

**Architecture:** A single runner script (`scripts/integration/smoke-test.sh`) orchestrates four test layers: API, CLI, workflows, and web UI. Shared helpers live in `scripts/integration/smoke-lib.sh`. Each layer is independently runnable. The web UI layer uses Playwright. All tests assume the local stack is already running (`docker compose up -d` + `scripts/integration/start.sh`).

**Tech Stack:** Bash (API/CLI/workflow tests), Playwright + TypeScript (web UI tests), PostgreSQL queries for verification, Temporal CLI for workflow status.

---

## File Structure

```
scripts/integration/
  smoke-lib.sh          — Shared test helpers (auth, assertions, polling, cleanup)
  smoke-test.sh         — Top-level runner: runs all layers, reports summary
  smoke/
    01-api.sh           — API endpoint tests (CRUD for all resource types)
    02-cli.sh           — CLI command tests (workflow, run, credential, knowledge)
    03-workflows.sh     — Built-in workflow E2E tests (all 15 workflows)
    04-web-ui.sh        — Launches Playwright tests
  smoke/playwright/
    playwright.config.ts — Playwright config (base URL, timeouts, single worker)
    package.json         — Playwright + TS dependencies
    tests/
      navigation.spec.ts — Page navigation + content verification
      workflow-run.spec.ts — Submit workflow from UI, watch run complete
      credentials.spec.ts — CRUD credentials from settings page
```

Existing test scripts (`run-sandbox-test.sh`, `run-profile-test.sh`, `run-mcp-test.sh`) are NOT modified. They continue to work standalone. The new suite incorporates equivalent coverage using the shared helpers.

---

### Task 1: Shared Test Library (`smoke-lib.sh`)

**Files:**
- Create: `scripts/integration/smoke-lib.sh`

This is the foundation. Every other test file sources it. It provides: JWT generation, HTTP helpers, assertion functions, workflow polling, DB queries, and test lifecycle (setup/teardown/reporting).

- [ ] **Step 1: Create smoke-lib.sh with core helpers**

```bash
#!/usr/bin/env bash
# smoke-lib.sh — shared helpers for fleetlift smoke tests
# Source this file at the top of every smoke test script.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Source environment
source "$SCRIPT_DIR/dev-env.sh"

# ── Test bookkeeping ──────────────────────────────────────────────
_PASS=0; _FAIL=0; _SKIP=0; _ERRORS=()

pass() { _PASS=$((_PASS + 1)); printf "  \033[32mPASS\033[0m: %s\n" "$1"; }
fail() { _FAIL=$((_FAIL + 1)); _ERRORS+=("$1"); printf "  \033[31mFAIL\033[0m: %s\n" "$1"; }
skip() { _SKIP=$((_SKIP + 1)); printf "  \033[33mSKIP\033[0m: %s\n" "$1"; }

section() { printf "\n\033[1m── %s ──\033[0m\n" "$1"; }

smoke_summary() {
  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Results: $_PASS passed, $_FAIL failed, $_SKIP skipped"
  if [[ ${#_ERRORS[@]} -gt 0 ]]; then
    echo ""
    echo "  Failures:"
    for e in "${_ERRORS[@]}"; do
      echo "    - $e"
    done
  fi
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  [[ $_FAIL -eq 0 ]]
}

# ── Auth ──────────────────────────────────────────────────────────
_JWT_CACHE=""

smoke_jwt() {
  if [[ -n "$_JWT_CACHE" ]]; then echo "$_JWT_CACHE"; return; fi

  local tmpdir
  tmpdir=$(mktemp -d)
  cat > "$tmpdir/jwt.go" <<'GOEOF'
package main

import (
	"fmt"; "os"; "time"
	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID    string            `json:"user_id"`
	TeamRoles map[string]string `json:"team_roles"`
	jwt.RegisteredClaims
}

func main() {
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID:    os.Args[1],
		TeamRoles: map[string]string{os.Args[2]: "admin"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	})
	s, _ := t.SignedString([]byte(os.Args[3]))
	fmt.Print(s)
}
GOEOF

  _JWT_CACHE=$(cd "$PROJECT_ROOT" && go run "$tmpdir/jwt.go" "$DEV_USER_ID" "$DEV_TEAM_ID" "$JWT_SECRET")
  rm -rf "$tmpdir"
  echo "$_JWT_CACHE"
}

auth_headers() {
  local jwt
  jwt=$(smoke_jwt)
  echo "-H 'Authorization: Bearer $jwt' -H 'X-Team-ID: $DEV_TEAM_ID'"
}

# ── HTTP helpers ──────────────────────────────────────────────────
# Usage: api_call GET /api/runs
#        api_call POST /api/runs '{"workflow_id":"sandbox-test"}'
# Sets: HTTP_STATUS, HTTP_BODY
HTTP_STATUS=""
HTTP_BODY=""

api_call() {
  local method="$1" path="$2" body="${3:-}"
  local jwt
  jwt=$(smoke_jwt)
  local url="http://localhost:8080${path}"
  local tmpfile
  tmpfile=$(mktemp)

  local curl_args=(-s -w "\n%{http_code}" -X "$method"
    -H "Authorization: Bearer $jwt"
    -H "X-Team-ID: $DEV_TEAM_ID"
    -H "Content-Type: application/json")

  if [[ -n "$body" ]]; then
    curl_args+=(-d "$body")
  fi

  local response
  response=$(curl "${curl_args[@]}" "$url")
  HTTP_STATUS=$(echo "$response" | tail -1)
  HTTP_BODY=$(echo "$response" | sed '$d')
}

json_field() {
  echo "$HTTP_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)$1)" 2>/dev/null
}

# ── Assertions ────────────────────────────────────────────────────
# All assertion failures print the actual value / response body so you can
# diagnose without re-running.

assert_status() {
  local test_name="$1" expected="$2"
  if [[ "$HTTP_STATUS" == "$expected" ]]; then
    pass "$test_name (HTTP $HTTP_STATUS)"
  else
    fail "$test_name — expected HTTP $expected, got $HTTP_STATUS"
    # Show truncated response body for context
    local body_preview="${HTTP_BODY:0:500}"
    [[ -n "$body_preview" ]] && printf "    Body: %s\n" "$body_preview"
  fi
}

assert_contains() {
  local test_name="$1" haystack="$2" needle="$3"
  if echo "$haystack" | grep -q "$needle"; then
    pass "$test_name"
  else
    fail "$test_name — output missing '$needle'"
    # Show what was actually there (truncated)
    local preview="${haystack:0:500}"
    [[ -n "$preview" ]] && printf "    Actual output:\n%s\n" "$(echo "$preview" | sed 's/^/      /')"
  fi
}

assert_not_empty() {
  local test_name="$1" value="$2"
  if [[ -n "$value" ]]; then
    pass "$test_name"
  else
    fail "$test_name — value was empty"
  fi
}

assert_equals() {
  local test_name="$1" expected="$2" actual="$3"
  if [[ "$actual" == "$expected" ]]; then
    pass "$test_name"
  else
    fail "$test_name — expected '$expected', got '$actual'"
  fi
}

# ── Workflow helpers ──────────────────────────────────────────────
# submit_run <workflow_id> '<json_params>'
# Sets: RUN_ID, TEMPORAL_ID
RUN_ID=""
TEMPORAL_ID=""

submit_run() {
  local workflow_id="$1" params="${2:-\{\}}"
  api_call POST /api/runs "{\"workflow_id\":\"$workflow_id\",\"parameters\":$params}"
  if [[ "$HTTP_STATUS" == "201" ]]; then
    RUN_ID=$(json_field "['id']")
    TEMPORAL_ID=$(json_field "['temporal_id']")
  else
    RUN_ID=""
    TEMPORAL_ID=""
  fi
}

# wait_for_run [timeout_seconds]
# Polls Temporal until workflow completes. Sets WORKFLOW_STATUS.
# On failure, prints the Temporal failure message and step statuses.
WORKFLOW_STATUS=""

wait_for_run() {
  local timeout="${1:-120}"
  local interval=2
  local elapsed=0
  WORKFLOW_STATUS=""

  while [[ $elapsed -lt $timeout ]]; do
    local desc
    desc=$(temporal workflow describe --workflow-id "$TEMPORAL_ID" --address "$TEMPORAL_ADDRESS" 2>/dev/null || true)
    if echo "$desc" | grep -q "COMPLETED"; then
      WORKFLOW_STATUS="COMPLETED"; return 0
    elif echo "$desc" | grep -qE "FAILED|TERMINATED|CANCELED|TIMED_OUT"; then
      WORKFLOW_STATUS=$(echo "$desc" | grep -oE "FAILED|TERMINATED|CANCELED|TIMED_OUT" | head -1)
      _dump_workflow_failure
      return 1
    fi
    sleep "$interval"
    elapsed=$((elapsed + interval))
  done
  WORKFLOW_STATUS="TIMEOUT"
  printf "    Timed out after %ds waiting for workflow %s\n" "$timeout" "$TEMPORAL_ID"
  _dump_workflow_failure
  return 1
}

# Print diagnostic context when a workflow fails.
_dump_workflow_failure() {
  # Temporal failure message
  local failure_msg
  failure_msg=$(temporal workflow show --workflow-id "$TEMPORAL_ID" --address "$TEMPORAL_ADDRESS" 2>/dev/null \
    | grep -A5 "Failure" | head -6 || true)
  if [[ -n "$failure_msg" ]]; then
    printf "    Temporal failure:\n%s\n" "$(echo "$failure_msg" | sed 's/^/      /')"
  fi

  # Step statuses from DB
  if [[ -n "$RUN_ID" ]]; then
    local steps
    steps=$(fl_sql "SELECT step_id || ': ' || status || coalesce(' — ' || error_message, '') FROM step_runs WHERE run_id = '$RUN_ID' ORDER BY created_at" 2>/dev/null || true)
    if [[ -n "$steps" ]]; then
      printf "    Step statuses:\n%s\n" "$(echo "$steps" | sed 's/^/      /')"
    fi
  fi
}

# step_status <run_id> <step_id>
step_status() {
  fl_sql "SELECT status FROM step_runs WHERE run_id = '$1' AND step_id = '$2'" | tr -d ' \n'
}

# step_output <run_id> <step_id>
step_output() {
  fl_sql "SELECT output FROM step_runs WHERE run_id = '$1' AND step_id = '$2'" | tr -d ' \n'
}

# step_count <run_id>
step_count() {
  fl_sql "SELECT count(*) FROM step_runs WHERE run_id = '$1'" | tr -d ' \n'
}

# ── Credential helpers ────────────────────────────────────────────
has_credential() {
  local name="$1"
  local exists
  exists=$(fl_sql "SELECT count(*) FROM credentials WHERE team_id = '$DEV_TEAM_ID' AND name = '$name'" | tr -d ' \n')
  [[ "$exists" -gt 0 ]]
}

# ── Cleanup ───────────────────────────────────────────────────────
_CLEANUP_CMDS=()

on_cleanup() { _CLEANUP_CMDS+=("$1"); }

run_cleanup() {
  for cmd in "${_CLEANUP_CMDS[@]:-}"; do
    eval "$cmd" 2>/dev/null || true
  done
}

trap run_cleanup EXIT
```

- [ ] **Step 2: Verify the library loads without error**

Run: `bash -n scripts/integration/smoke-lib.sh && echo OK`
Expected: `OK` (no syntax errors)

- [ ] **Step 3: Commit**

```bash
git add scripts/integration/smoke-lib.sh
git commit -m "feat: add shared smoke test library (smoke-lib.sh)"
```

---

### Task 2: API Endpoint Tests (`smoke/01-api.sh`)

**Files:**
- Create: `scripts/integration/smoke/01-api.sh`

Tests CRUD operations for every major API resource: health, config, workflows, runs, credentials, knowledge, inbox, reports, agent profiles, and marketplaces.

- [ ] **Step 1: Create the API test script**

```bash
#!/usr/bin/env bash
# 01-api.sh — API endpoint smoke tests
source "$(cd "$(dirname "$0")/.." && pwd)/smoke-lib.sh"

section "Health & Config"

api_call GET /health
assert_status "GET /health" 200

api_call GET /api/config
assert_status "GET /api/config" 200
assert_contains "config has dev_no_auth" "$HTTP_BODY" "dev_no_auth"

api_call GET /api/me
assert_status "GET /api/me" 200

# ── Workflows ─────────────────────────────────────────────────────
section "Workflows"

api_call GET /api/workflows
assert_status "GET /api/workflows" 200
WF_COUNT=$(json_field "['total']" 2>/dev/null || echo "")
assert_not_empty "workflow list has total" "$WF_COUNT"

api_call GET "/api/workflows/sandbox-test"
assert_status "GET /api/workflows/sandbox-test" 200

api_call GET "/api/workflows/pr-review"
assert_status "GET /api/workflows/pr-review" 200

api_call GET "/api/workflows/nonexistent-workflow"
assert_status "GET /api/workflows/nonexistent (404)" 404

# Fork a builtin workflow
api_call POST "/api/workflows/sandbox-test/fork" '{}'
if [[ "$HTTP_STATUS" == "201" || "$HTTP_STATUS" == "200" ]]; then
  pass "POST /api/workflows/sandbox-test/fork"
  FORKED_SLUG=$(json_field "['slug']" 2>/dev/null || echo "")
  if [[ -n "$FORKED_SLUG" ]]; then
    on_cleanup "api_call DELETE /api/workflows/$FORKED_SLUG"
  fi
else
  # May already exist from a previous run
  if [[ "$HTTP_STATUS" == "409" ]]; then
    pass "POST /api/workflows/sandbox-test/fork (already exists)"
  else
    fail "POST /api/workflows/sandbox-test/fork — HTTP $HTTP_STATUS"
  fi
fi

# ── Credentials ───────────────────────────────────────────────────
section "Credentials"

api_call GET /api/credentials
assert_status "GET /api/credentials" 200

api_call POST /api/credentials '{"name":"SMOKE_TEST_CRED","value":"smoke-value-123"}'
assert_status "POST /api/credentials (create)" 201
on_cleanup "api_call DELETE /api/credentials/SMOKE_TEST_CRED"

api_call GET /api/credentials
assert_status "GET /api/credentials (after create)" 200
assert_contains "credential list includes SMOKE_TEST_CRED" "$HTTP_BODY" "SMOKE_TEST_CRED"

api_call DELETE /api/credentials/SMOKE_TEST_CRED
assert_status "DELETE /api/credentials" 204

# ── Knowledge ─────────────────────────────────────────────────────
section "Knowledge"

api_call GET "/api/knowledge"
assert_status "GET /api/knowledge" 200

api_call GET "/api/knowledge?status=approved"
assert_status "GET /api/knowledge?status=approved" 200

# ── Inbox ─────────────────────────────────────────────────────────
section "Inbox"

api_call GET /api/inbox
assert_status "GET /api/inbox" 200

# ── Reports ───────────────────────────────────────────────────────
section "Reports"

api_call GET /api/reports
assert_status "GET /api/reports" 200

# ── Agent Profiles ────────────────────────────────────────────────
section "Agent Profiles"

api_call POST /api/agent-profiles '{"name":"smoke-test-profile","description":"smoke test","body":{"plugins":[]}}'
if [[ "$HTTP_STATUS" == "201" ]]; then
  pass "POST /api/agent-profiles (create)"
  PROFILE_ID=$(json_field "['id']" 2>/dev/null || echo "")

  api_call GET "/api/agent-profiles/$PROFILE_ID"
  assert_status "GET /api/agent-profiles/:id" 200

  api_call PUT "/api/agent-profiles/$PROFILE_ID" '{"name":"smoke-test-profile-updated","description":"updated","body":{"plugins":[]}}'
  assert_status "PUT /api/agent-profiles/:id" 200

  api_call DELETE "/api/agent-profiles/$PROFILE_ID"
  assert_status "DELETE /api/agent-profiles/:id" 204
else
  fail "POST /api/agent-profiles — HTTP $HTTP_STATUS"
fi

# ── Action Types ──────────────────────────────────────────────────
section "Action Types"

api_call GET /api/action-types
assert_status "GET /api/action-types" 200

# ── Runs (basic CRUD, no workflow execution) ──────────────────────
section "Runs (list/get)"

api_call GET /api/runs
assert_status "GET /api/runs" 200

# ── SSE endpoints (quick connect/disconnect) ──────────────────────
section "SSE Endpoints"

# Test that SSE endpoints respond (we just check they don't 404/500)
# Use a fake run ID — should get 200 with empty stream or 404
SSE_STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "Authorization: Bearer $(smoke_jwt)" \
  -H "X-Team-ID: $DEV_TEAM_ID" \
  --max-time 2 \
  "http://localhost:8080/api/runs/00000000-0000-0000-0000-000000000000/events" 2>/dev/null || echo "000")
if [[ "$SSE_STATUS" == "200" || "$SSE_STATUS" == "404" ]]; then
  pass "SSE /api/runs/:id/events responds ($SSE_STATUS)"
else
  fail "SSE /api/runs/:id/events — HTTP $SSE_STATUS"
fi

# ── Summary ───────────────────────────────────────────────────────
smoke_summary
```

- [ ] **Step 2: Make executable and verify syntax**

```bash
chmod +x scripts/integration/smoke/01-api.sh
bash -n scripts/integration/smoke/01-api.sh && echo OK
```

- [ ] **Step 3: Run the API tests against the local stack**

Run: `scripts/integration/smoke/01-api.sh`
Expected: All tests pass. Fix any failures before proceeding.

- [ ] **Step 4: Commit**

```bash
git add scripts/integration/smoke/01-api.sh
git commit -m "feat: add API endpoint smoke tests"
```

---

### Task 3: CLI Command Tests (`smoke/02-cli.sh`)

**Files:**
- Create: `scripts/integration/smoke/02-cli.sh`

Tests the `fleetlift` CLI binary against the running server. The CLI uses `--server http://localhost:8080` and in dev mode doesn't require GitHub OAuth (the server accepts any token when `DEV_NO_AUTH=1`).

- [ ] **Step 1: Create the CLI test script**

```bash
#!/usr/bin/env bash
# 02-cli.sh — CLI command smoke tests
source "$(cd "$(dirname "$0")/.." && pwd)/smoke-lib.sh"

CLI="$PROJECT_ROOT/bin/fleetlift"
if [[ ! -x "$CLI" ]]; then
  echo "Building fleetlift CLI..."
  (cd "$PROJECT_ROOT" && go build -buildvcs=false -o bin/fleetlift ./cmd/cli)
fi

# The CLI reads auth from ~/.fleetlift/auth.json.
# In dev mode, we write a dev token there temporarily.
AUTH_FILE="$HOME/.fleetlift/auth.json"
AUTH_BACKUP=""
if [[ -f "$AUTH_FILE" ]]; then
  AUTH_BACKUP=$(cat "$AUTH_FILE")
fi
mkdir -p "$(dirname "$AUTH_FILE")"
echo '{"token":"dev-token"}' > "$AUTH_FILE"
on_cleanup "if [[ -n \"\$AUTH_BACKUP\" ]]; then echo '\$AUTH_BACKUP' > '$AUTH_FILE'; else rm -f '$AUTH_FILE'; fi"

FL="$CLI --server http://localhost:8080"

# ── Workflow commands ─────────────────────────────────────────────
section "CLI: workflow"

OUTPUT=$($FL workflow list 2>&1) || true
assert_contains "workflow list shows sandbox-test" "$OUTPUT" "sandbox-test"
assert_contains "workflow list shows pr-review" "$OUTPUT" "pr-review"
assert_contains "workflow list shows bug-fix" "$OUTPUT" "bug-fix"

OUTPUT=$($FL workflow get sandbox-test 2>&1) || true
assert_contains "workflow get sandbox-test has title" "$OUTPUT" "Sandbox Test"

OUTPUT=$($FL workflow get nonexistent-wf 2>&1) && {
  fail "workflow get nonexistent should fail"
} || {
  pass "workflow get nonexistent returns error"
}

# ── Credential commands ───────────────────────────────────────────
section "CLI: credential"

$FL credential set SMOKE_CLI_CRED smoke-cli-value 2>&1 || true
on_cleanup "$FL credential delete SMOKE_CLI_CRED 2>/dev/null || true"

OUTPUT=$($FL credential list 2>&1) || true
assert_contains "credential list shows SMOKE_CLI_CRED" "$OUTPUT" "SMOKE_CLI_CRED"

$FL credential delete SMOKE_CLI_CRED 2>&1 || true
OUTPUT=$($FL credential list 2>&1) || true
if echo "$OUTPUT" | grep -q "SMOKE_CLI_CRED"; then
  fail "credential delete — still listed"
else
  pass "credential delete — removed"
fi

# ── Run commands ──────────────────────────────────────────────────
section "CLI: run"

OUTPUT=$($FL run list 2>&1) || true
assert_status_cli "run list succeeds" "$?"

# Start a sandbox-test via CLI
OUTPUT=$($FL run start sandbox-test -p duration=3 2>&1) || true
assert_contains "run start returns run ID" "$OUTPUT" "Run started"

# Extract run ID from output (format: "Run started: <uuid>")
CLI_RUN_ID=$(echo "$OUTPUT" | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1)
if [[ -n "$CLI_RUN_ID" ]]; then
  pass "run start returned ID: $CLI_RUN_ID"

  # Wait for it to complete
  sleep 15

  OUTPUT=$($FL run get "$CLI_RUN_ID" 2>&1) || true
  assert_contains "run get shows status" "$OUTPUT" "status"
else
  fail "run start — could not extract run ID"
fi

# ── Knowledge commands ────────────────────────────────────────────
section "CLI: knowledge"

OUTPUT=$($FL knowledge list 2>&1) || true
# Should succeed even if empty
if [[ $? -eq 0 ]] || echo "$OUTPUT" | grep -qiE "no knowledge|ID"; then
  pass "knowledge list succeeds"
else
  fail "knowledge list — exit code non-zero"
fi

# ── Summary ───────────────────────────────────────────────────────
smoke_summary
```

Note: `assert_status_cli` is a simple wrapper — add it to `smoke-lib.sh`:

```bash
assert_status_cli() {
  local test_name="$1" exit_code="$2"
  if [[ "$exit_code" == "0" ]]; then
    pass "$test_name"
  else
    fail "$test_name — exit code $exit_code"
  fi
}
```

- [ ] **Step 2: Add `assert_status_cli` to smoke-lib.sh**

Append to the assertions section of `scripts/integration/smoke-lib.sh`:

```bash
assert_status_cli() {
  local test_name="$1" exit_code="$2"
  if [[ "$exit_code" == "0" ]]; then
    pass "$test_name"
  else
    fail "$test_name — exit code $exit_code"
  fi
}
```

- [ ] **Step 3: Make executable and verify syntax**

```bash
chmod +x scripts/integration/smoke/02-cli.sh
bash -n scripts/integration/smoke/02-cli.sh && echo OK
```

- [ ] **Step 4: Run the CLI tests**

Run: `scripts/integration/smoke/02-cli.sh`
Expected: All tests pass. The `run start` test waits 15s for sandbox-test to complete.

- [ ] **Step 5: Commit**

```bash
git add scripts/integration/smoke/02-cli.sh scripts/integration/smoke-lib.sh
git commit -m "feat: add CLI command smoke tests"
```

---

### Task 4: Workflow E2E Tests (`smoke/03-workflows.sh`)

**Files:**
- Create: `scripts/integration/smoke/03-workflows.sh`

Tests every built-in workflow. Workflows are grouped into tiers:

- **Tier 1 (always run):** `sandbox-test`, `profile-test`, `mcp-test` — no external credentials needed
- **Tier 2 (requires GITHUB_TOKEN):** `clone-test`, `pr-review`, `triage`, `bug-fix` — needs a real GitHub token and a test repo
- **Tier 3 (requires GITHUB_TOKEN + expensive):** `fleet-research`, `fleet-transform`, `migration`, `dependency-update`, `doc-assessment`, `audit`, `incident-response` — multi-repo, long-running
- **Skipped:** `auto-debt-slayer` — requires Jira credentials

The script auto-detects available credentials and runs the appropriate tiers.

- [ ] **Step 1: Create the workflow test script**

```bash
#!/usr/bin/env bash
# 03-workflows.sh — Built-in workflow E2E smoke tests
source "$(cd "$(dirname "$0")/.." && pwd)/smoke-lib.sh"

# ── Credential detection ─────────────────────────────────────────
HAS_GITHUB_TOKEN=false
if has_credential "GITHUB_TOKEN"; then
  HAS_GITHUB_TOKEN=true
fi

# Test repository (public, small) for workflows that need a repo
TEST_REPO="https://github.com/tinkerloft/fleetlift"

# ── Helper: run a workflow and verify completion ─────────────────
# Usage: run_workflow <test_name> <workflow_id> '<json_params>' [timeout_seconds]
run_workflow() {
  local test_name="$1" workflow_id="$2" params="$3" timeout="${4:-120}"

  submit_run "$workflow_id" "$params"
  if [[ "$HTTP_STATUS" != "201" ]]; then
    fail "$test_name — submit failed (HTTP $HTTP_STATUS)"
    local body_preview="${HTTP_BODY:0:500}"
    [[ -n "$body_preview" ]] && printf "    Body: %s\n" "$body_preview"
    return
  fi
  pass "$test_name — submitted (run=$RUN_ID)"

  if wait_for_run "$timeout"; then
    pass "$test_name — completed"
  else
    fail "$test_name — $WORKFLOW_STATUS (run=$RUN_ID, temporal=$TEMPORAL_ID)"
    # Diagnostic context is already printed by wait_for_run/_dump_workflow_failure
    printf "    Hint: temporal workflow show --workflow-id %s --address %s\n" "$TEMPORAL_ID" "$TEMPORAL_ADDRESS"
  fi
}

# ── Helper: verify all steps in a run completed ──────────────────
verify_steps() {
  local test_name="$1" run_id="$2"
  local failed_steps
  failed_steps=$(fl_sql "SELECT step_id FROM step_runs WHERE run_id = '$run_id' AND status != 'complete'" | tr -d ' ')
  if [[ -z "$failed_steps" ]]; then
    pass "$test_name — all steps complete"
  else
    fail "$test_name — incomplete steps: $failed_steps"
  fi
}

# ══════════════════════════════════════════════════════════════════
# TIER 1: No external credentials required
# ══════════════════════════════════════════════════════════════════

section "Tier 1: Diagnostic Workflows (no credentials)"

# ── sandbox-test ──────────────────────────────────────────────────
run_workflow "sandbox-test" "sandbox-test" '{"duration":5,"command2":"echo smoke-ok"}'
if [[ "$WORKFLOW_STATUS" == "COMPLETED" ]]; then
  verify_steps "sandbox-test" "$RUN_ID"

  # Verify step 2 received output from step 1
  S2_OUTPUT=$(step_output "$RUN_ID" "verify_output")
  assert_contains "sandbox-test step2 got step1 output" "${S2_OUTPUT:-}" "smoke-ok"
fi

# ── profile-test ──────────────────────────────────────────────────
run_workflow "profile-test" "profile-test" '{}' 180
if [[ "$WORKFLOW_STATUS" == "COMPLETED" ]]; then
  verify_steps "profile-test" "$RUN_ID"
fi

# ── mcp-test ──────────────────────────────────────────────────────
run_workflow "mcp-test" "mcp-test" '{}' 180
if [[ "$WORKFLOW_STATUS" == "COMPLETED" ]]; then
  # Verify shell step passed
  MCP_STEP1=$(step_status "$RUN_ID" "verify_mcp_endpoints")
  assert_equals "mcp-test shell step" "complete" "$MCP_STEP1"

  # Verify artifacts were created
  ARTIFACT_COUNT=$(fl_sql "SELECT count(*) FROM artifacts a JOIN step_runs sr ON a.step_run_id = sr.id WHERE sr.run_id = '$RUN_ID'" | tr -d ' \n')
  if [[ "$ARTIFACT_COUNT" -gt 0 ]]; then
    pass "mcp-test — $ARTIFACT_COUNT artifact(s) created"
  else
    fail "mcp-test — no artifacts created"
  fi

  # Cleanup test knowledge items
  fl_sql "DELETE FROM knowledge_items WHERE summary = 'mcp-test-learning'" >/dev/null 2>&1 || true
fi

# ══════════════════════════════════════════════════════════════════
# TIER 2: Requires GITHUB_TOKEN credential
# ══════════════════════════════════════════════════════════════════

section "Tier 2: GitHub Workflows (requires GITHUB_TOKEN)"

if [[ "$HAS_GITHUB_TOKEN" != "true" ]]; then
  skip "clone-test — no GITHUB_TOKEN credential"
  skip "pr-review — no GITHUB_TOKEN credential"
  skip "triage — no GITHUB_TOKEN credential"
  skip "bug-fix — no GITHUB_TOKEN credential"
else

  # ── clone-test ────────────────────────────────────────────────
  run_workflow "clone-test" "clone-test" "{\"repo_url\":\"$TEST_REPO\"}" 120
  if [[ "$WORKFLOW_STATUS" == "COMPLETED" ]]; then
    verify_steps "clone-test" "$RUN_ID"
    CLONE_OUTPUT=$(step_output "$RUN_ID" "clone_and_verify")
    assert_contains "clone-test output has clone-ok" "${CLONE_OUTPUT:-}" "clone-ok"
  fi

  # ── pr-review ─────────────────────────────────────────────────
  # Use PR #52 which exists on the test repo
  run_workflow "pr-review" "pr-review" "{\"repo_url\":\"$TEST_REPO\",\"pr_number\":52}" 300
  if [[ "$WORKFLOW_STATUS" == "COMPLETED" ]]; then
    verify_steps "pr-review" "$RUN_ID"
  fi

  # ── triage ────────────────────────────────────────────────────
  # Use a synthetic issue body to test classification
  TRIAGE_PARAMS=$(python3 -c "
import json
print(json.dumps({
  'repo_url': '$TEST_REPO',
  'issue_number': 1,
  'issue_body': 'Smoke test issue: the server returns 500 on GET /api/health when the database is down.'
}))
  ")
  run_workflow "triage" "triage" "$TRIAGE_PARAMS" 300
  if [[ "$WORKFLOW_STATUS" == "COMPLETED" ]]; then
    # At minimum the analyze step should complete
    ANALYZE_STATUS=$(step_status "$RUN_ID" "analyze")
    assert_equals "triage analyze step" "complete" "$ANALYZE_STATUS"
  fi

  # ── bug-fix ───────────────────────────────────────────────────
  BUGFIX_PARAMS=$(python3 -c "
import json
print(json.dumps({
  'repo_url': '$TEST_REPO',
  'issue_body': 'Smoke test: add a comment to the top of README.md noting the last smoke test date.'
}))
  ")
  run_workflow "bug-fix" "bug-fix" "$BUGFIX_PARAMS" 300
  if [[ "$WORKFLOW_STATUS" == "COMPLETED" ]]; then
    verify_steps "bug-fix" "$RUN_ID"
  fi

fi

# ══════════════════════════════════════════════════════════════════
# TIER 3: Multi-repo / expensive workflows
# ══════════════════════════════════════════════════════════════════

section "Tier 3: Fleet Workflows (expensive, optional)"

# These are skipped by default. Set SMOKE_TIER3=1 to enable.
if [[ "${SMOKE_TIER3:-}" != "1" ]]; then
  skip "fleet-research — set SMOKE_TIER3=1 to enable"
  skip "fleet-transform — set SMOKE_TIER3=1 to enable"
  skip "migration — set SMOKE_TIER3=1 to enable"
  skip "dependency-update — set SMOKE_TIER3=1 to enable"
  skip "doc-assessment — set SMOKE_TIER3=1 to enable"
  skip "audit — set SMOKE_TIER3=1 to enable"
  skip "incident-response — set SMOKE_TIER3=1 to enable"
  skip "auto-debt-slayer — requires Jira credentials"
elif [[ "$HAS_GITHUB_TOKEN" != "true" ]]; then
  skip "Tier 3 — no GITHUB_TOKEN credential"
else

  REPOS_JSON="[\"$TEST_REPO\"]"

  run_workflow "fleet-research" "fleet-research" \
    "{\"repos\":$REPOS_JSON,\"prompt\":\"List the top-level directories and summarize the project structure.\",\"max_parallel\":1}" 300

  run_workflow "fleet-transform" "fleet-transform" \
    "{\"repos\":$REPOS_JSON,\"prompt\":\"Add a comment at the top of README.md: smoke test transform.\",\"max_parallel\":1}" 300

  run_workflow "audit" "audit" \
    "{\"repos\":$REPOS_JSON,\"audit_prompt\":\"Check if the project has a LICENSE file.\"}" 300

  run_workflow "incident-response" "incident-response" \
    "{\"repo_url\":\"$TEST_REPO\",\"log_excerpt\":\"ERROR: connection refused to database\",\"incident_description\":\"Smoke test incident\"}" 300

fi

# ── Summary ───────────────────────────────────────────────────────
smoke_summary
```

- [ ] **Step 2: Make executable and verify syntax**

```bash
chmod +x scripts/integration/smoke/03-workflows.sh
bash -n scripts/integration/smoke/03-workflows.sh && echo OK
```

- [ ] **Step 3: Run Tier 1 tests**

Run: `scripts/integration/smoke/03-workflows.sh`
Expected: Tier 1 passes, Tier 2 passes if GITHUB_TOKEN exists, Tier 3 skipped by default.

- [ ] **Step 4: Commit**

```bash
git add scripts/integration/smoke/03-workflows.sh
git commit -m "feat: add built-in workflow E2E smoke tests (tiered)"
```

---

### Task 5: Web UI Tests — Playwright Setup (`smoke/playwright/`)

**Files:**
- Create: `scripts/integration/smoke/playwright/package.json`
- Create: `scripts/integration/smoke/playwright/playwright.config.ts`
- Create: `scripts/integration/smoke/04-web-ui.sh`

- [ ] **Step 1: Create package.json**

```json
{
  "name": "fleetlift-smoke-playwright",
  "private": true,
  "scripts": {
    "test": "npx playwright test",
    "install-browsers": "npx playwright install chromium"
  },
  "devDependencies": {
    "@playwright/test": "^1.50.0",
    "typescript": "^5.7.0"
  }
}
```

- [ ] **Step 2: Create playwright.config.ts**

```typescript
import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  timeout: 60_000,
  retries: 0,
  workers: 1,
  use: {
    baseURL: 'http://localhost:8080',
    // Dev mode auto-sets token — no login needed
    storageState: undefined,
  },
  reporter: [['list']],
});
```

- [ ] **Step 3: Create the shell wrapper**

```bash
#!/usr/bin/env bash
# 04-web-ui.sh — Web UI smoke tests via Playwright
source "$(cd "$(dirname "$0")/.." && pwd)/smoke-lib.sh"

section "Web UI (Playwright)"

PW_DIR="$SCRIPT_DIR/smoke/playwright"

# Check if Playwright is installed
if [[ ! -d "$PW_DIR/node_modules" ]]; then
  echo "Installing Playwright dependencies..."
  (cd "$PW_DIR" && npm install && npx playwright install chromium) || {
    fail "Playwright install failed"
    smoke_summary
    exit 1
  }
fi

# Run Playwright tests
(cd "$PW_DIR" && npx playwright test 2>&1) | while IFS= read -r line; do
  echo "  $line"
  if echo "$line" | grep -qE '^\s+\d+ passed'; then
    _PW_PASSED=$(echo "$line" | grep -oE '\d+ passed' | grep -oE '\d+')
  fi
  if echo "$line" | grep -qE '^\s+\d+ failed'; then
    _PW_FAILED=$(echo "$line" | grep -oE '\d+ failed' | grep -oE '\d+')
  fi
done

PW_EXIT=${PIPESTATUS[0]}
if [[ "$PW_EXIT" -eq 0 ]]; then
  pass "Playwright tests passed"
else
  fail "Playwright tests failed (exit $PW_EXIT)"
fi

smoke_summary
```

- [ ] **Step 4: Make executable**

```bash
chmod +x scripts/integration/smoke/04-web-ui.sh
```

- [ ] **Step 5: Commit**

```bash
git add scripts/integration/smoke/playwright/package.json \
        scripts/integration/smoke/playwright/playwright.config.ts \
        scripts/integration/smoke/04-web-ui.sh
git commit -m "feat: add Playwright scaffold for web UI smoke tests"
```

---

### Task 6: Web UI Tests — Playwright Specs

**Files:**
- Create: `scripts/integration/smoke/playwright/tests/navigation.spec.ts`
- Create: `scripts/integration/smoke/playwright/tests/workflow-run.spec.ts`
- Create: `scripts/integration/smoke/playwright/tests/credentials.spec.ts`
- Create: `scripts/integration/smoke/playwright/tests/sse-streaming.spec.ts`

- [ ] **Step 1: Create navigation.spec.ts**

Tests that all major pages load and render content. In dev mode, the app auto-sets `localStorage.token = 'dev-token'` after fetching `/api/config`.

```typescript
import { test, expect } from '@playwright/test';

// Dev mode auto-login: the app fetches /api/config, sees dev_no_auth=true,
// and sets localStorage.token = 'dev-token'. We just need to visit any page.

test.describe('Page navigation', () => {

  test('workflow library loads', async ({ page }) => {
    await page.goto('/workflows');
    await expect(page.getByRole('heading', { name: /Workflow Library/i })).toBeVisible();
    // Verify at least some builtin workflows appear
    await expect(page.getByText('Sandbox Test')).toBeVisible();
    await expect(page.getByText('PR Review')).toBeVisible();
    await expect(page.getByText('Bug Fix')).toBeVisible();
  });

  test('runs page loads', async ({ page }) => {
    await page.goto('/runs');
    await expect(page.getByRole('heading', { name: /Runs/i })).toBeVisible();
  });

  test('inbox page loads', async ({ page }) => {
    await page.goto('/inbox');
    await expect(page.getByRole('heading', { name: /Inbox/i })).toBeVisible();
  });

  test('reports page loads', async ({ page }) => {
    await page.goto('/reports');
    await expect(page.getByRole('heading', { name: /Reports/i })).toBeVisible();
  });

  test('knowledge page loads', async ({ page }) => {
    await page.goto('/knowledge');
    await expect(page.getByRole('heading', { name: /Knowledge/i })).toBeVisible();
  });

  test('settings page loads', async ({ page }) => {
    await page.goto('/settings');
    // Settings page shows credentials management
    await expect(page.getByText(/Credentials/i)).toBeVisible();
  });

  test('system health page loads', async ({ page }) => {
    await page.goto('/system');
    await expect(page.getByText(/System/i)).toBeVisible();
  });

});
```

- [ ] **Step 2: Create workflow-run.spec.ts**

Tests launching a workflow from the UI and verifying it appears in the runs list.

```typescript
import { test, expect } from '@playwright/test';

test.describe('Workflow execution from UI', () => {

  test('launch sandbox-test from workflow detail page', async ({ page }) => {
    // Navigate to workflow detail
    await page.goto('/workflows/sandbox-test');
    await expect(page.getByText('Sandbox Test')).toBeVisible();

    // Fill in parameters (if the form is visible)
    const durationInput = page.locator('input[name="duration"], [data-param="duration"] input');
    if (await durationInput.isVisible({ timeout: 3000 }).catch(() => false)) {
      await durationInput.fill('3');
    }

    // Click the run/start button
    const runButton = page.getByRole('button', { name: /Run|Start|Execute/i });
    await expect(runButton).toBeVisible();
    await runButton.click();

    // Should navigate to the run detail page or show success
    await expect(page).toHaveURL(/\/runs\/[0-9a-f-]+/, { timeout: 10000 });

    // Verify run detail page shows the workflow name
    await expect(page.getByText('Sandbox Test')).toBeVisible({ timeout: 5000 });
  });

  test('runs list shows recent runs', async ({ page }) => {
    await page.goto('/runs');
    await expect(page.getByRole('heading', { name: /Runs/i })).toBeVisible();

    // Wait for table to populate (auto-refreshes every 5s)
    await page.waitForTimeout(2000);

    // There should be at least one run from our tests
    const rows = page.locator('table tbody tr, [data-run-id]');
    await expect(rows.first()).toBeVisible({ timeout: 10000 });
  });

});
```

- [ ] **Step 3: Create credentials.spec.ts**

Tests CRUD operations on the credentials settings page.

```typescript
import { test, expect } from '@playwright/test';

test.describe('Credentials management', () => {

  test('create and delete a credential', async ({ page }) => {
    await page.goto('/settings');
    await expect(page.getByText(/Credentials/i)).toBeVisible();

    // Click add/create button
    const addButton = page.getByRole('button', { name: /Add|Create|New/i });
    await expect(addButton).toBeVisible();
    await addButton.click();

    // Fill in credential form
    const nameInput = page.locator('input[name="name"], input[placeholder*="name" i]');
    await nameInput.fill('PLAYWRIGHT_SMOKE_CRED');

    const valueInput = page.locator('input[name="value"], input[placeholder*="value" i], input[type="password"]');
    await valueInput.fill('playwright-test-value');

    // Submit
    const saveButton = page.getByRole('button', { name: /Save|Create|Add/i }).last();
    await saveButton.click();

    // Verify it appears in the list
    await expect(page.getByText('PLAYWRIGHT_SMOKE_CRED')).toBeVisible({ timeout: 5000 });

    // Delete it
    const row = page.locator('tr, [data-credential]').filter({ hasText: 'PLAYWRIGHT_SMOKE_CRED' });
    const deleteButton = row.getByRole('button', { name: /Delete|Remove/i });
    await deleteButton.click();

    // Confirm deletion if there's a dialog
    const confirmButton = page.getByRole('button', { name: /Confirm|Yes|Delete/i });
    if (await confirmButton.isVisible({ timeout: 2000 }).catch(() => false)) {
      await confirmButton.click();
    }

    // Verify it's gone
    await expect(page.getByText('PLAYWRIGHT_SMOKE_CRED')).not.toBeVisible({ timeout: 5000 });
  });

});
```

- [ ] **Step 4: Create sse-streaming.spec.ts**

Submits a long-running sandbox-test via the API, navigates to the run detail page, selects the running step, and observes whether the LogStream component receives events in real-time. Captures diagnostic info to help debug the known SSE frontend issue.

```typescript
import { test, expect } from '@playwright/test';

// Helper: submit a run via API and return the run ID
async function submitSandboxTest(baseURL: string): Promise<{ runId: string }> {
  // Fetch a dev JWT by calling dev-login
  const loginRes = await fetch(`${baseURL}/api/auth/dev-login`);
  const { token } = await loginRes.json() as { token: string };

  const res = await fetch(`${baseURL}/api/runs`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`,
    },
    body: JSON.stringify({
      workflow_id: 'sandbox-test',
      parameters: { duration: 20, command2: 'echo sse-test-done' },
    }),
  });
  const body = await res.json() as { id: string };
  return { runId: body.id };
}

test.describe('SSE log streaming', () => {

  test('logs stream in real-time while step is running', async ({ page, baseURL }) => {
    test.setTimeout(90_000);

    // 1. Submit a 20-second sandbox-test via API
    const { runId } = await submitSandboxTest(baseURL!);

    // 2. Navigate to run detail page
    await page.goto(`/runs/${runId}`);
    await expect(page.getByText('Sandbox Test')).toBeVisible({ timeout: 10_000 });

    // 3. Wait for the run_command step to appear as "running"
    //    The step timeline/DAG updates via SSE or polling.
    const runCommandStep = page.locator('text=Run shell command, text=run_command').first();
    await expect(runCommandStep).toBeVisible({ timeout: 30_000 });

    // 4. Click on the step to mount the LogStream component
    await runCommandStep.click();

    // 5. Diagnostic: capture browser state
    const cookies = await page.context().cookies();
    const flTokenCookie = cookies.find(c => c.name === 'fl_token');
    const localStorageToken = await page.evaluate(() => localStorage.getItem('token'));

    console.log('--- SSE Diagnostic ---');
    console.log(`fl_token cookie: ${flTokenCookie ? 'present' : 'MISSING'}`);
    console.log(`localStorage.token: ${localStorageToken ? 'present' : 'MISSING'}`);

    // 6. Capture network requests to the SSE endpoint
    const sseRequests: { url: string; status: number; headers: Record<string, string> }[] = [];
    page.on('response', response => {
      if (response.url().includes('/api/runs/steps/') && response.url().includes('/logs')) {
        sseRequests.push({
          url: response.url(),
          status: response.status(),
          headers: response.headers(),
        });
        console.log(`SSE response: ${response.status()} ${response.url()}`);
      }
    });

    // 7. Capture console errors
    const consoleErrors: string[] = [];
    page.on('console', msg => {
      if (msg.type() === 'error') {
        consoleErrors.push(msg.text());
      }
    });

    // 8. Screenshot: initial state of log panel
    await page.waitForTimeout(2_000);
    await page.screenshot({ path: 'test-results/sse-01-initial.png', fullPage: true });

    // 9. Wait for log lines to appear in the LogStream container
    //    The container has classes: font-mono text-xs text-green-400
    const logContainer = page.locator('.font-mono.text-green-400');
    await expect(logContainer).toBeVisible({ timeout: 5_000 });

    // 10. Check for streaming content — look for "tick" lines
    let sawTick = false;
    let initialCount = 0;
    let finalCount = 0;

    // Wait up to 15s for at least one tick line
    for (let i = 0; i < 15; i++) {
      const text = await logContainer.textContent() ?? '';
      const tickMatches = text.match(/\[tick \d+/g);
      if (tickMatches && tickMatches.length > 0) {
        sawTick = true;
        initialCount = tickMatches.length;
        console.log(`Found ${initialCount} tick lines after ${i + 1}s`);
        break;
      }
      await page.waitForTimeout(1_000);
    }

    // 11. Screenshot: after waiting for ticks
    await page.screenshot({ path: 'test-results/sse-02-after-wait.png', fullPage: true });

    // 12. If we saw ticks, wait 5 more seconds and check the count increased
    if (sawTick) {
      await page.waitForTimeout(5_000);
      const text = await logContainer.textContent() ?? '';
      const tickMatches = text.match(/\[tick \d+/g);
      finalCount = tickMatches?.length ?? 0;
      console.log(`After 5s more: ${finalCount} tick lines (was ${initialCount})`);
    }

    // 13. Screenshot: final state
    await page.screenshot({ path: 'test-results/sse-03-final.png', fullPage: true });

    // 14. Print diagnostics regardless of outcome
    console.log('--- SSE Network ---');
    for (const req of sseRequests) {
      console.log(`  ${req.status} ${req.url}`);
      console.log(`  content-type: ${req.headers['content-type'] ?? 'none'}`);
    }
    if (sseRequests.length === 0) {
      console.log('  NO SSE requests captured — EventSource may not have connected');
    }

    console.log('--- Console Errors ---');
    for (const err of consoleErrors) {
      console.log(`  ${err}`);
    }

    // 15. Assertions
    expect(sawTick, 'Expected to see at least one [tick N/20] log line while step is running').toBe(true);
    expect(finalCount, 'Expected log count to increase over 5 seconds (real-time streaming)').toBeGreaterThan(initialCount);
  });

});
```

- [ ] **Step 5: Install Playwright and run tests locally**

```bash
cd scripts/integration/smoke/playwright
npm install
npx playwright install chromium
npx playwright test
```

Expected: Navigation tests pass. SSE test may fail (capturing diagnostics is the point). Fix any locator issues for workflow-run and credentials tests.

- [ ] **Step 6: Commit**

```bash
git add scripts/integration/smoke/playwright/tests/
git commit -m "feat: add Playwright specs including SSE streaming diagnostic test"
```

---

### Task 7: Standalone SSE Test Script (`run-sse-test.sh`)

**Files:**
- Create: `scripts/integration/run-sse-test.sh`

A standalone script (like the existing `run-sandbox-test.sh`) that tests SSE log streaming end-to-end without Playwright. It submits a long-running sandbox-test, connects to both the backend SSE endpoint (via curl) and monitors the database, verifying logs arrive incrementally. This is useful for isolating whether a streaming issue is backend or frontend.

- [ ] **Step 1: Create the script**

```bash
#!/usr/bin/env bash
# run-sse-test.sh — Verify SSE log streaming works end-to-end.
#
# Tests three levels:
#   1. Worker → DB: logs are inserted incrementally during execution
#   2. DB → SSE: the SSE endpoint delivers events in real-time
#   3. (Optional) Browser: run with --playwright to also test the web UI
#
# Usage:
#   scripts/integration/run-sse-test.sh
#   scripts/integration/run-sse-test.sh --playwright
set -euo pipefail
source "$(dirname "$0")/dev-env.sh"
cd "$PROJECT_ROOT"

USE_PLAYWRIGHT=false
[[ "${1:-}" == "--playwright" ]] && USE_PLAYWRIGHT=true

# ── Generate JWT ──────────────────────────────────────────────────
TEAM_ID="${DEV_TEAM_ID:-$(fl_sql "SELECT id FROM teams LIMIT 1" | tr -d ' \n')}"
USER_ID="${DEV_USER_ID:-$(fl_sql "SELECT id FROM users LIMIT 1" | tr -d ' \n')}"

TMPJWT=$(mktemp -d)/genjwt.go
cat > "$TMPJWT" <<'GOEOF'
package main
import (
	"fmt"; "os"; "time"
	"github.com/golang-jwt/jwt/v5"
)
type Claims struct {
	UserID    string            `json:"user_id"`
	TeamRoles map[string]string `json:"team_roles"`
	jwt.RegisteredClaims
}
func main() {
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID:    os.Args[1],
		TeamRoles: map[string]string{os.Args[2]: "admin"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	})
	s, _ := t.SignedString([]byte(os.Args[3]))
	fmt.Print(s)
}
GOEOF
JWT=$(go run "$TMPJWT" "$USER_ID" "$TEAM_ID" "$JWT_SECRET")
command rm -f "$TMPJWT"

API="http://localhost:8080"
AUTH=(-H "Authorization: Bearer $JWT" -H "X-Team-ID: $TEAM_ID")

# ── Submit sandbox-test with 20s duration ─────────────────────────
echo "=== SSE Streaming Test ==="
echo ""
echo "Submitting sandbox-test (duration=20)..."
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST \
  "${AUTH[@]}" -H "Content-Type: application/json" \
  -d '{"workflow_id":"sandbox-test","parameters":{"duration":20,"command2":"echo sse-done"}}' \
  "$API/api/runs")
HTTP_CODE=$(echo "$RESPONSE" | tail -1)
BODY=$(echo "$RESPONSE" | sed '$d')

if [[ "$HTTP_CODE" != "201" ]]; then
  echo "FAIL: Could not submit run (HTTP $HTTP_CODE)"
  exit 1
fi

RUN_ID=$(echo "$BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
TEMPORAL_ID=$(echo "$BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)['temporal_id'])")
echo "Run ID:      $RUN_ID"
echo "Temporal ID: $TEMPORAL_ID"

# ── Wait for step to start running ────────────────────────────────
echo ""
echo "Waiting for run_command step to start..."
STEP_RUN_ID=""
for i in $(seq 1 30); do
  STEP_RUN_ID=$(fl_sql "SELECT id FROM step_runs WHERE run_id = '$RUN_ID' AND step_id = 'run_command' AND status = 'running' LIMIT 1" 2>/dev/null | tr -d ' \n')
  if [[ -n "$STEP_RUN_ID" ]]; then
    break
  fi
  sleep 1
done

if [[ -z "$STEP_RUN_ID" ]]; then
  echo "FAIL: run_command step never reached 'running' status"
  exit 1
fi
echo "Step Run ID: $STEP_RUN_ID (running)"

# ══════════════════════════════════════════════════════════════════
# TEST 1: Worker → DB (logs inserted incrementally)
# ══════════════════════════════════════════════════════════════════
echo ""
echo "── Test 1: Worker → DB (incremental log insertion) ──"

PASS1=true
COUNTS=()
for i in $(seq 1 6); do
  COUNT=$(fl_sql "SELECT count(*) FROM step_run_logs WHERE step_run_id = '$STEP_RUN_ID'" | tr -d ' \n')
  COUNTS+=("$COUNT")
  echo "  [t+${i}s] $COUNT log rows in DB"
  sleep 2
done

# Check that counts are strictly increasing
INCREASING=true
for ((i=1; i<${#COUNTS[@]}; i++)); do
  if [[ "${COUNTS[$i]}" -le "${COUNTS[$((i-1))]}" ]]; then
    INCREASING=false
    break
  fi
done

if [[ "$INCREASING" == "true" ]]; then
  echo "  PASS: Log count increased every 2s (${COUNTS[0]} → ${COUNTS[-1]})"
else
  echo "  FAIL: Log count did not increase monotonically: ${COUNTS[*]}"
  PASS1=false
fi

# ══════════════════════════════════════════════════════════════════
# TEST 2: DB → SSE (endpoint streams events in real-time)
# ══════════════════════════════════════════════════════════════════
echo ""
echo "── Test 2: DB → SSE (real-time event delivery) ──"

SSE_TMPFILE=$(mktemp)
# Connect to SSE for 8 seconds in background
timeout 8 curl -s -N \
  "${AUTH[@]}" \
  "$API/api/runs/steps/$STEP_RUN_ID/logs" > "$SSE_TMPFILE" 2>/dev/null &
SSE_PID=$!

# Wait for curl to finish
wait "$SSE_PID" 2>/dev/null || true

SSE_LINES=$(grep -c "^data:" "$SSE_TMPFILE" 2>/dev/null || echo "0")
SSE_TICKS=$(grep -c "tick" "$SSE_TMPFILE" 2>/dev/null || echo "0")

echo "  SSE lines received: $SSE_LINES"
echo "  Lines containing 'tick': $SSE_TICKS"

PASS2=true
if [[ "$SSE_LINES" -gt 0 ]]; then
  echo "  PASS: SSE endpoint delivered $SSE_LINES events"
else
  echo "  FAIL: SSE endpoint delivered 0 events"
  PASS2=false
fi

if [[ "$SSE_TICKS" -gt 1 ]]; then
  echo "  PASS: Multiple tick events arrived (real-time streaming confirmed)"
else
  echo "  FAIL: Expected multiple tick events, got $SSE_TICKS"
  PASS2=false
fi

# Check for "connected" comment (SSE handshake)
if grep -q ": connected" "$SSE_TMPFILE"; then
  echo "  PASS: SSE handshake (': connected' comment received)"
else
  echo "  FAIL: No SSE handshake — ': connected' comment missing"
  PASS2=false
fi

rm -f "$SSE_TMPFILE"

# ══════════════════════════════════════════════════════════════════
# Wait for workflow to finish
# ══════════════════════════════════════════════════════════════════
echo ""
echo "Waiting for workflow to complete..."
for i in $(seq 1 30); do
  DESCRIBE=$(temporal workflow describe --workflow-id "$TEMPORAL_ID" --address "$TEMPORAL_ADDRESS" 2>/dev/null || echo "")
  if echo "$DESCRIBE" | grep -q "COMPLETED"; then
    echo "Workflow completed."
    break
  elif echo "$DESCRIBE" | grep -qE "FAILED|TERMINATED"; then
    echo "Workflow failed."
    break
  fi
  sleep 2
done

# ══════════════════════════════════════════════════════════════════
# TEST 3 (optional): Browser — Playwright SSE test
# ══════════════════════════════════════════════════════════════════
if [[ "$USE_PLAYWRIGHT" == "true" ]]; then
  echo ""
  echo "── Test 3: Browser SSE (Playwright) ──"
  PW_DIR="$PROJECT_ROOT/scripts/integration/smoke/playwright"
  if [[ ! -d "$PW_DIR/node_modules" ]]; then
    (cd "$PW_DIR" && npm install && npx playwright install chromium)
  fi
  (cd "$PW_DIR" && npx playwright test sse-streaming.spec.ts 2>&1) | sed 's/^/  /'
  PW_EXIT=${PIPESTATUS[0]}
  if [[ "$PW_EXIT" -eq 0 ]]; then
    echo "  PASS: Playwright SSE test passed"
  else
    echo "  FAIL: Playwright SSE test failed (screenshots in $PW_DIR/test-results/)"
  fi
fi

# ══════════════════════════════════════════════════════════════════
# Summary
# ══════════════════════════════════════════════════════════════════
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  SSE Test Results:"
if [[ "$PASS1" == "true" ]]; then echo "    Worker → DB:  PASS"; else echo "    Worker → DB:  FAIL"; fi
if [[ "$PASS2" == "true" ]]; then echo "    DB → SSE:     PASS"; else echo "    DB → SSE:     FAIL"; fi
if [[ "$USE_PLAYWRIGHT" == "true" ]]; then
  if [[ "${PW_EXIT:-1}" -eq 0 ]]; then echo "    Browser SSE:  PASS"; else echo "    Browser SSE:  FAIL"; fi
fi
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

[[ "$PASS1" == "true" && "$PASS2" == "true" ]]
```

- [ ] **Step 2: Make executable**

```bash
chmod +x scripts/integration/run-sse-test.sh
```

- [ ] **Step 3: Run the standalone SSE test**

Run: `scripts/integration/run-sse-test.sh`
Expected: Test 1 (Worker → DB) and Test 2 (DB → SSE) both pass.

Run: `scripts/integration/run-sse-test.sh --playwright`
Expected: Test 3 (Browser) may fail — capturing diagnostics.

- [ ] **Step 4: Commit**

```bash
git add scripts/integration/run-sse-test.sh
git commit -m "feat: add standalone SSE streaming test (backend + optional Playwright)"
```

---

### Task 8: Top-Level Runner (`smoke-test.sh`)

**Files:**
- Create: `scripts/integration/smoke-test.sh`

Runs all four test layers in sequence and reports a combined summary.

- [ ] **Step 1: Create the runner script**

```bash
#!/usr/bin/env bash
# smoke-test.sh — Run the full fleetlift smoke test suite
#
# Usage:
#   scripts/integration/smoke-test.sh              # Run all layers
#   scripts/integration/smoke-test.sh api           # Run only API tests
#   scripts/integration/smoke-test.sh cli           # Run only CLI tests
#   scripts/integration/smoke-test.sh workflows     # Run only workflow tests
#   scripts/integration/smoke-test.sh web           # Run only web UI tests
#   SMOKE_TIER3=1 scripts/integration/smoke-test.sh # Include expensive workflow tests

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SMOKE_DIR="$SCRIPT_DIR/smoke"

LAYERS=("$@")
if [[ ${#LAYERS[@]} -eq 0 ]]; then
  LAYERS=(api cli workflows web)
fi

TOTAL_PASS=0
TOTAL_FAIL=0
TOTAL_SKIP=0
FAILED_LAYERS=()

run_layer() {
  local name="$1" script="$2"
  echo ""
  echo "╔══════════════════════════════════════════╗"
  echo "║  $name"
  echo "╚══════════════════════════════════════════╝"

  if "$script"; then
    echo "  >>> $name: PASSED"
  else
    echo "  >>> $name: FAILURES DETECTED"
    FAILED_LAYERS+=("$name")
  fi
}

for layer in "${LAYERS[@]}"; do
  case "$layer" in
    api)       run_layer "API Endpoints"     "$SMOKE_DIR/01-api.sh" ;;
    cli)       run_layer "CLI Commands"      "$SMOKE_DIR/02-cli.sh" ;;
    workflows) run_layer "Workflow E2E"      "$SMOKE_DIR/03-workflows.sh" ;;
    web)       run_layer "Web UI"            "$SMOKE_DIR/04-web-ui.sh" ;;
    *)         echo "Unknown layer: $layer (valid: api, cli, workflows, web)"; exit 1 ;;
  esac
done

echo ""
echo "╔══════════════════════════════════════════╗"
echo "║  SMOKE TEST SUMMARY"
echo "╚══════════════════════════════════════════╝"

if [[ ${#FAILED_LAYERS[@]} -eq 0 ]]; then
  echo "  All layers passed."
  exit 0
else
  echo "  Failed layers:"
  for f in "${FAILED_LAYERS[@]}"; do
    echo "    - $f"
  done
  exit 1
fi
```

- [ ] **Step 2: Make executable**

```bash
chmod +x scripts/integration/smoke-test.sh
```

- [ ] **Step 3: Run the full suite**

Run: `scripts/integration/smoke-test.sh`
Expected: All layers pass (Tier 2/3 workflows may skip depending on credentials).

- [ ] **Step 4: Commit**

```bash
git add scripts/integration/smoke-test.sh
git commit -m "feat: add top-level smoke test runner with layer selection"
```

---

### Task 9: Wire Up and Fix Failures

This task is about running the full suite against the real stack, fixing any issues, and making all tests green.

**Files:**
- Modify: Any test file that has assertion failures or selector mismatches

- [ ] **Step 1: Build the CLI binary**

```bash
cd /home/andrew/dev/projects/fleetlift/worktree-pin-opensandbox-versions
go build -buildvcs=false -o bin/fleetlift ./cmd/cli
```

- [ ] **Step 2: Install Playwright browsers**

```bash
cd scripts/integration/smoke/playwright
npm install
npx playwright install chromium
```

- [ ] **Step 3: Run API tests, fix failures**

```bash
scripts/integration/smoke/01-api.sh
```

Fix any assertion or endpoint issues. Common fixes: adjust expected HTTP status codes, fix JSON field names in `json_field` calls.

- [ ] **Step 4: Run CLI tests, fix failures**

```bash
scripts/integration/smoke/02-cli.sh
```

Fix any CLI output parsing issues. The `run start` output format may need adjustment.

- [ ] **Step 5: Run workflow tests, fix failures**

```bash
scripts/integration/smoke/03-workflows.sh
```

Fix any timeout or step verification issues.

- [ ] **Step 6: Run Playwright tests, fix selectors**

```bash
cd scripts/integration/smoke/playwright && npx playwright test
```

Fix any DOM selectors. Use `npx playwright test --headed` to see the browser and debug. Common fixes: adjust `getByRole` names, `locator` selectors, and timeouts.

- [ ] **Step 7: Run full suite end-to-end**

```bash
scripts/integration/smoke-test.sh
```

All layers should pass.

- [ ] **Step 8: Commit**

```bash
git add scripts/integration/
git commit -m "fix: stabilize smoke test suite against running stack"
```

---

### Task 10: Add .gitignore and Documentation

**Files:**
- Modify: `.gitignore`
- Create: `scripts/integration/smoke/playwright/.gitignore`

- [ ] **Step 1: Create Playwright .gitignore**

```
node_modules/
test-results/
playwright-report/
```

- [ ] **Step 2: Add entry to project .gitignore if needed**

Check if `node_modules` is already ignored globally. If not, add:

```
scripts/integration/smoke/playwright/node_modules/
scripts/integration/smoke/playwright/test-results/
```

- [ ] **Step 3: Commit**

```bash
git add .gitignore scripts/integration/smoke/playwright/.gitignore
git commit -m "chore: add gitignore for Playwright smoke test artifacts"
```
