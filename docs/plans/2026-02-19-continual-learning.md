# Continual Learning (Phase 10) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Capture reusable knowledge from approved transformations (especially steering corrections) and inject it into future runs to reduce steering rounds needed.

**Architecture:** Two new Temporal activities — `CaptureKnowledge` (runs post-approval, calls Claude API to extract `KnowledgeItem`s and writes YAML to `~/.fleetlift/knowledge/{task-id}/`) and `EnrichPrompt` (reads knowledge store, filters by relevance, prepends lessons to the Claude Code prompt). A new `internal/knowledge/` package handles all YAML I/O. CLI `knowledge` sub-command covers list/show/add/delete. Both activities are non-blocking (failures are logged, not returned as errors).

**Tech Stack:** Go, Anthropic Go SDK (`github.com/anthropics/anthropic-sdk-go`), `gopkg.in/yaml.v3` (already in go.mod), Cobra (already in go.mod), Temporal SDK (already in go.mod).

---

### Task 1: Add Anthropic Go SDK dependency

**Files:**
- Modify: `go.mod`

**Step 1: Add the SDK**

```bash
cd /Users/andrew/dev/code/projects/fleetlift
go get github.com/anthropics/anthropic-sdk-go@latest
```

**Step 2: Verify it appears in go.mod**

```bash
grep anthropic go.mod
```
Expected: `github.com/anthropics/anthropic-sdk-go v...`

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add anthropic-sdk-go dependency for knowledge capture"
```

---

### Task 2: Define knowledge data model

**Files:**
- Create: `internal/model/knowledge.go`

**Step 1: Write the failing test**

Create `internal/model/knowledge_test.go`:

```go
package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestKnowledgeItemValidation(t *testing.T) {
	item := model.KnowledgeItem{
		ID:         "test-001",
		Type:       model.KnowledgeTypeCorrection,
		Summary:    "Always run go mod tidy after changing imports",
		Details:    "Failing to run go mod tidy leaves stale entries.",
		Source:     model.KnowledgeSourceSteeringExtracted,
		Tags:       []string{"go", "imports"},
		Confidence: 0.9,
		CreatedAt:  time.Now(),
	}
	assert.Equal(t, model.KnowledgeTypeCorrection, item.Type)
	assert.Equal(t, model.KnowledgeSourceSteeringExtracted, item.Source)
	assert.Len(t, item.Tags, 2)
}

func TestKnowledgeConfig_Defaults(t *testing.T) {
	cfg := model.KnowledgeConfig{}
	assert.False(t, cfg.CaptureDisabled)
	assert.False(t, cfg.EnrichDisabled)
	assert.Equal(t, 0, cfg.MaxItems) // 0 means use default (10)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/model/... -run TestKnowledgeItem -v
```
Expected: FAIL with compile error (types not defined)

**Step 3: Write minimal implementation**

Create `internal/model/knowledge.go`:

```go
package model

import "time"

// KnowledgeType classifies the kind of knowledge item.
type KnowledgeType string

const (
	// KnowledgeTypePattern is a reusable approach that worked.
	KnowledgeTypePattern KnowledgeType = "pattern"
	// KnowledgeTypeCorrection is extracted from steering (agent went wrong, was corrected).
	KnowledgeTypeCorrection KnowledgeType = "correction"
	// KnowledgeTypeGotcha is a non-obvious failure mode.
	KnowledgeTypeGotcha KnowledgeType = "gotcha"
	// KnowledgeTypeContext is repo-specific knowledge.
	KnowledgeTypeContext KnowledgeType = "context"
)

// KnowledgeSource describes how a knowledge item was created.
type KnowledgeSource string

const (
	KnowledgeSourceAutoCaptured       KnowledgeSource = "auto_captured"
	KnowledgeSourceSteeringExtracted  KnowledgeSource = "steering_extracted"
	KnowledgeSourceManual             KnowledgeSource = "manual"
)

// KnowledgeOrigin links a knowledge item back to its source execution.
type KnowledgeOrigin struct {
	TaskID        string `json:"task_id" yaml:"task_id"`
	Repository    string `json:"repository,omitempty" yaml:"repository,omitempty"`
	SteeringPrompt string `json:"steering_prompt,omitempty" yaml:"steering_prompt,omitempty"`
	Iteration     int    `json:"iteration,omitempty" yaml:"iteration,omitempty"`
}

// KnowledgeItem is a reusable piece of knowledge extracted from a transformation.
type KnowledgeItem struct {
	ID          string           `json:"id" yaml:"id"`
	Type        KnowledgeType    `json:"type" yaml:"type"`
	Summary     string           `json:"summary" yaml:"summary"`
	Details     string           `json:"details" yaml:"details"`
	Source      KnowledgeSource  `json:"source" yaml:"source"`
	Tags        []string         `json:"tags,omitempty" yaml:"tags,omitempty"`
	Confidence  float64          `json:"confidence" yaml:"confidence"`
	CreatedFrom *KnowledgeOrigin `json:"created_from,omitempty" yaml:"created_from,omitempty"`
	CreatedAt   time.Time        `json:"created_at" yaml:"created_at"`
}

// KnowledgeConfig is the optional knowledge section in a Task YAML.
type KnowledgeConfig struct {
	// CaptureDisabled disables auto-capture after approval (default: false = capture enabled).
	CaptureDisabled bool `json:"capture_disabled,omitempty" yaml:"capture_disabled,omitempty"`
	// EnrichDisabled disables prompt enrichment before agent execution (default: false = enrich enabled).
	EnrichDisabled bool `json:"enrich_disabled,omitempty" yaml:"enrich_disabled,omitempty"`
	// MaxItems caps how many knowledge items are injected into the prompt (default: 10).
	MaxItems int `json:"max_items,omitempty" yaml:"max_items,omitempty"`
	// Tags are additional tags for filtering/matching knowledge items.
	Tags []string `json:"tags,omitempty" yaml:"tags,omitempty"`
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/model/... -run TestKnowledge -v
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/model/knowledge.go internal/model/knowledge_test.go
git commit -m "feat(knowledge): add KnowledgeItem and KnowledgeConfig data model"
```

---

### Task 3: Add Knowledge field to Task and wire YAML parsing

**Files:**
- Modify: `internal/model/task.go` (line 310 — end of Task struct, after `Credentials` field)

**Step 1: Write the failing test**

Add to `internal/model/knowledge_test.go`:

```go
func TestTask_KnowledgeConfig_YAML(t *testing.T) {
	yaml := `
version: 1
id: test-task
title: Test
execution:
  agentic:
    prompt: "do something"
knowledge:
  max_items: 5
  tags: [go, logging]
`
	task, err := ParseTaskYAML([]byte(yaml))
	require.NoError(t, err)
	require.NotNil(t, task.Knowledge)
	assert.Equal(t, 5, task.Knowledge.MaxItems)
	assert.Equal(t, []string{"go", "logging"}, task.Knowledge.Tags)
}
```

You'll need to find where `ParseTaskYAML` is defined (check `internal/config/` or `internal/model/`) and import accordingly.

**Step 2: Run test to verify it fails**

```bash
go test ./internal/model/... -run TestTask_KnowledgeConfig -v
```
Expected: FAIL (Knowledge field missing)

**Step 3: Add the field to Task struct**

In `internal/model/task.go`, find the end of the Task struct (after the `Credentials` field, around line 310). Add:

```go
	// Knowledge capture and enrichment configuration (optional).
	Knowledge *KnowledgeConfig `json:"knowledge,omitempty" yaml:"knowledge,omitempty"`
```

**Step 4: Add helper methods to Task**

In `internal/model/task.go`, after the existing helper methods, add:

```go
// KnowledgeCaptureEnabled returns true if knowledge capture is enabled (default: true).
func (t Task) KnowledgeCaptureEnabled() bool {
	return t.Knowledge == nil || !t.Knowledge.CaptureDisabled
}

// KnowledgeEnrichEnabled returns true if prompt enrichment is enabled (default: true).
func (t Task) KnowledgeEnrichEnabled() bool {
	return t.Knowledge == nil || !t.Knowledge.EnrichDisabled
}

// KnowledgeMaxItems returns the max items for prompt injection (default: 10).
func (t Task) KnowledgeMaxItems() int {
	if t.Knowledge == nil || t.Knowledge.MaxItems <= 0 {
		return 10
	}
	return t.Knowledge.MaxItems
}

// KnowledgeTags returns any extra tags configured for knowledge filtering.
func (t Task) KnowledgeTags() []string {
	if t.Knowledge == nil {
		return nil
	}
	return t.Knowledge.Tags
}
```

**Step 5: Run tests**

```bash
go test ./internal/model/... -v
go build ./...
```

**Step 6: Commit**

```bash
git add internal/model/task.go internal/model/knowledge_test.go
git commit -m "feat(knowledge): add Knowledge field to Task with helper methods"
```

---

### Task 4: Implement the knowledge storage package

**Files:**
- Create: `internal/knowledge/store.go`
- Create: `internal/knowledge/store_test.go`

**Step 1: Write the failing tests**

Create `internal/knowledge/store_test.go`:

```go
package knowledge_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestStore_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	store := knowledge.NewStore(dir)

	item := model.KnowledgeItem{
		ID:         "item-001",
		Type:       model.KnowledgeTypeCorrection,
		Summary:    "Run go mod tidy after imports",
		Details:    "Prevents stale entries in go.sum",
		Source:     model.KnowledgeSourceSteeringExtracted,
		Tags:       []string{"go"},
		Confidence: 0.9,
		CreatedAt:  time.Now().UTC().Truncate(time.Second),
	}

	err := store.Write("task-abc", item)
	require.NoError(t, err)

	items, err := store.List("task-abc")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, item.ID, items[0].ID)
	assert.Equal(t, item.Summary, items[0].Summary)
}

func TestStore_List_EmptyTaskID(t *testing.T) {
	dir := t.TempDir()
	store := knowledge.NewStore(dir)

	items, err := store.List("nonexistent-task")
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestStore_ListAll(t *testing.T) {
	dir := t.TempDir()
	store := knowledge.NewStore(dir)

	item1 := model.KnowledgeItem{ID: "i1", Type: model.KnowledgeTypePattern, Summary: "p1", Source: model.KnowledgeSourceAutoCaptured, CreatedAt: time.Now()}
	item2 := model.KnowledgeItem{ID: "i2", Type: model.KnowledgeTypeGotcha, Summary: "g1", Source: model.KnowledgeSourceManual, CreatedAt: time.Now()}

	require.NoError(t, store.Write("task-1", item1))
	require.NoError(t, store.Write("task-2", item2))

	all, err := store.ListAll()
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store := knowledge.NewStore(dir)

	item := model.KnowledgeItem{ID: "del-01", Type: model.KnowledgeTypePattern, Summary: "x", Source: model.KnowledgeSourceManual, CreatedAt: time.Now()}
	require.NoError(t, store.Write("task-abc", item))

	err := store.Delete("del-01")
	require.NoError(t, err)

	items, err := store.List("task-abc")
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestStore_FilterByTags(t *testing.T) {
	dir := t.TempDir()
	store := knowledge.NewStore(dir)

	goItem := model.KnowledgeItem{ID: "go-01", Type: model.KnowledgeTypePattern, Summary: "go thing", Source: model.KnowledgeSourceManual, Tags: []string{"go", "imports"}, Confidence: 0.8, CreatedAt: time.Now()}
	pyItem := model.KnowledgeItem{ID: "py-01", Type: model.KnowledgeTypeGotcha, Summary: "python thing", Source: model.KnowledgeSourceManual, Tags: []string{"python"}, Confidence: 0.7, CreatedAt: time.Now()}

	require.NoError(t, store.Write("t1", goItem))
	require.NoError(t, store.Write("t1", pyItem))

	results, err := store.FilterByTags([]string{"go"}, 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "go-01", results[0].ID)
}

func TestStore_DefaultBaseDir(t *testing.T) {
	store := knowledge.DefaultStore()
	// Just verify it doesn't panic and has a reasonable path
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".fleetlift", "knowledge")
	assert.Equal(t, expected, store.BaseDir())
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/knowledge/... -v
```
Expected: FAIL with package not found

**Step 3: Write the implementation**

Create `internal/knowledge/store.go`:

```go
// Package knowledge provides local persistent storage for knowledge items.
package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// Store manages knowledge items on the local filesystem.
// Layout: {BaseDir}/{task-id}/item-{item-id}.yaml
type Store struct {
	baseDir string
}

// NewStore creates a Store using the given base directory.
func NewStore(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

// DefaultStore creates a Store using ~/.fleetlift/knowledge.
func DefaultStore() *Store {
	home, _ := os.UserHomeDir()
	return NewStore(filepath.Join(home, ".fleetlift", "knowledge"))
}

// BaseDir returns the base directory for this store.
func (s *Store) BaseDir() string {
	return s.baseDir
}

// Write persists a knowledge item for the given task ID.
func (s *Store) Write(taskID string, item model.KnowledgeItem) error {
	dir := filepath.Join(s.baseDir, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating knowledge dir: %w", err)
	}
	path := filepath.Join(dir, "item-"+item.ID+".yaml")
	data, err := yaml.Marshal(item)
	if err != nil {
		return fmt.Errorf("marshaling knowledge item: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// List returns all knowledge items for a given task ID.
func (s *Store) List(taskID string) ([]model.KnowledgeItem, error) {
	dir := filepath.Join(s.baseDir, taskID)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading knowledge dir: %w", err)
	}

	var items []model.KnowledgeItem
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		item, err := readItem(filepath.Join(dir, e.Name()))
		if err != nil {
			continue // skip malformed items
		}
		items = append(items, item)
	}
	return items, nil
}

// ListAll returns all knowledge items across all tasks.
func (s *Store) ListAll() ([]model.KnowledgeItem, error) {
	entries, err := os.ReadDir(s.baseDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading knowledge base dir: %w", err)
	}

	var all []model.KnowledgeItem
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		items, err := s.List(e.Name())
		if err != nil {
			continue
		}
		all = append(all, items...)
	}
	return all, nil
}

// Delete removes a knowledge item by its ID (searches all task subdirs).
func (s *Store) Delete(itemID string) error {
	taskDirs, err := os.ReadDir(s.baseDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading knowledge base dir: %w", err)
	}

	target := "item-" + itemID + ".yaml"
	for _, td := range taskDirs {
		if !td.IsDir() {
			continue
		}
		path := filepath.Join(s.baseDir, td.Name(), target)
		if err := os.Remove(path); err == nil {
			return nil
		}
	}
	return fmt.Errorf("knowledge item %q not found", itemID)
}

// FilterByTags returns up to maxItems knowledge items whose tags overlap with filterTags.
// If filterTags is empty, returns all items up to maxItems, sorted by confidence descending.
func (s *Store) FilterByTags(filterTags []string, maxItems int) ([]model.KnowledgeItem, error) {
	all, err := s.ListAll()
	if err != nil {
		return nil, err
	}

	tagSet := make(map[string]bool, len(filterTags))
	for _, t := range filterTags {
		tagSet[strings.ToLower(t)] = true
	}

	var matched []model.KnowledgeItem
	for _, item := range all {
		if len(tagSet) == 0 {
			matched = append(matched, item)
			continue
		}
		for _, t := range item.Tags {
			if tagSet[strings.ToLower(t)] {
				matched = append(matched, item)
				break
			}
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].Confidence > matched[j].Confidence
	})

	if maxItems > 0 && len(matched) > maxItems {
		matched = matched[:maxItems]
	}
	return matched, nil
}

// LoadFromRepo loads knowledge items from a transformation repository's
// .fleetlift/knowledge/items/ directory.
func LoadFromRepo(repoPath string) ([]model.KnowledgeItem, error) {
	dir := filepath.Join(repoPath, ".fleetlift", "knowledge", "items")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading repo knowledge dir: %w", err)
	}

	var items []model.KnowledgeItem
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		item, err := readItem(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func readItem(path string) (model.KnowledgeItem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.KnowledgeItem{}, err
	}
	var item model.KnowledgeItem
	if err := yaml.Unmarshal(data, &item); err != nil {
		return model.KnowledgeItem{}, err
	}
	return item, nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/knowledge/... -v
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/knowledge/
git commit -m "feat(knowledge): add local knowledge store with YAML persistence"
```

---

### Task 5: Implement CaptureKnowledge activity

**Files:**
- Create: `internal/activity/knowledge.go`
- Create: `internal/activity/knowledge_test.go`

**Step 1: Write the failing test**

Create `internal/activity/knowledge_test.go`:

```go
package activity_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinkerloft/fleetlift/internal/activity"
	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestBuildCapturePrompt(t *testing.T) {
	input := activity.CaptureKnowledgeInput{
		TaskID:        "slog-migration-123",
		OriginalPrompt: "Migrate all Go services from logrus to slog",
		SteeringHistory: []model.SteeringIteration{
			{
				IterationNumber: 1,
				Prompt:          "Also update test helpers",
				Output:          "Updated 3 files",
				Timestamp:       time.Now(),
			},
		},
		DiffSummary:    "5 files changed, +120, -40",
		VerifiersPassed: true,
	}

	prompt := activity.BuildCapturePrompt(input)
	assert.Contains(t, prompt, "slog-migration-123")
	assert.Contains(t, prompt, "Migrate all Go services")
	assert.Contains(t, prompt, "Also update test helpers")
	assert.Contains(t, prompt, "5 files changed")
	assert.Contains(t, prompt, "KnowledgeItem")
}

func TestParseKnowledgeItems(t *testing.T) {
	raw := `[
  {
    "type": "correction",
    "summary": "Always run go mod tidy after import changes",
    "details": "Prevents stale go.sum entries that cause CI failures.",
    "tags": ["go", "imports"],
    "confidence": 0.95
  },
  {
    "type": "gotcha",
    "summary": "Test helpers that wrap logrus need separate updates",
    "details": "The main logger migration misses wrapper functions.",
    "tags": ["go", "logging", "testing"],
    "confidence": 0.85
  }
]`

	items, err := activity.ParseKnowledgeItems(raw, "task-abc", []model.SteeringIteration{
		{Prompt: "Also update test helpers"},
	})
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, model.KnowledgeTypeCorrection, items[0].Type)
	assert.NotEmpty(t, items[0].ID)
	assert.Equal(t, model.KnowledgeSourceSteeringExtracted, items[0].Source)
	assert.Equal(t, model.KnowledgeTypeGotcha, items[1].Type)
	assert.Equal(t, model.KnowledgeSourceAutoCaptured, items[1].Source)
}

func TestParseKnowledgeItems_EmptyJSON(t *testing.T) {
	items, err := activity.ParseKnowledgeItems("[]", "task-abc", nil)
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestParseKnowledgeItems_MalformedJSON(t *testing.T) {
	_, err := activity.ParseKnowledgeItems("not json", "task-abc", nil)
	assert.Error(t, err)
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/activity/... -run TestBuildCapturePrompt -v
go test ./internal/activity/... -run TestParseKnowledgeItems -v
```
Expected: FAIL (activity package missing types)

**Step 3: Write the implementation**

Create `internal/activity/knowledge.go`:

```go
// Package activity contains Temporal activity implementations.
package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	temporalactivity "go.temporal.io/sdk/activity"

	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// KnowledgeActivities contains activities for knowledge capture and prompt enrichment.
type KnowledgeActivities struct {
	Store *knowledge.Store
}

// NewKnowledgeActivities creates a new KnowledgeActivities with the default local store.
func NewKnowledgeActivities() *KnowledgeActivities {
	return &KnowledgeActivities{Store: knowledge.DefaultStore()}
}

// CaptureKnowledgeInput is the input for CaptureKnowledge.
type CaptureKnowledgeInput struct {
	TaskID          string
	OriginalPrompt  string
	SteeringHistory []model.SteeringIteration
	DiffSummary     string
	VerifiersPassed bool
	// RepoNames are the repositories that were transformed (for tagging).
	RepoNames []string
}

// EnrichPromptInput is the input for EnrichPrompt.
type EnrichPromptInput struct {
	OriginalPrompt string
	// FilterTags are tags used to find relevant knowledge (e.g. language, framework).
	FilterTags []string
	// MaxItems caps how many knowledge items to inject (default 10).
	MaxItems int
	// TransformationRepoPath is the local path to a transformation repo (optional).
	// When set, Tier 3 knowledge (from .fleetlift/knowledge/items/) takes precedence.
	TransformationRepoPath string
}

// CaptureKnowledge calls Claude API to extract KnowledgeItems from an approved
// transformation and persists them to the local knowledge store.
// This activity is non-blocking: failures are logged but not returned as errors,
// so a Claude API outage never prevents PR creation.
func (a *KnowledgeActivities) CaptureKnowledge(ctx context.Context, input CaptureKnowledgeInput) ([]model.KnowledgeItem, error) {
	logger := temporalactivity.GetLogger(ctx)

	// Skip if no steering happened and no diff — nothing meaningful to capture.
	if len(input.SteeringHistory) == 0 && input.DiffSummary == "" {
		logger.Info("Skipping knowledge capture: no steering history and no diff")
		return nil, nil
	}

	prompt := BuildCapturePrompt(input)

	client := anthropic.NewClient() // reads ANTHROPIC_API_KEY from env
	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaude Haiku 4 5,
		MaxTokens: anthropic.Int(2048),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		// Non-blocking: log warning, return empty (no error)
		logger.Warn("Knowledge capture API call failed", "error", err)
		return nil, nil
	}

	if len(msg.Content) == 0 {
		return nil, nil
	}

	rawJSON := extractJSONArray(msg.Content[0].Text)
	items, err := ParseKnowledgeItems(rawJSON, input.TaskID, input.SteeringHistory)
	if err != nil {
		logger.Warn("Failed to parse knowledge items from Claude response", "error", err)
		return nil, nil
	}

	for _, item := range items {
		if err := a.Store.Write(input.TaskID, item); err != nil {
			logger.Warn("Failed to persist knowledge item", "id", item.ID, "error", err)
		}
	}

	logger.Info("Knowledge capture complete", "items", len(items), "task", input.TaskID)
	return items, nil
}

// EnrichPrompt loads relevant knowledge items and prepends them to the original prompt.
// Returns the original prompt unchanged if no items are found.
func (a *KnowledgeActivities) EnrichPrompt(ctx context.Context, input EnrichPromptInput) (string, error) {
	logger := temporalactivity.GetLogger(ctx)

	maxItems := input.MaxItems
	if maxItems <= 0 {
		maxItems = 10
	}

	var items []model.KnowledgeItem

	// Tier 3: transformation repo takes precedence when available.
	if input.TransformationRepoPath != "" {
		repoItems, err := knowledge.LoadFromRepo(input.TransformationRepoPath)
		if err != nil {
			logger.Warn("Failed to load knowledge from transformation repo", "error", err)
		} else {
			items = append(items, repoItems...)
		}
	}

	// Tier 2: local store, filtered by tags.
	if len(items) < maxItems {
		localItems, err := a.Store.FilterByTags(input.FilterTags, maxItems-len(items))
		if err != nil {
			logger.Warn("Failed to load local knowledge items", "error", err)
		} else {
			items = append(items, localItems...)
		}
	}

	if len(items) == 0 {
		return input.OriginalPrompt, nil
	}

	var sb strings.Builder
	sb.WriteString(input.OriginalPrompt)
	sb.WriteString("\n\n---\n## Lessons from previous runs\n\n")
	sb.WriteString("Keep these in mind based on previous transformations:\n")
	for _, item := range items {
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", item.Type, item.Summary))
	}

	logger.Info("Prompt enriched with knowledge items", "items", len(items))
	return sb.String(), nil
}

// BuildCapturePrompt builds the Claude prompt for knowledge extraction.
// Exported for testing.
func BuildCapturePrompt(input CaptureKnowledgeInput) string {
	var sb strings.Builder

	sb.WriteString(`Analyze this code transformation execution and extract reusable knowledge items.

Return ONLY a JSON array of knowledge items. Each item must have these fields:
- "type": one of "pattern", "correction", "gotcha", "context"
- "summary": one-line description (max 100 chars)
- "details": 1-3 sentences explaining why this matters
- "tags": array of relevant tags (language, framework, tool names)
- "confidence": float 0.0-1.0

Focus especially on steering corrections — they indicate where the agent went wrong.
If there's nothing useful to capture, return an empty array: []

KnowledgeItem types:
- "pattern": reusable approach that worked well
- "correction": extracted from steering (agent was wrong, human corrected it)
- "gotcha": non-obvious failure mode others should avoid
- "context": repo-specific knowledge

`)

	sb.WriteString(fmt.Sprintf("## Task ID: %s\n\n", input.TaskID))
	sb.WriteString(fmt.Sprintf("## Original Prompt\n%s\n\n", input.OriginalPrompt))

	if len(input.SteeringHistory) > 0 {
		sb.WriteString("## Steering Corrections\n")
		for _, iter := range input.SteeringHistory {
			sb.WriteString(fmt.Sprintf("### Iteration %d\nHuman feedback: %s\n", iter.IterationNumber, iter.Prompt))
			if iter.Output != "" {
				sb.WriteString(fmt.Sprintf("Agent response summary: %s\n", truncate(iter.Output, 500)))
			}
			sb.WriteString("\n")
		}
	}

	if input.DiffSummary != "" {
		sb.WriteString(fmt.Sprintf("## Result\n%s\nVerifiers passed: %v\n\n", input.DiffSummary, input.VerifiersPassed))
	}

	sb.WriteString("\nRespond with only the JSON array, no markdown, no explanation.")
	return sb.String()
}

// rawKnowledgeItem is used for JSON unmarshaling from Claude's response.
type rawKnowledgeItem struct {
	Type       string   `json:"type"`
	Summary    string   `json:"summary"`
	Details    string   `json:"details"`
	Tags       []string `json:"tags"`
	Confidence float64  `json:"confidence"`
}

// ParseKnowledgeItems parses Claude's JSON response into KnowledgeItem slice.
// Exported for testing.
func ParseKnowledgeItems(rawJSON string, taskID string, steeringHistory []model.SteeringIteration) ([]model.KnowledgeItem, error) {
	var raw []rawKnowledgeItem
	if err := json.Unmarshal([]byte(rawJSON), &raw); err != nil {
		return nil, fmt.Errorf("parsing knowledge JSON: %w", err)
	}

	// Build a set of steering prompt keywords for source classification.
	steeringKeywords := make(map[string]bool)
	for _, iter := range steeringHistory {
		for _, word := range strings.Fields(strings.ToLower(iter.Prompt)) {
			steeringKeywords[word] = true
		}
	}

	now := time.Now().UTC()
	var items []model.KnowledgeItem
	for _, r := range raw {
		if r.Summary == "" {
			continue
		}
		source := model.KnowledgeSourceAutoCaptured
		if r.Type == "correction" && len(steeringKeywords) > 0 {
			source = model.KnowledgeSourceSteeringExtracted
		}

		items = append(items, model.KnowledgeItem{
			ID:         uuid.New().String()[:8],
			Type:       model.KnowledgeType(r.Type),
			Summary:    r.Summary,
			Details:    r.Details,
			Source:     source,
			Tags:       r.Tags,
			Confidence: r.Confidence,
			CreatedFrom: &model.KnowledgeOrigin{
				TaskID: taskID,
			},
			CreatedAt: now,
		})
	}
	return items, nil
}

// extractJSONArray extracts a JSON array from text that might contain surrounding prose.
func extractJSONArray(text string) string {
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start == -1 || end == -1 || end < start {
		return "[]"
	}
	return text[start : end+1]
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
```

**Note on model constant:** Use the correct Haiku model ID from the Anthropic SDK. Check the SDK docs for the exact constant name — it may be `anthropic.ModelClaudeHaiku45` or similar. Adjust accordingly after running `go build`.

**Step 4: Run tests**

```bash
go test ./internal/activity/... -run TestBuildCapturePrompt -v
go test ./internal/activity/... -run TestParseKnowledgeItems -v
```
Expected: PASS (these test exported helpers, not the full activity)

**Step 5: Build to catch any import issues**

```bash
go build ./...
```
Fix any import or constant name issues from the Anthropic SDK.

**Step 6: Commit**

```bash
git add internal/activity/knowledge.go internal/activity/knowledge_test.go
git commit -m "feat(knowledge): add CaptureKnowledge and EnrichPrompt activities"
```

---

### Task 6: Add activity constants and register with worker

**Files:**
- Modify: `internal/activity/constants.go` (after line 48, end of activity name constants block)
- Modify: `cmd/worker/main.go`

**Step 1: Add constants**

In `internal/activity/constants.go`, add to the constants block after `ActivitySubmitSteeringAction`:

```go
	// Knowledge capture and prompt enrichment activities
	ActivityCaptureKnowledge = "CaptureKnowledge"
	ActivityEnrichPrompt     = "EnrichPrompt"
```

**Step 2: Register with worker**

In `cmd/worker/main.go`:

1. After existing `New*Activities` calls, add:
```go
knowledgeActivities := activity.NewKnowledgeActivities()
```

2. After existing `RegisterActivityWithOptions` calls for steering activities, add:
```go
w.RegisterActivityWithOptions(knowledgeActivities.CaptureKnowledge,
    temporalactivity.RegisterOptions{Name: activity.ActivityCaptureKnowledge})
w.RegisterActivityWithOptions(knowledgeActivities.EnrichPrompt,
    temporalactivity.RegisterOptions{Name: activity.ActivityEnrichPrompt})
```

**Step 3: Build**

```bash
go build ./...
```
Expected: no errors

**Step 4: Commit**

```bash
git add internal/activity/constants.go cmd/worker/main.go
git commit -m "feat(knowledge): register CaptureKnowledge and EnrichPrompt activities"
```

---

### Task 7: Integrate activities into Transform workflow

**Files:**
- Modify: `internal/workflow/transform.go`

**Step 1: Add EnrichPrompt call before initial Claude Code run**

Locate `internal/workflow/transform.go` around line 341 where `prompt := buildPrompt(task)` is called.

Replace:
```go
		prompt := buildPrompt(task)
```

With:
```go
		prompt := buildPrompt(task)

		// Enrich prompt with knowledge from previous runs (non-blocking).
		if task.KnowledgeEnrichEnabled() {
			var transformRepoPath string
			if task.UsesTransformationRepo() {
				transformRepoPath = "/workspace"
			}
			enrichInput := activity.EnrichPromptInput{
				OriginalPrompt:         prompt,
				FilterTags:             task.KnowledgeTags(),
				MaxItems:               task.KnowledgeMaxItems(),
				TransformationRepoPath: transformRepoPath,
			}
			enrichCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
				StartToCloseTimeout: 30 * time.Second,
				RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
			})
			var enrichedPrompt string
			if err := workflow.ExecuteActivity(enrichCtx, activity.ActivityEnrichPrompt, enrichInput).Get(enrichCtx, &enrichedPrompt); err != nil {
				logger.Warn("Prompt enrichment failed, using original prompt", "error", err)
			} else {
				prompt = enrichedPrompt
			}
		}
```

**Note:** You'll need to add `"go.temporal.io/sdk/temporal"` to the imports if not already present.

**Step 2: Add CaptureKnowledge call after approval**

Locate the `break steeringLoop` at line 466. After `}` that closes the entire `if task.RequireApproval && len(filesModified) > 0 {` block (around line 553), add:

```go
	// 5b. Capture knowledge from this transformation (non-blocking, runs after approval).
	if task.KnowledgeCaptureEnabled() && len(steeringState.History) > 0 {
		diffSummary := buildDiffSummary(cachedDiffs)
		captureInput := activity.CaptureKnowledgeInput{
			TaskID:          task.ID,
			OriginalPrompt:  buildPrompt(task),
			SteeringHistory: steeringState.History,
			DiffSummary:     diffSummary,
			VerifiersPassed: true,
		}
		for _, repo := range effectiveRepos {
			captureInput.RepoNames = append(captureInput.RepoNames, repo.Name)
		}
		captureCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 2 * time.Minute,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
		})
		// Fire-and-forget style: ignore result, activity logs warnings internally.
		_ = workflow.ExecuteActivity(captureCtx, activity.ActivityCaptureKnowledge, captureInput).Get(captureCtx, nil)
	}
```

**Step 3: Build**

```bash
go build ./...
```

**Step 4: Run workflow tests**

```bash
go test ./internal/workflow/... -v
```
Expected: PASS (new activities are called with RetryPolicy MaximumAttempts=1, so failures won't block the workflow)

**Step 5: Commit**

```bash
git add internal/workflow/transform.go
git commit -m "feat(knowledge): wire EnrichPrompt and CaptureKnowledge into Transform workflow"
```

---

### Task 8: Add knowledge CLI commands

**Files:**
- Modify: `cmd/cli/main.go`
- Create: `cmd/cli/knowledge.go`

**Step 1: Write the test**

Create `cmd/cli/knowledge_test.go`:

```go
package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
	"time"
)

func TestFormatKnowledgeTable(t *testing.T) {
	items := []model.KnowledgeItem{
		{
			ID:         "abc-001",
			Type:       model.KnowledgeTypeCorrection,
			Summary:    "Run go mod tidy",
			Tags:       []string{"go"},
			Confidence: 0.9,
			CreatedAt:  time.Now(),
		},
	}
	output := formatKnowledgeTable(items)
	assert.Contains(t, output, "abc-001")
	assert.Contains(t, output, "correction")
	assert.Contains(t, output, "Run go mod tidy")
}

func TestKnowledgeAddAndList(t *testing.T) {
	dir := t.TempDir()
	store := knowledge.NewStore(dir)

	item := model.KnowledgeItem{
		ID:         "manual-001",
		Type:       model.KnowledgeTypePattern,
		Summary:    "Always check for nil",
		Source:     model.KnowledgeSourceManual,
		Confidence: 1.0,
		CreatedAt:  time.Now(),
	}
	require.NoError(t, store.Write("manual", item))

	items, err := store.ListAll()
	require.NoError(t, err)
	assert.Len(t, items, 1)
}
```

**Step 2: Run tests to verify they compile**

```bash
go test ./cmd/cli/... -run TestFormatKnowledge -v
```
Expected: FAIL (formatKnowledgeTable not defined yet)

**Step 3: Create knowledge.go in cmd/cli/**

Create `cmd/cli/knowledge.go`:

```go
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
)

var knowledgeCmd = &cobra.Command{
	Use:   "knowledge",
	Short: "Manage knowledge items from past transformations",
}

var knowledgeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List knowledge items",
	RunE:  runKnowledgeList,
}

var knowledgeShowCmd = &cobra.Command{
	Use:   "show <item-id>",
	Short: "Show a knowledge item in detail",
	Args:  cobra.ExactArgs(1),
	RunE:  runKnowledgeShow,
}

var knowledgeAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Manually add a knowledge item",
	RunE:  runKnowledgeAdd,
}

var knowledgeDeleteCmd = &cobra.Command{
	Use:   "delete <item-id>",
	Short: "Delete a knowledge item",
	Args:  cobra.ExactArgs(1),
	RunE:  runKnowledgeDelete,
}

func init() {
	knowledgeListCmd.Flags().String("task-id", "", "Filter by task ID")
	knowledgeListCmd.Flags().String("type", "", "Filter by type (pattern|correction|gotcha|context)")
	knowledgeListCmd.Flags().String("tag", "", "Filter by tag")

	knowledgeAddCmd.Flags().String("summary", "", "One-line summary (required)")
	knowledgeAddCmd.Flags().String("type", "pattern", "Type: pattern|correction|gotcha|context")
	knowledgeAddCmd.Flags().String("details", "", "Longer explanation")
	knowledgeAddCmd.Flags().StringSlice("tags", nil, "Comma-separated tags")
	_ = knowledgeAddCmd.MarkFlagRequired("summary")

	knowledgeCmd.AddCommand(knowledgeListCmd, knowledgeShowCmd, knowledgeAddCmd, knowledgeDeleteCmd)
}

func runKnowledgeList(cmd *cobra.Command, _ []string) error {
	taskID, _ := cmd.Flags().GetString("task-id")
	typeFilter, _ := cmd.Flags().GetString("type")
	tagFilter, _ := cmd.Flags().GetString("tag")

	store := knowledge.DefaultStore()

	var items []model.KnowledgeItem
	var err error
	if taskID != "" {
		items, err = store.List(taskID)
	} else {
		items, err = store.ListAll()
	}
	if err != nil {
		return fmt.Errorf("listing knowledge items: %w", err)
	}

	// Apply filters.
	items = filterKnowledgeItems(items, typeFilter, tagFilter)

	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "No knowledge items found.")
		return nil
	}

	fmt.Print(formatKnowledgeTable(items))
	return nil
}

func runKnowledgeShow(cmd *cobra.Command, args []string) error {
	itemID := args[0]
	store := knowledge.DefaultStore()

	all, err := store.ListAll()
	if err != nil {
		return err
	}
	for _, item := range all {
		if item.ID == itemID {
			fmt.Printf("ID:         %s\n", item.ID)
			fmt.Printf("Type:       %s\n", item.Type)
			fmt.Printf("Source:     %s\n", item.Source)
			fmt.Printf("Confidence: %.2f\n", item.Confidence)
			fmt.Printf("Tags:       %s\n", strings.Join(item.Tags, ", "))
			fmt.Printf("Created:    %s\n", item.CreatedAt.Format(time.RFC3339))
			fmt.Printf("\nSummary:\n  %s\n", item.Summary)
			if item.Details != "" {
				fmt.Printf("\nDetails:\n  %s\n", item.Details)
			}
			if item.CreatedFrom != nil {
				fmt.Printf("\nOrigin:\n  Task: %s\n", item.CreatedFrom.TaskID)
				if item.CreatedFrom.SteeringPrompt != "" {
					fmt.Printf("  Steering: %s\n", item.CreatedFrom.SteeringPrompt)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("knowledge item %q not found", itemID)
}

func runKnowledgeAdd(cmd *cobra.Command, _ []string) error {
	summary, _ := cmd.Flags().GetString("summary")
	typStr, _ := cmd.Flags().GetString("type")
	details, _ := cmd.Flags().GetString("details")
	tags, _ := cmd.Flags().GetStringSlice("tags")

	item := model.KnowledgeItem{
		ID:         uuid.New().String()[:8],
		Type:       model.KnowledgeType(typStr),
		Summary:    summary,
		Details:    details,
		Source:     model.KnowledgeSourceManual,
		Tags:       tags,
		Confidence: 1.0,
		CreatedAt:  time.Now().UTC(),
	}

	store := knowledge.DefaultStore()
	if err := store.Write("manual", item); err != nil {
		return fmt.Errorf("saving knowledge item: %w", err)
	}
	fmt.Printf("Added knowledge item: %s\n", item.ID)
	return nil
}

func runKnowledgeDelete(cmd *cobra.Command, args []string) error {
	itemID := args[0]
	store := knowledge.DefaultStore()
	if err := store.Delete(itemID); err != nil {
		return err
	}
	fmt.Printf("Deleted knowledge item: %s\n", itemID)
	return nil
}

// filterKnowledgeItems filters items by type and/or tag.
func filterKnowledgeItems(items []model.KnowledgeItem, typeFilter, tagFilter string) []model.KnowledgeItem {
	if typeFilter == "" && tagFilter == "" {
		return items
	}
	var out []model.KnowledgeItem
	for _, item := range items {
		if typeFilter != "" && string(item.Type) != typeFilter {
			continue
		}
		if tagFilter != "" {
			matched := false
			for _, t := range item.Tags {
				if strings.EqualFold(t, tagFilter) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, item)
	}
	return out
}

// formatKnowledgeTable formats knowledge items as a plain text table.
// Exported for testing (lowercase in same package is fine).
func formatKnowledgeTable(items []model.KnowledgeItem) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%-10s  %-12s  %-6s  %-8s  %s\n", "ID", "TYPE", "CONF", "TAGS", "SUMMARY"))
	sb.WriteString(strings.Repeat("-", 80) + "\n")
	for _, item := range items {
		tags := strings.Join(item.Tags, ",")
		if len(tags) > 8 {
			tags = tags[:7] + "…"
		}
		summary := item.Summary
		if len(summary) > 50 {
			summary = summary[:49] + "…"
		}
		sb.WriteString(fmt.Sprintf("%-10s  %-12s  %.2f    %-8s  %s\n",
			item.ID, item.Type, item.Confidence, tags, summary))
	}
	return sb.String()
}
```

**Step 4: Register knowledgeCmd in main.go**

In `cmd/cli/main.go`, find where other sub-commands are added to `rootCmd` (look for `rootCmd.AddCommand`). Add:

```go
rootCmd.AddCommand(knowledgeCmd)
```

**Step 5: Run tests and build**

```bash
go test ./cmd/cli/... -run TestFormatKnowledge -v
go test ./cmd/cli/... -run TestKnowledgeAdd -v
go build ./...
```
Expected: PASS

**Step 6: Commit**

```bash
git add cmd/cli/knowledge.go cmd/cli/knowledge_test.go cmd/cli/main.go
git commit -m "feat(knowledge): add knowledge CLI subcommands (list, show, add, delete)"
```

---

### Task 9: Final verification

**Step 1: Run all tests**

```bash
go test ./...
```
Expected: all PASS

**Step 2: Run linter**

```bash
make lint
```
Expected: no errors. Fix any lint issues before continuing.

**Step 3: Build all binaries**

```bash
go build ./...
```

**Step 4: Manual smoke test of CLI**

```bash
./fleetlift knowledge add --summary "Test knowledge item" --type pattern --tags go,test
./fleetlift knowledge list
./fleetlift knowledge show <id-from-above>
./fleetlift knowledge delete <id-from-above>
./fleetlift knowledge list
```
Expected: item added, listed, shown with detail, deleted, empty list.

**Step 5: Commit**

```bash
git add .
git commit -m "test(knowledge): verify full Phase 10 implementation passes lint and tests"
```

---

## Implementation Checklist

- [ ] Task 1: Add Anthropic SDK dependency
- [ ] Task 2: KnowledgeItem/KnowledgeConfig data model
- [ ] Task 3: Add Knowledge field to Task struct + helpers
- [ ] Task 4: knowledge.Store YAML persistence package
- [ ] Task 5: CaptureKnowledge + EnrichPrompt activities
- [ ] Task 6: Register activities in constants.go + worker
- [ ] Task 7: Wire activities into Transform workflow
- [ ] Task 8: `fleetlift knowledge` CLI subcommands
- [ ] Task 9: Final verification (all tests pass, lint clean)

## Out of scope (Phase 10b)

- `fleetlift knowledge review` — interactive TUI for reviewing captured items
- `fleetlift knowledge commit` — copy curated items into transformation repo
- `fleetlift knowledge stats` — efficacy tracking
- Knowledge in the forEach/report-mode path
- Knowledge in TransformV2 (sidecar agent) workflow
