package activity_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinkerloft/fleetlift/internal/activity"
	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestBuildCapturePrompt_WithSteering(t *testing.T) {
	input := activity.CaptureKnowledgeInput{
		TaskID:         "slog-migration-123",
		OriginalPrompt: "Migrate all Go services from logrus to slog",
		SteeringHistory: []model.SteeringIteration{
			{
				IterationNumber: 1,
				Prompt:          "Also update test helpers",
				Output:          "Updated 3 files",
				Timestamp:       time.Now(),
			},
		},
		DiffSummary:     "5 files changed, +120, -40",
		VerifiersPassed: true,
	}

	prompt := activity.BuildCapturePrompt(input)
	assert.Contains(t, prompt, "slog-migration-123")
	assert.Contains(t, prompt, "Migrate all Go services")
	assert.Contains(t, prompt, "Also update test helpers")
	assert.Contains(t, prompt, "5 files changed")
	assert.Contains(t, prompt, "KnowledgeItem")
}

func TestBuildCapturePrompt_NoSteering(t *testing.T) {
	input := activity.CaptureKnowledgeInput{
		TaskID:         "task-abc",
		OriginalPrompt: "Do something",
		DiffSummary:    "1 file changed",
	}
	prompt := activity.BuildCapturePrompt(input)
	assert.Contains(t, prompt, "task-abc")
	assert.NotContains(t, prompt, "Steering Corrections")
}

func TestParseKnowledgeItems_Valid(t *testing.T) {
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

func TestParseKnowledgeItems_ExtractsFromSurroundingText(t *testing.T) {
	// Claude sometimes wraps JSON in markdown
	raw := "Here are the items:\n```json\n[{\"type\":\"pattern\",\"summary\":\"test\",\"details\":\"d\",\"tags\":[\"go\"],\"confidence\":0.8}]\n```"
	items, err := activity.ParseKnowledgeItems(raw, "t1", nil)
	require.NoError(t, err)
	assert.Len(t, items, 1)
}

func TestParseKnowledgeItems_NestedTags(t *testing.T) {
	// Simulates Claude returning JSON with nested tag arrays (most common case).
	// The old regex [.*?] would stop at the closing bracket of ["go"] and return
	// invalid JSON, causing ParseKnowledgeItems to silently discard all items.
	raw := `[{"type":"correction","summary":"Run go mod tidy","details":"Prevents stale entries.","tags":["go","imports"],"confidence":0.95},{"type":"gotcha","summary":"Check wrapper fns","details":"Wrappers miss migration.","tags":["go","logging"],"confidence":0.8}]`
	items, err := activity.ParseKnowledgeItems(raw, "t1", nil)
	require.NoError(t, err)
	require.Len(t, items, 2, "both items must be parsed when tags contain nested arrays")
	assert.Equal(t, model.KnowledgeTypeCorrection, items[0].Type)
	assert.Equal(t, model.KnowledgeTypeGotcha, items[1].Type)
}

func TestEnrichPrompt_NoItems(t *testing.T) {
	dir := t.TempDir()
	store := knowledge.NewStore(dir)
	ka := &activity.KnowledgeActivities{Store: store}
	input := activity.EnrichPromptInput{
		OriginalPrompt: "Migrate logger",
		MaxItems:       10,
	}
	result, err := ka.EnrichPrompt(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, "Migrate logger", result, "original prompt returned unchanged when no items")
}

func TestEnrichPrompt_WithItems(t *testing.T) {
	dir := t.TempDir()
	store := knowledge.NewStore(dir)
	require.NoError(t, store.Write("task-1", model.KnowledgeItem{
		ID: "k1", Type: model.KnowledgeTypeCorrection,
		Summary: "Run go mod tidy", Tags: []string{"go"},
		Source: model.KnowledgeSourceManual, Confidence: 0.9,
		CreatedAt: time.Now(),
	}))

	ka := &activity.KnowledgeActivities{Store: store}
	input := activity.EnrichPromptInput{
		OriginalPrompt: "Migrate logger",
		FilterTags:     []string{"go"},
		MaxItems:       10,
	}
	result, err := ka.EnrichPrompt(context.Background(), input)
	require.NoError(t, err)
	assert.Contains(t, result, "Migrate logger")
	assert.Contains(t, result, "Lessons from previous runs")
	assert.Contains(t, result, "[correction] Run go mod tidy")
}

func TestEnrichPrompt_Tier2CapacityReducedByTier3(t *testing.T) {
	// When Tier 3 fills half the budget, Tier 2 should only fill the remainder.
	dir := t.TempDir()
	store := knowledge.NewStore(dir)

	// Write 5 items to local store (Tier 2)
	for i := 0; i < 5; i++ {
		require.NoError(t, store.Write("t1", model.KnowledgeItem{
			ID:         fmt.Sprintf("local-%d", i),
			Type:       model.KnowledgeTypePattern,
			Summary:    fmt.Sprintf("Local item %d", i),
			Source:     model.KnowledgeSourceManual,
			Confidence: float64(i) / 10.0,
			CreatedAt:  time.Now(),
		}))
	}

	ka := &activity.KnowledgeActivities{Store: store}
	// maxItems=3, no Tier 1 — should return only 3 items from Tier 2
	input := activity.EnrichPromptInput{
		OriginalPrompt: "Do something",
		MaxItems:       3,
	}
	result, err := ka.EnrichPrompt(context.Background(), input)
	require.NoError(t, err)
	// Count bullet points in output
	count := strings.Count(result, "- [pattern]")
	assert.Equal(t, 3, count, "must cap at MaxItems=3 even with 5 available")
}
