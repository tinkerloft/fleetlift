package workflow

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// dagMockActivities provides mock implementations of all activities invoked by
// DAGWorkflow and its child StepWorkflow instances.  Method names must match the
// string activity names registered in cmd/worker/main.go so the test environment
// can resolve them by name.
type dagMockActivities struct {
	mock.Mock
}

func (m *dagMockActivities) UpdateRunStatus(_ context.Context, runID, status, errorMsg string) error {
	args := m.Called(runID, status, errorMsg)
	return args.Error(0)
}

func (m *dagMockActivities) CreateStepRun(_ context.Context, runID, stepID, stepTitle, temporalWorkflowID string, input map[string]any) (string, error) {
	args := m.Called(runID, stepID, stepTitle, temporalWorkflowID)
	return args.String(0), args.Error(1)
}

func (m *dagMockActivities) CompleteStepRun(_ context.Context, stepRunID, status string, output map[string]any, diff, errorMsg string, costUSD float64) error {
	args := m.Called(stepRunID, status, output, diff, errorMsg, costUSD)
	return args.Error(0)
}

func (m *dagMockActivities) CreateInboxItem(_ context.Context, teamID, runID, stepRunID, kind, title, summary, artifactID string) error {
	args := m.Called(teamID, runID, stepRunID, kind, title, summary, artifactID)
	return args.Error(0)
}

func (m *dagMockActivities) GetPrimaryRunArtifactID(_ context.Context, runID string) (string, error) {
	args := m.Called(runID)
	return args.String(0), args.Error(1)
}

func (m *dagMockActivities) CleanupSandbox(_ context.Context, sandboxID string) error {
	args := m.Called(sandboxID)
	return args.Error(0)
}

func (m *dagMockActivities) ValidateCredentials(_ context.Context, teamID string, credNames []string) error {
	args := m.Called(teamID, credNames)
	return args.Error(0)
}

func (m *dagMockActivities) ProvisionSandbox(_ context.Context, input StepInput) (string, error) {
	args := m.Called(input)
	return args.String(0), args.Error(1)
}

func (m *dagMockActivities) ExecuteStep(_ context.Context, input ExecuteStepInput) (*model.StepOutput, error) {
	args := m.Called(input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.StepOutput), args.Error(1)
}

func (m *dagMockActivities) ExecuteAction(_ context.Context, stepRunID, actionType string, config map[string]any, teamID string, credNames []string) (map[string]any, error) {
	args := m.Called(stepRunID, actionType, config, teamID, credNames)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]any), args.Error(1)
}

func (m *dagMockActivities) UpdateStepStatus(_ context.Context, stepRunID, status string) error {
	args := m.Called(stepRunID, status)
	return args.Error(0)
}

func (m *dagMockActivities) ResolveAgentProfile(_ context.Context, input ResolveProfileInput) (model.AgentProfileBody, error) {
	args := m.Called(input)
	return args.Get(0).(model.AgentProfileBody), args.Error(1)
}

func (m *dagMockActivities) RunPreflight(_ context.Context, input RunPreflightInput) (RunPreflightOutput, error) {
	args := m.Called(input)
	return args.Get(0).(RunPreflightOutput), args.Error(1)
}

// newDAGTestEnv creates a Temporal test environment with both DAGWorkflow and
// StepWorkflow registered, and default success mocks for all DB/status activities.
// Callers can set expectations on the returned mocks for step-level activities
// (ProvisionSandbox, ExecuteStep, ExecuteAction) before calling env.ExecuteWorkflow.
func newDAGTestEnv(t *testing.T) (*testsuite.TestWorkflowEnvironment, *dagMockActivities) {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	env.RegisterWorkflow(DAGWorkflow)
	env.RegisterWorkflow(StepWorkflow)

	mocks := &dagMockActivities{}

	// Register all activity implementations so the test env can resolve them.
	env.RegisterActivity(mocks.UpdateRunStatus)
	env.RegisterActivity(mocks.CreateStepRun)
	env.RegisterActivity(mocks.CompleteStepRun)
	env.RegisterActivity(mocks.CreateInboxItem)
	env.RegisterActivity(mocks.GetPrimaryRunArtifactID)
	env.RegisterActivity(mocks.CleanupSandbox)
	env.RegisterActivity(mocks.ValidateCredentials)
	env.RegisterActivity(mocks.ProvisionSandbox)
	env.RegisterActivity(mocks.ExecuteStep)
	env.RegisterActivity(mocks.ExecuteAction)
	env.RegisterActivity(mocks.UpdateStepStatus)
	env.RegisterActivity(mocks.ResolveAgentProfile)
	env.RegisterActivity(mocks.RunPreflight)

	// Default success stubs for DB/status activities that every DAG execution hits.
	// Tests that need to assert specific call arguments can override these.
	mocks.On("UpdateRunStatus", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mocks.On("CreateStepRun", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("sr-1", nil)
	mocks.On("CompleteStepRun", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.AnythingOfType("float64")).Return(nil)
	mocks.On("CreateInboxItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mocks.On("GetPrimaryRunArtifactID", mock.Anything).Return("", nil)
	mocks.On("CleanupSandbox", mock.Anything).Return(nil)
	mocks.On("ValidateCredentials", mock.Anything, mock.Anything).Return(nil)
	mocks.On("UpdateStepStatus", mock.Anything, mock.Anything).Return(nil)
	mocks.On("ResolveAgentProfile", mock.Anything).Return(model.AgentProfileBody{}, nil).Maybe()
	mocks.On("RunPreflight", mock.Anything).Return(RunPreflightOutput{}, nil).Maybe()

	return env, mocks
}

// TestDAGWorkflow_LinearAgentThenAction verifies a two-step workflow where:
// - step-1 is an agent (execution) step with no dependencies
// - step-2 is an action step (slack_notify) that depends on step-1
// Both steps must complete and the overall workflow must succeed.
func TestDAGWorkflow_LinearAgentThenAction(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	// Agent step (step-1): provision a fresh sandbox then execute the agent.
	// CleanupSandbox is already stubbed by newDAGTestEnv.
	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)
	mocks.On("ExecuteStep", mock.Anything).Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
		Output: map[string]any{"summary": "done"},
	}, nil)

	// Action step (step-2): execute action after step-1 completes.
	mocks.On("ExecuteAction", mock.Anything, "slack_notify", mock.Anything, mock.Anything, mock.Anything).Return(
		map[string]any{"status": "sent", "channel": "#ops"},
		nil,
	)

	def := model.WorkflowDef{
		ID:    "test-linear-wf",
		Title: "Linear Agent Then Action",
		Steps: []model.StepDef{
			{
				ID:    "step-1",
				Title: "Agent Step",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something",
				},
			},
			{
				ID:        "step-2",
				Title:     "Notify Step",
				DependsOn: []string{"step-1"},
				Action: &model.ActionDef{
					Type: "slack_notify",
					Config: map[string]any{
						"channel": "#ops",
						"message": "hello",
					},
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-linear-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
}

// TestDAGWorkflow_StepFailsRunFails verifies that when a step's ExecuteStep activity
// returns an error, the DAGWorkflow itself fails with an error mentioning that step.
func TestDAGWorkflow_StepFailsRunFails(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)
	mocks.On("ExecuteStep", mock.MatchedBy(func(ei ExecuteStepInput) bool {
		return ei.StepInput.StepDef.ID == "step-1"
	})).Return(nil, temporal.NewNonRetryableApplicationError(
		"execution failed", "ExecutionError", nil,
	))

	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{
				ID:    "step-1",
				Title: "Failing Step",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something",
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-fail-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "step-1")
}

// TestDAGWorkflow_DownstreamSkippedOnFailure verifies that when step-2 fails,
// step-3 (which depends on step-2) is skipped and the workflow fails mentioning step-2.
func TestDAGWorkflow_DownstreamSkippedOnFailure(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)

	// step-1 succeeds
	mocks.On("ExecuteStep", mock.MatchedBy(func(ei ExecuteStepInput) bool {
		return ei.StepInput.StepDef.ID == "step-1"
	})).Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
	}, nil)

	// step-2 fails — non-retryable to avoid spurious retries in test env.
	mocks.On("ExecuteStep", mock.MatchedBy(func(ei ExecuteStepInput) bool {
		return ei.StepInput.StepDef.ID == "step-2"
	})).Return(nil, temporal.NewNonRetryableApplicationError(
		"execution failed", "ExecutionError", nil,
	))

	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{
				ID:    "step-1",
				Title: "Step 1",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "step one",
				},
			},
			{
				ID:        "step-2",
				Title:     "Step 2",
				DependsOn: []string{"step-1"},
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "step two",
				},
			},
			{
				ID:        "step-3",
				Title:     "Step 3",
				DependsOn: []string{"step-2"},
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "step three",
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-downstream-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "step-2")
}

// TestDAGWorkflow_FanOutParallelSteps verifies that when a step specifies multiple
// repositories, the DAG launches one child StepWorkflow per repo in parallel and
// the overall workflow completes successfully when all children succeed.
func TestDAGWorkflow_FanOutParallelSteps(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)
	// Both fan-out children call ExecuteStep; use mock.Anything to match either.
	mocks.On("ExecuteStep", mock.Anything).Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
		Output: map[string]any{"summary": "done"},
	}, nil)

	def := model.WorkflowDef{
		ID:    "test-fanout-wf",
		Title: "Fan-Out Parallel Steps",
		Steps: []model.StepDef{
			{
				ID:    "step-1",
				Title: "Fan-Out Agent Step",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something",
				},
				Repositories: []any{
					map[string]any{"url": "https://github.com/test/repo1"},
					map[string]any{"url": "https://github.com/test/repo2"},
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-fanout-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
}

// TestDAGWorkflow_FanOutPartialFailure_Terminate verifies that when one fan-out child
// fails and the operator sends a "terminate" resolve signal, the workflow fails with
// an error mentioning the step.
func TestDAGWorkflow_FanOutPartialFailure_Terminate(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)
	// First child call succeeds.
	mocks.On("ExecuteStep", mock.Anything).Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
		Output: map[string]any{"summary": "done"},
	}, nil).Once()
	// Second child call fails with a non-retryable error to avoid spurious retry panics in the test env.
	mocks.On("ExecuteStep", mock.Anything).Return(nil,
		temporal.NewNonRetryableApplicationError("repo2 failed", "ExecutionError", nil),
	).Once()

	// After the partial failure inbox item is created, send a "terminate" signal to the parent DAGWorkflow.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalFanOutResolve, FanOutResolvePayload{Action: "terminate", StepID: "step-1"}) //nolint:errcheck
	}, time.Second)

	fanoutPartialFailureDef := model.WorkflowDef{
		ID:    "test-fanout-fail-wf",
		Title: "Fan-Out Partial Failure",
		Steps: []model.StepDef{
			{
				ID:    "step-1",
				Title: "Fan-Out Agent Step",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something",
				},
				Repositories: []any{
					map[string]any{"url": "https://github.com/test/repo1"},
					map[string]any{"url": "https://github.com/test/repo2"},
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-fanout-fail-1",
		TeamID:      "team-1",
		WorkflowDef: fanoutPartialFailureDef,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "step-1")
}

// TestDAGWorkflow_FanOutPartialFailure_Proceed verifies that when one fan-out child
// fails and the operator sends a "proceed" resolve signal, the workflow succeeds using
// only the successful results.
func TestDAGWorkflow_FanOutPartialFailure_Proceed(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)
	// First child call succeeds.
	mocks.On("ExecuteStep", mock.Anything).Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
		Output: map[string]any{"summary": "done"},
	}, nil).Once()
	// Second child call fails.
	mocks.On("ExecuteStep", mock.Anything).Return(nil,
		temporal.NewNonRetryableApplicationError("repo2 failed", "ExecutionError", nil),
	).Once()

	// After the partial failure inbox item is created, send a "proceed" signal.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalFanOutResolve, FanOutResolvePayload{Action: "proceed", StepID: "step-1"}) //nolint:errcheck
	}, time.Second)

	def := model.WorkflowDef{
		ID:    "test-fanout-proceed-wf",
		Title: "Fan-Out Partial Failure Proceed",
		Steps: []model.StepDef{
			{
				ID:    "step-1",
				Title: "Fan-Out Agent Step",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something",
				},
				Repositories: []any{
					map[string]any{"url": "https://github.com/test/repo1"},
					map[string]any{"url": "https://github.com/test/repo2"},
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-fanout-proceed-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
}

// TestDAGWorkflow_ConditionFalseSkipsStep verifies that when a step's condition evaluates
// to false (step-1 succeeded, but condition checks for "failed"), step-2 is skipped and
// ExecuteStep is only called once (for step-1, not step-2).
func TestDAGWorkflow_ConditionFalseSkipsStep(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)
	// step-1 succeeds → status="complete"
	mocks.On("ExecuteStep", mock.Anything).Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
	}, nil)

	def := model.WorkflowDef{
		ID:    "test-cond-false-wf",
		Title: "Condition False Skips Step",
		Steps: []model.StepDef{
			{
				ID:    "step-1",
				Title: "Agent Step",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something",
				},
			},
			{
				ID:        "step-2",
				Title:     "Conditional Step",
				DependsOn: []string{"step-1"},
				// Condition is false: step-1 status is "complete", not "failed"
				Condition: `{{eq (index .steps "step-1").status "failed"}}`,
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "should not run",
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-cond-false-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
	// ExecuteStep should only have been called for step-1, not step-2
	mocks.AssertNumberOfCalls(t, "ExecuteStep", 1)
}

// TestDAGWorkflow_ConditionTrueExecutesStep verifies that when a step's condition evaluates
// to true (step-1 succeeded and condition checks for "complete"), step-2 executes normally.
func TestDAGWorkflow_ConditionTrueExecutesStep(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)
	// step-1 executes and succeeds.
	mocks.On("ExecuteStep", mock.MatchedBy(func(ei ExecuteStepInput) bool {
		return ei.StepInput.StepDef.ID == "step-1"
	})).Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
	}, nil)
	// step-2 condition is true → step-2 also executes and succeeds.
	mocks.On("ExecuteStep", mock.MatchedBy(func(ei ExecuteStepInput) bool {
		return ei.StepInput.StepDef.ID == "step-2"
	})).Return(&model.StepOutput{
		StepID: "step-2",
		Status: model.StepStatusComplete,
	}, nil)

	def := model.WorkflowDef{
		ID:    "test-cond-true-wf",
		Title: "Condition True Executes Step",
		Steps: []model.StepDef{
			{
				ID:    "step-1",
				Title: "Agent Step",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something",
				},
			},
			{
				ID:        "step-2",
				Title:     "Conditional Step",
				DependsOn: []string{"step-1"},
				// Condition is true: step-1 status is "complete"
				Condition: `{{eq (index .steps "step-1").status "complete"}}`,
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "should run",
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-cond-true-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
}

// TestDAGWorkflow_ActionStepDispatch verifies that a single action step (type="slack_notify")
// dispatches to ExecuteAction and completes without provisioning a sandbox or executing an
// agent step.
func TestDAGWorkflow_ActionStepDispatch(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	mocks.On("ExecuteAction", mock.Anything, "slack_notify", mock.Anything, mock.Anything, mock.Anything).Return(
		map[string]any{"status": "sent", "channel": "#test"},
		nil,
	)

	def := model.WorkflowDef{
		ID:    "test-action-dispatch-wf",
		Title: "Action Step Dispatch",
		Steps: []model.StepDef{
			{
				ID:    "step-1",
				Title: "Slack Notify",
				Action: &model.ActionDef{
					Type: "slack_notify",
					Config: map[string]any{
						"channel": "#test",
						"message": "hello",
					},
					Credentials: []string{"SLACK_BOT_TOKEN"},
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-action-dispatch-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
	// Action steps must not provision sandboxes or execute agent steps.
	mocks.AssertNotCalled(t, "ProvisionSandbox", mock.Anything)
	mocks.AssertNotCalled(t, "ExecuteStep", mock.Anything)
}

// TestDAGWorkflow_CredentialPreflightFails verifies that when ValidateCredentials returns
// an error, the workflow fails with "credential preflight" in the error message and no
// sandbox is provisioned.
func TestDAGWorkflow_CredentialPreflightFails(t *testing.T) {
	// Build the env inline so we can set ValidateCredentials to fail
	// instead of the default success stub used by newDAGTestEnv.
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(DAGWorkflow)
	env.RegisterWorkflow(StepWorkflow)

	mocks := &dagMockActivities{}
	env.RegisterActivity(mocks.UpdateRunStatus)
	env.RegisterActivity(mocks.CreateStepRun)
	env.RegisterActivity(mocks.CompleteStepRun)
	env.RegisterActivity(mocks.CreateInboxItem)
	env.RegisterActivity(mocks.GetPrimaryRunArtifactID)
	env.RegisterActivity(mocks.CleanupSandbox)
	env.RegisterActivity(mocks.ValidateCredentials)
	env.RegisterActivity(mocks.ProvisionSandbox)
	env.RegisterActivity(mocks.ExecuteStep)
	env.RegisterActivity(mocks.ExecuteAction)
	env.RegisterActivity(mocks.UpdateStepStatus)

	// Default success stubs for DB/status activities.
	mocks.On("UpdateRunStatus", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mocks.On("CreateStepRun", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("sr-1", nil)
	mocks.On("CompleteStepRun", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.AnythingOfType("float64")).Return(nil)
	mocks.On("CreateInboxItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mocks.On("GetPrimaryRunArtifactID", mock.Anything).Return("", nil)
	mocks.On("CleanupSandbox", mock.Anything).Return(nil)
	mocks.On("UpdateStepStatus", mock.Anything, mock.Anything).Return(nil)

	// Credential preflight fails — use non-retryable to avoid test env retry loops.
	mocks.On("ValidateCredentials", mock.Anything, mock.Anything).Return(
		temporal.NewNonRetryableApplicationError("missing GITHUB_TOKEN", "CredentialError", nil),
	)

	def := model.WorkflowDef{
		ID:    "test-cred-fail-wf",
		Title: "Credential Preflight Fails",
		Steps: []model.StepDef{
			{
				ID:    "step-1",
				Title: "Agent Step",
				Execution: &model.ExecutionDef{
					Agent:       "claude-code",
					Prompt:      "do something",
					Credentials: []string{"GITHUB_TOKEN"},
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-cred-fail-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credential preflight")
	// Preflight fails before any step execution starts.
	mocks.AssertNotCalled(t, "ProvisionSandbox", mock.Anything)
	mocks.AssertNotCalled(t, "ExecuteStep", mock.Anything)
}

// TestDAGWorkflow_CredentialPreflightPasses verifies that when ValidateCredentials succeeds,
// the workflow proceeds to provision a sandbox and execute the agent step normally.
func TestDAGWorkflow_CredentialPreflightPasses(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)
	mocks.On("ExecuteStep", mock.Anything).Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
		Output: map[string]any{"summary": "done"},
	}, nil)

	def := model.WorkflowDef{
		ID:    "test-cred-pass-wf",
		Title: "Credential Preflight Passes",
		Steps: []model.StepDef{
			{
				ID:    "step-1",
				Title: "Agent Step",
				Execution: &model.ExecutionDef{
					Agent:       "claude-code",
					Prompt:      "do something",
					Credentials: []string{"GITHUB_TOKEN"},
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-cred-pass-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
}

// TestDAGWorkflow_SandboxGroupReuse verifies that when two sequential steps share the
// same sandbox_group, the DAG provisions the sandbox exactly once at the DAG level.
// The second step receives the same sandbox ID without triggering a second provision call.
func TestDAGWorkflow_SandboxGroupReuse(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	// ProvisionSandbox must be called exactly once for the "main" group.
	mocks.On("ProvisionSandbox", mock.Anything).Return("shared-sb", nil)

	mocks.On("ExecuteStep", mock.MatchedBy(func(ei ExecuteStepInput) bool {
		return ei.StepInput.StepDef.ID == "step-1"
	})).Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
	}, nil)
	mocks.On("ExecuteStep", mock.MatchedBy(func(ei ExecuteStepInput) bool {
		return ei.StepInput.StepDef.ID == "step-2"
	})).Return(&model.StepOutput{
		StepID: "step-2",
		Status: model.StepStatusComplete,
	}, nil)

	def := model.WorkflowDef{
		ID:    "test-sandbox-reuse-wf",
		Title: "Sandbox Group Reuse",
		Steps: []model.StepDef{
			{
				ID:           "step-1",
				Title:        "Step 1",
				SandboxGroup: "main",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "step one",
				},
			},
			{
				ID:           "step-2",
				Title:        "Step 2",
				SandboxGroup: "main",
				DependsOn:    []string{"step-1"},
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "step two",
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-sb-reuse-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
	// The DAG provisions the sandbox group once; step-2 reuses it without re-provisioning.
	mocks.AssertNumberOfCalls(t, "ProvisionSandbox", 1)
}

// TestDAGWorkflow_HITLApproval verifies that when a step has approval_policy "always",
// the StepWorkflow pauses awaiting a signal, and an approve signal allows it to continue
// to completion.
func TestDAGWorkflow_HITLApproval(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-hitl", nil)
	mocks.On("ExecuteStep", mock.Anything).Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
		Diff:   "some diff",
		Output: map[string]any{"summary": "done"},
	}, nil)

	// Register a delayed callback to send approve signal to the child StepWorkflow.
	// The child workflow ID is "{runID}-{stepID}" = "run-hitl-1-step-1".
	// Use a non-zero delay so the mock clock fires after the child workflow is registered
	// and blocking on its signal channel.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflowByID("run-hitl-1-step-1", string(SignalApprove), nil) //nolint:errcheck
	}, time.Second)

	def := model.WorkflowDef{
		ID:    "test-hitl-approval-wf",
		Title: "HITL Approval",
		Steps: []model.StepDef{
			{
				ID:             "step-1",
				Title:          "Agent Step",
				ApprovalPolicy: "always",
				Mode:           "transform",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something",
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-hitl-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
}

// TestDAGWorkflow_HITLRejection verifies that when a step has approval_policy "always"
// and the user sends a reject signal, the step fails and the overall workflow fails with
// an error mentioning the step.
func TestDAGWorkflow_HITLRejection(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-hitl", nil)
	mocks.On("ExecuteStep", mock.Anything).Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
		Diff:   "some diff",
		Output: map[string]any{"summary": "done"},
	}, nil)

	// Register a delayed callback to send reject signal to the child StepWorkflow.
	// Use a non-zero delay so the mock clock fires after the child workflow is blocking
	// on its signal channel.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflowByID("run-hitl-2-step-1", string(SignalReject), nil) //nolint:errcheck
	}, time.Second)

	def := model.WorkflowDef{
		ID:    "test-hitl-rejection-wf",
		Title: "HITL Rejection",
		Steps: []model.StepDef{
			{
				ID:             "step-1",
				Title:          "Agent Step",
				ApprovalPolicy: "always",
				Mode:           "transform",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something",
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-hitl-2",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "step-1")
}

// TestDAGWorkflow_OptionalStepFailureDoesntFailRun verifies that when an optional step
// fails, the overall workflow still completes successfully.
func TestDAGWorkflow_OptionalStepFailureDoesntFailRun(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)

	// step-1 succeeds
	mocks.On("ExecuteStep", mock.MatchedBy(func(ei ExecuteStepInput) bool {
		return ei.StepInput.StepDef.ID == "step-1"
	})).Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
	}, nil)

	// step-2 (optional) fails — non-retryable to avoid spurious retries in test env.
	mocks.On("ExecuteStep", mock.MatchedBy(func(ei ExecuteStepInput) bool {
		return ei.StepInput.StepDef.ID == "step-2"
	})).Return(nil, temporal.NewNonRetryableApplicationError(
		"optional step execution failed", "ExecutionError", nil,
	))

	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{
				ID:    "step-1",
				Title: "Required Step",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "required work",
				},
			},
			{
				ID:       "step-2",
				Title:    "Optional Step",
				Optional: true,
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "optional work",
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-optional-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
}

// TestDAGWorkflow_TemplateRendering verifies that prompt templates are rendered with
// workflow parameters before being passed to ExecuteStep. The rendered prompt must
// contain the interpolated parameter value, not the raw template expression.
func TestDAGWorkflow_TemplateRendering(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)
	// ExecuteStep must receive the fully rendered prompt, not the raw template.
	mocks.On("ExecuteStep", mock.MatchedBy(func(ei ExecuteStepInput) bool {
		return strings.Contains(ei.Prompt, "Analyze https://github.com/test/repo")
	})).Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
	}, nil)

	def := model.WorkflowDef{
		ID:    "test-template-render-wf",
		Title: "Template Rendering",
		Steps: []model.StepDef{
			{
				ID:    "step-1",
				Title: "Agent Step",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "Analyze {{ .Params.repo_url }}",
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-template-render-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{"repo_url": "https://github.com/test/repo"},
	})

	require.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
}

// TestDAGWorkflow_ActionConfigRendering verifies that template expressions in action step
// config are rendered using both workflow parameters and prior step outputs before the
// action is dispatched. The resolved config values must appear in the ExecuteAction call.
func TestDAGWorkflow_ActionConfigRendering(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)

	// step-1: agent step that produces output used in step-2's config template.
	mocks.On("ExecuteStep", mock.Anything).Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
		Output: map[string]any{"summary": "all good"},
	}, nil)

	// step-2: action step — config must have rendered values from params and step-1 output.
	mocks.On("ExecuteAction", mock.Anything, "slack_notify", mock.MatchedBy(func(config map[string]any) bool {
		channel, _ := config["channel"].(string)
		message, _ := config["message"].(string)
		return channel == "#ops" && strings.Contains(message, "all good")
	}), mock.Anything, mock.Anything).Return(
		map[string]any{"status": "sent", "channel": "#ops"}, nil,
	)

	def := model.WorkflowDef{
		ID:    "test-action-config-render-wf",
		Title: "Action Config Rendering",
		Steps: []model.StepDef{
			{
				ID:    "step-1",
				Title: "Agent Step",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do the analysis",
				},
			},
			{
				ID:        "step-2",
				Title:     "Notify Step",
				DependsOn: []string{"step-1"},
				Action: &model.ActionDef{
					Type: "slack_notify",
					Config: map[string]any{
						"channel": "{{ .Params.slack_channel }}",
						"message": `Result: {{ (index .Steps "step-1").Output.summary }}`,
					},
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-action-config-render-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{"slack_channel": "#ops"},
	})

	require.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
}

// TestDAGWorkflow_CancellationPath verifies that cancelling the DAG workflow marks the
// run as cancelled and cleans up sandbox groups.
func TestDAGWorkflow_CancellationPath(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)
	mocks.On("ExecuteStep", mock.Anything).Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
	}, nil)

	env.RegisterDelayedCallback(func() {
		env.CancelWorkflow()
	}, 0)

	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{
				ID:           "step-1",
				Title:        "Long Running Step",
				SandboxGroup: "main",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something slow",
				},
			},
			{
				ID:        "step-2",
				Title:     "After Long Step",
				DependsOn: []string{"step-1"},
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something after",
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-cancel-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "canceled")
}

// TestDAGWorkflow_DeadlockDetection verifies that a circular dependency is detected
// as a deadlock instead of hanging forever.
func TestDAGWorkflow_DeadlockDetection(t *testing.T) {
	env, mocks := newDAGTestEnv(t)
	_ = mocks // No step-level mocks needed — deadlock detected before any step runs.

	def := model.WorkflowDef{
		Steps: []model.StepDef{
			{
				ID:        "step-1",
				Title:     "Step A",
				DependsOn: []string{"step-2"},
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "never runs",
				},
			},
			{
				ID:        "step-2",
				Title:     "Step B",
				DependsOn: []string{"step-1"},
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "never runs",
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-deadlock-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deadlock")
}

func TestDAGWorkflow_ResolvesAgentProfile(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	expectedProfile := model.AgentProfileBody{
		Plugins: []model.PluginSource{{Plugin: "plugins/test-plugin"}},
	}

	// Override the default stub with specific expectation
	mocks.ExpectedCalls = removeCall(mocks.ExpectedCalls, "ResolveAgentProfile")
	mocks.On("ResolveAgentProfile", mock.MatchedBy(func(input ResolveProfileInput) bool {
		return input.ProfileName == "test-profile" && input.TeamID == "team-1"
	})).Return(expectedProfile, nil)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)
	mocks.On("ExecuteStep", mock.Anything).Return(&model.StepOutput{StepID: "step1", Status: model.StepStatusComplete}, nil)

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:  "run-1",
		TeamID: "team-1",
		WorkflowDef: model.WorkflowDef{
			AgentProfile: "test-profile",
			Steps: []model.StepDef{{
				ID:        "step1",
				Execution: &model.ExecutionDef{Agent: "claude-code", Prompt: "test"},
			}},
		},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	mocks.AssertCalled(t, "ResolveAgentProfile", mock.MatchedBy(func(input ResolveProfileInput) bool {
		return input.ProfileName == "test-profile"
	}))
}

func TestDAGWorkflow_NoProfileSkipsResolution(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)
	mocks.On("ExecuteStep", mock.Anything).Return(&model.StepOutput{StepID: "step1", Status: model.StepStatusComplete}, nil)

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:  "run-1",
		TeamID: "team-1",
		WorkflowDef: model.WorkflowDef{
			// No AgentProfile set
			Steps: []model.StepDef{{
				ID:        "step1",
				Execution: &model.ExecutionDef{Agent: "claude-code", Prompt: "test"},
			}},
		},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	mocks.AssertNotCalled(t, "ResolveAgentProfile", mock.Anything)
}

func TestDAGWorkflow_MCPCredsIncludedInPreflight(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	profileWithMCPCreds := model.AgentProfileBody{
		MCPs: []model.MCPConfig{{Name: "mcp1", Credentials: []string{"MCP_TOKEN"}}},
	}

	mocks.ExpectedCalls = removeCall(mocks.ExpectedCalls, "ResolveAgentProfile")
	mocks.On("ResolveAgentProfile", mock.Anything).Return(profileWithMCPCreds, nil)

	// Override ValidateCredentials to check MCP_TOKEN is included
	mocks.ExpectedCalls = removeCall(mocks.ExpectedCalls, "ValidateCredentials")
	mocks.On("ValidateCredentials", "team-1", mock.MatchedBy(func(creds []string) bool {
		return slices.Contains(creds, "MCP_TOKEN")
	})).Return(nil)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)
	mocks.On("ExecuteStep", mock.Anything).Return(&model.StepOutput{StepID: "step1", Status: model.StepStatusComplete}, nil)

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:  "run-1",
		TeamID: "team-1",
		WorkflowDef: model.WorkflowDef{
			AgentProfile: "with-mcp",
			Steps: []model.StepDef{{
				ID:        "step1",
				Execution: &model.ExecutionDef{Agent: "claude-code", Prompt: "test", Credentials: []string{"GITHUB_TOKEN"}},
			}},
		},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	mocks.AssertCalled(t, "ValidateCredentials", "team-1", mock.MatchedBy(func(creds []string) bool {
		return slices.Contains(creds, "MCP_TOKEN") && slices.Contains(creds, "GITHUB_TOKEN")
	}))
}

// TestDAGWorkflow_FanOutPartialFailure_ConcurrentStepsSignaledByStepID verifies that
// when two parallel fan-out steps both have partial failures, each receives only its
// own resolve signal. The step receiving "terminate" causes the workflow to fail while
// the step receiving "proceed" completes successfully.
func TestDAGWorkflow_FanOutPartialFailure_ConcurrentStepsSignaledByStepID(t *testing.T) {
	env, mocks := newDAGTestEnv(t)

	mocks.On("ProvisionSandbox", mock.Anything).Return("sb-1", nil)

	// step-1: first call succeeds, second fails (partial failure).
	mocks.On("ExecuteStep", mock.MatchedBy(func(input ExecuteStepInput) bool {
		return input.StepInput.StepDef.ID == "step-1"
	})).Return(&model.StepOutput{
		StepID: "step-1",
		Status: model.StepStatusComplete,
		Output: map[string]any{"summary": "done"},
	}, nil).Once()
	mocks.On("ExecuteStep", mock.MatchedBy(func(input ExecuteStepInput) bool {
		return input.StepInput.StepDef.ID == "step-1"
	})).Return(nil,
		temporal.NewNonRetryableApplicationError("step-1 repo2 failed", "ExecutionError", nil),
	).Once()

	// step-2: first call succeeds, second fails (partial failure).
	mocks.On("ExecuteStep", mock.MatchedBy(func(input ExecuteStepInput) bool {
		return input.StepInput.StepDef.ID == "step-2"
	})).Return(&model.StepOutput{
		StepID: "step-2",
		Status: model.StepStatusComplete,
		Output: map[string]any{"summary": "done"},
	}, nil).Once()
	mocks.On("ExecuteStep", mock.MatchedBy(func(input ExecuteStepInput) bool {
		return input.StepInput.StepDef.ID == "step-2"
	})).Return(nil,
		temporal.NewNonRetryableApplicationError("step-2 repo2 failed", "ExecutionError", nil),
	).Once()

	// Signal both steps: step-1 proceeds, step-2 terminates. Without StepID routing, one
	// step would consume the other's signal and produce incorrect results.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalFanOutResolve, FanOutResolvePayload{Action: "proceed", StepID: "step-1"})   //nolint:errcheck
		env.SignalWorkflow(SignalFanOutResolve, FanOutResolvePayload{Action: "terminate", StepID: "step-2"}) //nolint:errcheck
	}, time.Second)

	def := model.WorkflowDef{
		ID:    "test-concurrent-fanout-wf",
		Title: "Concurrent Fan-Out Routing",
		Steps: []model.StepDef{
			{
				ID:    "step-1",
				Title: "Fan-Out Step 1",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something",
				},
				Repositories: []any{
					map[string]any{"url": "https://github.com/test/repo1"},
					map[string]any{"url": "https://github.com/test/repo2"},
				},
			},
			{
				ID:    "step-2",
				Title: "Fan-Out Step 2",
				Execution: &model.ExecutionDef{
					Agent:  "claude-code",
					Prompt: "do something else",
				},
				Repositories: []any{
					map[string]any{"url": "https://github.com/test/repo3"},
					map[string]any{"url": "https://github.com/test/repo4"},
				},
			},
		},
	}

	env.ExecuteWorkflow(DAGWorkflow, DAGInput{
		RunID:       "run-concurrent-fanout-1",
		TeamID:      "team-1",
		WorkflowDef: def,
		Parameters:  map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	// step-2 terminated → workflow fails and error references step-2.
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "step-2")
}

// removeCall removes all expectations for a given method name from the list.
// This allows overriding default stubs set by newDAGTestEnv.
func removeCall(calls []*mock.Call, methodName string) []*mock.Call {
	filtered := make([]*mock.Call, 0, len(calls))
	for _, c := range calls {
		if c.Method != methodName {
			filtered = append(filtered, c)
		}
	}
	return filtered
}
