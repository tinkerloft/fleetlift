// Package workflow contains Temporal workflow definitions.
package workflow

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// Signal and query names
const (
	SignalApprove  = "approve"
	SignalReject   = "reject"
	SignalCancel   = "cancel"
	SignalSteer    = "steer"
	SignalContinue = "continue"

	QueryStatus            = "get_status"
	QueryResult            = "get_claude_result"
	QueryDiff              = "get_diff"
	QueryVerifierLogs      = "get_verifier_logs"
	QuerySteeringState     = "get_steering_state"
	QueryExecutionProgress = "get_execution_progress"
	QuerySandboxID         = "get_sandbox_id"
)

// DefaultMaxSteeringIterations is the default limit for steering iterations.
const DefaultMaxSteeringIterations = 5

// Transform is the main workflow for code transformations.
// When the task has multiple groups, it orchestrates parallel group execution.
// Otherwise it delegates to TransformV2 which uses the sidecar agent pattern.
func Transform(ctx workflow.Context, task model.Task) (*model.TaskResult, error) {
	groups := task.GetExecutionGroups()
	if len(groups) <= 1 {
		return TransformV2(ctx, task)
	}
	return transformGrouped(ctx, task, groups)
}

// transformGrouped runs each group as a TransformGroup child workflow in parallel
// batches, with failure threshold checking and pause/abort support.
func transformGrouped(ctx workflow.Context, task model.Task, groups []model.RepositoryGroup) (*model.TaskResult, error) {
	startTime := workflow.Now(ctx)
	logger := workflow.GetLogger(ctx)

	// Track progress state that is exposed via the query handler.
	progress := model.ExecutionProgress{
		TotalGroups:      len(groups),
		FailedGroupNames: []string{},
	}

	_ = workflow.SetQueryHandler(ctx, QueryExecutionProgress, func() (model.ExecutionProgress, error) {
		return progress, nil
	})

	continueChannel := workflow.GetSignalChannel(ctx, SignalContinue)

	maxParallel := task.GetMaxParallel()
	allGroupResults := make([]model.GroupResult, 0, len(groups))

	// Process groups in batches.
	for batchStart := 0; batchStart < len(groups); batchStart += maxParallel {
		batchEnd := batchStart + maxParallel
		if batchEnd > len(groups) {
			batchEnd = len(groups)
		}
		batch := groups[batchStart:batchEnd]

		// Launch all groups in this batch concurrently.
		batchOutcomes := make([]model.GroupResult, len(batch))
		wg := workflow.NewWaitGroup(ctx)
		for i, group := range batch {
			i, group := i, group
			wg.Add(1)
			workflow.Go(ctx, func(ctx workflow.Context) {
				defer wg.Done()
				childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
					WorkflowID: fmt.Sprintf("%s-%s", task.ID, group.Name),
				})
				input := GroupTransformInput{Task: task, Group: group}
				var result GroupTransformResult
				// TransformGroup embeds failures in result.Error rather than returning a non-nil error.
				// Check result.Error explicitly — a nil err does not mean the group succeeded.
				err := workflow.ExecuteChildWorkflow(childCtx, TransformGroup, input).Get(childCtx, &result)
				if err != nil {
					errMsg := err.Error()
					batchOutcomes[i] = model.GroupResult{
						GroupName: group.Name,
						Status:    "failed",
						Error:     &errMsg,
					}
					return
				}
				groupStatus := "success"
				if result.Error != nil {
					groupStatus = "failed"
					errMsg := result.Error.Error()
					batchOutcomes[i] = model.GroupResult{
						GroupName:    result.GroupName,
						Status:       groupStatus,
						Repositories: result.Repositories,
						Error:        &errMsg,
					}
					return
				}
				batchOutcomes[i] = model.GroupResult{
					GroupName:    result.GroupName,
					Status:       groupStatus,
					Repositories: result.Repositories,
				}
			})
		}
		wg.Wait(ctx)

		// Collect batch results and update progress.
		for _, gr := range batchOutcomes {
			allGroupResults = append(allGroupResults, gr)
			progress.CompletedGroups++
			if gr.Status == "failed" {
				progress.FailedGroups++
				progress.FailedGroupNames = append(progress.FailedGroupNames, gr.GroupName)
			}
		}
		if progress.CompletedGroups > 0 {
			progress.FailurePercent = (float64(progress.FailedGroups) / float64(progress.CompletedGroups)) * 100
		}

		// Check failure threshold if there are remaining groups.
		remainingStart := batchEnd
		hasRemaining := remainingStart < len(groups)
		if hasRemaining && task.ShouldPauseOnFailure(progress.CompletedGroups, progress.FailedGroups) {
			action := task.GetFailureAction()
			logger.Info("Failure threshold exceeded", "action", action, "failedGroups", progress.FailedGroups, "completedGroups", progress.CompletedGroups)

			if action == "abort" {
				// Mark remaining groups as skipped.
				for _, g := range groups[remainingStart:] {
					allGroupResults = append(allGroupResults, model.GroupResult{
						GroupName: g.Name,
						Status:    "skipped",
					})
				}
				break
			}

			// action == "pause" (default)
			progress.IsPaused = true
			progress.PausedReason = fmt.Sprintf("failure threshold exceeded: %.1f%% failed (threshold: %d%%)",
				progress.FailurePercent, task.GetFailureThresholdPercent())

			var continuePayload model.ContinueSignalPayload
			received := false
			ok, _ := workflow.AwaitWithTimeout(ctx, 24*time.Hour, func() bool {
				if continueChannel.ReceiveAsync(&continuePayload) {
					received = true
					return true
				}
				return false
			})

			progress.IsPaused = false
			progress.PausedReason = ""

			if !ok || (received && continuePayload.SkipRemaining) {
				// Timeout or explicit skip — mark remaining as skipped.
				for _, g := range groups[remainingStart:] {
					allGroupResults = append(allGroupResults, model.GroupResult{
						GroupName: g.Name,
						Status:    "skipped",
					})
				}
				break
			}
			// Otherwise continue with next batch.
		}
	}

	// Determine overall status.
	failedCount := 0
	for _, gr := range allGroupResults {
		if gr.Status == "failed" {
			failedCount++
		}
	}
	overallStatus := model.TaskStatusCompleted
	if failedCount == len(allGroupResults) && len(allGroupResults) > 0 {
		overallStatus = model.TaskStatusFailed
	}

	// Flatten repositories from all groups.
	var allRepos []model.RepositoryResult
	for _, gr := range allGroupResults {
		allRepos = append(allRepos, gr.Repositories...)
	}

	duration := workflow.Now(ctx).Sub(startTime).Seconds()
	result := model.TaskResult{
		TaskID:          task.ID,
		Status:          overallStatus,
		Mode:            task.GetMode(),
		Groups:          allGroupResults,
		Repositories:    allRepos,
		DurationSeconds: &duration,
	}

	return &result, nil
}

// substitutePromptTemplate substitutes {{.Name}} and {{.Context}} variables in the prompt.
func substitutePromptTemplate(prompt string, target model.ForEachTarget) (string, error) {
	tmpl, err := template.New("prompt").Parse(prompt)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, target); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// buildDiffSummary creates a human-readable summary of diffs for notifications.
func buildDiffSummary(diffs []model.DiffOutput) string {
	if len(diffs) == 0 {
		return "No changes detected."
	}

	var sb strings.Builder
	sb.WriteString("**Diff summary:**\n")

	for _, diff := range diffs {
		if len(diff.Files) == 0 {
			sb.WriteString(fmt.Sprintf("- **%s**: no changes\n", diff.Repository))
			continue
		}

		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", diff.Repository, diff.Summary))
		for _, f := range diff.Files {
			sb.WriteString(fmt.Sprintf("  - %s (%s, +%d/-%d)\n", f.Path, f.Status, f.Additions, f.Deletions))
		}
	}

	return sb.String()
}
