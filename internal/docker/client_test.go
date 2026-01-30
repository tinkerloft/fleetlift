package docker

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	// This test requires Docker to be available
	client, err := NewClient()
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}
	require.NotNil(t, client)

	// Verify we can ping Docker
	_, err = client.Ping(context.Background())
	assert.NoError(t, err)
}

func TestExecResult(t *testing.T) {
	result := &ExecResult{
		ExitCode: 0,
		Stdout:   "hello world",
		Stderr:   "",
	}

	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "hello world", result.Stdout)
	assert.Empty(t, result.Stderr)
}

// Integration tests that require Docker

func TestCreateAndStartContainer(t *testing.T) {
	client, err := NewClient()
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}

	ctx := context.Background()

	// Pull a small test image
	err = client.PullImageIfNeeded(ctx, "alpine:latest")
	if err != nil {
		t.Skip("Cannot pull alpine image, skipping test")
	}

	// Create and start container
	containerID, err := client.CreateAndStartContainer(ctx,
		&container.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "10"},
		},
		&container.HostConfig{},
		"test-container-"+t.Name(),
	)
	require.NoError(t, err)
	require.NotEmpty(t, containerID)

	// Cleanup
	defer func() {
		_ = client.StopAndRemoveContainer(ctx, containerID, 5)
	}()

	// Verify container is running
	running, err := client.IsContainerRunning(ctx, containerID)
	require.NoError(t, err)
	assert.True(t, running)
}

func TestExecCommand(t *testing.T) {
	client, err := NewClient()
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}

	ctx := context.Background()

	// Pull and start container
	err = client.PullImageIfNeeded(ctx, "alpine:latest")
	if err != nil {
		t.Skip("Cannot pull alpine image, skipping test")
	}

	containerID, err := client.CreateAndStartContainer(ctx,
		&container.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "30"},
		},
		&container.HostConfig{},
		"test-exec-"+t.Name(),
	)
	require.NoError(t, err)

	defer func() {
		_ = client.StopAndRemoveContainer(ctx, containerID, 5)
	}()

	// Execute command
	result, err := client.ExecCommand(ctx, containerID, []string{"echo", "hello"}, "")
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "hello")
}

func TestExecShellCommand(t *testing.T) {
	client, err := NewClient()
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}

	ctx := context.Background()

	// Pull and start container - use ubuntu which has bash
	err = client.PullImageIfNeeded(ctx, "ubuntu:22.04")
	if err != nil {
		t.Skip("Cannot pull ubuntu image, skipping test")
	}

	containerID, err := client.CreateAndStartContainer(ctx,
		&container.Config{
			Image: "ubuntu:22.04",
			Cmd:   []string{"sleep", "30"},
		},
		&container.HostConfig{},
		"test-shell-"+t.Name(),
	)
	require.NoError(t, err)

	defer func() {
		_ = client.StopAndRemoveContainer(ctx, containerID, 5)
	}()

	// Execute shell command with pipe
	result, err := client.ExecShellCommand(ctx, containerID, "echo hello | tr 'h' 'H'", "")
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "Hello")
}

func TestIsContainerRunning_NotFound(t *testing.T) {
	client, err := NewClient()
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}

	running, err := client.IsContainerRunning(context.Background(), "nonexistent-container-id")
	require.NoError(t, err)
	assert.False(t, running)
}

func TestStopAndRemoveContainer_NotFound(t *testing.T) {
	client, err := NewClient()
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}

	// Should not error on non-existent container
	err = client.StopAndRemoveContainer(context.Background(), "nonexistent-container-id", 5)
	assert.NoError(t, err)
}
