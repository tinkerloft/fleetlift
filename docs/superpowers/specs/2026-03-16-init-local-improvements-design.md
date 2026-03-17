# init-local Improvements Design

**Date:** 2026-03-16
**Goal:** Make `fleetlift init-local` fully self-contained — after running it, the server and worker are running and the developer can open the browser immediately, with no manual steps.

---

## Problem

The current `init-local` wizard sets up config, seeds the database, and then tells the user to run `scripts/integration/start.sh` manually. Several issues make this friction-heavy for new users:

- Missing tool dependencies (`go`, `npm`, `temporal` CLI) are not checked upfront — they surface as confusing errors mid-flow
- Port 8080 conflicts from stale processes are silently masked by `start.sh`'s health check
- Binaries are never built by the wizard, so `start.sh` fails with "Binaries not found"
- The frontend requires a `localStorage` token that the wizard never sets up for dev mode (fixed separately)

---

## Constraints

**Working directory:** `init-local` must be invoked from the repository root. Existing code already relies on relative paths (`internal/db/schema.sql`, `scripts/integration/dev-env.sh`, `make build`). The wizard checks for `go.mod` in cwd and exits with a clear error if not in the repo root.

Note: `~/.fleetlift/local.env` and `~/.fleetlift/auth.json` use absolute paths derived from `os.UserHomeDir()` — they are unaffected by the working directory constraint.

---

## Design

### Approach: Preflight phase + build step + auto-start

Keep the existing wizard structure. Add a silent preflight phase at the top, move binary builds to before the database step, and add a start-and-verify step at the end.

---

### 1. Preflight Checks (new, before any prompts)

Run silently before the first user prompt. All failures exit immediately except port 8080 which prompts first.

| Check | Pass condition | On failure |
|-------|---------------|-----------|
| Repo root | `go.mod` exists in cwd | Exit: "Run init-local from the repo root" |
| Docker running | `docker info` exits 0 | Exit: "Start Docker Desktop and re-run" |
| `go` in PATH | `exec.LookPath("go")` succeeds | Exit: "Install Go: brew install go" |
| `npm` in PATH | `exec.LookPath("npm")` succeeds | Exit: "Install Node.js: brew install node" |
| `temporal` in PATH | `exec.LookPath("temporal")` succeeds | Exit: "Install Temporal CLI: brew install temporal" |
| Port 8080 free | TCP connect to `:8080` fails | Prompt then exit (see below) |

`temporal` CLI is required for developer workflow inspection (`temporal workflow list`, etc.) and `scripts/integration/status.sh`. It is not used by the server binary itself.

**Port 8080 kill flow:**
1. Get PID(s) via `exec.Command("lsof", "-ti", ":8080")` — parse newline-separated integers. Note: `lsof` is macOS/Linux only; this is acceptable given the project's macOS-primary target (Windows is out of scope).
2. Prompt user (huh Confirm): "Port 8080 is in use by PID N — kill it? [y/N]"
3. If **No**: exit with "Free port 8080 and re-run"
4. If **Yes**: send `SIGTERM` to each PID, then poll `lsof -ti :8080` every 500ms for up to 3s
5. If port still occupied after 3s: exit with "Could not free port 8080 — kill PID N manually and re-run"
6. Continue wizard

There is no `portWasOccupied` flag — by the time the wizard reaches step 11, port 8080 is either free or the wizard has already exited.

---

### 2. Revised Step Order

```
1.  Preflight checks                         (new)
2.  Prompt: Claude auth
3.  Prompt: GitHub token
4.  Write ~/.fleetlift/local.env
5.  Patch scripts/integration/dev-env.sh
6.  Build binaries                           (new, before services)
7.  Start Docker stacks                      (auto-detect if already running)
8.  Ensure DB + apply schema + seed dev identity
9.  Seed GitHub token (if provided)
10. Write ~/.fleetlift/auth.json
11. Start server + verify                    (new)
```

Building before Docker means a compile failure aborts before any services are started. Build is placed after the prompts (steps 2–5) intentionally — there is no value in burning build time before the user has provided their API credentials.

---

### 3. Build Step (step 6)

Runs automatically — no user prompt. Freshness check skips the build on re-runs where nothing has changed.

**Fresh check:** call `filepath.Walk(".")` (relative path — the wizard runs from repo root) to find the most recently modified source file. Check:
- `.go` files: `strings.HasSuffix(path, ".go")`
- Frontend source: `strings.HasPrefix(path, "web/src/")`

Using `"."` (not an absolute path) ensures the prefix check `strings.HasPrefix(path, "web/src/")` matches correctly.

Compare the latest source mod time against the **older** of `bin/fleetlift-server` and `bin/fleetlift-worker` mod times. Using the older binary ensures both binaries are current. If both binaries exist and are newer than all source files, skip.

Do not use `filepath.Match("**/*.go")` — Go's `filepath.Match` does not support `**`. Use `filepath.Walk` with explicit suffix/prefix checks as described above.

Covering both Go and frontend source ensures that `git pull` changes to `web/src/` trigger a rebuild, since `make build` re-embeds the compiled frontend into the Go binary.

- Fresh → `✓ Binaries up to date (skipping build)`
- Stale/missing → `[build] Running make build...` → `✓ Binaries built`

**Track result:** store `rebuilt bool` for use in the success banner message (e.g. "Binaries rebuilt and server restarted" vs "Server restarted"). Step 11 always restarts regardless of this flag.

---

### 4. Docker Step (step 7)

Before showing the "Start Docker Compose stacks?" prompt, auto-detect running containers using two separate invocations (the two compose files are independent):

```
docker compose ps --services --filter status=running
docker compose -f docker-compose.opensandbox.yaml ps --services --filter status=running
```

If the first outputs both `temporal` and `postgres`, and the second outputs `opensandbox-server`, skip the prompt:
`✓ Docker services already running (skipping)`

Otherwise, show the existing prompt and proceed as before.

---

### 5. Start + Verify Step (step 11)

After `auth.json` is written:

**Liveness check:** the server is considered "alive" only when **both** conditions hold:
- `kill -0 <pid>` succeeds (PID file exists and process is running)
- `GET http://localhost:8080/health` returns HTTP 200

**Decision logic:** always run `scripts/integration/start.sh`. It calls `stop.sh` internally, handles graceful shutdown of any running processes, and then starts fresh. No skip logic — always restarting is simpler and correct (e.g. template YAML or schema changes require a fresh server process even if no Go code changed).

Do NOT call `stop.sh` separately before `start.sh` — `start.sh` already calls it internally.

**Health endpoint:** the wizard uses `GET /health` for its final confirmation check. (`start.sh` polls `http://localhost:8080/` internally — both return 200 when the server is up. The wizard always uses `/health`.)

**After `start.sh` exits:**
- Check exit code first: if non-zero, print `start.sh`'s captured stderr and exit with "Server failed to start — check the output above". Do not attempt to read the server log — the failure may be a `start.sh` preflight (e.g. OpenSandbox not reachable on `:8090`) with no server log written yet
- If exit code is 0: run one `GET /health` check
- If 200: show success banner
- If not 200: print last 20 lines of `/tmp/fleetlift-server.log` and suggest `scripts/integration/logs.sh`

```
┌─────────────────────────────────────────┐
│   Fleetlift is running!                 │
│   http://localhost:8080                 │
│   Temporal UI: http://localhost:8233    │
└─────────────────────────────────────────┘
```

---

### 6. Re-run Idempotency

Each step detects existing state and skips cleanly on subsequent runs:

| Step | Skip condition |
|------|----------------|
| Write `local.env` | File exists — prompt to overwrite (existing behaviour) |
| Build binaries | Both binaries newer than newest source file (`.go` or `web/src/**`) |
| Docker stacks | All three services running per two `docker compose ps` calls |
| DB seed | `ON CONFLICT DO NOTHING` — already idempotent |
| GitHub token | Already in DB — prompt to overwrite (existing behaviour) |
| Start server | Never skipped — always restarts via `start.sh` |

A typical re-run after `git pull` that changed Go or frontend source: rebuilds binaries, skips Docker/DB/token steps, restarts the server.

---

## Files Changed

- `cmd/cli/init_local.go` — all logic changes (preflight, build step, docker auto-detect, start+verify)
- No changes to `start.sh`, `stop.sh`, `dev-env.sh`, or other scripts

---

## Out of Scope

- Windows support
- Production deployment
- Multi-user or shared-environment setup
- Changing the Docker compose configuration
