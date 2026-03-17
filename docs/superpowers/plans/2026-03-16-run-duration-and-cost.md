# Run Duration & Cost Tracking Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Duration column to the runs list (I3) and end-to-end cost tracking from Claude agent output through to DB storage and UI display (I5).

**Architecture:** I3 is a pure frontend change using the existing `useLiveDuration` hook. I5 threads a `cost_usd` field from agent output → `StepOutput` → `CompleteStepRun` activity → `step_runs.cost_usd` column, then aggregates to `runs.total_cost_usd` when the run reaches a terminal state.

**Tech Stack:** Go (Temporal activities, PostgreSQL via sqlx), React 19 + TypeScript + Vite, TanStack Query

---

## Task 1: Run Duration Column (I3)

Frontend-only. No backend changes.

**Files:**
- Modify: `web/src/pages/RunList.tsx`
- Modify: `web/src/pages/RunList.test.tsx` (new if no test exists)

**Context:** `useLiveDuration(startTime, endTime)` already exists at `web/src/lib/use-live-duration.ts`. It ticks every second when `endTime` is absent (running runs) and returns a static string when `endTime` is present.

The hook can't be called inside `.map()` — React rules of hooks forbid conditional/loop calls. Extract each row into a sub-component.

- [ ] **Step 1: Create a `RunRow` sub-component inside `RunList.tsx`**

Replace the inline `<tr>` lambda with a named component so `useLiveDuration` can be called unconditionally:

```tsx
function RunRow({ run }: { run: Run }) {
  const duration = useLiveDuration(run.started_at, run.completed_at)
  return (
    <tr className="border-b last:border-0 hover:bg-muted/50">
      <td className="px-4 py-3">
        <Link to={`/runs/${run.id}`} className="font-medium hover:underline">
          {run.workflow_title}
        </Link>
      </td>
      <td className="px-4 py-3">
        <StatusBadge status={run.status} />
      </td>
      <td className="px-4 py-3 text-muted-foreground">
        {run.started_at ? new Date(run.started_at).toLocaleString() : '-'}
      </td>
      <td className="px-4 py-3 text-muted-foreground tabular-nums">
        {duration ?? '-'}
      </td>
      <td className="px-4 py-3 font-mono text-xs text-muted-foreground">
        {run.id.slice(0, 8)}
      </td>
    </tr>
  )
}
```

- [ ] **Step 2: Update the table header and row rendering**

In `RunListPage`, update `<thead>` to add the Duration column between Started and ID, and replace the inline `<tr>` with `<RunRow run={run} />`.

```tsx
<thead>
  <tr className="border-b text-left text-muted-foreground">
    <th className="px-4 py-3 font-medium">Workflow</th>
    <th className="px-4 py-3 font-medium">Status</th>
    <th className="px-4 py-3 font-medium">Started</th>
    <th className="px-4 py-3 font-medium">Duration</th>
    <th className="px-4 py-3 font-medium">ID</th>
  </tr>
</thead>
<tbody>
  {data?.items?.map((run) => <RunRow key={run.id} run={run} />)}
  {data?.items?.length === 0 && (
    <tr>
      <td colSpan={5} className="p-0">
        <EmptyState ... />
      </td>
    </tr>
  )}
</tbody>
```

Also add the `useLiveDuration` import at the top of the file.

- [ ] **Step 3: Check for existing RunList tests**

Run: `ls web/src/pages/RunList.test.*`

If no test file exists, create `web/src/pages/RunList.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RunListPage } from './RunList'
import { vi } from 'vitest'

vi.mock('@/api/client', () => ({
  api: {
    listRuns: vi.fn().mockResolvedValue({
      items: [
        {
          id: 'abc12345-0000-0000-0000-000000000000',
          workflow_title: 'Test Workflow',
          status: 'complete',
          started_at: '2026-03-16T10:00:00Z',
          completed_at: '2026-03-16T10:05:30Z',
          created_at: '2026-03-16T10:00:00Z',
        },
      ],
    }),
  },
}))

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  )
}

test('shows Duration column header', async () => {
  render(<RunListPage />, { wrapper })
  expect(await screen.findByText('Duration')).toBeInTheDocument()
})

test('shows formatted duration for completed run', async () => {
  render(<RunListPage />, { wrapper })
  // 5m 30s
  expect(await screen.findByText('5m 30s')).toBeInTheDocument()
})
```

- [ ] **Step 4: Run tests**

```bash
cd web && npx vitest run src/pages/RunList.test.tsx
```

Expected: PASS

- [ ] **Step 5: Build check**

```bash
cd web && npx tsc --noEmit
```

Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/RunList.tsx web/src/pages/RunList.test.tsx
git commit -m "feat(ui): add Duration column to runs list (I3)"
```

---

## Task 2: Cost Tracking (I5)

Multi-layer change. Sub-tasks 2a–2d can be implemented sequentially.

### Task 2a: DB Schema + Go Models

**Files:**
- Modify: `internal/db/schema.sql`
- Modify: `internal/model/step.go`
- Modify: `internal/model/run.go`

- [ ] **Step 1: Add columns to schema.sql**

After the `step_runs` CREATE TABLE block (after line ~101), add:

```sql
ALTER TABLE step_runs ADD COLUMN IF NOT EXISTS cost_usd NUMERIC(10,6);
ALTER TABLE runs      ADD COLUMN IF NOT EXISTS total_cost_usd NUMERIC(10,6);
```

Add these lines at the end of the schema file, in a clearly labelled section:

```sql
-- Incremental schema additions (run manually against existing databases)
ALTER TABLE step_runs ADD COLUMN IF NOT EXISTS cost_usd NUMERIC(10,6);
ALTER TABLE runs      ADD COLUMN IF NOT EXISTS total_cost_usd NUMERIC(10,6);
```

- [ ] **Step 2: Add `CostUSD` to `StepRun` model**

In `internal/model/step.go`, add after `CompletedAt`:

```go
CostUSD     *float64   `db:"cost_usd" json:"cost_usd,omitempty"`
```

- [ ] **Step 3: Add `TotalCostUSD` to `Run` model**

In `internal/model/run.go`, add after `ErrorMessage`:

```go
TotalCostUSD *float64  `db:"total_cost_usd" json:"total_cost_usd,omitempty"`
```

- [ ] **Step 4: Add `CostUSD` to `StepOutput`**

In `internal/model/step.go`, add to `StepOutput` struct after `Error`:

```go
CostUSD float64 `json:"cost_usd,omitempty"`
```

- [ ] **Step 5: Build check**

```bash
go build ./...
```

Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add internal/db/schema.sql internal/model/step.go internal/model/run.go
git commit -m "feat(model): add cost_usd to step_runs and total_cost_usd to runs (I5)"
```

---

### Task 2b: Extract Cost from Agent Output

**Files:**
- Modify: `internal/activity/execute.go`

**Context:** After the agent completes, `lastOutput` is the raw Claude Code `result` event map. The Claude Code CLI includes a `cost_usd` field (and possibly `cost` as an alias). Extract it in `ExecuteStep` before building the return value.

- [ ] **Step 1: Fix the stale test fixture in `claudecode_test.go`**

The existing test uses `"cost":0.05` but the real Claude Code result event field is `cost_usd`. Update `TestParseClaudeEvent_Result` in `internal/agent/claudecode_test.go`:

```go
func TestParseClaudeEvent_Result(t *testing.T) {
    line := `{"type":"result","subtype":"success","cost_usd":0.05,"session_id":"abc"}`
    ev := parseClaudeEvent(line)
    assert.Equal(t, "complete", ev.Type)
    assert.Equal(t, "result", ev.Output["type"])
    assert.Equal(t, 0.05, ev.Output["cost_usd"])
}
```

Run: `go test ./internal/agent/ -run TestParseClaudeEvent_Result -v`

Expected: PASS (no logic change, just fixture correction)

- [ ] **Step 2: Write a failing test for cost extraction**

Find or create `internal/activity/execute_test.go`. Add:

```go
func TestExtractCostFromOutput(t *testing.T) {
    tests := []struct {
        name     string
        raw      map[string]any
        wantCost float64
    }{
        {"cost_usd field", map[string]any{"cost_usd": 0.05, "result": "done"}, 0.05},
        {"no cost field", map[string]any{"result": "done"}, 0.0},
        {"zero cost", map[string]any{"cost_usd": 0.0, "result": "done"}, 0.0},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := extractCostUSD(tt.raw)
            if got != tt.wantCost {
                t.Errorf("extractCostUSD() = %v, want %v", got, tt.wantCost)
            }
        })
    }
}
```

Run: `go test ./internal/activity/ -run TestExtractCostFromOutput`

Expected: FAIL (function doesn't exist yet)

- [ ] **Step 3: Add `extractCostUSD` to `execute.go`**

Add near the bottom of `internal/activity/execute.go`:

```go
// extractCostUSD reads the cost from a Claude Code result event.
// Claude Code CLI emits cost_usd in the result event.
func extractCostUSD(raw map[string]any) float64 {
    if v, ok := raw["cost_usd"].(float64); ok {
        return v
    }
    return 0
}
```

- [ ] **Step 4: Use it in `ExecuteStep` return value**

In `ExecuteStep` in `execute.go`, find the final successful return:

```go
return &model.StepOutput{
    StepID: stepInput.StepDef.ID,
    Status: model.StepStatusComplete,
    Output: structured,
    Diff:   diff,
}, nil
```

Change to:

```go
return &model.StepOutput{
    StepID:  stepInput.StepDef.ID,
    Status:  model.StepStatusComplete,
    Output:  structured,
    Diff:    diff,
    CostUSD: extractCostUSD(lastOutput),
}, nil
```

- [ ] **Step 5: Run the test to verify it passes**

```bash
go test ./internal/activity/ -run TestExtractCostFromOutput -v
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agent/claudecode_test.go internal/activity/execute.go
git commit -m "feat(activity): extract cost_usd from agent result event (I5)"
```

---

### Task 2c: Store Cost in DB

**Files:**
- Modify: `internal/activity/status.go`
- Modify: `internal/workflow/step.go`
- Modify: `internal/workflow/step_test.go`
- Modify: `internal/workflow/dag_integration_test.go`

**Context:** `CompleteStepRun` activity currently has signature `(ctx, stepRunID, status, output, diff, errorMsg string)`. We add `costUSD float64` as a new parameter. `UpdateRunStatus` for terminal states will aggregate cost from step_runs into runs.total_cost_usd.

- [ ] **Step 1: Write a failing test for `CompleteStepRun` with cost**

In `internal/activity/status_test.go` (create if missing), add:

```go
func TestCompleteStepRunStoresCost(t *testing.T) {
    db := openTestDB(t) // assumes a helper exists or use testify/sqlmock
    a := &Activities{DB: db}
    // Insert a step_run row first
    stepRunID := insertTestStepRun(t, db)

    err := a.CompleteStepRun(context.Background(), stepRunID, "complete", nil, "", "", 0.042)
    require.NoError(t, err)

    var cost float64
    err = db.QueryRowContext(context.Background(), "SELECT cost_usd FROM step_runs WHERE id = $1", stepRunID).Scan(&cost)
    require.NoError(t, err)
    assert.InDelta(t, 0.042, cost, 0.0001)
}
```

Run: `go test ./internal/activity/ -run TestCompleteStepRunStoresCost`

Expected: FAIL (signature mismatch)

- [ ] **Step 2: Update `CompleteStepRun` signature and query**

In `internal/activity/status.go`, change:

```go
func (a *Activities) CompleteStepRun(ctx context.Context, stepRunID string, status string, output map[string]any, diff string, errorMsg string) error {
    now := time.Now()
    _, err := a.DB.ExecContext(ctx,
        `UPDATE step_runs
         SET status = $1,
             output = $2,
             diff = NULLIF($3, ''),
             error_message = NULLIF($4, ''),
             started_at = COALESCE(started_at, $5),
             completed_at = $6
         WHERE id = $7`,
        status, model.JSONMap(output), diff, errorMsg, now, now, stepRunID)
```

To:

```go
func (a *Activities) CompleteStepRun(ctx context.Context, stepRunID string, status string, output map[string]any, diff string, errorMsg string, costUSD float64) error {
    now := time.Now()
    _, err := a.DB.ExecContext(ctx,
        `UPDATE step_runs
         SET status = $1,
             output = $2,
             diff = NULLIF($3, ''),
             error_message = NULLIF($4, ''),
             started_at = COALESCE(started_at, $5),
             completed_at = $6,
             cost_usd = NULLIF($8, 0)
         WHERE id = $7`,
        status, model.JSONMap(output), diff, errorMsg, now, now, stepRunID, costUSD)
```

- [ ] **Step 3: Aggregate cost to `runs` when a run reaches terminal state**

In `UpdateRunStatus` in `status.go`, update the terminal case to also set `total_cost_usd`:

```go
case isRunTerminal(model.RunStatus(status)):
    query = `UPDATE runs
             SET status = $1,
                 completed_at = $2,
                 error_message = NULLIF($3, ''),
                 total_cost_usd = (
                     SELECT COALESCE(SUM(cost_usd), 0)
                     FROM step_runs
                     WHERE run_id = $4
                 )
             WHERE id = $4`
    args = []any{status, now, errorMsg, runID}
```

- [ ] **Step 4: Update `finalizeStep` call in `step.go` to pass `CostUSD`**

In `internal/workflow/step.go`, the `CompleteStepRunActivity` is called with positional args. Find all three call sites of `finalizeStep` and the `finalizeStep` function itself.

The `finalizeStep` function calls `CompleteStepRunActivity` — update the activity call to pass `output.CostUSD`:

```go
if err := workflow.ExecuteActivity(
    workflow.WithActivityOptions(ctx, ao),
    CompleteStepRunActivity,
    stepRunID,
    string(output.Status),
    output.Output,
    output.Diff,
    output.Error,
    output.CostUSD,   // new
).Get(ctx, nil); err != nil {
```

- [ ] **Step 5: Fix mock activity signatures in test files**

Update `CompleteStepRun` mock method signature in both:

`internal/workflow/step_test.go`:
```go
func (m *stepMockActivities) CompleteStepRun(_ context.Context, stepRunID, status string, output map[string]any, diff, errorMsg string, costUSD float64) error {
    args := m.Called(stepRunID, status, output, diff, errorMsg, costUSD)
    return args.Error(0)
}
```

`internal/workflow/dag_integration_test.go`:
```go
func (m *dagMockActivities) CompleteStepRun(_ context.Context, stepRunID, status string, output map[string]any, diff, errorMsg string, costUSD float64) error {
    args := m.Called(stepRunID, status, output, diff, errorMsg, costUSD)
    return args.Error(0)
}
```

Update any `.On("CompleteStepRun", ...)` mock setup calls in those files to add a `mock.AnythingOfType("float64")` argument.

- [ ] **Step 6: Run all backend tests**

```bash
go test ./internal/...
```

Expected: PASS

- [ ] **Step 7: Build check**

```bash
go build ./...
```

Expected: no errors

- [ ] **Step 8: Commit**

```bash
git add internal/activity/status.go internal/workflow/step.go internal/workflow/step_test.go internal/workflow/dag_integration_test.go
git commit -m "feat(activity): store cost_usd per step, aggregate total_cost_usd on run completion (I5)"
```

---

### Task 2d: Frontend Cost Display

**Files:**
- Modify: `web/src/api/types.ts`
- Modify: `web/src/pages/RunList.tsx`
- Modify: `web/src/pages/RunDetail.tsx`
- Modify: `web/src/lib/format.ts` (add `formatCost`)

- [ ] **Step 1: Add cost fields to TypeScript types**

In `web/src/api/types.ts`, add to `Run` interface:
```ts
total_cost_usd?: number
```

Add to `StepRun` interface:
```ts
cost_usd?: number
```

- [ ] **Step 2: Add `formatCost` utility**

In `web/src/lib/format.ts`, add:

```ts
export function formatCost(usd?: number | null): string {
  if (usd == null || usd === 0) return '-'
  if (usd < 0.01) return '<$0.01'
  return `$${usd.toFixed(2)}`
}
```

- [ ] **Step 3: Write a test for `formatCost`**

Find the format test file (likely `web/src/lib/format.test.ts`). Add:

```ts
import { formatCost } from './format'

describe('formatCost', () => {
  it('returns - for null/undefined/zero', () => {
    expect(formatCost(undefined)).toBe('-')
    expect(formatCost(null)).toBe('-')
    expect(formatCost(0)).toBe('-')
  })
  it('returns <$0.01 for tiny amounts', () => {
    expect(formatCost(0.001)).toBe('<$0.01')
  })
  it('formats normal amounts to 2dp', () => {
    expect(formatCost(0.05)).toBe('$0.05')
    expect(formatCost(1.234)).toBe('$1.23')
  })
})
```

Run: `cd web && npx vitest run src/lib/format.test.ts`

Expected: PASS

- [ ] **Step 4: Add Cost column to `RunList.tsx`**

In `RunRow`, import `formatCost` and add a Cost cell after Duration:

```tsx
import { formatCost } from '@/lib/format'

// inside RunRow:
<td className="px-4 py-3 text-muted-foreground tabular-nums">
  {formatCost(run.total_cost_usd)}
</td>
```

Add `<th className="px-4 py-3 font-medium">Cost</th>` after Duration header.

Update `colSpan` on the empty-state row from 5 to 6.

- [ ] **Step 5: Add cost display to `RunDetail.tsx`**

In the run header/metadata area, add a cost line alongside the existing duration display. Find the section that shows live duration and add next to it:

```tsx
{run.total_cost_usd != null && run.total_cost_usd > 0 && (
  <span className="text-sm text-muted-foreground">
    Cost: {formatCost(run.total_cost_usd)}
  </span>
)}
```

In the step timeline/detail panel, if a step is shown individually, add per-step cost if available (check `StepTimeline.tsx` for where per-step data is rendered and add a small cost badge if `step.cost_usd` is non-null).

- [ ] **Step 6: Run frontend tests**

```bash
cd web && npx vitest run
```

Expected: PASS

- [ ] **Step 7: TypeScript build check**

```bash
cd web && npx tsc --noEmit
```

Expected: no errors

- [ ] **Step 8: Run backend tests**

```bash
go test ./...
```

Expected: PASS (no backend changes in this task, just a sanity check)

- [ ] **Step 9: Lint**

```bash
make lint
```

Expected: no errors

- [ ] **Step 10: Commit**

```bash
git add web/src/api/types.ts web/src/lib/format.ts web/src/pages/RunList.tsx web/src/pages/RunDetail.tsx web/src/components/StepTimeline.tsx
git commit -m "feat(ui): display cost in runs list and run detail (I5)"
```

---

## Pre-merge Checklist

- [ ] `make lint` passes
- [ ] `go test ./...` passes
- [ ] `cd web && npx vitest run` passes
- [ ] `cd web && npx tsc --noEmit` passes
- [ ] `go build ./...` passes
- [ ] New columns applied to local dev DB: `psql $DATABASE_URL -c "ALTER TABLE step_runs ADD COLUMN IF NOT EXISTS cost_usd NUMERIC(10,6); ALTER TABLE runs ADD COLUMN IF NOT EXISTS total_cost_usd NUMERIC(10,6);"`
