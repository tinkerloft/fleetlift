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

# Load credentials from .env file if not already set in environment
if [[ -f ".env" ]]; then
  if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
    export $(grep -E '^ANTHROPIC_API_KEY=' .env | xargs) 2>/dev/null || true
  fi
  if [[ -z "${CLAUDE_CODE_OAUTH_TOKEN:-}" ]]; then
    export $(grep -E '^CLAUDE_CODE_OAUTH_TOKEN=' .env | xargs) 2>/dev/null || true
  fi
fi

if [[ -z "${ANTHROPIC_API_KEY:-}" && -z "${CLAUDE_CODE_OAUTH_TOKEN:-}" ]]; then
  echo "WARNING: Neither ANTHROPIC_API_KEY nor CLAUDE_CODE_OAUTH_TOKEN is set — Claude Code will not authenticate inside the sandbox."
  echo "  Set one in the environment or create a .env file."
fi

OPEN_SANDBOX_DOMAIN=http://localhost:8090 \
OPEN_SANDBOX_USE_SERVER_PROXY=true \
ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY:-}" \
CLAUDE_CODE_OAUTH_TOKEN="${CLAUDE_CODE_OAUTH_TOKEN:-}" \
  ./bin/fleetlift-worker >> "$LOGFILE" 2>&1 &

echo $! > "$PIDFILE"
echo "Worker started (PID $(cat $PIDFILE)), log: $LOGFILE"
