#!/usr/bin/env bash
# Show status of all fleetlift services.
set -euo pipefail
source "$(dirname "$0")/dev-env.sh"

check_pid() {
  local name=$1 pidfile=$2
  if [[ -f "$pidfile" ]]; then
    PID=$(cat "$pidfile")
    if kill -0 "$PID" 2>/dev/null; then
      echo "  $name: running (PID $PID)"
    else
      echo "  $name: dead (stale PID $PID)"
    fi
  else
    echo "  $name: stopped"
  fi
}

echo "Fleetlift services:"
check_pid "Worker" "$WORKER_PIDFILE"
check_pid "Server" "$SERVER_PIDFILE"

echo ""
echo "Infrastructure:"
if curl -sf http://localhost:8080/ > /dev/null 2>&1; then
  echo "  API:         http://localhost:8080  ✓"
else
  echo "  API:         http://localhost:8080  ✗"
fi
if temporal workflow list --limit 1 --address "$TEMPORAL_ADDRESS" > /dev/null 2>&1; then
  echo "  Temporal:    $TEMPORAL_ADDRESS  ✓"
else
  echo "  Temporal:    $TEMPORAL_ADDRESS  ✗"
fi
if curl -sf http://localhost:8090/v1/sandboxes > /dev/null 2>&1; then
  echo "  OpenSandbox: http://localhost:8090  ✓"
else
  echo "  OpenSandbox: http://localhost:8090  ✗"
fi
if docker exec fleetlift-postgres-1 pg_isready -U fleetlift > /dev/null 2>&1; then
  echo "  PostgreSQL:  localhost:5432  ✓"
else
  echo "  PostgreSQL:  localhost:5432  ✗"
fi
