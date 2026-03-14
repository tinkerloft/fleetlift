# Reliability Audit Plan

Audit the codebase for the four recurring failure patterns identified during the ui_update debugging session. Each pattern has its own section with specific grep targets, files to inspect, and the fix to apply.

---

## Pattern 1: Failure propagation gaps

**The bug:** A function completes subordinate work that fails, but returns success anyway. Most dangerous in workflow orchestration — a step fails but the run is marked complete.

**Search targets:**

```bash
# Activities that return nil without checking for subordinate errors
rg "return nil$" internal/workflow/ internal/activity/ --include="*.go" -n

# Goroutines in workflow Go() that set results[i] but may leave it nil on early return
rg "results\[" internal/workflow/dag.go -n

# StepOutput Status set to Complete without checking subordinate outputs
rg "StepStatusComplete" internal/ --include="*.go" -n

# Any place we call .Get() on a child workflow or activity and ignore the error
rg "\.Get\(.*nil\)" internal/workflow/ --include="*.go" -n
```

**Files to inspect:**
- `internal/workflow/dag.go` — parallel step goroutines, fan-out aggregation, the main loop exit condition
- `internal/workflow/step.go` — verifier result handling, steer loop, finalize paths
- `internal/activity/execute.go` — agent completion handling, `gotComplete` check, `is_error` check

**What to look for:**
- Any goroutine that can `return` without setting `results[i]`
- Any aggregation function that returns Complete when some sub-results are nil
- Any `.Get()` call whose error is discarded with `_`
- Any `if err != nil { logger.Error(...) }` in a workflow (should be `return err`)

**Fix pattern:** Return the error up. If a goroutine can't return an error directly, set the result to a failed StepOutput. Never log-and-continue in a workflow — log and return.

---

## Pattern 2: Unbounded retries on permanent errors

**The bug:** `workflow.ActivityOptions` with no `RetryPolicy`, or one with `MaximumAttempts: 0` (unlimited). A permanent failure (SQL type error, wrong field name, missing schema) retries forever.

**Search targets:**

```bash
# All ActivityOptions declarations — check each for RetryPolicy
rg "ActivityOptions{" internal/workflow/ --include="*.go" -n -A 5

# Options with explicit timeout but no retry policy
rg "StartToCloseTimeout.*}" internal/workflow/ --include="*.go" -n

# Activities using default retry (no options override at all)
rg "ExecuteActivity\b" internal/workflow/ --include="*.go" -n
```

**Files to inspect:**
- `internal/workflow/dag.go` — sandbox provision, all inline `workflow.ActivityOptions{}`
- `internal/workflow/step.go` — verify, PR create, cleanup, finalize activities
- Any new activity added since the `dbRetry` variable was introduced

**What to look for:**
- `workflow.ActivityOptions{StartToCloseTimeout: ...}` with no `RetryPolicy` field — these use Temporal's default (unlimited)
- Long-running activities (sandbox provision, PR create) that should fail fast after 2–3 attempts
- DB activities not using the shared `dbRetry` variable

**Fix pattern:**
- Short DB/infra activities → `RetryPolicy: dbRetry` (MaximumAttempts: 5)
- Execution activities → `RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 2}`
- Sandbox provision → `MaximumAttempts: 3` (transient infra errors are expected)
- Cleanup activities → can stay unlimited (idempotent, best-effort)

---

## Pattern 3: Integration contract mismatches

**The bug:** Code sends a field name or value that an external API silently ignores. Or treats HTTP 200 as proof of success when the actual effect didn't happen.

**Search targets:**

```bash
# All places we build JSON bodies for external API calls
rg "map\[string\]any{" internal/sandbox/ --include="*.go" -n -A 10

# All ExecStream / Exec calls — verify the workdir/cwd field
rg "ExecStream\|Exec\b" internal/ --include="*.go" -n

# Places that don't verify the result of a command (ignoring stdout/stderr)
rg "sb\.Exec\(.*_,.*_,\|sb\.Exec\(.*_, _" internal/ --include="*.go" -n

# Git operations that don't check stderr for "fatal:" or "error:"
rg "git " internal/activity/ --include="*.go" -n

# HTTP responses where we don't check status code before using body
rg "resp\.Body\|res\.Body" internal/ --include="*.go" -n -B 2
```

**Files to inspect:**
- `internal/sandbox/opensandbox/client.go` — all JSON request bodies sent to OpenSandbox; compare field names against `opensandbox/specs/execd-api.yaml`
- `internal/activity/execute.go` — git clone/fetch/checkout/diff; every `sb.Exec` call
- `internal/activity/pr.go` (if exists) — GitHub API calls
- `internal/server/handlers/` — any outbound HTTP calls

**What to look for:**
- JSON field names that don't match the target API's schema (consult OpenSandbox specs in `../opensandbox/specs/`)
- `sb.Exec` results where stdout/stderr are ignored (`_`) for operations that can fail
- Missing post-condition checks (e.g. verify file exists after write, verify branch exists after push)
- Any place we parse only HTTP status without checking the response body for error fields

**Fix pattern:** After every external call with a side effect, verify the effect happened. For git operations, check both `gitFailed(stderr)` and a post-condition. For OpenSandbox API calls, cross-reference field names against the spec.

---

## Pattern 4: Streaming buffer size assumptions

**The bug:** `bufio.NewScanner` default buffer is 64 KB. Any single JSON line from Claude Code larger than that (large file reads, big diffs) silently errors, failing the activity.

**Search targets:**

```bash
# Every bufio.Scanner instantiation
rg "bufio.NewScanner\|bufio.NewReader" internal/ --include="*.go" -n

# Every scanner.Scan() loop — check for explicit buffer override
rg "scanner\.Scan\(\)" internal/ --include="*.go" -n -B 5

# Any io.ReadAll on a potentially large stream
rg "io\.ReadAll" internal/ --include="*.go" -n

# SSE / log streaming readers
rg "NewReader\|Scanner" internal/server/ --include="*.go" -n
```

**Files to inspect:**
- `internal/sandbox/opensandbox/client.go` — the `ExecStream` scanner (already fixed; verify buffer is `4*1024*1024`)
- `internal/server/handlers/` — SSE streaming, log streaming
- Any future agent runner that wraps streaming output

**What to look for:**
- `bufio.NewScanner(r)` not followed immediately by `scanner.Buffer(...)`
- `bufio.NewReader` used with `ReadString` or `ReadLine` on unbounded input
- Any stream reader processing agent output without an explicit max line size

**Fix pattern:** Immediately after `bufio.NewScanner(r)`, call:
```go
scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024)
```
For `bufio.NewReader`, use `ReadBytes` with explicit size checking or switch to Scanner with the buffer override.

---

## Execution order

Run in this order — Pattern 1 and 2 are highest risk for correctness, 3 and 4 are hardest to find without targeted search.

1. Pattern 2 (unbounded retries) — quick wins, grep-driven, low risk to fix
2. Pattern 1 (failure propagation) — requires reading logic carefully, highest impact
3. Pattern 3 (integration contracts) — requires cross-referencing external API specs
4. Pattern 4 (buffer sizes) — quick fix once found, lowest blast radius

For each finding: add a test that would have caught the bug before fixing it, then fix it.
