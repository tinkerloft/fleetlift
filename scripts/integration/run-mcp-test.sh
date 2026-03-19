#!/usr/bin/env bash
# Run the mcp-test workflow and verify MCP sidecar integration.
# Exercises: sidecar provisioning → health check → 7 API endpoints → Claude Code agent MCP tool calls.
# Usage: run-mcp-test.sh
set -euo pipefail
source "$(dirname "$0")/dev-env.sh"
cd "$PROJECT_ROOT"

# Helper: run psql via local binary or docker
dbquery() {
  if command -v psql &>/dev/null; then
    psql "$DATABASE_URL" -t -c "$1" 2>/dev/null
  else
    local PG=$(docker ps --format '{{.Names}}' | grep postgres | head -1)
    docker exec "$PG" psql -U fleetlift -d fleetlift -t -c "$1" 2>/dev/null
  fi
}

# ── Check prerequisites ─────────────────────────────────────────────────────
if [[ ! -f bin/fleetlift-mcp-amd64 ]] && [[ ! -f bin/fleetlift-mcp-arm64 ]]; then
  echo "ERROR: MCP sidecar binaries not found."
  echo "  Run: make mcp-sidecar"
  exit 1
fi

# Confirm at least one ELF binary exists for the sandbox (linux target)
MCP_ELF=""
for arch in amd64 arm64; do
  if [[ -f "bin/fleetlift-mcp-$arch" ]] && file "bin/fleetlift-mcp-$arch" | grep -q ELF; then
    MCP_ELF="bin/fleetlift-mcp-$arch"
    break
  fi
done
if [[ -z "$MCP_ELF" ]]; then
  echo "ERROR: No Linux ELF MCP binary found (sandbox requires linux/amd64 or linux/arm64)."
  echo "  Run: make mcp-sidecar"
  exit 1
fi
echo "MCP binary: $MCP_ELF"

# Check worker is running
if [[ -f "$WORKER_PIDFILE" ]] && kill -0 "$(cat "$WORKER_PIDFILE")" 2>/dev/null; then
  echo "Worker PID: $(cat "$WORKER_PIDFILE")"
else
  echo "ERROR: Worker is not running."
  echo "  Run: scripts/integration/start.sh"
  exit 1
fi

# ── Resolve team/user IDs ─────────────────────────────────────────────────────
TEAM_ID="${DEV_TEAM_ID:-}"
USER_ID="${DEV_USER_ID:-}"

if [[ -z "$TEAM_ID" || -z "$USER_ID" ]]; then
  TEAM_ID=$(dbquery "SELECT id FROM teams LIMIT 1" 2>/dev/null | tr -d ' \n')
  USER_ID=$(dbquery "SELECT id FROM users LIMIT 1" 2>/dev/null | tr -d ' \n')
fi

if [[ -z "$TEAM_ID" || -z "$USER_ID" ]]; then
  echo "ERROR: No team or user found. Set DEV_TEAM_ID/DEV_USER_ID or ensure DB is seeded."
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
echo "Submitting mcp-test workflow..."
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST \
  "${AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d '{"workflow_id": "mcp-test", "parameters": {}}' \
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

# ── Wait for completion (agent step takes longer) ─────────────────────────────
TIMEOUT=180
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

STEP_RESULTS=$(dbquery "
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

# Shell endpoint step
SHELL_STATUS=$(dbquery \
  "SELECT status FROM step_runs WHERE run_id = '$RUN_ID' AND step_id = 'verify_mcp_endpoints'" 2>/dev/null | tr -d ' \n')
if [[ "$SHELL_STATUS" == "complete" ]]; then
  echo "PASS: verify_mcp_endpoints completed"
else
  echo "FAIL: verify_mcp_endpoints status=$SHELL_STATUS"
  OK=false
fi

# Shell artifact
ARTIFACT_COUNT=$(dbquery \
  "SELECT count(*) FROM artifacts a JOIN step_runs sr ON a.step_run_id = sr.id WHERE sr.run_id = '$RUN_ID' AND a.name = 'mcp-test-shell.txt'" 2>/dev/null | tr -d ' \n')
if [[ "$ARTIFACT_COUNT" -ge 1 ]]; then
  echo "PASS: shell artifact 'mcp-test-shell.txt' found"
else
  echo "FAIL: shell artifact 'mcp-test-shell.txt' not found"
  OK=false
fi

# Knowledge item
KNOWLEDGE_COUNT=$(dbquery \
  "SELECT count(*) FROM knowledge_items WHERE team_id = '$TEAM_ID' AND summary = 'mcp-test-learning'" 2>/dev/null | tr -d ' \n')
if [[ "$KNOWLEDGE_COUNT" -ge 1 ]]; then
  echo "PASS: knowledge item 'mcp-test-learning' found"
else
  echo "FAIL: knowledge item 'mcp-test-learning' not found"
  OK=false
fi

# Claude Code agent step
AGENT_STATUS=$(dbquery \
  "SELECT status FROM step_runs WHERE run_id = '$RUN_ID' AND step_id = 'agent_uses_mcp'" 2>/dev/null | tr -d ' \n')
if [[ "$AGENT_STATUS" == "complete" ]]; then
  echo "PASS: agent_uses_mcp completed"
else
  echo "FAIL: agent_uses_mcp status=$AGENT_STATUS"
  OK=false
fi

# Agent artifact (proves the agent actually called mcp__fleetlift__artifact__create)
AGENT_ARTIFACT=$(dbquery \
  "SELECT count(*) FROM artifacts a JOIN step_runs sr ON a.step_run_id = sr.id WHERE sr.run_id = '$RUN_ID' AND a.name = 'mcp-test-agent.txt'" 2>/dev/null | tr -d ' \n')
if [[ "$AGENT_ARTIFACT" -ge 1 ]]; then
  echo "PASS: agent artifact 'mcp-test-agent.txt' found"
else
  echo "FAIL: agent artifact 'mcp-test-agent.txt' not found (agent did not call artifact.create)"
  OK=false
fi

# ── Cleanup test data ─────────────────────────────────────────────────────────
dbquery "DELETE FROM knowledge_items WHERE summary = 'mcp-test-learning'" 2>/dev/null || true

# ── Final result ──────────────────────────────────────────────────────────────
echo ""
if $OK; then
  echo "mcp-test: PASSED"
else
  echo "mcp-test: FAILED"
  exit 1
fi
