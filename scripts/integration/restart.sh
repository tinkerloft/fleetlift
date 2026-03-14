#!/usr/bin/env bash
# Rebuild and restart the fleetlift worker and server.
set -euo pipefail
bash "$(dirname "$0")/stop.sh"
bash "$(dirname "$0")/start.sh" --build
