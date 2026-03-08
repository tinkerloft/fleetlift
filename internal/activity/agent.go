package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

const (
	agentStaleThreshold = 5 * time.Minute
	pollInterval        = 500 * time.Millisecond
)

// AgentActivities contains activities for the sidecar agent workflow pattern.
type AgentActivities struct {
	Provider sandbox.AgentProvider
}

// NewAgentActivities creates a new AgentActivities.
func NewAgentActivities(provider sandbox.AgentProvider) *AgentActivities {
	return &AgentActivities{Provider: provider}
}

// SubmitTaskManifestInput contains the input for SubmitTaskManifest.
type SubmitTaskManifestInput struct {
	SandboxID string                  `json:"sandbox_id"`
	Manifest  fleetproto.TaskManifest `json:"manifest"`
}

// SubmitTaskManifest writes the task manifest to the sandbox for the agent to execute.
func (a *AgentActivities) SubmitTaskManifest(ctx context.Context, input SubmitTaskManifestInput) error {
	data, err := json.Marshal(input.Manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	return a.Provider.SubmitManifest(ctx, input.SandboxID, data)
}

// WaitForAgentPhaseInput contains the input for WaitForAgentPhase.
type WaitForAgentPhaseInput struct {
	SandboxID    string   `json:"sandbox_id"`
	TargetPhases []string `json:"target_phases"` // e.g., ["awaiting_input", "complete", "failed"]
}

// WaitForAgentPhase polls the agent's status until it reaches one of the target phases.
// Uses Temporal heartbeats for long-running polling.
func (a *AgentActivities) WaitForAgentPhase(ctx context.Context, input WaitForAgentPhaseInput) (*fleetproto.AgentStatus, error) {
	targetSet := make(map[fleetproto.Phase]bool, len(input.TargetPhases)+2)
	for _, p := range input.TargetPhases {
		targetSet[fleetproto.Phase(p)] = true
	}
	// Always include terminal phases so polling does not loop forever on a failed agent.
	targetSet[fleetproto.PhaseFailed] = true
	targetSet[fleetproto.PhaseCancelled] = true

	for {
		activity.RecordHeartbeat(ctx, fmt.Sprintf("polling agent phases: %s", strings.Join(input.TargetPhases, "|")))

		raw, err := a.Provider.PollStatus(ctx, input.SandboxID)
		if err != nil {
			return nil, fmt.Errorf("poll status: %w", err)
		}

		var status fleetproto.AgentStatus
		if err := json.Unmarshal(raw, &status); err != nil {
			return nil, fmt.Errorf("unmarshal status: %w", err)
		}

		if targetSet[status.Phase] {
			return &status, nil
		}

		// Staleness detection: agent may have crashed if it hasn't updated recently.
		if !status.UpdatedAt.IsZero() && time.Since(status.UpdatedAt) > agentStaleThreshold {
			containerStatus, cerr := a.Provider.Status(ctx, input.SandboxID)
			if cerr == nil && containerStatus.Phase != sandbox.SandboxPhaseRunning {
				return nil, fmt.Errorf("agent stale: last update %s, agent phase %s, container %s",
					status.UpdatedAt.Format(time.RFC3339), status.Phase, containerStatus.Phase)
			}
			// Container still running — likely clock skew, continue polling.
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// ReadAgentResultInput contains the input for ReadAgentResult.
type ReadAgentResultInput struct {
	SandboxID string `json:"sandbox_id"`
}

// ReadAgentResult reads the full result from the sandbox agent.
func (a *AgentActivities) ReadAgentResult(ctx context.Context, input ReadAgentResultInput) (*fleetproto.AgentResult, error) {
	data, err := a.Provider.ReadResult(ctx, input.SandboxID)
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
	action := fleetproto.SteeringAction(input.Action)
	switch action {
	case fleetproto.SteeringActionSteer, fleetproto.SteeringActionApprove,
		fleetproto.SteeringActionReject, fleetproto.SteeringActionCancel:
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

	return a.Provider.SubmitSteering(ctx, input.SandboxID, data)
}
