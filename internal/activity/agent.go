package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	agentboxproto "github.com/tinkerloft/agentbox/protocol"
	agentboxsandbox "github.com/tinkerloft/agentbox/sandbox"
	agentboxtemporalkit "github.com/tinkerloft/agentbox/temporalkit"

	"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
)

// AgentActivities contains activities for the sidecar agent workflow pattern.
type AgentActivities struct {
	base *agentboxtemporalkit.AgentActivities
}

// NewAgentActivities creates a new AgentActivities.
func NewAgentActivities(provider agentboxsandbox.AgentProvider) *AgentActivities {
	return &AgentActivities{
		base: &agentboxtemporalkit.AgentActivities{Provider: provider},
	}
}

// SubmitTaskManifestInput contains the input for SubmitTaskManifest.
type SubmitTaskManifestInput struct {
	SandboxID string                `json:"sandbox_id"`
	Manifest  fleetproto.TaskManifest `json:"manifest"`
}

// SubmitTaskManifest writes the task manifest to the sandbox for the agent to execute.
func (a *AgentActivities) SubmitTaskManifest(ctx context.Context, input SubmitTaskManifestInput) error {
	data, err := json.Marshal(input.Manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	return a.base.SubmitManifest(ctx, input.SandboxID, data)
}

// WaitForAgentPhaseInput contains the input for WaitForAgentPhase.
type WaitForAgentPhaseInput struct {
	SandboxID    string   `json:"sandbox_id"`
	TargetPhases []string `json:"target_phases"` // e.g., ["awaiting_input", "complete", "failed"]
}

// WaitForAgentPhase polls the agent's status until it reaches one of the target phases.
// Uses Temporal heartbeats for long-running polling.
func (a *AgentActivities) WaitForAgentPhase(ctx context.Context, input WaitForAgentPhaseInput) (*agentboxproto.AgentStatus, error) {
	raw, err := a.base.WaitForPhase(ctx, input.SandboxID, input.TargetPhases)
	if err != nil {
		return nil, err
	}

	var status agentboxproto.AgentStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return nil, fmt.Errorf("failed to parse agent status: %w", err)
	}

	return &status, nil
}

// ReadAgentResultInput contains the input for ReadAgentResult.
type ReadAgentResultInput struct {
	SandboxID string `json:"sandbox_id"`
}

// ReadAgentResult reads the full result from the sandbox agent.
func (a *AgentActivities) ReadAgentResult(ctx context.Context, input ReadAgentResultInput) (*fleetproto.AgentResult, error) {
	data, err := a.base.ReadResult(ctx, input.SandboxID)
	if err != nil {
		return nil, fmt.Errorf("failed to read result: %w", err)
	}

	var result fleetproto.AgentResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	return &result, nil
}

// SubmitSteeringActionInput contains the input for SubmitSteeringAction.
type SubmitSteeringActionInput struct {
	SandboxID string `json:"sandbox_id"`
	Action    string `json:"action"` // "steer", "approve", "reject", "cancel"
	Prompt    string `json:"prompt,omitempty"`
	Iteration int    `json:"iteration"`
}

// SubmitSteeringAction writes a steering instruction to the sandbox for the agent to process.
func (a *AgentActivities) SubmitSteeringAction(ctx context.Context, input SubmitSteeringActionInput) error {
	// Validate steering action
	action := agentboxproto.SteeringAction(input.Action)
	switch action {
	case agentboxproto.SteeringActionSteer, agentboxproto.SteeringActionApprove,
		agentboxproto.SteeringActionReject, agentboxproto.SteeringActionCancel:
		// valid
	default:
		return fmt.Errorf("invalid steering action: %q", input.Action)
	}

	instruction := fleetproto.SteeringInstruction{
		Action:    action,
		Prompt:    input.Prompt,
		Iteration: input.Iteration,
		Timestamp: time.Now().UTC(),
	}

	data, err := json.Marshal(instruction)
	if err != nil {
		return fmt.Errorf("failed to marshal steering instruction: %w", err)
	}

	return a.base.SubmitSteering(ctx, input.SandboxID, data)
}
