# Track E3: MCP Interactive Tools Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `inbox.notify` and `inbox.request_input` MCP tools that let agents send notifications and request human input mid-execution, with the latter suspending the step and resuming in a fresh continuation step.

**Architecture:** The MCP sidecar exposes two new tools; the backend creates inbox items and sets step state; `StepWorkflow` waits for a `respond` Temporal signal, then creates a continuation `step_run` and re-executes in a fresh sandbox. The human responds via a new `/api/inbox/{id}/respond` endpoint.

**Tech Stack:** Go 1.22, Temporal SDK, chi router, sqlx/PostgreSQL, mark3labs/mcp-go, React 19 + TypeScript

**Spec:** `docs/superpowers/specs/2026-03-16-track-e3-interactive-tools-design.md`

---

## Chunk 1: Data + Model Layer

### Task 1: DB Schema Migration

**Files:**
- Modify: `internal/db/schema.sql`

- [ ] **Step 1: Add new columns to `inbox_items`**

Append to the **bottom** of `internal/db/schema.sql` (follow the existing `ALTER TABLE` pattern — do NOT modify the `CREATE TABLE` blocks, which would break idempotency for existing databases):

```sql
-- Added 2026-03-16 (E3): inbox interactive tools
ALTER TABLE inbox_items ADD COLUMN IF NOT EXISTS question     TEXT;
ALTER TABLE inbox_items ADD COLUMN IF NOT EXISTS options      TEXT[];
ALTER TABLE inbox_items ADD COLUMN IF NOT EXISTS answer       TEXT;
ALTER TABLE inbox_items ADD COLUMN IF NOT EXISTS answered_at  TIMESTAMPTZ;
ALTER TABLE inbox_items ADD COLUMN IF NOT EXISTS answered_by  TEXT;
ALTER TABLE inbox_items ADD COLUMN IF NOT EXISTS urgency      TEXT NOT NULL DEFAULT 'normal';
```

Also update the `kind` CHECK constraint to include new values (wrapped in a DO block for idempotency — see existing pattern in schema.sql):

```sql
DO $$ BEGIN
  ALTER TABLE inbox_items DROP CONSTRAINT IF EXISTS inbox_items_kind_check;
  ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_kind_check
    CHECK (kind IN ('awaiting_input','output_ready','notify','request_input'));
EXCEPTION WHEN others THEN NULL;
END $$;
```

- [ ] **Step 2: Add new columns to `step_runs`**

Append to the bottom of `internal/db/schema.sql`:

```sql
-- Added 2026-03-16 (E3): continuation step support
ALTER TABLE step_runs ADD COLUMN IF NOT EXISTS parent_step_run_id     UUID REFERENCES step_runs(id);
ALTER TABLE step_runs ADD COLUMN IF NOT EXISTS checkpoint_branch      TEXT;
ALTER TABLE step_runs ADD COLUMN IF NOT EXISTS checkpoint_artifact_id UUID REFERENCES artifacts(id);
```

- [ ] **Step 3: Add index for continuation step lookup**

```sql
CREATE INDEX IF NOT EXISTS step_runs_parent
    ON step_runs(parent_step_run_id) WHERE parent_step_run_id IS NOT NULL;
```

- [ ] **Step 4: Verify schema compiles**

```bash
psql $DATABASE_URL -c "\i internal/db/schema.sql" 2>&1 | grep -E "ERROR|WARNING" || echo "OK"
```

Expected: no errors (or "already exists" if re-running against live DB — that's fine for the file check).

---

### Task 2: Go Model Type Updates

**Files:**
- Modify: `internal/model/inbox.go`
- Modify: `internal/model/step.go` (or wherever `StepOutput` is defined)
- Create: `internal/model/continuation.go`

- [ ] **Step 1: Extend `InboxItem` struct in `internal/model/inbox.go`**

Add new fields to `InboxItem`:

```go
type InboxItem struct {
    ID         string     `db:"id"`
    TeamID     string     `db:"team_id"`
    RunID      string     `db:"run_id"`
    StepRunID  *string    `db:"step_run_id"`
    Kind       string     `db:"kind"`
    Title      string     `db:"title"`
    Summary    *string    `db:"summary"`
    // E3 fields
    Question   *string    `db:"question"`
    Options    pq.StringArray `db:"options"`
    Answer     *string    `db:"answer"`
    AnsweredAt *time.Time `db:"answered_at"`
    AnsweredBy *string    `db:"answered_by"`
    Urgency    string     `db:"urgency"`
    CreatedAt  time.Time  `db:"created_at"`
}
```

Ensure `github.com/lib/pq` is imported for `pq.StringArray`.

- [ ] **Step 2: Extend `StepOutput` to carry awaiting_input metadata**

Find `StepOutput` in `internal/model/` and add fields used when `Status == "awaiting_input"`:

```go
// Add to existing StepOutput struct:
InboxItemID      string `json:"inbox_item_id,omitempty"`
Question         string `json:"question,omitempty"`
CheckpointBranch string `json:"checkpoint_branch,omitempty"`
StateArtifactID  string `json:"state_artifact_id,omitempty"`
```

- [ ] **Step 3: Create `internal/model/continuation.go`**

```go
package model

// ContinuationContext is passed to a continuation ExecuteStep call.
type ContinuationContext struct {
    InboxItemID      string
    Question         string
    HumanAnswer      string
    CheckpointBranch string // empty if not set
    StateArtifactID  string // empty if no state_summary
}

// InboxAnswer is delivered via the Temporal "respond" signal.
type InboxAnswer struct {
    Answer    string
    Responder string
}

// CleanupCheckpointInput is the input for CleanupCheckpointBranch activity.
type CleanupCheckpointInput struct {
    RepoURL        string
    Branch         string
    CredentialName string // GitHub token credential name
    TeamID         string
}

// CreateContinuationStepRunInput is the input for CreateContinuationStepRun activity.
type CreateContinuationStepRunInput struct {
    RunID               string
    StepID              string // e.g. "fix-resume-1"
    StepTitle           string // e.g. "Fix (resumed)"
    TemporalWorkflowID  string
    ParentStepRunID     string
    CheckpointBranch    string
    CheckpointArtifactID string
}
```

- [ ] **Step 4: Build to verify**

```bash
go build ./internal/model/...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/db/schema.sql internal/model/
git commit -m "feat(e3): data model — inbox_items/step_runs columns + continuation types"
```

---

## Chunk 2: Activity Layer

### Task 3: Activity Constants + CleanupCheckpointBranch

**Files:**
- Modify: `internal/activity/constants.go`
- Modify: `internal/activity/provision.go`
- Create: `internal/activity/provision_test.go` (add test for new activity)

- [ ] **Step 1: Add constants to `internal/activity/constants.go`**

```go
const (
    // ... existing constants ...
    ActivityCleanupCheckpointBranch  = "CleanupCheckpointBranch"
    ActivityCreateContinuationStepRun = "CreateContinuationStepRun"
)
```

- [ ] **Step 2: Write failing test for `CleanupCheckpointBranch`**

In `internal/activity/provision_test.go` (create if not exists), add:

```go
func TestCleanupCheckpointBranch_BranchNotExist(t *testing.T) {
    // Should return nil when branch doesn't exist (idempotent)
    acts := &Activities{DB: nil, Sandbox: nil, CredStore: nil}
    err := acts.CleanupCheckpointBranch(context.Background(), model.CleanupCheckpointInput{
        RepoURL:        "https://github.com/example/repo",
        Branch:         "fleetlift/checkpoint/run-abc-step-fix",
        CredentialName: "",
        TeamID:         "team-1",
    })
    // With no credential and no real git, we expect an error about missing credential
    // (not a panic). Adjust assertion when integration tests run against real git.
    require.Error(t, err)
    require.Contains(t, err.Error(), "credential")
}
```

- [ ] **Step 3: Run test to confirm it fails**

```bash
go test ./internal/activity/... -run TestCleanupCheckpointBranch -v
```

Expected: FAIL (method not defined yet).

- [ ] **Step 4: Implement `CleanupCheckpointBranch` in `internal/activity/provision.go`**

```go
// CleanupCheckpointBranch deletes a fleetlift checkpoint branch from the remote.
// Returns nil if the branch does not exist (idempotent).
func (a *Activities) CleanupCheckpointBranch(ctx context.Context, input model.CleanupCheckpointInput) error {
    if input.Branch == "" {
        return nil
    }
    // Fetch GitHub token credential
    if input.CredentialName == "" {
        return fmt.Errorf("credential name required to delete checkpoint branch")
    }
    creds, err := a.CredStore.GetBatch(ctx, input.TeamID, []string{input.CredentialName})
    if err != nil {
        return fmt.Errorf("fetch credential: %w", err)
    }
    token, ok := creds[input.CredentialName]
    if !ok {
        return fmt.Errorf("credential %q not found", input.CredentialName)
    }
    // Inject token into repo URL for push auth
    repoWithToken, err := injectGitToken(input.RepoURL, token)
    if err != nil {
        return fmt.Errorf("inject token: %w", err)
    }
    // Pass branch name as a discrete argv entry — no shellquote needed (not a shell string)
    cmd := exec.CommandContext(ctx, "git", "push", repoWithToken, "--delete", input.Branch)
    out, err := cmd.CombinedOutput()
    if err != nil {
        // Branch not found is not an error
        if strings.Contains(string(out), "remote ref does not exist") ||
            strings.Contains(string(out), "error: unable to delete") {
            return nil
        }
        return fmt.Errorf("git push --delete: %w: %s", err, out)
    }
    return nil
}

// injectGitToken returns https://token@host/path for authenticated push.
func injectGitToken(repoURL, token string) (string, error) {
    u, err := url.Parse(repoURL)
    if err != nil {
        return "", err
    }
    u.User = url.UserPassword("x-access-token", token)
    return u.String(), nil
}
```

- [ ] **Step 5: Run test**

```bash
go test ./internal/activity/... -run TestCleanupCheckpointBranch -v
```

Expected: PASS (the test now hits the credential error path as designed).

- [ ] **Step 6: Build**

```bash
go build ./internal/activity/...
```

---

### Task 4: CreateContinuationStepRun Activity

**Files:**
- Modify: `internal/activity/status.go`
- Modify: `internal/activity/status_test.go`

- [ ] **Step 1: Write failing test**

In `internal/activity/status_test.go`, add:

```go
func TestCreateContinuationStepRun(t *testing.T) {
    db := setupTestDB(t) // use existing test DB helper
    acts := &Activities{DB: db}
    id, err := acts.CreateContinuationStepRun(context.Background(),
        model.CreateContinuationStepRunInput{
            RunID:              "run-1",
            StepID:             "fix-resume-1",
            StepTitle:          "Fix (resumed)",
            TemporalWorkflowID: "run-1-fix-resume-1",
            ParentStepRunID:    "parent-step-run-id",
            CheckpointBranch:   "fleetlift/checkpoint/run-1-fix",
        })
    require.NoError(t, err)
    require.NotEmpty(t, id)

    // Verify parent_step_run_id was persisted
    var parentID string
    err = db.QueryRowContext(context.Background(),
        "SELECT parent_step_run_id FROM step_runs WHERE id = $1", id).Scan(&parentID)
    require.NoError(t, err)
    require.Equal(t, "parent-step-run-id", parentID)
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/activity/... -run TestCreateContinuationStepRun -v
```

Expected: FAIL.

- [ ] **Step 3: Implement `CreateContinuationStepRun` in `internal/activity/status.go`**

```go
func (a *Activities) CreateContinuationStepRun(ctx context.Context, input model.CreateContinuationStepRunInput) (string, error) {
    id := uuid.New().String()
    nullStr := func(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
    _, err := a.DB.ExecContext(ctx, `
        INSERT INTO step_runs
            (id, run_id, step_id, step_title, status, temporal_workflow_id,
             parent_step_run_id, checkpoint_branch, checkpoint_artifact_id, created_at)
        VALUES ($1,$2,$3,$4,'pending',$5,$6,$7,$8,now())`,
        id, input.RunID, input.StepID, input.StepTitle, input.TemporalWorkflowID,
        input.ParentStepRunID,
        nullStr(input.CheckpointBranch),
        nullStr(input.CheckpointArtifactID),
    )
    if err != nil {
        return "", fmt.Errorf("create continuation step_run: %w", err)
    }
    return id, nil
}
```

(Use existing `nullableString` helper if present, or `sql.NullString{String: s, Valid: s != ""}`)

- [ ] **Step 4: Run test**

```bash
go test ./internal/activity/... -run TestCreateContinuationStepRun -v
```

Expected: PASS.

---

### Task 5: Extend ExecuteStep for awaiting_input + Checkpoint Checkout

**Files:**
- Modify: `internal/activity/execute.go`
- Modify: `internal/activity/execute_test.go`

The key changes: (a) `ExecuteStepInput` gains `ContinuationContext *model.ContinuationContext`; (b) if `ContinuationContext.CheckpointBranch` is set, checkout that branch after cloning; (c) after agent exits, query the `step_run` status — if `awaiting_input`, populate `StepOutput` with inbox metadata.

- [ ] **Step 1: Add `ContinuationContext` to `ExecuteStepInput`**

`ExecuteStepInput` is defined in `internal/workflow/step.go` (not execute.go). Add the field there:

```go
type ExecuteStepInput struct {
    StepInput           StepInput `json:"step_input"`
    SandboxID           string    `json:"sandbox_id"`
    Prompt              string    `json:"prompt"`
    ConversationHistory string    `json:"conversation_history,omitempty"`
    ContinuationContext *model.ContinuationContext `json:"continuation_context,omitempty"` // E3
}
```

- [ ] **Step 2: Write failing test for checkpoint branch checkout**

In `internal/activity/execute_test.go`:

```go
func TestExecuteStep_CheckpointBranchInjectedIntoPrompt(t *testing.T) {
    // If ContinuationContext.CheckpointBranch is set, the prompt should contain
    // the human answer and the clone logic should attempt to checkout the branch.
    // This test validates prompt assembly only (no real sandbox).
    cc := &model.ContinuationContext{
        InboxItemID:      "inbox-1",
        Question:         "Fix or skip flaky tests?",
        HumanAnswer:      "Fix flaky tests",
        CheckpointBranch: "fleetlift/checkpoint/run-1-fix",
    }
    prompt := buildContinuationPrompt("Original prompt text", cc)
    require.Contains(t, prompt, "Fix flaky tests")
    require.Contains(t, prompt, "Fix or skip flaky tests?")
    require.Contains(t, prompt, "Original prompt text")
}
```

- [ ] **Step 3: Run to confirm failure**

```bash
go test ./internal/activity/... -run TestExecuteStep_CheckpointBranch -v
```

Expected: FAIL (`buildContinuationPrompt` undefined).

- [ ] **Step 4: Add `buildContinuationPrompt` and checkpoint branch checkout**

Add to `internal/activity/execute.go`:

```go
// buildContinuationPrompt prepends the original prompt with continuation context.
func buildContinuationPrompt(originalPrompt string, cc *model.ContinuationContext) string {
    if cc == nil {
        return originalPrompt
    }
    header := fmt.Sprintf(
        "[CONTINUATION CONTEXT]\nPrevious step asked: %q\nHuman answered: %q\n\n"+
            "Your working state has been preserved. If a checkpoint branch was provided, "+
            "your working directory already contains your previous changes.\n"+
            "[END CONTINUATION CONTEXT]\n\n",
        cc.Question, cc.HumanAnswer,
    )
    return header + originalPrompt
}
```

In the repo-cloning section of `internal/activity/execute.go` (after clone succeeds, before launching agent), add checkpoint branch checkout:

```go
// After successful clone of repo, check out checkpoint branch if set
if input.ContinuationContext != nil && input.ContinuationContext.CheckpointBranch != "" {
    branch := input.ContinuationContext.CheckpointBranch
    // Validate branch name pattern before using in shell command
    if !checkpointBranchRe.MatchString(branch) {
        return nil, fmt.Errorf("invalid checkpoint branch name: %q", branch)
    }
    checkoutCmd := fmt.Sprintf("cd %s && git fetch origin %s && git checkout %s",
        shellquote.Quote(repoDir),
        shellquote.Quote(branch),
        shellquote.Quote(branch),
    )
    // Exec signature: (ctx, sandboxID, cmd, cwd) → (stdout, stderr, error)
    if _, _, err := a.Sandbox.Exec(ctx, input.SandboxID, checkoutCmd, "/"); err != nil {
        return nil, fmt.Errorf("checkout checkpoint branch %q: %w", branch, err)
    }
}
```

Add package-level regex:

```go
var checkpointBranchRe = regexp.MustCompile(`^fleetlift/checkpoint/[a-zA-Z0-9_-]+$`)
```

Apply continuation prompt to the `Prompt` field if `ContinuationContext` is set:

```go
if input.ContinuationContext != nil {
    input.Prompt = buildContinuationPrompt(input.Prompt, input.ContinuationContext)
}
```

- [ ] **Step 5: Detect `awaiting_input` after agent exits**

Ensure `"database/sql"` is imported in `internal/activity/execute.go` (add to import block if not present).

After the agent run loop completes (after processing all events), add:

```go
// Check if MCP handler set status to awaiting_input during this execution
if output.Status == "" || output.Status == "running" {
    var dbStatus string
    if err := a.DB.QueryRowContext(ctx,
        "SELECT status FROM step_runs WHERE id = $1",
        input.StepInput.StepRunID,
    ).Scan(&dbStatus); err == nil && dbStatus == "awaiting_input" {
        // Fetch inbox item question
        var inboxItemID, question string
        _ = a.DB.QueryRowContext(ctx,
            `SELECT id, COALESCE(question,'') FROM inbox_items
             WHERE step_run_id = $1 AND kind = 'request_input'
             ORDER BY created_at DESC LIMIT 1`,
            input.StepInput.StepRunID,
        ).Scan(&inboxItemID, &question)

        // Fetch checkpoint_branch and checkpoint_artifact_id from step_runs (not inbox_items)
        var checkpointBranch, stateArtifactID sql.NullString
        _ = a.DB.QueryRowContext(ctx,
            `SELECT COALESCE(checkpoint_branch,''), COALESCE(checkpoint_artifact_id::text,'')
             FROM step_runs WHERE id = $1`,
            input.StepInput.StepRunID,
        ).Scan(&checkpointBranch, &stateArtifactID)

        return &model.StepOutput{
            StepID:           input.StepInput.StepDef.ID, // logical step ID, not UUID
            Status:           "awaiting_input",
            InboxItemID:      inboxItemID,
            Question:         question,
            CheckpointBranch: checkpointBranch.String,
            StateArtifactID:  stateArtifactID.String,
        }, nil
    }
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/activity/... -run TestExecuteStep -v
```

Expected: all PASS.

- [ ] **Step 7: Build**

```bash
go build ./internal/activity/...
```

- [ ] **Step 8: Commit**

```bash
git add internal/activity/
git commit -m "feat(e3): activities — CleanupCheckpointBranch, CreateContinuationStepRun, ExecuteStep awaiting_input"
```

---

## Chunk 3: Backend API

### Task 6: MCP Inbox Handlers

**Files:**
- Modify: `internal/server/handlers/mcp.go`
- Modify: `internal/server/handlers/mcp_test.go`

- [ ] **Step 1: Write failing test for `HandleInboxNotify`**

In `internal/server/handlers/mcp_test.go`:

```go
func TestHandleMCPInboxNotify(t *testing.T) {
    h, db := newMCPTestHandler(t)
    teamID := insertTestTeam(t, db)
    runID := insertTestRun(t, db, teamID)

    body := `{"title":"Test notification","summary":"Something happened","urgency":"low"}`
    req := newMCPRequest(t, "POST", "/api/mcp/inbox/notify", body, teamID, runID)
    rec := httptest.NewRecorder()
    h.HandleInboxNotify(rec, req)

    require.Equal(t, http.StatusCreated, rec.Code)
    var resp map[string]string
    require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
    require.NotEmpty(t, resp["inbox_item_id"])

    // Verify DB record
    var kind, title string
    err := db.QueryRowContext(context.Background(),
        "SELECT kind, title FROM inbox_items WHERE id = $1", resp["inbox_item_id"],
    ).Scan(&kind, &title)
    require.NoError(t, err)
    require.Equal(t, "notify", kind)
    require.Equal(t, "Test notification", title)
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/server/handlers/... -run TestHandleMCPInboxNotify -v
```

Expected: FAIL.

- [ ] **Step 3: Implement `HandleInboxNotify`**

Add to `internal/server/handlers/mcp.go`:

```go
func (h *MCPHandler) HandleInboxNotify(w http.ResponseWriter, r *http.Request) {
    claims := mcpClaims(w, r)
    if claims == nil {
        return
    }
    var req struct {
        Title   string `json:"title"`
        Summary string `json:"summary"`
        Urgency string `json:"urgency"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeMCPErr(w, http.StatusBadRequest, "invalid request body")
        return
    }
    if req.Title == "" {
        writeMCPErr(w, http.StatusBadRequest, "title is required")
        return
    }
    if req.Urgency == "" {
        req.Urgency = "normal"
    }
    id := uuid.New().String()
    var summary sql.NullString
    if req.Summary != "" {
        summary = sql.NullString{String: req.Summary, Valid: true}
    }
    _, err := h.db.ExecContext(r.Context(), `
        INSERT INTO inbox_items (id, team_id, run_id, kind, title, summary, urgency, created_at)
        VALUES ($1,$2,$3,'notify',$4,$5,$6,now())`,
        id, claims.TeamID, claims.RunID, req.Title, summary, req.Urgency,
    )
    if err != nil {
        slog.Error("inbox notify: insert", "err", err)
        writeMCPErr(w, http.StatusInternalServerError, "failed to create notification")
        return
    }
    writeMCPJSON(w, http.StatusCreated, map[string]string{"inbox_item_id": id})
}
```

- [ ] **Step 4: Write failing test for `HandleInboxRequestInput`**

```go
func TestHandleMCPInboxRequestInput(t *testing.T) {
    h, db := newMCPTestHandler(t)
    teamID := insertTestTeam(t, db)
    runID, stepRunID := insertTestRunAndStep(t, db, teamID)

    body := `{
        "question": "Fix or skip flaky tests?",
        "state_summary": "Refactor complete, 3 flaky tests remain.",
        "options": ["Fix tests","Skip tests"],
        "checkpoint_branch": "fleetlift/checkpoint/run-abc-fix",
        "urgency": "normal"
    }`
    req := newMCPRequestWithStepRun(t, "POST", "/api/mcp/inbox/request_input", body, teamID, runID, stepRunID)
    rec := httptest.NewRecorder()
    h.HandleInboxRequestInput(rec, req)

    require.Equal(t, http.StatusCreated, rec.Code)
    var resp map[string]string
    require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
    require.Equal(t, "input_requested", resp["status"])
    require.NotEmpty(t, resp["inbox_item_id"])

    // Verify step_run is now awaiting_input
    var status string
    err := db.QueryRowContext(context.Background(),
        "SELECT status FROM step_runs WHERE id = $1", stepRunID).Scan(&status)
    require.NoError(t, err)
    require.Equal(t, "awaiting_input", status)
}

func TestHandleMCPInboxRequestInput_InvalidBranch(t *testing.T) {
    h, db := newMCPTestHandler(t)
    teamID := insertTestTeam(t, db)
    runID, stepRunID := insertTestRunAndStep(t, db, teamID)

    body := `{"question":"Q?","state_summary":"S","checkpoint_branch":"../../etc/passwd"}`
    req := newMCPRequestWithStepRun(t, "POST", "/api/mcp/inbox/request_input", body, teamID, runID, stepRunID)
    rec := httptest.NewRecorder()
    h.HandleInboxRequestInput(rec, req)
    require.Equal(t, http.StatusBadRequest, rec.Code)
}
```

- [ ] **Step 5: Run to confirm failure**

```bash
go test ./internal/server/handlers/... -run TestHandleMCPInboxRequestInput -v
```

Expected: FAIL.

- [ ] **Step 6: Implement `HandleInboxRequestInput`**

```go
var checkpointBranchRe = regexp.MustCompile(`^fleetlift/checkpoint/[a-zA-Z0-9_-]+$`)

func (h *MCPHandler) HandleInboxRequestInput(w http.ResponseWriter, r *http.Request) {
    claims := mcpClaims(w, r)
    if claims == nil {
        return
    }
    var req struct {
        Question         string   `json:"question"`
        StateSummary     string   `json:"state_summary"`
        Options          []string `json:"options"`
        CheckpointBranch string   `json:"checkpoint_branch"`
        Urgency          string   `json:"urgency"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeMCPErr(w, http.StatusBadRequest, "invalid request body")
        return
    }
    if req.Question == "" {
        writeMCPErr(w, http.StatusBadRequest, "question is required")
        return
    }
    if req.CheckpointBranch != "" && !checkpointBranchRe.MatchString(req.CheckpointBranch) {
        writeMCPErr(w, http.StatusBadRequest, "invalid checkpoint_branch: must match fleetlift/checkpoint/<alphanumeric-dash-underscore>")
        return
    }
    if req.Urgency == "" {
        req.Urgency = "normal"
    }

    // Look up step_run_id from the MCP token's run context (current running step)
    var stepRunID string
    err := h.db.QueryRowContext(r.Context(), `
        SELECT id FROM step_runs
        WHERE run_id = $1 AND status NOT IN ('complete','failed','skipped','awaiting_input')
        ORDER BY created_at DESC LIMIT 1`, claims.RunID,
    ).Scan(&stepRunID)
    if err != nil {
        slog.Error("inbox request_input: find step_run", "err", err)
        writeMCPErr(w, http.StatusInternalServerError, "could not find active step")
        return
    }

    // Create checkpoint artifact if state_summary provided
    var artifactID sql.NullString
    if req.StateSummary != "" {
        aid := uuid.New().String()
        _, err = h.db.ExecContext(r.Context(), `
            INSERT INTO artifacts (id, step_run_id, name, path, size_bytes, content_type, storage, data, created_at)
            VALUES ($1,$2,'agent-checkpoint','/checkpoint.md',$3,'text/markdown','inline',$4,now())`,
            aid, stepRunID, len(req.StateSummary), req.StateSummary,
        )
        if err != nil {
            slog.Error("inbox request_input: create artifact", "err", err)
        } else {
            artifactID = sql.NullString{String: aid, Valid: true}
        }
    }

    // Create inbox item (checkpoint_artifact_id lives on step_runs, not inbox_items)
    var stateSummary sql.NullString
    if req.StateSummary != "" {
        stateSummary = sql.NullString{String: req.StateSummary, Valid: true}
    }
    itemID := uuid.New().String()
    _, err = h.db.ExecContext(r.Context(), `
        INSERT INTO inbox_items
            (id, team_id, run_id, step_run_id, kind, title, summary, question, options, urgency, created_at)
        VALUES ($1,$2,$3,$4,'request_input',$5,$6,$7,$8,$9,now())`,
        itemID, claims.TeamID, claims.RunID, stepRunID,
        req.Question,  // title
        stateSummary,
        req.Question,
        pq.StringArray(req.Options),
        req.Urgency,
    )
    if err != nil {
        slog.Error("inbox request_input: insert item", "err", err)
        writeMCPErr(w, http.StatusInternalServerError, "failed to create inbox item")
        return
    }

    // Mark step as awaiting_input
    if _, err := h.db.ExecContext(r.Context(),
        "UPDATE step_runs SET status='awaiting_input' WHERE id=$1", stepRunID,
    ); err != nil {
        slog.Error("inbox request_input: update step status", "err", err)
    }

    // Store checkpoint_branch and checkpoint_artifact_id on step_runs
    if req.CheckpointBranch != "" || artifactID.Valid {
        if _, err := h.db.ExecContext(r.Context(),
            "UPDATE step_runs SET checkpoint_branch=$1, checkpoint_artifact_id=$2 WHERE id=$3",
            sql.NullString{String: req.CheckpointBranch, Valid: req.CheckpointBranch != ""},
            artifactID,
            stepRunID,
        ); err != nil {
            slog.Error("inbox request_input: store checkpoint fields on step_run", "err", err)
        }
    }

    writeMCPJSON(w, http.StatusCreated, map[string]string{
        "inbox_item_id": itemID,
        "status":        "input_requested",
    })
}
```

- [ ] **Step 7: Run tests**

```bash
go test ./internal/server/handlers/... -run "TestHandleMCPInbox" -v
```

Expected: all PASS.

---

### Task 7: Inbox Respond Handler

**Files:**
- Modify: `internal/server/handlers/inbox.go`
- Modify: `internal/server/handlers/inbox_test.go`

The `Respond` handler validates the inbox item, stores the answer, and sends a Temporal `respond` signal to the waiting `StepWorkflow`.

- [ ] **Step 1: Add Temporal client to `InboxHandler`**

In `internal/server/handlers/inbox.go`, extend the handler struct:

```go
type InboxHandler struct {
    db             *sqlx.DB
    temporalClient client.Client // add this
}

func NewInboxHandler(db *sqlx.DB, tc client.Client) *InboxHandler {
    return &InboxHandler{db: db, temporalClient: tc}
}
```

Update the instantiation in `cmd/server/main.go`:

```go
Inbox: handlers.NewInboxHandler(database, temporalClient),
```

- [ ] **Step 2: Write failing test for `Respond`**

In `internal/server/handlers/inbox_test.go`:

```go
func TestInboxRespond(t *testing.T) {
    db := setupTestDB(t)
    // Use a mock Temporal client that records signals
    mockTC := &mockTemporalClient{}
    h := NewInboxHandler(db, mockTC)

    teamID, userID := insertTestTeamAndUser(t, db)
    runID := insertTestRun(t, db, teamID)
    stepRunID := insertTestStepRun(t, db, runID, "awaiting_input", "wf-id-123")
    itemID := insertTestInboxItem(t, db, teamID, runID, stepRunID, "request_input")

    body := `{"answer":"Fix flaky tests"}`
    req := newAuthenticatedRequest(t, "POST", "/api/inbox/"+itemID+"/respond", body, userID, teamID)
    rec := httptest.NewRecorder()

    router := chi.NewRouter()
    router.Post("/api/inbox/{id}/respond", h.Respond)
    router.ServeHTTP(rec, req)

    require.Equal(t, http.StatusNoContent, rec.Code)

    // Verify answer persisted
    var answer string
    err := db.QueryRowContext(context.Background(),
        "SELECT answer FROM inbox_items WHERE id=$1", itemID).Scan(&answer)
    require.NoError(t, err)
    require.Equal(t, "Fix flaky tests", answer)

    // Verify Temporal signal was sent
    require.Len(t, mockTC.signals, 1)
    require.Equal(t, "wf-id-123", mockTC.signals[0].workflowID)
    require.Equal(t, "respond", mockTC.signals[0].signalName)
}

func TestInboxRespond_AlreadyAnswered(t *testing.T) {
    // Should return 409 if already answered
    ...
}
```

- [ ] **Step 3: Run to confirm failure**

```bash
go test ./internal/server/handlers/... -run TestInboxRespond -v
```

Expected: FAIL.

- [ ] **Step 4: Implement `Respond` handler**

```go
func (h *InboxHandler) Respond(w http.ResponseWriter, r *http.Request) {
    itemID := chi.URLParam(r, "id")
    claims := auth.ClaimsFromContext(r.Context())
    if claims == nil {
        writeJSONError(w, http.StatusUnauthorized, "unauthorized")
        return
    }

    var req struct {
        Answer string `json:"answer"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Answer == "" {
        writeJSONError(w, http.StatusBadRequest, "answer is required")
        return
    }

    // Fetch inbox item — validate ownership and kind
    var item model.InboxItem
    err := h.db.GetContext(r.Context(), &item,
        `SELECT * FROM inbox_items WHERE id=$1 AND team_id=$2`, itemID, claims.TeamID)
    if err == sql.ErrNoRows {
        writeJSONError(w, http.StatusNotFound, "inbox item not found")
        return
    } else if err != nil {
        slog.Error("inbox respond: fetch item", "err", err)
        writeJSONError(w, http.StatusInternalServerError, "internal error")
        return
    }
    if item.Kind != "request_input" {
        writeJSONError(w, http.StatusBadRequest, "item is not a request_input")
        return
    }
    if item.AnsweredAt != nil {
        writeJSONError(w, http.StatusConflict, "already answered")
        return
    }

    // Persist answer
    now := time.Now()
    _, err = h.db.ExecContext(r.Context(), `
        UPDATE inbox_items SET answer=$1, answered_at=$2, answered_by=$3 WHERE id=$4`,
        req.Answer, now, claims.UserID, itemID, // Claims has no Email field; UserID is the identity
    )
    if err != nil {
        slog.Error("inbox respond: store answer", "err", err)
        writeJSONError(w, http.StatusInternalServerError, "failed to store answer")
        return
    }

    // Look up the waiting step_run's temporal_workflow_id
    if item.StepRunID != nil {
        var workflowID string
        if err := h.db.QueryRowContext(r.Context(),
            "SELECT temporal_workflow_id FROM step_runs WHERE id=$1", *item.StepRunID,
        ).Scan(&workflowID); err == nil && workflowID != "" {
            signal := model.InboxAnswer{Answer: req.Answer, Responder: claims.UserID}
            if err := h.temporalClient.SignalWorkflow(r.Context(), workflowID, "",
                "respond", signal,
            ); err != nil {
                slog.Error("inbox respond: signal workflow", "err", err, "workflow_id", workflowID)
                // Non-fatal: answer is stored; workflow will see it on next poll
            }
        }
    }

    w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/server/handlers/... -run "TestInboxRespond" -v
```

Expected: all PASS.

---

### Task 8: Route Wiring

**Files:**
- Modify: `internal/server/router.go`

- [ ] **Step 1: Add new MCP routes**

In the `/api/mcp` route group in `internal/server/router.go`:

```go
r.Post("/inbox/notify", deps.MCP.HandleInboxNotify)
r.Post("/inbox/request_input", deps.MCP.HandleInboxRequestInput)
```

- [ ] **Step 2: Add inbox respond route**

In the authenticated route group:

```go
r.Post("/api/inbox/{id}/respond", deps.Inbox.Respond)
```

- [ ] **Step 3: Build server**

```bash
go build ./cmd/server/...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/server/
git commit -m "feat(e3): backend API — HandleInboxNotify, HandleInboxRequestInput, inbox Respond endpoint"
```

---

## Chunk 4: Temporal Workflow

### Task 9: StepWorkflow Await/Resume Cycle

**Files:**
- Modify: `internal/workflow/step.go`
- Modify: `internal/workflow/step_test.go`

This is the most complex task. The `StepWorkflow` must:
1. Register a `respond` signal handler
2. After `ExecuteStep` returns, check if status is `awaiting_input`
3. If so: wait for `respond` signal, then call `CreateContinuationStepRun` + re-run `ExecuteStep` with `ContinuationContext`
4. Use the continuation result as the final output
5. After continuation completes (success or failure): call `CleanupCheckpointBranch` if needed

- [ ] **Step 1: Write failing test for HITL await/resume cycle**

In `internal/workflow/step_test.go`, add a test using `go.temporal.io/sdk/testsuite`:

```go
func TestStepWorkflow_AwaitResumeCycle(t *testing.T) {
    suite := &testsuite.WorkflowTestSuite{}
    env := suite.NewTestWorkflowEnvironment()

    // Mock ExecuteStep: first call returns awaiting_input; second call (continuation) returns complete
    callCount := 0
    env.OnActivity(activity.ActivityExecuteStep, mock.Anything, mock.Anything).
        Return(func(ctx context.Context, input activity.ExecuteStepInput) (*model.StepOutput, error) {
            callCount++
            if callCount == 1 {
                return &model.StepOutput{
                    Status:      "awaiting_input",
                    InboxItemID: "inbox-1",
                    Question:    "Fix or skip?",
                }, nil
            }
            // Continuation call — verify ContinuationContext was set
            require.NotNil(t, input.ContinuationContext)
            require.Equal(t, "Fix tests", input.ContinuationContext.HumanAnswer)
            return &model.StepOutput{Status: "complete", Output: map[string]any{"result": "done"}}, nil
        })

    env.OnActivity(activity.ActivityCreateContinuationStepRun, mock.Anything, mock.Anything).
        Return("continuation-step-run-id", nil)
    env.OnActivity(activity.ActivityUpdateStepStatus, mock.Anything, mock.Anything).
        Return(nil)
    env.OnActivity(activity.ActivityCompleteStepRun, mock.Anything, mock.Anything).
        Return(nil)
    env.OnActivity(activity.ActivityCleanupSandbox, mock.Anything, mock.Anything).
        Return(nil)
    env.OnActivity(activity.ActivityProvisionSandbox, mock.Anything, mock.Anything).
        Return("sandbox-id", nil)

    // Deliver the respond signal after workflow starts
    env.RegisterDelayedCallback(func() {
        env.SignalWorkflow("respond", model.InboxAnswer{
            Answer:    "Fix tests",
            Responder: "jane@example.com",
        })
    }, 0)

    env.ExecuteWorkflow(StepWorkflow, StepInput{
        RunID:    "run-1",
        StepRunID: "step-run-1",
        TeamID:   "team-1",
    })

    require.True(t, env.IsWorkflowCompleted())
    require.NoError(t, env.GetWorkflowError())
    require.Equal(t, 2, callCount) // both original and continuation were called
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/workflow/... -run TestStepWorkflow_AwaitResumeCycle -v
```

Expected: FAIL.

- [ ] **Step 3: Add `respond` signal channel to StepWorkflow**

Near the top of `StepWorkflow` in `internal/workflow/step.go`, after the existing signal channels are set up via `workflow.GetSignalChannel`, add:

```go
respondCh := workflow.GetSignalChannel(ctx, "respond")
```

- [ ] **Step 4: Add await/resume logic after ExecuteStep returns**

After the call to `ExecuteStepActivity` and before sandbox cleanup, insert:

```go
if stepOutput != nil && stepOutput.Status == "awaiting_input" {
    // Status already written to DB by MCP handler — just wait for human signal
    // TODO(v2): add WorkflowExecutionTimeout — indefinite wait accumulates Temporal history events

    var answer model.InboxAnswer
    respondCh.Receive(ctx, &answer)

    // Create continuation step_run record
    continuationStepID := input.StepDef.ID + "-resume-1"
    continuationWorkflowID := input.RunID + "-" + continuationStepID
    var continuationStepRunID string

    err = workflow.ExecuteActivity(
        workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
            StartToCloseTimeout: 30 * time.Second,
            RetryPolicy:         dbRetry,
        }),
        CreateContinuationStepRunActivity,
        model.CreateContinuationStepRunInput{
            RunID:                input.RunID,
            StepID:               continuationStepID,
            StepTitle:            input.StepDef.Title + " (resumed)",
            TemporalWorkflowID:   continuationWorkflowID,
            ParentStepRunID:      input.StepRunID,
            CheckpointBranch:     stepOutput.CheckpointBranch,
            CheckpointArtifactID: stepOutput.StateArtifactID,
        },
    ).Get(ctx, &continuationStepRunID)
    if err != nil {
        return nil, fmt.Errorf("create continuation step_run: %w", err)
    }

    // Provision a fresh sandbox (same pattern as top of StepWorkflow)
    continuationStepInput := input
    continuationStepInput.StepRunID = continuationStepRunID
    continuationStepInput.SandboxID = "" // force new provision

    var continuationSandboxID string
    err = workflow.ExecuteActivity(
        workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
            StartToCloseTimeout: 5 * time.Minute,
            RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
        }),
        ProvisionSandboxActivity, continuationStepInput,
    ).Get(ctx, &continuationSandboxID)
    if err != nil {
        return nil, fmt.Errorf("provision continuation sandbox: %w", err)
    }

    // Re-execute with continuation context
    var continuationOutput *model.StepOutput
    err = workflow.ExecuteActivity(
        workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
            StartToCloseTimeout: timeout,
            HeartbeatTimeout:    2 * time.Minute,
            RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
        }),
        ExecuteStepActivity, ExecuteStepInput{
            StepInput:           continuationStepInput,
            SandboxID:           continuationSandboxID,
            Prompt:              execInput.Prompt, // original rendered prompt; buildContinuationPrompt prepends context
            ContinuationContext: &model.ContinuationContext{
                InboxItemID:      stepOutput.InboxItemID,
                Question:         stepOutput.Question,
                HumanAnswer:      answer.Answer,
                CheckpointBranch: stepOutput.CheckpointBranch,
                StateArtifactID:  stepOutput.StateArtifactID,
            },
        },
    ).Get(ctx, &continuationOutput)

    // Cleanup continuation sandbox
    _ = workflow.ExecuteActivity(
        workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
            StartToCloseTimeout: 2 * time.Minute,
            RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
        }),
        CleanupSandboxActivity, continuationSandboxID,
    ).Get(ctx, nil)

    // Cleanup checkpoint branch if set
    // ResolvedOpts.Repos has the repo list; Credentials has the credential names
    if stepOutput.CheckpointBranch != "" && len(input.ResolvedOpts.Repos) > 0 {
        credName := ""
        if len(input.ResolvedOpts.Credentials) > 0 {
            credName = input.ResolvedOpts.Credentials[0]
        }
        _ = workflow.ExecuteActivity(
            workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
                StartToCloseTimeout: 2 * time.Minute,
                RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
            }),
            CleanupCheckpointBranchActivity, model.CleanupCheckpointInput{
                RepoURL:        input.ResolvedOpts.Repos[0].URL,
                Branch:         stepOutput.CheckpointBranch,
                CredentialName: credName,
                TeamID:         input.TeamID,
            },
        ).Get(ctx, nil)
    }

    stepOutput = continuationOutput
}
```

Note: `input` here refers to `StepInput` (not `ExecuteStepInput`). The `StepWorkflow` receives `StepInput` directly. `execInput` is the `ExecuteStepInput` that was assembled to call `ExecuteStepActivity` — reference it for the `Prompt` field.

- [ ] **Step 5: Add new activity name constants to `step.go` local variable block**

In `internal/workflow/step.go`, find the block where activity name constants are aliased (e.g. `ProvisionSandboxActivity`, `ExecuteStepActivity`, etc.) and add:

```go
CreateContinuationStepRunActivity  = activity.ActivityCreateContinuationStepRun
CleanupCheckpointBranchActivity    = activity.ActivityCleanupCheckpointBranch
```

These constants are defined in `internal/activity/constants.go` (added in Task 3). The aliases here follow the existing pattern in `step.go`.

- [ ] **Step 6: Register in `cmd/worker/main.go`**

The new activities (`CleanupCheckpointBranch`, `CreateContinuationStepRun`) are methods on the `Activities` struct, so `w.RegisterActivity(acts)` already covers them. No additional registration needed — but add constants to `internal/activity/constants.go` to confirm (done in Task 3).

- [ ] **Step 7: Run test**

```bash
go test ./internal/workflow/... -run TestStepWorkflow_AwaitResumeCycle -v
```

Expected: PASS.

- [ ] **Step 8: Run all workflow tests**

```bash
go test ./internal/workflow/... -v
```

Expected: all PASS.

- [ ] **Step 9: Build worker**

```bash
go build ./cmd/worker/...
```

- [ ] **Step 10: Commit**

```bash
git add internal/workflow/ cmd/worker/
git commit -m "feat(e3): StepWorkflow await/resume cycle — respond signal + continuation step"
```

---

## Chunk 5: MCP Sidecar + Lint + Tests

### Task 10: MCP Sidecar — Register inbox Tools

**Files:**
- Modify: `cmd/mcp-sidecar/main.go`
- Modify: `cmd/mcp-sidecar/main_test.go`

- [ ] **Step 1: Write failing test for tool registration**

In `cmd/mcp-sidecar/main_test.go`:

```go
func TestSidecarRegistersInboxTools(t *testing.T) {
    tools := registeredToolNames()
    require.Contains(t, tools, "inbox.notify")
    require.Contains(t, tools, "inbox.request_input")
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./cmd/mcp-sidecar/... -run TestSidecarRegistersInboxTools -v
```

Expected: FAIL.

- [ ] **Step 3: Register `inbox.notify`**

In `cmd/mcp-sidecar/main.go`, tools are registered as methods on the `Shim` struct (`s`). Add inside the `registerTools` function (or wherever other tools are registered), following the existing pattern:

```go
srv.AddTool(mcp.NewTool("inbox.notify",
    mcp.WithDescription("Send a notification to the team inbox without blocking execution."),
    mcp.WithString("title", mcp.Required()),
    mcp.WithString("summary", mcp.Required()),
    mcp.WithString("urgency"), // "low" | "normal" | "high"; default: "normal"
), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    result, err := s.call("POST", "/api/mcp/inbox/notify", map[string]any{
        "title":   req.Params.Arguments["title"],
        "summary": req.Params.Arguments["summary"],
        "urgency": req.Params.Arguments["urgency"],
    })
    if err != nil {
        return mcp.NewToolResultError(err.Error()), nil
    }
    return resultJSON(result), nil
})
```

- [ ] **Step 4: Register `inbox.request_input`**

```go
srv.AddTool(mcp.NewTool("inbox.request_input",
    mcp.WithDescription(
        "Request human input. This ends the current step. "+
            "A continuation step will run in a fresh sandbox with the human's answer once they respond. "+
            "IMPORTANT: Call this as your FINAL action before exiting. Do not continue work after this call.",
    ),
    mcp.WithString("question", mcp.Required()),
    mcp.WithString("state_summary"),     // optional: what you've done so far
    mcp.WithArray("options"),            // optional: list of choices for the human
    mcp.WithString("checkpoint_branch"), // optional: git branch with committed working state
    mcp.WithString("urgency"),           // "low" | "normal" | "high"
), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    result, err := s.call("POST", "/api/mcp/inbox/request_input", map[string]any{
        "question":          req.Params.Arguments["question"],
        "state_summary":     req.Params.Arguments["state_summary"],
        "options":           req.Params.Arguments["options"],
        "checkpoint_branch": req.Params.Arguments["checkpoint_branch"],
        "urgency":           req.Params.Arguments["urgency"],
    })
    if err != nil {
        return mcp.NewToolResultError(err.Error()), nil
    }
    return resultJSON(result), nil
})
```

- [ ] **Step 5: Run test**

```bash
go test ./cmd/mcp-sidecar/... -run TestSidecarRegistersInboxTools -v
```

Expected: PASS.

- [ ] **Step 6: Build sidecar (both arches)**

```bash
make mcp-sidecar
```

Expected: `bin/fleetlift-mcp-amd64` and `bin/fleetlift-mcp-arm64` produced.

- [ ] **Step 7: Run full test suite**

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 8: Run linter**

```bash
make lint
```

Expected: no lint errors.

- [ ] **Step 9: Commit**

```bash
git add cmd/mcp-sidecar/
git commit -m "feat(e3): MCP sidecar — inbox.notify and inbox.request_input tools"
```

---

## Chunk 6: Frontend

### Task 11: Inbox — request_input Item UI

**Files:**
- Modify: `web/src/pages/Inbox.tsx` (or wherever inbox items are rendered)
- Modify: `web/src/api/client.ts` (or equivalent API client)
- Create: `web/src/components/InboxRequestInput.tsx`
- Create: `web/src/components/InboxRequestInput.test.tsx`

- [ ] **Step 1: Add `respondToInbox` to API client**

In `web/src/api/client.ts` (or equivalent):

```ts
export async function respondToInbox(itemId: string, answer: string): Promise<void> {
  const res = await fetch(`/api/inbox/${itemId}/respond`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ answer }),
  })
  if (res.status !== 204) {
    const err = await res.json().catch(() => ({ error: 'unknown error' }))
    throw new Error(err.error ?? 'Failed to submit answer')
  }
}
```

- [ ] **Step 2: Write failing test for `InboxRequestInput` component**

In `web/src/components/InboxRequestInput.test.tsx`:

```tsx
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { InboxRequestInput } from './InboxRequestInput'

const mockItem = {
  id: 'item-1',
  kind: 'request_input',
  title: 'Fix or skip flaky tests?',
  question: 'Fix or skip flaky tests?',
  options: ['Fix tests', 'Skip tests'],
  urgency: 'normal',
  created_at: '2026-03-16T10:00:00Z',
}

test('renders question and options', () => {
  render(<InboxRequestInput item={mockItem} onRespond={jest.fn()} />)
  expect(screen.getByText('Fix or skip flaky tests?')).toBeInTheDocument()
  expect(screen.getByText('Fix tests')).toBeInTheDocument()
  expect(screen.getByText('Skip tests')).toBeInTheDocument()
})

test('calls onRespond with selected option', async () => {
  const onRespond = jest.fn()
  render(<InboxRequestInput item={mockItem} onRespond={onRespond} />)
  fireEvent.click(screen.getByText('Fix tests'))
  await waitFor(() => expect(onRespond).toHaveBeenCalledWith('Fix tests'))
})

test('renders free-text input when no options', () => {
  render(<InboxRequestInput item={{...mockItem, options: []}} onRespond={jest.fn()} />)
  expect(screen.getByRole('textbox')).toBeInTheDocument()
  expect(screen.getByText('Submit')).toBeInTheDocument()
})
```

- [ ] **Step 3: Run to confirm failure**

```bash
cd web && npm test -- --testPathPattern=InboxRequestInput --watchAll=false
```

Expected: FAIL (component not found).

- [ ] **Step 4: Create `web/src/components/InboxRequestInput.tsx`**

```tsx
import { useState } from 'react'
import { respondToInbox } from '../api/client'

interface InboxItem {
  id: string
  kind: string
  title: string
  question?: string
  options?: string[]
  urgency?: string
  created_at: string
  answer?: string
}

interface Props {
  item: InboxItem
  onRespond: (answer: string) => void
}

export function InboxRequestInput({ item, onRespond }: Props) {
  const [freeText, setFreeText] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async (answer: string) => {
    if (!answer.trim()) return
    setSubmitting(true)
    setError(null)
    try {
      await respondToInbox(item.id, answer)
      onRespond(answer)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to submit')
    } finally {
      setSubmitting(false)
    }
  }

  if (item.answer) {
    return (
      <div className="inbox-request-answered">
        <span className="label">Answered:</span> {item.answer}
      </div>
    )
  }

  return (
    <div className="inbox-request-input">
      <p className="question">{item.question ?? item.title}</p>
      {item.options && item.options.length > 0 ? (
        <div className="options">
          {item.options.map(opt => (
            <button
              key={opt}
              className="option-btn"
              disabled={submitting}
              onClick={() => submit(opt)}
            >
              {opt}
            </button>
          ))}
        </div>
      ) : (
        <div className="free-text">
          <textarea
            value={freeText}
            onChange={e => setFreeText(e.target.value)}
            placeholder="Type your response…"
            rows={3}
            disabled={submitting}
          />
          <button onClick={() => submit(freeText)} disabled={submitting || !freeText.trim()}>
            Submit
          </button>
        </div>
      )}
      {error && <p className="error">{error}</p>}
    </div>
  )
}
```

- [ ] **Step 5: Run tests**

```bash
cd web && npm test -- --testPathPattern=InboxRequestInput --watchAll=false
```

Expected: all PASS.

- [ ] **Step 6: Integrate into Inbox page**

In `web/src/pages/Inbox.tsx` (or wherever inbox items are listed), update the item renderer to use `InboxRequestInput` for `kind === 'request_input'` items:

```tsx
import { InboxRequestInput } from '../components/InboxRequestInput'

// In the item render:
{item.kind === 'request_input' ? (
  <InboxRequestInput item={item} onRespond={() => refetchInbox()} />
) : (
  // existing rendering for awaiting_input / output_ready
  <ExistingInboxItemRenderer item={item} />
)}
```

---

### Task 12: Run Detail — Continuation Steps

**Files:**
- Modify: `web/src/pages/RunDetail.tsx` (or step timeline component)

- [ ] **Step 1: Extend step type to include continuation fields**

In the TypeScript types file (wherever step types are defined):

```ts
interface StepRun {
  // ... existing fields ...
  parent_step_run_id?: string
  checkpoint_branch?: string
  status: 'pending' | 'cloning' | 'running' | 'verifying' | 'awaiting_input' |
          'complete' | 'failed' | 'skipped'
}
```

- [ ] **Step 2: Group continuation steps under their parent in the timeline**

In the step timeline rendering logic:

```tsx
// Build step tree: group continuation steps under their parent
const rootSteps = steps.filter(s => !s.parent_step_run_id)
const continuationsByParent = new Map<string, StepRun[]>()
steps.filter(s => s.parent_step_run_id).forEach(s => {
  const list = continuationsByParent.get(s.parent_step_run_id!) ?? []
  list.push(s)
  continuationsByParent.set(s.parent_step_run_id!, list)
})
```

- [ ] **Step 3: Render `awaiting_input` status badge**

In the status badge component, add the `awaiting_input` case (alongside the existing ones):

```tsx
case 'awaiting_input':
  return <span className="badge badge-waiting">Waiting for input</span>
```

- [ ] **Step 4: Render continuation step indented under parent**

```tsx
{rootSteps.map(step => (
  <div key={step.id} className="step-group">
    <StepRow step={step} />
    {continuationsByParent.get(step.id)?.map(continuation => (
      <div key={continuation.id} className="step-continuation">
        <span className="resume-badge">Resumed</span>
        <StepRow step={continuation} />
      </div>
    ))}
  </div>
))}
```

- [ ] **Step 5: Build frontend**

```bash
cd web && npm run build
```

Expected: no errors.

- [ ] **Step 6: Run all frontend tests**

```bash
cd web && npm test -- --watchAll=false
```

Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/
git commit -m "feat(e3): frontend — inbox request_input UI + continuation steps in run detail"
```

---

## Final Verification

- [ ] **Run full test suite**

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Run linter**

```bash
make lint
```

Expected: no errors.

- [ ] **Build all binaries**

```bash
go build ./... && make mcp-sidecar
```

Expected: clean build.

- [ ] **Start integration environment and run MCP test**

```bash
scripts/integration/start.sh --build
scripts/integration/run-mcp-test.sh
```

Expected: all checks pass.

- [ ] **Update ROADMAP.md**

Mark E3 as complete:

```markdown
| E3 | Interactive tools | `inbox.request_input`, `inbox.notify` | ✅ |
```

- [ ] **Final commit**

```bash
git add docs/plans/ROADMAP.md
git commit -m "docs: mark Track E complete — E3 inbox interactive tools shipped"
```
