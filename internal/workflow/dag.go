package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"text/template"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/tinkerloft/fleetlift/internal/model"
	fltemplate "github.com/tinkerloft/fleetlift/internal/template"
)

// dbRetry caps retries for short-lived DB/infra activities. Without a bound,
// a permanent error (schema mismatch, wrong type, etc.) retries forever and
// the workflow never terminates. Five attempts with exponential backoff covers
// transient failures while failing fast on permanent ones.
var dbRetry = &temporal.RetryPolicy{MaximumAttempts: 5}

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
func DAGWorkflow(ctx workflow.Context, input DAGInput) (retErr error) {
	logger := workflow.GetLogger(ctx)

	// Mark run as running — do this in the workflow, not the HTTP handler.
	{
		ao := workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second, RetryPolicy: dbRetry}
		if err := workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, ao),
			UpdateRunStatusActivity, input.RunID, string(model.RunStatusRunning), "",
		).Get(ctx, nil); err != nil {
			return fmt.Errorf("mark run running: %w", err)
		}
	}

	defer func() {
		// Use a disconnected context so cleanup activities run even after cancellation.
		dCtx, _ := workflow.NewDisconnectedContext(ctx)
		finalStatus := string(model.RunStatusComplete)
		finalError := ""
		if retErr != nil {
			if temporal.IsCanceledError(retErr) {
				finalStatus = string(model.RunStatusCancelled)
			} else {
				finalStatus = string(model.RunStatusFailed)
				finalError = retErr.Error()
			}
		}
		ao := workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second, RetryPolicy: dbRetry}
		_ = workflow.ExecuteActivity(
			workflow.WithActivityOptions(dCtx, ao),
			UpdateRunStatusActivity, input.RunID, finalStatus, finalError,
		).Get(dCtx, nil)

		// Create inbox notification for completed/failed runs.
		if finalStatus != string(model.RunStatusCancelled) {
			kind := "output_ready"
			title := input.WorkflowDef.Title
			if title == "" {
				title = input.WorkflowTemplateID
			}
			summary := "Run completed successfully"
			if finalStatus == string(model.RunStatusFailed) {
				kind = "output_ready"
				summary = "Run failed"
				if finalError != "" {
					summary = "Run failed: " + finalError
				}
			}
			// For output_ready events, look up the primary artifact.
			primaryArtifactID := ""
			if kind == "output_ready" {
				var artifactID string
				if err := workflow.ExecuteActivity(
					workflow.WithActivityOptions(dCtx, ao),
					GetPrimaryRunArtifactIDActivity, input.RunID,
				).Get(dCtx, &artifactID); err == nil {
					primaryArtifactID = artifactID
				}
				// Non-fatal: proceed without artifact_id if lookup fails
			}
			_ = workflow.ExecuteActivity(
				workflow.WithActivityOptions(dCtx, ao),
				CreateInboxItemActivity, input.TeamID, input.RunID, "", kind, title, summary, primaryArtifactID, "",
			).Get(dCtx, nil)
		}
	}()

	steps := input.WorkflowDef.Steps

	// Resolve agent profile — must happen before credential preflight so MCP
	// credentials are included in the validation pass.
	var effectiveProfile *model.AgentProfileBody
	if input.WorkflowDef.AgentProfile != "" {
		var resolved model.AgentProfileBody
		ao := workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second, RetryPolicy: dbRetry}
		if err := workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, ao),
			ResolveAgentProfileActivity,
			ResolveProfileInput{
				TeamID:      input.TeamID,
				ProfileName: input.WorkflowDef.AgentProfile,
			},
		).Get(ctx, &resolved); err != nil {
			return fmt.Errorf("resolve agent profile: %w", err)
		}
		effectiveProfile = &resolved
	}

	// Preflight: verify all required credentials exist before starting any work.
	// This fails fast instead of discovering missing credentials deep in execution.
	{
		seen := map[string]struct{}{}
		var allCreds []string
		for _, s := range steps {
			var names []string
			if s.Execution != nil {
				names = append(names, s.Execution.Credentials...)
			}
			if s.Action != nil {
				names = append(names, s.Action.Credentials...)
			}
			for _, n := range names {
				if _, ok := seen[n]; !ok {
					seen[n] = struct{}{}
					allCreds = append(allCreds, n)
				}
			}
		}
		// Add MCP credentials from the effective profile
		if effectiveProfile != nil {
			for _, mcp := range effectiveProfile.MCPs {
				for _, credName := range mcp.Credentials {
					if _, ok := seen[credName]; !ok {
						seen[credName] = struct{}{}
						allCreds = append(allCreds, credName)
					}
				}
			}
		}
		sort.Strings(allCreds)
		if len(allCreds) > 0 {
			// MaximumAttempts: 3 — allows recovery from transient DB failures;
			// missing-credential errors are marked non-retryable by the activity itself.
			ao := workflow.ActivityOptions{
				StartToCloseTimeout: 30 * time.Second,
				RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
			}
			if err := workflow.ExecuteActivity(
				workflow.WithActivityOptions(ctx, ao),
				ValidateCredentialsActivity, input.TeamID, allCreds,
			).Get(ctx, nil); err != nil {
				return fmt.Errorf("credential preflight: %w", err)
			}
		}
	}

	outputs := map[string]*model.StepOutput{}
	sandboxes := map[string]string{} // sandbox_group -> sandbox_id
	pending := make(map[string]model.StepDef, len(steps))
	for _, s := range steps {
		pending[s.ID] = s
	}

	// Pre-allocate a per-step channel for fan_out_resolve signals so that concurrent
	// fan-out steps each receive only their own signal. A dispatcher goroutine routes
	// incoming signals from the shared channel to the correct per-step channel.
	fanOutResolveChannels := make(map[string]workflow.Channel)
	for _, s := range steps {
		if s.Repositories != nil {
			fanOutResolveChannels[s.ID] = workflow.NewChannel(ctx)
		}
	}
	if len(fanOutResolveChannels) > 0 {
		sharedResolveCh := workflow.GetSignalChannel(ctx, SignalFanOutResolve)
		workflow.Go(ctx, func(gCtx workflow.Context) {
			for {
				var payload FanOutResolvePayload
				more := sharedResolveCh.Receive(gCtx, &payload)
				if !more {
					return
				}
				if ch, ok := fanOutResolveChannels[payload.StepID]; ok {
					ch.Send(gCtx, payload)
				} else {
					logger.Error("fan_out_resolve for unknown step", "step_id", payload.StepID)
				}
			}
		})
	}

	// Cleanup sandbox groups on any exit path (normal, failure, or cancellation).
	defer func() {
		cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
		cleanupGroups := make([]string, 0, len(sandboxes))
		for group := range sandboxes {
			cleanupGroups = append(cleanupGroups, group)
		}
		sort.Strings(cleanupGroups)
		for _, group := range cleanupGroups {
			sandboxID := sandboxes[group]
			ao := workflow.ActivityOptions{
				StartToCloseTimeout: 2 * time.Minute,
				RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
			}
			_ = workflow.ExecuteActivity(
				workflow.WithActivityOptions(cleanupCtx, ao),
				CleanupSandboxActivity, sandboxID,
			).Get(cleanupCtx, nil)
			logger.Info("cleaned up sandbox group", "group", group)
		}
	}()

	for len(pending) > 0 {
		// Check for cancellation before starting new steps.
		if ctx.Err() != nil {
			return temporal.NewCanceledError()
		}

		ready := findReady(pending, outputs)
		if len(ready) == 0 {
			return fmt.Errorf("DAG deadlock: circular dependency or all steps blocked")
		}

		// Provision sandbox groups for ready steps that need new sandboxes
		for _, step := range ready {
			if step.SandboxGroup != "" && sandboxes[step.SandboxGroup] == "" {
				ao := workflow.ActivityOptions{
					StartToCloseTimeout: 5 * time.Minute,
					RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
				}
				// Build a proper StepInput so ProvisionSandbox gets the right agent/credentials.
				provisionInput := StepInput{
					RunID:  input.RunID,
					TeamID: input.TeamID,
				}
				groupImage := input.WorkflowDef.SandboxGroups[step.SandboxGroup].Image
				if step.Execution != nil {
					provisionInput.ResolvedOpts = ResolvedStepOpts{
						Agent:             step.Execution.Agent,
						Credentials:       step.Execution.Credentials,
						SandboxGroupImage: groupImage,
					}
				} else {
					provisionInput.ResolvedOpts = ResolvedStepOpts{SandboxGroupImage: groupImage}
				}
				var sandboxID string
				err := workflow.ExecuteActivity(
					workflow.WithActivityOptions(ctx, ao),
					ProvisionSandboxActivity, provisionInput,
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
			wg.Add(1)
			workflow.Go(ctx, func(gCtx workflow.Context) {
				defer wg.Done()

				// If any required dependency was skipped or failed, skip this step too
				// rather than attempting to render templates against absent output.
				for _, dep := range step.DependsOn {
					if out, ok := outputs[dep]; ok && (out.Status == model.StepStatusSkipped || out.Status == model.StepStatusFailed) {
						results[i] = &model.StepOutput{
							StepID: step.ID,
							Status: model.StepStatusSkipped,
							Error:  fmt.Sprintf("skipped: dependency %s did not complete successfully", dep),
						}
						return
					}
				}

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
				resolved.EffectiveProfile = effectiveProfile
				// Render eval_plugins template values for this step
				if step.Execution != nil {
					for _, rawURL := range step.Execution.EvalPlugins {
						rendered, renderErr := fltemplate.RenderPrompt(rawURL, fltemplate.RenderContext{
							Params: input.Parameters,
							Steps:  outputs,
						})
						if renderErr != nil {
							results[i] = &model.StepOutput{
								StepID: step.ID,
								Status: model.StepStatusFailed,
								Error:  fmt.Sprintf("render eval_plugin for step %s: %v", step.ID, renderErr),
							}
							return
						}
						resolved.EvalPluginURLs = append(resolved.EvalPluginURLs, rendered)
					}
				}

				// Check condition
				if step.Condition != "" && !evalCondition(gCtx, step.Condition, input.Parameters, outputs) {
					results[i] = &model.StepOutput{StepID: step.ID, Status: model.StepStatusSkipped}
					return
				}

				// Action step — no sandbox needed
				if step.Action != nil {
					// Create step_run record so the step is visible in the UI.
					createAO := workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second, RetryPolicy: dbRetry}
					var stepRunID string
					if err = workflow.ExecuteActivity(
						workflow.WithActivityOptions(gCtx, createAO),
						CreateStepRunActivity, input.RunID, step.ID, step.Title, "", map[string]any(nil),
					).Get(gCtx, &stepRunID); err != nil {
						results[i] = &model.StepOutput{
							StepID: step.ID,
							Status: model.StepStatusFailed,
							Error:  fmt.Sprintf("create step run: %v", err),
						}
						return
					}

					// Resolve template strings in action config.
					configKeys := make([]string, 0, len(step.Action.Config))
					for k := range step.Action.Config {
						configKeys = append(configKeys, k)
					}
					sort.Strings(configKeys)

					resolvedConfig := make(map[string]any, len(step.Action.Config))
					for _, k := range configKeys {
						v := step.Action.Config[k]
						if s, ok := v.(string); ok {
							rendered, renderErr := fltemplate.RenderPrompt(s, fltemplate.RenderContext{
								Params: input.Parameters,
								Steps:  outputs,
							})
							if renderErr != nil {
								failOutput := &model.StepOutput{
									StepID: step.ID,
									Status: model.StepStatusFailed,
									Error:  fmt.Sprintf("render action config %s: %v", k, renderErr),
								}
								_ = finalizeStep(gCtx, logger, stepRunID, failOutput)
								results[i] = failOutput
								return
							}
							resolvedConfig[k] = rendered
						} else {
							resolvedConfig[k] = v
						}
					}
					step.Action.Config = resolvedConfig
					results[i] = executeAction(gCtx, step, input.TeamID, stepRunID, step.Action.Credentials)
					_ = finalizeStep(gCtx, logger, stepRunID, results[i])
					return
				}

				// Agent step — run as child StepWorkflow(s)
				// Fan-out: one child per repo if multiple repos are specified.
				repos := resolved.Repos
				if len(repos) <= 1 {
					// Single execution (no fan-out) — create a step_run record first.
					childWFID := fmt.Sprintf("%s-%s", input.RunID, step.ID)
					createAO := workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second, RetryPolicy: dbRetry}
					var stepRunID string
					var singleStepInput map[string]any
					if len(repos) == 1 {
						singleStepInput = map[string]any{"repo_url": repos[0].URL, "ref": repos[0].Ref}
					}
					if err = workflow.ExecuteActivity(
						workflow.WithActivityOptions(gCtx, createAO),
						CreateStepRunActivity, input.RunID, step.ID, step.Title, childWFID, singleStepInput,
					).Get(gCtx, &stepRunID); err != nil {
						results[i] = &model.StepOutput{
							StepID: step.ID,
							Status: model.StepStatusFailed,
							Error:  fmt.Sprintf("create step run: %v", err),
						}
						return
					}
					cwo := workflow.ChildWorkflowOptions{
						WorkflowID: childWFID,
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
					fanWg.Add(1)
					workflow.Go(gCtx, func(rCtx workflow.Context) {
						defer fanWg.Done()
						// Create a step_run record for each fan-out child.
						fanStepID := fmt.Sprintf("%s-%d", step.ID, j)
						fanChildWFID := fmt.Sprintf("%s-%s-%d", input.RunID, step.ID, j)
						createAO := workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second, RetryPolicy: dbRetry}
						var stepRunID string
						if err := workflow.ExecuteActivity(
							workflow.WithActivityOptions(rCtx, createAO),
							CreateStepRunActivity, input.RunID, fanStepID, step.Title, fanChildWFID,
							map[string]any{"repo_url": repo.URL, "ref": repo.Ref},
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
							WorkflowID: fanChildWFID,
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

				// Check for partial fan-out failure: some repos succeeded, some failed.
				fanSuccesses := 0
				fanFailures := 0
				for _, fr := range fanResults {
					if fr != nil && fr.Status == model.StepStatusFailed {
						fanFailures++
					} else if fr != nil {
						fanSuccesses++
					}
				}

				if fanSuccesses > 0 && fanFailures > 0 {
					// Partial failure: raise inbox item and wait for operator decision.
					inboxAO := workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second, RetryPolicy: dbRetry}
					title := fmt.Sprintf("Fan-out partial failure: %s (%d/%d repos failed)", step.ID, fanFailures, len(repos))
					summary := buildFanOutFailureSummary(fanResults)
					if err := workflow.ExecuteActivity(
						workflow.WithActivityOptions(gCtx, inboxAO),
						CreateInboxItemActivity, input.TeamID, input.RunID, "", "fan_out_partial_failure", title, summary, "", step.ID,
					).Get(gCtx, nil); err != nil {
						logger.Error("failed to create fan-out partial failure inbox item", "error", err)
					}

					// Wait for fan_out_resolve signal with a 48-hour timeout.
					// Using a selector prevents the workflow from blocking forever if the
					// operator never responds to the inbox item.
					resolveCh := fanOutResolveChannels[step.ID]
					var resolvePayload FanOutResolvePayload
					timedOut := false
					sel := workflow.NewSelector(gCtx)
					sel.AddReceive(resolveCh, func(c workflow.ReceiveChannel, _ bool) {
						c.Receive(gCtx, &resolvePayload)
					})
					sel.AddFuture(workflow.NewTimer(gCtx, 48*time.Hour), func(_ workflow.Future) {
						timedOut = true
					})
					sel.Select(gCtx)
					if timedOut {
						results[i] = &model.StepOutput{
							StepID: step.ID,
							Status: model.StepStatusFailed,
							Error:  fmt.Sprintf("fan-out partial failure timed out after 48h waiting for operator decision (%d/%d repos failed)", fanFailures, len(repos)),
						}
						return
					}

					if resolvePayload.Action == "terminate" {
						results[i] = &model.StepOutput{
							StepID: step.ID,
							Status: model.StepStatusFailed,
							Error:  fmt.Sprintf("operator terminated after partial failure (%d/%d repos failed)", fanFailures, len(repos)),
						}
						return
					}
					// proceed: collect only successful results and aggregate them
					var successResults []*model.StepOutput
					for _, fr := range fanResults {
						if fr != nil && fr.Status != model.StepStatusFailed {
							successResults = append(successResults, fr)
						}
					}
					results[i] = aggregateFanOut(step.ID, successResults)
					return
				}

				results[i] = aggregateFanOut(step.ID, fanResults)
			})
		}
		wg.Wait(ctx)

		// Check for cancellation after parallel step execution.
		if ctx.Err() != nil {
			return temporal.NewCanceledError()
		}

		// Collect results
		var stepErrors []string
		for idx, r := range results {
			if r == nil {
				// Goroutine panicked or failed to set result — surface as failure.
				r = &model.StepOutput{
					StepID: ready[idx].ID,
					Status: model.StepStatusFailed,
					Error:  "step goroutine exited without producing a result",
				}
			}
			outputs[r.StepID] = r
			delete(pending, r.StepID)

			if r.Status == model.StepStatusFailed && !isOptional(steps, r.StepID) {
				skipDownstream(pending, r.StepID, steps, outputs)
				msg := fmt.Sprintf("step %s failed", r.StepID)
				if r.Error != "" {
					msg = fmt.Sprintf("step %s failed: %s", r.StepID, r.Error)
				}
				stepErrors = append(stepErrors, msg)
			}
		}
		if len(stepErrors) > 0 {
			return fmt.Errorf("%s", strings.Join(stepErrors, "; "))
		}
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
	opts.MaxTurns = step.Execution.MaxTurns
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
		rendered, err := fltemplate.RenderPrompt(string(b), fltemplate.RenderContext{
			Params: params,
			Steps:  outputs,
		})
		if err != nil {
			return nil, fmt.Errorf("render repositories template: %w", err)
		}
		jsonBytes = []byte(rendered)
	}

	var repos []model.RepoRef
	if err := json.Unmarshal(jsonBytes, &repos); err != nil {
		return nil, fmt.Errorf("parse repositories as []RepoRef: %w", err)
	}
	return repos, nil
}

// buildFanOutFailureSummary returns a newline-joined list of failed fan-out repo errors.
func buildFanOutFailureSummary(results []*model.StepOutput) string {
	var lines []string
	for _, r := range results {
		if r != nil && r.Status == model.StepStatusFailed {
			line := r.StepID
			if r.Error != "" {
				line += ": " + r.Error
			}
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// aggregateFanOut merges per-repo StepOutput results into one aggregate StepOutput.
// Status is complete only if all sub-outputs are complete.
func aggregateFanOut(stepID string, results []*model.StepOutput) *model.StepOutput {
	// Single result — return it directly to preserve Output for downstream templates.
	if len(results) == 1 && results[0] != nil {
		out := *results[0]
		out.StepID = stepID
		return &out
	}

	agg := &model.StepOutput{
		StepID:  stepID,
		Status:  model.StepStatusComplete,
		Outputs: make([]model.StepOutput, len(results)),
	}
	var errs []string
	for i, r := range results {
		if r == nil {
			r = &model.StepOutput{
				StepID: stepID,
				Status: model.StepStatusFailed,
				Error:  "fan-out child failed to produce result",
			}
		}
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
func evalCondition(ctx workflow.Context, condition string, params map[string]any, outputs map[string]*model.StepOutput) bool {
	if condition == "" {
		return true
	}

	steps := map[string]map[string]any{}
	for id, out := range outputs {
		if out != nil {
			steps[id] = map[string]any{
				"status": string(out.Status),
				"error":  out.Error,
				"output": out.Output,
			}
		}
	}

	data := map[string]any{
		"steps":  steps,
		"params": params,
	}

	tmpl, err := template.New("cond").Parse(condition)
	if err != nil {
		workflow.GetLogger(ctx).Warn("condition template parse error — defaulting to false",
			"condition", condition, "error", err)
		return false
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		workflow.GetLogger(ctx).Warn("condition template execute error — defaulting to false",
			"condition", condition, "error", err)
		return false
	}

	return strings.TrimSpace(buf.String()) == "true"
}

// executeAction runs a non-agent action step (e.g., slack notification, GitHub action).
func executeAction(ctx workflow.Context, step model.StepDef, teamID, stepRunID string, credNames []string) *model.StepOutput {
	// Action steps are dispatched to specific activities based on action type
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
	}
	actCtx := workflow.WithActivityOptions(ctx, ao)

	var result map[string]any
	err := workflow.ExecuteActivity(actCtx, "ExecuteAction", stepRunID, step.Action.Type, step.Action.Config, teamID, credNames).Get(actCtx, &result)
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
		if slices.Contains(step.DependsOn, failedID) {
			outputs[step.ID] = &model.StepOutput{
				StepID: step.ID,
				Status: model.StepStatusSkipped,
				Error:  fmt.Sprintf("skipped: dependency %s failed", failedID),
			}
			delete(pending, step.ID)
			// Recursively skip downstream of this skipped step
			skipDownstream(pending, step.ID, allSteps, outputs)
		}
	}
}
