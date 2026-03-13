package activity_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/activity"
	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

func TestCaptureKnowledge_ParsesFile(t *testing.T) {
	items := []map[string]any{
		{"type": "pattern", "summary": "use defer for cleanup", "confidence": 0.9},
	}
	data, _ := json.Marshal(items)

	sb := sandbox.NewMemoryClient(map[string][]byte{
		"fleetlift-knowledge.json": data,
	})
	store := knowledge.NewMemoryStore()
	acts := &activity.Activities{
		Sandbox:        sb,
		KnowledgeStore: store,
	}

	err := acts.CaptureKnowledge(context.Background(), model.CaptureKnowledgeInput{
		SandboxID:          "sandbox-1",
		TeamID:             "team-1",
		WorkflowTemplateID: "wf-1",
		StepRunID:          "step-1",
	})
	require.NoError(t, err)

	saved, _ := store.ListByTeam(context.Background(), "team-1", "")
	assert.Len(t, saved, 1)
	assert.Equal(t, "use defer for cleanup", saved[0].Summary)
}
