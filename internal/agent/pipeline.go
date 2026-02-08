// Package agent implements the sandbox sidecar agent that executes task manifests autonomously.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
)

// Pipeline executes the full task lifecycle from manifest to completion.
type Pipeline struct {
	basePath string
	fs       FileSystem
	exec     CommandExecutor
	logger   *slog.Logger
}

// NewPipeline creates a new agent pipeline with explicit dependencies.
func NewPipeline(basePath string, fs FileSystem, exec CommandExecutor, logger *slog.Logger) *Pipeline {
	return &Pipeline{basePath: basePath, fs: fs, exec: exec, logger: logger}
}

// NewDefaultPipeline creates a pipeline with real OS implementations.
func NewDefaultPipeline(basePath string) *Pipeline {
	return NewPipeline(basePath, osFileSystem{}, osCommandExecutor{}, slog.New(slog.NewJSONHandler(os.Stderr, nil)))
}

// Run watches for a manifest and executes the full pipeline.
func (p *Pipeline) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle SIGTERM/SIGINT
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			p.logger.Info("Received shutdown signal")
			cancel()
		case <-ctx.Done():
		}
	}()

	p.logger.Info("Agent starting, waiting for manifest...")
	p.writeStatus(protocol.AgentStatus{
		Phase:     protocol.PhaseInitializing,
		Message:   "Waiting for manifest",
		UpdatedAt: time.Now().UTC(),
	})

	// Wait for manifest
	manifest, err := p.waitForManifest(ctx)
	if err != nil {
		return fmt.Errorf("waiting for manifest: %w", err)
	}

	p.logger.Info("Manifest received", "taskID", manifest.TaskID, "mode", manifest.Mode)

	// Validate manifest
	if err := ValidateManifest(manifest); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}

	// Set timeout from manifest
	if manifest.TimeoutSeconds > 0 {
		var timeoutCancel context.CancelFunc
		ctx, timeoutCancel = context.WithTimeout(ctx, time.Duration(manifest.TimeoutSeconds)*time.Second)
		defer timeoutCancel()
	}

	return p.execute(ctx, manifest)
}

func (p *Pipeline) execute(ctx context.Context, manifest *protocol.TaskManifest) error {
	startedAt := time.Now().UTC()
	result := &protocol.AgentResult{
		Status:    protocol.PhaseExecuting,
		StartedAt: startedAt,
	}

	// Helper to fail with error
	fail := func(errMsg string) error {
		p.logger.Error("Pipeline failed", "error", errMsg)
		now := time.Now().UTC()
		result.Status = protocol.PhaseFailed
		result.Error = &errMsg
		result.CompletedAt = &now
		if err := p.writeResult(result); err != nil {
			p.logger.Warn("Failed to write failure result", "error", err)
		}
		p.writeStatus(protocol.AgentStatus{
			Phase:     protocol.PhaseFailed,
			Message:   errMsg,
			UpdatedAt: now,
		})
		return fmt.Errorf("%s", errMsg)
	}

	// 1. Clone repositories
	p.writeStatus(protocol.AgentStatus{
		Phase:     protocol.PhaseExecuting,
		Step:      "cloning",
		Message:   "Cloning repositories...",
		UpdatedAt: time.Now().UTC(),
	})

	if err := p.cloneRepos(ctx, manifest); err != nil {
		return fail(fmt.Sprintf("clone failed: %v", err))
	}

	// 2. Execute transformation
	p.writeStatus(protocol.AgentStatus{
		Phase:     protocol.PhaseExecuting,
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
		p.writeStatus(protocol.AgentStatus{
			Phase:     protocol.PhaseVerifying,
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
		result.Status = protocol.PhaseAwaitingInput
		if err := p.writeResult(result); err != nil {
			p.logger.Warn("Failed to write awaiting result", "error", err)
		}
		p.writeStatus(protocol.AgentStatus{
			Phase:     protocol.PhaseAwaitingInput,
			Message:   "Awaiting human input",
			UpdatedAt: time.Now().UTC(),
		})

		finalAction, err := p.steeringLoop(ctx, manifest, result)
		if err != nil {
			return fail(fmt.Sprintf("steering loop failed: %v", err))
		}

		if finalAction == protocol.SteeringActionCancel || finalAction == protocol.SteeringActionReject {
			now := time.Now().UTC()
			result.Status = protocol.PhaseCancelled
			result.CompletedAt = &now
			if err := p.writeResult(result); err != nil {
				p.logger.Warn("Failed to write cancelled result", "error", err)
			}
			p.writeStatus(protocol.AgentStatus{
				Phase:     protocol.PhaseCancelled,
				Message:   "Cancelled by user",
				UpdatedAt: now,
			})
			return nil
		}
	}

	// 6. Create PRs (transform mode only)
	if manifest.Mode == "transform" && manifest.PullRequest != nil {
		p.writeStatus(protocol.AgentStatus{
			Phase:     protocol.PhaseCreatingPRs,
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
	result.Status = protocol.PhaseComplete
	result.CompletedAt = &now
	if err := p.writeResult(result); err != nil {
		p.logger.Warn("Failed to write final result", "error", err)
	}
	p.writeStatus(protocol.AgentStatus{
		Phase:     protocol.PhaseComplete,
		Message:   "Task completed",
		UpdatedAt: now,
	})

	p.logger.Info("Pipeline completed successfully")
	return nil
}

// steeringLoop handles the HITL steering cycle.
func (p *Pipeline) steeringLoop(ctx context.Context, manifest *protocol.TaskManifest, result *protocol.AgentResult) (protocol.SteeringAction, error) {
	maxIterations := manifest.MaxSteeringIterations
	if maxIterations <= 0 {
		maxIterations = DefaultMaxSteering
	}

	iteration := 0
	for {
		// Check for context cancellation (e.g., timeout)
		select {
		case <-ctx.Done():
			return protocol.SteeringAction(""), fmt.Errorf("steering loop timed out: %w", ctx.Err())
		default:
		}

		instruction, err := p.waitForSteering(ctx)
		if err != nil {
			return protocol.SteeringAction(""), err
		}

		switch instruction.Action {
		case protocol.SteeringActionApprove:
			p.logger.Info("Received approve")
			return protocol.SteeringActionApprove, nil

		case protocol.SteeringActionReject, protocol.SteeringActionCancel:
			p.logger.Info("Received action", "action", instruction.Action)
			return instruction.Action, nil

		case protocol.SteeringActionSteer:
			iteration++
			if iteration > maxIterations {
				p.logger.Warn("Max steering iterations reached, waiting for approve/reject", "max", maxIterations)
				continue
			}

			p.logger.Info("Steering iteration", "iteration", iteration, "prompt", instruction.Prompt)

			// Record steering
			result.SteeringHistory = append(result.SteeringHistory, protocol.SteeringRecord{
				Iteration: iteration,
				Prompt:    instruction.Prompt,
				Timestamp: time.Now().UTC(),
			})

			// Re-run transformation with steering prompt
			p.writeStatus(protocol.AgentStatus{
				Phase:     protocol.PhaseExecuting,
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
			result.Status = protocol.PhaseAwaitingInput
			if err := p.writeResult(result); err != nil {
				p.logger.Warn("Failed to write steering result", "error", err)
			}
			p.writeStatus(protocol.AgentStatus{
				Phase:     protocol.PhaseAwaitingInput,
				Message:   fmt.Sprintf("Steering iteration %d complete, awaiting input", iteration),
				Iteration: iteration,
				UpdatedAt: time.Now().UTC(),
			})
		}
	}
}

// waitForManifest polls for the manifest file.
func (p *Pipeline) waitForManifest(ctx context.Context) (*protocol.TaskManifest, error) {
	manifestPath := filepath.Join(p.basePath, "manifest.json")

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		data, err := p.fs.ReadFile(manifestPath)
		if err == nil {
			var manifest protocol.TaskManifest
			if err := json.Unmarshal(data, &manifest); err != nil {
				return nil, fmt.Errorf("invalid manifest: %w", err)
			}
			return &manifest, nil
		}

		time.Sleep(ManifestPollInterval)
	}
}

// waitForSteering polls for the steering file using atomic rename to prevent TOCTOU races.
func (p *Pipeline) waitForSteering(ctx context.Context) (*protocol.SteeringInstruction, error) {
	steeringPath := filepath.Join(p.basePath, "steering.json")
	processingPath := steeringPath + ".processing"

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Atomic claim via rename (H4 fix)
		if err := p.fs.Rename(steeringPath, processingPath); err != nil {
			// File doesn't exist yet — keep polling
			time.Sleep(SteeringPollInterval)
			continue
		}

		data, err := p.fs.ReadFile(processingPath)
		_ = p.fs.Remove(processingPath)
		if err != nil {
			p.logger.Warn("Failed to read steering file after rename", "error", err)
			continue
		}

		var instruction protocol.SteeringInstruction
		if err := json.Unmarshal(data, &instruction); err != nil {
			p.logger.Warn("Invalid steering.json, ignoring", "error", err)
			continue
		}
		return &instruction, nil
	}
}

// writeStatus writes the status file atomically to prevent partial reads.
func (p *Pipeline) writeStatus(status protocol.AgentStatus) {
	data, err := json.Marshal(status)
	if err != nil {
		p.logger.Error("Failed to marshal status", "error", err)
		return
	}
	// Atomic write via rename to prevent partial reads
	tmpPath := filepath.Join(p.basePath, "status.json.tmp")
	if err := p.fs.WriteFile(tmpPath, data, 0644); err != nil {
		p.logger.Warn("Failed to write status tmp", "error", err)
		return
	}
	if err := p.fs.Rename(tmpPath, filepath.Join(p.basePath, "status.json")); err != nil {
		p.logger.Warn("Failed to rename status", "error", err)
	}
}

// writeResult writes the result file atomically. Returns error because result is critical.
func (p *Pipeline) writeResult(result *protocol.AgentResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	// Atomic write via rename to prevent partial reads
	tmpPath := filepath.Join(p.basePath, "result.json.tmp")
	if err := p.fs.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write result tmp: %w", err)
	}
	if err := p.fs.Rename(tmpPath, filepath.Join(p.basePath, "result.json")); err != nil {
		return fmt.Errorf("rename result: %w", err)
	}
	return nil
}
