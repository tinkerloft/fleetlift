#!/usr/bin/env bash
# Submit the standard integration-test security audit task.
# Prints the workflow ID on stdout.
set -euo pipefail
cd "$(dirname "$0")/../.."

./bin/fleetlift run \
  --repo https://github.com/sindresorhus/p-limit \
  --repo https://github.com/sindresorhus/p-map \
  --repo https://github.com/sindresorhus/p-queue \
  --no-approval \
  --parallel \
  --mode report \
  --prompt "Quick security audit: look only at the root-level source files and package.json (do not recurse into node_modules or subdirectories). Identify the single most significant security risk, or note if none found. Write a short 2-3 sentence report."
