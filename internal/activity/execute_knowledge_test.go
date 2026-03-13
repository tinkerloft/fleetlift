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
