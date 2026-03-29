#!/usr/bin/env bash
# smoke-test.sh — Run the full fleetlift smoke test suite
#
# Usage:
#   scripts/integration/smoke-test.sh              # Run all layers
#   scripts/integration/smoke-test.sh api           # Run only API tests
#   scripts/integration/smoke-test.sh cli           # Run only CLI tests
#   scripts/integration/smoke-test.sh workflows     # Run only workflow tests
#   scripts/integration/smoke-test.sh web           # Run only web UI tests
#   SMOKE_TIER3=1 scripts/integration/smoke-test.sh # Include expensive workflow tests

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SMOKE_DIR="$SCRIPT_DIR/smoke"

LAYERS=("$@")
if [[ ${#LAYERS[@]} -eq 0 ]]; then
  LAYERS=(api cli workflows web)
fi

FAILED_LAYERS=()

run_layer() {
  local name="$1" script="$2"
  echo ""
  echo "╔══════════════════════════════════════════╗"
  printf "║  %-40s║\n" "$name"
  echo "╚══════════════════════════════════════════╝"

  if "$script"; then
    echo "  >>> $name: PASSED"
  else
    echo "  >>> $name: FAILURES DETECTED"
    FAILED_LAYERS+=("$name")
  fi
}

for layer in "${LAYERS[@]}"; do
  case "$layer" in
    api)       run_layer "API Endpoints"     "$SMOKE_DIR/01-api.sh" ;;
    cli)       run_layer "CLI Commands"      "$SMOKE_DIR/02-cli.sh" ;;
    workflows) run_layer "Workflow E2E"      "$SMOKE_DIR/03-workflows.sh" ;;
    web)       run_layer "Web UI"            "$SMOKE_DIR/04-web-ui.sh" ;;
    *)         echo "Unknown layer: $layer (valid: api, cli, workflows, web)"; exit 1 ;;
  esac
done

echo ""
echo "╔══════════════════════════════════════════╗"
echo "║  SMOKE TEST SUMMARY                      ║"
echo "╚══════════════════════════════════════════╝"

if [[ ${#FAILED_LAYERS[@]} -eq 0 ]]; then
  echo "  All layers passed."
  exit 0
else
  echo "  Failed layers:"
  for f in "${FAILED_LAYERS[@]}"; do
    echo "    - $f"
  done
  exit 1
fi
