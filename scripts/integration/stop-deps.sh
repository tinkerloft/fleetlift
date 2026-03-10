#!/usr/bin/env bash
# Stop Temporal dev server and OpenSandbox container.
set -euo pipefail

echo "[deps] Stopping worker..."
PIDFILE=/tmp/fleetlift-worker.pid
if [[ -f "$PIDFILE" ]]; then
  PID=$(cat "$PIDFILE")
  kill "$PID" 2>/dev/null || true
  rm -f "$PIDFILE"
  echo "[deps] Worker stopped"
fi

echo "[deps] Stopping Temporal..."
if [[ -f /tmp/temporal-dev.pid ]]; then
  PID=$(cat /tmp/temporal-dev.pid)
  kill "$PID" 2>/dev/null || true
  rm -f /tmp/temporal-dev.pid
fi
pkill -f "temporal server start-dev" 2>/dev/null || true
echo "[deps] Temporal stopped"

echo "[deps] Stopping OpenSandbox..."
docker rm -f fleetlift-opensandbox 2>/dev/null || true
echo "[deps] OpenSandbox stopped"
