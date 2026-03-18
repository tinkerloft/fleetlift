# Troubleshooting

Common issues and solutions for Fleetlift v2.

---

## 1. Worker not connecting to Temporal

**Symptom:** Worker exits immediately or logs `temporal connect: ...` errors.

**Causes and fixes:**

- Temporal is not running. Start it with `docker compose up -d` and verify it is healthy.
- `TEMPORAL_ADDRESS` is wrong or not set. Default is `localhost:7233`. Ensure the value matches the Temporal server's gRPC port.
- Firewall or Docker networking issue. Try `curl -v telnet://localhost:7233` to verify connectivity.

```bash
export TEMPORAL_ADDRESS=localhost:7233
go run ./cmd/worker
```

---

## 2. Sandbox provisioning failures

**Symptom:** Steps fail with errors like `provision sandbox: ...` or `opensandbox: 401 Unauthorized`.

**Causes and fixes:**

- `OPENSANDBOX_DOMAIN` is not set or points to the wrong URL.
- `OPENSANDBOX_API_KEY` is missing or expired.
- The OpenSandbox service is unreachable. Check network access from the worker host.

```bash
export OPENSANDBOX_DOMAIN=https://api.opensandbox.example.com
export OPENSANDBOX_API_KEY=your-key
go run ./cmd/worker
```

---

## 3. JWT auth errors

**Symptom:** API returns `401 Unauthorized` or `invalid token` even after login.

**Causes and fixes:**

- `JWT_SECRET` is not set on the server. The server will exit at startup if `JWT_SECRET` is empty.
- The secret changed between login and the current request. Old tokens are invalid. Re-run `fleetlift auth login`.
- The CLI is pointing at a different server than the one where the token was issued.

```bash
# Server
export JWT_SECRET=your-secret
go run ./cmd/server

# CLI re-auth
fleetlift auth login
```

---

## 4. Agent not starting

**Symptom:** Steps immediately fail with `agent error: ANTHROPIC_API_KEY not set` or the sandbox starts but the agent produces no output.

**Causes and fixes:**

- `ANTHROPIC_API_KEY` is not set in the worker's environment. The worker injects it into each sandbox.
- The `AGENT_IMAGE` does not include Claude Code. Verify the image tag is correct.

```bash
export ANTHROPIC_API_KEY=sk-ant-...
export AGENT_IMAGE=claude-code:latest
go run ./cmd/worker
```

---

## 5. Database migration errors

**Symptom:** Server fails to start with `connect db: ...` or SQL schema errors.

**Causes and fixes:**

- `DATABASE_URL` is wrong or the database is not running. Default: `postgres://fleetlift:fleetlift@localhost:5432/fleetlift`.
- The schema is out of date. The server applies migrations automatically at startup via the embedded `schema.sql`.
- Conflicting manual schema changes. Inspect the database and reconcile with `internal/db/schema.sql`.

```bash
export DATABASE_URL=postgres://fleetlift:fleetlift@localhost:5432/fleetlift
docker compose up -d   # ensure postgres is running
go run ./cmd/server
```

---

## 6. Web UI shows blank page

**Symptom:** Browser shows an empty page or "SPA not yet built" when visiting the server.

**Cause:** The React SPA has not been compiled. The server embeds the `web/dist` directory at build time; if it is missing, the SPA handler returns a placeholder.

**Fix:**

```bash
cd web
npm install
npm run build
cd ..
go run ./cmd/server   # rebuild with embedded dist
```

---

## 7. CLI shows "unauthorized" after login

**Symptom:** Commands succeed right after `fleetlift auth login` but return `401` shortly after, or on a different machine.

**Causes and fixes:**

- The JWT has expired. Run `fleetlift auth login` again.
- The server's `JWT_SECRET` was rotated. All existing tokens are invalidated. Re-login.
- The CLI `--server` flag or `FLEETLIFT_SERVER` points to a different server instance.
- Verify your identity: `curl -H "Authorization: Bearer $(cat ~/.fleetlift/auth.json | jq -r .token)" http://localhost:8080/api/me`

---

## 8. Steps stuck in "running"

**Symptom:** `fleetlift run get <id>` shows step status as `running` indefinitely. No new log lines appear.

**Causes and fixes:**

- The worker crashed. Check worker process logs. Restart with `go run ./cmd/worker`.
- The Temporal workflow is waiting for a signal that was never sent (e.g. an approve/reject for a step with `approval_policy: always`). Check `fleetlift inbox list`.
- OpenSandbox container failed silently. Check the OpenSandbox dashboard or API logs.
- The step has no heartbeat and its activity timed out in Temporal. Open the Temporal UI (default: `http://localhost:8233`) to inspect workflow history and pending activities.

```bash
# Check inbox for pending approvals
fleetlift inbox list

# Stream logs to see if the agent is still active
fleetlift run logs <id>
```
