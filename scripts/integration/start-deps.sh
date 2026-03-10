#!/usr/bin/env bash
# Start Temporal dev server and OpenSandbox in the background.
# Idempotent - safe to re-run.
set -euo pipefail

TEMPORAL_LOG=/tmp/temporal-dev.log
OPENSANDBOX_CONTAINER=fleetlift-opensandbox

# ── 1. Temporal ──────────────────────────────────────────────────────────────
if temporal workflow list --limit 1 &>/dev/null; then
  echo "[deps] Temporal already running"
else
  echo "[deps] Starting Temporal dev server..."
  temporal server start-dev \
    --ui-port 8233 \
    --db-filename /tmp/temporal-fleetlift.db \
    > "$TEMPORAL_LOG" 2>&1 &
  echo $! > /tmp/temporal-dev.pid
  # Wait for Temporal to be ready
  for i in $(seq 1 30); do
    if temporal workflow list --limit 1 &>/dev/null 2>&1; then
      echo "[deps] Temporal ready"
      break
    fi
    sleep 1
    if [[ $i -eq 30 ]]; then
      echo "[deps] ERROR: Temporal failed to start. Log:"
      tail -20 "$TEMPORAL_LOG"
      exit 1
    fi
  done
fi

# ── 2. OpenSandbox ───────────────────────────────────────────────────────────
if curl -sf http://localhost:8090/health &>/dev/null; then
  echo "[deps] OpenSandbox already running on :8090"
elif docker ps --filter "name=$OPENSANDBOX_CONTAINER" --filter "status=running" --format '{{.Names}}' | grep -q "$OPENSANDBOX_CONTAINER"; then
  echo "[deps] OpenSandbox already running"
else
  echo "[deps] Starting OpenSandbox server..."
  # Stop/remove any existing container
  docker rm -f "$OPENSANDBOX_CONTAINER" 2>/dev/null || true

  # Write config to a temp file (bind-mount it in)
  SANDBOX_CFG=/tmp/opensandbox-fleetlift.toml
  cat > "$SANDBOX_CFG" <<'TOML'
[server]
host = "0.0.0.0"
port = 8090
log_level = "INFO"

[runtime]
type = "docker"
execd_image = "opensandbox/execd:v1.0.6"

[egress]
image = "opensandbox/egress:v1.0.1"

[docker]
network_mode = "bridge"
host_ip = "host.docker.internal"
drop_capabilities = ["AUDIT_WRITE", "MKNOD", "NET_ADMIN", "NET_RAW", "SYS_ADMIN", "SYS_MODULE", "SYS_PTRACE", "SYS_TIME", "SYS_TTY_CONFIG"]
no_new_privileges = true
pids_limit = 512

[ingress]
mode = "direct"
TOML

  docker run -d \
    --name "$OPENSANDBOX_CONTAINER" \
    -p 8090:8090 \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v "$SANDBOX_CFG":/etc/opensandbox/config.toml:ro \
    -e SANDBOX_CONFIG_PATH=/etc/opensandbox/config.toml \
    opensandbox/server:latest

  # Wait for OpenSandbox to be ready
  for i in $(seq 1 30); do
    if curl -sf http://localhost:8090/health &>/dev/null; then
      echo "[deps] OpenSandbox ready"
      break
    fi
    sleep 1
    if [[ $i -eq 30 ]]; then
      echo "[deps] ERROR: OpenSandbox failed to start. Logs:"
      docker logs "$OPENSANDBOX_CONTAINER" 2>&1 | tail -20
      exit 1
    fi
  done
fi

echo "[deps] All dependencies ready"
echo "  Temporal UI:  http://localhost:8233"
echo "  OpenSandbox:  http://localhost:8090"
