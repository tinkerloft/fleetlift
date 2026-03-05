# Agentbox Split — Phase 2b & 3: Agent Primitives, Temporalkit, and Fleetlift Refactor

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `agent` Protocol primitives and `temporalkit` Temporal activity helpers to agentbox, then refactor fleetlift (branch `feat/agentbox-split`) to import agentbox instead of its internal sandbox/protocol packages.

**Architecture:** Phase 2b adds `agent/protocol.go` (Protocol struct with WaitForManifest/WriteStatus/WriteResult/WaitForSteering extracted from fleetlift's pipeline.go) and `temporalkit` (AgentActivities + SandboxActivities with `[]byte` interfaces). Phase 3 creates a `fleetproto` package for fleetlift-specific types, adds a protocol shim for backward compatibility, deletes `internal/sandbox/`, and updates all imports.

**Tech Stack:** Go 1.25.0, `go.temporal.io/sdk v1.27.0`, `github.com/stretchr/testify v1.11.1`

**Working directories:**
- agentbox: `/Users/andrew/dev/code/projects/agentbox`
- fleetlift: `/Users/andrew/dev/code/projects/fleetlift` (branch: `feat/agentbox-split`)

---

> **Status (2026-03-05):** PHASE 2b complete (Tasks 6-8). PHASE 3 Steps 9-13 (Tasks 9-13) complete. Task 14 (final verification) and legacy workflow migration (AB-3c) in progress.

## PHASE 2b — agentbox repo ✅ Complete

---

## Task 6: `agent` package — deps and constants ✅ Complete

**Files:**
- Create: `agent/deps.go`
- Create: `agent/constants.go`

**Step 1: No test needed** — these are pure interface/constant definitions.

**Step 2: Implement `agent/deps.go`**

Port exactly from `fleetlift/internal/agent/deps.go`, changing only the package doc:

```go
// Package agent provides primitives for building sandbox agent binaries.
// Consumers use Protocol to implement the file-based IPC protocol with
// the orchestrator, and FileSystem/CommandExecutor to run commands testably.
package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// ExitError represents a command that exited with a non-zero exit code.
type ExitError struct {
	Code   int
	Stderr string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit code %d: %s", e.Code, e.Stderr)
}

// FileSystem abstracts filesystem operations for testability.
type FileSystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
	Remove(path string) error
	Rename(oldpath, newpath string) error
	Stat(path string) (os.FileInfo, error)
}

// CommandExecutor abstracts command execution for testability.
type CommandExecutor interface {
	Run(ctx context.Context, opts CommandOpts) (*CommandResult, error)
}

// CommandOpts configures a command execution.
type CommandOpts struct {
	Name  string
	Args  []string
	Dir   string
	// Env, if non-empty, REPLACES the process environment entirely.
	Env   []string
	Stdin io.Reader
}

// CommandResult holds the output of a command execution.
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// osFileSystem implements FileSystem using the real OS.
type osFileSystem struct{}

func (osFileSystem) ReadFile(path string) ([]byte, error)                { return os.ReadFile(path) }
func (osFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}
func (osFileSystem) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (osFileSystem) Remove(path string) error                     { return os.Remove(path) }
func (osFileSystem) Rename(oldpath, newpath string) error         { return os.Rename(oldpath, newpath) }
func (osFileSystem) Stat(path string) (os.FileInfo, error)        { return os.Stat(path) }

// osCommandExecutor implements CommandExecutor using os/exec.
type osCommandExecutor struct{}

func (osCommandExecutor) Run(ctx context.Context, opts CommandOpts) (*CommandResult, error) {
	cmd := exec.CommandContext(ctx, opts.Name, opts.Args...)
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	if len(opts.Env) > 0 {
		cmd.Env = opts.Env
	}
	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return &CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: -1}, err
		}
	}

	result := &CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}
	if exitCode != 0 {
		return result, &ExitError{Code: exitCode, Stderr: stderr.String()}
	}
	return result, nil
}
```

**Step 3: Implement `agent/constants.go`**

Only the poll intervals — NOT fleetlift-specific constants (no DefaultCloneDepth, MaxOutputTruncation, etc.):

```go
package agent

import "time"

// Polling intervals for the file-based protocol.
const (
	ManifestPollInterval = 500 * time.Millisecond
	SteeringPollInterval = 2 * time.Second
)
```

**Step 4: Build**

```bash
cd /Users/andrew/dev/code/projects/agentbox
go build ./agent/...
```

Expected: builds cleanly

**Step 5: Commit**

```bash
git add agent/deps.go agent/constants.go
git commit -m "feat: add agent deps interfaces and poll interval constants"
```

---

## Task 7: `agent/protocol.go` — Protocol struct ✅ Complete

This extracts the generic file-protocol mechanics from fleetlift's `pipeline.go` into a reusable `Protocol` struct. The logic is identical to fleetlift's `waitForManifest`, `writeStatus`, `writeResult`, and `waitForSteering` methods — just made into public methods with `[]byte`/`json.RawMessage` return types.

**Files:**
- Create: `agent/protocol.go`
- Create: `agent/protocol_test.go`

**Step 1: Write the failing test**

Create `agent/protocol_test.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/agentbox/protocol"
)

// mockFS implements FileSystem for testing.
type mockFS struct {
	files map[string][]byte
	err   error // if non-nil, all writes return this
}

func newMockFS() *mockFS { return &mockFS{files: make(map[string][]byte)} }

func (m *mockFS) ReadFile(path string) ([]byte, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}
func (m *mockFS) WriteFile(path string, data []byte, _ os.FileMode) error {
	if m.err != nil {
		return m.err
	}
	m.files[path] = data
	return nil
}
func (m *mockFS) MkdirAll(_ string, _ os.FileMode) error { return nil }
func (m *mockFS) Remove(path string) error               { delete(m.files, path); return nil }
func (m *mockFS) Rename(old, new string) error {
	data, ok := m.files[old]
	if !ok {
		return os.ErrNotExist
	}
	m.files[new] = data
	delete(m.files, old)
	return nil
}
func (m *mockFS) Stat(path string) (os.FileInfo, error) {
	if _, ok := m.files[path]; ok {
		return nil, nil // non-nil means "exists" for Stat callers
	}
	return nil, os.ErrNotExist
}

func TestWaitForManifest_ReturnsBytes(t *testing.T) {
	fs := newMockFS()
	proto := NewProtocol(ProtocolConfig{BasePath: "/base"}, fs, nil)

	manifest := map[string]string{"task_id": "test-123"}
	data, _ := json.Marshal(manifest)
	fs.files["/base/manifest.json"] = data

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	raw, err := proto.WaitForManifest(ctx)
	require.NoError(t, err)
	assert.Equal(t, json.RawMessage(data), raw)
}

func TestWaitForManifest_RespectsContextCancel(t *testing.T) {
	fs := newMockFS() // empty — no manifest file
	proto := NewProtocol(ProtocolConfig{BasePath: "/base", ManifestPollInterval: 10 * time.Millisecond}, fs, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := proto.WaitForManifest(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestWriteStatus_AtomicWrite(t *testing.T) {
	fs := newMockFS()
	proto := NewProtocol(ProtocolConfig{BasePath: "/base"}, fs, nil)

	status := protocol.AgentStatus{
		Phase:   protocol.PhaseExecuting,
		Message: "running",
	}
	proto.WriteStatus(status)

	// Check that status.json was written (not just .tmp)
	data, ok := fs.files["/base/status.json"]
	require.True(t, ok, "status.json should exist")
	_, tmpExists := fs.files["/base/status.json.tmp"]
	assert.False(t, tmpExists, "tmp file should be renamed away")

	var decoded protocol.AgentStatus
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, protocol.PhaseExecuting, decoded.Phase)
	assert.Equal(t, "running", decoded.Message)
}

func TestWriteResult_AtomicWrite(t *testing.T) {
	fs := newMockFS()
	proto := NewProtocol(ProtocolConfig{BasePath: "/base"}, fs, nil)

	result := json.RawMessage(`{"status":"complete"}`)
	err := proto.WriteResult(result)
	require.NoError(t, err)

	data, ok := fs.files["/base/result.json"]
	require.True(t, ok, "result.json should exist")
	_, tmpExists := fs.files["/base/result.json.tmp"]
	assert.False(t, tmpExists, "tmp file should be renamed away")
	assert.Equal(t, []byte(result), data)
}

func TestWaitForSteering_ClaimsAtomically(t *testing.T) {
	fs := newMockFS()
	proto := NewProtocol(ProtocolConfig{BasePath: "/base", SteeringPollInterval: 10 * time.Millisecond}, fs, nil)

	instruction := protocol.SteeringInstruction{
		Action:    protocol.SteeringActionApprove,
		Iteration: 1,
	}
	data, _ := json.Marshal(instruction)
	fs.files["/base/steering.json"] = data

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := proto.WaitForSteering(ctx)
	require.NoError(t, err)
	assert.Equal(t, protocol.SteeringActionApprove, result.Action)
	assert.Equal(t, 1, result.Iteration)

	// Original file and processing file should both be gone
	_, steeringExists := fs.files["/base/steering.json"]
	_, processingExists := fs.files["/base/steering.json.processing"]
	assert.False(t, steeringExists)
	assert.False(t, processingExists)
}

func TestWaitForSteering_IgnoresInvalidJSON(t *testing.T) {
	fs := newMockFS()
	proto := NewProtocol(ProtocolConfig{BasePath: "/base", SteeringPollInterval: 10 * time.Millisecond}, fs, nil)

	// First write invalid JSON — should be ignored (logged, not fatal)
	fs.files["/base/steering.json"] = []byte("not valid json{{{")

	// Then after a short delay, write valid JSON
	go func() {
		time.Sleep(30 * time.Millisecond)
		data, _ := json.Marshal(protocol.SteeringInstruction{Action: protocol.SteeringActionCancel})
		fs.files["/base/steering.json"] = data
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	result, err := proto.WaitForSteering(ctx)
	require.NoError(t, err)
	assert.Equal(t, protocol.SteeringActionCancel, result.Action)
}

func TestWaitForSteering_RespectsContextCancel(t *testing.T) {
	fs := newMockFS()
	proto := NewProtocol(ProtocolConfig{BasePath: "/base", SteeringPollInterval: 10 * time.Millisecond}, fs, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := proto.WaitForSteering(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestNewProtocol_Defaults(t *testing.T) {
	proto := NewProtocol(ProtocolConfig{}, osFileSystem{}, nil)
	assert.Equal(t, protocol.DefaultBasePath, proto.basePath)
	assert.Equal(t, ManifestPollInterval, proto.manifestPollInterval)
	assert.Equal(t, SteeringPollInterval, proto.steeringPollInterval)
}

func TestNewProtocol_DefaultBasePath(t *testing.T) {
	// Verify the expected default path constant
	assert.Equal(t, "/workspace/.agentbox", protocol.DefaultBasePath)
	_ = filepath.Join // just using filepath to avoid unused import
}
```

**Step 2: Run test — expect failure**

```bash
cd /Users/andrew/dev/code/projects/agentbox
go test ./agent/...
```

Expected: `undefined: NewProtocol`, `undefined: Protocol`

**Step 3: Implement `agent/protocol.go`**

```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/tinkerloft/agentbox/protocol"
)

// ProtocolConfig configures the file-based protocol I/O paths and timing.
type ProtocolConfig struct {
	BasePath             string        // default: protocol.DefaultBasePath
	ManifestPollInterval time.Duration // default: ManifestPollInterval (500ms)
	SteeringPollInterval time.Duration // default: SteeringPollInterval (2s)
}

// Protocol handles file-based IPC between the agent binary and the orchestrator.
// It is the agentbox equivalent of fleetlift's pipeline file-I/O methods.
type Protocol struct {
	basePath             string
	manifestPollInterval time.Duration
	steeringPollInterval time.Duration
	fs                   FileSystem
	logger               *slog.Logger
}

// NewProtocol creates a Protocol with the given config and dependencies.
// Nil fs defaults to real OS filesystem. Nil logger defaults to JSON stderr.
func NewProtocol(cfg ProtocolConfig, fs FileSystem, logger *slog.Logger) *Protocol {
	basePath := cfg.BasePath
	if basePath == "" {
		basePath = protocol.DefaultBasePath
	}
	manifestInterval := cfg.ManifestPollInterval
	if manifestInterval == 0 {
		manifestInterval = ManifestPollInterval
	}
	steeringInterval := cfg.SteeringPollInterval
	if steeringInterval == 0 {
		steeringInterval = SteeringPollInterval
	}
	if fs == nil {
		fs = osFileSystem{}
	}
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	return &Protocol{
		basePath:             basePath,
		manifestPollInterval: manifestInterval,
		steeringPollInterval: steeringInterval,
		fs:                   fs,
		logger:               logger,
	}
}

// WaitForManifest blocks until manifest.json appears in the base path,
// then returns its contents as raw bytes. Respects context cancellation.
func (p *Protocol) WaitForManifest(ctx context.Context) (json.RawMessage, error) {
	manifestPath := filepath.Join(p.basePath, protocol.ManifestFilename)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		data, err := p.fs.ReadFile(manifestPath)
		if err == nil {
			return json.RawMessage(data), nil
		}
		time.Sleep(p.manifestPollInterval)
	}
}

// WriteStatus atomically writes the agent status to status.json.
// Errors are logged but not returned — status writes are best-effort.
func (p *Protocol) WriteStatus(status protocol.AgentStatus) {
	data, err := json.Marshal(status)
	if err != nil {
		p.logger.Error("Failed to marshal status", "error", err)
		return
	}
	if err := p.fs.MkdirAll(p.basePath, 0755); err != nil {
		p.logger.Warn("Failed to create basePath dir", "error", err)
		return
	}
	tmpPath := filepath.Join(p.basePath, protocol.StatusFilename+".tmp")
	if err := p.fs.WriteFile(tmpPath, data, 0644); err != nil {
		p.logger.Warn("Failed to write status tmp", "error", err)
		return
	}
	if err := p.fs.Rename(tmpPath, filepath.Join(p.basePath, protocol.StatusFilename)); err != nil {
		p.logger.Warn("Failed to rename status", "error", err)
	}
}

// WriteResult atomically writes the agent result to result.json.
// Returns error because result writes are critical (lost result = lost work).
func (p *Protocol) WriteResult(result json.RawMessage) error {
	if err := p.fs.MkdirAll(p.basePath, 0755); err != nil {
		return fmt.Errorf("create basePath: %w", err)
	}
	tmpPath := filepath.Join(p.basePath, protocol.ResultFilename+".tmp")
	if err := p.fs.WriteFile(tmpPath, result, 0644); err != nil {
		return fmt.Errorf("write result tmp: %w", err)
	}
	if err := p.fs.Rename(tmpPath, filepath.Join(p.basePath, protocol.ResultFilename)); err != nil {
		return fmt.Errorf("rename result: %w", err)
	}
	return nil
}

// WaitForSteering blocks until steering.json appears, claims it atomically
// via rename to steering.json.processing, reads it, removes it, and returns
// the parsed instruction. Skips invalid JSON and continues polling.
func (p *Protocol) WaitForSteering(ctx context.Context) (*protocol.SteeringInstruction, error) {
	steeringPath := filepath.Join(p.basePath, protocol.SteeringFilename)
	processingPath := steeringPath + ".processing"

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Atomic claim via rename — prevents TOCTOU races if multiple readers exist
		if err := p.fs.Rename(steeringPath, processingPath); err != nil {
			time.Sleep(p.steeringPollInterval)
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
```

**Step 4: Run tests — expect pass**

```bash
go test ./agent/... -v
```

Expected: all tests PASS

**Step 5: Commit**

```bash
git add agent/protocol.go agent/protocol_test.go
git commit -m "feat: add agent Protocol struct with WaitForManifest/WriteStatus/WriteResult/WaitForSteering"
```

---

## Task 8: `temporalkit` package ✅ Complete

**Files:**
- Create: `temporalkit/agent_activities.go`
- Create: `temporalkit/sandbox_activities.go`
- Create: `temporalkit/agent_activities_test.go`

**Step 1: Write the failing test**

Create `temporalkit/agent_activities_test.go`:

```go
package temporalkit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/tinkerloft/agentbox/protocol"
	"github.com/tinkerloft/agentbox/sandbox"
)

// mockAgentProvider implements sandbox.AgentProvider for testing.
type mockAgentProvider struct {
	submitManifestFn func(ctx context.Context, id string, manifest []byte) error
	pollStatusFn     func(ctx context.Context, id string) ([]byte, error)
	readResultFn     func(ctx context.Context, id string) ([]byte, error)
	submitSteeringFn func(ctx context.Context, id string, instruction []byte) error
	statusFn         func(ctx context.Context, id string) (*sandbox.SandboxStatus, error)
}

func (m *mockAgentProvider) Name() string { return "mock" }
func (m *mockAgentProvider) Provision(_ context.Context, _ sandbox.ProvisionOptions) (*sandbox.Sandbox, error) {
	return nil, errors.New("not implemented")
}
func (m *mockAgentProvider) Exec(_ context.Context, _ string, _ sandbox.ExecCommand) (*sandbox.ExecResult, error) {
	return nil, errors.New("not implemented")
}
func (m *mockAgentProvider) ExecShell(_ context.Context, _ string, _ string, _ string) (*sandbox.ExecResult, error) {
	return nil, errors.New("not implemented")
}
func (m *mockAgentProvider) CopyTo(_ context.Context, _ string, _ io.Reader, _ string) error {
	return errors.New("not implemented")
}
func (m *mockAgentProvider) CopyFrom(_ context.Context, _ string, _ string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}
func (m *mockAgentProvider) Cleanup(_ context.Context, _ string) error { return nil }
func (m *mockAgentProvider) SubmitManifest(ctx context.Context, id string, manifest []byte) error {
	return m.submitManifestFn(ctx, id, manifest)
}
func (m *mockAgentProvider) PollStatus(ctx context.Context, id string) ([]byte, error) {
	return m.pollStatusFn(ctx, id)
}
func (m *mockAgentProvider) ReadResult(ctx context.Context, id string) ([]byte, error) {
	return m.readResultFn(ctx, id)
}
func (m *mockAgentProvider) SubmitSteering(ctx context.Context, id string, instruction []byte) error {
	return m.submitSteeringFn(ctx, id, instruction)
}
func (m *mockAgentProvider) Status(ctx context.Context, id string) (*sandbox.SandboxStatus, error) {
	if m.statusFn != nil {
		return m.statusFn(ctx, id)
	}
	return &sandbox.SandboxStatus{Phase: sandbox.SandboxPhaseRunning}, nil
}

func TestSubmitManifest_PassesBytesToProvider(t *testing.T) {
	var capturedID string
	var capturedManifest []byte
	mock := &mockAgentProvider{
		submitManifestFn: func(_ context.Context, id string, manifest []byte) error {
			capturedID = id
			capturedManifest = manifest
			return nil
		},
	}
	acts := NewAgentActivities(mock)

	testEnv := &testsuite.WorkflowTestSuite{}
	env := testEnv.NewTestActivityEnvironment()
	env.RegisterActivity(acts)

	input := SubmitManifestInput{SandboxID: "sb-1", Manifest: []byte(`{"task_id":"t1"}`)}
	_, err := env.ExecuteActivity(acts.SubmitManifest, input)
	require.NoError(t, err)
	assert.Equal(t, "sb-1", capturedID)
	assert.Equal(t, []byte(`{"task_id":"t1"}`), capturedManifest)
}

func TestWaitForPhase_ReturnsOnTargetPhase(t *testing.T) {
	callCount := 0
	mock := &mockAgentProvider{
		pollStatusFn: func(_ context.Context, _ string) ([]byte, error) {
			callCount++
			phase := protocol.PhaseExecuting
			if callCount >= 3 {
				phase = protocol.PhaseAwaitingInput
			}
			data, _ := json.Marshal(protocol.AgentStatus{Phase: phase, UpdatedAt: time.Now()})
			return data, nil
		},
	}
	acts := NewAgentActivities(mock)

	testEnv := &testsuite.WorkflowTestSuite{}
	env := testEnv.NewTestActivityEnvironment()
	env.RegisterActivity(acts)

	input := WaitForPhaseInput{SandboxID: "sb-1", TargetPhases: []string{"awaiting_input"}}
	val, err := env.ExecuteActivity(acts.WaitForPhase, input)
	require.NoError(t, err)

	var status protocol.AgentStatus
	require.NoError(t, val.Get(&status))
	assert.Equal(t, protocol.PhaseAwaitingInput, status.Phase)
}

func TestWaitForPhase_AlwaysTargetsFailedAndCancelled(t *testing.T) {
	// Even if TargetPhases is empty, failed/cancelled should terminate
	mock := &mockAgentProvider{
		pollStatusFn: func(_ context.Context, _ string) ([]byte, error) {
			data, _ := json.Marshal(protocol.AgentStatus{Phase: protocol.PhaseFailed, UpdatedAt: time.Now()})
			return data, nil
		},
	}
	acts := NewAgentActivities(mock)

	testEnv := &testsuite.WorkflowTestSuite{}
	env := testEnv.NewTestActivityEnvironment()
	env.RegisterActivity(acts)

	input := WaitForPhaseInput{SandboxID: "sb-1", TargetPhases: []string{"complete"}}
	val, err := env.ExecuteActivity(acts.WaitForPhase, input)
	require.NoError(t, err)

	var status protocol.AgentStatus
	require.NoError(t, val.Get(&status))
	assert.Equal(t, protocol.PhaseFailed, status.Phase)
}

func TestWaitForPhase_StaleAgentDeadContainer_ReturnsError(t *testing.T) {
	staleTime := time.Now().Add(-(agentStaleThreshold + time.Minute))
	mock := &mockAgentProvider{
		pollStatusFn: func(_ context.Context, _ string) ([]byte, error) {
			data, _ := json.Marshal(protocol.AgentStatus{
				Phase:     protocol.PhaseExecuting,
				UpdatedAt: staleTime,
			})
			return data, nil
		},
		statusFn: func(_ context.Context, _ string) (*sandbox.SandboxStatus, error) {
			return &sandbox.SandboxStatus{Phase: sandbox.SandboxPhaseFailed, Message: "container died"}, nil
		},
	}
	acts := NewAgentActivities(mock)

	testEnv := &testsuite.WorkflowTestSuite{}
	env := testEnv.NewTestActivityEnvironment()
	env.RegisterActivity(acts)

	input := WaitForPhaseInput{SandboxID: "sb-1", TargetPhases: []string{"complete"}}
	_, err := env.ExecuteActivity(acts.WaitForPhase, input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "agent stale")
}

func TestWaitForPhase_StaleAgentLiveContainer_Continues(t *testing.T) {
	staleTime := time.Now().Add(-(agentStaleThreshold + time.Minute))
	callCount := 0
	mock := &mockAgentProvider{
		pollStatusFn: func(_ context.Context, _ string) ([]byte, error) {
			callCount++
			phase := protocol.PhaseExecuting
			updatedAt := staleTime
			if callCount >= 5 {
				// Eventually complete
				phase = protocol.PhaseComplete
				updatedAt = time.Now()
			}
			data, _ := json.Marshal(protocol.AgentStatus{Phase: phase, UpdatedAt: updatedAt})
			return data, nil
		},
		statusFn: func(_ context.Context, _ string) (*sandbox.SandboxStatus, error) {
			// Container is still running — clock skew scenario
			return &sandbox.SandboxStatus{Phase: sandbox.SandboxPhaseRunning}, nil
		},
	}
	acts := NewAgentActivities(mock)

	testEnv := &testsuite.WorkflowTestSuite{}
	env := testEnv.NewTestActivityEnvironment()
	env.RegisterActivity(acts)

	input := WaitForPhaseInput{SandboxID: "sb-1", TargetPhases: []string{"complete"}}
	val, err := env.ExecuteActivity(acts.WaitForPhase, input)
	require.NoError(t, err)

	var status protocol.AgentStatus
	require.NoError(t, val.Get(&status))
	assert.Equal(t, protocol.PhaseComplete, status.Phase)
}

func TestReadResult_ReturnsBytesFromProvider(t *testing.T) {
	resultBytes := []byte(`{"status":"complete","repositories":[]}`)
	mock := &mockAgentProvider{
		readResultFn: func(_ context.Context, id string) ([]byte, error) {
			assert.Equal(t, "sb-1", id)
			return resultBytes, nil
		},
	}
	acts := NewAgentActivities(mock)

	testEnv := &testsuite.WorkflowTestSuite{}
	env := testEnv.NewTestActivityEnvironment()
	env.RegisterActivity(acts)

	val, err := env.ExecuteActivity(acts.ReadResult, ReadResultInput{SandboxID: "sb-1"})
	require.NoError(t, err)

	var got []byte
	require.NoError(t, val.Get(&got))
	assert.Equal(t, resultBytes, got)
}

func TestSubmitSteering_PassesBytesToProvider(t *testing.T) {
	var capturedBytes []byte
	mock := &mockAgentProvider{
		submitSteeringFn: func(_ context.Context, _ string, instruction []byte) error {
			capturedBytes = instruction
			return nil
		},
	}
	acts := NewAgentActivities(mock)

	testEnv := &testsuite.WorkflowTestSuite{}
	env := testEnv.NewTestActivityEnvironment()
	env.RegisterActivity(acts)

	instructionBytes := []byte(`{"action":"approve","iteration":1}`)
	input := SubmitSteeringInput{SandboxID: "sb-1", Instruction: instructionBytes}
	_, err := env.ExecuteActivity(acts.SubmitSteering, input)
	require.NoError(t, err)
	assert.Equal(t, instructionBytes, capturedBytes)
}

// Ensure activity.RecordHeartbeat compiles (it's a no-op in test env but must import correctly)
var _ = activity.RecordHeartbeat
```

**Step 2: Run test — expect failure**

```bash
cd /Users/andrew/dev/code/projects/agentbox
go test ./temporalkit/...
```

Expected: `cannot find package "github.com/tinkerloft/agentbox/temporalkit"`

**Step 3a: Implement `temporalkit/agent_activities.go`**

```go
// Package temporalkit provides Temporal activity helpers for agentbox sandbox + agent operations.
package temporalkit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/tinkerloft/agentbox/protocol"
	"github.com/tinkerloft/agentbox/sandbox"
)

const agentStaleThreshold = 5 * time.Minute

// AgentActivities contains Temporal activities for the file-based agent protocol.
// All manifest/result/steering payloads are raw bytes — callers own serialization.
type AgentActivities struct {
	provider sandbox.AgentProvider
}

// NewAgentActivities creates AgentActivities backed by the given provider.
func NewAgentActivities(provider sandbox.AgentProvider) *AgentActivities {
	return &AgentActivities{provider: provider}
}

// SubmitManifestInput is the input for SubmitManifest.
type SubmitManifestInput struct {
	SandboxID string `json:"sandbox_id"`
	Manifest  []byte `json:"manifest"`
}

// SubmitManifest writes manifest bytes to the sandbox.
func (a *AgentActivities) SubmitManifest(ctx context.Context, input SubmitManifestInput) error {
	return a.provider.SubmitManifest(ctx, input.SandboxID, input.Manifest)
}

// WaitForPhaseInput is the input for WaitForPhase.
type WaitForPhaseInput struct {
	SandboxID    string   `json:"sandbox_id"`
	TargetPhases []string `json:"target_phases"` // e.g. ["awaiting_input", "complete"]
}

// WaitForPhase polls the agent status until it reaches one of the target phases.
// Always terminates on PhaseFailed or PhaseCancelled regardless of TargetPhases.
// Uses Temporal heartbeats for long-running polling support.
// Detects stale agents (no status update > 5min) and validates container is still alive.
func (a *AgentActivities) WaitForPhase(ctx context.Context, input WaitForPhaseInput) (*protocol.AgentStatus, error) {
	targetSet := make(map[protocol.Phase]bool)
	for _, p := range input.TargetPhases {
		targetSet[protocol.Phase(p)] = true
	}
	// Always terminate on terminal phases
	targetSet[protocol.PhaseFailed] = true
	targetSet[protocol.PhaseCancelled] = true

	for {
		activity.RecordHeartbeat(ctx, fmt.Sprintf("polling agent phases: %s", strings.Join(input.TargetPhases, "|")))

		raw, err := a.provider.PollStatus(ctx, input.SandboxID)
		if err != nil {
			return nil, fmt.Errorf("poll status: %w", err)
		}

		var status protocol.AgentStatus
		if err := json.Unmarshal(raw, &status); err != nil {
			return nil, fmt.Errorf("parse status: %w", err)
		}

		if targetSet[status.Phase] {
			return &status, nil
		}

		// Staleness detection: agent hasn't written a status update in a while.
		// Double-check container health before declaring stale — avoids false positives
		// from clock skew or slow init.
		if !status.UpdatedAt.IsZero() && time.Since(status.UpdatedAt) > agentStaleThreshold {
			containerStatus, err := a.provider.Status(ctx, input.SandboxID)
			if err == nil && containerStatus.Phase != sandbox.SandboxPhaseRunning {
				return nil, fmt.Errorf("agent stale: last update %s, phase %s, container %s",
					status.UpdatedAt.Format(time.RFC3339), status.Phase, containerStatus.Phase)
			}
			// Container still running — likely clock skew, continue polling
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// ReadResultInput is the input for ReadResult.
type ReadResultInput struct {
	SandboxID string `json:"sandbox_id"`
}

// ReadResult reads the agent result as raw bytes.
// Callers unmarshal into their own domain result types.
func (a *AgentActivities) ReadResult(ctx context.Context, input ReadResultInput) ([]byte, error) {
	data, err := a.provider.ReadResult(ctx, input.SandboxID)
	if err != nil {
		return nil, fmt.Errorf("read result: %w", err)
	}
	return data, nil
}

// SubmitSteeringInput is the input for SubmitSteering.
type SubmitSteeringInput struct {
	SandboxID   string `json:"sandbox_id"`
	Instruction []byte `json:"instruction"` // JSON-encoded protocol.SteeringInstruction
}

// SubmitSteering writes a steering instruction to the sandbox.
func (a *AgentActivities) SubmitSteering(ctx context.Context, input SubmitSteeringInput) error {
	return a.provider.SubmitSteering(ctx, input.SandboxID, input.Instruction)
}
```

**Step 3b: Implement `temporalkit/sandbox_activities.go`**

```go
package temporalkit

import (
	"context"
	"fmt"

	"github.com/tinkerloft/agentbox/sandbox"
)

// SandboxActivities contains Temporal activities for sandbox container lifecycle.
type SandboxActivities struct {
	provider sandbox.Provider
}

// NewSandboxActivities creates SandboxActivities backed by the given provider.
func NewSandboxActivities(provider sandbox.Provider) *SandboxActivities {
	return &SandboxActivities{provider: provider}
}

// ProvisionInput is the input for Provision.
type ProvisionInput struct {
	TaskID        string                  `json:"task_id"`
	Image         string                  `json:"image"`
	Cmd           []string                `json:"cmd,omitempty"`
	Env           map[string]string       `json:"env"`
	Resources     sandbox.ResourceLimits  `json:"resources"`
	Volumes       []sandbox.VolumeMount   `json:"volumes,omitempty"`
	BasePath      string                  `json:"base_path,omitempty"`
	WorkingDir    string                  `json:"working_dir,omitempty"`
	RuntimeClass  string                  `json:"runtime_class,omitempty"`
	NodeSelector  map[string]string       `json:"node_selector,omitempty"`
	UserNamespace bool                    `json:"user_namespace,omitempty"`
	OwnerRef      *sandbox.OwnerReference `json:"owner_ref,omitempty"`
}

// SandboxInfo is the output of Provision.
type SandboxInfo struct {
	ContainerID   string `json:"container_id"`
	WorkspacePath string `json:"workspace_path"`
}

// Provision creates a new sandbox container.
func (a *SandboxActivities) Provision(ctx context.Context, input ProvisionInput) (*SandboxInfo, error) {
	sb, err := a.provider.Provision(ctx, sandbox.ProvisionOptions{
		TaskID:        input.TaskID,
		Image:         input.Image,
		Cmd:           input.Cmd,
		Env:           input.Env,
		Resources:     input.Resources,
		Volumes:       input.Volumes,
		BasePath:      input.BasePath,
		WorkingDir:    input.WorkingDir,
		RuntimeClass:  input.RuntimeClass,
		NodeSelector:  input.NodeSelector,
		UserNamespace: input.UserNamespace,
		OwnerRef:      input.OwnerRef,
	})
	if err != nil {
		return nil, fmt.Errorf("provision sandbox: %w", err)
	}
	return &SandboxInfo{ContainerID: sb.ID, WorkspacePath: sb.WorkingDir}, nil
}

// Cleanup removes a sandbox container.
func (a *SandboxActivities) Cleanup(ctx context.Context, containerID string) error {
	return a.provider.Cleanup(ctx, containerID)
}
```

**Step 4: Run tests — expect pass**

```bash
go test ./temporalkit/... -v
go build ./...
go test ./...
```

Expected: all tests PASS

**Step 5: Commit**

```bash
git add temporalkit/
git commit -m "feat: add temporalkit with AgentActivities and SandboxActivities ([]byte interfaces)"
```

---

## PHASE 3 — fleetlift repo (branch: feat/agentbox-split)

All commands in this phase use: `cd /Users/andrew/dev/code/projects/fleetlift`

---

## Task 9: Add agentbox dependency to fleetlift ✅ Complete

**Files:**
- Modify: `go.mod`

**Step 1: Add local replace directive**

Since agentbox isn't published to GitHub yet, use a `replace` directive pointing to the local path:

```bash
cd /Users/andrew/dev/code/projects/fleetlift
go mod edit -replace github.com/tinkerloft/agentbox=../agentbox
go get github.com/tinkerloft/agentbox@v0.0.0
go mod tidy
```

**Step 2: Verify build still works**

```bash
go build ./...
```

Expected: builds cleanly (no agentbox imports yet, so no change)

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "feat: add agentbox dependency (local replace directive)"
```

---

## Task 10: Create `internal/agent/fleetproto` package + protocol shim ✅ Complete

This is the most important structural task. It creates a clear boundary between generic (agentbox) and fleetlift-specific types, with zero breakage of existing code.

**Files:**
- Create: `internal/agent/fleetproto/types.go`
- Modify: `internal/agent/protocol/types.go` (replace content with shim)
- Create: `internal/agent/protocol/types_test.go` (keep existing tests passing)

**Step 1: Create `internal/agent/fleetproto/types.go`**

Move all fleetlift-specific types here. These are the types that agentbox explicitly does NOT own:

```go
// Package fleetproto contains fleetlift-specific agent protocol types.
// Generic types (Phase, AgentStatus, SteeringInstruction, etc.) live in
// github.com/tinkerloft/agentbox/protocol and are re-exported via
// internal/agent/protocol for backward compatibility.
package fleetproto

import (
	"path/filepath"
	"time"

	agentboxproto "github.com/tinkerloft/agentbox/protocol"
)

// MaxDiffLinesPerFile is the default truncation limit for per-file diffs.
const MaxDiffLinesPerFile = 1000

// WorkspacePath is the fleetlift workspace root inside the sandbox.
const WorkspacePath = "/workspace"

// TaskManifest is the full task definition written by the worker for the fleetlift agent.
type TaskManifest struct {
	TaskID                string             `json:"task_id"`
	Mode                  string             `json:"mode"` // "transform" or "report"
	Title                 string             `json:"title"`
	Repositories          []ManifestRepo     `json:"repositories"`
	Transformation        *ManifestRepo      `json:"transformation,omitempty"`
	Targets               []ManifestRepo     `json:"targets,omitempty"`
	ForEach               []ForEachTarget    `json:"for_each,omitempty"`
	Execution             ManifestExecution  `json:"execution"`
	Verifiers             []ManifestVerifier `json:"verifiers,omitempty"`
	TimeoutSeconds        int                `json:"timeout_seconds"`
	RequireApproval       bool               `json:"require_approval"`
	MaxSteeringIterations int                `json:"max_steering_iterations"`
	PullRequest           *ManifestPRConfig  `json:"pull_request,omitempty"`
	GitConfig             ManifestGitConfig  `json:"git_config"`
}

// EffectiveRepos returns the repos to operate on.
func (m *TaskManifest) EffectiveRepos() []ManifestRepo {
	if m.Transformation != nil && len(m.Targets) > 0 {
		return m.Targets
	}
	return m.Repositories
}

// RepoBasePath returns the base path where repos are cloned.
func (m *TaskManifest) RepoBasePath() string {
	if m.Transformation != nil {
		return filepath.Join(WorkspacePath, "targets")
	}
	return WorkspacePath
}

// RepoPath returns the full path for a named repository.
func (m *TaskManifest) RepoPath(repoName string) string {
	return filepath.Join(m.RepoBasePath(), repoName)
}

// ManifestRepo defines a repository to clone.
type ManifestRepo struct {
	URL    string   `json:"url"`
	Branch string   `json:"branch,omitempty"`
	Name   string   `json:"name,omitempty"`
	Setup  []string `json:"setup,omitempty"`
}

// ForEachTarget is a named iteration target within a repository.
type ForEachTarget struct {
	Name    string `json:"name"`
	Context string `json:"context"`
}

// ManifestExecution specifies what to run.
type ManifestExecution struct {
	Type    string            `json:"type"` // "agentic" or "deterministic"
	Prompt  string            `json:"prompt,omitempty"`
	Image   string            `json:"image,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Command []string          `json:"command,omitempty"`
}

// ManifestVerifier defines a verification command.
type ManifestVerifier struct {
	Name    string   `json:"name"`
	Command []string `json:"command"`
}

// ManifestPRConfig contains PR creation settings.
type ManifestPRConfig struct {
	BranchPrefix string   `json:"branch_prefix,omitempty"`
	Title        string   `json:"title,omitempty"`
	Body         string   `json:"body,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Reviewers    []string `json:"reviewers,omitempty"`
}

// ManifestGitConfig contains git identity settings.
type ManifestGitConfig struct {
	UserEmail  string `json:"user_email"`
	UserName   string `json:"user_name"`
	CloneDepth int    `json:"clone_depth,omitempty"`
}

// AgentResult is the full structured result written by the fleetlift agent.
type AgentResult struct {
	Status          agentboxproto.Phase `json:"status"`
	Repositories    []RepoResult        `json:"repositories"`
	AgentOutput     string              `json:"agent_output,omitempty"`
	SteeringHistory []SteeringRecord    `json:"steering_history,omitempty"`
	Error           *string             `json:"error,omitempty"`
	StartedAt       time.Time           `json:"started_at"`
	CompletedAt     *time.Time          `json:"completed_at,omitempty"`
}

// RepoResult contains the result for a single repository.
type RepoResult struct {
	Name            string           `json:"name"`
	Status          string           `json:"status"`
	FilesModified   []string         `json:"files_modified,omitempty"`
	Diffs           []DiffEntry      `json:"diffs,omitempty"`
	VerifierResults []VerifierResult `json:"verifier_results,omitempty"`
	Report          *ReportResult    `json:"report,omitempty"`
	ForEachResults  []ForEachResult  `json:"for_each_results,omitempty"`
	PullRequest     *PRInfo          `json:"pull_request,omitempty"`
	Error           *string          `json:"error,omitempty"`
}

// DiffEntry represents a single file's diff.
type DiffEntry struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Diff      string `json:"diff"`
}

// VerifierResult is the result of running a single verifier.
type VerifierResult struct {
	Name     string `json:"name"`
	Success  bool   `json:"success"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output,omitempty"`
}

// ReportResult contains report-mode output for a repo.
type ReportResult struct {
	Frontmatter      map[string]any `json:"frontmatter,omitempty"`
	Body             string         `json:"body,omitempty"`
	Raw              string         `json:"raw"`
	ValidationErrors []string       `json:"validation_errors,omitempty"`
}

// ForEachResult contains the result for a single forEach target.
type ForEachResult struct {
	Target ForEachTarget `json:"target"`
	Report *ReportResult `json:"report,omitempty"`
	Error  *string       `json:"error,omitempty"`
}

// PRInfo contains information about a created pull request.
type PRInfo struct {
	URL        string `json:"url"`
	Number     int    `json:"number"`
	BranchName string `json:"branch_name"`
	Title      string `json:"title"`
}

// SteeringRecord records a single steering interaction (fleetlift-specific — includes Timestamp).
type SteeringRecord struct {
	Iteration int       `json:"iteration"`
	Prompt    string    `json:"prompt"`
	Timestamp time.Time `json:"timestamp"`
}
```

**Step 2: Replace `internal/agent/protocol/types.go` with a shim**

This shim re-exports everything from agentbox and fleetproto so that ALL existing importers of `internal/agent/protocol` continue to compile without any changes:

```go
// Package protocol re-exports agentbox generic types and fleetlift-specific types.
// Existing importers of this package need no changes during the migration.
//
// Deprecated: New code should import github.com/tinkerloft/agentbox/protocol directly
// for generic types, and internal/agent/fleetproto for fleetlift-specific types.
package protocol

import (
	"path/filepath"

	agentboxproto "github.com/tinkerloft/agentbox/protocol"
	"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
)

// --- Generic types re-exported from agentbox/protocol ---

type Phase = agentboxproto.Phase
type AgentStatus = agentboxproto.AgentStatus
type StatusProgress = agentboxproto.StatusProgress
type SteeringAction = agentboxproto.SteeringAction
type SteeringInstruction = agentboxproto.SteeringInstruction

const (
	PhaseInitializing  = agentboxproto.PhaseInitializing
	PhaseExecuting     = agentboxproto.PhaseExecuting
	PhaseVerifying     = agentboxproto.PhaseVerifying
	PhaseAwaitingInput = agentboxproto.PhaseAwaitingInput
	PhaseComplete      = agentboxproto.PhaseComplete
	PhaseFailed        = agentboxproto.PhaseFailed
	PhaseCancelled     = agentboxproto.PhaseCancelled

	// PhaseCreatingPRs is fleetlift-specific (not in agentbox).
	PhaseCreatingPRs Phase = "creating_prs"

	SteeringActionSteer   = agentboxproto.SteeringActionSteer
	SteeringActionApprove = agentboxproto.SteeringActionApprove
	SteeringActionReject  = agentboxproto.SteeringActionReject
	SteeringActionCancel  = agentboxproto.SteeringActionCancel

	DefaultBasePath  = agentboxproto.DefaultBasePath
	ManifestFilename = agentboxproto.ManifestFilename
	StatusFilename   = agentboxproto.StatusFilename
	ResultFilename   = agentboxproto.ResultFilename
	SteeringFilename = agentboxproto.SteeringFilename

	// BasePath kept for backward compatibility with existing code.
	BasePath = agentboxproto.DefaultBasePath

	// WorkspacePath is the fleetlift workspace root.
	WorkspacePath = fleetproto.WorkspacePath
)

// Path helpers — re-exported as functions (previously were constants).
func ManifestPath(base ...string) string {
	b := agentboxproto.DefaultBasePath
	if len(base) > 0 && base[0] != "" {
		b = base[0]
	}
	return agentboxproto.ManifestPath(b)
}
func StatusPath(base ...string) string {
	b := agentboxproto.DefaultBasePath
	if len(base) > 0 && base[0] != "" {
		b = base[0]
	}
	return agentboxproto.StatusPath(b)
}
func ResultPath(base ...string) string {
	b := agentboxproto.DefaultBasePath
	if len(base) > 0 && base[0] != "" {
		b = base[0]
	}
	return agentboxproto.ResultPath(b)
}
func SteeringPath(base ...string) string {
	b := agentboxproto.DefaultBasePath
	if len(base) > 0 && base[0] != "" {
		b = base[0]
	}
	return agentboxproto.SteeringPath(b)
}

// --- Fleetlift-specific types re-exported from fleetproto ---

type TaskManifest = fleetproto.TaskManifest
type AgentResult = fleetproto.AgentResult
type RepoResult = fleetproto.RepoResult
type DiffEntry = fleetproto.DiffEntry
type VerifierResult = fleetproto.VerifierResult
type ReportResult = fleetproto.ReportResult
type ForEachTarget = fleetproto.ForEachTarget
type ForEachResult = fleetproto.ForEachResult
type PRInfo = fleetproto.PRInfo
type SteeringRecord = fleetproto.SteeringRecord
type ManifestRepo = fleetproto.ManifestRepo
type ManifestExecution = fleetproto.ManifestExecution
type ManifestVerifier = fleetproto.ManifestVerifier
type ManifestPRConfig = fleetproto.ManifestPRConfig
type ManifestGitConfig = fleetproto.ManifestGitConfig

const MaxDiffLinesPerFile = fleetproto.MaxDiffLinesPerFile

// _ ensures filepath is used (for the Rename call in the shim functions).
var _ = filepath.Join
```

> **Note on ManifestPath/StatusPath/etc.:** The old code used these as string constants (e.g., `protocol.ManifestPath`). The new shim makes them functions. Search all usages:
> ```bash
> grep -rn "protocol\.ManifestPath\|protocol\.StatusPath\|protocol\.ResultPath\|protocol\.SteeringPath" --include="*.go" .
> ```
> For each usage of the old constant form (e.g., `protocol.ManifestPath`), if it was used as a string constant value, it now becomes a function call `protocol.ManifestPath()`. Update each caller.

**Step 3: Build to verify zero breakage**

```bash
go build ./...
```

Expected: builds cleanly — all existing importers of `internal/agent/protocol` still compile via the shim.

**Step 4: Commit**

```bash
git add internal/agent/fleetproto/ internal/agent/protocol/types.go
git commit -m "feat: create fleetproto package + protocol shim re-exporting agentbox types"
```

---

## Task 11: Delete `internal/sandbox/`, update imports ✅ Complete

**Files:**
- Delete: `internal/sandbox/` (entire directory)
- Modify: all files importing `internal/sandbox`

**Step 1: Find all importers**

```bash
grep -rn "tinkerloft/fleetlift/internal/sandbox" --include="*.go" -l
```

Expected output (approximately):
```
internal/activity/agent.go
internal/activity/sandbox.go
cmd/worker/main.go
```

Also check for docker/k8s sub-package imports:
```bash
grep -rn "internal/sandbox/docker\|internal/sandbox/k8s" --include="*.go" -l
```

**Step 2: Update `internal/activity/agent.go`**

Change import:
```go
// Before:
"github.com/tinkerloft/fleetlift/internal/sandbox"

// After:
"github.com/tinkerloft/agentbox/sandbox"
```

No other changes needed — all types (`sandbox.AgentProvider`, `sandbox.SandboxPhaseRunning`) are the same.

**Step 3: Update `internal/activity/sandbox.go`**

Change import:
```go
// Before:
"github.com/tinkerloft/fleetlift/internal/sandbox"

// After:
"github.com/tinkerloft/agentbox/sandbox"
```

Also update the `ProvisionAgentSandbox` activity — replace `UseAgentMode: true` with `Cmd`:
```go
// Before:
sb, err := a.Provider.Provision(ctx, sandbox.ProvisionOptions{
    TaskID:       input.TaskID,
    Image:        image,
    UseAgentMode: true,
    WorkingDir:   WorkspacePath,
    ...
})

// After:
sb, err := a.Provider.Provision(ctx, sandbox.ProvisionOptions{
    TaskID:     input.TaskID,
    Image:      image,
    Cmd:        []string{"/agent-bin/agent", "serve"},
    WorkingDir: WorkspacePath,
    ...
})
```

**Step 4: Update `cmd/worker/main.go`**

Change blank imports:
```go
// Before:
_ "github.com/tinkerloft/fleetlift/internal/sandbox/docker"
_ "github.com/tinkerloft/fleetlift/internal/sandbox/k8s"

// After:
_ "github.com/tinkerloft/agentbox/sandbox/docker"
_ "github.com/tinkerloft/agentbox/sandbox/k8s"
```

Also update the `sandbox.NewProvider(...)` call — the type is now `agentbox/sandbox.AgentProvider`. The import path changes but the API is identical.

**Step 5: Delete internal/sandbox/**

```bash
rm -rf internal/sandbox/
```

**Step 6: Build**

```bash
go build ./...
```

Fix any remaining import errors.

**Step 7: Run tests**

```bash
go test ./...
```

Expected: all tests pass (sandbox tests are now in agentbox repo)

**Step 8: Commit**

```bash
git add -A
git commit -m "feat: replace internal/sandbox with agentbox/sandbox imports"
```

---

## Task 12: Update `internal/activity/agent.go` for `[]byte` PollStatus ✅ Complete

The `AgentProvider.PollStatus` now returns `([]byte, error)` instead of `(*protocol.AgentStatus, error)`. Update `WaitForAgentPhase` to unmarshal the bytes.

**Files:**
- Modify: `internal/activity/agent.go`

**Step 1: Identify the change**

In `WaitForAgentPhase`, the current code is:
```go
status, err := a.provider.PollStatus(ctx, input.SandboxID)
if err != nil {
    return nil, fmt.Errorf("failed to poll status: %w", err)
}
if targetSet[status.Phase] {
    return status, nil
}
if !status.UpdatedAt.IsZero() && time.Since(status.UpdatedAt) > AgentStaleThreshold {
    ...
}
```

Change to:
```go
raw, err := a.provider.PollStatus(ctx, input.SandboxID)
if err != nil {
    return nil, fmt.Errorf("failed to poll status: %w", err)
}
var status protocol.AgentStatus
if err := json.Unmarshal(raw, &status); err != nil {
    return nil, fmt.Errorf("failed to parse status: %w", err)
}
if targetSet[status.Phase] {
    return &status, nil
}
if !status.UpdatedAt.IsZero() && time.Since(status.UpdatedAt) > AgentStaleThreshold {
    ...
}
```

Add `"encoding/json"` to imports if not already present.

**Step 2: Build**

```bash
go build ./internal/activity/...
```

**Step 3: Run activity tests**

```bash
go test ./internal/activity/... -v
```

Expected: PASS

**Step 4: Commit**

```bash
git add internal/activity/agent.go
git commit -m "fix: unmarshal []byte from agentbox PollStatus in WaitForAgentPhase"
```

---

## Task 13: Update `internal/agent/pipeline.go` to use `agentbox/agent.Protocol` ✅ Complete

Replace the inline `waitForManifest`, `writeStatus`, `writeResult`, `waitForSteering` methods with delegation to an `agentbox/agent.Protocol` instance.

**Files:**
- Modify: `internal/agent/pipeline.go`

**Step 1: Add `proto` field to Pipeline**

```go
import (
    agentpkg "github.com/tinkerloft/agentbox/agent"
    // ... existing imports
)

type Pipeline struct {
    basePath string
    fs       FileSystem
    exec     CommandExecutor
    logger   *slog.Logger
    proto    *agentpkg.Protocol  // ADD THIS
}
```

**Step 2: Initialize proto in NewPipeline**

```go
func NewPipeline(basePath string, fs FileSystem, exec CommandExecutor, logger *slog.Logger) *Pipeline {
    proto := agentpkg.NewProtocol(agentpkg.ProtocolConfig{BasePath: basePath}, fs, logger)
    return &Pipeline{basePath: basePath, fs: fs, exec: exec, logger: logger, proto: proto}
}
```

**Step 3: Replace waitForManifest**

```go
func (p *Pipeline) waitForManifest(ctx context.Context) (*protocol.TaskManifest, error) {
    raw, err := p.proto.WaitForManifest(ctx)
    if err != nil {
        return nil, err
    }
    var manifest protocol.TaskManifest
    if err := json.Unmarshal(raw, &manifest); err != nil {
        return nil, fmt.Errorf("invalid manifest: %w", err)
    }
    return &manifest, nil
}
```

**Step 4: Replace writeStatus**

```go
func (p *Pipeline) writeStatus(status protocol.AgentStatus) {
    p.proto.WriteStatus(status)
}
```

**Step 5: Replace writeResult**

```go
func (p *Pipeline) writeResult(result *protocol.AgentResult) error {
    data, err := json.Marshal(result)
    if err != nil {
        return fmt.Errorf("marshal result: %w", err)
    }
    return p.proto.WriteResult(data)
}
```

**Step 6: Replace waitForSteering**

```go
func (p *Pipeline) waitForSteering(ctx context.Context) (*protocol.SteeringInstruction, error) {
    return p.proto.WaitForSteering(ctx)
}
```

**Step 7: Build and test**

```bash
go build ./internal/agent/...
go test ./internal/agent/... -v
```

Expected: PASS

**Step 8: Commit**

```bash
git add internal/agent/pipeline.go
git commit -m "feat: pipeline delegates file-protocol I/O to agentbox/agent.Protocol"
```

---

## Task 14: Final verification 🔄 In progress (blocked on AB-3c legacy workflow migration)

**Step 1: Run linter**

```bash
cd /Users/andrew/dev/code/projects/fleetlift
make lint
```

Fix any lint issues.

**Step 2: Run all tests**

```bash
go test ./...
```

Expected: all tests PASS

**Step 3: Build all binaries**

```bash
go build ./...
```

Expected: all binaries build cleanly

**Step 4: Run agentbox tests too**

```bash
cd /Users/andrew/dev/code/projects/agentbox
go test ./...
go build ./...
```

Expected: all tests PASS

**Step 5: Final commit**

```bash
cd /Users/andrew/dev/code/projects/fleetlift
git add -A
git commit -m "chore: final cleanup after agentbox split — lint fixes and verification"
```

---

## Summary of All Changes

### agentbox repo (new files)
| File | What |
|------|------|
| `go.mod` | New module `github.com/tinkerloft/agentbox` |
| `protocol/protocol.go` | Generic phases, steering, file paths, Metadata |
| `sandbox/provider.go` | Provider/AgentProvider interfaces + value types |
| `sandbox/factory.go` | Registration registry |
| `sandbox/docker/provider.go` | Docker provider (Cmd replaces UseAgentMode, basePath cache) |
| `sandbox/docker/register.go` | init() self-registration |
| `sandbox/k8s/job.go` | K8s job builder (RuntimeClass, UserNamespace, OwnerRef, EphemeralStorage) |
| `sandbox/k8s/exec.go` | SPDY exec helpers |
| `sandbox/k8s/wait.go` | Pod readiness watcher |
| `sandbox/k8s/provider.go` | K8s provider ([]byte PollStatus, basePath cache) |
| `sandbox/k8s/register.go` | init() self-registration |
| `agent/deps.go` | FileSystem + CommandExecutor interfaces |
| `agent/constants.go` | Poll interval constants |
| `agent/protocol.go` | Protocol struct primitives |
| `temporalkit/agent_activities.go` | Temporal activities ([]byte interfaces) |
| `temporalkit/sandbox_activities.go` | Provision + Cleanup activities |

### fleetlift repo (branch: feat/agentbox-split)
| File | What |
|------|------|
| `go.mod` | Added agentbox dependency with local replace |
| `internal/agent/fleetproto/types.go` | New — all fleetlift-specific manifest/result types |
| `internal/agent/protocol/types.go` | Replaced with shim re-exporting agentbox + fleetproto |
| `internal/sandbox/` | **Deleted** |
| `internal/activity/agent.go` | Updated import + unmarshal []byte from PollStatus |
| `internal/activity/sandbox.go` | Updated import + Cmd instead of UseAgentMode |
| `internal/agent/pipeline.go` | Delegates file-I/O to agentbox/agent.Protocol |
| `cmd/worker/main.go` | Updated blank imports for docker/k8s registration |
