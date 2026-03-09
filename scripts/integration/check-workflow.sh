#!/usr/bin/env bash
# Show Temporal workflow history.
# Usage: check-workflow.sh [workflow-id]
# With no args, shows the most recent workflow.
set -euo pipefail

if [[ -n "${1:-}" ]]; then
  temporal workflow show --workflow-id "$1"
else
  # Get the most recent workflow ID and show it
  WF_ID=$(temporal workflow list --limit 1 --output json 2>/dev/null \
    | grep -o '"workflowId":"[^"]*"' | head -1 | cut -d'"' -f4)
  if [[ -z "$WF_ID" ]]; then
    echo "No workflows found"
    exit 1
  fi
  echo "=== Most recent workflow: $WF_ID ==="
  temporal workflow show --workflow-id "$WF_ID"
fi
