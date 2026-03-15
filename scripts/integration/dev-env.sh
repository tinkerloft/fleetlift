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

# Dev auth bypass — set DEV_NO_AUTH=1 to skip JWT validation in the server.
# DEV_TEAM_ID is the team ID injected into the dev claims.
export DEV_NO_AUTH="${DEV_NO_AUTH:-1}"
export DEV_TEAM_ID="${DEV_TEAM_ID:-$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c "SELECT id FROM teams LIMIT 1" 2>/dev/null | tr -d ' \n')}"
export DEV_USER_ID="${DEV_USER_ID:-$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c "SELECT id FROM users LIMIT 1" 2>/dev/null | tr -d ' \n')}"

# PID/log files
export WORKER_PIDFILE=/tmp/fleetlift-worker.pid
export SERVER_PIDFILE=/tmp/fleetlift-server.pid
export WORKER_LOGFILE=/tmp/fleetlift-worker.log
export SERVER_LOGFILE=/tmp/fleetlift-server.log
