# Fleetlift Enhancement Backlog

All tracked items are in [`docs/plans/ROADMAP.md`](docs/plans/ROADMAP.md).

---

## Worker-restart-safe ExecuteStep

**Problem:** If the worker is restarted while `ExecuteStep` is running, the activity loses its heartbeat. After `HeartbeatTimeout` (2 min), Temporal marks attempt 1 failed and retries. The retry typically fails immediately with `context canceled` due to a race in Temporal's task-token management. Any fan-out step running at restart time will fail permanently.

**Root cause:**
- `ExecuteStep` heartbeats on every agent event — correct and necessary.
- `MaximumAttempts: 2` allows one retry, but retries are not idempotent: the agent restarts from scratch. The sandbox survives (git clone, MCP setup, written files persist); only the in-memory conversation is lost.

**What a proper fix requires:**
1. **Heartbeat-detail checkpointing** — record a "started" token before running the agent. On retry, call `activity.GetInfo(ctx).HeartbeatDetails` to detect the restart, then either skip (if output already written to `/workspace/.fleetlift-output.json`) or re-run from scratch.
2. **Per-step retry flag in YAML** — `retry_on_worker_restart: true/false`, defaulting to `false` for `transform` mode steps (PRs may already be open).

**Workaround:** Check `scripts/integration/status.sh` and wait for in-flight runs to complete before calling `restart.sh`.
