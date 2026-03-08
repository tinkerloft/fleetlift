// Package agent implements the sandbox sidecar agent that executes task manifests autonomously.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
)

// Pipeline executes the full task lifecycle from manifest to completion.
type Pipeline struct {
	basePath string
	fs       FileSystem
	exec     CommandExecutor
	logger   *slog.Logger
	proto    *Protocol
}

// NewPipeline creates a new agent pipeline with explicit dependencies.
func NewPipeline(basePath string, fs FileSystem, exec CommandExecutor, logger *slog.Logger) *Pipeline {
	return &Pipeline{
		basePath: basePath,
		fs:       fs,
		exec:     exec,
		logger:   logger,
		proto:    NewProtocol(basePath, fs),
	}
}

// NewDefaultPipeline creates a pipeline with real OS implementations.
func NewDefaultPipeline(basePath string) *Pipeline {
	return NewPipeline(basePath, osFileSystem{}, osCommandExecutor{}, slog.New(slog.NewJSONHandler(os.Stderr, nil)))
}

// Execute runs the full task pipeline for the given manifest.
// The caller is responsible for acquiring the manifest (e.g., via Protocol.WaitForManifest).
func (p *Pipeline) Execute(ctx context.Context, manifest *fleetproto.TaskManifest) error {
	startedAt := time.Now().UTC()
	result := &fleetproto.AgentResult{
		Status:    fleetproto.PhaseExecuting,
		StartedAt: startedAt,
	}

	// Helper to fail with error
	fail := func(errMsg string) error {
		p.logger.Error("Pipeline failed", "error", errMsg)
		now := time.Now().UTC()
		result.Status = fleetproto.PhaseFailed
		result.Error = &errMsg
		result.CompletedAt = &now
		if err := p.writeResult(result); err != nil {
			p.logger.Warn("Failed to write failure result", "error", err)
		}
		p.writeStatus(fleetproto.AgentStatus{
			Phase:     fleetproto.PhaseFailed,
			Message:   errMsg,
			UpdatedAt: now,
		})
		return fmt.Errorf("%s", errMsg)
	}

	// 1. Clone repositories
	p.writeStatus(fleetproto.AgentStatus{
		Phase:     fleetproto.PhaseExecuting,
		Step:      "cloning",
		Message:   "Cloning repositories...",
		UpdatedAt: time.Now().UTC(),
	})

	if err := p.cloneRepos(ctx, manifest); err != nil {
		return fail(fmt.Sprintf("clone failed: %v", err))
	}

	// 2. Execute transformation
	p.writeStatus(fleetproto.AgentStatus{
		Phase:     fleetproto.PhaseExecuting,
		Step:      "running_transformation",
		Message:   "Running transformation...",
		UpdatedAt: time.Now().UTC(),
	})

	agentOutput, err := p.runTransformation(ctx, manifest)
	if err != nil {
		return fail(fmt.Sprintf("transformation failed: %v", err))
	}
	result.AgentOutput = agentOutput

	// 3. Run verifiers
	if len(manifest.Verifiers) > 0 {
		p.writeStatus(fleetproto.AgentStatus{
			Phase:     fleetproto.PhaseVerifying,
			Step:      "running_verifiers",
			Message:   "Running verifiers...",
			UpdatedAt: time.Now().UTC(),
		})
	}
	verifierResults := p.runVerifiers(ctx, manifest)

	// 4. Collect results (diffs, modified files, reports)
	repoResults := p.collectResults(ctx, manifest, verifierResults)
	result.Repositories = repoResults

	// Write initial result
	if err := p.writeResult(result); err != nil {
		p.logger.Warn("Failed to write initial result", "error", err)
	}

	// 5. HITL loop or auto-complete
	if manifest.RequireApproval && manifest.Mode == "transform" {
		result.Status = fleetproto.PhaseAwaitingInput
		if err := p.writeResult(result); err != nil {
			p.logger.Warn("Failed to write awaiting result", "error", err)
		}
		p.writeStatus(fleetproto.AgentStatus{
			Phase:     fleetproto.PhaseAwaitingInput,
			Message:   "Awaiting human input",
			UpdatedAt: time.Now().UTC(),
		})

		finalAction, err := p.steeringLoop(ctx, manifest, result)
		if err != nil {
			return fail(fmt.Sprintf("steering loop failed: %v", err))
		}

		if finalAction == fleetproto.SteeringActionCancel || finalAction == fleetproto.SteeringActionReject {
			now := time.Now().UTC()
			result.Status = fleetproto.PhaseCancelled
			result.CompletedAt = &now
			if err := p.writeResult(result); err != nil {
				p.logger.Warn("Failed to write cancelled result", "error", err)
			}
			p.writeStatus(fleetproto.AgentStatus{
				Phase:     fleetproto.PhaseCancelled,
				Message:   "Cancelled by user",
				UpdatedAt: now,
			})
			return nil
		}
	}

	// 6. Create PRs (transform mode only)
	if manifest.Mode == "transform" && manifest.PullRequest != nil {
		p.writeStatus(fleetproto.AgentStatus{
			Phase:     fleetproto.PhaseCreatingPRs,
			Message:   "Creating pull requests...",
			UpdatedAt: time.Now().UTC(),
		})

		result.Repositories = p.createPullRequests(ctx, manifest, result.Repositories)
		if err := p.writeResult(result); err != nil {
			p.logger.Warn("Failed to write PR result", "error", err)
		}
	}

	// 7. Complete
	now := time.Now().UTC()
	result.Status = fleetproto.PhaseComplete
	result.CompletedAt = &now
	if err := p.writeResult(result); err != nil {
		p.logger.Warn("Failed to write final result", "error", err)
	}
	p.writeStatus(fleetproto.AgentStatus{
		Phase:     fleetproto.PhaseComplete,
		Message:   "Task completed",
		UpdatedAt: now,
	})

	p.logger.Info("Pipeline completed successfully")
	return nil
}

// steeringLoop handles the HITL steering cycle.
func (p *Pipeline) steeringLoop(ctx context.Context, manifest *fleetproto.TaskManifest, result *fleetproto.AgentResult) (fleetproto.SteeringAction, error) {
	maxIterations := manifest.MaxSteeringIterations
	if maxIterations <= 0 {
		maxIterations = DefaultMaxSteering
	}

	iteration := 0
	for {
		// Check for context cancellation (e.g., timeout)
		select {
		case <-ctx.Done():
			return fleetproto.SteeringAction(""), fmt.Errorf("steering loop timed out: %w", ctx.Err())
		default:
		}

		instruction, err := p.waitForSteering(ctx)
		if err != nil {
			return fleetproto.SteeringAction(""), err
		}

		switch instruction.Action {
		case fleetproto.SteeringActionApprove:
			p.logger.Info("Received approve")
			return fleetproto.SteeringActionApprove, nil

		case fleetproto.SteeringActionReject, fleetproto.SteeringActionCancel:
			p.logger.Info("Received action", "action", instruction.Action)
			return instruction.Action, nil

		case fleetproto.SteeringActionSteer:
			iteration++
			if iteration > maxIterations {
				p.logger.Warn("Max steering iterations reached, waiting for approve/reject", "max", maxIterations)
				continue
			}

			p.logger.Info("Steering iteration", "iteration", iteration, "prompt", instruction.Prompt)

			// Record steering
			result.SteeringHistory = append(result.SteeringHistory, fleetproto.SteeringRecord{
				Iteration: iteration,
				Prompt:    instruction.Prompt,
				Timestamp: time.Now().UTC(),
			})

			// Re-run transformation with steering prompt
			p.writeStatus(fleetproto.AgentStatus{
				Phase:     fleetproto.PhaseExecuting,
				Step:      "running_transformation",
				Message:   fmt.Sprintf("Steering iteration %d...", iteration),
				Iteration: iteration,
				UpdatedAt: time.Now().UTC(),
			})

			steeringOutput, err := p.runSteeringTransformation(ctx, manifest, instruction.Prompt, iteration, result.AgentOutput)
			if err != nil {
				p.logger.Error("Steering transformation failed", "error", err)
				// Continue to allow retry or approval of current state
			} else {
				result.AgentOutput = steeringOutput
			}

			// Re-run verifiers and re-collect
			verifierResults := p.runVerifiers(ctx, manifest)
			result.Repositories = p.collectResults(ctx, manifest, verifierResults)

			// Update result and status
			result.Status = fleetproto.PhaseAwaitingInput
			if err := p.writeResult(result); err != nil {
				p.logger.Warn("Failed to write steering result", "error", err)
			}
			p.writeStatus(fleetproto.AgentStatus{
				Phase:     fleetproto.PhaseAwaitingInput,
				Message:   fmt.Sprintf("Steering iteration %d complete, awaiting input", iteration),
				Iteration: iteration,
				UpdatedAt: time.Now().UTC(),
			})
		}
	}
}

// waitForManifest polls for the manifest file via Protocol.
func (p *Pipeline) waitForManifest(ctx context.Context) (*fleetproto.TaskManifest, error) {
	data, err := p.proto.WaitForManifest(ctx)
	if err != nil {
		return nil, err
	}
	var manifest fleetproto.TaskManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}
	return &manifest, nil
}

// waitForSteering polls for the steering file via Protocol.
// Protocol handles atomic rename to prevent TOCTOU races.
func (p *Pipeline) waitForSteering(ctx context.Context) (*fleetproto.SteeringInstruction, error) {
	return p.proto.WaitForSteering(ctx)
}

// writeStatus writes the status file atomically via Protocol.
func (p *Pipeline) writeStatus(status fleetproto.AgentStatus) {
	if err := p.proto.WriteStatus(status); err != nil {
		p.logger.Warn("Failed to write status", "error", err)
	}
}

// writeResult writes the result file atomically via Protocol.
func (p *Pipeline) writeResult(result *fleetproto.AgentResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	return p.proto.WriteResult(data)
}
