#!/usr/bin/env bash
# Build all fleetlift binaries
set -euo pipefail
cd "$(dirname "$0")/../.."
go build -o bin/fleetlift-worker ./cmd/worker
go build -o bin/fleetlift ./cmd/cli
echo "Build OK"
