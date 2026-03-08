# Phase 10b: Knowledge Curation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable humans to curate auto-captured knowledge items before promoting them to transformation repos — via `fleetlift knowledge review` (interactive approve/edit/delete) and `fleetlift knowledge commit --repo PATH` (copy approved items to repo).

**Architecture:** Add a `Status` field to `KnowledgeItem` (pending/approved) and a `Store.Update()` method so the review command can mark items. The `commit` command copies all approved items to `{repo}/.fleetlift/knowledge/items/`. The workflow logs a post-capture hint when items are captured. All changes are backward-compatible (empty status = pending).

**Tech Stack:** Go, Cobra CLI, `gopkg.in/yaml.v3`, `bufio` (stdin prompts), existing `internal/knowledge` + `internal/model` packages.

**Key files:**
- Modify: `internal/model/knowledge.go`
- Modify: `internal/knowledge/store.go` + `internal/knowledge/store_test.go`
- Modify: `cmd/cli/knowledge.go` + `cmd/cli/knowledge_test.go`
- Modify: `internal/workflow/transform_v2.go` (minor — capture CaptureKnowledge result)

---

## Task 1: Add `KnowledgeStatus` to model and `Store.Update` + `Store.ListApproved`

**Files:**
- Modify: `internal/model/knowledge.go`
- Modify: `internal/knowledge/store.go`
- Modify: `internal/knowledge/store_test.go`

### Step 1: Add `KnowledgeStatus` type and `Status` field

In `internal/model/knowledge.go`, after the `KnowledgeSource` constants block, add:

```go
// KnowledgeStatus represents the curation state of a knowledge item.
type KnowledgeStatus string

const (
	// KnowledgeStatusPending is the default state — item awaits review.
	KnowledgeStatusPending KnowledgeStatus = "pending"
	// KnowledgeStatusApproved means the item has been reviewed and approved for promotion.
	KnowledgeStatusApproved KnowledgeStatus = "approved"
)
```

In `KnowledgeItem`, add the `Status` field after `CreatedAt`:

```go
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
	Status      KnowledgeStatus  `json:"status,omitempty" yaml:"status,omitempty"`
}
```

> `omitempty` ensures existing YAML files without a status field are still loaded cleanly (Status defaults to `""`).

### Step 2: Add `Store.Update` and `Store.ListApproved`

In `internal/knowledge/store.go`, add after `Delete`:

```go
// Update finds a knowledge item by ID across all task directories and rewrites it in-place.
// Use this to update Status, Summary, or any other mutable field.
func (s *Store) Update(item model.KnowledgeItem) error {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("item %s not found", item.ID)
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		itemPath := filepath.Join(s.baseDir, entry.Name(), "item-"+item.ID+".yaml")
		if _, err := os.Stat(itemPath); os.IsNotExist(err) {
			continue
		}
		data, err := yaml.Marshal(item)
		if err != nil {
			return fmt.Errorf("marshaling item: %w", err)
		}
		return os.WriteFile(itemPath, data, 0o644)
	}
	return fmt.Errorf("item %s not found", item.ID)
}

// ListApproved returns all knowledge items with Status == KnowledgeStatusApproved.
func (s *Store) ListApproved() ([]model.KnowledgeItem, error) {
	all, err := s.ListAll()
	if err != nil {
		return nil, err
	}
	var approved []model.KnowledgeItem
	for _, item := range all {
		if item.Status == model.KnowledgeStatusApproved {
			approved = append(approved, item)
		}
	}
	return approved, nil
}
```

> These methods follow the same pattern as `Delete` (scan task dirs, match by item ID file).

### Step 3: Write tests for `Update` and `ListApproved`

In `internal/knowledge/store_test.go`, add after `TestStore_FilterByTags_NoFilter_ReturnsAll`:

```go
func TestStore_Update_Status(t *testing.T) {
	dir := t.TempDir()
	store := knowledge.NewStore(dir)

	item := model.KnowledgeItem{
		ID:        "upd-01",
		Type:      model.KnowledgeTypePattern,
		Summary:   "original",
		Source:    model.KnowledgeSourceManual,
		CreatedAt: time.Now(),
	}
	require.NoError(t, store.Write("task-x", item))

	item.Status = model.KnowledgeStatusApproved
	require.NoError(t, store.Update(item))

	items, err := store.List("task-x")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, model.KnowledgeStatusApproved, items[0].Status)
}

func TestStore_Update_NotFound(t *testing.T) {
	store := knowledge.NewStore(t.TempDir())
	err := store.Update(model.KnowledgeItem{ID: "missing"})
	assert.ErrorContains(t, err, "not found")
}

func TestStore_ListApproved(t *testing.T) {
	dir := t.TempDir()
	store := knowledge.NewStore(dir)

	pending := model.KnowledgeItem{ID: "p1", Type: model.KnowledgeTypePattern, Summary: "p", Source: model.KnowledgeSourceManual, CreatedAt: time.Now()}
	approved := model.KnowledgeItem{ID: "a1", Type: model.KnowledgeTypePattern, Summary: "a", Source: model.KnowledgeSourceManual, Status: model.KnowledgeStatusApproved, CreatedAt: time.Now()}

	require.NoError(t, store.Write("t1", pending))
	require.NoError(t, store.Write("t1", approved))

	results, err := store.ListApproved()
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "a1", results[0].ID)
}
```

### Step 4: Run tests

```bash
go test ./internal/knowledge/... -v
```

Expected: all pass including the 3 new tests.

### Step 5: Commit

```bash
git add internal/model/knowledge.go internal/knowledge/store.go internal/knowledge/store_test.go
git commit -m "feat(knowledge): add KnowledgeStatus, Store.Update, Store.ListApproved"
```

---

## Task 2: `fleetlift knowledge review` command

**Files:**
- Modify: `cmd/cli/knowledge.go`
- Modify: `cmd/cli/knowledge_test.go`

### Step 1: Add the `review` subcommand

In `cmd/cli/knowledge.go`, add after the `knowledgeDeleteCmd` variable declaration (and before the `init()` function that wires commands):

```go
var knowledgeReviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Interactively review pending knowledge items (approve/edit/delete)",
	Long: `Review auto-captured knowledge items one by one.
For each item: [a]pprove promotes it, [d]elete removes it, [e]dit lets you update the summary before approving, [s]kip leaves it as pending.

After review, run 'fleetlift knowledge commit --repo <path>' to copy approved items to a repo.`,
	RunE: runKnowledgeReview,
}

func runKnowledgeReview(cmd *cobra.Command, args []string) error {
	taskID, _ := cmd.Flags().GetString("task-id")
	store := knowledge.DefaultStore()

	var items []model.KnowledgeItem
	var err error
	if taskID != "" {
		items, err = store.List(taskID)
	} else {
		items, err = store.ListAll()
	}
	if err != nil {
		return err
	}

	// Filter to pending items only (Status == "" or "pending")
	var pending []model.KnowledgeItem
	for _, item := range items {
		if item.Status == "" || item.Status == model.KnowledgeStatusPending {
			pending = append(pending, item)
		}
	}

	if len(pending) == 0 {
		fmt.Println("No pending knowledge items to review.")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	approved, deleted, skipped := 0, 0, 0

	for i, item := range pending {
		fmt.Printf("\n--- Item %d/%d ---\n", i+1, len(pending))
		fmt.Printf("ID:         %s\n", item.ID)
		fmt.Printf("Type:       %s\n", item.Type)
		fmt.Printf("Confidence: %.2f\n", item.Confidence)
		if len(item.Tags) > 0 {
			fmt.Printf("Tags:       %s\n", strings.Join(item.Tags, ", "))
		}
		fmt.Printf("Summary:    %s\n", item.Summary)
		if item.Details != "" {
			fmt.Printf("Details:    %s\n", item.Details)
		}
		if item.CreatedFrom != nil && item.CreatedFrom.TaskID != "" {
			fmt.Printf("From task:  %s\n", item.CreatedFrom.TaskID)
		}

	prompt:
		fmt.Print("\n[a]pprove / [d]elete / [s]kip / [e]dit summary: ")
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))

		switch line {
		case "a", "approve":
			item.Status = model.KnowledgeStatusApproved
			if err := store.Update(item); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to approve %s: %v\n", item.ID, err)
			} else {
				approved++
			}
		case "d", "delete":
			if err := store.Delete(item.ID); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to delete %s: %v\n", item.ID, err)
			} else {
				deleted++
			}
		case "s", "skip", "":
			skipped++
		case "e", "edit":
			fmt.Printf("New summary (leave blank to cancel): ")
			newSummary, _ := reader.ReadString('\n')
			newSummary = strings.TrimSpace(newSummary)
			if newSummary == "" {
				goto prompt
			}
			item.Summary = newSummary
			item.Status = model.KnowledgeStatusApproved
			if err := store.Update(item); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to update %s: %v\n", item.ID, err)
			} else {
				approved++
			}
		default:
			fmt.Println("Please enter a, d, s, or e.")
			goto prompt
		}
	}

	fmt.Printf("\nReview complete: %d approved, %d deleted, %d skipped\n", approved, deleted, skipped)
	if approved > 0 {
		fmt.Println("Run 'fleetlift knowledge commit --repo <path>' to promote approved items to a repo.")
	}
	return nil
}
```

**Required imports** — add `bufio` and ensure `os` is imported (check existing imports in the file; `bufio` may need to be added).

### Step 2: Register `knowledgeReviewCmd` in `init()`

In `cmd/cli/knowledge.go`, in the `init()` function where other subcommands are added to `knowledgeCmd`, add:

```go
knowledgeCmd.AddCommand(knowledgeReviewCmd)
knowledgeReviewCmd.Flags().String("task-id", "", "Filter review to a specific task ID")
```

### Step 3: Run tests to verify it compiles

```bash
go build ./cmd/cli/... 2>&1
```

Expected: no errors.

### Step 4: Write a unit test for the pending-filter logic

Add to `cmd/cli/knowledge_test.go`:

```go
func TestKnowledgeReview_PendingFilter(t *testing.T) {
	dir := t.TempDir()
	store := knowledge.NewStore(dir)

	pending := model.KnowledgeItem{ID: "pend-01", Type: model.KnowledgeTypePattern, Summary: "pending item", Source: model.KnowledgeSourceAutoCaptured, CreatedAt: time.Now()}
	approved := model.KnowledgeItem{ID: "appr-01", Type: model.KnowledgeTypePattern, Summary: "approved item", Source: model.KnowledgeSourceAutoCaptured, Status: model.KnowledgeStatusApproved, CreatedAt: time.Now()}

	require.NoError(t, store.Write("task-1", pending))
	require.NoError(t, store.Write("task-1", approved))

	all, err := store.ListAll()
	require.NoError(t, err)

	var pendingItems []model.KnowledgeItem
	for _, item := range all {
		if item.Status == "" || item.Status == model.KnowledgeStatusPending {
			pendingItems = append(pendingItems, item)
		}
	}
	assert.Len(t, pendingItems, 1)
	assert.Equal(t, "pend-01", pendingItems[0].ID)
}
```

### Step 5: Run all CLI tests

```bash
go test ./cmd/cli/... -v
```

Expected: all pass.

### Step 6: Commit

```bash
git add cmd/cli/knowledge.go cmd/cli/knowledge_test.go
git commit -m "feat(cli): add 'knowledge review' interactive curation command"
```

---

## Task 3: `fleetlift knowledge commit` command

**Files:**
- Modify: `cmd/cli/knowledge.go`
- Modify: `cmd/cli/knowledge_test.go`

### Step 1: Add `commit` subcommand

In `cmd/cli/knowledge.go`, add after `knowledgeReviewCmd`:

```go
var knowledgeCommitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Copy approved knowledge items into a transformation repo",
	Long: `Copies all approved knowledge items from the local store (~/.fleetlift/knowledge)
into the repo's knowledge directory ({repo}/.fleetlift/knowledge/items/).

Items must be approved first via 'fleetlift knowledge review'.`,
	RunE: runKnowledgeCommit,
}

func runKnowledgeCommit(cmd *cobra.Command, args []string) error {
	repoPath, _ := cmd.Flags().GetString("repo")
	if repoPath == "" {
		return fmt.Errorf("--repo is required: specify the path to the transformation repo")
	}

	store := knowledge.DefaultStore()
	approved, err := store.ListApproved()
	if err != nil {
		return fmt.Errorf("listing approved items: %w", err)
	}

	if len(approved) == 0 {
		fmt.Println("No approved knowledge items found.")
		fmt.Println("Run 'fleetlift knowledge review' to review and approve items first.")
		return nil
	}

	destDir := filepath.Join(repoPath, ".fleetlift", "knowledge", "items")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("creating destination directory: %w", err)
	}

	committed := 0
	for _, item := range approved {
		data, err := yaml.Marshal(item)
		if err != nil {
			return fmt.Errorf("marshaling item %s: %w", item.ID, err)
		}
		destPath := filepath.Join(destDir, "item-"+item.ID+".yaml")
		if err := os.WriteFile(destPath, data, 0o644); err != nil {
			return fmt.Errorf("writing item %s: %w", item.ID, err)
		}
		committed++
	}

	fmt.Printf("Committed %d knowledge item(s) to %s\n", committed, destDir)
	return nil
}
```

**Imports needed:** `path/filepath`, `gopkg.in/yaml.v3` (check existing imports — `filepath` is likely present; `yaml` is used in the store but may not be in the CLI file yet). Add as needed.

### Step 2: Register `knowledgeCommitCmd` in `init()`

```go
knowledgeCmd.AddCommand(knowledgeCommitCmd)
knowledgeCommitCmd.Flags().String("repo", "", "Path to the transformation repo (required)")
```

### Step 3: Run build check

```bash
go build ./cmd/cli/... 2>&1
```

### Step 4: Write tests for `commit`

Add to `cmd/cli/knowledge_test.go`:

```go
func TestRunKnowledgeCommit_NoApprovedItems(t *testing.T) {
	dir := t.TempDir()
	store := knowledge.NewStore(dir)

	// Write a pending item only
	item := model.KnowledgeItem{ID: "pend-01", Type: model.KnowledgeTypePattern, Summary: "pending", Source: model.KnowledgeSourceManual, CreatedAt: time.Now()}
	require.NoError(t, store.Write("t1", item))

	approved, err := store.ListApproved()
	require.NoError(t, err)
	assert.Empty(t, approved)
}

func TestRunKnowledgeCommit_CopiesApprovedToRepo(t *testing.T) {
	dir := t.TempDir()
	store := knowledge.NewStore(dir)
	repoDir := t.TempDir()

	item := model.KnowledgeItem{
		ID:        "appr-01",
		Type:      model.KnowledgeTypePattern,
		Summary:   "use structured logging",
		Source:    model.KnowledgeSourceAutoCaptured,
		Status:    model.KnowledgeStatusApproved,
		CreatedAt: time.Now(),
	}
	require.NoError(t, store.Write("t1", item))

	approved, err := store.ListApproved()
	require.NoError(t, err)
	require.Len(t, approved, 1)

	// Simulate what commit does: write to repo
	destDir := filepath.Join(repoDir, ".fleetlift", "knowledge", "items")
	require.NoError(t, os.MkdirAll(destDir, 0o755))

	data, err := yaml.Marshal(approved[0])
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(destDir, "item-appr-01.yaml"), data, 0o644))

	// Verify it's readable via LoadFromRepo
	loaded, err := knowledge.LoadFromRepo(repoDir)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "appr-01", loaded[0].ID)
	assert.Equal(t, "use structured logging", loaded[0].Summary)
}
```

**Note:** The test imports `path/filepath`, `os`, `gopkg.in/yaml.v3`, `github.com/tinkerloft/fleetlift/internal/knowledge`. Add these to the import block if not already present.

### Step 5: Run all CLI tests

```bash
go test ./cmd/cli/... -v
```

Expected: all pass.

### Step 6: Commit

```bash
git add cmd/cli/knowledge.go cmd/cli/knowledge_test.go
git commit -m "feat(cli): add 'knowledge commit' command to promote approved items to repo"
```

---

## Task 4: Post-capture workflow log

**Files:**
- Modify: `internal/workflow/transform_v2.go`

The workflow currently calls `CaptureKnowledge` and discards the result. This task logs a user-visible hint when items are captured.

### Step 1: Capture the result in `transform_v2.go`

Find the `CaptureKnowledge` call (around line 372 after Task 3 of the previous plan). Change:

```go
if err := workflow.ExecuteActivity(captureCtx, activity.ActivityCaptureKnowledge, captureInput).Get(captureCtx, nil); err != nil {
    logger.Warn("TransformV2: CaptureKnowledge failed (non-blocking)", "error", err)
}
```

To:

```go
var capturedItems []model.KnowledgeItem
if err := workflow.ExecuteActivity(captureCtx, activity.ActivityCaptureKnowledge, captureInput).Get(captureCtx, &capturedItems); err != nil {
    logger.Warn("TransformV2: CaptureKnowledge failed (non-blocking)", "error", err)
} else if len(capturedItems) > 0 {
    logger.Info("TransformV2: knowledge captured — run 'fleetlift knowledge review' to curate",
        "count", len(capturedItems),
        "task_id", task.ID,
    )
}
```

### Step 2: Run all workflow tests

```bash
go test ./internal/workflow/... -v
```

Expected: all 13 tests pass. The existing mock for `CaptureKnowledge` returns `nil, nil` — this binds cleanly to `&capturedItems` (nil slice, len 0, no log message). ✓

### Step 3: Run full suite + lint + build

```bash
go test ./... && make lint && go build ./...
```

Expected: all pass.

### Step 4: Update docs

In `docs/plans/ROADMAP.md`, find the Phase 10b section and mark the completed items:

```markdown
- [x] `fleetlift knowledge review [--task-id ID]` — interactive TUI; approve/edit/delete items before promotion to Tier 3
- [x] `fleetlift knowledge commit [--repo PATH]` — copy approved items into `.fleetlift/knowledge/` in a transformation repo
- [x] Post-approval CLI log: "N knowledge items captured. Run `fleetlift knowledge review` to curate."
- [ ] Grouped execution wiring: single-group path done; multi-group path needs knowledge capture per-group contributing to shared pool
- [ ] Efficacy tracking: add `times_used`, `success_rate` fields to `KnowledgeItem`; `fleetlift knowledge stats` command; auto-deprecate items with low confidence after N uses with no improvement
```

### Step 5: Commit

```bash
git add internal/workflow/transform_v2.go docs/plans/ROADMAP.md
git commit -m "feat(workflow): log post-capture hint; docs: mark Phase 10b implemented items complete"
```

---

## Unanswered Questions

1. **`yaml` import in `cmd/cli/knowledge.go`**: The `knowledge commit` command uses `yaml.Marshal`. Check whether `gopkg.in/yaml.v3` is already imported in that file (it may not be since the CLI doesn't marshal YAML). If not, add `"gopkg.in/yaml.v3"` to imports.

2. **`bufio` import in `cmd/cli/knowledge.go`**: The `knowledge review` command uses `bufio.NewReader`. Check whether it's already imported; add if not.

3. **`filepath` import in `cmd/cli/knowledge_test.go`**: The commit test uses `filepath.Join`. Add if not present.

4. **Existing items after workflow wiring (Phase 10a)**: Items already in `~/.fleetlift/knowledge/` from runs before this change will have `Status: ""` — they are treated as pending by the review command (the `item.Status == ""` guard handles this correctly).
