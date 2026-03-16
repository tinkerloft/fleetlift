# Track E3: MCP Interactive Tools Design

**Date:** 2026-03-16
**Status:** Draft
**Track:** E3 — completes the MCP Agent Interface (E1+E2 shipped in PR #36)

---

## Problem

Agents can produce artifacts and knowledge mid-run (E2), but cannot interact with humans during execution. Today, human-in-the-loop only happens at step *boundaries* via the existing `approve`/`reject`/`steer` signals. An agent that needs a decision at minute 5 of a 30-minute run must either guess or abort.

Two gaps:

1. **Blocking input** — agent needs a human decision before it can continue
2. **Async notification** — agent wants to flag something without stopping

---

## Design

### Tool: `inbox.notify`

Fire-and-forget. Creates an inbox item and returns immediately. No container lifecycle change.

```json
// Input
{
  "title": "Large dependency tree detected",
  "summary": "Found 847 transitive deps. Analysis will take longer than usual.",
  "urgency": "low"   // "low" | "normal" | "high"
}
// Response
{ "inbox_item_id": "550e8400-e29b-41d4-a716-446655440000" }
```

### Tool: `inbox.request_input`

Pauses the step and waits for a human response. The container exits normally after the call. Execution resumes in a **continuation step** — a fresh sandbox with no inherited container state.

```json
// Input
{
  "question": "Found 3 flaky tests. Should I fix them or skip and proceed?",
  "options": ["Fix flaky tests", "Skip and proceed", "Abort"],  // optional
  "state_summary": "Completed auth module refactor. 47/50 tests passing.",  // optional
  "checkpoint_branch": "fleetlift/checkpoint/run-abc123-fix",  // optional
  "urgency": "normal"   // "low" | "normal" | "high"
}
// Response (to the sidecar immediately — not the human's answer)
{ "inbox_item_id": "550e8400-e29b-41d4-a716-446655440001", "status": "input_requested" }
```

The tool call is **non-blocking** from the sidecar's perspective. The sidecar posts the request, receives the immediate confirmation, and returns the confirmation to the agent. The agent should then exit cleanly — the step is done from the agent's perspective. The human's answer is never returned to the calling agent; it is injected into the continuation step's context.

---

## Continuation Step

A continuation step is a dynamically created `step_run` record, visible in the run timeline as a child of the original step. It is **not** declared in the workflow YAML — it is created at runtime when a step enters `awaiting_input`.

### Continuation step ID

The continuation step's `step_id` is derived deterministically from the original:

```
{original_step_id}-resume-{n}
```

where `n` starts at 1 and increments if a continuation itself calls `inbox.request_input`. The Temporal child workflow ID follows the existing pattern: `{runID}-{continuation_step_id}`.

### What the continuation step receives

The continuation agent's prompt is assembled from:

1. **Original step prompt** (re-rendered from the workflow template)
2. **State summary** (from the checkpoint artifact, if `state_summary` was provided)
3. **Human answer** (injected as a labelled context block: `[Human input requested by previous step: "{question}" — Answer: "{answer}"]`)
4. **Checkpoint branch** (if present — continuation clones repo and checks out this branch instead of the default)

These are passed as new fields on `ExecuteStepInput`:

```go
type ExecuteStepInput struct {
    // ... existing fields ...
    ContinuationContext *ContinuationContext
}

type ContinuationContext struct {
    InboxItemID         string
    Question            string
    HumanAnswer         string
    CheckpointBranch    string  // empty if not set
    StateArtifactID     string  // empty if no state_summary was provided
}

// ExecuteStepResult — returned by ExecuteStep activity
type ExecuteStepResult struct {
    Status           string  // "complete" | "failed" | "awaiting_input"
    InboxItemID      string  // set when Status == "awaiting_input"
    Question         string  // the agent's question
    CheckpointBranch string  // set if agent provided checkpoint_branch
    StateArtifactID  string  // set if agent provided state_summary
}
```

### What the continuation step does NOT inherit

- The working directory (re-cloned from scratch, or checked out from checkpoint branch)
- Any in-memory agent state
- Environment variables beyond what any new step normally receives

### Git checkpoint lifecycle

If `checkpoint_branch` is provided:

1. **Before calling `inbox.request_input`**: agent commits and pushes working changes to `fleetlift/checkpoint/{run_id}-{step_id}` using credentials already available in the sandbox
2. **Continuation step provisioning**: `ProvisionSandbox` clones the repo and checks out the checkpoint branch
3. **After continuation step completes** (success or failure): `CleanupCheckpointBranch` activity deletes the branch from the remote

Checkpoint branch names are validated server-side against the pattern `^fleetlift/checkpoint/[a-zA-Z0-9_-]+$`. Any `checkpoint_branch` value that does not match this pattern is rejected with a 400 error and the tool call returns an error to the agent. This prevents shell injection via agent-supplied branch names.

---

## Data Model

### `inbox_items` table — new columns

| Column | Type | Notes |
|--------|------|-------|
| `question` | `TEXT` | The agent's question (`request_input` items only) |
| `options` | `TEXT[]` (`pq.StringArray`) | Optional list of choices |
| `answer` | `TEXT` | Human's free-text response (null until answered) |
| `answered_at` | `TIMESTAMPTZ` | When the human responded |
| `answered_by` | `TEXT` | Email/user ID of responder |
| `urgency` | `TEXT` | `"low"` / `"normal"` / `"high"` (default `"normal"`) |

The existing `kind` column is repurposed: new values `"notify"` and `"request_input"` added alongside the existing `"awaiting_input"` and `"output_ready"`. The existing `step_run_id UUID` column is unchanged.

> **Migration note:** Add a `CHECK` constraint to `kind` that includes the new values, or use an unconstrained TEXT column as today.

### `step_runs` table — new columns

| Column | Type | Notes |
|--------|------|-------|
| `parent_step_run_id` | `UUID` | FK → `step_runs.id` (null for non-continuation steps) |
| `checkpoint_branch` | `TEXT` | Git branch to check out on provision (null if none) |
| `checkpoint_artifact_id` | `UUID` | FK → `artifacts.id` (null if no `state_summary`) |

---

## API

### New MCP endpoints (sidecar → backend)

```
POST /api/mcp/inbox/notify
POST /api/mcp/inbox/request_input
```

Both require the existing `MCPAuth` middleware (MCP JWT, run liveness check). Both return immediately with a JSON response — neither blocks.

`/api/mcp/inbox/request_input` handler:
1. Validates `checkpoint_branch` pattern if provided
2. Creates an `inbox_item` with `kind = "request_input"`
3. Creates a checkpoint artifact if `state_summary` is non-empty
4. Updates the `step_run` to `awaiting_input` status in the DB
5. Returns `{ "inbox_item_id": "...", "status": "input_requested" }`

The Temporal signal is NOT sent from this HTTP handler. The `ExecuteStep` activity polls or detects the `awaiting_input` DB state as its return condition (see Temporal section below).

### New inbox response endpoint

```
POST /api/inbox/{id}/respond
Body: { "answer": "Fix flaky tests" }
```

- Auth: standard user JWT (not MCP JWT)
- Validates: `kind = "request_input"`, `answered_at IS NULL`, user belongs to the item's team
- Stores answer + `answered_at` + `answered_by`
- Sends `respond` signal to the waiting `StepWorkflow` via the Temporal client, carrying `InboxAnswer{Answer: "...", Responder: "..."}`

---

## Temporal / Workflow Changes

### `ExecuteStep` activity — return value extension

The `ExecuteStep` activity returns a new status value `"awaiting_input"` when the agent calls `inbox.request_input`. The activity does **not** signal the workflow from inside itself — Temporal activities cannot do this. Instead:

1. The MCP backend handler writes `awaiting_input` status to the `step_run` row and creates the inbox item
2. `ExecuteStep` detects this condition (polling the DB or via a return signal from the MCP sidecar process exiting) and returns `ExecuteStepResult{Status: "awaiting_input", InboxItemID: "..."}`
3. `StepWorkflow` inspects the result and enters the await/resume cycle

### `StepWorkflow` — await/resume cycle

```go
// New respond signal — distinct from approve/reject/steer which are end-of-step signals
workflow.SetSignalHandler(ctx, "respond", func(answer InboxAnswer) {
    respondCh <- answer
})

result := executeStepResult  // returned by ExecuteStep activity

if result.Status == "awaiting_input" {
    // Transition already written by MCP handler — just wait
    answer := <-respondCh

    // Create continuation step_run record via CreateStepRun activity
    continuationStepID := fmt.Sprintf("%s-resume-1", step.ID)
    // Pass: runID, continuationStepID, title="{original title} (resumed)",
    //       parent_step_run_id=original step_run ID,
    //       checkpoint_branch=result.CheckpointBranch,
    //       checkpoint_artifact_id=result.StateArtifactID

    // Re-execute with continuation context
    continuationResult, err = workflow.ExecuteActivity(ctx, activities.ExecuteStep,
        ExecuteStepInput{
            // ... original fields ...
            ContinuationContext: &ContinuationContext{
                InboxItemID:      result.InboxItemID,
                Question:         result.Question,
                HumanAnswer:      answer.Answer,
                CheckpointBranch: result.CheckpointBranch,
                StateArtifactID:  result.StateArtifactID,
            },
        }).Get(ctx, &continuationResult)
}
```

The `respond` signal is separate from `approve`/`reject`/`steer`. These are mutually exclusive per execution: a step either has a HITL pause mid-execution (`respond`) or an approval gate post-execution (`approve`/`reject`/`steer`), never both simultaneously. If a step has `approval_policy: always`, the approval gate runs after the continuation step completes, not before it.

### `DAGWorkflow` — no changes required

The DAG treats the original step as "pending" for as long as `StepWorkflow` is running. Since `StepWorkflow` now handles the entire await/resume cycle internally — returning only after the continuation completes — the DAG sees a single result. Downstream steps reference that result as normal. No DAG changes are needed.

### Sandbox cleanup ordering

When `ExecuteStep` returns `awaiting_input`, the existing `StepWorkflow` sandbox cleanup block (already present in `step.go`) fires immediately — this is correct. The original sandbox is gone before the continuation runs; the continuation's `ProvisionSandbox` provisions a fresh one. No changes to the cleanup ordering are needed.

### New activity: `CleanupCheckpointBranch`

Location: `internal/activity/provision.go` (alongside existing sandbox lifecycle activities).

```go
func (a *Activities) CleanupCheckpointBranch(ctx context.Context, input CleanupCheckpointInput) error
```

`CleanupCheckpointInput`:
- `RepoURL string` — remote repo URL
- `Branch string` — `fleetlift/checkpoint/...`
- `CredentialName string` — credential name for the GitHub token (from the step's credential config)
- `TeamID string` — for credential store lookup

Behavior: runs `git push origin --delete {branch}` using the credential. Returns nil if branch does not exist (idempotent).

- `RetryPolicy`: `MaximumAttempts: 3`, `InitialInterval: 2s`
- Must be registered in `cmd/worker/main.go`
- Activity name constant added to `internal/activity/constants.go`

---

## MCP Sidecar Changes

### New tool registrations in `cmd/mcp-sidecar/main.go`

```go
// inbox.notify
server.AddTool(mcp.NewTool("inbox.notify",
    mcp.WithDescription("Send a notification to the team inbox without blocking execution"),
    mcp.WithString("title", mcp.Required()),
    mcp.WithString("summary", mcp.Required()),
    mcp.WithString("urgency"),  // default: "normal"
), handleInboxNotify)

// inbox.request_input
server.AddTool(mcp.NewTool("inbox.request_input",
    mcp.WithDescription("Request human input. This ends the current step. A continuation step will run with the human's answer once they respond. Call this as your final action when you need human guidance before proceeding."),
    mcp.WithString("question", mcp.Required()),
    mcp.WithString("state_summary"),     // optional
    mcp.WithArray("options"),            // optional
    mcp.WithString("checkpoint_branch"), // optional — git branch with committed working state
    mcp.WithString("urgency"),           // default: "normal"
), handleInboxRequestInput)
```

`handleInboxRequestInput`: POST to `/api/mcp/inbox/request_input`, return the immediate confirmation. The tool description explicitly instructs the agent to treat this as a terminal call.

---

## Frontend Changes

### Inbox — `request_input` items

`request_input` inbox items display:
- The agent's question
- Option buttons (if `options` provided) OR a free-text input field
- A "Submit" button that POSTs to `/api/inbox/{id}/respond`
- Step context: run name, step name, urgency badge
- Checkpoint branch name (if any) as an informational note

### Run detail — continuation steps

Continuation steps appear in the step timeline indented under the original step:
- Original step shows status `awaiting_input` with a "Waiting for input" label and timestamp
- Continuation step appears below with a "Resumed" badge and its own status/duration/output
- Downstream steps' "depends on" arrows point to the continuation step's output

---

## Backward Compatibility

- All E1/E2 tools unchanged
- Existing inbox items and `approve`/`reject`/`steer` flow unaffected
- Steps that never call `inbox.request_input` behave identically to today
- `inbox.notify` items render with a default display if the frontend doesn't recognise the new `kind` value

---

## Out of Scope

- **Timeout for `inbox.request_input`**: no timeout in v1 — steps wait indefinitely. Note: indefinitely-waiting `StepWorkflow` instances accumulate Temporal history events. A `WorkflowExecutionTimeout` on `StepWorkflow` is the right mechanism for v2; leave a TODO comment in `step.go`.
- **Multiple HITL rounds**: a continuation step can itself call `inbox.request_input`, creating another continuation (`-resume-2`, etc.) — this falls out of the design naturally but is not explicitly tested in v1.
- **Fan-out steps calling `inbox.request_input`**: deferred — parallel branch + HITL interaction needs separate design.
- **Kubernetes pause/resume**: not applicable — this design requires no container pause capability.
