# Fleetlift Enhancement Backlog

All tracked items have been moved to `docs/plans/ROADMAP.md`.

---

## Pending Triage

Items below have not yet been placed on the roadmap.

### Background Document Assessor

Run overnight across every repository with recent changes, check the documents against the code, report back findings and/or raise PRs to fix.

### End-to-End Code Change Manager

Manage a code change from start to finish: creation → CI → fixes → handle review comments → CI → notify user to take over.

### Agent MCP / Skill Profiles

Workflows declare an `agent_profile` that installs plugins, skills, and MCPs into the Claude agent sandbox before execution, with eval-time plugin injection support.

### User prompt injecting into exisitng workflows
- Tailoring PR review prompts

### User authoring new workflows

## Remove 'mode: [report|transform]' from workflow schema

## PRD for platform improvements
- docs/plans/2026-03-18-workflow-expressiveness-prd.md


### Worker-restart-safe ExecuteStep

**Problem:** If the Fleetlift worker process is restarted while a `ExecuteStep` activity is running
(e.g. via `restart.sh` to deploy a build), the activity loses its heartbeat connection to Temporal.
After `HeartbeatTimeout` (currently 2 minutes), Temporal marks attempt 1 as failed and schedules a
retry (attempt 2, `MaximumAttempts: 2`). Attempt 2 typically fails immediately with `context canceled`
due to a race in Temporal's task-token management when both the heartbeat timeout and retry scheduling
happen simultaneously for multiple activities. The net effect: any fan-out step that was still running
at restart time will fail permanently.

**Root cause research:**
- `ExecuteStep` heartbeats on every agent event (line 212 of `execute.go`) — correct and necessary.
- `HeartbeatTimeout: 2 * time.Minute` — detects dead workers within 2 min, which is fine.
- `MaximumAttempts: 2` — allows one retry, but the retry is not idempotent: it restarts the Claude
  Code agent from scratch inside the same sandbox. If the agent had already done significant work
  (file edits, tool calls), that work is lost and duplicated.
- The sandbox stays alive independently of the worker, so the git clone, MCP setup, and any files
  written to /workspace survive the restart. Only the in-memory agent conversation is lost.

**What a proper fix looks like:**
Two layers are needed:

1. **Activity-level checkpoint via heartbeat details**: Before running the agent, record a "started"
   heartbeat with a token (e.g. sandbox ID + timestamp). On retry, call `activity.GetInfo(ctx).HeartbeatDetails`
   to detect "this is a retry — the sandbox is already set up". Then decide whether to:
   - **Skip** (if the agent already produced a result and wrote it to a known file in /workspace), or
   - **Re-run** the agent from scratch (safe for idempotent read-only steps like assessors).
   This requires the agent to write its JSON output to a deterministic path (e.g. `/workspace/.fleetlift-output.json`)
   so the retry can detect completion.

2. **Per-step retry decision in workflow YAML**: Some steps are safely retryable (read-only assessors,
   report generators); others are not (PRs already opened, commits already pushed). The workflow schema
   should allow `retry_on_worker_restart: true/false` per step, defaulting to false for `transform` mode.

**Workaround today:** Avoid restarting the worker while workflows are actively running. Check
`scripts/integration/status.sh` and wait for in-flight runs to complete before calling `restart.sh`.

### Snagging issues
 * Costs are collected or displaued
 * Run durations aren't shown against individual steps
 * Inputs **for a step** aren't shown, only the overall DAG
 * Outputs from each step don't appear as individual reports / artifacts
 * Outputs are not human readable, they're structured JSON.  How do I see at a glance, and download, a report that's easy to consume ?
