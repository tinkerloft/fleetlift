package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
