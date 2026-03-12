# Integration Testing Notes

Findings from end-to-end integration testing. Each entry describes a fragile area,
the observed failure mode, the fix applied (if any), and remaining risk.

---

## Session 1 — 2026-03-10

**Task:** 3-repo parallel security audit (express, fastify, koa) — report mode, no approval.

**Result:** PASSED (second attempt, ~15 min)

---

### Finding 1: Missing API key fails silently, 14 minutes later

**Category:** Configuration / Observability
**Status:** Partially mitigated (warning logged at startup; no hard fail)

**Observed:** Worker started without `ANTHROPIC_API_KEY`. Workflow ran for ~14 minutes, then
returned `"status":"failed"`, error: `claude code failed: exit code -1: \nOutput: `. The empty
output and exit code -1 are the only indicators the agent couldn't authenticate.

**Root cause:** The container runs `claude -p ...`. Without a key, Claude outputs "Not logged in"
and exits with a non-ExitError OS-level error (exit code -1 in Go). The error propagates through
the full timeout before the worker gives up.

**Fix applied:** None to the code. User added `.env` file; `start-worker.sh` already loads it.

**Remaining risk:**
- A missing key causes ~14 minutes of wasted compute before any visible failure
- The error message (`exit code -1, Output: ""`) is not actionable — no mention of auth
- Operator running `./bin/fleetlift result` sees `failed` with no useful detail
- **Suggested improvement:** Fail fast — before running Claude, verify the key is set and valid
  (e.g., a preflight check in the agent pipeline, or a worker startup hard-fail when
  `REQUIRE_CONFIG=true`)

---

### Finding 2: `fleetlift stream` fails on multi-repo workflows

**Category:** CLI UX / Multi-repo
**Status:** Fixed

**Observed:**
```
Error: failed to get sandbox ID: failed to query sandbox ID: unknown queryType get_sandbox_id.
KnownQueryTypes=[__stack_trace __open_sessions get_execution_progress]
```

**Root cause:** `get_sandbox_id` is only registered on `TransformV2` (exec child) workflows.
The `stream` command was querying the parent `Transform` workflow, which only has
`get_execution_progress`.

**Fix applied:** `stream` now:
1. Detects grouped workflows (by catching the unknown query error and extracting the task ID)
2. Lists running `TransformV2` exec child workflows via Temporal's ListWorkflow API
3. If one group: auto-selects it. If multiple: prints suggestions with `--group <name>` syntax
4. Added `--group <name>` flag to directly target a specific repo

**Remaining risk:**
- `ListExecWorkflows` uses a `STARTS_WITH` query that depends on the `{taskID}-` naming
  convention; any deviation breaks discovery
- Completed exec workflows are included in the list (not filtered to Running only); may confuse
  users who run `stream` on a finished workflow

---

### Finding 3: `stream` uses direct container endpoint by default

**Category:** Networking / Deployment topology
**Status:** Fixed

**Observed:**
```
Post "http://host.docker.internal:55393/proxy/44772/command": dial tcp: lookup host.docker.internal: no such host
```

**Root cause:** When `OPEN_SANDBOX_USE_SERVER_PROXY` is not set, `GetEndpoint` returns the
direct container endpoint (e.g. `host.docker.internal:PORT`). This works inside Docker
containers on the bridge network but not from the macOS host, where `host.docker.internal`
may not resolve.

The proxy endpoint (`localhost:8090/sandboxes/{id}/proxy/44772`) always works from the host.

**Fix applied:** `stream` now defaults to `useServerProxy=true`. Can be overridden with
`OPEN_SANDBOX_USE_SERVER_PROXY=false`.

**Remaining risk:**
- Other CLI commands that interact with the sandbox (if added in future) will need the same
  default
- The proxy adds latency; for high-volume log streaming this may be noticeable
- If the OpenSandbox server is unreachable, the proxy mode fails with a less obvious error
  than a direct connection timeout

---

## Session 2 — 2026-03-10 (Iteration 3)

**Task:** 3-repo parallel security audit (p-limit, p-map, p-queue) — report mode, no approval.

**Result:** PARTIAL — 2 of 3 repos produced reports; p-queue failed.

---

### Finding 4: `fleetlift stream` outputs raw stream-json — unreadable

**Category:** CLI UX
**Status:** Fixed

**Observed:** `fleetlift stream` printed raw Claude stream-json events: `{"type":"assistant","message":...}` lines
instead of human-readable text.

**Root cause:** The `onLine` callback in `runStream` printed each log line verbatim. The agent log contains
`--output-format stream-json` output, which is one JSON event per line.

**Fix applied:** Added `formatStreamLine` parser in `cmd/cli/main.go`. It extracts:
- `text` content blocks from assistant messages → displayed as-is
- `tool_use` blocks → displayed as `[ToolName: primary_param]`
- `result` events → displayed as `✓ <final result>`
- All other event types (system/init, tool_result, etc.) are suppressed.

---

### Finding 5: `fleetlift reports` truncated body at 200 characters

**Category:** CLI UX
**Status:** Fixed

**Observed:** Report body was cut off mid-sentence with `...` after 200 characters.

**Root cause:** `displayReport` in `cmd/cli/main.go` explicitly limited body output to 200 runes.

**Fix applied:** Removed the 200-character truncation. Reports now display in full.

---

### Finding 6: `bufio.Scanner: token too long` when reading large result.json

**Category:** Reliability / File I/O
**Status:** Fixed

**Observed:** p-queue exec workflow completed with error:
```
read result: execd read /workspace/.fleetlift/result.json via command: reading command SSE: bufio.Scanner: token too long
```

**Root cause:** `readFileViaCommand` uses `cat | base64 -w0` to transfer file content, then the worker
reads the execd SSE response with a `bufio.Scanner`. The default scanner max token size is 64 KB.
When result.json is large (a detailed Claude report), the base64-encoded output of a single
`stdout` SSE event exceeds 64 KB, causing the scanner to fail.

**Fix applied:** In `RunCommand` (`execd.go`), called `scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)`
to increase the max token size to 16 MB (matching the 10 MB file size limit with base64 overhead).

**Remaining risk:**
- 16 MB is a conservative upper bound; the `maxFileSize` cap is 10 MB, so base64 adds ~33% → ~13.3 MB max.
  16 MB provides sufficient headroom.
- The `readFileViaDownload` path (which doesn't use a scanner) is not affected.

---

## Session 4 — 2026-03-10 (Iterations 6 & 7)

**Iteration 6 result:** FAILED — 0 of 3 (`Credit balance is too low` — API credits replenished between sessions)

**Iteration 7 result:** PASSED ✓ — 3/3 reports in 52 seconds

All prior fixes confirmed working end-to-end:
- Stream formatter shows human-readable output
- Reports show full body text
- Scanner buffer handles large result.json
- REPORT.md written to correct per-repo path
- `CLAUDE_CODE_OAUTH_TOKEN` supported as alternative to `ANTHROPIC_API_KEY`

---

## Session 3 — 2026-03-10 (Iterations 4 & 5)

**Task:** Same 3-repo audit as session 2.

**Iteration 4 result:** PARTIAL — 2 of 3 (p-queue failed with `exit code 1` after 371s; root cause: API credit exhaustion mid-run).

**Iteration 5 result:** FAILED — 0 of 3 (all repos immediately failed: `Credit balance is too low`).

---

### Finding 7: `exit code 1` from Claude obscures API-level errors (credit exhaustion)

**Category:** Observability / Error Reporting
**Status:** Unresolved

**Observed:** p-queue failed with `claude code failed: exit code 1`. Iteration 5 revealed the true cause:
`Credit balance is too low`. Claude exits with code 1 when API calls fail (e.g. credit exhausted, auth
error), but the exit code itself gives no indication of the root cause.

**Root cause:** `claude -p` exits with code 1 for both logic errors and API-level failures. The only
way to determine the actual cause is to parse the stream-json output for error messages, which is not
currently done.

**Suggested fix:** In `runAgenticTransformation`, parse the output for known API error strings
(e.g. "Credit balance is too low", "Invalid API key") and surface them in the returned error.

---

### Finding 8: `fleetlift stream` fails with HTTP 404 if sandbox was already torn down

**Category:** CLI UX / Timing
**Status:** Unresolved

**Observed:** When `stream` is started after the workflow completes (or just as it's completing), the
sandbox is already torn down and the endpoint returns HTTP 404:
```
Error: stream error: get execd endpoint for sandbox xxx: get endpoint xxx:44772: HTTP 404
```

**Root cause:** OpenSandbox deletes the container when the agent pipeline finishes. The `stream`
command has no way to know if the sandbox has already been torn down before attempting to connect.

**Suggested fix:** Distinguish HTTP 404 (sandbox gone) from other errors and print a more helpful
message: "Workflow already completed — sandbox has been torn down."

---

## Known Fragile Areas (Unresolved)

These were identified but not fixed during this session. Tracked here for future work.

### A: No fast-fail for missing `ANTHROPIC_API_KEY` in container

The agent pipeline has no preflight check. A missing key causes a ~14-minute wait followed by
an opaque `exit code -1` error. The worker startup warning exists but is easy to miss.

**Suggested fix:** In `runAgenticTransformation`, check `os.Getenv("ANTHROPIC_API_KEY") == ""`
and return an explicit error before invoking `claude`. Or add a preflight health check at
worker startup that fails hard when `REQUIRE_CONFIG=true`.

### F: `fleetlift stream` exits immediately if started before agent.log exists

**Status: Fixed**

`tail -f` exits with an error when the target file doesn't exist. If `stream` is run during the
clone/setup phase (before Claude Code has started writing `agent.log`), it prints:
```
tail: cannot open '/workspace/.fleetlift/agent.log' for reading: No such file or directory
tail: no files remaining
```
and exits, requiring the user to run it again.

**Fix:** Changed `tail -f` to `tail -F` (GNU coreutils). `-F` follows by filename and retries
if the file doesn't exist, outputting content once it appears. Also added filtering for
`tail:` status lines (e.g. "tail: '...' has appeared") so they don't appear in stream output.

---

### E: `fleetlift reports` fails on multi-repo report tasks — `Mode` not set

**Status: Fixed**

`transformGrouped` (the parent workflow for multi-repo tasks) assembled its final `TaskResult`
without setting the `Mode` field. `TransformV2` does set it, but for grouped tasks the parent
overwrites the result without copying the mode. The `reports` command then rejected it as
"not a report-mode task".

**Fix:** Added `Mode: task.GetMode()` to `transformGrouped`'s result in `transform.go`.

---

### B: `stream` includes completed exec workflows in group list

`ListExecWorkflows` queries all `TransformV2` workflows with the task prefix, including
completed ones. If a user re-runs the same task (same task ID, new timestamp), old completed
exec workflows appear in the list.

**Suggested fix:** Filter `ListExecWorkflows` to `Running` status only.

### C: No user-facing progress for multi-repo tasks during `fleetlift run`

The CLI's `run` command polls `get_execution_progress` for the parent workflow and shows a
progress bar, but there is no per-repo status (e.g. "express: cloning", "fastify: running
Claude"). Users have no visibility into which repos are stuck vs. progressing without
using `stream`.

**Suggested fix:** Poll `get_execution_progress` on each exec child workflow and surface
per-group status during the wait loop.

### D: Workflow result for multi-repo tasks doesn't surface per-repo errors clearly

`fleetlift result` shows the aggregate status. If 2 of 3 repos succeed and 1 fails, the top-
level result is `completed` but the per-group failure is buried. Users need to inspect Temporal
UI to find which repo failed and why.

**Suggested fix:** `fleetlift result` should print per-group outcomes when groups are present,
including error summaries for failed groups.
