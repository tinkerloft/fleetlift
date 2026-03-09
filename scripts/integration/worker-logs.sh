#!/usr/bin/env bash
# Tail the worker log. Pass -n N to get last N lines (default 50).
N=${1:-50}
tail -n "$N" /tmp/fleetlift-worker.log 2>/dev/null || echo "No log file yet"
