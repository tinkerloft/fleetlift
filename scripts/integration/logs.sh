#!/usr/bin/env bash
# Tail worker and/or server logs.
# Usage: logs.sh [worker|server|both] [lines]
set -euo pipefail
source "$(dirname "$0")/dev-env.sh"

TARGET=${1:-both}
LINES=${2:-50}

case "$TARGET" in
  worker)
    echo "=== Worker log (last $LINES lines) ==="
    tail -n "$LINES" "$WORKER_LOGFILE" 2>/dev/null || echo "No worker log"
    ;;
  server)
    echo "=== Server log (last $LINES lines) ==="
    tail -n "$LINES" "$SERVER_LOGFILE" 2>/dev/null || echo "No server log"
    ;;
  both|*)
    echo "=== Worker log (last $LINES lines) ==="
    tail -n "$LINES" "$WORKER_LOGFILE" 2>/dev/null || echo "No worker log"
    echo ""
    echo "=== Server log (last $LINES lines) ==="
    tail -n "$LINES" "$SERVER_LOGFILE" 2>/dev/null || echo "No server log"
    ;;
esac
