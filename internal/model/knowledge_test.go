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
		Source:     model.KnowledgeSourceAutoCaptured,
		Tags:       []string{"go", "imports"},
		Confidence: 0.9,
		CreatedAt:  time.Now(),
	}
	assert.Equal(t, model.KnowledgeTypeCorrection, item.Type)
	assert.Equal(t, model.KnowledgeSourceAutoCaptured, item.Source)
	assert.Len(t, item.Tags, 2)
}

func TestKnowledgeDef_Defaults(t *testing.T) {
	def := model.KnowledgeDef{}
	assert.False(t, def.Capture)
	assert.False(t, def.Enrich)
	assert.Equal(t, 0, def.MaxItems) // 0 means use default (10)
}
