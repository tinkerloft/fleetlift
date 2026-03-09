#!/usr/bin/env bash
# Start fleetlift-worker in the background with OpenSandbox env vars.
# Writes PID to /tmp/fleetlift-worker.pid and logs to /tmp/fleetlift-worker.log
set -euo pipefail
cd "$(dirname "$0")/../.."

PIDFILE=/tmp/fleetlift-worker.pid
LOGFILE=/tmp/fleetlift-worker.log

# Kill any running fleetlift-worker processes
pkill -f "fleetlift-worker" 2>/dev/null || true
sleep 1
rm -f "$PIDFILE"

# Load from .env file if present and key not already set
if [[ -z "${ANTHROPIC_API_KEY:-}" && -f ".env" ]]; then
  export $(grep -E '^ANTHROPIC_API_KEY=' .env | xargs) 2>/dev/null || true
fi

if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
  echo "WARNING: ANTHROPIC_API_KEY not set — Claude Code will not authenticate inside the sandbox."
  echo "  Set it in the environment or create a .env file with ANTHROPIC_API_KEY=sk-ant-..."
fi

OPEN_SANDBOX_DOMAIN=http://localhost:8090 \
OPEN_SANDBOX_USE_SERVER_PROXY=true \
ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY:-}" \
  ./bin/fleetlift-worker >> "$LOGFILE" 2>&1 &

echo $! > "$PIDFILE"
echo "Worker started (PID $(cat $PIDFILE)), log: $LOGFILE"
