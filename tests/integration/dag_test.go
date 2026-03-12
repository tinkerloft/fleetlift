//go:build integration

package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

func TestDAGWorkflow_HappyPath(t *testing.T) {
	suite := testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterWorkflow(workflow.DAGWorkflow)
	env.RegisterWorkflow(workflow.StepWorkflow)

	env.OnActivity("ProvisionSandbox").Return("test-sandbox", nil)
	env.OnActivity("ExecuteStep").Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
	}, nil)
	env.OnActivity("UpdateStepStatus").Return(nil)
	env.OnActivity("CleanupSandbox").Return(nil)

	def := model.WorkflowDef{
		ID:    "test-wf",
		Title: "Test Workflow",
		Steps: []model.StepDef{
			{
				ID:    "step-1",
				Title: "Test Step",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something",
				},
			},
		},
	}

	env.ExecuteWorkflow(workflow.DAGWorkflow, workflow.DAGInput{
		RunID:       "run-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
}

func TestDAGWorkflow_ConditionalStepSkipped(t *testing.T) {
	suite := testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterWorkflow(workflow.DAGWorkflow)
	env.RegisterWorkflow(workflow.StepWorkflow)

	env.OnActivity("ProvisionSandbox").Return("test-sandbox", nil)
	env.OnActivity("ExecuteStep").Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
	}, nil)
	env.OnActivity("UpdateStepStatus").Return(nil)
	env.OnActivity("CleanupSandbox").Return(nil)

	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{ID: "step-1", Execution: &model.ExecutionDef{Agent: "claude-code", Prompt: "first"}},
			{
				ID:        "step-2",
				Condition: `{{eq (index .steps "step-1").status "failed"}}`,
				Execution: &model.ExecutionDef{Agent: "claude-code", Prompt: "second"},
				DependsOn: []string{"step-1"},
			},
		},
	}

	env.ExecuteWorkflow(workflow.DAGWorkflow, workflow.DAGInput{
		RunID:       "run-2",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
}
