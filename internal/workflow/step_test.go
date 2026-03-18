package workflow

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

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
	assert.Equal(t, "step-run-456", input.StepRunID)
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

// stepMockActivities provides mock implementations of the activities used by StepWorkflow.
// Activity methods are named to match the string constants in step.go.
type stepMockActivities struct {
	mock.Mock
}

func (m *stepMockActivities) ProvisionSandbox(_ context.Context, input StepInput) (string, error) {
	args := m.Called(input)
	return args.String(0), args.Error(1)
}

func (m *stepMockActivities) ExecuteStep(_ context.Context, input ExecuteStepInput) (*model.StepOutput, error) {
	args := m.Called(input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.StepOutput), args.Error(1)
}

func (m *stepMockActivities) CleanupSandbox(_ context.Context, sandboxID string) error {
	args := m.Called(sandboxID)
	return args.Error(0)
}

func (m *stepMockActivities) UpdateStepStatus(_ context.Context, stepRunID, status string) error {
	args := m.Called(stepRunID, status)
	return args.Error(0)
}

func (m *stepMockActivities) CreatePullRequest(_ context.Context, sandboxID string, input StepInput) (string, error) {
	args := m.Called(sandboxID, input)
	return args.String(0), args.Error(1)
}

func (m *stepMockActivities) VerifyStep(_ context.Context, sandboxID, stepRunID string, verifiers any) error {
	args := m.Called(sandboxID, stepRunID, verifiers)
	return args.Error(0)
}

func (m *stepMockActivities) CompleteStepRun(_ context.Context, stepRunID, status string, output map[string]any, diff, errorMsg string, costUSD float64) error {
	args := m.Called(stepRunID, status, output, diff, errorMsg, costUSD)
	return args.Error(0)
}

func (m *stepMockActivities) CreateContinuationStepRun(_ context.Context, input model.CreateContinuationStepRunInput) (string, error) {
	args := m.Called(input)
	return args.String(0), args.Error(1)
}

func (m *stepMockActivities) CleanupCheckpointBranch(_ context.Context, input model.CleanupCheckpointInput) error {
	args := m.Called(input)
	return args.Error(0)
}

func (m *stepMockActivities) RunPreflight(_ context.Context, input RunPreflightInput) (RunPreflightOutput, error) {
	args := m.Called(input)
	return args.Get(0).(RunPreflightOutput), args.Error(1)
}

// newStepWorkflowEnv creates a configured Temporal test environment with all
// StepWorkflow activities registered from the mock struct.
func newStepWorkflowEnv(t *testing.T) (*testsuite.TestWorkflowEnvironment, *stepMockActivities) {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	mocks := &stepMockActivities{}
	env.RegisterActivity(mocks.ProvisionSandbox)
	env.RegisterActivity(mocks.ExecuteStep)
	env.RegisterActivity(mocks.CleanupSandbox)
	env.RegisterActivity(mocks.UpdateStepStatus)
	env.RegisterActivity(mocks.CreatePullRequest)
	env.RegisterActivity(mocks.VerifyStep)
	env.RegisterActivity(mocks.CompleteStepRun)
	env.RegisterActivity(mocks.CreateContinuationStepRun)
	env.RegisterActivity(mocks.CleanupCheckpointBranch)
	env.RegisterActivity(mocks.RunPreflight)
	return env, mocks
}

// TestStepWorkflow_ReusesSandboxID verifies that when SandboxID is pre-provided in
// StepInput, ProvisionSandbox is never called — the provided ID is used directly.
func TestStepWorkflow_ReusesSandboxID(t *testing.T) {
	env, mocks := newStepWorkflowEnv(t)

	input := StepInput{
		RunID:     "run-1",
		StepRunID: "sr-1",
		StepDef: model.StepDef{
			ID:             "analyze",
			Mode:           "report",
			ApprovalPolicy: "never",
		},
		ResolvedOpts: ResolvedStepOpts{
			Prompt: "Analyze the code",
			Agent:  "claude-code",
		},
		// Pre-provided sandbox ID — ProvisionSandbox must not be called.
		SandboxID: "existing-sandbox-abc",
	}

	expectedOutput := &model.StepOutput{
		StepID: "analyze",
		Status: model.StepStatusComplete,
	}

	// ExecuteStep must receive the pre-provided sandbox ID.
	mocks.On("ExecuteStep", mock.MatchedBy(func(ei ExecuteStepInput) bool {
		return ei.SandboxID == "existing-sandbox-abc"
	})).Return(expectedOutput, nil)
	mocks.On("CompleteStepRun", "sr-1", "complete", mock.Anything, mock.Anything, mock.Anything, mock.AnythingOfType("float64")).Return(nil)

	env.ExecuteWorkflow(StepWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.StepOutput
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Equal(t, model.StepStatusComplete, result.Status)

	// ProvisionSandbox must not have been called.
	mocks.AssertNotCalled(t, "ProvisionSandbox", mock.Anything)
	// CleanupSandbox must not have been called.
	mocks.AssertNotCalled(t, "CleanupSandbox", mock.Anything)
	// CompleteStepRun must have been called with 'complete' status.
	mocks.AssertCalled(t, "CompleteStepRun", "sr-1", "complete", mock.Anything, mock.Anything, mock.Anything, mock.AnythingOfType("float64"))
}

// TestStepWorkflow_ProvisionAndCleanup verifies that when SandboxID is empty and
// SandboxGroup is empty, ProvisionSandbox IS called and CleanupSandbox IS called.
func TestStepWorkflow_ProvisionAndCleanup(t *testing.T) {
	env, mocks := newStepWorkflowEnv(t)

	input := StepInput{
		RunID:     "run-2",
		StepRunID: "sr-2",
		StepDef: model.StepDef{
			ID:             "transform",
			Mode:           "transform",
			ApprovalPolicy: "never",
			// SandboxGroup is empty — exclusive sandbox, must be cleaned up.
		},
		ResolvedOpts: ResolvedStepOpts{
			Prompt: "Fix the bug",
			Agent:  "claude-code",
		},
		// SandboxID is empty — workflow must provision a new sandbox.
		SandboxID: "",
	}

	expectedOutput := &model.StepOutput{
		StepID: "transform",
		Status: model.StepStatusComplete,
		Diff:   "diff content",
	}

	mocks.On("ProvisionSandbox", input).Return("provisioned-sandbox-xyz", nil)
	mocks.On("ExecuteStep", mock.MatchedBy(func(ei ExecuteStepInput) bool {
		return ei.SandboxID == "provisioned-sandbox-xyz"
	})).Return(expectedOutput, nil)
	mocks.On("CleanupSandbox", "provisioned-sandbox-xyz").Return(nil)
	mocks.On("CompleteStepRun", "sr-2", "complete", mock.Anything, mock.Anything, mock.Anything, mock.AnythingOfType("float64")).Return(nil)

	env.ExecuteWorkflow(StepWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.StepOutput
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Equal(t, model.StepStatusComplete, result.Status)

	mocks.AssertCalled(t, "ProvisionSandbox", input)
	mocks.AssertCalled(t, "CleanupSandbox", "provisioned-sandbox-xyz")
	mocks.AssertCalled(t, "CompleteStepRun", "sr-2", "complete", mock.Anything, mock.Anything, mock.Anything, mock.AnythingOfType("float64"))
}

// TestStepWorkflow_SandboxGroupSkipsCleanup verifies that when SandboxGroup is set
// (sandbox is shared across steps), CleanupSandbox is NOT called — the DAGWorkflow
// is responsible for cleanup in that case.
func TestStepWorkflow_SandboxGroupSkipsCleanup(t *testing.T) {
	env, mocks := newStepWorkflowEnv(t)

	input := StepInput{
		RunID:     "run-3",
		StepRunID: "sr-3",
		StepDef: model.StepDef{
			ID:             "lint",
			Mode:           "report",
			ApprovalPolicy: "never",
			// SandboxGroup is set — sandbox is shared, DAGWorkflow cleans it up.
			SandboxGroup: "shared-group",
		},
		ResolvedOpts: ResolvedStepOpts{
			Prompt: "Run linters",
			Agent:  "claude-code",
		},
		// SandboxID is empty — ProvisionSandbox is called to get a sandbox for this group.
		SandboxID: "",
	}

	expectedOutput := &model.StepOutput{
		StepID: "lint",
		Status: model.StepStatusComplete,
	}

	mocks.On("ProvisionSandbox", input).Return("group-sandbox-123", nil)
	mocks.On("ExecuteStep", mock.MatchedBy(func(ei ExecuteStepInput) bool {
		return ei.SandboxID == "group-sandbox-123"
	})).Return(expectedOutput, nil)
	mocks.On("CompleteStepRun", "sr-3", "complete", mock.Anything, mock.Anything, mock.Anything, mock.AnythingOfType("float64")).Return(nil)

	// CleanupSandbox must NOT be called because SandboxGroup is set.

	env.ExecuteWorkflow(StepWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.StepOutput
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Equal(t, model.StepStatusComplete, result.Status)

	mocks.AssertCalled(t, "ProvisionSandbox", input)
	mocks.AssertNotCalled(t, "CleanupSandbox", mock.Anything)
	mocks.AssertCalled(t, "CompleteStepRun", "sr-3", "complete", mock.Anything, mock.Anything, mock.Anything, mock.AnythingOfType("float64"))
}

// TestStepWorkflow_ProvisionFailsAfterRetries verifies that when ProvisionSandbox
// permanently fails, the workflow surfaces an error (not infinite retry).
func TestStepWorkflow_ProvisionFailsAfterRetries(t *testing.T) {
	env, mocks := newStepWorkflowEnv(t)

	input := StepInput{
		RunID:     "run-prov-fail",
		StepRunID: "sr-prov-fail",
		StepDef: model.StepDef{
			ID:             "analyze",
			Mode:           "report",
			ApprovalPolicy: "never",
		},
		ResolvedOpts: ResolvedStepOpts{
			Prompt: "Analyze the code",
			Agent:  "claude-code",
		},
		SandboxID: "", // forces provision
	}

	mocks.On("ProvisionSandbox", input).Return("", assert.AnError)

	env.ExecuteWorkflow(StepWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provision sandbox")
}

// TestStepWorkflow_CreatePRFailureSetsError verifies that when CreatePullRequest fails,
// the step completes successfully but output.Error records the PR failure.
func TestStepWorkflow_CreatePRFailureSetsError(t *testing.T) {
	env, mocks := newStepWorkflowEnv(t)

	input := StepInput{
		RunID:     "run-pr-fail",
		StepRunID: "sr-pr-fail",
		StepDef: model.StepDef{
			ID:             "transform",
			Mode:           "transform",
			ApprovalPolicy: "never",
			PullRequest:    &model.PRDef{Title: "test PR"},
		},
		ResolvedOpts: ResolvedStepOpts{
			Prompt:   "Fix the bug",
			Agent:    "claude-code",
			PRConfig: &model.PRDef{Title: "test PR"},
		},
		SandboxID: "", // forces provision
	}

	execOutput := &model.StepOutput{
		StepID: "transform",
		Status: model.StepStatusComplete,
		Diff:   "some diff",
	}

	mocks.On("ProvisionSandbox", input).Return("sb-pr-fail", nil)
	mocks.On("ExecuteStep", mock.MatchedBy(func(ei ExecuteStepInput) bool {
		return ei.SandboxID == "sb-pr-fail"
	})).Return(execOutput, nil)
	mocks.On("CreatePullRequest", "sb-pr-fail", mock.Anything).Return("", assert.AnError)
	mocks.On("CleanupSandbox", "sb-pr-fail").Return(nil)
	mocks.On("CompleteStepRun", "sr-pr-fail", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.AnythingOfType("float64")).Return(nil)

	env.ExecuteWorkflow(StepWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.StepOutput
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Contains(t, result.Error, "PR creation failed")
}

// TestStepWorkflow_FinalizeFailurePropagates verifies that when CompleteStepRun
// fails, the workflow returns an error instead of silently swallowing it.
func TestStepWorkflow_FinalizeFailurePropagates(t *testing.T) {
	env, mocks := newStepWorkflowEnv(t)

	input := StepInput{
		RunID:     "run-fin",
		StepRunID: "sr-fin",
		StepDef: model.StepDef{
			ID:             "analyze",
			Mode:           "report",
			ApprovalPolicy: "never",
		},
		ResolvedOpts: ResolvedStepOpts{
			Prompt: "Analyze",
			Agent:  "claude-code",
		},
		SandboxID: "sb-fin",
	}

	mocks.On("ExecuteStep", mock.Anything).Return(&model.StepOutput{
		StepID: "analyze",
		Status: model.StepStatusComplete,
	}, nil)
	mocks.On("CompleteStepRun", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.AnythingOfType("float64")).Return(fmt.Errorf("db connection lost"))

	env.ExecuteWorkflow(StepWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "finalize step run")
}

func TestStepWorkflow_AwaitResumeCycle(t *testing.T) {
	env, mocks := newStepWorkflowEnv(t)

	input := StepInput{
		RunID:     "run-1",
		StepRunID: "sr-1",
		TeamID:    "team-1",
		StepDef: model.StepDef{
			ID:             "fix",
			Title:          "Fix",
			Mode:           "transform",
			ApprovalPolicy: "never",
		},
		ResolvedOpts: ResolvedStepOpts{
			Prompt: "Fix the bug",
			Agent:  "claude-code",
		},
		SandboxID: "sb-1", // pre-provisioned
	}

	// First call returns awaiting_input
	mocks.On("ExecuteStep", mock.MatchedBy(func(ei ExecuteStepInput) bool {
		return ei.ContinuationContext == nil // first call has no continuation
	})).Return(&model.StepOutput{
		StepID:      "fix",
		Status:      model.StepStatusAwaitingInput,
		InboxItemID: "inbox-1",
		Question:    "Fix or skip?",
	}, nil).Once()

	// Continuation call
	mocks.On("ExecuteStep", mock.MatchedBy(func(ei ExecuteStepInput) bool {
		if ei.ContinuationContext == nil {
			return false
		}
		return ei.ContinuationContext.HumanAnswer == "Fix tests"
	})).Return(&model.StepOutput{
		StepID: "fix",
		Status: model.StepStatusComplete,
		Output: map[string]any{"result": "done"},
	}, nil).Once()

	mocks.On("CreateContinuationStepRun", mock.Anything).Return("cont-sr-1", nil)
	mocks.On("ProvisionSandbox", mock.Anything).Return("cont-sb-1", nil)
	mocks.On("CleanupSandbox", "cont-sb-1").Return(nil)
	mocks.On("CompleteStepRun", mock.Anything, "complete", mock.Anything, mock.Anything, mock.Anything, mock.AnythingOfType("float64")).Return(nil)

	// Deliver respond signal after workflow starts
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("respond", model.InboxAnswer{
			Answer:    "Fix tests",
			Responder: "jane@example.com",
		})
	}, 0)

	env.ExecuteWorkflow(StepWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.StepOutput
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Equal(t, model.StepStatusComplete, result.Status)

	mocks.AssertCalled(t, "CreateContinuationStepRun", mock.Anything)
	mocks.AssertCalled(t, "ProvisionSandbox", mock.Anything)
	mocks.AssertCalled(t, "CleanupSandbox", "cont-sb-1")
}

func TestStepWorkflow_NoAwaitingInput_WorksNormally(t *testing.T) {
	env, mocks := newStepWorkflowEnv(t)

	input := StepInput{
		RunID:     "run-normal",
		StepRunID: "sr-normal",
		StepDef: model.StepDef{
			ID:             "analyze",
			Mode:           "report",
			ApprovalPolicy: "never",
		},
		ResolvedOpts: ResolvedStepOpts{
			Prompt: "Analyze",
			Agent:  "claude-code",
		},
		SandboxID: "sb-normal",
	}

	mocks.On("ExecuteStep", mock.Anything).Return(&model.StepOutput{
		StepID: "analyze",
		Status: model.StepStatusComplete,
	}, nil)
	mocks.On("CompleteStepRun", mock.Anything, "complete", mock.Anything, mock.Anything, mock.Anything, mock.AnythingOfType("float64")).Return(nil)

	env.ExecuteWorkflow(StepWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	// CreateContinuationStepRun should NOT be called
	mocks.AssertNotCalled(t, "CreateContinuationStepRun", mock.Anything)
}

func TestStepWorkflow_RunsPreflightWithProfile(t *testing.T) {
	env, mocks := newStepWorkflowEnv(t)

	profile := &model.AgentProfileBody{
		Plugins: []model.PluginSource{{Plugin: "plugins/test"}},
	}

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)
	mocks.On("RunPreflight", mock.MatchedBy(func(input RunPreflightInput) bool {
		return input.SandboxID == "sb-1" && len(input.Profile.Plugins) == 1
	})).Return(RunPreflightOutput{EvalPluginDirs: []string{"/tmp/eval-plugin-0/plugins/test"}}, nil)
	mocks.On("ExecuteStep", mock.MatchedBy(func(input ExecuteStepInput) bool {
		return len(input.EvalPluginDirs) == 1 && input.EvalPluginDirs[0] == "/tmp/eval-plugin-0/plugins/test"
	})).Return(&model.StepOutput{StepID: "step1", Status: model.StepStatusComplete}, nil)
	mocks.On("CompleteStepRun", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.AnythingOfType("float64")).Return(nil)
	mocks.On("CleanupSandbox", mock.Anything).Return(nil)

	env.ExecuteWorkflow(StepWorkflow, StepInput{
		RunID:     "run-1",
		StepRunID: "sr-1",
		TeamID:    "team-1",
		StepDef:   model.StepDef{ID: "step1", Execution: &model.ExecutionDef{Agent: "claude-code", Prompt: "test"}},
		ResolvedOpts: ResolvedStepOpts{
			Prompt:           "test",
			Agent:            "claude-code",
			EffectiveProfile: profile,
			EvalPluginURLs:   []string{"https://github.com/org/repo/tree/main/plugins/test"},
		},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	mocks.AssertCalled(t, "RunPreflight", mock.Anything)
}

func TestStepWorkflow_SkipsPreflightWithoutProfile(t *testing.T) {
	env, mocks := newStepWorkflowEnv(t)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)
	mocks.On("ExecuteStep", mock.Anything).Return(&model.StepOutput{StepID: "step1", Status: model.StepStatusComplete}, nil)
	mocks.On("CompleteStepRun", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.AnythingOfType("float64")).Return(nil)
	mocks.On("CleanupSandbox", mock.Anything).Return(nil)

	env.ExecuteWorkflow(StepWorkflow, StepInput{
		RunID:     "run-1",
		StepRunID: "sr-1",
		TeamID:    "team-1",
		StepDef:   model.StepDef{ID: "step1", Execution: &model.ExecutionDef{Agent: "claude-code", Prompt: "test"}},
		ResolvedOpts: ResolvedStepOpts{
			Prompt: "test",
			Agent:  "claude-code",
			// No EffectiveProfile, no EvalPluginURLs
		},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	mocks.AssertNotCalled(t, "RunPreflight", mock.Anything)
}

func TestStepWorkflow_PreflightFailurePropagates(t *testing.T) {
	env, mocks := newStepWorkflowEnv(t)

	profile := &model.AgentProfileBody{
		MCPs: []model.MCPConfig{{Name: "test-mcp"}},
	}

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)
	mocks.On("RunPreflight", mock.Anything).Return(RunPreflightOutput{}, fmt.Errorf("preflight failed: plugin install error"))
	mocks.On("CleanupSandbox", mock.Anything).Return(nil)

	env.ExecuteWorkflow(StepWorkflow, StepInput{
		RunID:     "run-1",
		StepRunID: "sr-1",
		TeamID:    "team-1",
		StepDef:   model.StepDef{ID: "step1", Execution: &model.ExecutionDef{Agent: "claude-code", Prompt: "test"}},
		ResolvedOpts: ResolvedStepOpts{
			Prompt:           "test",
			Agent:            "claude-code",
			EffectiveProfile: profile,
		},
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pre-flight")
}
