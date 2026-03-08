// Package workflow contains Temporal workflow definitions.
package workflow

import (
	"fmt"

	"go.temporal.io/sdk/workflow"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// GroupTransformInput contains the input for processing a repository group.
type GroupTransformInput struct {
	Task     model.Task
	Group    model.RepositoryGroup
	AgentsMD string
	PRTitle  string
	PRDesc   string
	Approved bool // Pre-approval status (approved at parent workflow level)
}

// GroupTransformResult contains the result of processing a repository group.
type GroupTransformResult struct {
	GroupName    string
	Repositories []model.RepositoryResult
	Error        error
}

// TransformGroup is a child workflow that processes a group of repositories.
// It delegates to TransformV2 with a task scoped to the repos in this group.
//
// Error convention: this workflow always returns (result, nil). Failures are
// embedded in GroupTransformResult.Error and per-repo RepositoryResult.Error
// so that the parent workflow (transformGrouped) can continue processing other
// groups and aggregate results. Callers MUST check result.Error.
func TransformGroup(ctx workflow.Context, input GroupTransformInput) (*GroupTransformResult, error) {
	logger := workflow.GetLogger(ctx)

	task := input.Task
	group := input.Group

	logger.Info("Starting group transform", "group", group.Name, "repos", len(group.Repositories), "taskID", task.ID)

	// Build a per-group task scoped to this group's repositories
	groupTask := task
	groupTask.ID = fmt.Sprintf("%s-%s", task.ID, group.Name)
	groupTask.Repositories = group.Repositories

	// Invoke TransformV2 as a child workflow
	childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID: groupTask.ID + "-exec",
	})

	var taskResult model.TaskResult
	if err := workflow.ExecuteChildWorkflow(childCtx, TransformV2, groupTask).Get(childCtx, &taskResult); err != nil {
		errMsg := fmt.Sprintf("group %s failed: %v", group.Name, err)
		var repoResults []model.RepositoryResult
		for _, repo := range group.Repositories {
			repoResults = append(repoResults, model.RepositoryResult{
				Repository: repo.Name,
				Status:     "failed",
				Error:      &errMsg,
			})
		}
		return &GroupTransformResult{
			GroupName:    group.Name,
			Repositories: repoResults,
			Error:        fmt.Errorf("%s", errMsg),
		}, nil
	}

	logger.Info("Group workflow completed", "group", group.Name, "status", taskResult.Status)

	return &GroupTransformResult{
		GroupName:    group.Name,
		Repositories: taskResult.Repositories,
	}, nil
}
