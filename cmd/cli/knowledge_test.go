package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
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

func TestRunKnowledgeCommit_NoApprovedItems(t *testing.T) {
	dir := t.TempDir()
	store := knowledge.NewStore(dir)

	// Write a pending item only (no approved items)
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

	// Simulate what commit does: marshal and write to repo
	destDir := filepath.Join(repoDir, ".fleetlift", "knowledge", "items")
	require.NoError(t, os.MkdirAll(destDir, 0o755))

	data, err := yaml.Marshal(approved[0])
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(destDir, "item-appr-01.yaml"), data, 0o644))

	// Verify it's readable via LoadFromRepo (the Tier 3 reader)
	loaded, err := knowledge.LoadFromRepo(repoDir)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "appr-01", loaded[0].ID)
	assert.Equal(t, "use structured logging", loaded[0].Summary)
}
