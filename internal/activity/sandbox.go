// Package activity contains Temporal activity implementations.
package activity

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"go.temporal.io/sdk/activity"

	agentboxsandbox "github.com/tinkerloft/agentbox/sandbox"
	agentboxtemporalkit "github.com/tinkerloft/agentbox/temporalkit"

	"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// shortContainerID safely truncates container ID for logging
func shortContainerID(id string) string {
	if id == "" {
		return "<empty>"
	}
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// SandboxActivities contains activities for managing sandbox containers.
type SandboxActivities struct {
	Provider agentboxsandbox.AgentProvider
	base     *agentboxtemporalkit.SandboxActivities
}

// NewSandboxActivities creates a new SandboxActivities instance.
func NewSandboxActivities(provider agentboxsandbox.AgentProvider) *SandboxActivities {
	return &SandboxActivities{
		Provider: provider,
		base:     &agentboxtemporalkit.SandboxActivities{Provider: provider},
	}
}

// getEnvOrDefault returns the environment variable value or a default.
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// parseMemory parses memory strings like "4g", "4G", "4096m", "4Gi" (BUG-002 fix)
func parseMemory(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 4 * 1024 * 1024 * 1024, nil // Default 4GB
	}

	var multiplier int64 = 1
	var numStr string

	switch {
	case strings.HasSuffix(s, "gi"):
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(s, "gi")
	case strings.HasSuffix(s, "g"):
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(s, "g")
	case strings.HasSuffix(s, "mi"):
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(s, "mi")
	case strings.HasSuffix(s, "m"):
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(s, "m")
	case strings.HasSuffix(s, "ki"):
		multiplier = 1024
		numStr = strings.TrimSuffix(s, "ki")
	case strings.HasSuffix(s, "k"):
		multiplier = 1024
		numStr = strings.TrimSuffix(s, "k")
	default:
		numStr = s
	}

	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 4 * 1024 * 1024 * 1024, fmt.Errorf("invalid memory value: %s", s)
	}

	return int64(val * float64(multiplier)), nil
}

// parseCPU parses CPU strings like "2", "2.5", "500m" (millicores) (BUG-002 fix)
func parseCPU(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 200000, nil // Default 2 CPUs
	}

	if strings.HasSuffix(s, "m") {
		// Millicores: 1000m = 1 CPU
		millis, err := strconv.ParseInt(strings.TrimSuffix(s, "m"), 10, 64)
		if err != nil {
			return 200000, fmt.Errorf("invalid cpu value: %s", s)
		}
		return millis * 100, nil // Convert to quota (100000 = 1 CPU)
	}

	cpus, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 200000, fmt.Errorf("invalid cpu value: %s", s)
	}
	return int64(cpus * 100000), nil
}

// ProvisionAgentSandboxInput contains options for agent-mode sandbox provisioning.
type ProvisionAgentSandboxInput struct {
	TaskID string `json:"task_id"`
	// Image optionally overrides the sandbox container image (M2 fix).
	// TRUST BOUNDARY: This value originates from the task manifest authored by
	// a trusted operator (not end-users). Production deployments that accept
	// manifests from less-trusted sources should validate images against an
	// allowlist in a policy layer above this activity.
	Image string `json:"image,omitempty"`
}

// ProvisionAgentSandbox creates a Docker container for the sidecar agent pattern.
// Unlike ProvisionSandbox, this sets UseAgentMode=true so the Dockerfile CMD runs (C2 fix),
// and accepts an optional image override (M2 fix).
func (a *SandboxActivities) ProvisionAgentSandbox(ctx context.Context, input ProvisionAgentSandboxInput) (*model.SandboxInfo, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Provisioning agent sandbox", "taskID", input.TaskID)

	sandboxImage := getEnvOrDefault("SANDBOX_IMAGE", "claude-code-sandbox:latest")
	if input.Image != "" {
		sandboxImage = input.Image
	}
	memoryLimit := getEnvOrDefault("SANDBOX_MEMORY_LIMIT", DefaultMemoryLimit)
	cpuLimit := getEnvOrDefault("SANDBOX_CPU_LIMIT", DefaultCPULimit)

	memLimitBytes, err := parseMemory(memoryLimit)
	if err != nil {
		logger.Warn("Failed to parse memory limit, using default", "value", memoryLimit, "error", err)
	}
	cpuQuota, err := parseCPU(cpuLimit)
	if err != nil {
		logger.Warn("Failed to parse CPU limit, using default", "value", cpuLimit, "error", err)
	}

	opts := agentboxsandbox.ProvisionOptions{
		TaskID:     input.TaskID,
		Image:      sandboxImage,
		Cmd:        []string{"/agent-bin/agent", "serve"},
		BasePath:   fleetproto.DefaultBasePath,
		WorkingDir: WorkspacePath,
		Env: map[string]string{
			"ANTHROPIC_API_KEY": os.Getenv("ANTHROPIC_API_KEY"),
			"GITHUB_TOKEN":      os.Getenv("GITHUB_TOKEN"),
			"TASK_ID":           input.TaskID,
		},
		Resources: agentboxsandbox.ResourceLimits{
			MemoryBytes: memLimitBytes,
			CPUQuota:    cpuQuota,
		},
	}

	sb, err := a.base.Provision(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to provision sandbox: %w", err)
	}

	logger.Info("Agent container created", "containerID", shortContainerID(sb.ID), "taskID", input.TaskID)

	return &model.SandboxInfo{
		ContainerID:   sb.ID,
		WorkspacePath: WorkspacePath,
	}, nil
}

// ProvisionSandbox creates a Docker container for Claude Code execution.
func (a *SandboxActivities) ProvisionSandbox(ctx context.Context, taskID string) (*model.SandboxInfo, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Provisioning sandbox", "taskID", taskID)

	sandboxImage := getEnvOrDefault("SANDBOX_IMAGE", "claude-code-sandbox:latest")
	memoryLimit := getEnvOrDefault("SANDBOX_MEMORY_LIMIT", DefaultMemoryLimit)
	cpuLimit := getEnvOrDefault("SANDBOX_CPU_LIMIT", DefaultCPULimit)

	// BUG-002: Use robust resource parsing
	memLimitBytes, err := parseMemory(memoryLimit)
	if err != nil {
		logger.Warn("Failed to parse memory limit, using default", "value", memoryLimit, "error", err)
	}

	cpuQuota, err := parseCPU(cpuLimit)
	if err != nil {
		logger.Warn("Failed to parse CPU limit, using default", "value", cpuLimit, "error", err)
	}

	opts := agentboxsandbox.ProvisionOptions{
		TaskID:     taskID,
		Image:      sandboxImage,
		WorkingDir: WorkspacePath,
		Env: map[string]string{
			"ANTHROPIC_API_KEY": os.Getenv("ANTHROPIC_API_KEY"),
			"GITHUB_TOKEN":      os.Getenv("GITHUB_TOKEN"),
			"TASK_ID":           taskID,
		},
		Resources: agentboxsandbox.ResourceLimits{
			MemoryBytes: memLimitBytes,
			CPUQuota:    cpuQuota,
		},
	}

	sb, err := a.base.Provision(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to provision sandbox: %w", err)
	}

	logger.Info("Container created", "containerID", shortContainerID(sb.ID), "taskID", taskID)

	return &model.SandboxInfo{
		ContainerID:   sb.ID,
		WorkspacePath: WorkspacePath,
	}, nil
}

// CleanupSandbox stops and removes the sandbox container.
// If DEBUG_NO_CLEANUP environment variable is set to "true", cleanup is skipped
// to allow inspection of failed containers.
func (a *SandboxActivities) CleanupSandbox(ctx context.Context, containerID string) error {
	logger := activity.GetLogger(ctx)

	// Check if cleanup should be skipped (debug mode)
	if os.Getenv("DEBUG_NO_CLEANUP") == "true" {
		logger.Info("Skipping container cleanup (DEBUG_NO_CLEANUP=true)", "containerID", shortContainerID(containerID))
		return nil
	}

	logger.Info("Cleaning up container", "containerID", shortContainerID(containerID))

	if err := a.base.Cleanup(ctx, containerID); err != nil {
		logger.Error("Error cleaning up container", "error", err)
		return err
	}

	logger.Info("Container removed", "containerID", shortContainerID(containerID))
	return nil
}

// RunVerifiers executes verification commands in each repository and returns results.
// RunVerifiersInput contains inputs for running verifiers.
type RunVerifiersInput struct {
	SandboxInfo             model.SandboxInfo
	Repos                   []model.Repository
	Verifiers               []model.Verifier
	UseTransformationLayout bool // If true, repos are at /workspace/targets/{name}
}

// RunVerifiers executes verification commands in each repository and returns results.
func (a *SandboxActivities) RunVerifiers(ctx context.Context, input RunVerifiersInput) (*model.VerifiersResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Running verifiers", "count", len(input.Verifiers), "repos", len(input.Repos))

	if len(input.Verifiers) == 0 {
		return &model.VerifiersResult{AllPassed: true, Results: []model.VerifierResult{}}, nil
	}

	// Determine base path
	basePath := "/workspace"
	if input.UseTransformationLayout {
		basePath = "/workspace/targets"
	}

	var results []model.VerifierResult
	allPassed := true

	for _, repo := range input.Repos {
		repoPath := fmt.Sprintf("%s/%s", basePath, repo.Name)

		for _, verifier := range input.Verifiers {
			logger.Info("Running verifier", "name", verifier.Name, "repo", repo.Name)

			// Build command string from verifier.Command slice
			cmdStr := strings.Join(verifier.Command, " ")
			fullCmd := fmt.Sprintf("cd %s && %s", repoPath, cmdStr)

			result, err := a.Provider.ExecShell(ctx, input.SandboxInfo.ContainerID, fullCmd, AgentUser)

			var vResult model.VerifierResult
			vResult.Name = fmt.Sprintf("%s:%s", repo.Name, verifier.Name)

			if err != nil {
				vResult.Success = false
				vResult.ExitCode = -1
				vResult.Error = err.Error()
				allPassed = false
			} else {
				vResult.ExitCode = result.ExitCode
				vResult.Success = result.ExitCode == 0
				vResult.Output = result.Stdout
				if result.Stderr != "" {
					vResult.Output += "\n" + result.Stderr
				}
				if !vResult.Success {
					vResult.Error = fmt.Sprintf("exit code %d", result.ExitCode)
					allPassed = false
				}
			}

			results = append(results, vResult)
			logger.Info("Verifier completed", "name", vResult.Name, "success", vResult.Success)

			activity.RecordHeartbeat(ctx, fmt.Sprintf("Verifier %s: %v", vResult.Name, vResult.Success))
		}
	}

	return &model.VerifiersResult{
		AllPassed: allPassed,
		Results:   results,
	}, nil
}
