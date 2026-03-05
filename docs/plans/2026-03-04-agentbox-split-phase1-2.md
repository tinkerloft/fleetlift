# Agentbox Split — Phase 1 & 2: Create agentbox Repo

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

> **Status (2026-03-05):** ALL TASKS COMPLETE. agentbox module published with protocol, sandbox, agent, temporalkit packages. All tests pass, lint clean.

**Goal:** Create the `github.com/tinkerloft/agentbox` Go module with `protocol`, `sandbox` (docker+k8s), `agent`, and `temporalkit` packages ported from fleetlift with generalized interfaces.

**Architecture:** New repo at `/Users/andrew/dev/code/projects/agentbox`. `protocol` contains generic file-IPC types (no fleetlift domain types). `sandbox` ports Docker+K8s providers with `[]byte` PollStatus and `Cmd []string` replacing `UseAgentMode bool`. `agent` exposes a `Protocol` struct with WaitForManifest/WriteStatus/WriteResult/WaitForSteering primitives. `temporalkit` wraps Temporal activities with `[]byte` interfaces.

**Tech Stack:** Go 1.25.0, `github.com/docker/docker v25.0.6`, `k8s.io/client-go v0.35.1`, `go.temporal.io/sdk v1.27.0`, `github.com/stretchr/testify v1.11.1`

**Working directory:** `/Users/andrew/dev/code/projects/agentbox`

---

## Task 1: Initialize go.mod and repo structure ✅ Complete

**Files:**
- Create: `go.mod`
- Create: `Makefile`

**Step 1: Initialize the module**

```bash
cd /Users/andrew/dev/code/projects/agentbox
go mod init github.com/tinkerloft/agentbox
```

**Step 2: Add required dependencies**

```bash
go get github.com/docker/docker@v25.0.6+incompatible
go get github.com/docker/docker/api/types@v25.0.6+incompatible
go get github.com/stretchr/testify@v1.11.1
go get go.temporal.io/sdk@v1.27.0
go get k8s.io/api@v0.35.1
go get k8s.io/apimachinery@v0.35.1
go get k8s.io/client-go@v0.35.1
go mod tidy
```

**Step 3: Create Makefile**

```makefile
.PHONY: test lint build

build:
	go build ./...

test:
	go test ./...

lint:
	golangci-lint run --timeout=5m
```

**Step 4: Verify**

```bash
go build ./...
```

Expected: no output (empty module, no packages yet)

**Step 5: Commit**

```bash
git add go.mod go.sum Makefile
git commit -m "chore: initialize agentbox module"
```

---

## Task 2: `protocol` package ✅ Complete

**Files:**
- Create: `protocol/protocol.go`
- Create: `protocol/protocol_test.go`

**Step 1: Write the failing test**

Create `protocol/protocol_test.go`:

```go
package protocol_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/agentbox/protocol"
)

func TestPathFunctions(t *testing.T) {
	base := "/workspace/.agentbox"
	assert.Equal(t, base+"/manifest.json", protocol.ManifestPath(base))
	assert.Equal(t, base+"/status.json", protocol.StatusPath(base))
	assert.Equal(t, base+"/result.json", protocol.ResultPath(base))
	assert.Equal(t, base+"/steering.json", protocol.SteeringPath(base))
}

func TestDefaultBasePath(t *testing.T) {
	assert.Equal(t, "/workspace/.agentbox", protocol.DefaultBasePath)
}

func TestPhaseConstants_Unique(t *testing.T) {
	phases := []protocol.Phase{
		protocol.PhaseInitializing, protocol.PhaseExecuting, protocol.PhaseVerifying,
		protocol.PhaseAwaitingInput, protocol.PhaseComplete, protocol.PhaseFailed,
		protocol.PhaseCancelled,
	}
	seen := make(map[protocol.Phase]bool)
	for _, p := range phases {
		assert.False(t, seen[p], "duplicate phase: %s", p)
		seen[p] = true
	}
	// PhaseCreatingPRs must NOT exist in agentbox (it's fleetlift-specific)
}

func TestSteeringActionConstants_Unique(t *testing.T) {
	actions := []protocol.SteeringAction{
		protocol.SteeringActionApprove, protocol.SteeringActionReject,
		protocol.SteeringActionCancel, protocol.SteeringActionSteer,
	}
	seen := make(map[protocol.SteeringAction]bool)
	for _, a := range actions {
		assert.False(t, seen[a], "duplicate action: %s", a)
		seen[a] = true
	}
}

func TestAgentStatus_JSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	status := protocol.AgentStatus{
		Phase:     protocol.PhaseExecuting,
		Step:      "running_agent",
		Message:   "Running...",
		Iteration: 1,
		Metadata:  map[string]string{"model": "claude-sonnet-4-6", "input_tokens": "1234"},
		Progress:  &protocol.StatusProgress{Current: 1, Total: 3},
		UpdatedAt: now,
	}

	data, err := json.Marshal(status)
	require.NoError(t, err)

	var decoded protocol.AgentStatus
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, protocol.PhaseExecuting, decoded.Phase)
	assert.Equal(t, "running_agent", decoded.Step)
	assert.Equal(t, 1, decoded.Progress.Current)
	assert.Equal(t, 3, decoded.Progress.Total)
	assert.Equal(t, "claude-sonnet-4-6", decoded.Metadata["model"])
	assert.Equal(t, now, decoded.UpdatedAt)
}

func TestSteeringInstruction_NoTimestamp(t *testing.T) {
	// SteeringInstruction in agentbox does NOT have a Timestamp field
	instruction := protocol.SteeringInstruction{
		Action:    protocol.SteeringActionSteer,
		Prompt:    "try harder",
		Iteration: 2,
	}
	data, err := json.Marshal(instruction)
	require.NoError(t, err)

	var decoded protocol.SteeringInstruction
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, protocol.SteeringActionSteer, decoded.Action)
	assert.Equal(t, "try harder", decoded.Prompt)
	assert.Equal(t, 2, decoded.Iteration)
}
```

**Step 2: Run test — expect failure**

```bash
cd /Users/andrew/dev/code/projects/agentbox
go test ./protocol/...
```

Expected: `cannot find package "github.com/tinkerloft/agentbox/protocol"`

**Step 3: Implement `protocol/protocol.go`**

```go
// Package protocol defines the file-based IPC protocol between an agentbox
// agent (running inside a sandbox container) and the orchestrator.
//
// All communication uses JSON files at a configurable base path
// (default: /workspace/.agentbox):
//
//	manifest.json  - Orchestrator → Agent: task definition (written once)
//	status.json    - Agent → Orchestrator: lightweight phase indicator (polled)
//	result.json    - Agent → Orchestrator: full structured result
//	steering.json  - Orchestrator → Agent: HITL instruction (deleted after processing)
package protocol

import (
	"path/filepath"
	"time"
)

// DefaultBasePath is the default directory for protocol files inside the sandbox.
const DefaultBasePath = "/workspace/.agentbox"

// Protocol file names.
const (
	ManifestFilename = "manifest.json"
	StatusFilename   = "status.json"
	ResultFilename   = "result.json"
	SteeringFilename = "steering.json"
)

// ManifestPath returns the path to manifest.json under base.
func ManifestPath(base string) string { return filepath.Join(base, ManifestFilename) }

// StatusPath returns the path to status.json under base.
func StatusPath(base string) string { return filepath.Join(base, StatusFilename) }

// ResultPath returns the path to result.json under base.
func ResultPath(base string) string { return filepath.Join(base, ResultFilename) }

// SteeringPath returns the path to steering.json under base.
func SteeringPath(base string) string { return filepath.Join(base, SteeringFilename) }

// Phase is a typed string for agent lifecycle phases.
type Phase string

// Generic lifecycle phases. Consumers may define additional domain-specific phases.
const (
	PhaseInitializing  Phase = "initializing"
	PhaseExecuting     Phase = "executing"
	PhaseVerifying     Phase = "verifying"
	PhaseAwaitingInput Phase = "awaiting_input" // HITL pause point
	PhaseComplete      Phase = "complete"
	PhaseFailed        Phase = "failed"
	PhaseCancelled     Phase = "cancelled"
)

// AgentStatus is a lightweight status indicator written by the agent.
// The orchestrator polls this file to track progress.
type AgentStatus struct {
	Phase     Phase             `json:"phase"`
	Step      string            `json:"step,omitempty"`
	Message   string            `json:"message,omitempty"`
	Progress  *StatusProgress   `json:"progress,omitempty"`
	Iteration int               `json:"iteration"`
	// Metadata is opaque consumer-written data. Well-known keys (by convention):
	//   "input_tokens", "output_tokens", "cache_read_tokens", "model", "cost_usd", "agent_cli"
	Metadata  map[string]string `json:"metadata,omitempty"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// StatusProgress tracks generic progress within a phase.
type StatusProgress struct {
	Current int `json:"current"`
	Total   int `json:"total"`
}

// SteeringAction is a typed string for HITL steering instructions.
type SteeringAction string

// SteeringAction constants for the four supported actions.
const (
	SteeringActionApprove SteeringAction = "approve"
	SteeringActionReject  SteeringAction = "reject"
	SteeringActionCancel  SteeringAction = "cancel"
	SteeringActionSteer   SteeringAction = "steer"
)

// SteeringInstruction is written by the orchestrator to direct the agent.
// The agent polls for this file and claims it atomically via rename.
type SteeringInstruction struct {
	Action    SteeringAction `json:"action"`
	Prompt    string         `json:"prompt,omitempty"`
	Iteration int            `json:"iteration"`
}
```

**Step 4: Run tests — expect pass**

```bash
go test ./protocol/... -v
```

Expected: all tests PASS

**Step 5: Commit**

```bash
git add protocol/
git commit -m "feat: add protocol package with generic file-IPC types"
```

---

## Task 3: `sandbox` — interfaces, types, and factory ✅ Complete

**Files:**
- Create: `sandbox/provider.go`
- Create: `sandbox/factory.go`
- Create: `sandbox/factory_test.go`

**Step 1: Write the failing test**

Create `sandbox/factory_test.go`:

```go
package sandbox_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tinkerloft/agentbox/sandbox"
)

func TestNewProvider_Unknown(t *testing.T) {
	_, err := sandbox.NewProvider("unknown-provider", sandbox.ProviderConfig{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown sandbox provider")
}

func TestNewProvider_EmptyDefaultsToDocker(t *testing.T) {
	// Register a fake docker provider for this test
	sandbox.RegisterProvider("docker", func(_ sandbox.ProviderConfig) (sandbox.AgentProvider, error) {
		return nil, nil // just checking registration works
	})
	p, err := sandbox.NewProvider("", sandbox.ProviderConfig{})
	assert.NoError(t, err)
	assert.Nil(t, p) // our fake returns nil
}

func TestExecResult_IsSuccess(t *testing.T) {
	assert.True(t, (&sandbox.ExecResult{ExitCode: 0}).IsSuccess())
	assert.False(t, (&sandbox.ExecResult{ExitCode: 1}).IsSuccess())
	assert.False(t, (&sandbox.ExecResult{ExitCode: 127}).IsSuccess())
}
```

**Step 2: Run test — expect failure**

```bash
go test ./sandbox/...
```

Expected: `cannot find package "github.com/tinkerloft/agentbox/sandbox"`

**Step 3a: Implement `sandbox/provider.go`**

```go
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
	TaskID     string
	Image      string
	Cmd        []string // explicit command override; empty = use image CMD
	WorkingDir string
	Env        map[string]string
	Resources  ResourceLimits
	Volumes    []VolumeMount
	Timeout    time.Duration
	BasePath   string // protocol base path; default: protocol.DefaultBasePath

	// K8s-specific fields (ignored by Docker provider)
	RuntimeClass  string            // e.g. "gvisor", "kata"
	NodeSelector  map[string]string // for dedicated sandbox node pools
	Tolerations   []Toleration      // for dedicated sandbox node pools
	UserNamespace bool              // pod.spec.hostUsers=false (K8s 1.25+)
	OwnerRef      *OwnerReference   // for K8s GC via ownerReferences
}

// ResourceLimits defines compute resource constraints for a sandbox.
type ResourceLimits struct {
	MemoryBytes      int64
	CPUQuota         int64 // in units of 1/100000 CPU (e.g. 200000 = 2 CPUs)
	EphemeralStorage int64 // bytes; used for K8s ephemeral-storage limit
}

// VolumeMount defines a volume to mount into the sandbox.
type VolumeMount struct {
	Name      string
	HostPath  string // Docker: host path
	ClaimName string // K8s: PVC name
	MountPath string
	ReadOnly  bool
}

// Toleration is a K8s pod toleration.
type Toleration struct {
	Key      string
	Operator string // "Exists" or "Equal"
	Value    string
	Effect   string // "NoSchedule", "PreferNoSchedule", "NoExecute"
}

// OwnerReference is a K8s owner reference for GC.
type OwnerReference struct {
	APIVersion string
	Kind       string
	Name       string
	UID        string
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
```

**Step 3b: Implement `sandbox/factory.go`**

```go
package sandbox

import "fmt"

// ProviderFactory creates a new AgentProvider from config.
type ProviderFactory func(cfg ProviderConfig) (AgentProvider, error)

// ProviderConfig holds provider-level configuration.
type ProviderConfig struct {
	Namespace      string
	AgentImage     string
	KubeconfigPath string
}

var providerFactories = map[string]ProviderFactory{}

// RegisterProvider registers a provider factory under the given name.
// Typically called from an init() function in the provider package.
func RegisterProvider(name string, factory ProviderFactory) {
	providerFactories[name] = factory
}

// NewProvider creates a provider by name. Empty name defaults to "docker".
func NewProvider(providerName string, cfg ProviderConfig) (AgentProvider, error) {
	if providerName == "" {
		providerName = "docker"
	}
	factory, ok := providerFactories[providerName]
	if !ok {
		return nil, fmt.Errorf("unknown sandbox provider %q (registered: %v)", providerName, registeredNames())
	}
	return factory(cfg)
}

func registeredNames() []string {
	names := make([]string, 0, len(providerFactories))
	for name := range providerFactories {
		names = append(names, name)
	}
	return names
}
```

**Step 4: Run tests — expect pass**

```bash
go test ./sandbox/... -v
```

Expected: all tests PASS

**Step 5: Commit**

```bash
git add sandbox/
git commit -m "feat: add sandbox package with Provider/AgentProvider interfaces and factory"
```

---

## Task 4: `sandbox/docker` package ✅ Complete

**Files:**
- Create: `sandbox/docker/provider.go`
- Create: `sandbox/docker/register.go`
- Create: `sandbox/docker/provider_test.go`

**Step 1: Write the failing test**

Create `sandbox/docker/provider_test.go`:

```go
package docker

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/agentbox/sandbox"
)

func TestNewProvider(t *testing.T) {
	provider, err := NewProvider()
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}
	require.NotNil(t, provider)

	_, err = provider.GetClient().Ping(context.Background())
	assert.NoError(t, err)
}

func TestExecResult_IsSuccess(t *testing.T) {
	result := &sandbox.ExecResult{ExitCode: 0, Stdout: "hello", Stderr: ""}
	assert.True(t, result.IsSuccess())
}

func TestProvision_AgentCmd(t *testing.T) {
	// Tests that Cmd override is used when provided
	// (Integration test — skipped without Docker)
	provider, err := NewProvider()
	if err != nil {
		t.Skip("Docker not available")
	}
	ctx := context.Background()
	_ = provider.PullImageIfNeeded(ctx, "alpine:latest")

	sb, err := provider.Provision(ctx, sandbox.ProvisionOptions{
		TaskID: "test-cmd",
		Image:  "alpine:latest",
		Cmd:    []string{"sleep", "30"},
	})
	if err != nil {
		t.Skip("Cannot provision container")
	}
	defer func() { _ = provider.Cleanup(ctx, sb.ID) }()

	running, err := provider.IsContainerRunning(ctx, sb.ID)
	require.NoError(t, err)
	assert.True(t, running)
}

func TestProvision_NoCmd_UsesImageDefault(t *testing.T) {
	provider, err := NewProvider()
	if err != nil {
		t.Skip("Docker not available")
	}
	ctx := context.Background()
	_ = provider.PullImageIfNeeded(ctx, "alpine:latest")

	// No Cmd — alpine has no default CMD that keeps it alive, so we expect it exits
	// Just verify Provision returns an ID without error initially
	sb, err := provider.Provision(ctx, sandbox.ProvisionOptions{
		TaskID: "test-no-cmd",
		Image:  "alpine:latest",
	})
	if err != nil {
		t.Skip("Cannot provision container")
	}
	defer func() { _ = provider.Cleanup(ctx, sb.ID) }()
	assert.NotEmpty(t, sb.ID)
}

func TestExec(t *testing.T) {
	provider, err := NewProvider()
	if err != nil {
		t.Skip("Docker not available")
	}
	ctx := context.Background()
	_ = provider.PullImageIfNeeded(ctx, "alpine:latest")

	client := provider.GetClient()
	resp, err := client.ContainerCreate(ctx,
		&container.Config{Image: "alpine:latest", Cmd: []string{"sleep", "30"}},
		&container.HostConfig{}, nil, nil, "agentbox-test-exec",
	)
	require.NoError(t, err)
	require.NoError(t, client.ContainerStart(ctx, resp.ID, container.StartOptions{}))
	defer func() { _ = provider.Cleanup(ctx, resp.ID) }()

	result, err := provider.Exec(ctx, resp.ID, sandbox.ExecCommand{Command: []string{"echo", "hello"}})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "hello")
}

func TestIsContainerRunning_NotFound(t *testing.T) {
	provider, err := NewProvider()
	if err != nil {
		t.Skip("Docker not available")
	}
	running, err := provider.IsContainerRunning(context.Background(), "nonexistent-agentbox-id")
	require.NoError(t, err)
	assert.False(t, running)
}

func TestCleanup_NotFound(t *testing.T) {
	provider, err := NewProvider()
	if err != nil {
		t.Skip("Docker not available")
	}
	err = provider.Cleanup(context.Background(), "nonexistent-agentbox-id")
	assert.NoError(t, err)
}
```

**Step 2: Run test — expect failure**

```bash
go test ./sandbox/docker/...
```

Expected: `cannot find package "github.com/tinkerloft/agentbox/sandbox/docker"`

**Step 3a: Implement `sandbox/docker/provider.go`**

```go
// Package docker provides a Docker-based sandbox provider.
package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/tinkerloft/agentbox/protocol"
	"github.com/tinkerloft/agentbox/sandbox"
)

// MaxFileReadSize is the maximum bytes to read from sandbox files.
const MaxFileReadSize = 10 << 20 // 10 MB

var validNetworkName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]+$`)

var _ sandbox.AgentProvider = (*Provider)(nil)

// Provider implements sandbox.AgentProvider using Docker containers.
type Provider struct {
	client        *client.Client
	basePathCache sync.Map // containerID -> basePath string
}

// NewProvider creates a new Docker sandbox provider.
func NewProvider() (*Provider, error) {
	var opts []client.Opt
	if dockerHost := os.Getenv("DOCKER_HOST"); dockerHost != "" {
		opts = append(opts, client.FromEnv, client.WithAPIVersionNegotiation())
	} else {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			macOSSocket := filepath.Join(homeDir, ".docker", "run", "docker.sock")
			if _, err := os.Stat(macOSSocket); err == nil {
				opts = append(opts, client.WithHost("unix://"+macOSSocket))
			}
		}
		opts = append(opts, client.WithAPIVersionNegotiation())
	}
	if len(opts) == 1 {
		opts = append([]client.Opt{client.FromEnv}, opts...)
	}
	c, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}
	return &Provider{client: c}, nil
}

func (p *Provider) Name() string { return "docker" }

// basePath returns the protocol base path for the given container ID.
func (p *Provider) basePath(id string) string {
	if v, ok := p.basePathCache.Load(id); ok {
		return v.(string)
	}
	return protocol.DefaultBasePath
}

// Provision creates and starts a new Docker container.
func (p *Provider) Provision(ctx context.Context, opts sandbox.ProvisionOptions) (*sandbox.Sandbox, error) {
	containerConfig := &container.Config{
		Image:     opts.Image,
		Tty:       true,
		OpenStdin: true,
		Env:       envMapToSlice(opts.Env),
	}
	if len(opts.Cmd) > 0 {
		containerConfig.Cmd = opts.Cmd
	}

	hostConfig := &container.HostConfig{
		Resources: container.Resources{
			Memory:    opts.Resources.MemoryBytes,
			CPUPeriod: 100000,
			CPUQuota:  opts.Resources.CPUQuota,
		},
		SecurityOpt: []string{"no-new-privileges:true"},
	}

	networkMode := os.Getenv("SANDBOX_NETWORK_MODE")
	if networkMode == "" {
		networkMode = "bridge"
	}
	if err := validateNetworkMode(networkMode); err != nil {
		return nil, fmt.Errorf("invalid network mode: %w", err)
	}
	hostConfig.NetworkMode = container.NetworkMode(networkMode)

	containerName := fmt.Sprintf("agentbox-%s", opts.TaskID)

	resp, err := p.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	if err := p.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = p.client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	// Store base path for this container
	bp := opts.BasePath
	if bp == "" {
		bp = protocol.DefaultBasePath
	}
	p.basePathCache.Store(resp.ID, bp)

	return &sandbox.Sandbox{
		ID:         resp.ID,
		Provider:   "docker",
		WorkingDir: opts.WorkingDir,
	}, nil
}

func validateNetworkMode(mode string) error {
	switch mode {
	case "bridge", "none":
		return nil
	case "host":
		return fmt.Errorf("'host' network mode is not allowed for sandbox containers")
	default:
		if !validNetworkName.MatchString(mode) {
			return fmt.Errorf("network name %q contains invalid characters", mode)
		}
		return nil
	}
}

// Exec executes a command in a Docker container.
func (p *Provider) Exec(ctx context.Context, id string, cmd sandbox.ExecCommand) (*sandbox.ExecResult, error) {
	execConfig := types.ExecConfig{
		Cmd:          cmd.Command,
		AttachStdout: true,
		AttachStderr: true,
		User:         cmd.User,
		WorkingDir:   cmd.WorkingDir,
		Env:          envMapToSlice(cmd.Env),
	}
	execID, err := p.client.ContainerExecCreate(ctx, id, execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}
	resp, err := p.client.ContainerExecAttach(ctx, execID.ID, types.ExecStartCheck{})
	if err != nil {
		return nil, fmt.Errorf("failed to attach exec: %w", err)
	}
	defer resp.Close()

	var stdout, stderr bytes.Buffer
	_, err = stdcopy.StdCopy(&stdout, &stderr, resp.Reader)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read exec output: %w", err)
	}
	inspect, err := p.client.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect exec: %w", err)
	}

	result := &sandbox.ExecResult{
		ExitCode: inspect.ExitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}

	// Log to container log file (best-effort)
	timestamp := time.Now().Format(time.RFC3339)
	logEntry := fmt.Sprintf("[%s] Command: %v\nExit Code: %d\nStdout:\n%s\nStderr:\n%s\n---\n",
		timestamp, execConfig.Cmd, result.ExitCode, result.Stdout, result.Stderr)
	_ = p.writeToLog(ctx, id, logEntry)

	return result, nil
}

func (p *Provider) writeToLog(ctx context.Context, id string, message string) error {
	execConfig := types.ExecConfig{
		Cmd:         []string{"sh", "-c", "cat >> /tmp/agentbox.log"},
		AttachStdin: true,
	}
	execID, err := p.client.ContainerExecCreate(ctx, id, execConfig)
	if err != nil {
		return err
	}
	resp, err := p.client.ContainerExecAttach(ctx, execID.ID, types.ExecStartCheck{})
	if err != nil {
		return err
	}
	defer resp.Close()
	_, err = resp.Conn.Write([]byte(message))
	if err != nil {
		return err
	}
	return resp.Conn.Close()
}

// ExecShell executes a shell command string in a container.
func (p *Provider) ExecShell(ctx context.Context, id string, command string, user string) (*sandbox.ExecResult, error) {
	return p.Exec(ctx, id, sandbox.ExecCommand{
		Command: []string{"sh", "-c", command},
		User:    user,
	})
}

// CopyFrom copies a file from a Docker container.
func (p *Provider) CopyFrom(ctx context.Context, id string, srcPath string) (io.ReadCloser, error) {
	reader, _, err := p.client.CopyFromContainer(ctx, id, srcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to copy from container: %w", err)
	}
	return reader, nil
}

// CopyTo copies data into a Docker container.
func (p *Provider) CopyTo(ctx context.Context, id string, src io.Reader, destPath string) error {
	limited := io.LimitReader(src, MaxFileReadSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("failed to read source data: %w", err)
	}
	if len(data) > MaxFileReadSize {
		return fmt.Errorf("source data exceeds maximum size (%d bytes)", MaxFileReadSize)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	header := &tar.Header{
		Name: path.Base(destPath),
		Mode: 0644,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("failed to write tar data: %w", err)
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("failed to close tar writer: %w", err)
	}

	err = p.client.CopyToContainer(ctx, id, path.Dir(destPath), &buf, types.CopyToContainerOptions{})
	if err != nil {
		return fmt.Errorf("failed to copy to container: %w", err)
	}
	return nil
}

// Status returns the current status of a Docker container.
func (p *Provider) Status(ctx context.Context, id string) (*sandbox.SandboxStatus, error) {
	inspect, err := p.client.ContainerInspect(ctx, id)
	if err != nil {
		if client.IsErrNotFound(err) {
			return &sandbox.SandboxStatus{Phase: sandbox.SandboxPhaseUnknown, Message: "container not found"}, nil
		}
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	var phase sandbox.SandboxPhase
	var message string
	switch {
	case inspect.State.Running:
		phase, message = sandbox.SandboxPhaseRunning, "container is running"
	case inspect.State.Paused:
		phase, message = sandbox.SandboxPhasePending, "container is paused"
	case inspect.State.Restarting:
		phase, message = sandbox.SandboxPhasePending, "container is restarting"
	case inspect.State.Dead:
		phase, message = sandbox.SandboxPhaseFailed, fmt.Sprintf("container is dead: %s", inspect.State.Error)
	case inspect.State.ExitCode == 0:
		phase, message = sandbox.SandboxPhaseSucceeded, "container exited successfully"
	case inspect.State.ExitCode != 0:
		phase, message = sandbox.SandboxPhaseFailed, fmt.Sprintf("container exited with code %d", inspect.State.ExitCode)
	default:
		phase, message = sandbox.SandboxPhaseUnknown, inspect.State.Status
	}
	return &sandbox.SandboxStatus{Phase: phase, Message: message}, nil
}

// Cleanup stops and removes a Docker container.
func (p *Provider) Cleanup(ctx context.Context, id string) error {
	timeout := 10
	if err := p.client.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout}); err != nil {
		if !client.IsErrNotFound(err) {
			fmt.Printf("Warning: failed to stop container %s: %v\n", shortID(id), err)
		}
	}
	if err := p.client.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}); err != nil {
		if client.IsErrNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to remove container: %w", err)
	}
	p.basePathCache.Delete(id)
	return nil
}

// IsContainerRunning returns true if the container is currently running.
func (p *Provider) IsContainerRunning(ctx context.Context, id string) (bool, error) {
	inspect, err := p.client.ContainerInspect(ctx, id)
	if err != nil {
		if client.IsErrNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return inspect.State.Running, nil
}

// PullImageIfNeeded pulls a container image if it doesn't exist locally.
func (p *Provider) PullImageIfNeeded(ctx context.Context, imageName string) error {
	_, _, err := p.client.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		return nil
	}
	if !client.IsErrNotFound(err) {
		return fmt.Errorf("failed to inspect image: %w", err)
	}
	reader, err := p.client.ImagePull(ctx, imageName, types.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}
	defer func() { _ = reader.Close() }()
	_, err = io.Copy(io.Discard, reader)
	return err
}

// GetClient returns the underlying Docker client.
func (p *Provider) GetClient() *client.Client { return p.client }

// --- AgentProvider protocol methods ---

// SubmitManifest writes the task manifest into the sandbox.
func (p *Provider) SubmitManifest(ctx context.Context, id string, manifest []byte) error {
	basePath := p.basePath(id)
	_, err := p.Exec(ctx, id, sandbox.ExecCommand{
		Command: []string{"mkdir", "-p", basePath},
		User:    "agent",
	})
	if err != nil {
		return fmt.Errorf("failed to create agentbox directory: %w", err)
	}
	return p.CopyTo(ctx, id, bytes.NewReader(manifest), protocol.ManifestPath(basePath))
}

// PollStatus reads the agent's current status from the sandbox as raw bytes.
func (p *Provider) PollStatus(ctx context.Context, id string) ([]byte, error) {
	data, err := p.readFile(ctx, id, protocol.StatusPath(p.basePath(id)))
	if err != nil {
		return nil, err
	}
	if data == nil {
		// File doesn't exist yet — return initializing status
		defaultStatus := protocol.AgentStatus{
			Phase:   protocol.PhaseInitializing,
			Message: "Waiting for agent to start",
		}
		return json.Marshal(defaultStatus)
	}
	return data, nil
}

// ReadResult reads the agent's full result from the sandbox as raw bytes.
func (p *Provider) ReadResult(ctx context.Context, id string) ([]byte, error) {
	data, err := p.readFile(ctx, id, protocol.ResultPath(p.basePath(id)))
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("result.json not found")
	}
	return data, nil
}

// SubmitSteering writes a steering instruction to the sandbox.
func (p *Provider) SubmitSteering(ctx context.Context, id string, instruction []byte) error {
	return p.CopyTo(ctx, id, bytes.NewReader(instruction), protocol.SteeringPath(p.basePath(id)))
}

// readFile reads a file from the sandbox via tar, returning nil if not found.
func (p *Provider) readFile(ctx context.Context, id string, filePath string) ([]byte, error) {
	reader, err := p.CopyFrom(ctx, id, filePath)
	if err != nil {
		return nil, nil //nolint:nilerr // file not existing is expected
	}
	defer reader.Close()

	tr := tar.NewReader(reader)
	_, err = tr.Next()
	if err != nil {
		return nil, nil //nolint:nilerr // empty tar = file-not-found
	}

	limited := io.LimitReader(tr, MaxFileReadSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
	}
	if len(data) > MaxFileReadSize {
		return nil, fmt.Errorf("file %s exceeds maximum size (%d bytes)", filePath, MaxFileReadSize)
	}
	return data, nil
}

func envMapToSlice(env map[string]string) []string {
	if env == nil {
		return nil
	}
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}
	return result
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	if id == "" {
		return "<empty>"
	}
	return id
}
```

**Step 3b: Implement `sandbox/docker/register.go`**

```go
package docker

import "github.com/tinkerloft/agentbox/sandbox"

func init() {
	sandbox.RegisterProvider("docker", func(_ sandbox.ProviderConfig) (sandbox.AgentProvider, error) {
		return NewProvider()
	})
}
```

**Step 4: Run tests — expect pass (Docker-requiring tests will skip)**

```bash
go test ./sandbox/docker/... -v
```

Expected: unit tests PASS, Docker-requiring tests SKIP if Docker unavailable

**Step 5: Commit**

```bash
git add sandbox/docker/
git commit -m "feat: add sandbox/docker provider"
```

---

## Task 5: `sandbox/k8s` package ✅ Complete

**Files:**
- Create: `sandbox/k8s/exec.go`
- Create: `sandbox/k8s/wait.go`
- Create: `sandbox/k8s/job.go`
- Create: `sandbox/k8s/provider.go`
- Create: `sandbox/k8s/register.go`
- Create: `sandbox/k8s/provider_test.go`

**Step 1: Write the failing test**

Create `sandbox/k8s/provider_test.go`:

```go
package k8s

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/tinkerloft/agentbox/sandbox"
)

func TestBuildJobSpec_Labels(t *testing.T) {
	opts := sandbox.ProvisionOptions{TaskID: "task-123", Image: "ubuntu:22.04"}
	job := buildJobSpec(opts, "sandbox-isolated", "agentbox-agent:latest")

	assert.Equal(t, "agentbox-sandbox-task-123", job.Name)
	assert.Equal(t, "sandbox-isolated", job.Namespace)
	assert.Equal(t, "task-123", job.Labels[labelTaskID])
	assert.Equal(t, "agentbox", job.Labels[labelManagedBy])
}

func TestBuildJobSpec_CmdOverride(t *testing.T) {
	t.Run("custom Cmd is used", func(t *testing.T) {
		opts := sandbox.ProvisionOptions{
			TaskID: "task-1", Image: "ubuntu:22.04",
			Cmd: []string{"/agent-bin/agent", "serve"},
		}
		job := buildJobSpec(opts, "test-ns", "agent:v1")
		assert.Equal(t, []string{"/agent-bin/agent", "serve"}, job.Spec.Template.Spec.Containers[0].Command)
	})

	t.Run("empty Cmd falls back to idle", func(t *testing.T) {
		opts := sandbox.ProvisionOptions{TaskID: "task-2", Image: "ubuntu:22.04"}
		job := buildJobSpec(opts, "test-ns", "agent:v1")
		assert.Equal(t, []string{"sh", "-c", "tail -f /dev/null"}, job.Spec.Template.Spec.Containers[0].Command)
	})
}

func TestBuildJobSpec_SecurityContext(t *testing.T) {
	opts := sandbox.ProvisionOptions{TaskID: "task-sec", Image: "ubuntu:22.04"}
	job := buildJobSpec(opts, "sandbox-isolated", "agent:v1")

	podSec := job.Spec.Template.Spec.SecurityContext
	require.NotNil(t, podSec)
	assert.True(t, *podSec.RunAsNonRoot)
	assert.Equal(t, int64(1000), *podSec.RunAsUser)
	assert.Equal(t, int64(1000), *podSec.FSGroup)
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, podSec.SeccompProfile.Type)
	assert.False(t, *job.Spec.Template.Spec.AutomountServiceAccountToken)

	containerSec := job.Spec.Template.Spec.Containers[0].SecurityContext
	require.NotNil(t, containerSec)
	assert.Equal(t, []corev1.Capability{"ALL"}, containerSec.Capabilities.Drop)
}

func TestBuildJobSpec_RuntimeClass(t *testing.T) {
	opts := sandbox.ProvisionOptions{
		TaskID: "task-rc", Image: "ubuntu:22.04", RuntimeClass: "gvisor",
	}
	job := buildJobSpec(opts, "test-ns", "agent:v1")
	require.NotNil(t, job.Spec.Template.Spec.RuntimeClassName)
	assert.Equal(t, "gvisor", *job.Spec.Template.Spec.RuntimeClassName)
}

func TestBuildJobSpec_UserNamespace(t *testing.T) {
	opts := sandbox.ProvisionOptions{
		TaskID: "task-uns", Image: "ubuntu:22.04", UserNamespace: true,
	}
	job := buildJobSpec(opts, "test-ns", "agent:v1")
	require.NotNil(t, job.Spec.Template.Spec.HostUsers)
	assert.False(t, *job.Spec.Template.Spec.HostUsers)
}

func TestBuildJobSpec_Resources(t *testing.T) {
	opts := sandbox.ProvisionOptions{
		TaskID: "task-res", Image: "ubuntu:22.04",
		Resources: sandbox.ResourceLimits{
			MemoryBytes: 4 * 1024 * 1024 * 1024, CPUQuota: 200000, EphemeralStorage: 20 * 1024 * 1024 * 1024,
		},
	}
	job := buildJobSpec(opts, "sandbox-isolated", "agent:v1")
	limits := job.Spec.Template.Spec.Containers[0].Resources.Limits
	require.NotNil(t, limits)
	assert.Equal(t, int64(4*1024*1024*1024), limits[corev1.ResourceMemory].Value())
	assert.Equal(t, int64(2000), limits[corev1.ResourceCPU].MilliValue())
	assert.Equal(t, int64(20*1024*1024*1024), limits[corev1.ResourceEphemeralStorage].Value())
}

func TestBuildJobSpec_BackoffLimit(t *testing.T) {
	opts := sandbox.ProvisionOptions{TaskID: "task-bo", Image: "ubuntu:22.04"}
	job := buildJobSpec(opts, "sandbox-isolated", "agent:v1")
	require.NotNil(t, job.Spec.BackoffLimit)
	assert.Equal(t, int32(0), *job.Spec.BackoffLimit)
}

func TestProvision_CreatesJob(t *testing.T) {
	//nolint:staticcheck
	clientset := fake.NewSimpleClientset()
	provider := newProviderFromClient(clientset, nil, "test-ns", "agent:v1")

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "agentbox-sandbox-task-1-abc", Namespace: "test-ns",
			Labels: map[string]string{"job-name": "agentbox-sandbox-task-1"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	_, err := clientset.CoreV1().Pods("test-ns").Create(context.Background(), pod, metav1.CreateOptions{})
	require.NoError(t, err)

	sb, err := provider.Provision(context.Background(), sandbox.ProvisionOptions{
		TaskID: "task-1", Image: "ubuntu:22.04",
		Cmd: []string{"/agent-bin/agent", "serve"},
	})
	require.NoError(t, err)
	assert.Equal(t, "agentbox-sandbox-task-1", sb.ID)
	assert.Equal(t, "kubernetes", sb.Provider)
}

func TestCleanup_DeletesJob(t *testing.T) {
	//nolint:staticcheck
	clientset := fake.NewSimpleClientset()
	provider := newProviderFromClient(clientset, nil, "test-ns", "agent:v1")

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "agentbox-sandbox-task-2", Namespace: "test-ns"},
	}
	_, err := clientset.BatchV1().Jobs("test-ns").Create(context.Background(), job, metav1.CreateOptions{})
	require.NoError(t, err)

	err = provider.Cleanup(context.Background(), "agentbox-sandbox-task-2")
	assert.NoError(t, err)
}

func TestCleanup_Idempotent(t *testing.T) {
	//nolint:staticcheck
	clientset := fake.NewSimpleClientset()
	provider := newProviderFromClient(clientset, nil, "test-ns", "agent:v1")
	err := provider.Cleanup(context.Background(), "non-existent-job")
	assert.NoError(t, err)
}

func TestStatus_PodPhases(t *testing.T) {
	tests := []struct {
		podPhase      corev1.PodPhase
		expectedPhase sandbox.SandboxPhase
	}{
		{corev1.PodPending, sandbox.SandboxPhasePending},
		{corev1.PodRunning, sandbox.SandboxPhaseRunning},
		{corev1.PodSucceeded, sandbox.SandboxPhaseSucceeded},
		{corev1.PodFailed, sandbox.SandboxPhaseFailed},
	}
	for _, tt := range tests {
		t.Run(string(tt.podPhase), func(t *testing.T) {
			jobName := "agentbox-sandbox-status-" + string(tt.podPhase)
			//nolint:staticcheck
			clientset := fake.NewSimpleClientset()
			provider := newProviderFromClient(clientset, nil, "test-ns", "agent:v1")
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: jobName + "-pod", Namespace: "test-ns",
					Labels: map[string]string{"job-name": jobName},
				},
				Status: corev1.PodStatus{Phase: tt.podPhase},
			}
			_, err := clientset.CoreV1().Pods("test-ns").Create(context.Background(), pod, metav1.CreateOptions{})
			require.NoError(t, err)
			status, err := provider.Status(context.Background(), jobName)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedPhase, status.Phase)
		})
	}
}

func TestName(t *testing.T) {
	//nolint:staticcheck
	clientset := fake.NewSimpleClientset()
	provider := newProviderFromClient(clientset, nil, "test-ns", "agent:v1")
	assert.Equal(t, "kubernetes", provider.Name())
}
```

**Step 2: Run test — expect failure**

```bash
go test ./sandbox/k8s/...
```

Expected: `cannot find package "github.com/tinkerloft/agentbox/sandbox/k8s"`

**Step 3a: Implement `sandbox/k8s/exec.go`**

```go
package k8s

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"

	"github.com/tinkerloft/agentbox/sandbox"
)

// MaxFileReadSize is the maximum bytes to read from sandbox files.
const MaxFileReadSize = 10 << 20 // 10 MB

func execCommand(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config, namespace, podName, containerName string, cmd []string, stdin io.Reader) (*sandbox.ExecResult, error) {
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").Name(podName).Namespace(namespace).SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdin:     stdin != nil,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return nil, fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		if exitErr, ok := err.(utilexec.ExitError); ok {
			return &sandbox.ExecResult{
				ExitCode: exitErr.ExitStatus(),
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
			}, nil
		}
		return nil, fmt.Errorf("exec stream failed: %w", err)
	}
	return &sandbox.ExecResult{ExitCode: 0, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

func execReadFile(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config, namespace, podName, filePath string) ([]byte, error) {
	result, err := execCommand(ctx, clientset, restConfig, namespace, podName, mainContainerName,
		[]string{"cat", filePath}, nil)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, nil
	}
	if len(result.Stdout) > MaxFileReadSize {
		return nil, fmt.Errorf("file %s exceeds maximum size (%d bytes)", filePath, MaxFileReadSize)
	}
	return []byte(result.Stdout), nil
}

func execWriteFile(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config, namespace, podName, filePath string, data []byte) error {
	dir := path.Dir(filePath)
	mkdirResult, err := execCommand(ctx, clientset, restConfig, namespace, podName, mainContainerName,
		[]string{"mkdir", "-p", dir}, nil)
	if err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	if mkdirResult.ExitCode != 0 {
		return fmt.Errorf("create directory %s failed (exit %d): %s", dir, mkdirResult.ExitCode, mkdirResult.Stderr)
	}

	result, err := execCommand(ctx, clientset, restConfig, namespace, podName, mainContainerName,
		[]string{"sh", "-c", "cat > \"$1\"", "--", filePath}, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("write file %s: %w", filePath, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("write file %s failed (exit %d): %s", filePath, result.ExitCode, result.Stderr)
	}
	return nil
}
```

**Step 3b: Implement `sandbox/k8s/wait.go`**

```go
package k8s

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

func jobLabelSelector(jobName string) string {
	return fmt.Sprintf("job-name=%s", jobName)
}

func waitForPodRunning(ctx context.Context, clientset kubernetes.Interface, namespace, jobName string, cache *sync.Map) (string, error) {
	selector := jobLabelSelector(jobName)
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return "", fmt.Errorf("failed to list pods for job %s: %w", jobName, err)
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Status.Phase == corev1.PodRunning {
			cache.Store(jobName, pod.Name)
			return pod.Name, nil
		}
	}

	watcher, err := clientset.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{
		LabelSelector:   selector,
		ResourceVersion: pods.ResourceVersion,
	})
	if err != nil {
		return "", fmt.Errorf("failed to watch pods for job %s: %w", jobName, err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timed out waiting for pod: %w", ctx.Err())
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return "", fmt.Errorf("watch channel closed for job %s", jobName)
			}
			if event.Type == watch.Error {
				return "", fmt.Errorf("watch error for job %s", jobName)
			}
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				continue
			}
			switch pod.Status.Phase {
			case corev1.PodRunning:
				cache.Store(jobName, pod.Name)
				return pod.Name, nil
			case corev1.PodFailed:
				return "", fmt.Errorf("pod %s failed: %s", pod.Name, pod.Status.Message)
			case corev1.PodSucceeded:
				return "", fmt.Errorf("pod %s completed before reaching running state", pod.Name)
			}
		}
	}
}

func findPodForJob(ctx context.Context, clientset kubernetes.Interface, namespace, jobName string, cache *sync.Map) (string, error) {
	if cached, ok := cache.Load(jobName); ok {
		return cached.(string), nil
	}
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: jobLabelSelector(jobName),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods for job %s: %w", jobName, err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods found for job %s", jobName)
	}
	podName := pods.Items[0].Name
	cache.Store(jobName, podName)
	return podName, nil
}

func clearPodCache(jobName string, cache *sync.Map) {
	cache.Delete(jobName)
}
```

**Step 3c: Implement `sandbox/k8s/job.go`**

```go
// Package k8s provides a Kubernetes-based sandbox provider using Jobs.
package k8s

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/tinkerloft/agentbox/sandbox"
)

const (
	agentBinVolume    = "agent-bin"
	agentBinMountPath = "/agent-bin"
	mainContainerName = "sandbox"
	initContainerName = "inject-agent"
	labelTaskID       = "agentbox.io/task-id"
	labelManagedBy    = "app.kubernetes.io/managed-by"
)

func buildJobSpec(opts sandbox.ProvisionOptions, namespace, agentImage string) *batchv1.Job {
	labels := map[string]string{
		labelTaskID:    opts.TaskID,
		labelManagedBy: "agentbox",
	}

	// Default idle command; overridden by opts.Cmd if provided
	cmd := []string{"sh", "-c", "tail -f /dev/null"}
	if len(opts.Cmd) > 0 {
		cmd = opts.Cmd
	}

	agentBinMount := corev1.VolumeMount{Name: agentBinVolume, MountPath: agentBinMountPath}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("agentbox-sandbox-%s", opts.TaskID),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: ptr(int32(0)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					ServiceAccountName:           "sandbox-runner",
					AutomountServiceAccountToken: ptr(false),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr(true),
						RunAsUser:    ptr(int64(1000)),
						FSGroup:      ptr(int64(1000)),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
					InitContainers: []corev1.Container{
						{
							Name:         initContainerName,
							Image:        agentImage,
							Command:      []string{"cp", "/usr/local/bin/agent", agentBinMountPath + "/agent"},
							VolumeMounts: []corev1.VolumeMount{agentBinMount},
						},
					},
					Containers: []corev1.Container{
						{
							Name:      mainContainerName,
							Image:     opts.Image,
							Command:   cmd,
							Env:       buildEnvVars(opts.Env),
							Resources: buildResourceLimits(opts.Resources),
							SecurityContext: &corev1.SecurityContext{
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							VolumeMounts: []corev1.VolumeMount{agentBinMount},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name:         agentBinVolume,
							VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
						},
					},
				},
			},
		},
	}

	// Apply optional K8s security fields
	if opts.RuntimeClass != "" {
		job.Spec.Template.Spec.RuntimeClassName = ptr(opts.RuntimeClass)
	}
	if opts.UserNamespace {
		job.Spec.Template.Spec.HostUsers = ptr(false)
	}
	if len(opts.NodeSelector) > 0 {
		job.Spec.Template.Spec.NodeSelector = opts.NodeSelector
	}
	if len(opts.Tolerations) > 0 {
		job.Spec.Template.Spec.Tolerations = buildTolerations(opts.Tolerations)
	}
	if opts.OwnerRef != nil {
		job.ObjectMeta.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion: opts.OwnerRef.APIVersion,
				Kind:       opts.OwnerRef.Kind,
				Name:       opts.OwnerRef.Name,
				UID:        types_UID(opts.OwnerRef.UID),
			},
		}
	}

	return job
}

// types_UID converts string UID to k8s types.UID.
// Imported inline to avoid importing k8s.io/apimachinery/pkg/types just for this.
func types_UID(uid string) metav1.UID { return metav1.UID(uid) } //nolint:revive

func buildEnvVars(env map[string]string) []corev1.EnvVar {
	if len(env) == 0 {
		return nil
	}
	vars := make([]corev1.EnvVar, 0, len(env))
	for k, v := range env {
		vars = append(vars, corev1.EnvVar{Name: k, Value: v})
	}
	return vars
}

func buildResourceLimits(res sandbox.ResourceLimits) corev1.ResourceRequirements {
	if res.MemoryBytes <= 0 && res.CPUQuota <= 0 && res.EphemeralStorage <= 0 {
		return corev1.ResourceRequirements{}
	}
	limits := corev1.ResourceList{}
	if res.MemoryBytes > 0 {
		limits[corev1.ResourceMemory] = *resource.NewQuantity(res.MemoryBytes, resource.BinarySI)
	}
	if res.CPUQuota > 0 {
		limits[corev1.ResourceCPU] = *resource.NewMilliQuantity(res.CPUQuota/100, resource.DecimalSI)
	}
	if res.EphemeralStorage > 0 {
		limits[corev1.ResourceEphemeralStorage] = *resource.NewQuantity(res.EphemeralStorage, resource.BinarySI)
	}
	return corev1.ResourceRequirements{Limits: limits, Requests: limits} // requests=limits for Guaranteed QoS
}

func buildTolerations(tolerations []sandbox.Toleration) []corev1.Toleration {
	result := make([]corev1.Toleration, 0, len(tolerations))
	for _, t := range tolerations {
		result = append(result, corev1.Toleration{
			Key:      t.Key,
			Operator: corev1.TolerationOperator(t.Operator),
			Value:    t.Value,
			Effect:   corev1.TaintEffect(t.Effect),
		})
	}
	return result
}

func ptr[T any](v T) *T { return &v }
```

> **Note:** `metav1.UID` should be `k8s.io/apimachinery/pkg/types.UID`. Fix the `types_UID` function to import and use `k8s.io/apimachinery/pkg/types`:
> ```go
> import ktypes "k8s.io/apimachinery/pkg/types"
> // then use: UID: ktypes.UID(opts.OwnerRef.UID)
> ```
> Remove the `types_UID` helper and update `job.ObjectMeta.OwnerReferences` accordingly.

**Step 3d: Implement `sandbox/k8s/provider.go`**

```go
package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	corev1 "k8s.io/api/core/v1"

	"github.com/tinkerloft/agentbox/protocol"
	"github.com/tinkerloft/agentbox/sandbox"
)

const (
	defaultNamespace  = "sandbox-isolated"
	defaultAgentImage = "agentbox-agent:latest"
)

var _ sandbox.AgentProvider = (*Provider)(nil)

// Provider implements sandbox.AgentProvider using Kubernetes Jobs.
type Provider struct {
	clientset     kubernetes.Interface
	restConfig    *rest.Config
	namespace     string
	agentImage    string
	podCache      *sync.Map
	basePathCache sync.Map // jobName -> basePath string
}

func NewProvider(cfg sandbox.ProviderConfig) (*Provider, error) {
	namespace := valueOrDefault(cfg.Namespace, defaultNamespace)
	agentImage := valueOrDefault(cfg.AgentImage, defaultAgentImage)

	var restConfig *rest.Config
	var err error
	if cfg.KubeconfigPath != "" {
		restConfig, err = clientcmd.BuildConfigFromFlags("", cfg.KubeconfigPath)
	} else {
		restConfig, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to build k8s config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s clientset: %w", err)
	}
	return &Provider{
		clientset:  clientset,
		restConfig: restConfig,
		namespace:  namespace,
		agentImage: agentImage,
		podCache:   &sync.Map{},
	}, nil
}

func newProviderFromClient(clientset kubernetes.Interface, restConfig *rest.Config, namespace, agentImage string) *Provider {
	return &Provider{
		clientset:  clientset,
		restConfig: restConfig,
		namespace:  valueOrDefault(namespace, defaultNamespace),
		agentImage: valueOrDefault(agentImage, defaultAgentImage),
		podCache:   &sync.Map{},
	}
}

func valueOrDefault(val, fallback string) string {
	if val != "" {
		return val
	}
	return fallback
}

func (p *Provider) Name() string { return "kubernetes" }

func (p *Provider) basePath(id string) string {
	if v, ok := p.basePathCache.Load(id); ok {
		return v.(string)
	}
	return protocol.DefaultBasePath
}

func (p *Provider) Provision(ctx context.Context, opts sandbox.ProvisionOptions) (*sandbox.Sandbox, error) {
	job := buildJobSpec(opts, p.namespace, p.agentImage)
	created, err := p.clientset.BatchV1().Jobs(p.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}
	if _, err := waitForPodRunning(ctx, p.clientset, p.namespace, created.Name, p.podCache); err != nil {
		_ = p.Cleanup(ctx, created.Name)
		return nil, fmt.Errorf("failed waiting for pod: %w", err)
	}

	bp := opts.BasePath
	if bp == "" {
		bp = protocol.DefaultBasePath
	}
	p.basePathCache.Store(created.Name, bp)

	return &sandbox.Sandbox{ID: created.Name, Provider: "kubernetes", WorkingDir: opts.WorkingDir}, nil
}

func (p *Provider) Exec(ctx context.Context, id string, cmd sandbox.ExecCommand) (*sandbox.ExecResult, error) {
	podName, err := findPodForJob(ctx, p.clientset, p.namespace, id, p.podCache)
	if err != nil {
		return nil, err
	}
	execCmd := cmd.Command
	if cmd.WorkingDir != "" {
		shell := fmt.Sprintf("cd %q && exec \"$@\"", cmd.WorkingDir)
		execCmd = append([]string{"sh", "-c", shell, "--"}, cmd.Command...)
	}
	return execCommand(ctx, p.clientset, p.restConfig, p.namespace, podName, mainContainerName, execCmd, nil)
}

func (p *Provider) ExecShell(ctx context.Context, id string, command string, user string) (*sandbox.ExecResult, error) {
	return p.Exec(ctx, id, sandbox.ExecCommand{Command: []string{"bash", "-c", command}, User: user})
}

func (p *Provider) CopyTo(ctx context.Context, id string, src io.Reader, destPath string) error {
	podName, err := findPodForJob(ctx, p.clientset, p.namespace, id, p.podCache)
	if err != nil {
		return err
	}
	limited := io.LimitReader(src, MaxFileReadSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("failed to read source data: %w", err)
	}
	if len(data) > MaxFileReadSize {
		return fmt.Errorf("source data exceeds maximum size (%d bytes)", MaxFileReadSize)
	}
	return execWriteFile(ctx, p.clientset, p.restConfig, p.namespace, podName, destPath, data)
}

func (p *Provider) CopyFrom(ctx context.Context, id string, srcPath string) (io.ReadCloser, error) {
	podName, err := findPodForJob(ctx, p.clientset, p.namespace, id, p.podCache)
	if err != nil {
		return nil, err
	}
	data, err := execReadFile(ctx, p.clientset, p.restConfig, p.namespace, podName, srcPath)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("file not found: %s", srcPath)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (p *Provider) Status(ctx context.Context, id string) (*sandbox.SandboxStatus, error) {
	podName, err := findPodForJob(ctx, p.clientset, p.namespace, id, p.podCache)
	if err != nil {
		return &sandbox.SandboxStatus{Phase: sandbox.SandboxPhaseUnknown, Message: fmt.Sprintf("pod not found: %v", err)}, nil
	}
	pod, err := p.clientset.CoreV1().Pods(p.namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return &sandbox.SandboxStatus{Phase: sandbox.SandboxPhaseUnknown, Message: "pod not found"}, nil
		}
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}
	phase, message := mapPodPhase(pod.Status.Phase, pod.Status.Message)
	return &sandbox.SandboxStatus{Phase: phase, Message: message}, nil
}

func (p *Provider) Cleanup(ctx context.Context, id string) error {
	propagation := metav1.DeletePropagationForeground
	err := p.clientset.BatchV1().Jobs(p.namespace).Delete(ctx, id, metav1.DeleteOptions{PropagationPolicy: &propagation})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete job %s: %w", id, err)
	}
	clearPodCache(id, p.podCache)
	p.basePathCache.Delete(id)
	return nil
}

func (p *Provider) SubmitManifest(ctx context.Context, id string, manifest []byte) error {
	podName, err := findPodForJob(ctx, p.clientset, p.namespace, id, p.podCache)
	if err != nil {
		return err
	}
	return execWriteFile(ctx, p.clientset, p.restConfig, p.namespace, podName, protocol.ManifestPath(p.basePath(id)), manifest)
}

func (p *Provider) PollStatus(ctx context.Context, id string) ([]byte, error) {
	podName, err := findPodForJob(ctx, p.clientset, p.namespace, id, p.podCache)
	if err != nil {
		return nil, err
	}
	data, err := execReadFile(ctx, p.clientset, p.restConfig, p.namespace, podName, protocol.StatusPath(p.basePath(id)))
	if err != nil {
		return nil, err
	}
	if data == nil {
		defaultStatus := protocol.AgentStatus{Phase: protocol.PhaseInitializing, Message: "Waiting for agent to start"}
		return json.Marshal(defaultStatus)
	}
	return data, nil
}

func (p *Provider) ReadResult(ctx context.Context, id string) ([]byte, error) {
	podName, err := findPodForJob(ctx, p.clientset, p.namespace, id, p.podCache)
	if err != nil {
		return nil, err
	}
	data, err := execReadFile(ctx, p.clientset, p.restConfig, p.namespace, podName, protocol.ResultPath(p.basePath(id)))
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("result.json not found")
	}
	return data, nil
}

func (p *Provider) SubmitSteering(ctx context.Context, id string, instruction []byte) error {
	podName, err := findPodForJob(ctx, p.clientset, p.namespace, id, p.podCache)
	if err != nil {
		return err
	}
	return execWriteFile(ctx, p.clientset, p.restConfig, p.namespace, podName, protocol.SteeringPath(p.basePath(id)), instruction)
}

func mapPodPhase(phase corev1.PodPhase, message string) (sandbox.SandboxPhase, string) {
	switch phase {
	case corev1.PodPending:
		return sandbox.SandboxPhasePending, "pod is pending"
	case corev1.PodRunning:
		return sandbox.SandboxPhaseRunning, "pod is running"
	case corev1.PodSucceeded:
		return sandbox.SandboxPhaseSucceeded, "pod completed successfully"
	case corev1.PodFailed:
		msg := "pod failed"
		if message != "" {
			msg = fmt.Sprintf("pod failed: %s", message)
		}
		return sandbox.SandboxPhaseFailed, msg
	default:
		return sandbox.SandboxPhaseUnknown, fmt.Sprintf("unknown phase: %s", phase)
	}
}
```

**Step 3e: Implement `sandbox/k8s/register.go`**

```go
package k8s

import "github.com/tinkerloft/agentbox/sandbox"

func init() {
	sandbox.RegisterProvider("kubernetes", func(cfg sandbox.ProviderConfig) (sandbox.AgentProvider, error) {
		return NewProvider(cfg)
	})
	// Register k8s alias
	sandbox.RegisterProvider("k8s", func(cfg sandbox.ProviderConfig) (sandbox.AgentProvider, error) {
		return NewProvider(cfg)
	})
}
```

**Step 4: Run tests — expect pass**

```bash
go test ./sandbox/k8s/... -v
go build ./...
```

Expected: all unit tests PASS, integration tests SKIP

**Step 5: Commit**

```bash
git add sandbox/k8s/
git commit -m "feat: add sandbox/k8s provider with RuntimeClass/UserNamespace/OwnerRef support"
```

---

## Final Phase 1 Verification

```bash
cd /Users/andrew/dev/code/projects/agentbox
go build ./...
go test ./...
```

Expected: all packages build and all tests pass (Docker/K8s tests skip if infrastructure unavailable).
