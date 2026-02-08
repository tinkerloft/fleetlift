package workflow

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/tinkerloft/fleetlift/internal/activity"
	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// TransformV2 is the agent-mode workflow. Instead of exec'ing commands into the sandbox,
// it submits a task manifest to the sidecar agent and polls for completion.
// This makes the worker non-blocking and resilient to restarts.
func TransformV2(ctx workflow.Context, task model.Task) (*model.TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	startTime := workflow.Now(ctx)

	// Workflow state for query handlers
	var (
		status       = model.TaskStatusPending
		cachedDiffs  []model.DiffOutput
		steeringState = model.SteeringState{MaxIterations: DefaultMaxSteeringIterations}
		cancellationRequested bool
		approved              *bool
		steerRequested        bool
		steeringPrompt        string
	)

	if task.MaxSteeringIterations > 0 {
		steeringState.MaxIterations = task.MaxSteeringIterations
	}

	// Register query handlers
	_ = workflow.SetQueryHandler(ctx, QueryStatus, func() (model.TaskStatus, error) { return status, nil })
	_ = workflow.SetQueryHandler(ctx, QueryDiff, func() ([]model.DiffOutput, error) { return cachedDiffs, nil })
	_ = workflow.SetQueryHandler(ctx, QuerySteeringState, func() (*model.SteeringState, error) { return &steeringState, nil })

	// Signal channels
	approveChannel := workflow.GetSignalChannel(ctx, SignalApprove)
	rejectChannel := workflow.GetSignalChannel(ctx, SignalReject)
	cancelChannel := workflow.GetSignalChannel(ctx, SignalCancel)
	steerChannel := workflow.GetSignalChannel(ctx, SignalSteer)

	doneChannel := workflow.NewChannel(ctx)
	var signalHandlerDone bool

	// Async signal handler
	workflow.Go(ctx, func(ctx workflow.Context) {
		for !signalHandlerDone {
			selector := workflow.NewSelector(ctx)
			selector.AddReceive(doneChannel, func(c workflow.ReceiveChannel, more bool) {
				c.Receive(ctx, nil)
				signalHandlerDone = true
			})
			selector.AddReceive(approveChannel, func(c workflow.ReceiveChannel, more bool) {
				c.Receive(ctx, nil)
				val := true
				approved = &val
			})
			selector.AddReceive(rejectChannel, func(c workflow.ReceiveChannel, more bool) {
				c.Receive(ctx, nil)
				val := false
				approved = &val
			})
			selector.AddReceive(cancelChannel, func(c workflow.ReceiveChannel, more bool) {
				c.Receive(ctx, nil)
				cancellationRequested = true
			})
			selector.AddReceive(steerChannel, func(c workflow.ReceiveChannel, more bool) {
				var payload model.SteeringSignalPayload
				c.Receive(ctx, &payload)
				steerRequested = true
				steeringPrompt = payload.Prompt
			})
			selector.Select(ctx)
		}
	})

	signalDone := func() { doneChannel.Send(ctx, struct{}{}) }

	// Helpers
	failedResult := func(errMsg string) *model.TaskResult {
		signalDone()
		duration := workflow.Now(ctx).Sub(startTime).Seconds()
		return &model.TaskResult{
			TaskID:          task.ID,
			Status:          model.TaskStatusFailed,
			Error:           &errMsg,
			DurationSeconds: &duration,
		}
	}

	cancelledResult := func() *model.TaskResult {
		signalDone()
		duration := workflow.Now(ctx).Sub(startTime).Seconds()
		errMsg := "Workflow cancelled"
		return &model.TaskResult{
			TaskID:          task.ID,
			Status:          model.TaskStatusCancelled,
			Error:           &errMsg,
			DurationSeconds: &duration,
		}
	}

	retryPolicy := &temporal.RetryPolicy{
		InitialInterval:    time.Second,
		MaximumInterval:    time.Minute,
		BackoffCoefficient: 2.0,
		MaximumAttempts:    3,
	}

	shortCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy:         retryPolicy,
	})

	// Cleanup on exit
	var sandboxInfo *model.SandboxInfo
	defer func() {
		if sandboxInfo != nil {
			cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
			cleanupCtx = workflow.WithActivityOptions(cleanupCtx, workflow.ActivityOptions{
				StartToCloseTimeout: 2 * time.Minute,
				RetryPolicy:         retryPolicy,
			})
			_ = workflow.ExecuteActivity(cleanupCtx, activity.ActivityCleanupSandbox, sandboxInfo.ContainerID).Get(cleanupCtx, nil)
		}
	}()

	// --- Pipeline ---

	// 1. Provision sandbox (C2 fix: UseAgentMode, M2 fix: pass image)
	status = model.TaskStatusProvisioning
	logger.Info("TransformV2: provisioning sandbox", "taskID", task.ID)

	provisionInput := activity.ProvisionAgentSandboxInput{
		TaskID: task.ID,
	}
	if task.Execution.Deterministic != nil && task.Execution.Deterministic.Image != "" {
		provisionInput.Image = task.Execution.Deterministic.Image
	}

	if err := workflow.ExecuteActivity(shortCtx, activity.ActivityProvisionAgentSandbox, provisionInput).Get(shortCtx, &sandboxInfo); err != nil {
		return failedResult(fmt.Sprintf("provision failed: %v", err)), nil
	}

	// 2. Submit manifest
	status = model.TaskStatusRunning
	manifest := activity.BuildManifest(task)

	manifestInput := activity.SubmitTaskManifestInput{
		SandboxID: sandboxInfo.ContainerID,
		Manifest:  manifest,
	}
	if err := workflow.ExecuteActivity(shortCtx, activity.ActivitySubmitTaskManifest, manifestInput).Get(shortCtx, nil); err != nil {
		return failedResult(fmt.Sprintf("submit manifest failed: %v", err)), nil
	}

	logger.Info("TransformV2: manifest submitted, waiting for agent")

	// 3. Wait for agent to reach awaiting_input, complete, or failed
	timeoutMinutes := task.GetTimeoutMinutes()
	longCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Duration(timeoutMinutes+10) * time.Minute,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy:         retryPolicy,
	})

	waitInput := activity.WaitForAgentPhaseInput{
		SandboxID:    sandboxInfo.ContainerID,
		TargetPhases: []string{string(protocol.PhaseAwaitingInput), string(protocol.PhaseComplete), string(protocol.PhaseFailed)},
	}
	var agentStatus *protocol.AgentStatus
	if err := workflow.ExecuteActivity(longCtx, activity.ActivityWaitForAgentPhase, waitInput).Get(longCtx, &agentStatus); err != nil {
		return failedResult(fmt.Sprintf("wait for agent failed: %v", err)), nil
	}

	// 4. Read result
	resultInput := activity.ReadAgentResultInput{SandboxID: sandboxInfo.ContainerID}
	var agentResult *protocol.AgentResult
	if err := workflow.ExecuteActivity(shortCtx, activity.ActivityReadAgentResult, resultInput).Get(shortCtx, &agentResult); err != nil {
		return failedResult(fmt.Sprintf("read result failed: %v", err)), nil
	}

	if agentStatus.Phase == protocol.PhaseFailed {
		errMsg := "Agent execution failed"
		if agentResult != nil && agentResult.Error != nil {
			errMsg = *agentResult.Error
		}
		return failedResult(errMsg), nil
	}

	// For report mode or non-approval transform, we may already be complete
	if agentStatus.Phase == protocol.PhaseComplete {
		return buildTaskResultFromAgent(task, agentResult, startTime, workflow.Now(ctx), signalDone), nil
	}

	// 5. HITL steering loop (transform mode with approval)
	if task.RequireApproval {
		cachedDiffs = extractDiffsFromAgent(agentResult)
		status = model.TaskStatusAwaitingApproval

		if task.SlackChannel != nil {
			diffSummary := buildDiffSummary(cachedDiffs)
			msg := fmt.Sprintf("Claude completed %s (agent mode).\n\n%s\n\nReply: `approve`, `reject`, or `steer \"<prompt>\"`",
				task.ID, diffSummary)
			_ = workflow.ExecuteActivity(shortCtx, activity.ActivityNotifySlack, *task.SlackChannel, msg, (*string)(nil)).Get(shortCtx, nil)
		}

	steeringLoop:
		for {
			ok, err := workflow.AwaitWithTimeout(ctx, 24*time.Hour, func() bool {
				return approved != nil || cancellationRequested || steerRequested
			})
			if err != nil || !ok {
				return failedResult("Approval timeout (24 hours)"), nil
			}
			if cancellationRequested {
				// Tell agent to cancel
				cancelInput := activity.SubmitSteeringActionInput{
					SandboxID: sandboxInfo.ContainerID,
					Action:    string(protocol.SteeringActionCancel),
				}
				_ = workflow.ExecuteActivity(shortCtx, activity.ActivitySubmitSteeringAction, cancelInput).Get(shortCtx, nil)
				return cancelledResult(), nil
			}

			if approved != nil && !*approved {
				rejectInput := activity.SubmitSteeringActionInput{
					SandboxID: sandboxInfo.ContainerID,
					Action:    string(protocol.SteeringActionReject),
				}
				_ = workflow.ExecuteActivity(shortCtx, activity.ActivitySubmitSteeringAction, rejectInput).Get(shortCtx, nil)
				return cancelledResult(), nil
			}

			if approved != nil && *approved {
				// Approve → agent creates PRs
				approveInput := activity.SubmitSteeringActionInput{
					SandboxID: sandboxInfo.ContainerID,
					Action:    string(protocol.SteeringActionApprove),
				}
				if err := workflow.ExecuteActivity(shortCtx, activity.ActivitySubmitSteeringAction, approveInput).Get(shortCtx, nil); err != nil {
					return failedResult(fmt.Sprintf("submit approve failed: %v", err)), nil
				}

				// Wait for completion
				waitInput.TargetPhases = []string{string(protocol.PhaseComplete), string(protocol.PhaseFailed)}
				if err := workflow.ExecuteActivity(longCtx, activity.ActivityWaitForAgentPhase, waitInput).Get(longCtx, &agentStatus); err != nil {
					return failedResult(fmt.Sprintf("wait for completion failed: %v", err)), nil
				}

				// Re-read result (now includes PRs)
				if err := workflow.ExecuteActivity(shortCtx, activity.ActivityReadAgentResult, resultInput).Get(shortCtx, &agentResult); err != nil {
					return failedResult(fmt.Sprintf("read final result failed: %v", err)), nil
				}

				break steeringLoop
			}

			if steerRequested {
				if steeringState.CurrentIteration >= steeringState.MaxIterations {
					logger.Warn("Max steering iterations reached", "max", steeringState.MaxIterations)
					steerRequested = false
					steeringPrompt = ""
					continue
				}

				steeringState.CurrentIteration++
				status = model.TaskStatusRunning

				// Submit steer action to agent
				steerInput := activity.SubmitSteeringActionInput{
					SandboxID: sandboxInfo.ContainerID,
					Action:    string(protocol.SteeringActionSteer),
					Prompt:    steeringPrompt,
					Iteration: steeringState.CurrentIteration,
				}
				if err := workflow.ExecuteActivity(shortCtx, activity.ActivitySubmitSteeringAction, steerInput).Get(shortCtx, nil); err != nil {
					logger.Error("Failed to submit steering", "error", err)
					steerRequested = false
					steeringPrompt = ""
					continue
				}

				// Wait for agent to finish steering iteration
				waitInput.TargetPhases = []string{string(protocol.PhaseAwaitingInput), string(protocol.PhaseFailed)}
				if err := workflow.ExecuteActivity(longCtx, activity.ActivityWaitForAgentPhase, waitInput).Get(longCtx, &agentStatus); err != nil {
					logger.Error("Wait for steering result failed", "error", err)
				}

				// Re-read result
				if err := workflow.ExecuteActivity(shortCtx, activity.ActivityReadAgentResult, resultInput).Get(shortCtx, &agentResult); err != nil {
					logger.Warn("Failed to re-read result after steering", "error", err)
				}

				// Record steering
				steeringState.History = append(steeringState.History, model.SteeringIteration{
					IterationNumber: steeringState.CurrentIteration,
					Prompt:          steeringPrompt,
					Timestamp:       workflow.Now(ctx),
				})

				cachedDiffs = extractDiffsFromAgent(agentResult)
				status = model.TaskStatusAwaitingApproval

				if task.SlackChannel != nil {
					diffSummary := buildDiffSummary(cachedDiffs)
					msg := fmt.Sprintf("Steering iteration %d complete for %s.\n\n%s",
						steeringState.CurrentIteration, task.ID, diffSummary)
					_ = workflow.ExecuteActivity(shortCtx, activity.ActivityNotifySlack, *task.SlackChannel, msg, (*string)(nil)).Get(shortCtx, nil)
				}

				// Reset after processing steer
				steerRequested = false
				steeringPrompt = ""
			}
		}
	}

	return buildTaskResultFromAgent(task, agentResult, startTime, workflow.Now(ctx), signalDone), nil
}

// buildTaskResultFromAgent converts an AgentResult to a model.TaskResult.
func buildTaskResultFromAgent(task model.Task, ar *protocol.AgentResult, startTime, endTime time.Time, signalDone func()) *model.TaskResult {
	signalDone()
	duration := endTime.Sub(startTime).Seconds()

	taskResult := model.TaskResult{
		TaskID:          task.ID,
		Status:          model.TaskStatusCompleted,
		Mode:            task.GetMode(),
		DurationSeconds: &duration,
	}

	if ar == nil {
		return &taskResult
	}

	if ar.Status == protocol.PhaseFailed {
		taskResult.Status = model.TaskStatusFailed
		taskResult.Error = ar.Error
	}

	// Convert repo results
	for _, rr := range ar.Repositories {
		repoResult := model.RepositoryResult{
			Repository:    rr.Name,
			Status:        rr.Status,
			FilesModified: rr.FilesModified,
			Error:         rr.Error,
		}

		if rr.PullRequest != nil {
			repoResult.PullRequest = &model.PullRequest{
				RepoName:   rr.Name,
				PRURL:      rr.PullRequest.URL,
				PRNumber:   rr.PullRequest.Number,
				BranchName: rr.PullRequest.BranchName,
				Title:      rr.PullRequest.Title,
			}
		}

		if rr.Report != nil {
			repoResult.Report = &model.ReportOutput{
				Frontmatter:      rr.Report.Frontmatter,
				Body:             rr.Report.Body,
				Raw:              rr.Report.Raw,
				ValidationErrors: rr.Report.ValidationErrors,
			}
		}

		for _, fer := range rr.ForEachResults {
			feExec := model.ForEachExecution{
				Target: model.ForEachTarget{
					Name:    fer.Target.Name,
					Context: fer.Target.Context,
				},
				Error: fer.Error,
			}
			if fer.Report != nil {
				feExec.Report = &model.ReportOutput{
					Frontmatter:      fer.Report.Frontmatter,
					Body:             fer.Report.Body,
					Raw:              fer.Report.Raw,
					ValidationErrors: fer.Report.ValidationErrors,
				}
			}
			repoResult.ForEachResults = append(repoResult.ForEachResults, feExec)
		}

		taskResult.Repositories = append(taskResult.Repositories, repoResult)
	}

	// Populate legacy PullRequests field for backward compatibility
	for _, rr := range taskResult.Repositories {
		if rr.PullRequest != nil {
			taskResult.PullRequests = append(taskResult.PullRequests, *rr.PullRequest) //nolint:staticcheck // backward compat
		}
	}

	return &taskResult
}

// extractDiffsFromAgent converts agent result diffs to model.DiffOutput for query handlers.
func extractDiffsFromAgent(ar *protocol.AgentResult) []model.DiffOutput {
	if ar == nil {
		return nil
	}

	var diffs []model.DiffOutput
	for _, rr := range ar.Repositories {
		diffOutput := model.DiffOutput{
			Repository: rr.Name,
		}
		totalLines := 0
		for _, d := range rr.Diffs {
			diffOutput.Files = append(diffOutput.Files, model.FileDiff{
				Path:      d.Path,
				Status:    d.Status,
				Additions: d.Additions,
				Deletions: d.Deletions,
				Diff:      d.Diff,
			})
			totalLines += d.Additions + d.Deletions
		}
		diffOutput.TotalLines = totalLines
		diffOutput.Summary = fmt.Sprintf("%d files changed", len(rr.Diffs))
		diffs = append(diffs, diffOutput)
	}
	return diffs
}
