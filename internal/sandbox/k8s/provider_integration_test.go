//go:build integration

package k8s

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

func getTestProvider(t *testing.T) *Provider {
	t.Helper()
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		t.Skip("KUBECONFIG not set, skipping integration test")
	}

	p, err := NewProvider(sandbox.ProviderConfig{
		Namespace:      "sandbox-isolated",
		AgentImage:     "fleetlift-agent:latest",
		KubeconfigPath: kubeconfig,
	})
	require.NoError(t, err)
	return p
}

func TestIntegration_ProvisionAndCleanup(t *testing.T) {
	p := getTestProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sb, err := p.Provision(ctx, sandbox.ProvisionOptions{
		TaskID:       "integ-lifecycle",
		Image:        "ubuntu:22.04",
		UseAgentMode: false,
	})
	require.NoError(t, err)
	assert.Equal(t, "fleetlift-sandbox-integ-lifecycle", sb.ID)
	assert.Equal(t, "kubernetes", sb.Provider)

	// Verify status is running.
	status, err := p.Status(ctx, sb.ID)
	require.NoError(t, err)
	assert.Equal(t, sandbox.SandboxPhaseRunning, status.Phase)

	// Cleanup.
	err = p.Cleanup(ctx, sb.ID)
	assert.NoError(t, err)

	// Cleanup is idempotent.
	err = p.Cleanup(ctx, sb.ID)
	assert.NoError(t, err)
}

func TestIntegration_FileRoundTrip(t *testing.T) {
	p := getTestProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sb, err := p.Provision(ctx, sandbox.ProvisionOptions{
		TaskID:       "integ-files",
		Image:        "ubuntu:22.04",
		UseAgentMode: false,
	})
	require.NoError(t, err)
	defer func() { _ = p.Cleanup(ctx, sb.ID) }()

	podName, err := findPodForJob(ctx, p.clientset, p.namespace, sb.ID, p.podCache)
	require.NoError(t, err)

	// Write a file.
	testData := []byte("hello from integration test")
	err = execWriteFile(ctx, p.clientset, p.restConfig, p.namespace, podName, "/tmp/test-file.txt", testData)
	require.NoError(t, err)

	// Read it back.
	data, err := execReadFile(ctx, p.clientset, p.restConfig, p.namespace, podName, "/tmp/test-file.txt")
	require.NoError(t, err)
	assert.Equal(t, testData, data)

	// Read non-existent file returns nil.
	data, err = execReadFile(ctx, p.clientset, p.restConfig, p.namespace, podName, "/tmp/does-not-exist.txt")
	require.NoError(t, err)
	assert.Nil(t, data)
}

func TestIntegration_ShellMetacharactersInFilePaths(t *testing.T) {
	p := getTestProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sb, err := p.Provision(ctx, sandbox.ProvisionOptions{
		TaskID:       "integ-shell-safety",
		Image:        "ubuntu:22.04",
		UseAgentMode: false,
	})
	require.NoError(t, err)
	defer func() { _ = p.Cleanup(ctx, sb.ID) }()

	podName, err := findPodForJob(ctx, p.clientset, p.namespace, sb.ID, p.podCache)
	require.NoError(t, err)

	// Test various shell metacharacters in file paths
	testCases := []struct {
		name    string
		path    string
		content []byte
	}{
		{
			name:    "spaces",
			path:    "/tmp/file with spaces.txt",
			content: []byte("content1"),
		},
		{
			name:    "semicolon",
			path:    "/tmp/file;dangerous.txt",
			content: []byte("content2"),
		},
		{
			name:    "command substitution",
			path:    "/tmp/$(whoami).txt",
			content: []byte("content3"),
		},
		{
			name:    "backticks",
			path:    "/tmp/`date`.txt",
			content: []byte("content4"),
		},
		{
			name:    "pipe",
			path:    "/tmp/file|pipe.txt",
			content: []byte("content5"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Write file with special characters
			err := execWriteFile(ctx, p.clientset, p.restConfig, p.namespace, podName, tc.path, tc.content)
			require.NoError(t, err, "Failed to write file with path: %s", tc.path)

			// Verify file was created with exact name (not interpreted as shell command)
			data, err := execReadFile(ctx, p.clientset, p.restConfig, p.namespace, podName, tc.path)
			require.NoError(t, err, "Failed to read file with path: %s", tc.path)
			assert.Equal(t, tc.content, data, "Content mismatch for path: %s", tc.path)
		})
	}
}

func TestIntegration_AgentProtocol(t *testing.T) {
	p := getTestProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sb, err := p.Provision(ctx, sandbox.ProvisionOptions{
		TaskID:       "integ-protocol",
		Image:        "ubuntu:22.04",
		UseAgentMode: false,
	})
	require.NoError(t, err)
	defer func() { _ = p.Cleanup(ctx, sb.ID) }()

	// Submit manifest.
	manifest := protocol.TaskManifest{
		TaskID: "integ-protocol",
		Mode:   "transform",
		Title:  "Integration test",
	}
	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)
	err = p.SubmitManifest(ctx, sb.ID, manifestBytes)
	require.NoError(t, err)

	// PollStatus should return initializing (no status file yet).
	status, err := p.PollStatus(ctx, sb.ID)
	require.NoError(t, err)
	assert.Equal(t, protocol.PhaseInitializing, status.Phase)

	// Write a status file manually and read it.
	agentStatus := protocol.AgentStatus{
		Phase:   protocol.PhaseExecuting,
		Message: "running tests",
	}
	statusBytes, err := json.Marshal(agentStatus)
	require.NoError(t, err)

	podName, _ := findPodForJob(ctx, p.clientset, p.namespace, sb.ID, p.podCache)
	err = execWriteFile(ctx, p.clientset, p.restConfig, p.namespace, podName, protocol.StatusPath, statusBytes)
	require.NoError(t, err)

	status, err = p.PollStatus(ctx, sb.ID)
	require.NoError(t, err)
	assert.Equal(t, protocol.PhaseExecuting, status.Phase)
	assert.Equal(t, "running tests", status.Message)

	// Write a result file and read it.
	result := protocol.AgentResult{
		Status: protocol.PhaseComplete,
	}
	resultBytes, err := json.Marshal(result)
	require.NoError(t, err)
	err = execWriteFile(ctx, p.clientset, p.restConfig, p.namespace, podName, protocol.ResultPath, resultBytes)
	require.NoError(t, err)

	readBack, err := p.ReadResult(ctx, sb.ID)
	require.NoError(t, err)
	assert.Contains(t, string(readBack), `"status":"complete"`)

	// Submit steering.
	instruction := protocol.SteeringInstruction{
		Action:    protocol.SteeringActionApprove,
		Iteration: 1,
	}
	instrBytes, err := json.Marshal(instruction)
	require.NoError(t, err)
	err = p.SubmitSteering(ctx, sb.ID, instrBytes)
	assert.NoError(t, err)
}
