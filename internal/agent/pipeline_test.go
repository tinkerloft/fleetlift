package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
)

func TestWaitForManifest_Polling(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	manifest := &fleetproto.TaskManifest{
		TaskID: "task-1",
		Mode:   "transform",
		Repositories: []fleetproto.ManifestRepo{
			{Name: "svc"},
		},
	}
	data, _ := json.Marshal(manifest)

	// Write manifest after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = fs.WriteFile("/tmp/test-fleetlift/manifest.json", data, 0644)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	got, err := p.waitForManifest(ctx)
	require.NoError(t, err)
	assert.Equal(t, "task-1", got.TaskID)
}

func TestWaitForManifest_ContextCancelled(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediate cancel

	_, err := p.waitForManifest(ctx)
	assert.Error(t, err)
}

func TestWaitForSteering_AtomicRename(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	instruction := &fleetproto.SteeringInstruction{
		Action: fleetproto.SteeringActionApprove,
	}
	data, _ := json.Marshal(instruction)

	// Write steering file
	_ = fs.WriteFile("/tmp/test-fleetlift/steering.json", data, 0644)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	got, err := p.waitForSteering(ctx)
	require.NoError(t, err)
	assert.Equal(t, fleetproto.SteeringActionApprove, got.Action)

	// Verify the original file was removed (renamed then deleted)
	_, err = fs.ReadFile("/tmp/test-fleetlift/steering.json")
	assert.Error(t, err, "original steering file should be gone")
	_, err = fs.ReadFile("/tmp/test-fleetlift/steering.json.processing")
	assert.Error(t, err, "processing file should be cleaned up")
}

func TestWaitForSteering_RenameFailsNoFile(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	// Write steering file after a delay to test that rename failure is handled
	go func() {
		time.Sleep(50 * time.Millisecond)
		instruction := &fleetproto.SteeringInstruction{
			Action: fleetproto.SteeringActionSteer,
			Prompt: "fix tests",
		}
		data, _ := json.Marshal(instruction)
		_ = fs.WriteFile("/tmp/test-fleetlift/steering.json", data, 0644)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, err := p.waitForSteering(ctx)
	require.NoError(t, err)
	assert.Equal(t, fleetproto.SteeringActionSteer, got.Action)
	assert.Equal(t, "fix tests", got.Prompt)
}

func TestWriteStatus(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	p.writeStatus(fleetproto.AgentStatus{
		Phase:   fleetproto.PhaseExecuting,
		Message: "Running...",
	})

	data, err := fs.ReadFile("/tmp/test-fleetlift/status.json")
	require.NoError(t, err)

	var status fleetproto.AgentStatus
	require.NoError(t, json.Unmarshal(data, &status))
	assert.Equal(t, fleetproto.PhaseExecuting, status.Phase)
	assert.Equal(t, "Running...", status.Message)
}

func TestWriteResult_Success(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	result := &fleetproto.AgentResult{
		Status: fleetproto.PhaseComplete,
		Repositories: []fleetproto.RepoResult{
			{Name: "svc", Status: "success"},
		},
	}

	err := p.writeResult(result)
	require.NoError(t, err)

	data, err := fs.ReadFile("/tmp/test-fleetlift/result.json")
	require.NoError(t, err)

	var decoded fleetproto.AgentResult
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, fleetproto.PhaseComplete, decoded.Status)
}

func TestWriteResult_Error(t *testing.T) {
	fs := newMockFS()
	fs.writeFileFunc = func(path string, data []byte, perm os.FileMode) error {
		return fmt.Errorf("disk full")
	}
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	err := p.writeResult(&fleetproto.AgentResult{Status: fleetproto.PhaseComplete})
	assert.ErrorContains(t, err, "disk full")
}

func TestExecute_HappyPath(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	exec.runFunc = func(ctx context.Context, opts CommandOpts) (*CommandResult, error) {
		return &CommandResult{Stdout: "", ExitCode: 0}, nil
	}
	p := testPipeline(fs, exec)

	manifest := &fleetproto.TaskManifest{
		TaskID: "task-1",
		Mode:   "transform",
		Title:  "Fix bug",
		Repositories: []fleetproto.ManifestRepo{
			{Name: "svc", URL: "https://github.com/org/svc.git"},
		},
		Execution: fleetproto.ManifestExecution{
			Type:   "agentic",
			Prompt: "Fix it",
		},
		GitConfig: fleetproto.ManifestGitConfig{
			UserEmail: "bot@test.com",
			UserName:  "Bot",
		},
	}

	t.Setenv("GITHUB_TOKEN", "")

	err := p.Execute(context.Background(), manifest)
	require.NoError(t, err)

	// Verify final result was written
	data, err := fs.ReadFile("/tmp/test-fleetlift/result.json")
	require.NoError(t, err)

	var result fleetproto.AgentResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.Equal(t, fleetproto.PhaseComplete, result.Status)
	assert.NotNil(t, result.CompletedAt)
}

func TestExecute_CloneFailure(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	exec.runFunc = func(ctx context.Context, opts CommandOpts) (*CommandResult, error) {
		if opts.Name == "git" && len(opts.Args) > 0 && opts.Args[0] == "clone" {
			return &CommandResult{Stderr: "fatal: repo not found", ExitCode: 128}, fmt.Errorf("exit 128")
		}
		return &CommandResult{ExitCode: 0}, nil
	}
	p := testPipeline(fs, exec)

	manifest := &fleetproto.TaskManifest{
		TaskID: "task-1",
		Mode:   "transform",
		Repositories: []fleetproto.ManifestRepo{
			{Name: "svc", URL: "https://github.com/org/svc.git"},
		},
		Execution: fleetproto.ManifestExecution{Type: "agentic", Prompt: "Fix it"},
		GitConfig: fleetproto.ManifestGitConfig{UserEmail: "bot@test.com", UserName: "Bot"},
	}

	t.Setenv("GITHUB_TOKEN", "")

	err := p.Execute(context.Background(), manifest)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "clone failed")

	// Verify failure result was written
	data, err := fs.ReadFile("/tmp/test-fleetlift/result.json")
	require.NoError(t, err)

	var result fleetproto.AgentResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.Equal(t, fleetproto.PhaseFailed, result.Status)
	assert.NotNil(t, result.Error)
}
