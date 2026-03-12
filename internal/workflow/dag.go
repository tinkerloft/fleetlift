package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"text/template"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/tinkerloft/fleetlift/internal/model"
	fltemplate "github.com/tinkerloft/fleetlift/internal/template"
)

// DAGInput is the top-level input for the DAGWorkflow.
type DAGInput struct {
	RunID              string            `json:"run_id"`
	TeamID             string            `json:"team_id"`
	WorkflowTemplateID string            `json:"workflow_template_id,omitempty"`
	WorkflowDef        model.WorkflowDef `json:"workflow_def"`
	Parameters         map[string]any    `json:"parameters"`
}

// DAGWorkflow orchestrates a DAG of steps, running independent steps in parallel
// and respecting dependency edges between them.
func DAGWorkflow(ctx workflow.Context, input DAGInput) error {
	logger := workflow.GetLogger(ctx)
	steps := input.WorkflowDef.Steps
	outputs := map[string]*model.StepOutput{}
	sandboxes := map[string]string{} // sandbox_group -> sandbox_id
	pending := make(map[string]model.StepDef, len(steps))
	for _, s := range steps {
		pending[s.ID] = s
	}

	for len(pending) > 0 {
		ready := findReady(pending, outputs)
		if len(ready) == 0 {
			return fmt.Errorf("DAG deadlock: circular dependency or all steps blocked")
		}

		// Provision sandbox groups for ready steps that need new sandboxes
		for _, step := range ready {
			if step.SandboxGroup != "" && sandboxes[step.SandboxGroup] == "" {
				ao := workflow.ActivityOptions{StartToCloseTimeout: 5 * time.Minute}
				var sandboxID string
				err := workflow.ExecuteActivity(
					workflow.WithActivityOptions(ctx, ao),
					ProvisionSandboxActivity, step,
				).Get(ctx, &sandboxID)
				if err != nil {
					return fmt.Errorf("provision sandbox group %s: %w", step.SandboxGroup, err)
				}
				sandboxes[step.SandboxGroup] = sandboxID
				logger.Info("provisioned sandbox group", "group", step.SandboxGroup, "sandbox_id", sandboxID)
			}
		}

		// Launch ready steps in parallel
		wg := workflow.NewWaitGroup(ctx)
		results := make([]*model.StepOutput, len(ready))

		for i, step := range ready {
			i, step := i, step
			wg.Add(1)
			workflow.Go(ctx, func(gCtx workflow.Context) {
				defer wg.Done()

				// Resolve templates with current outputs + params
				resolved, err := resolveStep(step, input.Parameters, outputs)
				if err != nil {
					results[i] = &model.StepOutput{
						StepID: step.ID,
						Status: model.StepStatusFailed,
						Error:  err.Error(),
					}
					return
				}

				// Check condition
				if step.Condition != "" && !evalCondition(step.Condition, input.Parameters, outputs) {
					results[i] = &model.StepOutput{StepID: step.ID, Status: model.StepStatusSkipped}
					return
				}

				// Action step — no sandbox needed
				if step.Action != nil {
					results[i] = executeAction(gCtx, step, resolved)
					return
				}

				// Agent step — run as child StepWorkflow(s)
				// Fan-out: one child per repo if repos are specified.
				repos := resolved.Repos
				if len(repos) == 0 {
					// Single execution (no fan-out) — create a step_run record first.
					createAO := workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second}
					var stepRunID string
					if err = workflow.ExecuteActivity(
						workflow.WithActivityOptions(gCtx, createAO),
						CreateStepRunActivity, input.RunID, step.ID, step.Title,
					).Get(gCtx, &stepRunID); err != nil {
						results[i] = &model.StepOutput{
							StepID: step.ID,
							Status: model.StepStatusFailed,
							Error:  fmt.Sprintf("create step run: %v", err),
						}
						return
					}
					cwo := workflow.ChildWorkflowOptions{
						WorkflowID: fmt.Sprintf("%s-%s", input.RunID, step.ID),
					}
					var out model.StepOutput
					err = workflow.ExecuteChildWorkflow(
						workflow.WithChildOptions(gCtx, cwo),
						StepWorkflow,
						StepInput{
							RunID:              input.RunID,
							StepRunID:          stepRunID,
							TeamID:             input.TeamID,
							WorkflowTemplateID: input.WorkflowTemplateID,
							StepDef:            step,
							ResolvedOpts:       resolved,
							SandboxID:          sandboxes[step.SandboxGroup],
						},
					).Get(gCtx, &out)
					if err != nil {
						results[i] = &model.StepOutput{
							StepID: step.ID,
							Status: model.StepStatusFailed,
							Error:  err.Error(),
						}
						return
					}
					results[i] = &out
					return
				}

				// Fan-out: one child per repo
				// IMPORTANT: HITL signals cannot be routed to individual fan-out children
				// (they use indexed IDs not tracked by the signal router). Override to prevent hangs.
				if step.ApprovalPolicy != "" && step.ApprovalPolicy != "never" {
					logger.Warn("fan-out steps do not support HITL approval; overriding to 'never'",
						"step_id", step.ID, "original_policy", step.ApprovalPolicy)
					step.ApprovalPolicy = "never"
				}
				fanResults := make([]*model.StepOutput, len(repos))
				fanWg := workflow.NewWaitGroup(gCtx)
				for j, repo := range repos {
					j, repo := j, repo
					fanWg.Add(1)
					workflow.Go(gCtx, func(rCtx workflow.Context) {
						defer fanWg.Done()
						// Create a step_run record for each fan-out child.
						fanStepID := fmt.Sprintf("%s-%d", step.ID, j)
						createAO := workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second}
						var stepRunID string
						if err := workflow.ExecuteActivity(
							workflow.WithActivityOptions(rCtx, createAO),
							CreateStepRunActivity, input.RunID, fanStepID, step.Title,
						).Get(rCtx, &stepRunID); err != nil {
							fanResults[j] = &model.StepOutput{
								StepID: step.ID,
								Status: model.StepStatusFailed,
								Error:  fmt.Sprintf("create step run: %v", err),
							}
							return
						}
						repoResolved := resolved
						repoResolved.Repos = []model.RepoRef{repo}
						cwo := workflow.ChildWorkflowOptions{
							WorkflowID: fmt.Sprintf("%s-%s-%d", input.RunID, step.ID, j),
						}
						var out model.StepOutput
						err := workflow.ExecuteChildWorkflow(
							workflow.WithChildOptions(rCtx, cwo),
							StepWorkflow,
							StepInput{
								RunID:              input.RunID,
								StepRunID:          stepRunID,
								TeamID:             input.TeamID,
								WorkflowTemplateID: input.WorkflowTemplateID,
								StepDef:            step,
								ResolvedOpts:       repoResolved,
								SandboxID:          sandboxes[step.SandboxGroup],
							},
						).Get(rCtx, &out)
						if err != nil {
							fanResults[j] = &model.StepOutput{
								StepID: step.ID,
								Status: model.StepStatusFailed,
								Error:  err.Error(),
							}
							return
						}
						fanResults[j] = &out
					})
				}
				fanWg.Wait(gCtx)
				results[i] = aggregateFanOut(step.ID, fanResults)
			})
		}
		wg.Wait(ctx)

		// Collect results
		for _, r := range results {
			outputs[r.StepID] = r
			delete(pending, r.StepID)

			if r.Status == model.StepStatusFailed && !isOptional(steps, r.StepID) {
				skipDownstream(pending, r.StepID, steps, outputs)
			}
		}
	}

	// Cleanup sandbox groups
	for group, sandboxID := range sandboxes {
		ao := workflow.ActivityOptions{StartToCloseTimeout: 2 * time.Minute}
		_ = workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, ao),
			CleanupSandboxActivity, sandboxID,
		).Get(ctx, nil)
		logger.Info("cleaned up sandbox group", "group", group)
	}

	return nil
}

// findReady returns steps whose dependencies are all satisfied.
func findReady(pending map[string]model.StepDef, done map[string]*model.StepOutput) []model.StepDef {
	var ready []model.StepDef
	for _, step := range pending {
		allDone := true
		for _, dep := range step.DependsOn {
			if _, ok := done[dep]; !ok {
				allDone = false
				break
			}
		}
		if allDone {
			ready = append(ready, step)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i].ID < ready[j].ID })
	return ready
}

// resolveStep renders prompt templates using parameters and prior step outputs.
func resolveStep(step model.StepDef, params map[string]any, outputs map[string]*model.StepOutput) (ResolvedStepOpts, error) {
	var opts ResolvedStepOpts

	if step.Execution == nil {
		return opts, nil
	}

	prompt, err := fltemplate.RenderPrompt(step.Execution.Prompt, fltemplate.RenderContext{
		Params: params,
		Steps:  outputs,
	})
	if err != nil {
		return opts, fmt.Errorf("render prompt for step %s: %w", step.ID, err)
	}

	opts.Prompt = prompt
	opts.Agent = step.Execution.Agent
	if opts.Agent == "" {
		opts.Agent = "claude-code"
	}
	opts.Verifiers = step.Execution.Verifiers
	opts.Credentials = step.Execution.Credentials
	opts.PRConfig = step.PullRequest

	// Resolve repositories
	if step.Repositories != nil {
		repos, err := resolveRepos(step.Repositories, params, outputs)
		if err != nil {
			return opts, fmt.Errorf("resolve repos for step %s: %w", step.ID, err)
		}
		opts.Repos = repos
	}

	return opts, nil
}

// resolveRepos converts step.Repositories (any) into []model.RepoRef.
// Handles two cases:
//   - string: treated as a Go template or literal JSON, result parsed as JSON array of RepoRef
//   - []any (from YAML parsing): marshalled to JSON then unmarshalled as []RepoRef
func resolveRepos(raw any, params map[string]any, outputs map[string]*model.StepOutput) ([]model.RepoRef, error) {
	var jsonBytes []byte

	switch v := raw.(type) {
	case string:
		rendered, err := fltemplate.RenderPrompt(v, fltemplate.RenderContext{
			Params: params,
			Steps:  outputs,
		})
		if err != nil {
			return nil, fmt.Errorf("render repositories template: %w", err)
		}
		jsonBytes = []byte(rendered)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("marshal repositories: %w", err)
		}
		jsonBytes = b
	}

	var repos []model.RepoRef
	if err := json.Unmarshal(jsonBytes, &repos); err != nil {
		return nil, fmt.Errorf("parse repositories as []RepoRef: %w", err)
	}
	return repos, nil
}

// aggregateFanOut merges per-repo StepOutput results into one aggregate StepOutput.
// Status is complete only if all sub-outputs are complete.
func aggregateFanOut(stepID string, results []*model.StepOutput) *model.StepOutput {
	agg := &model.StepOutput{
		StepID:  stepID,
		Status:  model.StepStatusComplete,
		Outputs: make([]model.StepOutput, len(results)),
	}
	var errs []string
	for i, r := range results {
		agg.Outputs[i] = *r
		if r.Status == model.StepStatusFailed {
			agg.Status = model.StepStatusFailed
			if r.Error != "" {
				errs = append(errs, r.Error)
			}
		}
	}
	if len(errs) > 0 {
		agg.Error = strings.Join(errs, "; ")
	}
	return agg
}

// evalCondition evaluates a Go template condition string against step outputs and params.
// Returns true if the condition is empty or evaluates to "true"; false on parse/execute error.
func evalCondition(condition string, params map[string]any, outputs map[string]*model.StepOutput) bool {
	if condition == "" {
		return true
	}

	steps := map[string]map[string]any{}
	for id, out := range outputs {
		if out != nil {
			steps[id] = map[string]any{
				"status": string(out.Status),
				"error":  out.Error,
			}
		}
	}

	data := map[string]any{
		"steps":  steps,
		"params": params,
	}

	tmpl, err := template.New("cond").Parse(condition)
	if err != nil {
		slog.Warn("condition template parse error — defaulting to false",
			"condition", condition, "error", err)
		return false
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		slog.Warn("condition template execute error — defaulting to false",
			"condition", condition, "error", err)
		return false
	}

	return strings.TrimSpace(buf.String()) == "true"
}

// executeAction runs a non-agent action step (e.g., slack notification, GitHub action).
func executeAction(ctx workflow.Context, step model.StepDef, _ ResolvedStepOpts) *model.StepOutput {
	// Action steps are dispatched to specific activities based on action type
	ao := workflow.ActivityOptions{StartToCloseTimeout: 5 * time.Minute}
	actCtx := workflow.WithActivityOptions(ctx, ao)

	var result map[string]any
	err := workflow.ExecuteActivity(actCtx, "ExecuteAction", step.Action.Type, step.Action.Config).Get(actCtx, &result)
	if err != nil {
		return &model.StepOutput{
			StepID: step.ID,
			Status: model.StepStatusFailed,
			Error:  err.Error(),
		}
	}
	return &model.StepOutput{
		StepID: step.ID,
		Status: model.StepStatusComplete,
		Output: result,
	}
}

// isOptional checks if a step is marked as optional.
func isOptional(steps []model.StepDef, stepID string) bool {
	for _, s := range steps {
		if s.ID == stepID {
			return s.Optional
		}
	}
	return false
}

// skipDownstream marks all steps that depend on the failed step as skipped.
func skipDownstream(pending map[string]model.StepDef, failedID string, allSteps []model.StepDef, outputs map[string]*model.StepOutput) {
	for _, step := range allSteps {
		if _, isPending := pending[step.ID]; !isPending {
			continue
		}
		for _, dep := range step.DependsOn {
			if dep == failedID {
				outputs[step.ID] = &model.StepOutput{
					StepID: step.ID,
					Status: model.StepStatusSkipped,
					Error:  fmt.Sprintf("skipped: dependency %s failed", failedID),
				}
				delete(pending, step.ID)
				// Recursively skip downstream of this skipped step
				skipDownstream(pending, step.ID, allSteps, outputs)
				break
			}
		}
	}
}
