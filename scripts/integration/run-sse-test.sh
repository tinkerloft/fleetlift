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
  echo "  Body: ${BODY:0:500}"
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
  STEP_RUN_ID=$(fl_sql "SELECT id FROM step_runs WHERE run_id = '$RUN_ID' AND step_id = 'run_command' AND status = 'running' LIMIT 1" 2>/dev/null | tr -d ' \n' || true)
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
  COUNT=$(fl_sql "SELECT count(*) FROM step_run_logs WHERE step_run_id = '$STEP_RUN_ID'" | tr -d ' \n' || true)
  COUNTS+=("$COUNT")
  echo "  [t+$((i*2))s] $COUNT log rows in DB"
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
PW_EXIT=0
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
  if [[ "$PW_EXIT" -eq 0 ]]; then echo "    Browser SSE:  PASS"; else echo "    Browser SSE:  FAIL"; fi
fi
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

[[ "$PASS1" == "true" && "$PASS2" == "true" ]]
