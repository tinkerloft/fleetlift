# Phase 10.6: Grouped Execution Orchestration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Wire multi-group orchestration into `Transform` so tasks with multiple groups run each group as a parallel `TransformGroup` child workflow, with failure threshold checking and result aggregation.

**Architecture:** `Transform` branches on group count: 1 group → `TransformV2` directly (unchanged); N groups → new orchestration loop that runs `TransformGroup` children in batches of `max_parallel`, checks failure thresholds after each batch, pauses/aborts on breach, and aggregates `GroupResult` into `TaskResult`. Knowledge capture flows automatically through `TransformV2` inside each child.

**Tech Stack:** Go, Temporal SDK (`go.temporal.io/sdk`), testify, `testsuite.WorkflowTestSuite`

---

### Task 1: Add multi-group orchestration to `Transform`

**Files:**
- Modify: `internal/workflow/transform.go`

**Step 1: Replace the `Transform` stub with the full orchestration function**

Open `internal/workflow/transform.go`. Replace the current `Transform` function (which just calls `TransformV2`) with:

```go
// Transform is the main workflow entry point for code transformations.
// For tasks with a single group (or no explicit groups), it delegates directly to TransformV2.
// For tasks with multiple groups, it orchestrates parallel group execution with failure thresholds.
func Transform(ctx workflow.Context, task model.Task) (*model.TaskResult, error) {
	groups := task.GetExecutionGroups()
	if len(groups) <= 1 {
		// Single-group path: delegate to TransformV2 unchanged (preserves all signal handling)
		return TransformV2(ctx, task)
	}
	return transformGrouped(ctx, task, groups)
}

// transformGrouped orchestrates execution across multiple repository groups.
func transformGrouped(ctx workflow.Context, task model.Task, groups []model.RepositoryGroup) (*model.TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	startTime := workflow.Now(ctx)

	// Progress state (for query handler)
	progress := model.ExecutionProgress{
		TotalGroups: len(groups),
	}

	_ = workflow.SetQueryHandler(ctx, QueryExecutionProgress, func() (model.ExecutionProgress, error) {
		return progress, nil
	})

	// Continue signal (for pause/resume on failure threshold breach)
	var continuePayload model.ContinueSignalPayload
	continueReceived := false
	continueChannel := workflow.GetSignalChannel(ctx, SignalContinue)

	maxParallel := task.GetMaxParallel()
	var allGroups []model.GroupResult
	var allRepos []model.RepositoryResult

	// Process groups in batches of maxParallel
	for batchStart := 0; batchStart < len(groups); batchStart += maxParallel {
		batchEnd := batchStart + maxParallel
		if batchEnd > len(groups) {
			batchEnd = len(groups)
		}
		batch := groups[batchStart:batchEnd]

		logger.Info("transformGrouped: starting batch", "start", batchStart, "end", batchEnd, "groups", len(batch))

		// Launch all groups in this batch concurrently
		type groupOutcome struct {
			result *GroupTransformResult
			err    error
		}
		outcomes := make([]groupOutcome, len(batch))
		batchDone := workflow.NewWaitGroup(ctx)

		for i, group := range batch {
			i, group := i, group // capture loop vars
			batchDone.Add(1)
			workflow.Go(ctx, func(ctx workflow.Context) {
				defer batchDone.Done()
				childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
					WorkflowID: fmt.Sprintf("%s-%s", task.ID, group.Name),
				})
				input := GroupTransformInput{
					Task:  task,
					Group: group,
				}
				var result GroupTransformResult
				err := workflow.ExecuteChildWorkflow(childCtx, TransformGroup, input).Get(childCtx, &result)
				outcomes[i] = groupOutcome{result: &result, err: err}
			})
		}

		// Wait for entire batch
		batchDone.Wait(ctx)

		// Collect batch results
		for i, outcome := range outcomes {
			groupName := batch[i].Name
			gr := model.GroupResult{GroupName: groupName}

			if outcome.err != nil {
				errMsg := outcome.err.Error()
				gr.Status = "failed"
				gr.Error = &errMsg
				progress.FailedGroups++
				progress.FailedGroupNames = append(progress.FailedGroupNames, groupName)
			} else if outcome.result != nil && outcome.result.Error != nil {
				errMsg := outcome.result.Error.Error()
				gr.Status = "failed"
				gr.Error = &errMsg
				gr.Repositories = outcome.result.Repositories
				progress.FailedGroups++
				progress.FailedGroupNames = append(progress.FailedGroupNames, groupName)
			} else if outcome.result != nil {
				gr.Status = "success"
				gr.Repositories = outcome.result.Repositories
			}

			allGroups = append(allGroups, gr)
			allRepos = append(allRepos, gr.Repositories...)
			progress.CompletedGroups++
		}

		if progress.CompletedGroups > 0 {
			progress.FailurePercent = float64(progress.FailedGroups) / float64(progress.CompletedGroups) * 100
		}

		// Check failure threshold (skip if this was the last batch)
		remainingStart := batchEnd
		if remainingStart < len(groups) && task.ShouldPauseOnFailure(progress.CompletedGroups, progress.FailedGroups) {
			action := task.GetFailureAction()
			logger.Warn("transformGrouped: failure threshold exceeded", "action", action,
				"failed", progress.FailedGroups, "completed", progress.CompletedGroups)

			if action == "abort" {
				// Mark remaining groups as skipped
				for _, g := range groups[remainingStart:] {
					allGroups = append(allGroups, model.GroupResult{GroupName: g.Name, Status: "skipped"})
				}
				break
			}

			// action == "pause" (default)
			progress.IsPaused = true
			progress.PausedReason = fmt.Sprintf("failure threshold exceeded (%.0f%%)", progress.FailurePercent)

			ok, _ := workflow.AwaitWithTimeout(ctx, 24*time.Hour, func() bool {
				continueChannel.ReceiveAsync(&continuePayload)
				return continueReceived || continuePayload != (model.ContinueSignalPayload{})
			})

			progress.IsPaused = false

			if !ok || continuePayload.SkipRemaining {
				// Timeout or skip-remaining: mark remaining as skipped
				for _, g := range groups[remainingStart:] {
					allGroups = append(allGroups, model.GroupResult{GroupName: g.Name, Status: "skipped"})
				}
				break
			}
			// Resume: loop continues naturally
		}
	}

	// Determine overall status
	overallStatus := model.TaskStatusCompleted
	if progress.FailedGroups == progress.TotalGroups {
		overallStatus = model.TaskStatusFailed
	}

	duration := workflow.Now(ctx).Sub(startTime).Seconds()
	return &model.TaskResult{
		TaskID:          task.ID,
		Status:          overallStatus,
		Groups:          allGroups,
		Repositories:    allRepos,
		DurationSeconds: &duration,
	}, nil
}
```

Also add `"fmt"` and `"time"` to the imports in `transform.go` if not already present.

**Step 2: Build to confirm it compiles**

```bash
go build ./...
```

Expected: no errors. Fix any compile issues (missing imports, type mismatches).

**Step 3: Commit**

```bash
git add internal/workflow/transform.go
git commit -m "feat(workflow): add multi-group orchestration to Transform"
```

---

### Task 2: Add tests for multi-group orchestration

**Files:**
- Modify: `internal/workflow/transform_test.go`

Temporal's test suite supports child workflows: register the child workflow with `env.RegisterWorkflow(TransformGroup)`. The test suite executes it inline.

Because `TransformGroup` calls `TransformV2` which calls activities, you need to register mock activities in the test environment too.

**Step 1: Write failing tests**

Add the following tests to `internal/workflow/transform_test.go`:

```go
package workflow

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/tinkerloft/fleetlift/internal/activity"
	"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// makeGroupTask builds a Task with N explicit groups for testing.
func makeGroupTask(groups []model.RepositoryGroup) model.Task {
	return model.Task{
		Version: 1,
		ID:      "test-grouped",
		Title:   "Grouped Test",
		Mode:    model.TaskModeTransform,
		Groups:  groups,
		Execution: model.Execution{
			Agentic: &model.AgenticExecution{Prompt: "Fix it"},
		},
	}
}

// setupGroupEnv creates a test environment with all required workflows and mock activities registered.
func setupGroupEnv(t *testing.T) (*testsuite.TestWorkflowEnvironment, *AgentMockActivities) {
	t.Helper()
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.RegisterWorkflow(Transform)
	env.RegisterWorkflow(TransformGroup)
	env.RegisterWorkflow(TransformV2)

	mockActs := &AgentMockActivities{}
	env.RegisterActivity(mockActs.ProvisionAgentSandbox)
	env.RegisterActivity(mockActs.SubmitTaskManifest)
	env.RegisterActivity(mockActs.WaitForAgentPhase)
	env.RegisterActivity(mockActs.ReadAgentResult)
	env.RegisterActivity(mockActs.CleanupSandbox)
	env.RegisterActivity(mockActs.EnrichPrompt)
	env.RegisterActivity(mockActs.CaptureKnowledge)

	return env, mockActs
}

// stubSuccessfulAgent sets up mock expectations for a single successful agent run.
func stubSuccessfulAgent(mockActs *AgentMockActivities, containerID, repoName string) {
	sandboxInfo := &model.SandboxInfo{ContainerID: containerID, WorkspacePath: "/workspace"}
	mockActs.On("ProvisionAgentSandbox", mock.Anything, mock.Anything).Return(sandboxInfo, nil).Once()
	mockActs.On("SubmitTaskManifest", mock.Anything, mock.Anything).Return(nil).Once()
	mockActs.On("WaitForAgentPhase", mock.Anything, mock.Anything).Return(
		&fleetproto.AgentStatus{Phase: fleetproto.PhaseComplete}, nil,
	).Once()
	mockActs.On("ReadAgentResult", mock.Anything, mock.Anything).Return(
		&fleetproto.AgentResult{
			Status: fleetproto.PhaseComplete,
			Repositories: []fleetproto.RepoResult{
				{Name: repoName, Status: "success", FilesModified: []string{"main.go"}},
			},
		}, nil,
	).Once()
	mockActs.On("CleanupSandbox", mock.Anything, containerID).Return(nil).Once()
	mockActs.On("EnrichPrompt", mock.Anything, mock.Anything).Return("", nil).Maybe()
	mockActs.On("CaptureKnowledge", mock.Anything, mock.Anything).Return(nil, nil).Maybe()
}

// TestTransform_SingleGroup_DelegatesToTransformV2 verifies the single-group path is unchanged.
func TestTransform_SingleGroup_DelegatesToTransformV2(t *testing.T) {
	env, mockActs := setupGroupEnv(t)

	task := model.Task{
		Version: 1,
		ID:      "single-group",
		Mode:    model.TaskModeTransform,
		Repositories: []model.Repository{
			{URL: "https://github.com/org/svc.git", Name: "svc"},
		},
		Execution: model.Execution{Agentic: &model.AgenticExecution{Prompt: "Fix it"}},
	}

	stubSuccessfulAgent(mockActs, "container-single", "svc")

	env.ExecuteWorkflow(Transform, task)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.TaskResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Equal(t, model.TaskStatusCompleted, result.Status)
	assert.Len(t, result.Repositories, 1)
	assert.Nil(t, result.Groups) // single-group path doesn't populate Groups
}

// TestTransform_MultiGroup_AllSucceed verifies all groups run and results are aggregated.
func TestTransform_MultiGroup_AllSucceed(t *testing.T) {
	env, mockActs := setupGroupEnv(t)

	groups := []model.RepositoryGroup{
		{Name: "alpha", Repositories: []model.Repository{{URL: "https://github.com/org/alpha.git", Name: "alpha"}}},
		{Name: "beta", Repositories: []model.Repository{{URL: "https://github.com/org/beta.git", Name: "beta"}}},
	}
	task := makeGroupTask(groups)

	stubSuccessfulAgent(mockActs, "container-alpha", "alpha")
	stubSuccessfulAgent(mockActs, "container-beta", "beta")

	env.ExecuteWorkflow(Transform, task)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.TaskResult
	require.NoError(t, env.GetWorkflowResult(&result))

	assert.Equal(t, model.TaskStatusCompleted, result.Status)
	assert.Len(t, result.Groups, 2)
	assert.Len(t, result.Repositories, 2)
	assert.Equal(t, "success", result.Groups[0].Status)
	assert.Equal(t, "success", result.Groups[1].Status)
}

// TestTransform_MultiGroup_FailureThreshold_Abort verifies abort stops remaining groups.
func TestTransform_MultiGroup_FailureThreshold_Abort(t *testing.T) {
	env, mockActs := setupGroupEnv(t)

	groups := []model.RepositoryGroup{
		{Name: "g1", Repositories: []model.Repository{{URL: "https://github.com/org/g1.git", Name: "g1"}}},
		{Name: "g2", Repositories: []model.Repository{{URL: "https://github.com/org/g2.git", Name: "g2"}}},
	}
	task := makeGroupTask(groups)
	task.MaxParallel = 1 // serial: g1 first, then check threshold before g2
	task.Failure = &model.FailureConfig{ThresholdPercent: 50, Action: "abort"}

	// g1 fails (provision error)
	mockActs.On("ProvisionAgentSandbox", mock.Anything, mock.Anything).
		Return(nil, fmt.Errorf("provision failed")).Once()
	mockActs.On("CleanupSandbox", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockActs.On("EnrichPrompt", mock.Anything, mock.Anything).Return("", nil).Maybe()
	mockActs.On("CaptureKnowledge", mock.Anything, mock.Anything).Return(nil, nil).Maybe()

	env.ExecuteWorkflow(Transform, task)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.TaskResult
	require.NoError(t, env.GetWorkflowResult(&result))

	assert.Len(t, result.Groups, 2)
	assert.Equal(t, "failed", result.Groups[0].Status)  // g1 failed
	assert.Equal(t, "skipped", result.Groups[1].Status) // g2 skipped due to abort
}

// TestTransform_MultiGroup_FailureThreshold_Pause_Continue verifies pause then continue.
func TestTransform_MultiGroup_FailureThreshold_Pause_Continue(t *testing.T) {
	env, mockActs := setupGroupEnv(t)

	groups := []model.RepositoryGroup{
		{Name: "g1", Repositories: []model.Repository{{URL: "https://github.com/org/g1.git", Name: "g1"}}},
		{Name: "g2", Repositories: []model.Repository{{URL: "https://github.com/org/g2.git", Name: "g2"}}},
	}
	task := makeGroupTask(groups)
	task.MaxParallel = 1
	task.Failure = &model.FailureConfig{ThresholdPercent: 50, Action: "pause"}

	// g1 fails
	mockActs.On("ProvisionAgentSandbox", mock.Anything, mock.Anything).
		Return(nil, fmt.Errorf("provision failed")).Once()
	// g2 succeeds (sent after continue signal)
	stubSuccessfulAgent(mockActs, "container-g2", "g2")
	mockActs.On("CleanupSandbox", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockActs.On("EnrichPrompt", mock.Anything, mock.Anything).Return("", nil).Maybe()

	// Send continue signal after workflow pauses
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalContinue, model.ContinueSignalPayload{SkipRemaining: false})
	}, time.Millisecond*100)

	env.ExecuteWorkflow(Transform, task)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.TaskResult
	require.NoError(t, env.GetWorkflowResult(&result))

	assert.Len(t, result.Groups, 2)
	assert.Equal(t, "failed", result.Groups[0].Status)
	assert.Equal(t, "success", result.Groups[1].Status) // g2 ran after continue
}
```

Note: `fmt` is already imported in the test file via `import "fmt"` — add it if missing.

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/workflow/... -run "TestTransform_Multi|TestTransform_Single" -v
```

Expected: compile errors or test failures (the new `transformGrouped` function doesn't exist yet, or tests catch missing behavior).

**Step 3: Run tests after Task 1 is complete**

```bash
go test ./internal/workflow/... -run "TestTransform_Multi|TestTransform_Single" -v
```

Expected: all 4 new tests PASS.

**Step 4: Run full test suite**

```bash
go test ./...
```

Expected: all tests pass.

**Step 5: Lint**

```bash
make lint
```

Expected: no errors. Fix any lint issues.

**Step 6: Commit**

```bash
git add internal/workflow/transform_test.go
git commit -m "test(workflow): add multi-group orchestration tests"
```

---

### Task 3: Update implementation plan

**Files:**
- Modify: `docs/plans/IMPLEMENTATION_PLAN.md`

**Step 1: Mark Phase 10.6 complete**

In `IMPLEMENTATION_PLAN.md`, change:

```markdown
### 10.6 Workflow Integration 🔄 Partial

- Knowledge capture and prompt enrichment are wired into the single-repo `Transform` workflow.
- [ ] Grouped execution: knowledge capture runs per-group; all groups contribute to the same knowledge pool (single-group path done; grouped path not yet wired)
```

to:

```markdown
### 10.6 Workflow Integration ✅ Complete

- Knowledge capture and prompt enrichment are wired into the single-repo `Transform` workflow.
- Grouped execution: `Transform` orchestrates N groups via `TransformGroup` children in parallel batches; knowledge capture runs per-group through `TransformV2`; all groups contribute to the same knowledge pool.
```

Also update the summary table row for Phase 10 from `🔄 Mostly complete (10.8 deferred)` to `✅ Complete (10.8 deferred)`.

And update the "Polish Track" recommended next steps to remove the Phase 10.6 item.

**Step 2: Commit**

```bash
git add docs/plans/IMPLEMENTATION_PLAN.md
git commit -m "docs: mark Phase 10.6 complete"
```

---

## Verification Checklist

- [ ] `go build ./...` passes
- [ ] `go test ./...` passes
- [ ] `make lint` passes
- [ ] `TestTransform_SingleGroup_DelegatesToTransformV2` passes (backward compat preserved)
- [ ] `TestTransform_MultiGroup_AllSucceed` passes
- [ ] `TestTransform_MultiGroup_FailureThreshold_Abort` passes
- [ ] `TestTransform_MultiGroup_FailureThreshold_Pause_Continue` passes
- [ ] Implementation plan updated
