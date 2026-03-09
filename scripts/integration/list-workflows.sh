#!/usr/bin/env bash
# List recent Temporal workflows.
# Usage: list-workflows.sh [limit]
N=${1:-10}
temporal workflow list --limit "$N"
