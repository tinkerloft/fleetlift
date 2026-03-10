#!/usr/bin/env bash
# Full integration test: start deps, build, start worker, submit task, wait for result.
# Usage: run-integration-test.sh [--no-rebuild]
set -euo pipefail
cd "$(dirname "$0")/../.."

REBUILD=true
for arg in "$@"; do
  [[ "$arg" == "--no-rebuild" ]] && REBUILD=false
done

LOGFILE=/tmp/fleetlift-worker.log
RESULT_FILE=/tmp/fleetlift-last-result.json
TIMEOUT_SECS=600   # 10 minutes max

echo "============================================================"
echo " Fleetlift Integration Test - $(date)"
echo "============================================================"

# ── Load credentials ─────────────────────────────────────────────────────────
if [[ -f ".env" ]]; then
  if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
    export $(grep -E '^ANTHROPIC_API_KEY=' .env | xargs) 2>/dev/null || true
  fi
  if [[ -z "${CLAUDE_CODE_OAUTH_TOKEN:-}" ]]; then
    export $(grep -E '^CLAUDE_CODE_OAUTH_TOKEN=' .env | xargs) 2>/dev/null || true
  fi
fi
if [[ -z "${ANTHROPIC_API_KEY:-}" && -z "${CLAUDE_CODE_OAUTH_TOKEN:-}" ]]; then
  echo "ERROR: Neither ANTHROPIC_API_KEY nor CLAUDE_CODE_OAUTH_TOKEN is set"
  exit 1
fi

# ── 1. Start dependencies ────────────────────────────────────────────────────
echo ""
echo "── Step 1: Starting dependencies ──"
bash scripts/integration/start-deps.sh

# ── 2. Build ─────────────────────────────────────────────────────────────────
if [[ "$REBUILD" == "true" ]]; then
  echo ""
  echo "── Step 2: Building binaries ──"
  bash scripts/integration/build.sh
else
  echo ""
  echo "── Step 2: Skipping rebuild (--no-rebuild) ──"
fi

# ── 3. Start worker ──────────────────────────────────────────────────────────
echo ""
echo "── Step 3: Starting worker ──"
bash scripts/integration/start-worker.sh

# Give the worker a moment to connect to Temporal
sleep 3

# Verify worker connected
if ! grep -q "Connected to Temporal" "$LOGFILE" 2>/dev/null && \
   ! grep -q "Worker started" "$LOGFILE" 2>/dev/null; then
  echo "WARNING: Worker may not have connected. Last log lines:"
  tail -10 "$LOGFILE" 2>/dev/null || true
fi

# ── 4. Submit task ───────────────────────────────────────────────────────────
echo ""
echo "── Step 4: Submitting security audit task ──"
WORKFLOW_OUTPUT=$(bash scripts/integration/submit-security-audit.sh 2>&1) || {
  echo "ERROR: Failed to submit task:"
  echo "$WORKFLOW_OUTPUT"
  exit 1
}
echo "$WORKFLOW_OUTPUT"

# Extract workflow ID from output
WORKFLOW_ID=$(echo "$WORKFLOW_OUTPUT" | grep -oE 'transform-[a-zA-Z0-9_-]+' | head -1)
if [[ -z "$WORKFLOW_ID" ]]; then
  # Try to get it from temporal
  WORKFLOW_ID=$(temporal workflow list --limit 1 --output json 2>/dev/null \
    | grep -o '"workflowId":"[^"]*"' | head -1 | cut -d'"' -f4)
fi
echo "Workflow ID: $WORKFLOW_ID"

# ── 5. Wait for completion ───────────────────────────────────────────────────
echo ""
echo "── Step 5: Waiting for workflow completion (timeout: ${TIMEOUT_SECS}s) ──"
ELAPSED=0
POLL_INTERVAL=15
LAST_STATUS=""

while [[ $ELAPSED -lt $TIMEOUT_SECS ]]; do
  STATUS_OUTPUT=$(temporal workflow describe --workflow-id "$WORKFLOW_ID" --output json 2>/dev/null || echo '{}')
  STATUS=$(echo "$STATUS_OUTPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('workflowExecutionInfo',{}).get('status','UNKNOWN'))" 2>/dev/null || echo "UNKNOWN")

  if [[ "$STATUS" != "$LAST_STATUS" ]]; then
    echo "[$(date +%H:%M:%S)] Status: $STATUS"
    LAST_STATUS="$STATUS"
  fi

  case "$STATUS" in
    COMPLETED)
      echo ""
      echo "✓ Workflow COMPLETED successfully"
      break
      ;;
    FAILED|TERMINATED|CANCELED|TIMED_OUT)
      echo ""
      echo "✗ Workflow ended with status: $STATUS"
      echo ""
      echo "── Failure Details ──"
      bash scripts/integration/check-workflow.sh "$WORKFLOW_ID" 2>&1 | tail -60
      echo ""
      echo "── Worker Log (last 50 lines) ──"
      bash scripts/integration/worker-logs.sh 50
      exit 1
      ;;
  esac

  sleep $POLL_INTERVAL
  ELAPSED=$((ELAPSED + POLL_INTERVAL))
done

if [[ $ELAPSED -ge $TIMEOUT_SECS ]]; then
  echo ""
  echo "✗ Workflow timed out after ${TIMEOUT_SECS}s"
  echo ""
  echo "── Workflow History ──"
  bash scripts/integration/check-workflow.sh "$WORKFLOW_ID" 2>&1 | tail -80
  echo ""
  echo "── Worker Log (last 50 lines) ──"
  bash scripts/integration/worker-logs.sh 50
  exit 1
fi

# ── 6. Show result ───────────────────────────────────────────────────────────
echo ""
echo "── Step 6: Workflow Result ──"
./bin/fleetlift result --workflow-id "$WORKFLOW_ID" 2>&1 | tee "$RESULT_FILE" || true

echo ""
echo "============================================================"
echo " Integration test PASSED - $(date)"
echo "============================================================"
