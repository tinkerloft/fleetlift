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

assert_status_cli() {
  local test_name="$1" exit_code="$2"
  if [[ "$exit_code" == "0" ]]; then
    pass "$test_name"
  else
    fail "$test_name — exit code $exit_code"
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
  fl_sql "SELECT status FROM step_runs WHERE run_id = '$1' AND step_id = '$2'" 2>/dev/null | tr -d ' \n' || true
}

# step_output <run_id> <step_id>
step_output() {
  fl_sql "SELECT output FROM step_runs WHERE run_id = '$1' AND step_id = '$2'" 2>/dev/null | tr -d ' \n' || true
}

# step_count <run_id>
step_count() {
  fl_sql "SELECT count(*) FROM step_runs WHERE run_id = '$1'" 2>/dev/null | tr -d ' \n' || true
}

# ── Credential helpers ────────────────────────────────────────────
has_credential() {
  local name="$1"
  local exists
  exists=$(fl_sql "SELECT count(*) FROM credentials WHERE team_id = '$DEV_TEAM_ID' AND name = '$name'" 2>/dev/null | tr -d ' \n' || echo "0")
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
