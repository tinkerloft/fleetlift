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

# Run Playwright tests — capture exit code separately from output
PW_OUTPUT=""
PW_EXIT=0
PW_OUTPUT=$( cd "$PW_DIR" && npx playwright test 2>&1 ) || PW_EXIT=$?

echo "$PW_OUTPUT" | sed 's/^/  /'

# Extract the summary line (e.g. "10 passed", "1 failed", "10 passed (15.0s)")
PW_PASSED=$(echo "$PW_OUTPUT" | grep -oP '\d+ passed' | head -1 | grep -oP '\d+' || echo "0")
PW_FAILED=$(echo "$PW_OUTPUT" | grep -oP '\d+ failed' | head -1 | grep -oP '\d+' || echo "0")

if [[ "$PW_EXIT" -eq 0 ]]; then
  pass "Playwright tests passed (${PW_PASSED} passed)"
elif [[ "$PW_PASSED" -gt 0 && "$PW_FAILED" -gt 0 ]]; then
  fail "Playwright tests: ${PW_PASSED} passed, ${PW_FAILED} failed"
elif [[ "$PW_FAILED" -gt 0 ]]; then
  fail "Playwright tests failed (${PW_FAILED} failed)"
else
  fail "Playwright tests did not produce results (exit $PW_EXIT)"
  printf "    Output: %s\n" "${PW_OUTPUT:0:500}"
fi

smoke_summary
