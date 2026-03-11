package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// TestStepInput_Compile verifies that StepInput and related types compile and can be constructed.
func TestStepInput_Compile(t *testing.T) {
	input := StepInput{
		RunID:     "run-123",
		StepRunID: "step-run-456",
		StepDef: model.StepDef{
			ID:             "analyze",
			Mode:           "report",
			ApprovalPolicy: "never",
		},
		ResolvedOpts: ResolvedStepOpts{
			Prompt: "Analyze the code",
			Agent:  "claude-code",
		},
		SandboxID: "sandbox-789",
	}

	assert.Equal(t, "run-123", input.RunID)
	assert.Equal(t, "analyze", input.StepDef.ID)
	assert.Equal(t, "claude-code", input.ResolvedOpts.Agent)
	assert.Equal(t, "sandbox-789", input.SandboxID)
}

func TestStepSignals(t *testing.T) {
	assert.Equal(t, StepSignal("approve"), SignalApprove)
	assert.Equal(t, StepSignal("reject"), SignalReject)
	assert.Equal(t, StepSignal("steer"), SignalSteer)
	assert.Equal(t, StepSignal("cancel"), SignalCancel)
}

func TestExecuteStepInput_Compile(t *testing.T) {
	input := ExecuteStepInput{
		StepInput: StepInput{
			RunID: "run-1",
			StepDef: model.StepDef{
				ID: "transform",
			},
		},
		SandboxID:           "sb-1",
		Prompt:              "Fix the bug",
		ConversationHistory: "previous context",
	}

	assert.Equal(t, "sb-1", input.SandboxID)
	assert.Equal(t, "Fix the bug", input.Prompt)
	assert.Equal(t, "previous context", input.ConversationHistory)
}
