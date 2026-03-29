#!/usr/bin/env bash
# 04-web-ui.sh — Web UI smoke tests via Playwright
source "$(cd "$(dirname "$0")/.." && pwd)/smoke-lib.sh"

section "Web UI (Playwright)"

PW_DIR="$SCRIPT_DIR/smoke/playwright"

# Check if Playwright is installed
if [[ ! -d "$PW_DIR/node_modules" ]]; then
  echo "Installing Playwright dependencies..."
  (cd "$PW_DIR" && npm install && npx playwright install chromium) || {
    fail "Playwright install failed"
    smoke_summary
    exit 1
  }
fi

# Run Playwright tests
PW_OUTPUT=$( (cd "$PW_DIR" && npx playwright test 2>&1) )
PW_EXIT=$?

echo "$PW_OUTPUT" | sed 's/^/  /'

if [[ "$PW_EXIT" -eq 0 ]]; then
  pass "Playwright tests passed"
else
  fail "Playwright tests failed (exit $PW_EXIT)"
fi

smoke_summary
