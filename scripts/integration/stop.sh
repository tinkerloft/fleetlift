#!/usr/bin/env bash
# Stop the fleetlift worker and server.
# Does NOT stop docker-compose services (temporal, postgres, opensandbox).
set -euo pipefail
source "$(dirname "$0")/dev-env.sh"

stop_process() {
  local name=$1 pidfile=$2
  if [[ -f "$pidfile" ]]; then
    PID=$(cat "$pidfile")
    if kill -0 "$PID" 2>/dev/null; then
      kill "$PID"
      echo "[stop] $name (PID $PID) stopped"
    else
      echo "[stop] $name not running (stale PID $PID)"
    fi
    rm -f "$pidfile"
  else
    echo "[stop] $name not running (no PID file)"
  fi
}

stop_process "Worker" "$WORKER_PIDFILE"
stop_process "Server" "$SERVER_PIDFILE"

# Catch any strays
pkill -f "fleetlift-worker" 2>/dev/null || true
pkill -f "fleetlift-server" 2>/dev/null || true
