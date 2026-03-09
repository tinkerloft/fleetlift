#!/usr/bin/env bash
# Stop the background fleetlift-worker.
PIDFILE=/tmp/fleetlift-worker.pid
if [[ -f "$PIDFILE" ]]; then
  PID=$(cat "$PIDFILE")
  if kill -0 "$PID" 2>/dev/null; then
    kill "$PID"
    echo "Worker (PID $PID) stopped"
  else
    echo "Worker not running"
  fi
  rm -f "$PIDFILE"
else
  echo "No PID file found"
fi
