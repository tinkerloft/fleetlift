package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
)

func TestRunVerifiers_PerRepoPerVerifier(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		Repositories: []protocol.ManifestRepo{
			{Name: "svc-a"},
			{Name: "svc-b"},
		},
		Verifiers: []protocol.ManifestVerifier{
			{Name: "build", Command: []string{"go", "build", "./..."}},
			{Name: "lint", Command: []string{"golangci-lint", "run"}},
		},
	}

	results := p.runVerifiers(context.Background(), manifest)

	require.Len(t, results, 2)
	require.Len(t, results["svc-a"], 2)
	require.Len(t, results["svc-b"], 2)

	assert.Equal(t, "build", results["svc-a"][0].Name)
	assert.Equal(t, "lint", results["svc-a"][1].Name)
	assert.True(t, results["svc-a"][0].Success)
}

func TestRunVerifiers_EmptyVerifiers(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	manifest := &protocol.TaskManifest{
		Repositories: []protocol.ManifestRepo{{Name: "svc"}},
		Verifiers:    nil,
	}

	results := p.runVerifiers(context.Background(), manifest)
	assert.Nil(t, results)
}

func TestRunVerifier_EmptyCommand(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	result := p.runVerifier(context.Background(), protocol.ManifestVerifier{
		Name:    "empty",
		Command: nil,
	}, "/workspace/svc")

	assert.False(t, result.Success)
	assert.Equal(t, -1, result.ExitCode)
	assert.Equal(t, "empty command", result.Output)
}

func TestRunVerifier_OutputTruncation(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	exec.runFunc = func(ctx context.Context, opts CommandOpts) (*CommandResult, error) {
		return &CommandResult{
			Stdout:   strings.Repeat("x", MaxOutputTruncation+500),
			ExitCode: 0,
		}, nil
	}
	p := testPipeline(fs, exec)

	result := p.runVerifier(context.Background(), protocol.ManifestVerifier{
		Name:    "verbose",
		Command: []string{"make", "test"},
	}, "/workspace/svc")

	assert.True(t, result.Success)
	assert.Contains(t, result.Output, "[truncated]")
	assert.LessOrEqual(t, len(result.Output), MaxOutputTruncation+100)
}

func TestRunVerifier_NonZeroExit(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	exec.runFunc = func(ctx context.Context, opts CommandOpts) (*CommandResult, error) {
		return &CommandResult{
			Stdout:   "FAIL",
			ExitCode: 1,
		}, fmt.Errorf("exit status 1")
	}
	p := testPipeline(fs, exec)

	result := p.runVerifier(context.Background(), protocol.ManifestVerifier{
		Name:    "build",
		Command: []string{"go", "build"},
	}, "/workspace/svc")

	assert.False(t, result.Success)
	assert.Equal(t, 1, result.ExitCode)
}
