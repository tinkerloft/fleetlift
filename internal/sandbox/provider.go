// Package sandbox provides container sandbox abstractions.
package sandbox

import (
	"context"
	"io"
	"time"
)

// Provider defines the interface for sandbox container providers.
// Implementations include Docker (local development) and Kubernetes (production).
type Provider interface {
	// Provision creates a new sandbox container.
	Provision(ctx context.Context, opts ProvisionOptions) (*Sandbox, error)

	// Exec executes a command in a sandbox container.
	Exec(ctx context.Context, id string, cmd ExecCommand) (*ExecResult, error)

	// ExecShell executes a shell command string in a sandbox container.
	// This is a convenience method that wraps the command in bash -c.
	ExecShell(ctx context.Context, id string, command string, user string) (*ExecResult, error)

	// CopyTo copies data into the sandbox.
	CopyTo(ctx context.Context, id string, src io.Reader, destPath string) error

	// CopyFrom copies a file from a sandbox container.
	CopyFrom(ctx context.Context, id string, srcPath string) (io.ReadCloser, error)

	// Status returns the current sandbox status.
	Status(ctx context.Context, id string) (*SandboxStatus, error)

	// Cleanup stops and removes a sandbox container.
	Cleanup(ctx context.Context, id string) error

	// Name returns the provider name (e.g., "docker", "kubernetes").
	Name() string
}

// ProvisionOptions contains options for provisioning a sandbox.
type ProvisionOptions struct {
	TaskID     string
	Image      string
	WorkingDir string
	Env        map[string]string
	Resources  ResourceLimits
	Timeout    time.Duration
}

// ResourceLimits defines resource constraints for a sandbox.
type ResourceLimits struct {
	MemoryBytes int64
	CPUQuota    int64 // In units of 1/100000 of a CPU (e.g., 200000 = 2 CPUs)
}

// Sandbox represents a provisioned sandbox container.
type Sandbox struct {
	ID         string
	Provider   string
	WorkingDir string
}

// SandboxPhase represents the current state of a sandbox.
type SandboxPhase string

const (
	SandboxPhasePending   SandboxPhase = "pending"
	SandboxPhaseRunning   SandboxPhase = "running"
	SandboxPhaseSucceeded SandboxPhase = "succeeded"
	SandboxPhaseFailed    SandboxPhase = "failed"
	SandboxPhaseUnknown   SandboxPhase = "unknown"
)

// SandboxStatus contains the current status of a sandbox.
type SandboxStatus struct {
	Phase   SandboxPhase
	Message string
}

// ExecCommand contains the command to execute in a sandbox.
type ExecCommand struct {
	Command    []string
	WorkingDir string
	Env        map[string]string
	User       string
	Timeout    time.Duration
}

// ExecResult contains the result of executing a command.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// IsSuccess returns true if the command exited with code 0.
func (r *ExecResult) IsSuccess() bool {
	return r.ExitCode == 0
}

// CombinedOutput returns stdout and stderr combined.
func (r *ExecResult) CombinedOutput() string {
	if r.Stderr == "" {
		return r.Stdout
	}
	if r.Stdout == "" {
		return r.Stderr
	}
	return r.Stdout + "\n" + r.Stderr
}
