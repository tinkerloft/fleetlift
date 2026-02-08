package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

// AgentActivities contains activities for the sidecar agent workflow pattern.
type AgentActivities struct {
	provider sandbox.AgentProvider
}

// NewAgentActivities creates a new AgentActivities.
func NewAgentActivities(provider sandbox.AgentProvider) *AgentActivities {
	return &AgentActivities{provider: provider}
}

// SubmitTaskManifestInput contains the input for SubmitTaskManifest.
type SubmitTaskManifestInput struct {
	SandboxID string                `json:"sandbox_id"`
	Manifest  protocol.TaskManifest `json:"manifest"`
}

// SubmitTaskManifest writes the task manifest to the sandbox for the agent to execute.
func (a *AgentActivities) SubmitTaskManifest(ctx context.Context, input SubmitTaskManifestInput) error {
	data, err := json.Marshal(input.Manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	return a.provider.SubmitManifest(ctx, input.SandboxID, data)
}

// WaitForAgentPhaseInput contains the input for WaitForAgentPhase.
type WaitForAgentPhaseInput struct {
	SandboxID    string   `json:"sandbox_id"`
	TargetPhases []string `json:"target_phases"` // e.g., ["awaiting_input", "complete", "failed"]
}

// WaitForAgentPhase polls the agent's status until it reaches one of the target phases.
// Uses Temporal heartbeats for long-running polling.
func (a *AgentActivities) WaitForAgentPhase(ctx context.Context, input WaitForAgentPhaseInput) (*protocol.AgentStatus, error) {
	pollInterval := 500 * time.Millisecond

	targetSet := make(map[protocol.Phase]bool)
	for _, phase := range input.TargetPhases {
		targetSet[protocol.Phase(phase)] = true
	}
	// Always treat terminal phases as targets
	targetSet[protocol.PhaseFailed] = true
	targetSet[protocol.PhaseCancelled] = true

	for {
		activity.RecordHeartbeat(ctx, fmt.Sprintf("polling agent status for phases: %s", strings.Join(input.TargetPhases, "|")))

		status, err := a.provider.PollStatus(ctx, input.SandboxID)
		if err != nil {
			return nil, fmt.Errorf("failed to poll status: %w", err)
		}

		if targetSet[status.Phase] {
			return status, nil
		}

		// Staleness detection: if agent hasn't updated in a while, it may have crashed
		if !status.UpdatedAt.IsZero() && time.Since(status.UpdatedAt) > AgentStaleThreshold {
			return nil, fmt.Errorf("agent stale: last update %s, phase %s",
				status.UpdatedAt.Format(time.RFC3339), status.Phase)
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
func (a *AgentActivities) ReadAgentResult(ctx context.Context, input ReadAgentResultInput) (*protocol.AgentResult, error) {
	data, err := a.provider.ReadResult(ctx, input.SandboxID)
	if err != nil {
		return nil, fmt.Errorf("failed to read result: %w", err)
	}

	var result protocol.AgentResult
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
	instruction := protocol.SteeringInstruction{
		Action:    protocol.SteeringAction(input.Action),
		Prompt:    input.Prompt,
		Iteration: input.Iteration,
		Timestamp: time.Now().UTC(),
	}

	data, err := json.Marshal(instruction)
	if err != nil {
		return fmt.Errorf("failed to marshal steering instruction: %w", err)
	}

	return a.provider.SubmitSteering(ctx, input.SandboxID, data)
}
