# Phase 10: Knowledge Workflow Wiring Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Wire `EnrichPrompt` (before manifest submission) and `CaptureKnowledge` (after approval) into `TransformV2` — the only remaining Phase 10 gap.

**Architecture:** Two non-blocking activity calls added to `internal/workflow/transform_v2.go`. `EnrichPrompt` runs before `BuildManifest` to prepend knowledge lessons to the agentic prompt. `CaptureKnowledge` runs after the steering loop exits via approval, guarded by `KnowledgeCaptureEnabled() && len(steeringState.History) > 0`. Both activities use `MaximumAttempts: 1` (fire-and-forget; failures are logged, not propagated). Original prompt stored before enrichment and used in capture input.

**Tech Stack:** Go, `go.temporal.io/sdk`, `github.com/stretchr/testify/mock`, `github.com/tinkerloft/agentbox/protocol`

**Key files:**
- Modify: `internal/workflow/transform_v2.go`
- Modify: `internal/workflow/transform_v2_test.go`

**What's already done (Phase 10a):**
- `internal/model/knowledge.go` — data model
- `internal/knowledge/store.go` — YAML persistence
- `internal/activity/knowledge.go` — `CaptureKnowledge` + `EnrichPrompt` activities
- `internal/activity/constants.go` — `ActivityEnrichPrompt`, `ActivityCaptureKnowledge`
- `cmd/worker/main.go` — both activities registered with Temporal worker
- `cmd/cli/knowledge.go` — `knowledge list/show/add/delete` commands

**Relevant activity inputs:**
```go
// activity.EnrichPromptInput
type EnrichPromptInput struct {
    OriginalPrompt         string   `json:"original_prompt"`
    FilterTags             []string `json:"filter_tags,omitempty"`
    MaxItems               int      `json:"max_items,omitempty"`
    TransformationRepoPath string   `json:"transformation_repo_path,omitempty"`
}
// returns: (string, error) — enriched prompt, or original if no items found

// activity.CaptureKnowledgeInput
type CaptureKnowledgeInput struct {
    TaskID          string                    `json:"task_id"`
    OriginalPrompt  string                    `json:"original_prompt"`
    SteeringHistory []model.SteeringIteration `json:"steering_history,omitempty"`
    DiffSummary     string                    `json:"diff_summary,omitempty"`
    VerifiersPassed bool                      `json:"verifiers_passed"`
    RepoNames       []string                  `json:"repo_names,omitempty"`
}
// returns: ([]model.KnowledgeItem, error)
```

**Relevant task helpers (already exist in `internal/model/task.go`):**
```go
func (t Task) KnowledgeCaptureEnabled() bool  // default: true
func (t Task) KnowledgeEnrichEnabled() bool    // default: true
func (t Task) KnowledgeMaxItems() int          // default: 10
func (t Task) KnowledgeTags() []string         // extra filter tags
func (t Task) UsesTransformationRepo() bool    // true if t.Transformation != nil
func (t Task) GetEffectiveRepositories() []Repository
```

---

## Task 1: Add knowledge activity mocks and update existing tests

The workflow now calls `EnrichPrompt` and `CaptureKnowledge`. All existing tests must register these mock methods or the Temporal test environment will panic on unregistered activity calls.

**Files:**
- Modify: `internal/workflow/transform_v2_test.go`

**Step 1: Add mock methods to `AgentMockActivities`**

After the existing `NotifySlack` mock method (around line 69), add:

```go
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
```

**Step 2: Register mocks in all existing tests**

In every `TestTransformV2_*` test, after the existing `env.RegisterActivity(...)` calls, add:

```go
env.RegisterActivity(mockActivities.EnrichPrompt)
env.RegisterActivity(mockActivities.CaptureKnowledge)
```

Also add passthrough mock expectations in each test that has agentic execution (all of them):

```go
// EnrichPrompt returns original prompt (passthrough for non-knowledge tests)
mockActivities.On("EnrichPrompt", mock.Anything, mock.Anything).Return("", nil).Maybe()
// CaptureKnowledge is a no-op (only called after steer+approve with history)
mockActivities.On("CaptureKnowledge", mock.Anything, mock.Anything).Return(nil, nil).Maybe()
```

> Note: `Return("", nil)` for EnrichPrompt means the workflow falls back to the original prompt (the `if enrichedPrompt != ""` guard in the implementation).

**Step 3: Run existing tests to verify they still pass**

```bash
go test ./internal/workflow/... -v -run TestTransformV2
```

Expected: all existing tests pass. If any test fails due to unexpected mock calls, add the `.Maybe()` expectations as above.

**Step 4: Commit**

```bash
git add internal/workflow/transform_v2_test.go
git commit -m "test(workflow): add EnrichPrompt+CaptureKnowledge mocks to existing tests"
```

---

## Task 2: Wire EnrichPrompt — TDD

**Files:**
- Modify: `internal/workflow/transform_v2_test.go`
- Modify: `internal/workflow/transform_v2.go`

**Step 1: Write the failing test**

Add after `TestTransformV2_HappyPath`:

```go
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
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/workflow/... -run TestTransformV2_EnrichPrompt_EnrichesManifest -v
```

Expected: FAIL — `SubmitTaskManifest` matcher fails because prompt is not yet enriched.

**Step 3: Implement EnrichPrompt wiring in `transform_v2.go`**

Find the block starting at line 153 (`// 2. Submit manifest`). Insert the EnrichPrompt call between `status = model.TaskStatusRunning` and `manifest := activity.BuildManifest(task)`:

```go
// 2. Submit manifest
status = model.TaskStatusRunning

// Enrich prompt with knowledge from previous runs (non-blocking; errors use original prompt)
originalPrompt := ""
if task.Execution.Agentic != nil {
    originalPrompt = task.Execution.Agentic.Prompt
}
if task.KnowledgeEnrichEnabled() && task.Execution.Agentic != nil {
    enrichCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
        StartToCloseTimeout: 30 * time.Second,
        RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
    })
    enrichInput := activity.EnrichPromptInput{
        OriginalPrompt: task.Execution.Agentic.Prompt,
        FilterTags:     task.KnowledgeTags(),
        MaxItems:       task.KnowledgeMaxItems(),
    }
    if task.UsesTransformationRepo() {
        enrichInput.TransformationRepoPath = "/workspace"
    }
    var enrichedPrompt string
    if err := workflow.ExecuteActivity(enrichCtx, activity.ActivityEnrichPrompt, enrichInput).Get(enrichCtx, &enrichedPrompt); err != nil {
        logger.Warn("TransformV2: EnrichPrompt failed, using original prompt", "error", err)
    } else if enrichedPrompt != "" {
        task.Execution.Agentic.Prompt = enrichedPrompt
    }
}

manifest := activity.BuildManifest(task)
```

**Step 4: Run tests**

```bash
go test ./internal/workflow/... -run TestTransformV2_EnrichPrompt_EnrichesManifest -v
```

Expected: PASS.

**Step 5: Run all workflow tests**

```bash
go test ./internal/workflow/... -v
```

Expected: all pass.

**Step 6: Commit**

```bash
git add internal/workflow/transform_v2.go internal/workflow/transform_v2_test.go
git commit -m "feat(workflow): wire EnrichPrompt before manifest submission"
```

---

## Task 3: Wire CaptureKnowledge — TDD

**Files:**
- Modify: `internal/workflow/transform_v2_test.go`
- Modify: `internal/workflow/transform_v2.go`

**Step 1: Write the failing test**

Add after `TestTransformV2_SteeringLoop_Approve`:

```go
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

	// Send steer, then approve
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalSteer, model.SteeringSignalPayload{Prompt: "Also fix tests"})
	}, 0)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalApprove, nil)
	}, 0)

	env.ExecuteWorkflow(TransformV2, task)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	mockActivities.AssertExpectations(t)
}
```

Also add a test verifying CaptureKnowledge is NOT called when there's no steering history:

```go
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
	// CaptureKnowledge must NOT be called (no .On registration + AssertNotCalled)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalApprove, nil)
	}, 0)

	env.ExecuteWorkflow(TransformV2, task)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	mockActivities.AssertNotCalled(t, "CaptureKnowledge", mock.Anything, mock.Anything)
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/workflow/... -run "TestTransformV2_CaptureKnowledge" -v
```

Expected: FAIL — `CaptureKnowledge` not called (not implemented yet), AssertExpectations fails.

**Step 3: Implement CaptureKnowledge wiring in `transform_v2.go`**

Find the closing `}` of the `if task.RequireApproval {` block (after the `steeringLoop:` for loop, around line 325). Insert before the final `return buildTaskResultFromAgent(...)`:

```go
	} // end if task.RequireApproval

	// Capture knowledge from steering corrections (non-blocking; only when steering occurred)
	if task.KnowledgeCaptureEnabled() && len(steeringState.History) > 0 {
		captureCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 30 * time.Second,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
		})
		repoNames := make([]string, 0, len(task.GetEffectiveRepositories()))
		for _, r := range task.GetEffectiveRepositories() {
			repoNames = append(repoNames, r.Name)
		}
		captureInput := activity.CaptureKnowledgeInput{
			TaskID:          task.ID,
			OriginalPrompt:  originalPrompt,
			SteeringHistory: steeringState.History,
			DiffSummary:     buildDiffSummary(cachedDiffs),
			VerifiersPassed: true,
			RepoNames:       repoNames,
		}
		if err := workflow.ExecuteActivity(captureCtx, activity.ActivityCaptureKnowledge, captureInput).Get(captureCtx, nil); err != nil {
			logger.Warn("TransformV2: CaptureKnowledge failed (non-blocking)", "error", err)
		}
	}

	return buildTaskResultFromAgent(task, agentResult, startTime, workflow.Now(ctx), signalDone), nil
```

> Note: `originalPrompt` is declared in Task 2's EnrichPrompt block. If `task.Execution.Agentic == nil`, `originalPrompt` stays `""` which is fine — CaptureKnowledge's skip guard (`len(steeringHistory) == 0`) would typically prevent the call in non-agentic cases anyway.

**Step 4: Run the new tests**

```bash
go test ./internal/workflow/... -run "TestTransformV2_CaptureKnowledge" -v
```

Expected: both PASS.

**Step 5: Run all workflow tests**

```bash
go test ./internal/workflow/... -v
```

Expected: all pass.

**Step 6: Commit**

```bash
git add internal/workflow/transform_v2.go internal/workflow/transform_v2_test.go
git commit -m "feat(workflow): wire CaptureKnowledge after approval steering loop"
```

---

## Task 4: Final verification

**Step 1: Full test suite**

```bash
go test ./...
```

Expected: all packages pass.

**Step 2: Lint**

```bash
make lint
```

Expected: no errors. If `originalPrompt` is flagged as unused (in the non-RequireApproval path where CaptureKnowledge guard is false), add `_ = originalPrompt` after the EnrichPrompt block. Unlikely since the variable is referenced inside the `if task.RequireApproval` block which is captured by the closure.

**Step 3: Build**

```bash
go build ./...
```

Expected: no errors.

**Step 4: Update implementation plan**

In `docs/plans/IMPLEMENTATION_PLAN.md`, find the Phase 10 section and mark the wiring items as complete:
- `### 10.3 Knowledge Capture Activity` — add `[x]` to wiring into workflow item if present
- `### 10.4 Prompt Enrichment Activity` — same

Also update `docs/plans/ROADMAP.md` Phase 10b section to note that workflow wiring is now complete.

**Step 5: Commit**

```bash
git add docs/plans/IMPLEMENTATION_PLAN.md docs/plans/ROADMAP.md
git commit -m "docs: mark Phase 10 workflow wiring complete"
```

---

## Unanswered Questions

1. **`originalPrompt` scope when `task.Execution.Agentic == nil`**: The `CaptureKnowledge` input uses `originalPrompt` which will be `""` for deterministic tasks. This is fine — deterministic tasks don't have a prompt to capture, and the steering guard `len(steeringState.History) > 0` prevents the call in practice. But if needed, `originalPrompt` can be set to task.Title as a fallback.

2. **TransformGroup workflow**: `TransformGroup` (`transform_group.go`) invokes child `TransformV2` workflows per group — knowledge wiring happens inside each child workflow, so no changes needed to `transform_group.go`.
