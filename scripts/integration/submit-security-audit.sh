#!/usr/bin/env bash
# Submit the standard integration-test security audit task.
# Prints the workflow ID on stdout.
set -euo pipefail
cd "$(dirname "$0")/../.."

./bin/fleetlift run \
  --repo https://github.com/expressjs/express \
  --repo https://github.com/fastify/fastify \
  --repo https://github.com/koajs/koa \
  --no-approval \
  --parallel \
  --mode report \
  --prompt "Perform a security audit of these repositories. For each repo: clone it, identify the top 3 security risks in the code, and produce a brief markdown report."
