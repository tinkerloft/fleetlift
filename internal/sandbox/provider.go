// Package sandbox provides interfaces and types for container-based sandbox management.
package sandbox

import (
	"context"
	"io"
	"time"
)

// Provider manages sandbox container lifecycle.
type Provider interface {
	Provision(ctx context.Context, opts ProvisionOptions) (*Sandbox, error)
	Exec(ctx context.Context, id string, cmd ExecCommand) (*ExecResult, error)
	ExecShell(ctx context.Context, id string, command string, user string) (*ExecResult, error)
	CopyTo(ctx context.Context, id string, src io.Reader, destPath string) error
	CopyFrom(ctx context.Context, id string, srcPath string) (io.ReadCloser, error)
	Status(ctx context.Context, id string) (*SandboxStatus, error)
	Cleanup(ctx context.Context, id string) error
	Name() string
}

// AgentProvider extends Provider with file-based agent protocol operations.
// All payloads are raw bytes — callers marshal/unmarshal their own types.
type AgentProvider interface {
	Provider
	SubmitManifest(ctx context.Context, id string, manifest []byte) error
	PollStatus(ctx context.Context, id string) ([]byte, error)
	ReadResult(ctx context.Context, id string) ([]byte, error)
	SubmitSteering(ctx context.Context, id string, instruction []byte) error
}

// ProvisionOptions configures a new sandbox container.
type ProvisionOptions struct {
	TaskID       string
	Image        string
	Cmd          []string          // explicit command override; empty = use image CMD
	WorkingDir   string
	Env          map[string]string
	Resources    ResourceLimits
	Volumes      []VolumeMount
	Timeout      time.Duration
	BasePath     string // protocol base path; ignored by OpenSandbox provider
	UseAgentMode bool   // when true, image CMD runs the agent; when false, container is kept alive for exec

	// K8s-specific fields (ignored by OpenSandbox provider)
	RuntimeClass  string
	NodeSelector  map[string]string
	UserNamespace bool
}

// ResourceLimits defines compute resource constraints for a sandbox.
type ResourceLimits struct {
	MemoryBytes      int64
	CPUQuota         int64 // in units of 1/100000 CPU (e.g. 200000 = 2 CPUs)
	EphemeralStorage int64 // bytes
}

// VolumeMount defines a volume to mount into the sandbox.
type VolumeMount struct {
	Name      string
	HostPath  string
	ClaimName string
	MountPath string
	ReadOnly  bool
}

// Sandbox represents a provisioned container.
type Sandbox struct {
	ID         string
	Provider   string
	WorkingDir string
}

// SandboxPhase is the lifecycle state of a sandbox.
type SandboxPhase string

const (
	SandboxPhasePending   SandboxPhase = "pending"
	SandboxPhaseRunning   SandboxPhase = "running"
	SandboxPhaseSucceeded SandboxPhase = "succeeded"
	SandboxPhaseFailed    SandboxPhase = "failed"
	SandboxPhaseUnknown   SandboxPhase = "unknown"
)

// SandboxStatus is the current state of a sandbox.
type SandboxStatus struct {
	Phase   SandboxPhase
	Message string
}

// ExecCommand configures a command to execute in a sandbox.
type ExecCommand struct {
	Command    []string
	WorkingDir string
	Env        map[string]string
	User       string
	Timeout    time.Duration
}

// ExecResult holds the output of a command execution.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// IsSuccess returns true if the command exited with code 0.
func (r *ExecResult) IsSuccess() bool { return r.ExitCode == 0 }
