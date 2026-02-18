# Phase 9.2: Human-in-the-Loop Iterative Steering

Implementation plan for iterative human-agent collaboration in fleetlift.

> **Goal**: Enable humans to view changes and guide Claude Code through multiple refinement iterations before final approval, rather than binary approve/reject.

---

## Overview

| Feature | Description |
|---------|-------------|
| **View Changes** | See diffs and verifier output before deciding |
| **Steering** | Send follow-up prompts to refine agent work |
| **Iteration Tracking** | Track steering history and iteration count |
| **Web-Ready APIs** | Design for future web UI consumption |

**Not in scope**: Partial/granular file-level approval (explicitly excluded per requirements).

---

## 1. Data Model Changes

### File: `internal/model/task.go`

Add new types:

```go
// SteeringIteration represents one human-guided refinement cycle.
type SteeringIteration struct {
    IterationNumber int       `json:"iteration_number"`
    Prompt          string    `json:"prompt"`
    Timestamp       time.Time `json:"timestamp"`
    FilesModified   []string  `json:"files_modified,omitempty"`
    Output          string    `json:"output,omitempty"`
}

// SteeringState tracks iterative steering history.
type SteeringState struct {
    CurrentIteration int                 `json:"current_iteration"`
    MaxIterations    int                 `json:"max_iterations"` // Default: 5
    History          []SteeringIteration `json:"history,omitempty"`
}

// DiffOutput represents git diff results.
type DiffOutput struct {
    Repository string     `json:"repository"`
    Files      []FileDiff `json:"files"`
    Summary    string     `json:"summary"`
    TotalLines int        `json:"total_lines"`
    Truncated  bool       `json:"truncated"`
}

// FileDiff represents a single file's diff.
type FileDiff struct {
    Path      string `json:"path"`
    Status    string `json:"status"` // modified, added, deleted
    Additions int    `json:"additions"`
    Deletions int    `json:"deletions"`
    Diff      string `json:"diff"`
}

// VerifierOutput represents detailed verifier execution results.
type VerifierOutput struct {
    Verifier string `json:"verifier"`
    ExitCode int    `json:"exit_code"`
    Stdout   string `json:"stdout"`
    Stderr   string `json:"stderr"`
    Success  bool   `json:"success"`
}

// SteeringSignalPayload is sent with the steer signal.
type SteeringSignalPayload struct {
    Prompt string `json:"prompt"`
}
```

Add to `Task` struct:

```go
MaxSteeringIterations int `json:"max_steering_iterations,omitempty" yaml:"max_steering_iterations,omitempty"`
```

---

## 2. New Activities

### File: `internal/activity/steering.go` (new file)

```go
type SteeringActivities struct {
    Provider sandbox.Provider
}

// GetDiff returns git diffs for modified files.
func (a *SteeringActivities) GetDiff(ctx context.Context, input GetDiffInput) ([]model.DiffOutput, error)

// GetVerifierOutput runs verifiers and returns detailed output.
func (a *SteeringActivities) GetVerifierOutput(ctx context.Context, input GetVerifierOutputInput) ([]model.VerifierOutput, error)
```

### Input structs:

```go
type GetDiffInput struct {
    ContainerID             string
    Repos                   []model.Repository
    UseTransformationLayout bool
    MaxLines                int // 0 = default 1000
}

type GetVerifierOutputInput struct {
    ContainerID             string
    Repos                   []model.Repository
    Verifiers               []model.Verifier
    UseTransformationLayout bool
}
```

### File: `internal/activity/constants.go`

Add:
```go
ActivityGetDiff           = "GetDiff"
ActivityGetVerifierOutput = "GetVerifierOutput"
```

---

## 3. Workflow Changes

### File: `internal/workflow/transform.go`

#### 3.1 New Signal and Query Constants

```go
const (
    // Existing
    SignalApprove = "approve"
    SignalReject  = "reject"
    SignalCancel  = "cancel"

    // New
    SignalSteer   = "steer"

    // Existing queries
    QueryStatus = "get_status"
    QueryResult = "get_claude_result"

    // New queries
    QueryDiff          = "get_diff"
    QueryVerifierLogs  = "get_verifier_logs"
    QuerySteeringState = "get_steering_state"
)
```

#### 3.2 New Workflow State Variables

```go
var (
    // ... existing ...

    // Steering state
    steeringState        = model.SteeringState{MaxIterations: 5}
    steerRequested       bool
    steeringPrompt       string
    cachedDiffs          []model.DiffOutput
    cachedVerifierOutput []model.VerifierOutput
)
```

#### 3.3 Signal Channel Setup

Add `steerChannel`:
```go
steerChannel := workflow.GetSignalChannel(ctx, SignalSteer)
```

#### 3.4 Signal Handler Update

Add steer signal handling with payload:
```go
selector.AddReceive(steerChannel, func(c workflow.ReceiveChannel, more bool) {
    var payload model.SteeringSignalPayload
    c.Receive(ctx, &payload)
    logger.Info("Received steering signal", "prompt", payload.Prompt)
    steerRequested = true
    steeringPrompt = payload.Prompt
})
```

#### 3.5 Query Handlers

Add after existing query handlers:
```go
workflow.SetQueryHandler(ctx, QueryDiff, func() ([]model.DiffOutput, error) {
    return cachedDiffs, nil
})

workflow.SetQueryHandler(ctx, QueryVerifierLogs, func() ([]model.VerifierOutput, error) {
    return cachedVerifierOutput, nil
})

workflow.SetQueryHandler(ctx, QuerySteeringState, func() (*model.SteeringState, error) {
    return &steeringState, nil
})
```

#### 3.6 Steering Loop

Replace the approval section (~lines 338-381) with a steering loop:

```
APPROVAL_LOOP:
1. Cache diff and verifier output (call GetDiff, GetVerifierOutput activities)
2. Notify Slack with diff summary
3. Wait for signal: approve | reject | cancel | steer
4. On timeout (24h) -> return cancelled
5. On cancel -> return cancelled
6. On reject -> return cancelled with "rejected"
7. On approve -> break loop, continue to PR creation
8. On steer:
   a. Check iteration limit (default 5)
   b. Increment iteration count
   c. Build steering prompt with context
   d. Run Claude Code with steering prompt
   e. Record iteration in history
   f. Re-cache diff
   g. Notify Slack with updated changes
   h. Go to step 3
```

Key behaviors:
- Sandbox stays alive during iterations (existing defer handles cleanup)
- 24-hour timeout resets on each interaction
- Max iterations configurable via `task.MaxSteeringIterations`

---

## 4. Client Methods

### File: `internal/client/starter.go`

Add methods:

```go
// SteerWorkflow sends a steering signal with prompt payload.
func (c *Client) SteerWorkflow(ctx context.Context, workflowID, prompt string) error

// GetWorkflowDiff queries workflow for current diff state.
func (c *Client) GetWorkflowDiff(ctx context.Context, workflowID string) ([]model.DiffOutput, error)

// GetWorkflowVerifierLogs queries workflow for verifier output.
func (c *Client) GetWorkflowVerifierLogs(ctx context.Context, workflowID string) ([]model.VerifierOutput, error)

// GetSteeringState queries workflow for steering iteration history.
func (c *Client) GetSteeringState(ctx context.Context, workflowID string) (*model.SteeringState, error)
```

---

## 5. CLI Commands

### File: `cmd/cli/main.go`

#### 5.1 `fleetlift diff`

View changes made by workflow.

```
Usage: fleetlift diff [--workflow-id <id>] [--output json|table] [--full] [--file <path>]

Flags:
  --workflow-id   Workflow ID (defaults to last run)
  --output, -o    Output format: table (default), json
  --full          Show full diff content (default: summary only)
  --file          Filter to specific file path
```

#### 5.2 `fleetlift logs`

View verifier output.

```
Usage: fleetlift logs [--workflow-id <id>] [--output json|table] [--verifier <name>]

Flags:
  --workflow-id   Workflow ID (defaults to last run)
  --output, -o    Output format: table (default), json
  --verifier      Filter to specific verifier name
```

#### 5.3 `fleetlift steer`

Send steering prompt to workflow.

```
Usage: fleetlift steer --prompt <prompt> [--workflow-id <id>]

Flags:
  --workflow-id   Workflow ID (defaults to last run)
  --prompt, -p    Steering prompt (required)
```

---

## 6. Slack Integration

Update approval notification to include diff summary:

```
Claude completed {task.ID}.

Changes:
```
{changes summary}
```

Diff summary:
**repo-name**: 3 files changed, +45, -12
  - src/main.go (modified, +30/-5)
  - src/util.go (added, +15/-0)
  - README.md (modified, +0/-7)

Reply: `approve`, `reject`, or `steer "<prompt>"`
```

After steering iteration:
```
Steering iteration 2 complete for {task.ID}.

Updated changes:
{diff summary}

Reply: `approve`, `reject`, or `steer "<prompt>"`
```

**Note**: Full Slack bot integration (parsing replies) is out of scope for this phase. Users invoke CLI commands or a webhook calls the fleetlift CLI.

---

## 7. Worker Registration

### File: `cmd/worker/main.go`

```go
steeringActivities := activity.NewSteeringActivities(dockerProvider)

w.RegisterActivityWithOptions(steeringActivities.GetDiff,
    temporalactivity.RegisterOptions{Name: activity.ActivityGetDiff})
w.RegisterActivityWithOptions(steeringActivities.GetVerifierOutput,
    temporalactivity.RegisterOptions{Name: activity.ActivityGetVerifierOutput})
```

---

## 8. Web UI Considerations (Future Phase)

The design above is web-ready:

| Aspect | How It's Addressed |
|--------|-------------------|
| **Structured APIs** | All queries return JSON-serializable types |
| **Pagination** | `DiffOutput.Truncated`, `GetDiffInput.MaxLines` |
| **Real-time updates** | Poll `QueryStatus` + `QuerySteeringState` |
| **Signal with payload** | `SignalSteer` accepts `SteeringSignalPayload` |

### Future Web UI Phase Would Add:

1. **HTTP API Layer** - REST/GraphQL wrapping Temporal client
2. **WebSocket/SSE** - Real-time status updates without polling
3. **Diff Viewer** - Side-by-side diff rendering
4. **Conversation View** - Show steering iteration history
5. **Authentication** - OAuth/SSO integration

---

## 9. Implementation Order

### Phase A: Foundation (Days 1-2) — COMPLETE
- [x] Add data models to `internal/model/task.go`
- [x] Add activity constants to `constants.go`
- [x] Create `internal/activity/steering.go`
- [x] Register activities in worker
- [x] Run `make lint` and `go test ./...`

### Phase B: Workflow Signals & Queries (Days 3-4) — COMPLETE
- [x] Add signal/query constants
- [x] Add workflow state variables
- [x] Add steer signal channel and handler
- [x] Add query handlers (diff, logs, steering state)
- [x] Unit tests for signal handling

### Phase C: Steering Loop (Days 5-7) — COMPLETE
- [x] Implement `buildSteeringPrompt()` helper
- [x] Implement `cacheDiffAndVerifiers()` helper
- [x] Replace approval section with steering loop
- [x] Test iteration limits, timeout behavior
- [x] Integration tests

### Phase D: CLI & Client (Days 8-9) — COMPLETE
- [x] Add client methods to `starter.go`
- [x] Add `diff`, `logs`, `steer` CLI commands
- [x] Test CLI against running workflow

### Phase E: Polish & Docs (Day 10) — COMPLETE
- [x] End-to-end manual testing
- [x] Update `CLI_REFERENCE.md`
- [x] Update `IMPLEMENTATION_PLAN.md`

---

## 10. Files to Modify

| File | Changes |
|------|---------|
| `internal/model/task.go` | Add SteeringState, DiffOutput, VerifierOutput, etc. |
| `internal/activity/constants.go` | Add ActivityGetDiff, ActivityGetVerifierOutput |
| `internal/activity/steering.go` | **New file** - GetDiff, GetVerifierOutput activities |
| `internal/workflow/transform.go` | Add signal, queries, steering loop |
| `internal/client/starter.go` | Add Steer, GetDiff, GetVerifierLogs, GetSteeringState |
| `cmd/cli/main.go` | Add diff, logs, steer commands |
| `cmd/worker/main.go` | Register steering activities |
| `docs/CLI_REFERENCE.md` | Document new commands |
| `docs/plans/IMPLEMENTATION_PLAN.md` | Mark Phase 9.2 complete |

---

## 11. Verification

### Automated Tests
```bash
go test ./internal/activity/... -run Steering
go test ./internal/workflow/... -run Steer
make lint
```

### Manual Testing Checklist
- [ ] `fleetlift run -f task.yaml` with `require_approval: true`
- [ ] `fleetlift status` shows `awaiting_approval`
- [ ] `fleetlift diff` shows changes
- [ ] `fleetlift logs` shows verifier output
- [ ] `fleetlift steer -p "add error handling"` sends signal
- [ ] `fleetlift status` shows `running` then `awaiting_approval`
- [ ] `fleetlift diff` shows updated changes
- [ ] Repeat steer until max iterations, verify limit enforced
- [ ] `fleetlift approve` completes workflow
- [ ] Verify PR contains all iteration changes

---

## 12. Example Usage

```bash
# Start a task requiring approval
fleetlift run \
  --repo https://github.com/org/service.git \
  --prompt "Add input validation to the API handlers" \
  --verifier "test:go test ./..." \
  --require-approval

# Check status
fleetlift status
# Status: awaiting_approval

# View what changed
fleetlift diff
# Repository: service
#   Summary: 3 files changed, +45, -12
#   modified src/handlers/users.go (+30/-5)
#   modified src/handlers/orders.go (+15/-7)

# View test output
fleetlift logs
# [PASS] service:test (exit code: 0)
#   stdout: ok  github.com/org/service/handlers  0.5s

# Request changes
fleetlift steer -p "Also add validation to the payments handler"
# Steering signal sent. Use 'fleetlift status' to monitor.

# After Claude finishes...
fleetlift diff
# Now shows 4 files including payments.go

# Approve final changes
fleetlift approve
# PR created: https://github.com/org/service/pull/42
```
