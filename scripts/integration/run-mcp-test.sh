#!/usr/bin/env bash
# Run the mcp-test workflow and verify MCP sidecar integration.
# Exercises: sidecar provisioning → health check → 7 API endpoints → DB state.
# Usage: run-mcp-test.sh [--with-agent]
set -euo pipefail
source "$(dirname "$0")/dev-env.sh"
cd "$PROJECT_ROOT"

# ── Parse flags ──────────────────────────────────────────────────────────────
INCLUDE_AGENT="false"
TIMEOUT=60
if [[ "${1:-}" == "--with-agent" ]]; then
  INCLUDE_AGENT="true"
  TIMEOUT=120
fi

# ── Check prerequisites ─────────────────────────────────────────────────────
if [[ ! -f bin/fleetlift-mcp ]]; then
  echo "ERROR: bin/fleetlift-mcp not found."
  echo "  Run: make mcp-sidecar"
  exit 1
fi

if ! file bin/fleetlift-mcp | grep -q ELF; then
  echo "ERROR: bin/fleetlift-mcp is not a Linux ELF binary (sandbox requires linux/amd64)."
  echo "  Run: make mcp-sidecar"
  exit 1
fi

export FLEETLIFT_MCP_BINARY_PATH="$PROJECT_ROOT/bin/fleetlift-mcp"
echo "MCP binary: $FLEETLIFT_MCP_BINARY_PATH"

# Check worker is running
if [[ -f "$WORKER_PIDFILE" ]] && kill -0 "$(cat "$WORKER_PIDFILE")" 2>/dev/null; then
  echo "Worker PID: $(cat "$WORKER_PIDFILE")"
else
  echo "ERROR: Worker is not running."
  echo "  Run: scripts/integration/start.sh"
  exit 1
fi

# ── Generate JWT ──────────────────────────────────────────────────────────────
TEAM_ID=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c \
  "SELECT id FROM teams LIMIT 1" 2>/dev/null | tr -d ' \n')
USER_ID=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c \
  "SELECT id FROM users LIMIT 1" 2>/dev/null | tr -d ' \n')

if [[ -z "$TEAM_ID" || -z "$USER_ID" ]]; then
  echo "ERROR: No team or user found in database."
  echo "  Ensure the database is seeded with at least one team and user."
  exit 1
fi

TMPDIR_JWT=$(mktemp -d)
TMPJWT="$TMPDIR_JWT/genjwt.go"
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
rm -rf "$TMPDIR_JWT"

API="http://localhost:8080"
AUTH=(-H "Authorization: Bearer $JWT" -H "X-Team-ID: $TEAM_ID")

# ── Verify server is up ──────────────────────────────────────────────────────
if ! curl -sf "${AUTH[@]}" "$API/api/workflows" > /dev/null 2>&1; then
  echo "ERROR: Server not responding or auth failed."
  echo "  Run: scripts/integration/start.sh --build"
  exit 1
fi

# ── Submit mcp-test workflow ─────────────────────────────────────────────────
echo "Submitting mcp-test workflow (include_agent_step=$INCLUDE_AGENT)..."
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST \
  "${AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d "{
    \"workflow_id\": \"mcp-test\",
    \"parameters\": {
      \"include_agent_step\": \"$INCLUDE_AGENT\"
    }
  }" \
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
echo "Waiting for workflow to complete (timeout: ${TIMEOUT}s)..."
STATUS=""
for i in $(seq 1 $((TIMEOUT / 2))); do
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
  echo "TIMEOUT: Workflow did not complete in ${TIMEOUT} seconds"
  exit 1
fi

# ── Verify results ────────────────────────────────────────────────────────────
echo ""
echo "=== DB Verification ==="
OK=true

# Check step statuses
STEP_RESULTS=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c "
SELECT json_agg(json_build_object(
  'step_id', step_id,
  'status', status
)) FROM step_runs WHERE run_id = '$RUN_ID'" 2>/dev/null | tr -d '\n ')

echo "$STEP_RESULTS" | python3 -c "
import sys, json
steps = json.load(sys.stdin)
for s in steps:
    print(f\"  {s['step_id']}: {s['status']}\")
"

# Check shell step completed
SHELL_STATUS=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c \
  "SELECT status FROM step_runs WHERE run_id = '$RUN_ID' AND step_id = 'verify_mcp_endpoints'" 2>/dev/null | tr -d ' \n')
if [[ "$SHELL_STATUS" == "complete" ]]; then
  echo "PASS: verify_mcp_endpoints completed"
else
  echo "FAIL: verify_mcp_endpoints status=$SHELL_STATUS"
  OK=false
fi

# Check artifact was created
ARTIFACT_COUNT=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c \
  "SELECT count(*) FROM artifacts a JOIN step_runs sr ON a.step_run_id = sr.id WHERE sr.run_id = '$RUN_ID' AND a.name = 'mcp-test-shell.txt'" 2>/dev/null | tr -d ' \n')
if [[ "$ARTIFACT_COUNT" -ge 1 ]]; then
  echo "PASS: artifact 'mcp-test-shell.txt' found"
else
  echo "FAIL: artifact 'mcp-test-shell.txt' not found"
  OK=false
fi

# Check progress was written (may be overwritten by final output — accept either)
PROGRESS=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c \
  "SELECT output->'progress'->>'percentage' FROM step_runs WHERE run_id = '$RUN_ID' AND step_id = 'verify_mcp_endpoints'" 2>/dev/null | tr -d ' \n')
if [[ "$PROGRESS" == "25" ]]; then
  echo "PASS: progress percentage=25 found in step output"
else
  echo "INFO: progress percentage not found in final output (may have been overwritten — POST /progress returned 200 during execution)"
fi

# Check knowledge item was created
KNOWLEDGE_COUNT=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c \
  "SELECT count(*) FROM knowledge_items WHERE team_id = '$TEAM_ID' AND summary = 'mcp-test-learning'" 2>/dev/null | tr -d ' \n')
if [[ "$KNOWLEDGE_COUNT" -ge 1 ]]; then
  echo "PASS: knowledge item 'mcp-test-learning' found"
else
  echo "FAIL: knowledge item 'mcp-test-learning' not found"
  OK=false
fi

# ── Agent step verification (if enabled) ──────────────────────────────────────
if [[ "$INCLUDE_AGENT" == "true" ]]; then
  AGENT_STATUS=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c \
    "SELECT status FROM step_runs WHERE run_id = '$RUN_ID' AND step_id = 'agent_uses_mcp'" 2>/dev/null | tr -d ' \n')
  if [[ "$AGENT_STATUS" == "complete" ]]; then
    echo "PASS: agent_uses_mcp completed"
  else
    echo "FAIL: agent_uses_mcp status=$AGENT_STATUS"
    OK=false
  fi

  AGENT_ARTIFACT=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c \
    "SELECT count(*) FROM artifacts a JOIN step_runs sr ON a.step_run_id = sr.id WHERE sr.run_id = '$RUN_ID' AND a.name = 'mcp-test-agent.txt'" 2>/dev/null | tr -d ' \n')
  if [[ "$AGENT_ARTIFACT" -ge 1 ]]; then
    echo "PASS: agent artifact 'mcp-test-agent.txt' found"
  else
    echo "FAIL: agent artifact 'mcp-test-agent.txt' not found"
    OK=false
  fi
fi

# ── Cleanup test data ─────────────────────────────────────────────────────────
docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -c \
  "DELETE FROM knowledge_items WHERE summary = 'mcp-test-learning'" 2>/dev/null || true

# ── Final result ──────────────────────────────────────────────────────────────
echo ""
if $OK; then
  echo "mcp-test: PASSED"
else
  echo "mcp-test: FAILED"
  exit 1
fi
