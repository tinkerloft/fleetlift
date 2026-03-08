package workflow

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// TestSubstitutePromptTemplate tests the template substitution function.
func TestSubstitutePromptTemplate(t *testing.T) {
	tests := []struct {
		name    string
		prompt  string
		target  model.ForEachTarget
		want    string
		wantErr bool
	}{
		{
			name:   "simple substitution with Name and Context",
			prompt: "Analyze the {{.Name}} endpoint. Context: {{.Context}}",
			target: model.ForEachTarget{
				Name:    "users-api",
				Context: "Handles user authentication",
			},
			want:    "Analyze the users-api endpoint. Context: Handles user authentication",
			wantErr: false,
		},
		{
			name:   "prompt without template variables (no-op)",
			prompt: "Analyze all endpoints in the repository",
			target: model.ForEachTarget{
				Name:    "users-api",
				Context: "Handles user authentication",
			},
			want:    "Analyze all endpoints in the repository",
			wantErr: false,
		},
		{
			name:   "multiple occurrences of same variable",
			prompt: "Target: {{.Name}}. Processing {{.Name}} now. Done with {{.Name}}.",
			target: model.ForEachTarget{
				Name:    "orders-api",
				Context: "",
			},
			want:    "Target: orders-api. Processing orders-api now. Done with orders-api.",
			wantErr: false,
		},
		{
			name:   "empty context",
			prompt: "Target: {{.Name}}, Context: {{.Context}}",
			target: model.ForEachTarget{
				Name:    "payments-api",
				Context: "",
			},
			want:    "Target: payments-api, Context: ",
			wantErr: false,
		},
		{
			name:   "multiline context",
			prompt: "Target: {{.Name}}\nContext:\n{{.Context}}",
			target: model.ForEachTarget{
				Name: "health-api",
				Context: `Line 1
Line 2
Line 3`,
			},
			want:    "Target: health-api\nContext:\nLine 1\nLine 2\nLine 3",
			wantErr: false,
		},
		{
			name:    "invalid template syntax",
			prompt:  "Bad template: {{.Name",
			target:  model.ForEachTarget{Name: "test"},
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := substitutePromptTemplate(tt.prompt, tt.target)
			if tt.wantErr {
				if err == nil {
					t.Errorf("substitutePromptTemplate() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("substitutePromptTemplate() unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("substitutePromptTemplate() = %q, want %q", got, tt.want)
			}
		})
	}
}

// registerSuccessfulActivities registers all activities needed by TransformV2 and sets up
// default happy-path expectations. Container ID must match what ProvisionAgentSandbox returns.
func registerSuccessfulActivities(t *testing.T, env *testsuite.TestWorkflowEnvironment, sandboxInfo *model.SandboxInfo) *AgentMockActivities {
	t.Helper()
	mockActivities := &AgentMockActivities{}
	env.RegisterActivity(mockActivities.ProvisionAgentSandbox)
	env.RegisterActivity(mockActivities.SubmitTaskManifest)
	env.RegisterActivity(mockActivities.WaitForAgentPhase)
	env.RegisterActivity(mockActivities.ReadAgentResult)
	env.RegisterActivity(mockActivities.CleanupSandbox)
	env.RegisterActivity(mockActivities.EnrichPrompt)
	env.RegisterActivity(mockActivities.CaptureKnowledge)

	mockActivities.On("EnrichPrompt", mock.Anything, mock.Anything).Return("", nil).Maybe()
	mockActivities.On("CaptureKnowledge", mock.Anything, mock.Anything).Return(nil, nil).Maybe()
	mockActivities.On("ProvisionAgentSandbox", mock.Anything, mock.Anything).Return(sandboxInfo, nil)
	mockActivities.On("SubmitTaskManifest", mock.Anything, mock.Anything).Return(nil)
	mockActivities.On("WaitForAgentPhase", mock.Anything, mock.Anything).Return(
		&fleetproto.AgentStatus{Phase: fleetproto.PhaseComplete, Message: "done"}, nil,
	)
	mockActivities.On("ReadAgentResult", mock.Anything, mock.Anything).Return(
		&fleetproto.AgentResult{
			Status: fleetproto.PhaseComplete,
			Repositories: []fleetproto.RepoResult{
				{Name: "svc", Status: "success"},
			},
		}, nil,
	)
	mockActivities.On("CleanupSandbox", mock.Anything, sandboxInfo.ContainerID).Return(nil)
	return mockActivities
}

// TestTransform_SingleGroup_DelegatesToTransformV2 tests that Transform delegates to
// TransformV2 directly when the task has no explicit groups (single group via Repositories).
func TestTransform_SingleGroup_DelegatesToTransformV2(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	task := model.Task{
		Version: 1,
		ID:      "single-group-task",
		Title:   "Single Group Task",
		Mode:    model.TaskModeTransform,
		Repositories: []model.Repository{
			{URL: "https://github.com/org/svc.git", Branch: "main", Name: "svc"},
		},
		Execution: model.Execution{
			Agentic: &model.AgenticExecution{Prompt: "Fix the bug"},
		},
	}

	sandboxInfo := &model.SandboxInfo{ContainerID: "container-single", WorkspacePath: "/workspace"}

	// Register all three workflows (Transform will call TransformV2 directly)
	env.RegisterWorkflow(Transform)
	env.RegisterWorkflow(TransformGroup)
	env.RegisterWorkflow(TransformV2)

	registerSuccessfulActivities(t, env, sandboxInfo)

	env.ExecuteWorkflow(Transform, task)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.TaskResult
	require.NoError(t, env.GetWorkflowResult(&result))

	assert.Equal(t, model.TaskStatusCompleted, result.Status)
	assert.Equal(t, "single-group-task", result.TaskID)
	// When delegating to TransformV2 directly, Groups is nil (not a grouped execution)
	assert.Nil(t, result.Groups)
	require.Len(t, result.Repositories, 1)
	assert.Equal(t, "svc", result.Repositories[0].Repository)
}

// TestTransform_MultiGroup_AllSucceed tests that Transform orchestrates multiple groups
// as child workflows and flattens results when all groups succeed.
func TestTransform_MultiGroup_AllSucceed(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	task := model.Task{
		Version: 1,
		ID:      "multi-group-task",
		Title:   "Multi Group Task",
		Mode:    model.TaskModeTransform,
		Groups: []model.RepositoryGroup{
			{
				Name: "group-a",
				Repositories: []model.Repository{
					{URL: "https://github.com/org/svc-a.git", Branch: "main", Name: "svc-a"},
				},
			},
			{
				Name: "group-b",
				Repositories: []model.Repository{
					{URL: "https://github.com/org/svc-b.git", Branch: "main", Name: "svc-b"},
				},
			},
		},
		MaxParallel: 2, // Run both groups in parallel
		Execution: model.Execution{
			Agentic: &model.AgenticExecution{Prompt: "Fix the bug"},
		},
	}

	// Register workflows; mock TransformGroup so we don't need nested activity mocks
	env.RegisterWorkflow(Transform)
	env.RegisterWorkflow(TransformGroup)
	env.RegisterWorkflow(TransformV2)

	groupAResult := &GroupTransformResult{
		GroupName: "group-a",
		Repositories: []model.RepositoryResult{
			{Repository: "svc-a", Status: "success"},
		},
	}
	groupBResult := &GroupTransformResult{
		GroupName: "group-b",
		Repositories: []model.RepositoryResult{
			{Repository: "svc-b", Status: "success"},
		},
	}

	// Mock TransformGroup for group-a and group-b invocations
	env.OnWorkflow(TransformGroup, mock.Anything, mock.MatchedBy(func(input GroupTransformInput) bool {
		return input.Group.Name == "group-a"
	})).Return(groupAResult, nil)
	env.OnWorkflow(TransformGroup, mock.Anything, mock.MatchedBy(func(input GroupTransformInput) bool {
		return input.Group.Name == "group-b"
	})).Return(groupBResult, nil)

	env.ExecuteWorkflow(Transform, task)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.TaskResult
	require.NoError(t, env.GetWorkflowResult(&result))

	assert.Equal(t, model.TaskStatusCompleted, result.Status)
	assert.Equal(t, "multi-group-task", result.TaskID)

	// Both groups should appear in Groups
	require.Len(t, result.Groups, 2)
	groupNames := []string{result.Groups[0].GroupName, result.Groups[1].GroupName}
	assert.Contains(t, groupNames, "group-a")
	assert.Contains(t, groupNames, "group-b")
	for _, g := range result.Groups {
		assert.Equal(t, "success", g.Status)
	}

	// Repositories flattened from both groups
	require.Len(t, result.Repositories, 2)
	repoNames := []string{result.Repositories[0].Repository, result.Repositories[1].Repository}
	assert.Contains(t, repoNames, "svc-a")
	assert.Contains(t, repoNames, "svc-b")
}

// TestTransform_MultiGroup_FailureThreshold_Abort tests that when the failure threshold is
// exceeded and the action is "abort", remaining groups are skipped.
func TestTransform_MultiGroup_FailureThreshold_Abort(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	task := model.Task{
		Version: 1,
		ID:      "abort-task",
		Title:   "Abort On Failure Task",
		Mode:    model.TaskModeTransform,
		Groups: []model.RepositoryGroup{
			{
				Name: "group-a",
				Repositories: []model.Repository{
					{URL: "https://github.com/org/svc-a.git", Name: "svc-a"},
				},
			},
			{
				Name: "group-b",
				Repositories: []model.Repository{
					{URL: "https://github.com/org/svc-b.git", Name: "svc-b"},
				},
			},
		},
		MaxParallel: 1, // Serial: group-a runs first, then the threshold is checked before group-b
		Failure: &model.FailureConfig{
			ThresholdPercent: 50, // 100% failure in batch of 1 exceeds 50% threshold
			Action:           "abort",
		},
		Execution: model.Execution{
			Agentic: &model.AgenticExecution{Prompt: "Fix the bug"},
		},
	}

	env.RegisterWorkflow(Transform)
	env.RegisterWorkflow(TransformGroup)
	env.RegisterWorkflow(TransformV2)

	// group-a fails at the Temporal workflow level (Get() returns error)
	env.OnWorkflow(TransformGroup, mock.Anything, mock.MatchedBy(func(input GroupTransformInput) bool {
		return input.Group.Name == "group-a"
	})).Return(nil, errors.New("group-a workflow failed"))

	env.ExecuteWorkflow(Transform, task)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.TaskResult
	require.NoError(t, env.GetWorkflowResult(&result))

	// Overall status: not all groups failed (group-b is skipped, not failed)
	// group-a is failed, group-b is skipped → overallStatus should be completed (not all failed)
	require.Len(t, result.Groups, 2)

	// Find group-a and group-b results
	groupByName := make(map[string]model.GroupResult)
	for _, g := range result.Groups {
		groupByName[g.GroupName] = g
	}

	require.Contains(t, groupByName, "group-a")
	require.Contains(t, groupByName, "group-b")

	assert.Equal(t, "failed", groupByName["group-a"].Status)
	assert.Equal(t, "skipped", groupByName["group-b"].Status)
}

// TestTransform_MultiGroup_FailureThreshold_Pause_Continue tests that when the failure
// threshold is exceeded and the action is "pause", the workflow waits for a continue signal
// before processing remaining groups.
func TestTransform_MultiGroup_FailureThreshold_Pause_Continue(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	task := model.Task{
		Version: 1,
		ID:      "pause-task",
		Title:   "Pause On Failure Task",
		Mode:    model.TaskModeTransform,
		Groups: []model.RepositoryGroup{
			{
				Name: "group-a",
				Repositories: []model.Repository{
					{URL: "https://github.com/org/svc-a.git", Name: "svc-a"},
				},
			},
			{
				Name: "group-b",
				Repositories: []model.Repository{
					{URL: "https://github.com/org/svc-b.git", Name: "svc-b"},
				},
			},
		},
		MaxParallel: 1, // Serial: ensures group-a runs before threshold check for group-b
		Failure: &model.FailureConfig{
			ThresholdPercent: 50, // 100% failure in the first batch exceeds 50%
			Action:           "pause",
		},
		Execution: model.Execution{
			Agentic: &model.AgenticExecution{Prompt: "Fix the bug"},
		},
	}

	env.RegisterWorkflow(Transform)
	env.RegisterWorkflow(TransformGroup)
	env.RegisterWorkflow(TransformV2)

	// group-a fails at the Temporal workflow level
	env.OnWorkflow(TransformGroup, mock.Anything, mock.MatchedBy(func(input GroupTransformInput) bool {
		return input.Group.Name == "group-a"
	})).Return(nil, errors.New("group-a workflow failed"))

	// group-b succeeds after the continue signal is received
	groupBResult := &GroupTransformResult{
		GroupName: "group-b",
		Repositories: []model.RepositoryResult{
			{Repository: "svc-b", Status: "success"},
		},
	}
	env.OnWorkflow(TransformGroup, mock.Anything, mock.MatchedBy(func(input GroupTransformInput) bool {
		return input.Group.Name == "group-b"
	})).Return(groupBResult, nil)

	// Send the continue signal with SkipRemaining:false so group-b runs
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalContinue, model.ContinueSignalPayload{SkipRemaining: false})
	}, 0)

	env.ExecuteWorkflow(Transform, task)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.TaskResult
	require.NoError(t, env.GetWorkflowResult(&result))

	require.Len(t, result.Groups, 2)

	groupByName := make(map[string]model.GroupResult)
	for _, g := range result.Groups {
		groupByName[g.GroupName] = g
	}

	require.Contains(t, groupByName, "group-a")
	require.Contains(t, groupByName, "group-b")

	assert.Equal(t, "failed", groupByName["group-a"].Status)
	assert.Equal(t, "success", groupByName["group-b"].Status)

	// svc-b should appear in flattened repos
	require.Len(t, result.Repositories, 1)
	assert.Equal(t, "svc-b", result.Repositories[0].Repository)
}
