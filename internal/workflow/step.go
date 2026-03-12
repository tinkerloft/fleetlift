package workflow

import (
	"fmt"
	"time"

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
	Prompt      string         `json:"prompt"`
	Repos       []model.RepoRef `json:"repos"`
	Verifiers   any            `json:"verifiers,omitempty"`
	Credentials []string       `json:"credentials,omitempty"`
	PRConfig    *model.PRDef   `json:"pr_config,omitempty"`
	Agent       string         `json:"agent"`
}

// ExecuteStepInput is the input to the ExecuteStep activity.
type ExecuteStepInput struct {
	StepInput           StepInput `json:"step_input"`
	SandboxID           string    `json:"sandbox_id"`
	Prompt              string    `json:"prompt"`
	ConversationHistory string    `json:"conversation_history,omitempty"`
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
	ProvisionSandboxActivity  = "ProvisionSandbox"
	ExecuteStepActivity       = "ExecuteStep"
	UpdateStepStatusActivity  = "UpdateStepStatus"
	CreatePRActivity          = "CreatePullRequest"
	CleanupSandboxActivity    = "CleanupSandbox"
	CaptureKnowledgeActivity  = "CaptureKnowledge"
)

// StepWorkflow orchestrates a single step: provision sandbox, run agent, handle HITL signals, optionally create PR.
func StepWorkflow(ctx workflow.Context, input StepInput) (*model.StepOutput, error) {
	logger := workflow.GetLogger(ctx)

	// 1. Provision sandbox (unless reusing from group)
	var sandboxID string
	if input.SandboxID != "" {
		sandboxID = input.SandboxID
	} else {
		ao := workflow.ActivityOptions{StartToCloseTimeout: 5 * time.Minute}
		err := workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, ao),
			ProvisionSandboxActivity, input,
		).Get(ctx, &sandboxID)
		if err != nil {
			return nil, fmt.Errorf("provision sandbox: %w", err)
		}
	}

	// 2. Execute step (may loop for steer)
	var output *model.StepOutput
	prompt := input.ResolvedOpts.Prompt
	conversationHistory := ""

	for {
		ao := workflow.ActivityOptions{
			StartToCloseTimeout: 90 * time.Minute,
			HeartbeatTimeout:    2 * time.Minute,
		}
		err := workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, ao),
			ExecuteStepActivity, ExecuteStepInput{
				StepInput:           input,
				SandboxID:           sandboxID,
				Prompt:              prompt,
				ConversationHistory: conversationHistory,
			},
		).Get(ctx, &output)
		if err != nil {
			return nil, err
		}

		// 3. Evaluate approval policy
		if !shouldPause(input.StepDef, output) {
			break
		}

		// 4. Signal: awaiting_input
		statusAO := workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second}
		_ = workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, statusAO),
			UpdateStepStatusActivity, input.StepRunID, string(model.StepStatusAwaitingInput),
		).Get(ctx, nil)

		logger.Info("step awaiting input", "step_id", input.StepDef.ID)

		// 5. Wait for signal
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
			return &model.StepOutput{
				StepID: input.StepDef.ID,
				Status: model.StepStatusFailed,
				Error:  "rejected by user",
			}, nil
		}
		// Steer: rebuild prompt with history and new instruction
		conversationHistory = fmt.Sprintf("%s\n\nPrevious attempt output:\n%s\n\nSteering instruction:\n%s",
			conversationHistory, output.Diff, steerPayload.Prompt)
		prompt = input.ResolvedOpts.Prompt
	}

	// 6. Create PR if transform mode
	if input.StepDef.Mode == "transform" && input.StepDef.PullRequest != nil {
		prAO := workflow.ActivityOptions{StartToCloseTimeout: 5 * time.Minute}
		_ = workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, prAO),
			CreatePRActivity, sandboxID, input,
		).Get(ctx, &output.PRUrl)
	}

	// 7. Capture knowledge if configured
	if input.StepDef.Knowledge != nil && input.StepDef.Knowledge.Capture {
		captureAO := workflow.ActivityOptions{StartToCloseTimeout: 2 * time.Minute}
		captureInput := model.CaptureKnowledgeInput{
			SandboxID:          sandboxID,
			TeamID:             input.TeamID,
			WorkflowTemplateID: input.WorkflowTemplateID,
			StepRunID:          input.StepRunID,
		}
		_ = workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, captureAO),
			CaptureKnowledgeActivity, captureInput,
		).Get(ctx, nil)
	}

	// 8. Cleanup (unless sandbox_group — DAGWorkflow handles that)
	if input.StepDef.SandboxGroup == "" && input.SandboxID == "" {
		cleanupAO := workflow.ActivityOptions{StartToCloseTimeout: 2 * time.Minute}
		_ = workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, cleanupAO),
			CleanupSandboxActivity, sandboxID,
		).Get(ctx, nil)
	}

	return output, nil
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
