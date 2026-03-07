package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	agentboxproto "github.com/tinkerloft/agentbox/protocol"

	"github.com/tinkerloft/fleetlift/internal/activity"
	"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// AgentMockActivities provides mock implementations of agent activities
type AgentMockActivities struct {
	mock.Mock
}

func (m *AgentMockActivities) ProvisionAgentSandbox(ctx context.Context, input activity.ProvisionAgentSandboxInput) (*model.SandboxInfo, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.SandboxInfo), args.Error(1)
}

func (m *AgentMockActivities) SubmitTaskManifest(ctx context.Context, input activity.SubmitTaskManifestInput) error {
	args := m.Called(ctx, input)
	return args.Error(0)
}

func (m *AgentMockActivities) WaitForAgentPhase(ctx context.Context, input activity.WaitForAgentPhaseInput) (*agentboxproto.AgentStatus, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*agentboxproto.AgentStatus), args.Error(1)
}

func (m *AgentMockActivities) ReadAgentResult(ctx context.Context, input activity.ReadAgentResultInput) (*fleetproto.AgentResult, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*fleetproto.AgentResult), args.Error(1)
}

func (m *AgentMockActivities) SubmitSteeringAction(ctx context.Context, input activity.SubmitSteeringActionInput) error {
	args := m.Called(ctx, input)
	return args.Error(0)
}

func (m *AgentMockActivities) CleanupSandbox(ctx context.Context, containerID string) error {
	args := m.Called(ctx, containerID)
	return args.Error(0)
}

func (m *AgentMockActivities) NotifySlack(ctx context.Context, channel, message string, threadTS *string) (*string, error) {
	args := m.Called(ctx, channel, message, threadTS)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*string), args.Error(1)
}

func (m *AgentMockActivities) EnrichPrompt(ctx context.Context, input activity.EnrichPromptInput) (string, error) {
	args := m.Called(ctx, input)
	return args.String(0), args.Error(1)
}

func (m *AgentMockActivities) CaptureKnowledge(ctx context.Context, input activity.CaptureKnowledgeInput) ([]model.KnowledgeItem, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]model.KnowledgeItem), args.Error(1)
}

func TestTransformV2_HappyPath(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	task := model.Task{
		Version: 1,
		ID:      "test-task",
		Title:   "Test Task",
		Mode:    model.TaskModeTransform,
		Repositories: []model.Repository{
			{URL: "https://github.com/org/svc.git", Branch: "main", Name: "svc"},
		},
		Execution: model.Execution{
			Agentic: &model.AgenticExecution{
				Prompt: "Fix the bug",
			},
		},
	}

	sandboxInfo := &model.SandboxInfo{ContainerID: "container-123", WorkspacePath: "/workspace"}

	// Create and register mock activities
	mockActivities := &AgentMockActivities{}
	env.RegisterActivity(mockActivities.ProvisionAgentSandbox)
	env.RegisterActivity(mockActivities.SubmitTaskManifest)
	env.RegisterActivity(mockActivities.WaitForAgentPhase)
	env.RegisterActivity(mockActivities.ReadAgentResult)
	env.RegisterActivity(mockActivities.CleanupSandbox)
	env.RegisterActivity(mockActivities.EnrichPrompt)
	env.RegisterActivity(mockActivities.CaptureKnowledge)

	// Set up expectations
	mockActivities.On("ProvisionAgentSandbox", mock.Anything, mock.Anything).Return(sandboxInfo, nil)
	mockActivities.On("SubmitTaskManifest", mock.Anything, mock.Anything).Return(nil)
	mockActivities.On("WaitForAgentPhase", mock.Anything, mock.Anything).Return(
		&agentboxproto.AgentStatus{Phase: agentboxproto.PhaseComplete, Message: "done"}, nil,
	)
	mockActivities.On("ReadAgentResult", mock.Anything, mock.Anything).Return(
		&fleetproto.AgentResult{
			Status: agentboxproto.PhaseComplete,
			Repositories: []fleetproto.RepoResult{
				{
					Name:          "svc",
					Status:        "success",
					FilesModified: []string{"main.go"},
					PullRequest:   &fleetproto.PRInfo{URL: "https://github.com/org/svc/pull/1", Title: "Fix the bug"},
				},
			},
		}, nil,
	)
	mockActivities.On("CleanupSandbox", mock.Anything, "container-123").Return(nil)
	mockActivities.On("EnrichPrompt", mock.Anything, mock.Anything).Return("", nil).Maybe()
	mockActivities.On("CaptureKnowledge", mock.Anything, mock.Anything).Return(nil, nil).Maybe()

	env.ExecuteWorkflow(TransformV2, task)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.TaskResult
	require.NoError(t, env.GetWorkflowResult(&result))

	assert.Equal(t, model.TaskStatusCompleted, result.Status)
	assert.Equal(t, "test-task", result.TaskID)
	require.Len(t, result.Repositories, 1)
	assert.Equal(t, "svc", result.Repositories[0].Repository)
	assert.Equal(t, "success", result.Repositories[0].Status)
	assert.NotNil(t, result.Repositories[0].PullRequest)
	assert.Equal(t, "https://github.com/org/svc/pull/1", result.Repositories[0].PullRequest.PRURL)
}

func TestTransformV2_EnrichPrompt_EnrichesManifest(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	task := model.Task{
		Version: 1,
		ID:      "enrich-test",
		Title:   "Enrich Test",
		Mode:    model.TaskModeTransform,
		Repositories: []model.Repository{
			{URL: "https://github.com/org/svc.git", Branch: "main", Name: "svc"},
		},
		Execution: model.Execution{
			Agentic: &model.AgenticExecution{Prompt: "Fix the bug"},
		},
	}

	sandboxInfo := &model.SandboxInfo{ContainerID: "container-enrich", WorkspacePath: "/workspace"}
	enrichedPrompt := "Fix the bug\n\n---\n## Lessons from previous runs\n\n- [pattern] Use structured logging\n"

	mockActivities := &AgentMockActivities{}
	env.RegisterActivity(mockActivities.ProvisionAgentSandbox)
	env.RegisterActivity(mockActivities.SubmitTaskManifest)
	env.RegisterActivity(mockActivities.WaitForAgentPhase)
	env.RegisterActivity(mockActivities.ReadAgentResult)
	env.RegisterActivity(mockActivities.CleanupSandbox)
	env.RegisterActivity(mockActivities.EnrichPrompt)
	env.RegisterActivity(mockActivities.CaptureKnowledge)

	// EnrichPrompt is called with the original prompt and returns enriched version
	mockActivities.On("EnrichPrompt", mock.Anything, mock.MatchedBy(func(input activity.EnrichPromptInput) bool {
		return input.OriginalPrompt == "Fix the bug"
	})).Return(enrichedPrompt, nil)

	mockActivities.On("ProvisionAgentSandbox", mock.Anything, mock.Anything).Return(sandboxInfo, nil)

	// SubmitTaskManifest must receive the enriched prompt
	mockActivities.On("SubmitTaskManifest", mock.Anything, mock.MatchedBy(func(input activity.SubmitTaskManifestInput) bool {
		return input.Manifest.Execution.Prompt == enrichedPrompt
	})).Return(nil)

	mockActivities.On("WaitForAgentPhase", mock.Anything, mock.Anything).Return(
		&agentboxproto.AgentStatus{Phase: agentboxproto.PhaseComplete}, nil,
	)
	mockActivities.On("ReadAgentResult", mock.Anything, mock.Anything).Return(
		&fleetproto.AgentResult{Status: agentboxproto.PhaseComplete}, nil,
	)
	mockActivities.On("CleanupSandbox", mock.Anything, "container-enrich").Return(nil)
	mockActivities.On("CaptureKnowledge", mock.Anything, mock.Anything).Return(nil, nil).Maybe()

	env.ExecuteWorkflow(TransformV2, task)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	mockActivities.AssertExpectations(t)
}

func TestTransformV2_AgentFailed(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	task := model.Task{
		Version: 1,
		ID:      "failing-task",
		Title:   "Failing Task",
		Repositories: []model.Repository{
			{URL: "https://github.com/org/svc.git", Name: "svc"},
		},
		Execution: model.Execution{
			Agentic: &model.AgenticExecution{Prompt: "break things"},
		},
	}

	sandboxInfo := &model.SandboxInfo{ContainerID: "container-456", WorkspacePath: "/workspace"}

	mockActivities := &AgentMockActivities{}
	env.RegisterActivity(mockActivities.ProvisionAgentSandbox)
	env.RegisterActivity(mockActivities.SubmitTaskManifest)
	env.RegisterActivity(mockActivities.WaitForAgentPhase)
	env.RegisterActivity(mockActivities.ReadAgentResult)
	env.RegisterActivity(mockActivities.CleanupSandbox)
	env.RegisterActivity(mockActivities.EnrichPrompt)
	env.RegisterActivity(mockActivities.CaptureKnowledge)

	mockActivities.On("ProvisionAgentSandbox", mock.Anything, mock.Anything).Return(sandboxInfo, nil)
	mockActivities.On("SubmitTaskManifest", mock.Anything, mock.Anything).Return(nil)
	mockActivities.On("WaitForAgentPhase", mock.Anything, mock.Anything).Return(
		&agentboxproto.AgentStatus{Phase: agentboxproto.PhaseFailed, Message: "claude code crashed"}, nil,
	)
	errMsg := "claude code crashed"
	mockActivities.On("ReadAgentResult", mock.Anything, mock.Anything).Return(
		&fleetproto.AgentResult{
			Status: agentboxproto.PhaseFailed,
			Error:  &errMsg,
		}, nil,
	)
	mockActivities.On("CleanupSandbox", mock.Anything, "container-456").Return(nil)
	mockActivities.On("EnrichPrompt", mock.Anything, mock.Anything).Return("", nil).Maybe()
	mockActivities.On("CaptureKnowledge", mock.Anything, mock.Anything).Return(nil, nil).Maybe()

	env.ExecuteWorkflow(TransformV2, task)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.TaskResult
	require.NoError(t, env.GetWorkflowResult(&result))

	assert.Equal(t, model.TaskStatusFailed, result.Status)
	require.NotNil(t, result.Error)
	assert.Contains(t, *result.Error, "claude code crashed")
}

func TestTransformV2_ReportMode(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	task := model.Task{
		Version: 1,
		ID:      "report-task",
		Title:   "Security Audit",
		Mode:    model.TaskModeReport,
		Repositories: []model.Repository{
			{URL: "https://github.com/org/svc.git", Name: "svc"},
		},
		Execution: model.Execution{
			Agentic: &model.AgenticExecution{Prompt: "Audit auth"},
		},
	}

	sandboxInfo := &model.SandboxInfo{ContainerID: "container-789", WorkspacePath: "/workspace"}

	mockActivities := &AgentMockActivities{}
	env.RegisterActivity(mockActivities.ProvisionAgentSandbox)
	env.RegisterActivity(mockActivities.SubmitTaskManifest)
	env.RegisterActivity(mockActivities.WaitForAgentPhase)
	env.RegisterActivity(mockActivities.ReadAgentResult)
	env.RegisterActivity(mockActivities.CleanupSandbox)
	env.RegisterActivity(mockActivities.EnrichPrompt)
	env.RegisterActivity(mockActivities.CaptureKnowledge)

	mockActivities.On("ProvisionAgentSandbox", mock.Anything, mock.Anything).Return(sandboxInfo, nil)
	mockActivities.On("SubmitTaskManifest", mock.Anything, mock.Anything).Return(nil)
	mockActivities.On("WaitForAgentPhase", mock.Anything, mock.Anything).Return(
		&agentboxproto.AgentStatus{Phase: agentboxproto.PhaseComplete}, nil,
	)
	mockActivities.On("ReadAgentResult", mock.Anything, mock.Anything).Return(
		&fleetproto.AgentResult{
			Status: agentboxproto.PhaseComplete,
			Repositories: []fleetproto.RepoResult{
				{
					Name:   "svc",
					Status: "success",
					Report: &fleetproto.ReportResult{
						Frontmatter: map[string]any{"score": 8},
						Body:        "# Audit\nAll good.",
						Raw:         "---\nscore: 8\n---\n# Audit\nAll good.",
					},
				},
			},
		}, nil,
	)
	mockActivities.On("CleanupSandbox", mock.Anything, "container-789").Return(nil)
	mockActivities.On("EnrichPrompt", mock.Anything, mock.Anything).Return("", nil).Maybe()
	mockActivities.On("CaptureKnowledge", mock.Anything, mock.Anything).Return(nil, nil).Maybe()

	env.ExecuteWorkflow(TransformV2, task)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.TaskResult
	require.NoError(t, env.GetWorkflowResult(&result))

	assert.Equal(t, model.TaskStatusCompleted, result.Status)
	assert.Equal(t, model.TaskModeReport, result.Mode)
	require.Len(t, result.Repositories, 1)
	require.NotNil(t, result.Repositories[0].Report)
	assert.Equal(t, "# Audit\nAll good.", result.Repositories[0].Report.Body)
}

func TestTransformV2_SteeringLoop_Approve(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	task := model.Task{
		Version: 1,
		ID:      "steer-approve-task",
		Title:   "Steer then Approve",
		Mode:    model.TaskModeTransform,
		Repositories: []model.Repository{
			{URL: "https://github.com/org/svc.git", Branch: "main", Name: "svc"},
		},
		Execution: model.Execution{
			Agentic: &model.AgenticExecution{Prompt: "Fix the bug"},
		},
		RequireApproval: true,
	}

	sandboxInfo := &model.SandboxInfo{ContainerID: "container-steer", WorkspacePath: "/workspace"}

	mockActivities := &AgentMockActivities{}
	env.RegisterActivity(mockActivities.ProvisionAgentSandbox)
	env.RegisterActivity(mockActivities.SubmitTaskManifest)
	env.RegisterActivity(mockActivities.WaitForAgentPhase)
	env.RegisterActivity(mockActivities.ReadAgentResult)
	env.RegisterActivity(mockActivities.SubmitSteeringAction)
	env.RegisterActivity(mockActivities.CleanupSandbox)
	env.RegisterActivity(mockActivities.EnrichPrompt)
	env.RegisterActivity(mockActivities.CaptureKnowledge)

	mockActivities.On("ProvisionAgentSandbox", mock.Anything, mock.Anything).Return(sandboxInfo, nil)
	mockActivities.On("SubmitTaskManifest", mock.Anything, mock.Anything).Return(nil)
	mockActivities.On("EnrichPrompt", mock.Anything, mock.Anything).Return("", nil).Maybe()
	mockActivities.On("CaptureKnowledge", mock.Anything, mock.Anything).Return(nil, nil).Maybe()

	// First wait returns awaiting_input
	mockActivities.On("WaitForAgentPhase", mock.Anything, mock.MatchedBy(func(input activity.WaitForAgentPhaseInput) bool {
		for _, p := range input.TargetPhases {
			if p == string(agentboxproto.PhaseAwaitingInput) {
				return true
			}
		}
		return false
	})).Return(&agentboxproto.AgentStatus{Phase: agentboxproto.PhaseAwaitingInput}, nil).Once()

	// Second wait (after steer) returns awaiting_input again
	mockActivities.On("WaitForAgentPhase", mock.Anything, mock.MatchedBy(func(input activity.WaitForAgentPhaseInput) bool {
		for _, p := range input.TargetPhases {
			if p == string(agentboxproto.PhaseAwaitingInput) {
				return true
			}
		}
		return false
	})).Return(&agentboxproto.AgentStatus{Phase: agentboxproto.PhaseAwaitingInput}, nil).Once()

	// Third wait (after approve) returns complete
	mockActivities.On("WaitForAgentPhase", mock.Anything, mock.MatchedBy(func(input activity.WaitForAgentPhaseInput) bool {
		for _, p := range input.TargetPhases {
			if p == string(agentboxproto.PhaseComplete) {
				return true
			}
		}
		return false
	})).Return(&agentboxproto.AgentStatus{Phase: agentboxproto.PhaseComplete}, nil).Once()

	mockActivities.On("ReadAgentResult", mock.Anything, mock.Anything).Return(
		&fleetproto.AgentResult{
			Status: agentboxproto.PhaseAwaitingInput,
			Repositories: []fleetproto.RepoResult{
				{Name: "svc", Status: "success", FilesModified: []string{"main.go"}},
			},
		}, nil,
	).Once()

	// Re-read after steer
	mockActivities.On("ReadAgentResult", mock.Anything, mock.Anything).Return(
		&fleetproto.AgentResult{
			Status: agentboxproto.PhaseAwaitingInput,
			Repositories: []fleetproto.RepoResult{
				{Name: "svc", Status: "success", FilesModified: []string{"main.go", "handler.go"}},
			},
		}, nil,
	).Once()

	// Final read after approve
	mockActivities.On("ReadAgentResult", mock.Anything, mock.Anything).Return(
		&fleetproto.AgentResult{
			Status: agentboxproto.PhaseComplete,
			Repositories: []fleetproto.RepoResult{
				{
					Name: "svc", Status: "success",
					FilesModified: []string{"main.go", "handler.go"},
					PullRequest:   &fleetproto.PRInfo{URL: "https://github.com/org/svc/pull/42", Title: "Fix"},
				},
			},
		}, nil,
	).Once()

	mockActivities.On("SubmitSteeringAction", mock.Anything, mock.Anything).Return(nil)
	mockActivities.On("CleanupSandbox", mock.Anything, "container-steer").Return(nil)

	// Send steer signal, then approve
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalSteer, model.SteeringSignalPayload{Prompt: "Also fix tests"})
	}, 0)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalApprove, nil)
	}, 0)

	env.ExecuteWorkflow(TransformV2, task)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.TaskResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Equal(t, model.TaskStatusCompleted, result.Status)
}

func TestTransformV2_SteeringLoop_Reject(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	task := model.Task{
		Version: 1,
		ID:      "steer-reject-task",
		Title:   "Reject Task",
		Mode:    model.TaskModeTransform,
		Repositories: []model.Repository{
			{URL: "https://github.com/org/svc.git", Branch: "main", Name: "svc"},
		},
		Execution: model.Execution{
			Agentic: &model.AgenticExecution{Prompt: "Fix the bug"},
		},
		RequireApproval: true,
	}

	sandboxInfo := &model.SandboxInfo{ContainerID: "container-reject", WorkspacePath: "/workspace"}

	mockActivities := &AgentMockActivities{}
	env.RegisterActivity(mockActivities.ProvisionAgentSandbox)
	env.RegisterActivity(mockActivities.SubmitTaskManifest)
	env.RegisterActivity(mockActivities.WaitForAgentPhase)
	env.RegisterActivity(mockActivities.ReadAgentResult)
	env.RegisterActivity(mockActivities.SubmitSteeringAction)
	env.RegisterActivity(mockActivities.CleanupSandbox)
	env.RegisterActivity(mockActivities.EnrichPrompt)
	env.RegisterActivity(mockActivities.CaptureKnowledge)

	mockActivities.On("ProvisionAgentSandbox", mock.Anything, mock.Anything).Return(sandboxInfo, nil)
	mockActivities.On("SubmitTaskManifest", mock.Anything, mock.Anything).Return(nil)
	mockActivities.On("WaitForAgentPhase", mock.Anything, mock.Anything).Return(
		&agentboxproto.AgentStatus{Phase: agentboxproto.PhaseAwaitingInput}, nil,
	)
	mockActivities.On("ReadAgentResult", mock.Anything, mock.Anything).Return(
		&fleetproto.AgentResult{
			Status:       agentboxproto.PhaseAwaitingInput,
			Repositories: []fleetproto.RepoResult{{Name: "svc", Status: "success"}},
		}, nil,
	)
	mockActivities.On("SubmitSteeringAction", mock.Anything, mock.Anything).Return(nil)
	mockActivities.On("CleanupSandbox", mock.Anything, "container-reject").Return(nil)
	mockActivities.On("EnrichPrompt", mock.Anything, mock.Anything).Return("", nil).Maybe()
	mockActivities.On("CaptureKnowledge", mock.Anything, mock.Anything).Return(nil, nil).Maybe()

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReject, nil)
	}, 0)

	env.ExecuteWorkflow(TransformV2, task)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.TaskResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Equal(t, model.TaskStatusCancelled, result.Status)
}

func TestTransformV2_SteeringLoop_Cancel(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	task := model.Task{
		Version: 1,
		ID:      "steer-cancel-task",
		Title:   "Cancel Task",
		Mode:    model.TaskModeTransform,
		Repositories: []model.Repository{
			{URL: "https://github.com/org/svc.git", Branch: "main", Name: "svc"},
		},
		Execution: model.Execution{
			Agentic: &model.AgenticExecution{Prompt: "Fix the bug"},
		},
		RequireApproval: true,
	}

	sandboxInfo := &model.SandboxInfo{ContainerID: "container-cancel", WorkspacePath: "/workspace"}

	mockActivities := &AgentMockActivities{}
	env.RegisterActivity(mockActivities.ProvisionAgentSandbox)
	env.RegisterActivity(mockActivities.SubmitTaskManifest)
	env.RegisterActivity(mockActivities.WaitForAgentPhase)
	env.RegisterActivity(mockActivities.ReadAgentResult)
	env.RegisterActivity(mockActivities.SubmitSteeringAction)
	env.RegisterActivity(mockActivities.CleanupSandbox)
	env.RegisterActivity(mockActivities.EnrichPrompt)
	env.RegisterActivity(mockActivities.CaptureKnowledge)

	mockActivities.On("ProvisionAgentSandbox", mock.Anything, mock.Anything).Return(sandboxInfo, nil)
	mockActivities.On("SubmitTaskManifest", mock.Anything, mock.Anything).Return(nil)
	mockActivities.On("WaitForAgentPhase", mock.Anything, mock.Anything).Return(
		&agentboxproto.AgentStatus{Phase: agentboxproto.PhaseAwaitingInput}, nil,
	)
	mockActivities.On("ReadAgentResult", mock.Anything, mock.Anything).Return(
		&fleetproto.AgentResult{
			Status:       agentboxproto.PhaseAwaitingInput,
			Repositories: []fleetproto.RepoResult{{Name: "svc", Status: "success"}},
		}, nil,
	)
	mockActivities.On("SubmitSteeringAction", mock.Anything, mock.Anything).Return(nil)
	mockActivities.On("CleanupSandbox", mock.Anything, "container-cancel").Return(nil)
	mockActivities.On("EnrichPrompt", mock.Anything, mock.Anything).Return("", nil).Maybe()
	mockActivities.On("CaptureKnowledge", mock.Anything, mock.Anything).Return(nil, nil).Maybe()

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalCancel, nil)
	}, 0)

	env.ExecuteWorkflow(TransformV2, task)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.TaskResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Equal(t, model.TaskStatusCancelled, result.Status)
}

func TestTransformV2_CaptureKnowledge_CalledAfterSteering(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	task := model.Task{
		Version: 1,
		ID:      "capture-test",
		Title:   "Capture Test",
		Mode:    model.TaskModeTransform,
		Repositories: []model.Repository{
			{URL: "https://github.com/org/svc.git", Branch: "main", Name: "svc"},
		},
		Execution: model.Execution{
			Agentic: &model.AgenticExecution{Prompt: "Fix the bug"},
		},
		RequireApproval: true,
	}

	sandboxInfo := &model.SandboxInfo{ContainerID: "container-capture", WorkspacePath: "/workspace"}

	mockActivities := &AgentMockActivities{}
	env.RegisterActivity(mockActivities.ProvisionAgentSandbox)
	env.RegisterActivity(mockActivities.SubmitTaskManifest)
	env.RegisterActivity(mockActivities.WaitForAgentPhase)
	env.RegisterActivity(mockActivities.ReadAgentResult)
	env.RegisterActivity(mockActivities.SubmitSteeringAction)
	env.RegisterActivity(mockActivities.CleanupSandbox)
	env.RegisterActivity(mockActivities.EnrichPrompt)
	env.RegisterActivity(mockActivities.CaptureKnowledge)

	mockActivities.On("EnrichPrompt", mock.Anything, mock.Anything).Return("", nil)
	mockActivities.On("ProvisionAgentSandbox", mock.Anything, mock.Anything).Return(sandboxInfo, nil)
	mockActivities.On("SubmitTaskManifest", mock.Anything, mock.Anything).Return(nil)

	// First wait: awaiting input
	mockActivities.On("WaitForAgentPhase", mock.Anything, mock.MatchedBy(func(i activity.WaitForAgentPhaseInput) bool {
		for _, p := range i.TargetPhases {
			if p == string(agentboxproto.PhaseAwaitingInput) {
				return true
			}
		}
		return false
	})).Return(&agentboxproto.AgentStatus{Phase: agentboxproto.PhaseAwaitingInput}, nil).Once()

	// After steer: awaiting input again
	mockActivities.On("WaitForAgentPhase", mock.Anything, mock.MatchedBy(func(i activity.WaitForAgentPhaseInput) bool {
		for _, p := range i.TargetPhases {
			if p == string(agentboxproto.PhaseAwaitingInput) {
				return true
			}
		}
		return false
	})).Return(&agentboxproto.AgentStatus{Phase: agentboxproto.PhaseAwaitingInput}, nil).Once()

	// After approve: complete
	mockActivities.On("WaitForAgentPhase", mock.Anything, mock.MatchedBy(func(i activity.WaitForAgentPhaseInput) bool {
		for _, p := range i.TargetPhases {
			if p == string(agentboxproto.PhaseComplete) {
				return true
			}
		}
		return false
	})).Return(&agentboxproto.AgentStatus{Phase: agentboxproto.PhaseComplete}, nil).Once()

	agentResult := &fleetproto.AgentResult{
		Status: agentboxproto.PhaseAwaitingInput,
		Repositories: []fleetproto.RepoResult{
			{Name: "svc", Status: "success", FilesModified: []string{"main.go"}},
		},
	}
	mockActivities.On("ReadAgentResult", mock.Anything, mock.Anything).Return(agentResult, nil)
	mockActivities.On("SubmitSteeringAction", mock.Anything, mock.Anything).Return(nil)
	mockActivities.On("CleanupSandbox", mock.Anything, "container-capture").Return(nil)

	// CaptureKnowledge MUST be called with steering history
	mockActivities.On("CaptureKnowledge", mock.Anything, mock.MatchedBy(func(input activity.CaptureKnowledgeInput) bool {
		return input.TaskID == "capture-test" &&
			len(input.SteeringHistory) == 1 &&
			input.SteeringHistory[0].Prompt == "Also fix tests"
	})).Return(nil, nil).Once()

	// Send steer first; approve arrives after steer loop iteration completes (non-zero delay
	// ensures steer is processed before approve arrives, since AwaitWithTimeout advances
	// simulated time when waiting).
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalSteer, model.SteeringSignalPayload{Prompt: "Also fix tests"})
	}, 0)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalApprove, nil)
	}, time.Hour)

	env.ExecuteWorkflow(TransformV2, task)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	mockActivities.AssertExpectations(t)
}

func TestTransformV2_CaptureKnowledge_SkippedWithoutSteering(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	task := model.Task{
		Version: 1,
		ID:      "no-capture-test",
		Mode:    model.TaskModeTransform,
		Repositories: []model.Repository{
			{URL: "https://github.com/org/svc.git", Branch: "main", Name: "svc"},
		},
		Execution:       model.Execution{Agentic: &model.AgenticExecution{Prompt: "Fix it"}},
		RequireApproval: true,
	}

	sandboxInfo := &model.SandboxInfo{ContainerID: "container-no-cap", WorkspacePath: "/workspace"}

	mockActivities := &AgentMockActivities{}
	env.RegisterActivity(mockActivities.ProvisionAgentSandbox)
	env.RegisterActivity(mockActivities.SubmitTaskManifest)
	env.RegisterActivity(mockActivities.WaitForAgentPhase)
	env.RegisterActivity(mockActivities.ReadAgentResult)
	env.RegisterActivity(mockActivities.SubmitSteeringAction)
	env.RegisterActivity(mockActivities.CleanupSandbox)
	env.RegisterActivity(mockActivities.EnrichPrompt)
	env.RegisterActivity(mockActivities.CaptureKnowledge)

	mockActivities.On("EnrichPrompt", mock.Anything, mock.Anything).Return("", nil)
	mockActivities.On("ProvisionAgentSandbox", mock.Anything, mock.Anything).Return(sandboxInfo, nil)
	mockActivities.On("SubmitTaskManifest", mock.Anything, mock.Anything).Return(nil)
	mockActivities.On("WaitForAgentPhase", mock.Anything, mock.MatchedBy(func(i activity.WaitForAgentPhaseInput) bool {
		for _, p := range i.TargetPhases {
			if p == string(agentboxproto.PhaseAwaitingInput) {
				return true
			}
		}
		return false
	})).Return(&agentboxproto.AgentStatus{Phase: agentboxproto.PhaseAwaitingInput}, nil).Once()
	mockActivities.On("WaitForAgentPhase", mock.Anything, mock.MatchedBy(func(i activity.WaitForAgentPhaseInput) bool {
		for _, p := range i.TargetPhases {
			if p == string(agentboxproto.PhaseComplete) {
				return true
			}
		}
		return false
	})).Return(&agentboxproto.AgentStatus{Phase: agentboxproto.PhaseComplete}, nil).Once()
	mockActivities.On("ReadAgentResult", mock.Anything, mock.Anything).Return(
		&fleetproto.AgentResult{Status: agentboxproto.PhaseComplete}, nil,
	)
	mockActivities.On("SubmitSteeringAction", mock.Anything, mock.Anything).Return(nil)
	mockActivities.On("CleanupSandbox", mock.Anything, "container-no-cap").Return(nil)
	// CaptureKnowledge must NOT be called (no .On registration)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalApprove, nil)
	}, 0)

	env.ExecuteWorkflow(TransformV2, task)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	mockActivities.AssertNotCalled(t, "CaptureKnowledge", mock.Anything, mock.Anything)
}

func TestBuildTaskResultFromAgent(t *testing.T) {
	task := model.Task{
		ID:   "test-task",
		Mode: model.TaskModeTransform,
	}

	agentResult := &fleetproto.AgentResult{
		Status: agentboxproto.PhaseComplete,
		Repositories: []fleetproto.RepoResult{
			{
				Name:          "svc",
				Status:        "success",
				FilesModified: []string{"main.go", "handler.go"},
				Diffs: []fleetproto.DiffEntry{
					{Path: "main.go", Status: "modified", Additions: 10, Deletions: 5},
				},
				PullRequest: &fleetproto.PRInfo{
					URL:        "https://github.com/org/svc/pull/42",
					Number:     42,
					BranchName: "auto/test-task-svc",
					Title:      "Fix the bug",
				},
			},
		},
	}

	called := false
	signalDone := func() { called = true }

	startTime := agentResult.StartedAt
	endTime := startTime.Add(5 * 60 * 1e9) // 5 min

	result := buildTaskResultFromAgent(task, agentResult, startTime, endTime, signalDone)

	assert.True(t, called)
	assert.Equal(t, model.TaskStatusCompleted, result.Status)
	assert.Equal(t, "test-task", result.TaskID)
	require.Len(t, result.Repositories, 1)

	repo := result.Repositories[0]
	assert.Equal(t, "svc", repo.Repository)
	assert.Equal(t, "success", repo.Status)
	assert.Equal(t, []string{"main.go", "handler.go"}, repo.FilesModified)
	require.NotNil(t, repo.PullRequest)
	assert.Equal(t, "https://github.com/org/svc/pull/42", repo.PullRequest.PRURL)
	assert.Equal(t, 42, repo.PullRequest.PRNumber)
}

func TestExtractDiffsFromAgent(t *testing.T) {
	agentResult := &fleetproto.AgentResult{
		Repositories: []fleetproto.RepoResult{
			{
				Name: "svc",
				Diffs: []fleetproto.DiffEntry{
					{Path: "main.go", Status: "modified", Additions: 10, Deletions: 5, Diff: "..."},
					{Path: "new.go", Status: "added", Additions: 20, Deletions: 0, Diff: "..."},
				},
			},
			{
				Name:  "svc-b",
				Diffs: nil, // No changes
			},
		},
	}

	diffs := extractDiffsFromAgent(agentResult)

	require.Len(t, diffs, 2)

	assert.Equal(t, "svc", diffs[0].Repository)
	assert.Len(t, diffs[0].Files, 2)
	assert.Equal(t, 35, diffs[0].TotalLines) // 10+5+20+0
	assert.Contains(t, diffs[0].Summary, "2 files")

	assert.Equal(t, "svc-b", diffs[1].Repository)
	assert.Len(t, diffs[1].Files, 0)
}

func TestExtractDiffsFromAgent_Nil(t *testing.T) {
	assert.Nil(t, extractDiffsFromAgent(nil))
}
