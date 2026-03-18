#!/usr/bin/env bash
# Start the fleetlift worker and server in the background.
# Prerequisites: docker-compose services (temporal, postgres, opensandbox) must be running.
# Usage: start.sh [--build]
set -euo pipefail
source "$(dirname "$0")/dev-env.sh"
cd "$PROJECT_ROOT"

BUILD=false
for arg in "$@"; do
  [[ "$arg" == "--build" ]] && BUILD=true
done

# ── Preflight checks ─────────────────────────────────────────────────────────
echo "[start] Checking dependencies..."

if ! curl -sf http://localhost:8090/v1/sandboxes > /dev/null 2>&1; then
  echo "[start] ERROR: OpenSandbox not running on :8090"
  echo "  Run: docker compose up -d"
  exit 1
fi

TEMPORAL_HOST="${TEMPORAL_ADDRESS%:*}"
TEMPORAL_PORT="${TEMPORAL_ADDRESS##*:}"
if ! nc -z "$TEMPORAL_HOST" "$TEMPORAL_PORT" > /dev/null 2>&1; then
  echo "[start] ERROR: Temporal not running on $TEMPORAL_ADDRESS"
  echo "  Run: docker compose up -d"
  exit 1
fi

echo "[start] Dependencies OK"

# ── Build if requested ────────────────────────────────────────────────────────
if [[ "$BUILD" == "true" ]]; then
  echo "[start] Building..."
  make build
fi

if [[ ! -f bin/fleetlift-worker ]] || [[ ! -f bin/fleetlift-server ]]; then
  echo "[start] ERROR: Binaries not found. Run: make build"
  exit 1
fi

# ── Stop any existing processes ───────────────────────────────────────────────
bash "$(dirname "$0")/stop.sh" 2>/dev/null || true

# ── Start worker ──────────────────────────────────────────────────────────────
echo "[start] Starting worker..."

# Load API credentials from .env if present
if [[ -f ".env" ]]; then
  while IFS='=' read -r key value; do
    [[ "$key" =~ ^#.*$ || -z "$key" ]] && continue
    export "$key=$value"
  done < <(grep -E '^(ANTHROPIC_API_KEY|CLAUDE_CODE_OAUTH_TOKEN|GITHUB_TOKEN)=' .env 2>/dev/null || true)
fi

./bin/fleetlift-worker >> "$WORKER_LOGFILE" 2>&1 &
echo $! > "$WORKER_PIDFILE"
echo "[start] Worker started (PID $(cat $WORKER_PIDFILE)), log: $WORKER_LOGFILE"

# ── Start server ──────────────────────────────────────────────────────────────
echo "[start] Starting server..."
./bin/fleetlift-server >> "$SERVER_LOGFILE" 2>&1 &
echo $! > "$SERVER_PIDFILE"
echo "[start] Server started (PID $(cat $SERVER_PIDFILE)), log: $SERVER_LOGFILE"

# ── Wait for server to be ready ──────────────────────────────────────────────
for i in $(seq 1 10); do
  if curl -sf http://localhost:8080/ > /dev/null 2>&1; then
    echo "[start] Server ready at http://localhost:8080"
    break
  fi
  sleep 1
  if [[ $i -eq 10 ]]; then
    echo "[start] WARNING: Server may not be ready. Check: tail -20 $SERVER_LOGFILE"
  fi
done

echo "[start] All services running"
