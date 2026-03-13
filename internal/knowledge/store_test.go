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
