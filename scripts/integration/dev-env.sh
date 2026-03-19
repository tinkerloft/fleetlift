#!/usr/bin/env bash
[ -f ~/.fleetlift/local.env ] && source ~/.fleetlift/local.env
# Shared dev environment variables for integration scripts.
# Source this file from other scripts: source "$(dirname "$0")/dev-env.sh"

# Project root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Load .env.local if it exists (contains DB, auth, API keys, etc.)
if [[ -f "$PROJECT_ROOT/.env.local" ]]; then
  source "$PROJECT_ROOT/.env.local"
fi

# Defaults for anything not set by .env.local
export DATABASE_URL="${DATABASE_URL:-postgres://fleetlift:fleetlift@localhost:5432/fleetlift?sslmode=disable}"
export TEMPORAL_ADDRESS="${TEMPORAL_ADDRESS:-localhost:7233}"
export OPENSANDBOX_DOMAIN="${OPENSANDBOX_DOMAIN:-http://localhost:8090}"
export JWT_SECRET="${JWT_SECRET:-dev-secret-for-testing-only-32chars!!}"
export CREDENTIAL_ENCRYPTION_KEY="${CREDENTIAL_ENCRYPTION_KEY:-0000000000000000000000000000000000000000000000000000000000000000}"
# MCP sidecar — enable structured tool API for agent sandboxes (arch suffix appended at runtime)
export FLEETLIFT_MCP_BINARY_PATH="${FLEETLIFT_MCP_BINARY_PATH:-$PROJECT_ROOT/bin/fleetlift-mcp}"

# ── Postgres container helper ────────────────────────────────────────────────
# Finds the running postgres container regardless of compose project name.
# Used by scripts that need to run psql via docker exec.
_fl_pg_container() {
  # Prefer the canonical name from docker-compose.yaml name: fleetlift
  for name in fleetlift-postgres-1; do
    if docker inspect "$name" &>/dev/null; then
      echo "$name"
      return
    fi
  done
  # Fall back to any running container with "postgres" in the name
  docker ps --format '{{.Names}}' 2>/dev/null | grep -i postgres | head -1
}

# Run a SQL query against the fleetlift database.
# Tries psql on host first, then docker exec into the postgres container.
fl_sql() {
  local query="$1"
  if command -v psql &>/dev/null; then
    psql "$DATABASE_URL" -tAc "$query" 2>/dev/null && return
  fi
  local container
  container=$(_fl_pg_container)
  if [[ -n "$container" ]]; then
    docker exec "$container" psql -U fleetlift -d fleetlift -tAc "$query" 2>/dev/null && return
  fi
  return 1
}

# Dev auth bypass — set DEV_NO_AUTH=1 to skip JWT validation in the server.
# DEV_TEAM_ID is the team ID injected into the dev claims.
export DEV_NO_AUTH="${DEV_NO_AUTH:-1}"
export DEV_TEAM_ID="${DEV_TEAM_ID:-$(fl_sql "SELECT id FROM teams LIMIT 1" | tr -d ' \n')}"
export DEV_USER_ID="${DEV_USER_ID:-$(fl_sql "SELECT id FROM users LIMIT 1" | tr -d ' \n')}"

# PID/log files
export WORKER_PIDFILE=/tmp/fleetlift-worker.pid
export SERVER_PIDFILE=/tmp/fleetlift-server.pid
export WORKER_LOGFILE=/tmp/fleetlift-worker.log
export SERVER_LOGFILE=/tmp/fleetlift-server.log
