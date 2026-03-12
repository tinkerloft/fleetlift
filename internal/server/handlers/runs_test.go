package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStepWorkflowID(t *testing.T) {
	runID := "run-abc123"
	stepID := "analyze"
	got := stepWorkflowID(runID, stepID)
	assert.Equal(t, "run-abc123-analyze", got)
}
