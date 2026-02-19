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

func TestStore_FilterByTags_NoFilter_ReturnsAll(t *testing.T) {
	dir := t.TempDir()
	store := knowledge.NewStore(dir)

	item1 := model.KnowledgeItem{ID: "a1", Type: model.KnowledgeTypePattern, Summary: "a", Source: model.KnowledgeSourceManual, Confidence: 0.9, CreatedAt: time.Now()}
	item2 := model.KnowledgeItem{ID: "a2", Type: model.KnowledgeTypePattern, Summary: "b", Source: model.KnowledgeSourceManual, Confidence: 0.7, CreatedAt: time.Now()}
	require.NoError(t, store.Write("t1", item1))
	require.NoError(t, store.Write("t1", item2))

	results, err := store.FilterByTags(nil, 10)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	// Should be sorted by confidence descending
	assert.Equal(t, "a1", results[0].ID)
}

func TestStore_DefaultBaseDir(t *testing.T) {
	store := knowledge.DefaultStore()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".fleetlift", "knowledge")
	assert.Equal(t, expected, store.BaseDir())
}

func TestLoadFromRepo_MissingDir(t *testing.T) {
	items, err := knowledge.LoadFromRepo(t.TempDir()) // no .fleetlift/knowledge/items/
	require.NoError(t, err)
	assert.Empty(t, items)
}
