# Minion-Parity Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add individual-developer task delegation to Fleetlift (prompt-first Home page, quick-run workflow, model selection, prompt improvement, presets, co-author attribution) so it serves both fleet-wide operations and ad-hoc toil tasks.

**Architecture:** Five PRs, each independently shippable. Phase 1 is split into backend (PR 1) and frontend (PR 2). Phases 2–4 are one PR each. Every change is additive — existing workflow pages and fleet operations are untouched. The quick-run workflow is a builtin YAML with a `hidden: true` flag so it doesn't appear in the Workflows list. Model selection is a run-level override that flows through DAGInput → StepInput → RunOpts → bridge.js → `claude --model`.

**Tech Stack:** Go, Temporal SDK, React 19, TypeScript, Vite, Tailwind, PostgreSQL, Claude API (Anthropic SDK for prompt improvement)

**Spec:** `docs/superpowers/specs/2026-03-25-minion-parity-design.md`

## Progress Log

- 2026-03-26: Implemented (uncommitted) PR 1 Task 0b follow-up: replaced string-based refresh error classification with typed auth errors (`errors.Is`) and added refresh-path regression tests for invalid/expired/reused/internal-DB-failure cases.
- 2026-03-26: Implemented (uncommitted) PR 1 auth/personal-tenancy foundation: migration `012_personal_space_and_run_model.up.sql`, OAuth personal team provisioning, JWT + `/api/me` personal-team filtering, transactional refresh rotation via `RefreshSession`, fail-closed auth error handling, and expanded auth handler tests.
- 2026-03-26: Implemented (uncommitted) PR 1 backend slice for run model flow plumbing, `created_by=me` + `limit` run listing filters, builtin `hidden` support, and hidden `quick-run` builtin template. Verification passed (`go test ./...`, `make lint`, `go build ./...`).
- 2026-03-26: Git status remains dirty with no commits yet in this branch; commit steps below remain intentionally unchecked until commit(s) are created.

---

## File Map

### PR 1 — Backend: model override, created_by filter, hidden flag, quick-run workflow

| Action | File | Purpose |
|--------|------|---------|
| Create | `internal/db/migrations/012_personal_space_and_run_model.up.sql` | Add `users.personal_team_id` + unique index + nullable `runs.model` |
| Modify | `internal/server/handlers/auth.go` | Personal team provisioning, filtering, and fail-closed auth behavior |
| Modify | `internal/auth/jwt.go` | Transactional refresh (`RefreshSession`) with row locking |
| Modify | `internal/server/handlers/auth_test.go` | Regression tests for personal team + refresh + fail-closed behavior |
| Modify | `internal/model/run.go` | Add `Model` field to `Run` struct |
| Modify | `internal/server/handlers/runs.go` | Add `model` to `createRunRequest`, store in DB, add `created_by` filter to ListRuns |
| Modify | `internal/workflow/dag.go` | Add `ModelOverride` to `DAGInput`, pass to `StepInput` |
| Modify | `internal/workflow/step.go` | Add `ModelOverride` to `StepInput` |
| Modify | `internal/agent/runner.go` | Add `Model` to `RunOpts` |
| Modify | `internal/agent/claudecode.go` | Add `Model` to `bridgeRequest`, pass from `RunOpts` |
| Modify | `internal/activity/execute.go` | Pass `ModelOverride` from `StepInput` to `RunOpts.Model` |
| Modify | `docker/bridge.js` | Read `request.model`, add `--model` flag to CLI args |
| Modify | `internal/template/builtin.go` | Filter templates where `hidden: true` from List() |
| Modify | `internal/model/workflow.go` | Add `Hidden bool` to `WorkflowDef` |
| Create | `internal/template/workflows/quick-run.yaml` | Single-step passthrough workflow |
| Modify | `internal/template/builtin_test.go` | Update count, add quick-run tests |

### PR 2 — Frontend: Home page, nav, model dropdown, retry, log search

| Action | File | Purpose |
|--------|------|---------|
| Modify | `web/src/api/types.ts` | Add `model` to `Run` interface |
| Modify | `web/src/api/client.ts` | Update `createRun` to accept `model`, add `listMyRuns` |
| Create | `web/src/pages/HomePage.tsx` | Prompt-first Home page |
| Modify | `web/src/components/Layout.tsx` | Add Home nav item |
| Modify | `web/src/App.tsx` | Replace `/` redirect with HomePage route |
| Create | `web/src/components/ModelSelect.tsx` | Reusable model dropdown |
| Modify | `web/src/pages/WorkflowDetail.tsx` | Add ModelSelect to run form |
| Modify | `web/src/pages/RunDetail.tsx` | Add Retry button |
| Modify | `web/src/components/LogStream.tsx` | Add search/filter input |

### PR 3 — Prompt improvement

| Action | File | Purpose |
|--------|------|---------|
| Create | `internal/server/handlers/prompt.go` | `POST /api/prompt/improve` handler |
| Create | `internal/server/handlers/prompt_test.go` | Handler tests |
| Modify | `internal/server/router.go` (or equivalent) | Register route |
| Modify | `web/src/api/client.ts` | Add `improvePrompt` method |
| Create | `web/src/components/PromptImproveModal.tsx` | Side-by-side modal |

### PR 4 — Presets + saved repos

| Action | File | Purpose |
|--------|------|---------|
| Create | `internal/db/migrations/013_presets_repos.up.sql` | `prompt_presets` and `user_repos` tables |
| Create | `internal/server/handlers/presets.go` | CRUD handlers for presets |
| Create | `internal/server/handlers/saved_repos.go` | CRUD handlers for saved repos |
| Modify | `internal/server/router.go` | Register routes |
| Modify | `web/src/api/client.ts` | Add preset + repo API methods |
| Modify | `web/src/api/types.ts` | Add Preset + SavedRepo interfaces |
| Modify | `web/src/pages/HomePage.tsx` | Add presets sidebar, repo combobox |

### PR 5 — Co-author attribution

| Action | File | Purpose |
|--------|------|---------|
| Modify | `internal/workflow/dag.go` | Pass `CreatedBy` from run to `DAGInput` → `StepInput` |
| Modify | `internal/workflow/step.go` | Add `CreatedBy` to `StepInput` |
| Modify | `internal/activity/execute.go` | Look up user git identity, inject env vars into sandbox |
| Create | `internal/activity/execute_coauthor_test.go` | Tests for co-author injection |

---

## PR 1 — Backend: model override, created_by filter, hidden flag, quick-run workflow

### Task 0: Auth and personal tenancy foundation (already implemented)

**Files:**
- Create: `internal/db/migrations/012_personal_space_and_run_model.up.sql`
- Modify: `internal/server/handlers/auth.go`
- Modify: `internal/auth/jwt.go`
- Modify: `internal/server/handlers/auth_test.go`

- [x] **Step 0.1: Add schema support for personal team ownership and run model storage**

Migration includes:
- `users.personal_team_id UUID REFERENCES teams(id)`
- unique partial index on `users.personal_team_id`
- `runs.model TEXT`

- [x] **Step 0.2: Provision personal team on OAuth callback (idempotent + transactional)**

Handler behavior:
- Ensure a user has one personal team on login.
- Reuse existing `personal_team_id` when present.
- Personal team slug/name are deterministic from user identity.

- [x] **Step 0.3: Keep personal teams out of shared-team UX surfaces**

Backend behavior:
- Exclude personal teams from JWT `team_roles`.
- Exclude personal teams from `/api/me` team list.

- [x] **Step 0.4: Make refresh-session rotation atomic**

`RefreshSession` transaction now performs:
- refresh-token row lock (`SELECT ... FOR UPDATE`)
- revocation + replacement token issuance
- access token claim materialization and signing callback

- [x] **Step 0.5: Harden auth handlers to fail closed on DB read failures**

Behavior now distinguishes:
- auth failures (`401`)
- internal data-load failures (`500`)

- [x] **Step 0.6: Add auth regression tests for all new paths**

Coverage includes:
- personal-team provisioning and exclusion
- refresh failure behavior and non-burning-session guarantee
- role/admin/profile query failure paths returning `500`

---

### Task 0b: Required follow-up changes after Task 0

**Files:**
- Modify: `internal/auth/jwt.go`
- Modify: `internal/server/handlers/auth.go`
- Modify: `internal/server/handlers/auth_test.go`

- [x] **Step 0b.1: Replace string-based refresh error classification with typed/sentinel errors**

`HandleRefresh` currently distinguishes `401` vs `500` using string matching from `RefreshSession`; replace with typed errors (e.g. `ErrRefreshTokenInvalid`, `ErrRefreshTokenExpired`) to avoid brittle coupling.

- [x] **Step 0b.2: Add tests proving typed error mapping remains stable**

Add targeted tests for invalid/expired/revoked refresh paths and internal DB failures to ensure status-code mapping cannot regress silently.

- [x] **Step 0b.3: Verify auth package + handlers package**

```bash
go test ./internal/auth/... ./internal/server/handlers/... -count=1
```

---

### Task 1: Migration — add `model` column to `runs`

**Files:**
- Create: `internal/db/migrations/012_personal_space_and_run_model.up.sql`

- [x] **Step 1.1: Create migration**

```sql
ALTER TABLE runs ADD COLUMN IF NOT EXISTS model TEXT;
```

- [x] **Step 1.2: Build to verify migration embeds**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 1.3: Commit**

```bash
git add internal/db/migrations/012_personal_space_and_run_model.up.sql
git commit -m "db: add personal-team and run-model columns (migration 012)"
```

---

### Task 2: Run model + handler — accept and store `model` param

**Files:**
- Modify: `internal/model/run.go`
- Modify: `internal/server/handlers/runs.go`

- [x] **Step 2.1: Add `Model` field to `Run` struct**

In `internal/model/run.go`, add after `TriggeredBy`:

```go
Model        *string    `db:"model" json:"model,omitempty"`
```

- [x] **Step 2.2: Add `Model` to `createRunRequest`**

In `internal/server/handlers/runs.go`, find the `createRunRequest` struct (around line 35) and add:

```go
type createRunRequest struct {
	WorkflowID string         `json:"workflow_id"`
	Parameters map[string]any `json:"parameters"`
	Model      string         `json:"model,omitempty"`
}
```

- [x] **Step 2.3: Store `model` in the INSERT**

In the `CreateRun` handler, modify the INSERT statement to include `model`. Find the current INSERT (around line 95):

```go
// BEFORE:
_, err = h.db.ExecContext(r.Context(),
    `INSERT INTO runs (id, team_id, workflow_id, workflow_title, parameters, status, temporal_id, triggered_by)
     VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
    runID, teamID, tmpl.ID, tmpl.Title, model.JSONMap(params), model.RunStatusPending, temporalID, claims.UserID,
)
```

```go
// AFTER:
_, err = h.db.ExecContext(r.Context(),
    `INSERT INTO runs (id, team_id, workflow_id, workflow_title, parameters, status, temporal_id, triggered_by, model)
     VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''))`,
    runID, teamID, tmpl.ID, tmpl.Title, model.JSONMap(params), model.RunStatusPending, temporalID, claims.UserID, req.Model,
)
```

- [x] **Step 2.4: Build to verify**

```bash
go build ./...
```

- [ ] **Step 2.5: Commit**

```bash
git add internal/model/run.go internal/server/handlers/runs.go
git commit -m "feat: accept model param on run creation"
```

---

### Task 3: Model flow — DAGInput → StepInput → RunOpts → bridge

**Files:**
- Modify: `internal/workflow/dag.go`
- Modify: `internal/workflow/step.go`
- Modify: `internal/agent/runner.go`
- Modify: `internal/agent/claudecode.go`
- Modify: `internal/activity/execute.go`

- [x] **Step 3.1: Add `ModelOverride` to `DAGInput`**

In `internal/workflow/dag.go`, find the `DAGInput` struct (around line 26) and add:

```go
type DAGInput struct {
	RunID              string            `json:"run_id"`
	TeamID             string            `json:"team_id"`
	WorkflowTemplateID string            `json:"workflow_template_id,omitempty"`
	WorkflowDef        model.WorkflowDef `json:"workflow_def"`
	Parameters         map[string]any    `json:"parameters"`
	ModelOverride      string            `json:"model_override,omitempty"`
}
```

- [x] **Step 3.2: Pass `ModelOverride` into `StepInput`**

In `internal/workflow/step.go`, find the `StepInput` struct (around line 14) and add:

```go
type StepInput struct {
	RunID              string           `json:"run_id"`
	StepRunID          string           `json:"step_run_id"`
	TeamID             string           `json:"team_id"`
	WorkflowTemplateID string           `json:"workflow_template_id,omitempty"`
	StepDef            model.StepDef    `json:"step_def"`
	ResolvedOpts       ResolvedStepOpts `json:"resolved_opts"`
	SandboxID          string           `json:"sandbox_id"`
	ModelOverride      string           `json:"model_override,omitempty"`
}
```

In `internal/workflow/dag.go`, find where `StepInput` is constructed (around line 419–427 in the step execution goroutine). Add `ModelOverride: input.ModelOverride` to the construction. Search for `StepInput{` inside the step execution code to find the exact location — there will be at least one for single-step and one for fan-out. Add `ModelOverride` to all construction sites.

- [x] **Step 3.3: Add `Model` to `RunOpts`**

In `internal/agent/runner.go`, add the field:

```go
type RunOpts struct {
	Prompt         string
	WorkDir        string
	MaxTurns       int
	Model          string
	Environment    map[string]string
	EvalPluginDirs []string
}
```

- [x] **Step 3.4: Add `Model` to `bridgeRequest` and pass it**

In `internal/agent/claudecode.go`, find the `bridgeRequest` struct (around line 147) and add:

```go
type bridgeRequest struct {
	Version    int               `json:"version"`
	PromptFile string            `json:"prompt_file"`
	WorkDir    string            `json:"work_dir"`
	MaxTurns   int               `json:"max_turns"`
	Model      string            `json:"model,omitempty"`
	MCP        bridgeMCPRequest  `json:"mcp,omitempty"`
	PluginDirs []string          `json:"plugin_dirs,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
}
```

In the same file, find where `bridgeRequest` is constructed in the `Run()` method and add:

```go
Model:      opts.Model,
```

- [x] **Step 3.5: Pass model from StepInput to RunOpts in execute.go**

In `internal/activity/execute.go`, find the `runner.Run()` call (around line 201):

```go
// BEFORE:
events, err := runner.Run(ctx, input.SandboxID, agent.RunOpts{
    Prompt:         prompt,
    WorkDir:        workDir,
    MaxTurns:       stepInput.ResolvedOpts.MaxTurns,
    EvalPluginDirs: input.EvalPluginDirs,
})
```

```go
// AFTER:
events, err := runner.Run(ctx, input.SandboxID, agent.RunOpts{
    Prompt:         prompt,
    WorkDir:        workDir,
    MaxTurns:       stepInput.ResolvedOpts.MaxTurns,
    Model:          stepInput.ModelOverride,
    EvalPluginDirs: input.EvalPluginDirs,
})
```

- [x] **Step 3.6: Pass model from handler into DAGInput**

In `internal/server/handlers/runs.go`, find where `DAGInput` is constructed in `CreateRun` (around line 107). The run's model needs to flow into DAGInput. Find the struct literal:

```go
// Add ModelOverride to the DAGInput construction:
ModelOverride: req.Model,
```

- [x] **Step 3.7: Build to verify**

```bash
go build ./...
```

- [ ] **Step 3.8: Commit**

```bash
git add internal/workflow/dag.go internal/workflow/step.go internal/agent/runner.go internal/agent/claudecode.go internal/activity/execute.go internal/server/handlers/runs.go
git commit -m "feat: flow model override from run creation through to agent runner"
```

---

### Task 4: bridge.js — pass `--model` flag

**Files:**
- Modify: `docker/bridge.js`

- [x] **Step 4.1: Add `--model` to CLI args**

In `docker/bridge.js`, find the args array construction (around line 215–224). Add model support after the `--max-turns` line:

```javascript
  const args = [
    "-p",
    prompt,
    "--output-format",
    "stream-json",
    "--verbose",
    "--dangerously-skip-permissions",
    "--max-turns",
    String(request.max_turns),
  ];

  // Model override (run-level)
  if (typeof request.model === "string" && request.model !== "") {
    args.push("--model", request.model);
  }

  if (Array.isArray(request.plugin_dirs)) {
```

- [ ] **Step 4.2: Commit**

```bash
git add docker/bridge.js
git commit -m "feat: bridge.js passes --model flag to claude CLI when set"
```

---

### Task 5: `created_by=me` filter on ListRuns

**Files:**
- Modify: `internal/server/handlers/runs.go`

- [x] **Step 5.1: Write the failing test**

Find or create the runs handler test file. Add a test that calls `GET /api/runs?created_by=me` and verifies only runs created by the requesting user are returned. If no test file exists for runs handlers, create `internal/server/handlers/runs_test.go`.

The test should:
1. Insert two runs — one with `triggered_by = user-A`, one with `triggered_by = user-B`
2. Call `GET /api/runs?created_by=me` with JWT claims for `user-A`
3. Assert only the first run is returned

- [x] **Step 5.2: Modify ListRuns handler**

In `internal/server/handlers/runs.go`, find the `ListRuns` handler (around line 131). Replace the fixed query with a dynamic one:

```go
func (h *RunHandlers) ListRuns(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	teamID := claims.TeamID

	query := `SELECT * FROM runs WHERE team_id = $1`
	args := []any{teamID}

	if r.URL.Query().Get("created_by") == "me" {
		query += ` AND triggered_by = $2`
		args = append(args, claims.UserID)
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	query += ` ORDER BY created_at DESC LIMIT ` + strconv.Itoa(limit)

	var runs []model.Run
	if err := h.db.SelectContext(r.Context(), &runs, query, args...); err != nil {
		slog.Error("list runs", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"items": runs, "total": len(runs)})
}
```

Add `"strconv"` to the import block if not already present.

- [x] **Step 5.3: Run tests**

```bash
go test ./internal/server/handlers/... -count=1
```

- [ ] **Step 5.4: Commit**

```bash
git add internal/server/handlers/runs.go internal/server/handlers/runs_test.go
git commit -m "feat: add created_by=me and limit query params to ListRuns"
```

---

### Task 6: Hidden flag for builtin templates

**Files:**
- Modify: `internal/model/workflow.go`
- Modify: `internal/template/builtin.go`
- Modify: `internal/template/builtin_test.go`

- [x] **Step 6.1: Add `Hidden` to `WorkflowDef`**

In `internal/model/workflow.go`, find the `WorkflowDef` struct and add:

```go
Hidden      bool          `yaml:"hidden,omitempty" json:"hidden,omitempty"`
```

- [x] **Step 6.2: Filter hidden templates from `List()`**

In `internal/template/builtin.go`, modify the `List()` method:

```go
func (b *BuiltinProvider) List(_ context.Context, _ string) ([]*model.WorkflowTemplate, error) {
	var out []*model.WorkflowTemplate
	for _, t := range b.templates {
		if !t.hidden {
			out = append(out, t.tmpl)
		}
	}
	return out, nil
}
```

This requires changing the internal storage. Modify the `builtinEntry` or equivalent internal struct to track the hidden flag. In `NewBuiltinProvider()`, after parsing the YAML into `WorkflowDef`, check `def.Hidden` and store it alongside the template.

The simplest approach: add a `hidden` field to the slice elements. Change `b.templates` from `[]*model.WorkflowTemplate` to a struct:

```go
type builtinEntry struct {
	tmpl   *model.WorkflowTemplate
	hidden bool
}
```

Update `NewBuiltinProvider()` to store `builtinEntry` values, `List()` to filter by hidden, and `Get()` to search the new type (Get still returns hidden templates — they just don't appear in the list).

- [x] **Step 6.3: Update test — hidden template should not appear in List**

In `internal/template/builtin_test.go`, the count assertion will need updating after `quick-run.yaml` is added (Task 7). For now, verify the hidden filtering logic works by checking that the list count stays at 15 (the `quick-run` template will be hidden, so the visible count stays the same as before).

- [x] **Step 6.4: Build and run tests**

```bash
go build ./... && go test ./internal/template/... -count=1
```

- [ ] **Step 6.5: Commit**

```bash
git add internal/model/workflow.go internal/template/builtin.go internal/template/builtin_test.go
git commit -m "feat: add hidden flag to builtin workflows, filter from List()"
```

---

### Task 7: quick-run builtin workflow

**Files:**
- Create: `internal/template/workflows/quick-run.yaml`
- Modify: `internal/template/builtin_test.go`

- [x] **Step 7.1: Write failing test**

In `internal/template/builtin_test.go`, add:

```go
func TestQuickRunWorkflowTemplate_Parses(t *testing.T) {
	p, err := NewBuiltinProvider()
	require.NoError(t, err)

	tmpl, err := p.Get(context.Background(), "", "quick-run")
	require.NoError(t, err)
	assert.Equal(t, "Quick Run", tmpl.Title)

	var def model.WorkflowDef
	require.NoError(t, model.ParseWorkflowYAML([]byte(tmpl.YAMLBody), &def))

	assert.True(t, def.Hidden, "quick-run should be hidden")
	require.Len(t, def.Steps, 1)
	assert.Equal(t, "execute", def.Steps[0].ID)
	assert.NotNil(t, def.Steps[0].Execution)

	// Required params
	paramNames := make([]string, len(def.Parameters))
	for i, p := range def.Parameters {
		paramNames[i] = p.Name
	}
	assert.Contains(t, paramNames, "prompt")
	assert.Contains(t, paramNames, "repo_url")
}

func TestQuickRunWorkflowTemplate_HiddenFromList(t *testing.T) {
	p, err := NewBuiltinProvider()
	require.NoError(t, err)

	templates, err := p.List(context.Background(), "")
	require.NoError(t, err)

	for _, tmpl := range templates {
		assert.NotEqual(t, "quick-run", tmpl.Slug, "quick-run should not appear in List()")
	}
}
```

- [ ] **Step 7.2: Run test to verify it fails**

```bash
go test ./internal/template/... -run "TestQuickRun" -v -count=1
```

Expected: FAIL — template not found.

- [x] **Step 7.3: Create the workflow YAML**

Create `internal/template/workflows/quick-run.yaml`:

```yaml
version: 1
id: quick-run
title: Quick Run
description: >
  Single-step ad-hoc task execution. Type a prompt, point at a repo, and run.
  Used by the Home page for individual developer task delegation.
hidden: true
tags:
  - quick-run
  - ad-hoc

parameters:
  - name: prompt
    type: string
    required: true
    description: "What you want the agent to do"

  - name: repo_url
    type: string
    required: true
    description: "Repository URL, e.g. https://github.com/org/repo"

  - name: branch
    type: string
    required: false
    description: "Branch to work on (default: main)"

steps:
  - id: execute
    title: Execute task
    mode: transform
    repositories:
      - url: "{{ .Params.repo_url }}"
        ref: "{{ if .Params.branch }}{{ .Params.branch }}{{ end }}"
        create_branch: agent/quick-run
    execution:
      agent: claude-code
      credentials:
        - GITHUB_TOKEN
      prompt: "{{ .Params.prompt }}"
```

> **Design decision:** There is no `create_pr` parameter. Quick-run always creates a branch (`agent/quick-run`) and the agent is expected to open a PR if its changes are of sufficient quality. This matches the Stripe Minions model — PR creation is the default outcome; the agent self-assesses and skips it only when the work isn't ready. The Home page UI has no "Open PR" checkbox.

- [x] **Step 7.4: Run tests**

```bash
go test ./internal/template/... -run "TestQuickRun" -v -count=1
```

Expected: PASS.

- [x] **Step 7.5: Run full template + build**

```bash
go test ./internal/template/... -count=1 && go build ./...
```

- [ ] **Step 7.6: Commit**

```bash
git add internal/template/workflows/quick-run.yaml internal/template/builtin_test.go
git commit -m "feat: add quick-run builtin workflow (hidden, single-step passthrough)"
```

---

### Task 8: Full test + lint for PR 1

- [x] **Step 8.1: Run all tests**

```bash
go test ./... -count=1
```

- [x] **Step 8.2: Lint**

```bash
make lint
```

- [x] **Step 8.3: Build**

```bash
go build ./...
```

---

## PR 2 — Frontend: Home page, nav, model dropdown, retry, log search

### Task 9: API client + types updates

**Files:**
- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/client.ts`

- [ ] **Step 9.1: Add `model` to `Run` interface and `CreateRunResponse` type**

In `web/src/api/types.ts`, add to the `Run` interface:

```typescript
model?: string
```

Add a new type for the create response (the backend returns `{id, temporal_id}`, not a full `Run`):

```typescript
export interface CreateRunResponse {
  id: string
  temporal_id: string
}
```

- [ ] **Step 9.2: Update `createRun` to accept `model`, add `listMyRuns`**

In `web/src/api/client.ts`, update `createRun` to use the correct return type:

```typescript
createRun: (workflowId: string, parameters: Record<string, unknown>, model?: string) =>
  post<CreateRunResponse>('/runs', { workflow_id: workflowId, parameters, ...(model ? { model } : {}) }),
```

Update the import to include `CreateRunResponse`.

Add a new method:

```typescript
listMyRuns: (limit = 10) =>
  get<ListResponse<Run>>(`/runs?created_by=me&limit=${limit}`),
```

> **Note:** Callers that navigate after `createRun` use only `response.id` for the redirect (`/runs/${response.id}`). Retry reads `run.parameters`, `run.workflow_id`, and `run.model` from the full `Run` objects returned by `listMyRuns` or `getRun`, not from the create response.

- [ ] **Step 9.3: Commit**

```bash
git add web/src/api/types.ts web/src/api/client.ts
git commit -m "feat: update API client for model param and created_by filter"
```

---

### Task 10: ModelSelect component

**Files:**
- Create: `web/src/components/ModelSelect.tsx`

- [ ] **Step 10.1: Create the component**

```tsx
const MODELS = [
  { value: '', label: 'Default' },
  { value: 'claude-opus-4-6', label: 'Opus 4.6' },
  { value: 'claude-sonnet-4-6', label: 'Sonnet 4.6' },
  { value: 'claude-haiku-4-5', label: 'Haiku 4.5' },
] as const

const STORAGE_KEY = 'fleetlift-preferred-model'

export function ModelSelect({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <select
      value={value}
      onChange={(e) => {
        onChange(e.target.value)
        localStorage.setItem(STORAGE_KEY, e.target.value)
      }}
      className="rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-200"
    >
      {MODELS.map((m) => (
        <option key={m.value} value={m.value}>{m.label}</option>
      ))}
    </select>
  )
}

export function getPreferredModel(): string {
  return localStorage.getItem(STORAGE_KEY) ?? ''
}
```

- [ ] **Step 10.2: Commit**

```bash
git add web/src/components/ModelSelect.tsx
git commit -m "feat: add ModelSelect dropdown component"
```

---

### Task 11: Home page

**Files:**
- Create: `web/src/pages/HomePage.tsx`

- [ ] **Step 11.1: Create HomePage**

Build the page with these zones:
1. **Top zone:** prompt textarea (4-6 rows), repo URL input, branch input (default "main"), model dropdown (ModelSelect), "✦ Improve" button (disabled until Phase 2), "Run →" button.
2. **Bottom zone:** template grid fetched from `api.listWorkflows()`, rendered as clickable cards that navigate to `/workflows/:id`.
3. **Recent tasks:** `api.listMyRuns(10)` displayed as a list with StatusBadge, title (workflow_title), elapsed time, Retry button.

Key implementation details:
- "Run →" calls `api.createRun('quick-run', { prompt, repo_url, branch }, model)` and navigates to `/runs/:id` on success.
- Retry reads `run.parameters` and `run.workflow_id`, calls `api.createRun(run.workflow_id, run.parameters, run.model)`.
- Use `useQuery` for workflows and runs, `useMutation` for createRun.
- Template grid cards show icon, title, description, and tags — reuse the category/color logic from `workflowCategory()` in `lib/workflow-colors.ts`.

- [ ] **Step 11.2: Commit**

```bash
git add web/src/pages/HomePage.tsx
git commit -m "feat: add prompt-first Home page"
```

---

### Task 12: Nav update + route change

**Files:**
- Modify: `web/src/components/Layout.tsx`
- Modify: `web/src/App.tsx`

- [ ] **Step 12.1: Add Home to nav**

In `web/src/components/Layout.tsx`, add Home as the first nav item. Import `Home` from `lucide-react`:

```typescript
const NAV_ITEMS = [
  { href: '/',          label: 'Home',      icon: Home },
  { href: '/runs',      label: 'Runs',      icon: Activity },
  { href: '/workflows', label: 'Workflows', icon: LayoutTemplate },
  // ... rest unchanged
]
```

- [ ] **Step 12.2: Replace root redirect with HomePage**

In `web/src/App.tsx`, replace the `/` → `/runs` redirect with a route to `HomePage`:

```tsx
import { HomePage } from '@/pages/HomePage'

// In routes:
<Route path="/" element={<HomePage />} />
```

Remove the existing redirect: `<Route path="/" element={<Navigate to="/runs" replace />} />`.

- [ ] **Step 12.3: Commit**

```bash
git add web/src/components/Layout.tsx web/src/App.tsx
git commit -m "feat: add Home to nav, replace root redirect with HomePage"
```

---

### Task 13: Model dropdown on WorkflowDetail

**Files:**
- Modify: `web/src/pages/WorkflowDetail.tsx`

- [ ] **Step 13.1: Add ModelSelect to run form**

Import `ModelSelect` and `getPreferredModel`. Add state:

```tsx
const [model, setModel] = useState(getPreferredModel())
```

Add the dropdown below the params form, before the Run button. Update the `runMutation` to pass model:

```tsx
mutationFn: () => api.createRun(wf!.id, def ? coerceParams(def, params) : params, model),
```

- [ ] **Step 13.2: Commit**

```bash
git add web/src/pages/WorkflowDetail.tsx
git commit -m "feat: add model selection to WorkflowDetail run form"
```

---

### Task 14: Retry button on RunDetail

**Files:**
- Modify: `web/src/pages/RunDetail.tsx`

- [ ] **Step 14.1: Add Retry button**

In `RunDetail.tsx`, find the header action buttons area (near Cancel button). Add a Retry button that appears when the run status is `failed` or `completed`:

```tsx
const retryMutation = useMutation({
  mutationFn: () => api.createRun(run.workflow_id, run.parameters as Record<string, unknown>, run.model),
  onSuccess: (newRun) => navigate(`/runs/${newRun.id}`),
})
```

```tsx
{(run.status === 'failed' || run.status === 'completed') && (
  <Button
    variant="outline"
    size="sm"
    onClick={() => retryMutation.mutate()}
    disabled={retryMutation.isPending}
  >
    {retryMutation.isPending ? 'Retrying…' : 'Retry'}
  </Button>
)}
```

- [ ] **Step 14.2: Commit**

```bash
git add web/src/pages/RunDetail.tsx
git commit -m "feat: add Retry button to RunDetail for failed/completed runs"
```

---

### Task 15: LogStream search

**Files:**
- Modify: `web/src/components/LogStream.tsx`

- [ ] **Step 15.1: Add search input and filtering**

Add a search state and filter input above the log output:

```tsx
const [search, setSearch] = useState('')

// Filter logs
const filtered = search
  ? logs.filter((l) => l.content.toLowerCase().includes(search.toLowerCase()))
  : logs
```

Add a search input above the log stream container:

```tsx
<input
  type="text"
  value={search}
  onChange={(e) => setSearch(e.target.value)}
  placeholder="Search logs…"
  className="w-full rounded border border-zinc-700 bg-zinc-900 px-3 py-1.5 text-sm text-zinc-200 placeholder:text-zinc-500 mb-2"
/>
```

For highlighting, wrap matched text in a `<mark>` tag:

```tsx
function highlightMatch(text: string, query: string): React.ReactNode {
  if (!query) return text
  const idx = text.toLowerCase().indexOf(query.toLowerCase())
  if (idx === -1) return text
  return (
    <>
      {text.slice(0, idx)}
      <mark className="bg-yellow-500/30 text-yellow-200">{text.slice(idx, idx + query.length)}</mark>
      {text.slice(idx + query.length)}
    </>
  )
}
```

Render with `highlightMatch(log.content, search)` instead of `log.content`.

- [ ] **Step 15.2: Commit**

```bash
git add web/src/components/LogStream.tsx
git commit -m "feat: add search/filter to LogStream with match highlighting"
```

---

### Task 16: Frontend build verification for PR 2

- [ ] **Step 16.1: TypeScript check + build**

```bash
cd web && npx tsc --noEmit && npm run build
```

- [ ] **Step 16.2: Run frontend tests**

```bash
cd web && npm test -- --run
```

---

## PR 3 — Prompt Improvement

### Task 17: Backend handler

**Files:**
- Create: `internal/server/handlers/prompt.go`
- Create: `internal/server/handlers/prompt_test.go`
- Modify: server router file to register the route

- [ ] **Step 17.1: Write the handler**

Create `internal/server/handlers/prompt.go`:

```go
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type PromptHandlers struct{}

type improveRequest struct {
	Prompt string `json:"prompt"`
}

type scoreDetail struct {
	Rating string `json:"rating"`
	Reason string `json:"reason"`
}

type improveResponse struct {
	Improved string                 `json:"improved"`
	Scores   map[string]scoreDetail `json:"scores"`
	Summary  string                 `json:"summary"`
}

func (h *PromptHandlers) ImprovePrompt(w http.ResponseWriter, r *http.Request) {
	var req improveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Prompt == "" {
		http.Error(w, `{"error":"prompt is required"}`, http.StatusBadRequest)
		return
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		http.Error(w, `{"error":"ANTHROPIC_API_KEY not configured"}`, http.StatusServiceUnavailable)
		return
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	systemPrompt := `You are an AI prompt quality analyst. Given a developer's prompt for a coding agent, you must:
1. Analyze the prompt quality across four dimensions: clarity, context, structure, guidance
2. Rewrite it as a structured, high-quality prompt
3. Rate each dimension as "excellent", "good", or "poor" with a brief reason

Respond ONLY with valid JSON matching this schema:
{
  "improved": "the rewritten prompt text",
  "scores": {
    "clarity":   { "rating": "excellent|good|poor", "reason": "brief reason" },
    "context":   { "rating": "excellent|good|poor", "reason": "brief reason" },
    "structure": { "rating": "excellent|good|poor", "reason": "brief reason" },
    "guidance":  { "rating": "excellent|good|poor", "reason": "brief reason" }
  },
  "summary": "one sentence summarizing the improvement"
}`

	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 2048,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(
				fmt.Sprintf("Analyze and improve this prompt:\n\n%s", req.Prompt),
			)),
		},
	})
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"Claude API error: %s"}`, err.Error()), http.StatusBadGateway)
		return
	}

	// Extract text content from response
	var responseText string
	for _, block := range msg.Content {
		if block.Type == "text" {
			responseText = block.Text
			break
		}
	}

	// Parse the JSON response
	var result improveResponse
	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		// If Claude didn't return valid JSON, return raw text as the improved prompt
		result = improveResponse{
			Improved: responseText,
			Scores: map[string]scoreDetail{
				"clarity":   {Rating: "good", Reason: "Could not parse structured analysis"},
				"context":   {Rating: "good", Reason: "Could not parse structured analysis"},
				"structure": {Rating: "good", Reason: "Could not parse structured analysis"},
				"guidance":  {Rating: "good", Reason: "Could not parse structured analysis"},
			},
			Summary: "Prompt was improved but structured analysis was unavailable.",
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
```

- [ ] **Step 17.2: Register the route**

Find the server router setup (likely in `internal/server/router.go` or where handlers are registered). Add:

```go
promptH := &handlers.PromptHandlers{}
r.Post("/api/prompt/improve", promptH.ImprovePrompt)
```

Ensure the route is behind auth middleware (same as other `/api/` routes).

- [ ] **Step 17.3: Write handler test**

Create `internal/server/handlers/prompt_test.go` with a test that mocks the Claude API response (using httptest to intercept the API call or by making the client injectable). Test:
- Empty prompt → 400
- Valid prompt → 200 with correct JSON structure
- Missing API key → 503

- [ ] **Step 17.4: Add `anthropic-sdk-go` dependency**

```bash
go get github.com/anthropics/anthropic-sdk-go
```

- [ ] **Step 17.5: Build and test**

```bash
go build ./... && go test ./internal/server/handlers/... -count=1
```

- [ ] **Step 17.6: Commit**

```bash
git add internal/server/handlers/prompt.go internal/server/handlers/prompt_test.go go.mod go.sum
git commit -m "feat: add POST /api/prompt/improve endpoint"
```

---

### Task 18: Prompt improvement frontend

**Files:**
- Modify: `web/src/api/client.ts`
- Create: `web/src/components/PromptImproveModal.tsx`
- Modify: `web/src/pages/HomePage.tsx`

- [ ] **Step 18.1: Add API method**

In `web/src/api/client.ts`:

```typescript
improvePrompt: (prompt: string) =>
  post<{
    improved: string
    scores: Record<string, { rating: string; reason: string }>
    summary: string
  }>('/prompt/improve', { prompt }),
```

- [ ] **Step 18.2: Create PromptImproveModal**

Create `web/src/components/PromptImproveModal.tsx`:

Full-screen overlay with:
- Two columns: Original (left, "✗ ORIGINAL" red header) | Improved (right, "✓ IMPROVED" green header)
- Each column shows the prompt text + score badges below
- Score badge colours: `excellent` → green, `good` → yellow, `poor` → red
- Bottom bar: summary text + "Decline" button + "Use improved →" button
- Props: `original: string`, `onAccept: (improved: string) => void`, `onDecline: () => void`
- Uses `useMutation` to call `api.improvePrompt()` on mount
- Shows loading spinner while waiting

- [ ] **Step 18.3: Wire into HomePage**

In `HomePage.tsx`, enable the "✦ Improve" button. On click, open the `PromptImproveModal`. On accept, replace the textarea content.

- [ ] **Step 18.4: Build and verify**

```bash
cd web && npx tsc --noEmit && npm run build
```

- [ ] **Step 18.5: Commit**

```bash
git add web/src/api/client.ts web/src/components/PromptImproveModal.tsx web/src/pages/HomePage.tsx
git commit -m "feat: add prompt improvement modal with quality scores"
```

---

## PR 4 — Presets + Saved Repos

### Task 19: Database migrations

**Files:**
- Create: `internal/db/migrations/013_presets_repos.up.sql`

- [ ] **Step 19.1: Create migration**

```sql
CREATE TABLE prompt_presets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    scope       TEXT NOT NULL CHECK (scope IN ('personal', 'team')),
    title       TEXT NOT NULL,
    prompt      TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ON prompt_presets (team_id, scope, created_by);

CREATE TABLE user_repos (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id    UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    url        TEXT NOT NULL,
    label      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, url)
);
```

- [ ] **Step 19.2: Commit**

```bash
git add internal/db/migrations/013_presets_repos.up.sql
git commit -m "db: add prompt_presets and user_repos tables (migration 013)"
```

---

### Task 20: Backend handlers for presets and saved repos

**Files:**
- Create: `internal/server/handlers/presets.go`
- Create: `internal/server/handlers/saved_repos.go`
- Create: `internal/server/handlers/presets_test.go`
- Create: `internal/server/handlers/saved_repos_test.go`
- Modify: server router to register routes

- [ ] **Step 20.1: Presets handler**

Create `internal/server/handlers/presets.go` with CRUD operations:

- `ListPresets` — `SELECT * FROM prompt_presets WHERE team_id = $1 AND (scope = 'team' OR created_by = $2) ORDER BY created_at DESC`
- `CreatePreset` — INSERT with team_id from claims, created_by from claims, validate scope is "personal" or "team"
- `UpdatePreset` — UPDATE WHERE id = $1 AND created_by = $2 (creator only)
- `DeletePreset` — DELETE WHERE id = $1 AND created_by = $2 (creator only)

- [ ] **Step 20.2: Saved repos handler**

Create `internal/server/handlers/saved_repos.go`:

- `ListSavedRepos` — `SELECT * FROM user_repos WHERE user_id = $1 AND team_id = $2 ORDER BY created_at DESC`
- `CreateSavedRepo` — INSERT with user_id and team_id from claims, validate URL is non-empty
- `DeleteSavedRepo` — DELETE WHERE id = $1 AND user_id = $2

- [ ] **Step 20.3: Register routes**

```go
presetH := &handlers.PresetHandlers{DB: db}
r.Get("/api/presets", presetH.ListPresets)
r.Post("/api/presets", presetH.CreatePreset)
r.Put("/api/presets/{id}", presetH.UpdatePreset)
r.Delete("/api/presets/{id}", presetH.DeletePreset)

repoH := &handlers.SavedRepoHandlers{DB: db}
r.Get("/api/saved-repos", repoH.ListSavedRepos)
r.Post("/api/saved-repos", repoH.CreateSavedRepo)
r.Delete("/api/saved-repos/{id}", repoH.DeleteSavedRepo)
```

- [ ] **Step 20.4: Write tests**

Test each CRUD endpoint in `presets_test.go` and `saved_repos_test.go`. Test authorization (creator-only delete for presets, user-only for repos).

- [ ] **Step 20.5: Build and test**

```bash
go build ./... && go test ./internal/server/handlers/... -count=1
```

- [ ] **Step 20.6: Commit**

```bash
git add internal/server/handlers/presets.go internal/server/handlers/saved_repos.go internal/server/handlers/presets_test.go internal/server/handlers/saved_repos_test.go
git commit -m "feat: add CRUD handlers for presets and saved repos"
```

---

### Task 21: Frontend — presets sidebar + repo combobox

**Files:**
- Modify: `web/src/api/client.ts`
- Modify: `web/src/api/types.ts`
- Modify: `web/src/pages/HomePage.tsx`

- [ ] **Step 21.1: Add types and API methods**

In `types.ts`:

```typescript
export interface Preset {
  id: string
  team_id: string
  created_by: string
  scope: 'personal' | 'team'
  title: string
  prompt: string
  created_at: string
}

export interface SavedRepo {
  id: string
  url: string
  label?: string
  created_at: string
}
```

In `client.ts`:

```typescript
listPresets: () => get<ListResponse<Preset>>('/presets'),
createPreset: (title: string, prompt: string, scope: 'personal' | 'team') =>
  post<Preset>('/presets', { title, prompt, scope }),
deletePreset: (id: string) => del(`/presets/${id}`),

listSavedRepos: () => get<ListResponse<SavedRepo>>('/saved-repos'),
createSavedRepo: (url: string, label?: string) =>
  post<SavedRepo>('/saved-repos', { url, label }),
deleteSavedRepo: (id: string) => del(`/saved-repos/${id}`),
```

- [ ] **Step 21.2: Add presets sidebar to HomePage**

On the right side of the Home page (or below on narrow screens):
- Two sections: "My Presets" and "Team Presets"
- Clicking a preset fills the textarea
- "Save as preset" link below textarea — opens a small modal asking for title + scope

- [ ] **Step 21.3: Replace repo input with combobox**

Repo URL input becomes a combobox:
- Dropdown shows saved repos (label or URL)
- "Add new URL…" option at the bottom
- After a successful run, if the repo isn't saved, show a subtle toast: "Save this repo for quick access?" with a confirm button.

- [ ] **Step 21.4: Build and verify**

```bash
cd web && npx tsc --noEmit && npm run build
```

- [ ] **Step 21.5: Commit**

```bash
git add web/src/api/types.ts web/src/api/client.ts web/src/pages/HomePage.tsx
git commit -m "feat: add presets sidebar and repo combobox to Home page"
```

---

## PR 5 — Co-Author Attribution

### Task 22: Wire CreatedBy through the workflow

**Files:**
- Modify: `internal/workflow/dag.go`
- Modify: `internal/workflow/step.go`
- Modify: `internal/server/handlers/runs.go`

- [ ] **Step 22.1: Add `CreatedBy` to `DAGInput` and `StepInput`**

In `internal/workflow/dag.go`, add to `DAGInput`:

```go
CreatedBy string `json:"created_by,omitempty"`
```

In `internal/workflow/step.go`, add to `StepInput`:

```go
CreatedBy string `json:"created_by,omitempty"`
```

- [ ] **Step 22.2: Pass CreatedBy from handler through DAG to steps**

In `internal/server/handlers/runs.go`, add `CreatedBy: claims.UserID` to the `DAGInput` construction.

In `internal/workflow/dag.go`, find where `StepInput` is constructed and add `CreatedBy: input.CreatedBy`.

- [ ] **Step 22.3: Commit**

```bash
git add internal/workflow/dag.go internal/workflow/step.go internal/server/handlers/runs.go
git commit -m "feat: pass CreatedBy through DAGInput to StepInput"
```

---

### Task 23: Inject git author env vars

**Files:**
- Modify: `internal/activity/execute.go`
- Create: `internal/activity/execute_coauthor_test.go`

- [ ] **Step 23.1: Write failing test**

Create `internal/activity/execute_coauthor_test.go`:

```go
package activity

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestLookupUserGitIdentity_NotFound(t *testing.T) {
	// When DB returns no rows, should return empty strings, no error
	name, email := lookupUserGitIdentity(nil, "nonexistent-user-id")
	assert.Empty(t, name)
	assert.Empty(t, email)
}
```

- [ ] **Step 23.2: Implement lookupUserGitIdentity**

In `internal/activity/execute.go`, add:

```go
func lookupUserGitIdentity(db *sqlx.DB, userID string) (name, email string) {
	if db == nil || userID == "" {
		return "", ""
	}
	var n sql.NullString
	var e sql.NullString
	err := db.QueryRow(
		`SELECT name, COALESCE(email, '') FROM users WHERE id = $1`, userID,
	).Scan(&n, &e)
	if err != nil {
		return "", ""
	}
	if e.String == "" && n.String != "" {
		// GitHub noreply fallback: provider_id is the GitHub user ID
		var providerID string
		_ = db.QueryRow(`SELECT provider_id FROM users WHERE id = $1`, userID).Scan(&providerID)
		if providerID != "" {
			e.String = providerID + "+noreply@users.noreply.github.com"
		}
	}
	return n.String, e.String
}
```

> **Schema reference:** `internal/model/user.go` — columns are `name` (string), `email` (*string, nullable), `provider_id` (string, GitHub user ID).

- [ ] **Step 23.3: Inject env vars before agent execution**

In the `ExecuteStep` function, before the `runner.Run()` call, add:

```go
// Co-author: inject triggering user's git identity into sandbox
if stepInput.CreatedBy != "" {
	gitName, gitEmail := lookupUserGitIdentity(a.DB, stepInput.CreatedBy)
	if gitName != "" {
		envVars["GIT_AUTHOR_NAME"] = gitName
		envVars["GIT_COMMITTER_NAME"] = gitName
	}
	if gitEmail != "" {
		envVars["GIT_AUTHOR_EMAIL"] = gitEmail
		envVars["GIT_COMMITTER_EMAIL"] = gitEmail
	}
}
```

Find where sandbox env vars are written (look for `Exec` calls that write to `/tmp/fleetlift-mcp-env.sh` or similar) and add the git author vars to that set. If env vars are passed through `RunOpts.Environment`, add them there instead.

- [ ] **Step 23.4: Run tests**

```bash
go test ./internal/activity/... -count=1
```

- [ ] **Step 23.5: Full test + lint**

```bash
go test ./... -count=1 && make lint
```

- [ ] **Step 23.6: Commit**

```bash
git add internal/activity/execute.go internal/activity/execute_coauthor_test.go
git commit -m "feat: inject triggering user's git identity as co-author in sandbox"
```

---

## Verification Checklist

After all PRs are merged:

- [ ] `go test ./... -count=1` — all pass
- [ ] `make lint` — no errors
- [ ] `go build ./...` — compiles
- [ ] `cd web && npx tsc --noEmit && npm run build` — frontend builds
- [ ] Manual: visit `/` → see Home page with prompt input, template grid, recent tasks
- [ ] Manual: type a prompt, select a repo, hit Run → navigates to run detail, agent executes
- [ ] Manual: click "✦ Improve" → modal shows with quality scores and improved prompt
- [ ] Manual: retry a failed run from RunDetail → new run created with same params
- [ ] Manual: search logs on RunDetail → matches highlighted, non-matches hidden
- [ ] Manual: select model from dropdown → run uses the selected model
- [ ] Manual: save/load presets → presets sidebar on Home works
- [ ] Manual: co-author → PR commits show triggering user as co-author
