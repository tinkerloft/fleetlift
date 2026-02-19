package activity_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinkerloft/fleetlift/internal/activity"
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
