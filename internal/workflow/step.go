package workflow

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// StepInput is the input to a StepWorkflow child workflow.
type StepInput struct {
	RunID              string           `json:"run_id"`
	StepRunID          string           `json:"step_run_id"`
	TeamID             string           `json:"team_id"`
	WorkflowTemplateID string           `json:"workflow_template_id,omitempty"`
	StepDef            model.StepDef    `json:"step_def"`
	ResolvedOpts       ResolvedStepOpts `json:"resolved_opts"` // templates already rendered by DAGWorkflow
	SandboxID          string           `json:"sandbox_id"`    // non-empty if sandbox_group reuse
}

// ResolvedStepOpts holds step options after template rendering.
type ResolvedStepOpts struct {
	Prompt           string                  `json:"prompt"`
	Repos            []model.RepoRef         `json:"repos"`
	Verifiers        any                     `json:"verifiers,omitempty"`
	Credentials      []string                `json:"credentials,omitempty"`
	PRConfig         *model.PRDef            `json:"pr_config,omitempty"`
	Agent            string                  `json:"agent"`
	EffectiveProfile *model.AgentProfileBody `json:"effective_profile,omitempty"`
	EvalPluginURLs   []string                `json:"eval_plugin_urls,omitempty"`
}

// ExecuteStepInput is the input to the ExecuteStep activity.
type ExecuteStepInput struct {
	StepInput           StepInput                  `json:"step_input"`
	SandboxID           string                     `json:"sandbox_id"`
	Prompt              string                     `json:"prompt"`
	ConversationHistory string                     `json:"conversation_history,omitempty"`
	ContinuationContext *model.ContinuationContext `json:"continuation_context,omitempty"` // E3
	EvalPluginDirs      []string                   `json:"eval_plugin_dirs,omitempty"`
}

// StepSignal represents signals that can be sent to a StepWorkflow.
type StepSignal string

const (
	SignalApprove StepSignal = "approve"
	SignalReject  StepSignal = "reject"
	SignalSteer   StepSignal = "steer"
	SignalCancel  StepSignal = "cancel"
)

// SteerPayload is the payload for a steer signal.
type SteerPayload struct {
	Prompt string `json:"prompt"`
}

// Activity function references — these are registered in the worker and resolved at runtime.
// They are declared as variables so tests can substitute them.
var (
	ProvisionSandboxActivity          = "ProvisionSandbox"
	ExecuteStepActivity               = "ExecuteStep"
	VerifyStepActivity                = "VerifyStep"
	UpdateStepStatusActivity          = "UpdateStepStatus"
	UpdateRunStatusActivity           = "UpdateRunStatus"
	CreateStepRunActivity             = "CreateStepRun"
	CreatePRActivity                  = "CreatePullRequest"
	CleanupSandboxActivity            = "CleanupSandbox"
	CompleteStepRunActivity           = "CompleteStepRun"
	CreateInboxItemActivity           = "CreateInboxItem"
	ValidateCredentialsActivity       = "ValidateCredentials"
	CreateContinuationStepRunActivity = "CreateContinuationStepRun"
	CleanupCheckpointBranchActivity   = "CleanupCheckpointBranch"
	RunPreflightActivity              = "RunPreflight"
	ResolveAgentProfileActivity       = "ResolveAgentProfile"
)

// ResolveProfileInput is the input to the ResolveAgentProfile activity.
type ResolveProfileInput struct {
	TeamID      string `json:"team_id"`
	ProfileName string `json:"profile_name"`
}

// RunPreflightInput is the input to the RunPreflightActivity.
type RunPreflightInput struct {
	SandboxID      string                 `json:"sandbox_id"`
	TeamID         string                 `json:"team_id"`
	Profile        model.AgentProfileBody `json:"profile"`
	EvalPluginURLs []string               `json:"eval_plugin_urls,omitempty"`
}

// RunPreflightOutput is the output of RunPreflightActivity.
type RunPreflightOutput struct {
	EvalPluginDirs []string `json:"eval_plugin_dirs,omitempty"`
}

// StepWorkflow orchestrates a single step: provision sandbox, run agent, handle HITL signals, optionally create PR.
func StepWorkflow(ctx workflow.Context, input StepInput) (*model.StepOutput, error) {
	logger := workflow.GetLogger(ctx)
	respondCh := workflow.GetSignalChannel(ctx, "respond")

	// 1. Provision sandbox (unless reusing from group)
	var sandboxID string
	if input.SandboxID != "" {
		sandboxID = input.SandboxID
	} else {
		ao := workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Minute,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
		}
		err := workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, ao),
			ProvisionSandboxActivity, input,
		).Get(ctx, &sandboxID)
		if err != nil {
			return nil, fmt.Errorf("provision sandbox: %w", err)
		}
	}

	// Run pre-flight if the step has a profile or eval plugins.
	var evalPluginDirs []string
	if input.ResolvedOpts.EffectiveProfile != nil || len(input.ResolvedOpts.EvalPluginURLs) > 0 {
		profileBody := model.AgentProfileBody{}
		if input.ResolvedOpts.EffectiveProfile != nil {
			profileBody = *input.ResolvedOpts.EffectiveProfile
		}
		var preflightOut RunPreflightOutput
		ao := workflow.ActivityOptions{
			StartToCloseTimeout: 10 * time.Minute,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
		}
		if err := workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, ao),
			RunPreflightActivity,
			RunPreflightInput{
				SandboxID:      sandboxID,
				TeamID:         input.TeamID,
				Profile:        profileBody,
				EvalPluginURLs: input.ResolvedOpts.EvalPluginURLs,
			},
		).Get(ctx, &preflightOut); err != nil {
			return nil, fmt.Errorf("pre-flight: %w", err)
		}
		evalPluginDirs = preflightOut.EvalPluginDirs
	}

	// 2. Execute step (may loop for steer)
	var output *model.StepOutput
	prompt := input.ResolvedOpts.Prompt
	conversationHistory := ""

	for {
		timeout := 90 * time.Minute
		if input.StepDef.Timeout != "" {
			if parsed, err := time.ParseDuration(input.StepDef.Timeout); err == nil {
				timeout = parsed
			} else {
				logger.Warn("invalid step timeout, using default 90m", "timeout", input.StepDef.Timeout)
			}
		}
		ao := workflow.ActivityOptions{
			StartToCloseTimeout: timeout,
			HeartbeatTimeout:    2 * time.Minute,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
		}
		err := workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, ao),
			ExecuteStepActivity, ExecuteStepInput{
				StepInput:           input,
				SandboxID:           sandboxID,
				Prompt:              prompt,
				ConversationHistory: conversationHistory,
				EvalPluginDirs:      evalPluginDirs,
			},
		).Get(ctx, &output)
		if err != nil {
			return nil, err
		}

		// E3: If ExecuteStep returned awaiting_input, wait for human response
		// then create a continuation step and re-execute
		if output != nil && output.Status == model.StepStatusAwaitingInput {
			logger.Info("step awaiting human input", "step_id", input.StepDef.ID, "inbox_item_id", output.InboxItemID)

			var answer model.InboxAnswer
			respondCh.Receive(ctx, &answer)
			logger.Info("received human response", "step_id", input.StepDef.ID, "answer_length", len(answer.Answer))

			// Create continuation step_run record
			continuationStepID := input.StepDef.ID + "-resume-1"
			continuationWorkflowID := input.RunID + "-" + continuationStepID
			var continuationStepRunID string

			contStepAO := workflow.ActivityOptions{
				StartToCloseTimeout: 30 * time.Second,
				RetryPolicy:         dbRetry,
			}
			err = workflow.ExecuteActivity(
				workflow.WithActivityOptions(ctx, contStepAO),
				CreateContinuationStepRunActivity,
				model.CreateContinuationStepRunInput{
					RunID:                input.RunID,
					StepID:               continuationStepID,
					StepTitle:            input.StepDef.Title + " (resumed)",
					TemporalWorkflowID:   continuationWorkflowID,
					ParentStepRunID:      input.StepRunID,
					CheckpointBranch:     output.CheckpointBranch,
					CheckpointArtifactID: output.StateArtifactID,
				},
			).Get(ctx, &continuationStepRunID)
			if err != nil {
				return nil, fmt.Errorf("create continuation step_run: %w", err)
			}

			// Provision a fresh sandbox for the continuation
			var continuationSandboxID string
			contProvAO := workflow.ActivityOptions{
				StartToCloseTimeout: 5 * time.Minute,
				RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
			}
			continuationInput := input
			continuationInput.StepRunID = continuationStepRunID
			continuationInput.SandboxID = "" // force new provision

			err = workflow.ExecuteActivity(
				workflow.WithActivityOptions(ctx, contProvAO),
				ProvisionSandboxActivity, continuationInput,
			).Get(ctx, &continuationSandboxID)
			if err != nil {
				return nil, fmt.Errorf("provision continuation sandbox: %w", err)
			}

			// Re-execute with continuation context
			var continuationOutput *model.StepOutput
			contExecAO := workflow.ActivityOptions{
				StartToCloseTimeout: timeout,
				HeartbeatTimeout:    2 * time.Minute,
				RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
			}
			err = workflow.ExecuteActivity(
				workflow.WithActivityOptions(ctx, contExecAO),
				ExecuteStepActivity, ExecuteStepInput{
					StepInput: continuationInput,
					SandboxID: continuationSandboxID,
					Prompt:    prompt, // original prompt; buildContinuationPrompt prepends context in activity
					ContinuationContext: &model.ContinuationContext{
						InboxItemID:      output.InboxItemID,
						Question:         output.Question,
						HumanAnswer:      answer.Answer,
						CheckpointBranch: output.CheckpointBranch,
						StateArtifactID:  output.StateArtifactID,
					},
					EvalPluginDirs: evalPluginDirs,
				},
			).Get(ctx, &continuationOutput)

			// Cleanup continuation sandbox
			contCleanupAO := workflow.ActivityOptions{
				StartToCloseTimeout: 2 * time.Minute,
				RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
			}
			_ = workflow.ExecuteActivity(
				workflow.WithActivityOptions(ctx, contCleanupAO),
				CleanupSandboxActivity, continuationSandboxID,
			).Get(ctx, nil)

			// Cleanup checkpoint branch if set
			if output.CheckpointBranch != "" && len(input.ResolvedOpts.Repos) > 0 {
				credName := ""
				if len(input.ResolvedOpts.Credentials) > 0 {
					credName = input.ResolvedOpts.Credentials[0]
				}
				_ = workflow.ExecuteActivity(
					workflow.WithActivityOptions(ctx, contCleanupAO),
					CleanupCheckpointBranchActivity, model.CleanupCheckpointInput{
						RepoURL:        input.ResolvedOpts.Repos[0].URL,
						Branch:         output.CheckpointBranch,
						CredentialName: credName,
						TeamID:         input.TeamID,
					},
				).Get(ctx, nil)
			}

			if err != nil {
				return nil, fmt.Errorf("continuation step execution: %w", err)
			}

			// Nested request_input is not supported — fail explicitly rather than
			// silently colliding on step_id "fix-resume-1" a second time.
			if continuationOutput != nil && continuationOutput.Status == model.StepStatusAwaitingInput {
				return nil, fmt.Errorf("nested request_input is not supported: continuation steps cannot request additional human input")
			}

			// Use continuation result as final output
			output = continuationOutput
			// Skip the normal loop — go straight to finalize
			break
		}

		// 3. Run verifiers (if configured)
		if input.ResolvedOpts.Verifiers != nil {
			verifyAO := workflow.ActivityOptions{
				StartToCloseTimeout: 30 * time.Minute,
				HeartbeatTimeout:    2 * time.Minute,
				RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
			}
			if verifyErr := workflow.ExecuteActivity(
				workflow.WithActivityOptions(ctx, verifyAO),
				VerifyStepActivity, sandboxID, input.StepRunID, input.ResolvedOpts.Verifiers,
			).Get(ctx, nil); verifyErr != nil {
				failOutput := &model.StepOutput{
					StepID: input.StepDef.ID,
					Status: model.StepStatusFailed,
					Error:  fmt.Sprintf("verification failed: %v", verifyErr),
				}
				if fErr := finalizeStep(ctx, logger, input.StepRunID, failOutput); fErr != nil {
					return nil, fErr
				}
				return failOutput, nil
			}
		}

		// 4. Evaluate approval policy
		if !shouldPause(input.StepDef, output) {
			break
		}

		// 5. Signal: awaiting_input
		statusAO := workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second, RetryPolicy: dbRetry}
		if err := workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, statusAO),
			UpdateStepStatusActivity, input.StepRunID, string(model.StepStatusAwaitingInput),
		).Get(ctx, nil); err != nil {
			return nil, fmt.Errorf("set step status to awaiting_input: %w", err)
		}

		logger.Info("step awaiting input", "step_id", input.StepDef.ID)

		// 6. Wait for signal
		var steerPayload SteerPayload
		selector := workflow.NewSelector(ctx)
		var approved, rejected, cancelled bool

		selector.AddReceive(workflow.GetSignalChannel(ctx, string(SignalApprove)), func(c workflow.ReceiveChannel, _ bool) {
			c.Receive(ctx, nil)
			approved = true
		})
		selector.AddReceive(workflow.GetSignalChannel(ctx, string(SignalReject)), func(c workflow.ReceiveChannel, _ bool) {
			c.Receive(ctx, nil)
			rejected = true
		})
		selector.AddReceive(workflow.GetSignalChannel(ctx, string(SignalSteer)), func(c workflow.ReceiveChannel, _ bool) {
			c.Receive(ctx, &steerPayload)
		})
		selector.AddReceive(workflow.GetSignalChannel(ctx, string(SignalCancel)), func(c workflow.ReceiveChannel, _ bool) {
			c.Receive(ctx, nil)
			cancelled = true
		})
		selector.Select(ctx)

		if approved {
			break
		}
		if rejected || cancelled {
			rejectOutput := &model.StepOutput{
				StepID: input.StepDef.ID,
				Status: model.StepStatusFailed,
				Error:  "rejected by user",
			}
			if fErr := finalizeStep(ctx, logger, input.StepRunID, rejectOutput); fErr != nil {
				return nil, fErr
			}
			return rejectOutput, nil
		}
		// Steer: rebuild prompt with history and new instruction
		conversationHistory = fmt.Sprintf("%s\n\nPrevious attempt output:\n%s\n\nSteering instruction:\n%s",
			conversationHistory, output.Diff, steerPayload.Prompt)
		prompt = input.ResolvedOpts.Prompt
	}

	// 6. Create PR if transform mode
	if input.StepDef.Mode == "transform" && input.StepDef.PullRequest != nil {
		prAO := workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Minute,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
		}
		if prErr := workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, prAO),
			CreatePRActivity, sandboxID, input,
		).Get(ctx, &output.PRUrl); prErr != nil {
			logger.Error("failed to create pull request", "step_id", input.StepDef.ID, "error", prErr)
			output.Error = fmt.Sprintf("step completed but PR creation failed: %v", prErr)
		}
	}

	// 7. Cleanup (unless sandbox_group — DAGWorkflow handles that)
	if input.StepDef.SandboxGroup == "" && input.SandboxID == "" {
		cleanupAO := workflow.ActivityOptions{
			StartToCloseTimeout: 2 * time.Minute,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
		}
		if err := workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, cleanupAO),
			CleanupSandboxActivity, sandboxID,
		).Get(ctx, nil); err != nil {
			logger.Error("failed to cleanup sandbox", "sandbox_id", sandboxID, "error", err)
			// Sandbox may leak — monitor for orphaned sandboxes
		}
	}

	// 9. Finalize step_run record with status, output, diff, and error.
	if fErr := finalizeStep(ctx, logger, input.StepRunID, output); fErr != nil {
		return nil, fErr
	}

	return output, nil
}

// finalizeStep updates the step_run DB record with the final result.
func finalizeStep(ctx workflow.Context, logger log.Logger, stepRunID string, output *model.StepOutput) error {
	if stepRunID == "" || output == nil {
		return nil
	}
	ao := workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second, RetryPolicy: dbRetry}
	if err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, ao),
		CompleteStepRunActivity,
		stepRunID,
		string(output.Status),
		output.Output,
		output.Diff,
		output.Error,
		output.CostUSD,
	).Get(ctx, nil); err != nil {
		logger.Error("failed to finalize step run", "step_run_id", stepRunID, "error", err)
		return fmt.Errorf("finalize step run %s: %w", stepRunID, err)
	}
	return nil
}

func shouldPause(def model.StepDef, output *model.StepOutput) bool {
	switch def.ApprovalPolicy {
	case "always":
		return true
	case "never", "":
		return false
	case "agent":
		v, _ := output.Output["needs_review"].(bool)
		return v
	case "on_changes":
		return output.Diff != ""
	}
	return false
}
