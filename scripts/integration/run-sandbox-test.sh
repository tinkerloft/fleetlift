#!/usr/bin/env bash
# Run the sandbox-test workflow and verify results.
# Exercises: sandbox create → shell exec → step output passing → cleanup.
# Usage: run-sandbox-test.sh
set -euo pipefail
source "$(dirname "$0")/dev-env.sh"
cd "$PROJECT_ROOT"

# ── Generate JWT ──────────────────────────────────────────────────────────────
TEAM_ID="${DEV_TEAM_ID:-$(fl_sql "SELECT id FROM teams LIMIT 1" | tr -d ' \n')}"
USER_ID="${DEV_USER_ID:-$(fl_sql "SELECT id FROM users LIMIT 1" | tr -d ' \n')}"

if [[ -z "$TEAM_ID" || -z "$USER_ID" ]]; then
  echo "ERROR: No team or user found in database."
  echo "  Ensure the database is seeded with at least one team and user."
  exit 1
fi

# Generate JWT using Go (write to temp file since go run - doesn't work on all versions)
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

# ── Verify server is up ──────────────────────────────────────────────────────
if ! curl -sf "${AUTH[@]}" "$API/api/workflows" > /dev/null 2>&1; then
  echo "ERROR: Server not responding or auth failed."
  echo "  Run: scripts/integration/start.sh --build"
  exit 1
fi

# ── Submit sandbox-test workflow ──────────────────────────────────────────────
echo "Submitting sandbox-test workflow..."
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST \
  "${AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d '{
    "workflow_id": "sandbox-test",
    "parameters": {
      "duration": 3,
      "command2": "echo received-from-step1"
    }
  }' \
  "$API/api/runs")

HTTP_CODE=$(echo "$RESPONSE" | tail -1)
BODY=$(echo "$RESPONSE" | sed '$d')

if [[ "$HTTP_CODE" != "201" ]]; then
  echo "ERROR: Failed to start workflow (HTTP $HTTP_CODE): $BODY"
  exit 1
fi

RUN_ID=$(echo "$BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
TEMPORAL_ID=$(echo "$BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)['temporal_id'])")
echo "Run ID:      $RUN_ID"
echo "Temporal ID: $TEMPORAL_ID"

# ── Wait for completion ───────────────────────────────────────────────────────
echo "Waiting for workflow to complete..."
for i in $(seq 1 30); do
  # Use text output and grep for status — more reliable across temporal CLI versions.
  DESCRIBE=$(temporal workflow describe \
    --workflow-id "$TEMPORAL_ID" \
    --address "$TEMPORAL_ADDRESS" 2>/dev/null || echo "")

  if echo "$DESCRIBE" | grep -q "Status.*COMPLETED"; then
    STATUS="COMPLETED"
    break
  elif echo "$DESCRIBE" | grep -qE "Status.*(FAILED|TERMINATED|CANCELED|TIMED_OUT)"; then
    STATUS=$(echo "$DESCRIBE" | grep "Status" | awk '{print $NF}')
    echo "FAILED: Workflow ended with status $STATUS"
    temporal workflow show --workflow-id "$TEMPORAL_ID" --address "$TEMPORAL_ADDRESS" 2>&1 | tail -20
    exit 1
  fi

  sleep 2
done

if [[ "${STATUS:-}" != "COMPLETED" ]]; then
  echo "TIMEOUT: Workflow did not complete in 60 seconds"
  exit 1
fi

# ── Verify results ────────────────────────────────────────────────────────────
echo ""
echo "=== Results ==="

STEP_RESULTS=$(fl_sql "SELECT json_agg(json_build_object('step_id', step_id, 'status', status, 'output', output, 'has_completed_at', completed_at IS NOT NULL)) FROM step_runs WHERE run_id = '$RUN_ID'" | tr -d '\n ')

echo "$STEP_RESULTS" | python3 -c "
import sys, json
steps = json.load(sys.stdin)
ok = True
for s in steps:
    status = s['status']
    step_id = s['step_id']
    has_completed = s['has_completed_at']
    output = s.get('output') or {}
    print(f\"  {step_id}: status={status}, completed_at={'yes' if has_completed else 'NO'}\")
    if output:
        stdout = output.get('stdout', '')[:80]
        print(f\"    stdout: {stdout!r}\")
    if status != 'complete':
        print(f\"    ERROR: expected 'complete', got '{status}'\")
        ok = False
    if not has_completed:
        print(f'    ERROR: completed_at not set')
        ok = False
print()
if ok:
    print('sandbox-test: PASSED')
else:
    print('sandbox-test: FAILED')
    sys.exit(1)
"
