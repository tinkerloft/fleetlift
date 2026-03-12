# Platform v2 Completion Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Complete platform-v2 by wiring a DB-backed knowledge system, fixing outstanding gaps (GET /api/me, evalCondition, markdown export, integration tests), and updating all documentation.

**Architecture:** Knowledge items live in a new `knowledge_items` PostgreSQL table scoped per team + workflow template. Steps opt in to capture (agent writes `fleetlift-knowledge.json`) and/or enrichment (approved items injected into prompt). All other gaps are isolated fixes. Docs are a full archive-and-rewrite.

**Tech Stack:** Go 1.24, sqlx, Temporal SDK test suite, text/template (evalCondition), React 19 + TypeScript.

**Design doc:** `docs/plans/2026-03-12-platform-v2-completion-design.md`

---

## Progress

| Task | Status | Notes |
|------|--------|-------|
| 1 — DB schema: knowledge_items | ✅ | |
| 2 — model.KnowledgeItem v2 + KnowledgeDef | ✅ | |
| 3 — DB-backed KnowledgeStore | ✅ | |
| 4 — CaptureKnowledge activity | ✅ | |
| 5 — Wire capture into StepWorkflow | ✅ | |
| 6 — Wire enrichment into ExecuteStep | ✅ | |
| 7 — Knowledge API endpoints | ✅ | |
| 8 — Knowledge CLI commands | ✅ | |
| 9 — Knowledge Web UI page | ✅ | |
| 10 — GET /api/me | ✅ | |
| 11 — evalCondition implementation | ✅ | |
| 12 — Markdown export | ✅ | |
| 13 — Integration test skeleton | ✅ | |
| 14 — Archive v1 docs | ✅ | |
| 15 — README rewrite | ✅ | |
| 16 — WORKFLOW_REFERENCE.md | ✅ | |
| 17 — ARCHITECTURE.md | ✅ | |
| 18 — CLI_REFERENCE rewrite | ✅ | |
| 19 — TROUBLESHOOTING rewrite | ✅ | |

---

## Task 1: DB schema — knowledge_items table

**Files:**
- Modify: `internal/db/schema.sql`

**Step 1: Add table to schema**

Append to `internal/db/schema.sql`:

```sql
-- Knowledge items (captured from agent step runs, enriched into future prompts)
CREATE TABLE IF NOT EXISTS knowledge_items (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id              UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    workflow_template_id UUID REFERENCES workflow_templates(id) ON DELETE SET NULL,
    step_run_id          UUID REFERENCES step_runs(id) ON DELETE SET NULL,
    type                 TEXT NOT NULL,    -- pattern | correction | gotcha | context
    summary              TEXT NOT NULL,
    details              TEXT,
    source               TEXT NOT NULL,   -- auto_captured | manual
    tags                 TEXT[] NOT NULL DEFAULT '{}',
    confidence           FLOAT NOT NULL DEFAULT 1.0,
    status               TEXT NOT NULL DEFAULT 'pending',  -- pending | approved | rejected
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS knowledge_items_team_status ON knowledge_items(team_id, status);
CREATE INDEX IF NOT EXISTS knowledge_items_workflow ON knowledge_items(workflow_template_id, status);
```

**Step 2: Verify schema parses**

```bash
go build ./internal/db/...
```
Expected: no errors (schema is embedded via `//go:embed schema.sql`; the new SQL is appended idempotently with `IF NOT EXISTS`).

**Step 3: Commit**

```bash
git add internal/db/schema.sql
git commit -m "feat(knowledge): add knowledge_items table to schema"
```

---

## Task 2: Update model — KnowledgeItem v2 + KnowledgeDef in StepDef

**Files:**
- Modify: `internal/model/knowledge.go`
- Modify: `internal/model/workflow.go`

**Step 1: Rewrite `internal/model/knowledge.go`**

Replace the entire file with the v2 model (multi-tenant, DB-backed, drop file-specific fields):

```go
package model

import "time"

// KnowledgeType classifies the kind of knowledge item.
type KnowledgeType string

const (
	KnowledgeTypePattern    KnowledgeType = "pattern"
	KnowledgeTypeCorrection KnowledgeType = "correction"
	KnowledgeTypeGotcha     KnowledgeType = "gotcha"
	KnowledgeTypeContext     KnowledgeType = "context"
)

// KnowledgeSource describes how a knowledge item was created.
type KnowledgeSource string

const (
	KnowledgeSourceAutoCaptured KnowledgeSource = "auto_captured"
	KnowledgeSourceManual       KnowledgeSource = "manual"
)

// KnowledgeStatus represents the curation state of a knowledge item.
type KnowledgeStatus string

const (
	KnowledgeStatusPending  KnowledgeStatus = "pending"
	KnowledgeStatusApproved KnowledgeStatus = "approved"
	KnowledgeStatusRejected KnowledgeStatus = "rejected"
)

// KnowledgeItem is a reusable piece of knowledge extracted from a step run.
type KnowledgeItem struct {
	ID                 string          `db:"id" json:"id"`
	TeamID             string          `db:"team_id" json:"team_id"`
	WorkflowTemplateID string          `db:"workflow_template_id" json:"workflow_template_id,omitempty"`
	StepRunID          string          `db:"step_run_id" json:"step_run_id,omitempty"`
	Type               KnowledgeType   `db:"type" json:"type"`
	Summary            string          `db:"summary" json:"summary"`
	Details            string          `db:"details" json:"details,omitempty"`
	Source             KnowledgeSource `db:"source" json:"source"`
	Tags               []string        `db:"tags" json:"tags,omitempty"`
	Confidence         float64         `db:"confidence" json:"confidence"`
	Status             KnowledgeStatus `db:"status" json:"status"`
	CreatedAt          time.Time       `db:"created_at" json:"created_at"`
}

// KnowledgeDef is the optional knowledge config block in a StepDef YAML.
type KnowledgeDef struct {
	Capture  bool     `yaml:"capture,omitempty"`   // instruct agent to write fleetlift-knowledge.json
	Enrich   bool     `yaml:"enrich,omitempty"`    // inject approved items into step prompt
	MaxItems int      `yaml:"max_items,omitempty"` // cap on injected items (default 10)
	Tags     []string `yaml:"tags,omitempty"`      // filter enrichment by tags
}
```

**Step 2: Add `Knowledge` field to `StepDef` in `internal/model/workflow.go`**

In `StepDef`, after `Sandbox *SandboxSpec`:

```go
Knowledge *KnowledgeDef `yaml:"knowledge,omitempty"`
```

**Step 3: Build**

```bash
go build ./internal/model/...
```
Expected: no errors. `KnowledgeConfig` and `KnowledgeOrigin` are gone; check nothing else imported them.

```bash
grep -r "KnowledgeConfig\|KnowledgeOrigin" --include="*.go" .
```
Expected: no matches outside `internal/model/knowledge.go`.

**Step 4: Commit**

```bash
git add internal/model/knowledge.go internal/model/workflow.go
git commit -m "feat(knowledge): v2 model — DB-backed KnowledgeItem, KnowledgeDef in StepDef"
```

---

## Task 3: DB-backed KnowledgeStore

**Files:**
- Replace: `internal/knowledge/store.go`
- Replace: `internal/knowledge/store_test.go`

**Step 1: Write the failing test first**

Replace `internal/knowledge/store_test.go`:

```go
package knowledge_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestStore_SaveAndList(t *testing.T) {
	store := knowledge.NewMemoryStore() // in-memory store for unit tests

	item := model.KnowledgeItem{
		TeamID:  "team-1",
		Type:    model.KnowledgeTypePattern,
		Summary: "use context.WithTimeout for all DB calls",
		Source:  model.KnowledgeSourceAutoCaptured,
		Status:  model.KnowledgeStatusPending,
	}

	saved, err := store.Save(context.Background(), item)
	require.NoError(t, err)
	assert.NotEmpty(t, saved.ID)

	items, err := store.ListByTeam(context.Background(), "team-1", "")
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, saved.ID, items[0].ID)
}

func TestStore_UpdateStatus(t *testing.T) {
	store := knowledge.NewMemoryStore()

	item := model.KnowledgeItem{
		TeamID:  "team-1",
		Type:    model.KnowledgeTypePattern,
		Summary: "test item",
		Source:  model.KnowledgeSourceAutoCaptured,
		Status:  model.KnowledgeStatusPending,
	}
	saved, _ := store.Save(context.Background(), item)

	err := store.UpdateStatus(context.Background(), saved.ID, model.KnowledgeStatusApproved)
	require.NoError(t, err)

	items, _ := store.ListByTeam(context.Background(), "team-1", string(model.KnowledgeStatusApproved))
	assert.Len(t, items, 1)
}

func TestStore_ListApprovedByWorkflow(t *testing.T) {
	store := knowledge.NewMemoryStore()

	items := []model.KnowledgeItem{
		{TeamID: "team-1", WorkflowTemplateID: "wf-1", Type: model.KnowledgeTypePattern, Summary: "a", Source: model.KnowledgeSourceAutoCaptured, Status: model.KnowledgeStatusApproved},
		{TeamID: "team-1", WorkflowTemplateID: "wf-2", Type: model.KnowledgeTypePattern, Summary: "b", Source: model.KnowledgeSourceAutoCaptured, Status: model.KnowledgeStatusApproved},
	}
	for _, item := range items {
		_, _ = store.Save(context.Background(), item)
	}

	results, err := store.ListApprovedByWorkflow(context.Background(), "team-1", "wf-1", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "a", results[0].Summary)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/knowledge/... -v
```
Expected: FAIL — `NewMemoryStore` not defined.

**Step 3: Rewrite `internal/knowledge/store.go`**

```go
// Package knowledge provides storage for knowledge items.
package knowledge

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// Store is the interface for knowledge item persistence.
type Store interface {
	Save(ctx context.Context, item model.KnowledgeItem) (model.KnowledgeItem, error)
	ListByTeam(ctx context.Context, teamID, status string) ([]model.KnowledgeItem, error)
	ListApprovedByWorkflow(ctx context.Context, teamID, workflowTemplateID string, maxItems int) ([]model.KnowledgeItem, error)
	UpdateStatus(ctx context.Context, id string, status model.KnowledgeStatus) error
	Delete(ctx context.Context, id string) error
}

// DBStore is the production PostgreSQL-backed Store.
type DBStore struct {
	db *sqlx.DB
}

// NewDBStore creates a new DBStore.
func NewDBStore(db *sqlx.DB) *DBStore {
	return &DBStore{db: db}
}

func (s *DBStore) Save(ctx context.Context, item model.KnowledgeItem) (model.KnowledgeItem, error) {
	if item.ID == "" {
		item.ID = uuid.New().String()
	}
	item.CreatedAt = time.Now()
	if item.Status == "" {
		item.Status = model.KnowledgeStatusPending
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO knowledge_items (id, team_id, workflow_template_id, step_run_id, type, summary, details, source, tags, confidence, status, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		item.ID, item.TeamID, nullStr(item.WorkflowTemplateID), nullStr(item.StepRunID),
		string(item.Type), item.Summary, item.Details, string(item.Source),
		item.Tags, item.Confidence, string(item.Status), item.CreatedAt,
	)
	if err != nil {
		return item, fmt.Errorf("save knowledge item: %w", err)
	}
	return item, nil
}

func (s *DBStore) ListByTeam(ctx context.Context, teamID, status string) ([]model.KnowledgeItem, error) {
	var items []model.KnowledgeItem
	var err error
	if status != "" {
		err = s.db.SelectContext(ctx, &items,
			`SELECT * FROM knowledge_items WHERE team_id=$1 AND status=$2 ORDER BY created_at DESC`,
			teamID, status)
	} else {
		err = s.db.SelectContext(ctx, &items,
			`SELECT * FROM knowledge_items WHERE team_id=$1 ORDER BY created_at DESC`, teamID)
	}
	return items, err
}

func (s *DBStore) ListApprovedByWorkflow(ctx context.Context, teamID, workflowTemplateID string, maxItems int) ([]model.KnowledgeItem, error) {
	if maxItems <= 0 {
		maxItems = 10
	}
	var items []model.KnowledgeItem
	err := s.db.SelectContext(ctx, &items,
		`SELECT * FROM knowledge_items
		 WHERE team_id=$1 AND status='approved'
		   AND (workflow_template_id=$2 OR workflow_template_id IS NULL)
		 ORDER BY confidence DESC LIMIT $3`,
		teamID, workflowTemplateID, maxItems)
	return items, err
}

func (s *DBStore) UpdateStatus(ctx context.Context, id string, status model.KnowledgeStatus) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE knowledge_items SET status=$1 WHERE id=$2`, string(status), id)
	return err
}

func (s *DBStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM knowledge_items WHERE id=$1`, id)
	return err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// MemoryStore is an in-memory Store for unit tests.
type MemoryStore struct {
	mu    sync.Mutex
	items []model.KnowledgeItem
}

// NewMemoryStore creates a new in-memory Store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (s *MemoryStore) Save(_ context.Context, item model.KnowledgeItem) (model.KnowledgeItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item.ID == "" {
		item.ID = uuid.New().String()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}
	s.items = append(s.items, item)
	return item, nil
}

func (s *MemoryStore) ListByTeam(_ context.Context, teamID, status string) ([]model.KnowledgeItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.KnowledgeItem
	for _, item := range s.items {
		if item.TeamID != teamID {
			continue
		}
		if status != "" && string(item.Status) != status {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *MemoryStore) ListApprovedByWorkflow(_ context.Context, teamID, workflowTemplateID string, maxItems int) ([]model.KnowledgeItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.KnowledgeItem
	for _, item := range s.items {
		if item.TeamID != teamID {
			continue
		}
		if item.Status != model.KnowledgeStatusApproved {
			continue
		}
		if item.WorkflowTemplateID != "" && item.WorkflowTemplateID != workflowTemplateID {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	if maxItems > 0 && len(out) > maxItems {
		out = out[:maxItems]
	}
	return out, nil
}

func (s *MemoryStore) UpdateStatus(_ context.Context, id string, status model.KnowledgeStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.items {
		if item.ID == id {
			s.items[i].Status = status
			return nil
		}
	}
	return fmt.Errorf("item %s not found", id)
}

func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.items {
		if item.ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			return nil
		}
	}
	return nil
}

// FormatEnrichmentBlock formats approved knowledge items as a prompt context block.
func FormatEnrichmentBlock(items []model.KnowledgeItem) string {
	if len(items) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Knowledge Base\n\nThe following insights from previous runs may be relevant:\n\n")
	for _, item := range items {
		sb.WriteString(fmt.Sprintf("**[%s]** %s\n", item.Type, item.Summary))
		if item.Details != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", item.Details))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
```

**Step 4: Run tests**

```bash
go test ./internal/knowledge/... -v
```
Expected: all 3 tests PASS.

**Step 5: Build all**

```bash
go build ./...
```
Expected: no errors. (Old file-based store is gone; nothing else should be importing its unexported functions.)

**Step 6: Commit**

```bash
git add internal/knowledge/store.go internal/knowledge/store_test.go
git commit -m "feat(knowledge): replace file-based store with DB-backed + in-memory stores"
```

---

## Task 4: Implement CaptureKnowledge activity

**Files:**
- Modify: `internal/activity/knowledge.go`
- Modify: `internal/activity/activities.go` (add KnowledgeStore field)

**Step 1: Write failing test**

Create `internal/activity/knowledge_test.go`:

```go
package activity_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/activity"
	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

func TestCaptureKnowledge_ParsesFile(t *testing.T) {
	items := []map[string]any{
		{"type": "pattern", "summary": "use defer for cleanup", "confidence": 0.9},
	}
	data, _ := json.Marshal(items)

	sb := sandbox.NewMemoryClient(map[string][]byte{
		"fleetlift-knowledge.json": data,
	})
	store := knowledge.NewMemoryStore()
	acts := &activity.Activities{
		Sandbox:        sb,
		KnowledgeStore: store,
	}

	err := acts.CaptureKnowledge(context.Background(), activity.CaptureKnowledgeInput{
		SandboxID:          "sandbox-1",
		TeamID:             "team-1",
		WorkflowTemplateID: "wf-1",
		StepRunID:          "step-1",
	})
	require.NoError(t, err)

	saved, _ := store.ListByTeam(context.Background(), "team-1", "")
	assert.Len(t, saved, 1)
	assert.Equal(t, "use defer for cleanup", saved[0].Summary)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/activity/... -run TestCaptureKnowledge -v
```
Expected: FAIL — `KnowledgeStore` field and `CaptureKnowledgeInput` not defined.

**Step 3: Add `KnowledgeStore` to `Activities` struct**

In `internal/activity/activities.go`, add field:

```go
KnowledgeStore knowledge.Store
```

Add import: `"github.com/tinkerloft/fleetlift/internal/knowledge"`

**Step 4: Implement `CaptureKnowledgeInput` and `CaptureKnowledge` in `internal/activity/knowledge.go`**

```go
// Package activity contains Temporal activity implementations.
package activity

import (
	"context"
	"encoding/json"
	"fmt"

	"go.temporal.io/sdk/activity"

	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// CaptureKnowledgeInput is the input for the CaptureKnowledge activity.
type CaptureKnowledgeInput struct {
	SandboxID          string `json:"sandbox_id"`
	TeamID             string `json:"team_id"`
	WorkflowTemplateID string `json:"workflow_template_id,omitempty"`
	StepRunID          string `json:"step_run_id,omitempty"`
}

// KnowledgeActivities holds the knowledge store dependency.
// (kept for backwards compat with worker registration; real logic is on *Activities)
type KnowledgeActivities struct {
	Store knowledge.Store
}

// NewKnowledgeActivities is kept for backwards compatibility.
func NewKnowledgeActivities(store knowledge.Store) *KnowledgeActivities {
	return &KnowledgeActivities{Store: store}
}

// CaptureKnowledge reads fleetlift-knowledge.json from the sandbox and persists items to the DB.
func (a *Activities) CaptureKnowledge(ctx context.Context, input CaptureKnowledgeInput) error {
	logger := activity.GetLogger(ctx)

	if a.KnowledgeStore == nil {
		logger.Warn("KnowledgeStore not configured, skipping capture")
		return nil
	}

	// Read knowledge file from sandbox
	data, err := a.Sandbox.ReadFile(ctx, input.SandboxID, "fleetlift-knowledge.json")
	if err != nil {
		// Not an error if the agent chose not to write knowledge
		logger.Info("no fleetlift-knowledge.json found in sandbox", "sandbox_id", input.SandboxID)
		return nil
	}

	// Parse as array of knowledge item shapes
	type rawItem struct {
		Type       string   `json:"type"`
		Summary    string   `json:"summary"`
		Details    string   `json:"details"`
		Tags       []string `json:"tags"`
		Confidence float64  `json:"confidence"`
	}
	var raw []rawItem
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse fleetlift-knowledge.json: %w", err)
	}

	for _, r := range raw {
		if r.Summary == "" {
			continue
		}
		conf := r.Confidence
		if conf == 0 {
			conf = 1.0
		}
		item := model.KnowledgeItem{
			TeamID:             input.TeamID,
			WorkflowTemplateID: input.WorkflowTemplateID,
			StepRunID:          input.StepRunID,
			Type:               model.KnowledgeType(r.Type),
			Summary:            r.Summary,
			Details:            r.Details,
			Source:             model.KnowledgeSourceAutoCaptured,
			Tags:               r.Tags,
			Confidence:         conf,
			Status:             model.KnowledgeStatusPending,
		}
		if _, err := a.KnowledgeStore.Save(ctx, item); err != nil {
			logger.Error("failed to save knowledge item", "error", err)
		}
	}

	return nil
}
```

Note: `sandbox.Client` needs a `ReadFile(ctx, sandboxID, path string) ([]byte, error)` method. Check if it exists — if not, it was defined in Phase 7. Verify with:

```bash
grep -n "ReadFile" internal/sandbox/client.go
```

**Step 5: Add `MemoryClient` to sandbox package for tests**

Check if `sandbox.NewMemoryClient` exists:

```bash
grep -rn "MemoryClient\|NewMemoryClient" internal/sandbox/
```

If it does not exist, create `internal/sandbox/memory.go`:

```go
package sandbox

import (
	"context"
	"fmt"
	"io"
)

// MemoryClient is an in-memory sandbox client for tests.
type MemoryClient struct {
	files map[string][]byte
}

// NewMemoryClient creates a MemoryClient with preset file contents.
// keys are filenames (not paths), values are file contents.
func NewMemoryClient(files map[string][]byte) *MemoryClient {
	return &MemoryClient{files: files}
}

func (m *MemoryClient) Create(ctx context.Context, opts CreateOpts) (string, error) {
	return "test-sandbox", nil
}

func (m *MemoryClient) Exec(ctx context.Context, sandboxID, cmd, workdir string) (string, string, error) {
	return "", "", nil
}

func (m *MemoryClient) ExecStream(ctx context.Context, sandboxID, cmd, workdir string) (io.ReadCloser, error) {
	return io.NopCloser(nil), nil
}

func (m *MemoryClient) WriteFile(ctx context.Context, sandboxID, path string, data []byte) error {
	return nil
}

func (m *MemoryClient) ReadFile(ctx context.Context, sandboxID, path string) ([]byte, error) {
	// Check by basename
	for name, data := range m.files {
		if name == path || "/workspace/"+name == path || name == "fleetlift-knowledge.json" && path == "fleetlift-knowledge.json" {
			return data, nil
		}
	}
	return nil, fmt.Errorf("file not found: %s", path)
}

func (m *MemoryClient) Kill(ctx context.Context, sandboxID string) error {
	return nil
}
```

First check the actual `sandbox.Client` interface to match all methods:

```bash
cat internal/sandbox/client.go
```

Implement `MemoryClient` to satisfy exactly that interface.

**Step 6: Run tests**

```bash
go test ./internal/activity/... -run TestCaptureKnowledge -v
```
Expected: PASS.

**Step 7: Build**

```bash
go build ./...
```

**Step 8: Commit**

```bash
git add internal/activity/knowledge.go internal/activity/knowledge_test.go \
        internal/activity/activities.go internal/sandbox/memory.go
git commit -m "feat(knowledge): implement CaptureKnowledge activity with sandbox file reader"
```

---

## Task 5: Wire knowledge capture into StepWorkflow

**Files:**
- Modify: `internal/workflow/step.go`

**Step 1: Add CaptureKnowledgeActivity var**

In `internal/workflow/step.go`, add to the activity vars block:

```go
CaptureKnowledgeActivity = "CaptureKnowledge"
```

**Step 2: Add CaptureInput to StepInput**

Add fields to `StepInput` struct:

```go
TeamID             string `json:"team_id"`
WorkflowTemplateID string `json:"workflow_template_id,omitempty"`
StepRunID          string `json:"step_run_id"`
```

**Step 3: Call CaptureKnowledge after ExecuteStep**

In `StepWorkflow`, after the execution loop ends (before PR creation), add:

```go
// 6. Capture knowledge (if enabled for this step)
if input.StepDef.Knowledge != nil && input.StepDef.Knowledge.Capture {
    captureAO := workflow.ActivityOptions{StartToCloseTimeout: 2 * time.Minute}
    captureInput := activity.CaptureKnowledgeInput{
        SandboxID:          sandboxID,
        TeamID:             input.TeamID,
        WorkflowTemplateID: input.WorkflowTemplateID,
        StepRunID:          input.StepRunID,
    }
    _ = workflow.ExecuteActivity(
        workflow.WithActivityOptions(ctx, captureAO),
        CaptureKnowledgeActivity, captureInput,
    ).Get(ctx, nil)
}
```

Note: need to import `"github.com/tinkerloft/fleetlift/internal/activity"` for `CaptureKnowledgeInput` — but this creates a circular import (`activity` imports `workflow`, `workflow` imports `activity`).

**Resolution:** Move `CaptureKnowledgeInput` to `internal/workflow/step.go` itself (not in `activity`), and have the activity function accept it from there. OR define it in a shared package. Simplest: define `CaptureKnowledgeInput` in `internal/model/` (it's just a data struct):

Add to `internal/model/knowledge.go`:

```go
// CaptureKnowledgeInput is the input for the CaptureKnowledge Temporal activity.
type CaptureKnowledgeInput struct {
	SandboxID          string `json:"sandbox_id"`
	TeamID             string `json:"team_id"`
	WorkflowTemplateID string `json:"workflow_template_id,omitempty"`
	StepRunID          string `json:"step_run_id,omitempty"`
}
```

Then use `model.CaptureKnowledgeInput` in both `workflow/step.go` and `activity/knowledge.go`. Remove the separate `CaptureKnowledgeInput` from `activity/knowledge.go`.

**Step 4: Build**

```bash
go build ./...
```
Expected: no circular import errors.

**Step 5: Commit**

```bash
git add internal/workflow/step.go internal/model/knowledge.go internal/activity/knowledge.go
git commit -m "feat(knowledge): wire CaptureKnowledge into StepWorkflow post-execution"
```

---

## Task 6: Wire knowledge enrichment into ExecuteStep

**Files:**
- Modify: `internal/activity/execute.go`

**Step 1: Write failing test**

Add to `internal/activity/execute_knowledge_test.go`:

```go
package activity_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestFormatEnrichmentBlock_Empty(t *testing.T) {
	result := knowledge.FormatEnrichmentBlock(nil)
	assert.Empty(t, result)
}

func TestFormatEnrichmentBlock_WithItems(t *testing.T) {
	items := []model.KnowledgeItem{
		{Type: model.KnowledgeTypePattern, Summary: "use defer for cleanup"},
	}
	result := knowledge.FormatEnrichmentBlock(items)
	assert.Contains(t, result, "use defer for cleanup")
	assert.Contains(t, result, "Knowledge Base")
}
```

**Step 2: Run test**

```bash
go test ./internal/activity/... -run TestFormatEnrichmentBlock -v
```
Expected: PASS (FormatEnrichmentBlock already implemented in Task 3).

**Step 3: Add enrichment to `ExecuteStep`**

In `internal/activity/execute.go`, after the prompt is assembled and before `runner.Run(...)`:

```go
// Enrich prompt with approved knowledge items if configured
if input.StepInput.StepDef.Knowledge != nil && input.StepInput.StepDef.Knowledge.Enrich && a.KnowledgeStore != nil {
    maxItems := input.StepInput.StepDef.Knowledge.MaxItems
    if maxItems == 0 {
        maxItems = 10
    }
    knowledgeItems, err := a.KnowledgeStore.ListApprovedByWorkflow(ctx,
        input.StepInput.TeamID,
        input.StepInput.WorkflowTemplateID,
        maxItems,
    )
    if err == nil && len(knowledgeItems) > 0 {
        enrichBlock := knowledge.FormatEnrichmentBlock(knowledgeItems)
        prompt = enrichBlock + "\n\n" + prompt
    }
}
```

Add imports: `"github.com/tinkerloft/fleetlift/internal/knowledge"`

**Step 4: Also add knowledge capture prompt suffix if step has capture enabled**

After the enrichment block, add instruction to the prompt if capture is enabled:

```go
// Instruct agent to capture knowledge if configured
if input.StepInput.StepDef.Knowledge != nil && input.StepInput.StepDef.Knowledge.Capture {
    prompt += "\n\n## Knowledge Capture\n\nBefore exiting, write `fleetlift-knowledge.json` to the current directory with any insights you gained. Format:\n```json\n[\n  {\"type\": \"pattern|correction|gotcha|context\", \"summary\": \"brief insight\", \"details\": \"optional detail\", \"confidence\": 0.9}\n]\n```\nOnly include non-obvious insights worth sharing with future runs."
}
```

**Step 5: Build and test**

```bash
go build ./... && go test ./internal/activity/... -v
```
Expected: all pass.

**Step 6: Commit**

```bash
git add internal/activity/execute.go internal/activity/execute_knowledge_test.go
git commit -m "feat(knowledge): enrich step prompt with approved knowledge items"
```

---

## Task 7: Knowledge API endpoints

**Files:**
- Create: `internal/server/handlers/knowledge.go`
- Modify: `internal/server/router.go`
- Modify: `cmd/server/main.go`

**Step 1: Create `internal/server/handlers/knowledge.go`**

```go
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// KnowledgeHandler handles knowledge item CRUD endpoints.
type KnowledgeHandler struct {
	store knowledge.Store
}

// NewKnowledgeHandler creates a new KnowledgeHandler.
func NewKnowledgeHandler(store knowledge.Store) *KnowledgeHandler {
	return &KnowledgeHandler{store: store}
}

// List returns knowledge items for the user's team.
// Query params: status (pending|approved|rejected), workflow_id
func (h *KnowledgeHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	teamID := firstTeamID(claims)
	status := r.URL.Query().Get("status")

	items, err := h.store.ListByTeam(r.Context(), teamID, status)
	if err != nil {
		http.Error(w, "failed to list knowledge items", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// UpdateStatus approves or rejects a knowledge item.
func (h *KnowledgeHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	status := model.KnowledgeStatus(body.Status)
	if status != model.KnowledgeStatusApproved && status != model.KnowledgeStatusRejected {
		http.Error(w, "status must be 'approved' or 'rejected'", http.StatusBadRequest)
		return
	}

	if err := h.store.UpdateStatus(r.Context(), id, status); err != nil {
		http.Error(w, "failed to update knowledge item", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Delete removes a knowledge item.
func (h *KnowledgeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.store.Delete(r.Context(), id); err != nil {
		http.Error(w, "failed to delete knowledge item", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

**Step 2: Add `Knowledge` to `Deps` in `internal/server/router.go`**

```go
Knowledge *handlers.KnowledgeHandler
```

Add routes inside the authenticated group:

```go
// Knowledge
r.Get("/api/knowledge", deps.Knowledge.List)
r.Patch("/api/knowledge/{id}", deps.Knowledge.UpdateStatus)
r.Delete("/api/knowledge/{id}", deps.Knowledge.Delete)
```

**Step 3: Wire up in `cmd/server/main.go`**

Read the server main to find where handlers are constructed, then add:

```go
knowledgeStore := knowledge.NewDBStore(database)
// ...
Knowledge: handlers.NewKnowledgeHandler(knowledgeStore),
```

Add import: `"github.com/tinkerloft/fleetlift/internal/knowledge"`

**Step 4: Build**

```bash
go build ./...
```

**Step 5: Commit**

```bash
git add internal/server/handlers/knowledge.go internal/server/router.go cmd/server/main.go
git commit -m "feat(knowledge): API endpoints GET/PATCH/DELETE /api/knowledge"
```

---

## Task 8: Knowledge CLI commands

**Files:**
- Create: `cmd/cli/knowledge.go`
- Modify: `cmd/cli/main.go`

**Step 1: Create `cmd/cli/knowledge.go`**

```go
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func knowledgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "knowledge",
		Short: "Manage knowledge items",
	}
	cmd.AddCommand(knowledgeListCmd(), knowledgeApproveCmd(), knowledgeRejectCmd())
	return cmd
}

func knowledgeListCmd() *cobra.Command {
	var status string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List knowledge items",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			path := "/api/knowledge"
			if status != "" {
				path += "?status=" + status
			}
			return c.get(path)
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (pending|approved|rejected)")
	return cmd
}

func knowledgeApproveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve <id>",
		Short: "Approve a knowledge item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			_, err := c.patch("/api/knowledge/"+args[0], map[string]string{"status": "approved"})
			if err != nil {
				return err
			}
			fmt.Println("approved")
			return nil
		},
	}
}

func knowledgeRejectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reject <id>",
		Short: "Reject a knowledge item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			_, err := c.patch("/api/knowledge/"+args[0], map[string]string{"status": "rejected"})
			if err != nil {
				return err
			}
			fmt.Println("rejected")
			return nil
		},
	}
}
```

Check if `c.patch(...)` exists on the CLI client. If not, add it to `cmd/cli/client.go`.

**Step 2: Register in `cmd/cli/main.go`**

Add `knowledgeCmd()` to `root.AddCommand(...)`.

**Step 3: Build**

```bash
go build ./cmd/cli/...
```

**Step 4: Commit**

```bash
git add cmd/cli/knowledge.go cmd/cli/main.go cmd/cli/client.go
git commit -m "feat(knowledge): CLI commands — list, approve, reject"
```

---

## Task 9: Knowledge Web UI page

**Files:**
- Create: `web/src/pages/KnowledgePage.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/Layout.tsx`

**Step 1: Create `web/src/pages/KnowledgePage.tsx`**

```tsx
import { useState, useEffect } from 'react'

interface KnowledgeItem {
  id: string
  type: string
  summary: string
  details?: string
  tags?: string[]
  confidence: number
  status: 'pending' | 'approved' | 'rejected'
  workflow_template_id?: string
  created_at: string
}

export function KnowledgePage() {
  const [items, setItems] = useState<KnowledgeItem[]>([])
  const [statusFilter, setStatusFilter] = useState('pending')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch(`/api/knowledge?status=${statusFilter}`, {
      headers: { Authorization: `Bearer ${localStorage.getItem('token')}` },
    })
      .then(r => r.json())
      .then(data => setItems(data ?? []))
      .finally(() => setLoading(false))
  }, [statusFilter])

  const updateStatus = async (id: string, status: string) => {
    await fetch(`/api/knowledge/${id}`, {
      method: 'PATCH',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${localStorage.getItem('token')}`,
      },
      body: JSON.stringify({ status }),
    })
    setItems(prev => prev.filter(i => i.id !== id))
  }

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Knowledge Base</h1>
        <select
          className="border rounded px-3 py-1 text-sm"
          value={statusFilter}
          onChange={e => setStatusFilter(e.target.value)}
        >
          <option value="pending">Pending</option>
          <option value="approved">Approved</option>
          <option value="rejected">Rejected</option>
        </select>
      </div>

      {loading && <p className="text-muted-foreground">Loading...</p>}
      {!loading && items.length === 0 && <p className="text-muted-foreground">No items.</p>}

      {items.map(item => (
        <div key={item.id} className="border rounded-lg p-4 space-y-2">
          <div className="flex items-start justify-between gap-4">
            <div>
              <span className="text-xs font-mono uppercase text-muted-foreground mr-2">{item.type}</span>
              <span className="font-medium">{item.summary}</span>
              {item.details && <p className="text-sm text-muted-foreground mt-1">{item.details}</p>}
              {item.tags && item.tags.length > 0 && (
                <div className="flex gap-1 mt-1">
                  {item.tags.map(t => (
                    <span key={t} className="text-xs bg-muted px-2 py-0.5 rounded">{t}</span>
                  ))}
                </div>
              )}
            </div>
            {item.status === 'pending' && (
              <div className="flex gap-2 shrink-0">
                <button
                  onClick={() => updateStatus(item.id, 'approved')}
                  className="text-sm px-3 py-1 bg-green-600 text-white rounded hover:bg-green-700"
                >
                  Approve
                </button>
                <button
                  onClick={() => updateStatus(item.id, 'rejected')}
                  className="text-sm px-3 py-1 bg-red-600 text-white rounded hover:bg-red-700"
                >
                  Reject
                </button>
              </div>
            )}
          </div>
          <p className="text-xs text-muted-foreground">
            Confidence: {(item.confidence * 100).toFixed(0)}% · {new Date(item.created_at).toLocaleDateString()}
          </p>
        </div>
      ))}
    </div>
  )
}
```

**Step 2: Add route in `web/src/App.tsx`**

```tsx
import { KnowledgePage } from './pages/KnowledgePage'
// ...
<Route path="/knowledge" element={<KnowledgePage />} />
```

**Step 3: Add nav link in `web/src/components/Layout.tsx`**

Add a link to `/knowledge` in the nav alongside Inbox. Inspect the current Layout.tsx first to match the nav pattern.

**Step 4: Build web**

```bash
cd web && npm run build
```
Expected: builds successfully, no TypeScript errors.

**Step 5: Commit**

```bash
git add web/src/pages/KnowledgePage.tsx web/src/App.tsx web/src/components/Layout.tsx web/dist/
git commit -m "feat(knowledge): web UI — Knowledge page with approve/reject"
```

---

## Task 10: GET /api/me

**Files:**
- Modify: `internal/server/handlers/auth.go`
- Modify: `internal/server/router.go`

**Step 1: Add `HandleMe` to `internal/server/handlers/auth.go`**

```go
// HandleMe returns the authenticated user's identity.
func (h *AuthHandler) HandleMe(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":        claims.UserID,
		"team_roles":     claims.TeamRoles,
		"platform_admin": claims.PlatformAdmin,
	})
}
```

**Step 2: Register route in `internal/server/router.go`**

Inside the authenticated group:

```go
r.Get("/api/me", deps.Auth.HandleMe)
```

**Step 3: Build and verify**

```bash
go build ./...
```

**Step 4: Commit**

```bash
git add internal/server/handlers/auth.go internal/server/router.go
git commit -m "fix: add GET /api/me endpoint for CLI auth polling"
```

---

## Task 11: evalCondition implementation

**Files:**
- Modify: `internal/workflow/dag.go`

**Step 1: Write failing test**

Create `internal/workflow/dag_test.go`:

```go
package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestEvalCondition_Always(t *testing.T) {
	assert.True(t, evalCondition("true", nil, nil))
}

func TestEvalCondition_StepStatus(t *testing.T) {
	outputs := map[string]*model.StepOutput{
		"step-a": {Status: model.StepStatusComplete},
	}
	assert.True(t, evalCondition(`{{eq (index .steps "step-a").status "complete"}}`, nil, outputs))
	assert.False(t, evalCondition(`{{eq (index .steps "step-a").status "failed"}}`, nil, outputs))
}

func TestEvalCondition_Empty(t *testing.T) {
	// empty condition = always run
	assert.True(t, evalCondition("", nil, nil))
}

func TestEvalCondition_InvalidTemplate(t *testing.T) {
	// invalid template: default to true (don't block step)
	assert.True(t, evalCondition("{{broken", nil, nil))
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/workflow/... -run TestEvalCondition -v
```
Expected: FAIL — current impl returns `true` for everything (no template evaluation).

**Step 3: Implement `evalCondition` in `internal/workflow/dag.go`**

Replace the stub:

```go
import (
	"bytes"
	"strings"
	"text/template"
)

// evalCondition evaluates a Go template condition string against step outputs and params.
// Returns true if the rendered result equals "true" (case-insensitive), or if condition is empty.
// Defaults to true on parse/execution errors to avoid blocking steps on bad conditions.
func evalCondition(condition string, params map[string]any, outputs map[string]*model.StepOutput) bool {
	if condition == "" {
		return true
	}

	// Build a simplified view of outputs for template access
	steps := map[string]map[string]any{}
	for id, out := range outputs {
		if out != nil {
			steps[id] = map[string]any{
				"status": string(out.Status),
				"error":  out.Error,
			}
		}
	}

	data := map[string]any{
		"steps":  steps,
		"params": params,
	}

	tmpl, err := template.New("cond").Parse(condition)
	if err != nil {
		return true // don't block on bad template
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return true
	}

	return strings.TrimSpace(buf.String()) == "true"
}
```

**Step 4: Run tests**

```bash
go test ./internal/workflow/... -run TestEvalCondition -v
```
Expected: all 4 tests PASS.

**Step 5: Run full test suite**

```bash
go test ./...
```
Expected: all pass.

**Step 6: Commit**

```bash
git add internal/workflow/dag.go internal/workflow/dag_test.go
git commit -m "feat: implement evalCondition with Go template evaluator"
```

---

## Task 12: Markdown export

**Files:**
- Modify: `internal/server/handlers/reports.go`

**Step 1: Update `Export` handler**

Replace the `Export` method:

```go
// Export exports a report as Markdown or JSON (default).
func (h *ReportsHandler) Export(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")
	format := r.URL.Query().Get("format")

	var run model.Run
	if err := h.db.GetContext(r.Context(), &run, `SELECT * FROM runs WHERE id=$1`, runID); err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	var steps []model.StepRun
	if err := h.db.SelectContext(r.Context(), &steps,
		`SELECT * FROM step_runs WHERE run_id=$1 ORDER BY created_at`, runID); err != nil {
		http.Error(w, "failed to get steps", http.StatusInternalServerError)
		return
	}

	if format == "markdown" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=report-"+runID+".md")
		renderMarkdownReport(w, run, steps)
		return
	}

	w.Header().Set("Content-Disposition", "attachment; filename=report-"+runID+".json")
	writeJSON(w, http.StatusOK, map[string]any{"run_id": runID, "run": run, "steps": steps})
}

const markdownReportTmpl = `# Report: {{.Run.WorkflowTitle}}

**Run ID:** {{.Run.ID}}
**Status:** {{.Run.Status}}
**Started:** {{.Run.StartedAt}}
**Completed:** {{.Run.CompletedAt}}

---

## Steps

| Step | Status | Duration |
|------|--------|----------|
{{- range .Steps}}
| {{.StepTitle}} | {{.Status}} | {{stepDuration .}} |
{{- end}}

{{range .Steps}}
### {{.StepTitle}}

**Status:** {{.Status}}
{{if .ErrorMessage}}**Error:** {{.ErrorMessage}}{{end}}
{{if .PRUrl}}**PR:** {{.PRUrl}}{{end}}
{{if .Diff}}
<details><summary>Diff</summary>

` + "```diff\n{{.Diff}}\n```" + `

</details>
{{end}}
{{end}}
`

func renderMarkdownReport(w http.ResponseWriter, run model.Run, steps []model.StepRun) {
	funcMap := template.FuncMap{
		"stepDuration": func(s model.StepRun) string {
			if s.StartedAt == nil || s.CompletedAt == nil {
				return "—"
			}
			return s.CompletedAt.Sub(*s.StartedAt).Round(time.Second).String()
		},
	}
	tmpl := template.Must(template.New("report").Funcs(funcMap).Parse(markdownReportTmpl))
	_ = tmpl.Execute(w, map[string]any{"Run": run, "Steps": steps})
}
```

Add imports: `"text/template"`, `"time"`.

**Step 2: Build**

```bash
go build ./...
```

Check `model.Run` for field names (`StartedAt`, `CompletedAt`, `WorkflowTitle`) — adjust template if field names differ by reading `internal/model/run.go`.

**Step 3: Commit**

```bash
git add internal/server/handlers/reports.go
git commit -m "feat: add markdown export for reports (GET /api/reports/{id}/export?format=markdown)"
```

---

## Task 13: Integration test skeleton

**Files:**
- Modify: `tests/integration/dag_test.go`

**Step 1: Write the integration test**

Replace the stub contents of `tests/integration/dag_test.go`:

```go
//go:build integration

package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

func TestDAGWorkflow_HappyPath(t *testing.T) {
	suite := testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	// Register workflows
	env.RegisterWorkflow(workflow.DAGWorkflow)
	env.RegisterWorkflow(workflow.StepWorkflow)

	// Mock all activities to succeed
	env.OnActivity("ProvisionSandbox").Return("test-sandbox", nil)
	env.OnActivity("ExecuteStep").Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
	}, nil)
	env.OnActivity("UpdateStepStatus").Return(nil)
	env.OnActivity("CleanupSandbox").Return(nil)

	def := model.WorkflowDef{
		ID:    "test-wf",
		Title: "Test Workflow",
		Steps: []model.StepDef{
			{
				ID:    "step-1",
				Title: "Test Step",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something",
				},
			},
		},
	}

	env.ExecuteWorkflow(workflow.DAGWorkflow, workflow.DAGInput{
		RunID:       "run-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
}

func TestDAGWorkflow_ConditionalStepSkipped(t *testing.T) {
	suite := testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterWorkflow(workflow.DAGWorkflow)
	env.RegisterWorkflow(workflow.StepWorkflow)

	env.OnActivity("ProvisionSandbox").Return("test-sandbox", nil)
	env.OnActivity("ExecuteStep").Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
	}, nil)
	env.OnActivity("UpdateStepStatus").Return(nil)
	env.OnActivity("CleanupSandbox").Return(nil)

	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "step-1", Execution: &model.ExecutionDef{Agent: "claude-code", Prompt: "first"}},
			{
				ID:        "step-2",
				Condition: `{{eq (index .steps "step-1").status "failed"}}`, // should be skipped
				Execution: &model.ExecutionDef{Agent: "claude-code", Prompt: "second"},
				DependsOn: []string{"step-1"},
			},
		},
	}

	env.ExecuteWorkflow(workflow.DAGWorkflow, workflow.DAGInput{
		RunID:       "run-2",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
}
```

**Step 2: Verify normal test suite unaffected**

```bash
go test ./...
```
Expected: all pass. The `//go:build integration` tag means this file is excluded from normal runs.

**Step 3: Verify integration build tag works**

```bash
go build -tags integration ./tests/integration/...
```
Expected: compiles without errors.

**Step 4: Commit**

```bash
git add tests/integration/dag_test.go
git commit -m "test(integration): DAGWorkflow happy path and conditional step skip"
```

---

## Task 14: Archive v1 docs

**Files:**
- Create dir: `docs/archive/v1/`
- Move: various v1 docs

**Step 1: Read GITHUB_ACTIONS_DISCOVERY.md and INTEGRATION_NOTES.md briefly**

```bash
head -20 docs/GITHUB_ACTIONS_DISCOVERY.md
head -20 docs/INTEGRATION_NOTES.md
```

If v1-only: move. If still relevant: note and decide.

**Step 2: Move v1 docs**

```bash
mkdir -p docs/archive/v1/examples
mkdir -p docs/archive/v1/plans

mv docs/GROUPED_EXECUTION.md docs/archive/v1/
mv docs/TASK_FILE_REFERENCE.md docs/archive/v1/
mv docs/GITHUB_ACTIONS_DISCOVERY.md docs/archive/v1/
mv docs/INTEGRATION_NOTES.md docs/archive/v1/
mv docs/plans/DESIGN.md docs/archive/v1/plans/
mv docs/plans/SIDECAR_AGENT.md docs/archive/v1/plans/
mv docs/plans/CAPABILITY_INVENTORY.md docs/archive/v1/plans/
mv docs/plans/OVERVIEW.md docs/archive/v1/plans/
mv docs/plans/ROADMAP.md docs/archive/v1/plans/
mv docs/plans/IMPLEMENTATION_PLAN.md docs/archive/v1/plans/
mv docs/examples docs/archive/v1/examples
```

**Step 3: Build to verify no broken go:generate or embed paths**

```bash
go build ./...
```

**Step 4: Commit**

```bash
git add -A docs/
git commit -m "docs: archive v1 documentation to docs/archive/v1/"
```

---

## Task 15: Rewrite README.md

**Files:**
- Modify: `README.md`

**Step 1: Rewrite README.md**

Read the current README first (`cat README.md`) to see what's there, then replace with v2 content:

```markdown
# Fleetlift

Multi-tenant agentic workflow platform. Define DAG-based workflows as YAML templates, run them at scale across repositories in isolated sandboxes, and collaborate with AI agents via real-time streaming and human-in-the-loop (HITL) signals.

## What it does

- **Workflow templates** — YAML-defined DAGs with parallel steps, dependencies, conditions
- **Agent execution** — Claude Code runs each step in an OpenSandbox container
- **HITL** — approve, reject, or steer any step mid-execution
- **Knowledge loop** — capture agent insights; inject approved items into future runs
- **Multi-tenant** — teams, JWT auth, GitHub OAuth
- **Reports** — structured output from report-mode steps, exportable as Markdown
- **9 built-in templates** — add tests, fix lint, upgrade deps, security audit, and more

## Quick start

**Prerequisites:** Docker, Go 1.24+, Node 20+

```bash
# Start Temporal + PostgreSQL
docker compose up -d

# Run database migrations + start the API server
go run ./cmd/server

# In a separate terminal: start the worker
go run ./cmd/worker

# Build the web UI
cd web && npm install && npm run build && cd ..

# Authenticate
fleetlift auth login

# List built-in workflows
fleetlift workflow list

# Trigger a run
fleetlift run start --workflow add-tests --param repo=https://github.com/org/repo.git

# Watch progress
fleetlift run list
```

## Environment variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `DATABASE_URL` | PostgreSQL DSN | `postgres://fleetlift:fleetlift@localhost:5432/fleetlift` |
| `TEMPORAL_ADDRESS` | Temporal gRPC address | `localhost:7233` |
| `OPENSANDBOX_DOMAIN` | OpenSandbox API base URL | — |
| `OPENSANDBOX_API_KEY` | OpenSandbox auth key | — |
| `AGENT_IMAGE` | Default sandbox image | `claude-code:latest` |
| `JWT_SECRET` | Server JWT signing key | — |
| `CREDENTIAL_ENCRYPTION_KEY` | 32-byte hex key for AES-256-GCM | — |
| `GITHUB_CLIENT_ID` | OAuth app client ID | — |
| `GITHUB_CLIENT_SECRET` | OAuth app client secret | — |
| `ANTHROPIC_API_KEY` | Claude API key for agent | — |
| `FLEETLIFT_API_URL` | CLI base URL | `http://localhost:8080` |

## Documentation

- [Workflow Reference](docs/WORKFLOW_REFERENCE.md) — YAML schema for workflow templates
- [CLI Reference](docs/CLI_REFERENCE.md) — all CLI commands
- [Architecture](docs/ARCHITECTURE.md) — system design
- [Troubleshooting](docs/TROUBLESHOOTING.md) — common issues

## Development

```bash
make lint       # golangci-lint
go test ./...   # unit tests
go test -tags integration ./tests/integration/...  # integration tests
cd web && npm run build  # build SPA
```
```

**Step 2: Build to verify no issues**

```bash
go build ./...
```

**Step 3: Commit**

```bash
git add README.md
git commit -m "docs: rewrite README for platform-v2"
```

---

## Task 16: WORKFLOW_REFERENCE.md

**Files:**
- Create: `docs/WORKFLOW_REFERENCE.md`

**Step 1: Write the reference**

Create `docs/WORKFLOW_REFERENCE.md` with a complete YAML schema reference. Cover:
- Top-level fields: `version`, `id`, `title`, `description`, `tags`, `parameters`, `steps`
- `ParameterDef`: `name`, `type`, `required`, `default`, `description`
- `StepDef`: all fields (`id`, `title`, `depends_on`, `condition`, `mode`, `execution`, `sandbox`, `knowledge`, `approval_policy`, `pull_request`, `optional`, `timeout`)
- `ExecutionDef`: `agent`, `prompt`, `verifiers`, `credentials`
- `SandboxSpec`: `image`, `resources`, `egress`
- `KnowledgeDef`: `capture`, `enrich`, `max_items`, `tags`
- `PRDef`: `branch_prefix`, `title`, `body`, `labels`, `draft`
- Condition syntax: Go template with `.steps.<id>.status` and `.params.<name>`
- Complete annotated example workflow

**Step 2: Commit**

```bash
git add docs/WORKFLOW_REFERENCE.md
git commit -m "docs: add WORKFLOW_REFERENCE.md for v2 template schema"
```

---

## Task 17: ARCHITECTURE.md

**Files:**
- Create: `docs/ARCHITECTURE.md`

**Step 1: Write the architecture doc**

Create `docs/ARCHITECTURE.md` covering:
- System components diagram (server, worker, CLI, web, Temporal, PostgreSQL, OpenSandbox)
- DAG execution model: DAGWorkflow → StepWorkflow → activities
- Request flow: CLI/web → REST API → Temporal client → worker
- Multi-tenancy model: teams, JWT claims, team-scoped resources
- Auth: JWT (HS256) + GitHub OAuth flow
- Knowledge loop: capture → pending → approved → enrich
- Streaming: SSE endpoint, step_run_logs table

**Step 2: Commit**

```bash
git add docs/ARCHITECTURE.md
git commit -m "docs: add ARCHITECTURE.md for platform-v2"
```

---

## Task 18: CLI_REFERENCE.md rewrite

**Files:**
- Modify: `docs/CLI_REFERENCE.md`

**Step 1: Rewrite `docs/CLI_REFERENCE.md`**

Document all v2 commands by reading `cmd/cli/*.go` for the current command set:

```bash
grep -n "Use:" cmd/cli/*.go
```

Cover: `auth login`, `auth logout`, `workflow list`, `workflow get`, `workflow create`, `run start`, `run list`, `run get`, `run approve`, `run reject`, `run steer`, `run cancel`, `inbox list`, `inbox read`, `credential set`, `credential list`, `credential delete`, `knowledge list`, `knowledge approve`, `knowledge reject`.

**Step 2: Commit**

```bash
git add docs/CLI_REFERENCE.md
git commit -m "docs: rewrite CLI_REFERENCE.md for v2 commands"
```

---

## Task 19: TROUBLESHOOTING.md rewrite

**Files:**
- Modify: `docs/TROUBLESHOOTING.md`

**Step 1: Rewrite `docs/TROUBLESHOOTING.md`**

Cover v2-specific symptoms:
- Worker not connecting to Temporal (check `TEMPORAL_ADDRESS`, `docker compose up`)
- Sandbox provisioning failures (check `OPENSANDBOX_DOMAIN`, `OPENSANDBOX_API_KEY`)
- JWT auth errors (`JWT_SECRET` not set, token expired)
- Agent not starting (`ANTHROPIC_API_KEY` missing)
- Database migration errors (schema version conflicts)
- Web UI blank (dist not built — run `cd web && npm run build`)
- CLI "unauthorized" after login (`GET /api/me` check)

**Step 2: Run final checks**

```bash
make lint
go test ./...
go build ./...
cd web && npm run build
```
All must pass.

**Step 3: Final commit**

```bash
git add docs/TROUBLESHOOTING.md
git commit -m "docs: rewrite TROUBLESHOOTING.md for v2"
```

---

## Final Verification

```bash
make lint         # zero errors
go test ./...     # all pass
go build ./...    # clean build
cd web && npm run build  # clean build
```
