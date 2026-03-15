package knowledge_test

import (
	"context"
	"testing"

	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestStore_SaveAndList(t *testing.T) {
	store := knowledge.NewMemoryStore()

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

	err := store.UpdateStatus(context.Background(), saved.ID, "team-1", model.KnowledgeStatusApproved)
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

func TestMemoryStore_SearchByTeam(t *testing.T) {
	store := knowledge.NewMemoryStore()
	ctx := context.Background()

	_, _ = store.Save(ctx, model.KnowledgeItem{
		TeamID: "team-1", Summary: "React migration requires babel config",
		Tags: pq.StringArray{"react", "migration"}, Status: model.KnowledgeStatusApproved, Confidence: 0.9,
	})
	_, _ = store.Save(ctx, model.KnowledgeItem{
		TeamID: "team-1", Summary: "Go tests need -race flag",
		Tags: pq.StringArray{"go", "testing"}, Status: model.KnowledgeStatusApproved, Confidence: 0.8,
	})
	_, _ = store.Save(ctx, model.KnowledgeItem{
		TeamID: "team-1", Summary: "Pending item about React",
		Tags: pq.StringArray{"react"}, Status: model.KnowledgeStatusPending, Confidence: 0.7,
	})
	_, _ = store.Save(ctx, model.KnowledgeItem{
		TeamID: "team-2", Summary: "Other team React item",
		Tags: pq.StringArray{"react"}, Status: model.KnowledgeStatusApproved, Confidence: 0.9,
	})

	t.Run("search by query", func(t *testing.T) {
		items, err := store.SearchByTeam(ctx, "team-1", "React", nil, 10)
		require.NoError(t, err)
		assert.Len(t, items, 1)
		assert.Equal(t, "React migration requires babel config", items[0].Summary)
	})

	t.Run("search by tags", func(t *testing.T) {
		items, err := store.SearchByTeam(ctx, "team-1", "", []string{"go"}, 10)
		require.NoError(t, err)
		assert.Len(t, items, 1)
		assert.Equal(t, "Go tests need -race flag", items[0].Summary)
	})

	t.Run("max_items", func(t *testing.T) {
		items, err := store.SearchByTeam(ctx, "team-1", "", nil, 1)
		require.NoError(t, err)
		assert.Len(t, items, 1)
	})

	t.Run("team isolation", func(t *testing.T) {
		items, err := store.SearchByTeam(ctx, "team-2", "React", nil, 10)
		require.NoError(t, err)
		assert.Len(t, items, 1)
		assert.Equal(t, "Other team React item", items[0].Summary)
	})

	t.Run("default maxItems when zero", func(t *testing.T) {
		items, err := store.SearchByTeam(ctx, "team-1", "", nil, 0)
		require.NoError(t, err)
		assert.Len(t, items, 2) // both approved team-1 items
	})

	t.Run("query and tags combined", func(t *testing.T) {
		items, err := store.SearchByTeam(ctx, "team-1", "React", []string{"migration"}, 10)
		require.NoError(t, err)
		assert.Len(t, items, 1)
		assert.Equal(t, "React migration requires babel config", items[0].Summary)
	})

	t.Run("no results for non-matching query and tags", func(t *testing.T) {
		items, err := store.SearchByTeam(ctx, "team-1", "React", []string{"go"}, 10)
		require.NoError(t, err)
		assert.Len(t, items, 0)
	})
}
