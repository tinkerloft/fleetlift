package activity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

// agentMockProvider implements sandbox.AgentProvider for agent activity tests.
type agentMockProvider struct {
	submitManifestFunc func(ctx context.Context, id string, manifest []byte) error
	pollStatusFunc     func(ctx context.Context, id string) (*protocol.AgentStatus, error)
	readResultFunc     func(ctx context.Context, id string) ([]byte, error)
	submitSteeringFunc func(ctx context.Context, id string, instruction []byte) error
}

func (m *agentMockProvider) Provision(_ context.Context, _ sandbox.ProvisionOptions) (*sandbox.Sandbox, error) {
	return nil, errors.New("not implemented")
}
func (m *agentMockProvider) Exec(_ context.Context, _ string, _ sandbox.ExecCommand) (*sandbox.ExecResult, error) {
	return nil, errors.New("not implemented")
}
func (m *agentMockProvider) ExecShell(_ context.Context, _, _, _ string) (*sandbox.ExecResult, error) {
	return nil, errors.New("not implemented")
}
func (m *agentMockProvider) CopyTo(_ context.Context, _ string, _ io.Reader, _ string) error {
	return errors.New("not implemented")
}
func (m *agentMockProvider) CopyFrom(_ context.Context, _, _ string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}
func (m *agentMockProvider) Status(_ context.Context, _ string) (*sandbox.SandboxStatus, error) {
	return nil, errors.New("not implemented")
}
func (m *agentMockProvider) Cleanup(_ context.Context, _ string) error {
	return errors.New("not implemented")
}
func (m *agentMockProvider) Name() string { return "agent-mock" }

func (m *agentMockProvider) SubmitManifest(ctx context.Context, id string, manifest []byte) error {
	if m.submitManifestFunc != nil {
		return m.submitManifestFunc(ctx, id, manifest)
	}
	return nil
}

func (m *agentMockProvider) PollStatus(ctx context.Context, id string) (*protocol.AgentStatus, error) {
	if m.pollStatusFunc != nil {
		return m.pollStatusFunc(ctx, id)
	}
	return &protocol.AgentStatus{Phase: protocol.PhaseComplete}, nil
}

func (m *agentMockProvider) ReadResult(ctx context.Context, id string) ([]byte, error) {
	if m.readResultFunc != nil {
		return m.readResultFunc(ctx, id)
	}
	return nil, errors.New("not implemented")
}

func (m *agentMockProvider) SubmitSteering(ctx context.Context, id string, instruction []byte) error {
	if m.submitSteeringFunc != nil {
		return m.submitSteeringFunc(ctx, id, instruction)
	}
	return nil
}

func TestSubmitTaskManifest(t *testing.T) {
	var captured []byte
	var capturedID string

	provider := &agentMockProvider{
		submitManifestFunc: func(_ context.Context, id string, manifest []byte) error {
			capturedID = id
			captured = manifest
			return nil
		},
	}

	activities := NewAgentActivities(provider)

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()
	env.RegisterActivity(activities.SubmitTaskManifest)

	input := SubmitTaskManifestInput{
		SandboxID: "container-123",
		Manifest: protocol.TaskManifest{
			TaskID: "test-task",
			Mode:   "transform",
			Title:  "Test Task",
		},
	}

	_, err := env.ExecuteActivity(activities.SubmitTaskManifest, input)
	require.NoError(t, err)

	assert.Equal(t, "container-123", capturedID)

	var decoded protocol.TaskManifest
	require.NoError(t, json.Unmarshal(captured, &decoded))
	assert.Equal(t, "test-task", decoded.TaskID)
}

func TestSubmitTaskManifest_Error(t *testing.T) {
	provider := &agentMockProvider{
		submitManifestFunc: func(_ context.Context, _ string, _ []byte) error {
			return errors.New("write failed")
		},
	}

	activities := NewAgentActivities(provider)

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()
	env.RegisterActivity(activities.SubmitTaskManifest)

	input := SubmitTaskManifestInput{
		SandboxID: "container-123",
		Manifest:  protocol.TaskManifest{TaskID: "test"},
	}

	_, err := env.ExecuteActivity(activities.SubmitTaskManifest, input)
	assert.Error(t, err)
}

func TestWaitForAgentPhase_ImmediateMatch(t *testing.T) {
	provider := &agentMockProvider{
		pollStatusFunc: func(_ context.Context, _ string) (*protocol.AgentStatus, error) {
			return &protocol.AgentStatus{
				Phase:     protocol.PhaseComplete,
				Message:   "done",
				UpdatedAt: time.Now().UTC(),
			}, nil
		},
	}

	activities := NewAgentActivities(provider)

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()
	env.RegisterActivity(activities.WaitForAgentPhase)

	input := WaitForAgentPhaseInput{
		SandboxID:    "container-123",
		TargetPhases: []string{string(protocol.PhaseComplete)},
	}

	result, err := env.ExecuteActivity(activities.WaitForAgentPhase, input)
	require.NoError(t, err)

	var status protocol.AgentStatus
	require.NoError(t, result.Get(&status))
	assert.Equal(t, protocol.PhaseComplete, status.Phase)
}

func TestWaitForAgentPhase_PollingUntilReady(t *testing.T) {
	var callCount atomic.Int32

	provider := &agentMockProvider{
		pollStatusFunc: func(_ context.Context, _ string) (*protocol.AgentStatus, error) {
			count := callCount.Add(1)
			if count < 3 {
				return &protocol.AgentStatus{Phase: protocol.PhaseExecuting}, nil
			}
			return &protocol.AgentStatus{Phase: protocol.PhaseAwaitingInput}, nil
		},
	}

	activities := NewAgentActivities(provider)

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()
	env.RegisterActivity(activities.WaitForAgentPhase)

	input := WaitForAgentPhaseInput{
		SandboxID:    "container-123",
		TargetPhases: []string{string(protocol.PhaseAwaitingInput), string(protocol.PhaseComplete)},
	}

	result, err := env.ExecuteActivity(activities.WaitForAgentPhase, input)
	require.NoError(t, err)

	var status protocol.AgentStatus
	require.NoError(t, result.Get(&status))
	assert.Equal(t, protocol.PhaseAwaitingInput, status.Phase)
	assert.GreaterOrEqual(t, int(callCount.Load()), 3)
}

func TestWaitForAgentPhase_FailedIsAlwaysTerminal(t *testing.T) {
	provider := &agentMockProvider{
		pollStatusFunc: func(_ context.Context, _ string) (*protocol.AgentStatus, error) {
			return &protocol.AgentStatus{Phase: protocol.PhaseFailed, Message: "crash"}, nil
		},
	}

	activities := NewAgentActivities(provider)

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()
	env.RegisterActivity(activities.WaitForAgentPhase)

	// Even though we only ask for "complete", failed should be returned
	input := WaitForAgentPhaseInput{
		SandboxID:    "container-123",
		TargetPhases: []string{string(protocol.PhaseComplete)},
	}

	result, err := env.ExecuteActivity(activities.WaitForAgentPhase, input)
	require.NoError(t, err)

	var status protocol.AgentStatus
	require.NoError(t, result.Get(&status))
	assert.Equal(t, protocol.PhaseFailed, status.Phase)
}

func TestWaitForAgentPhase_StaleAgent(t *testing.T) {
	staleTime := time.Now().UTC().Add(-10 * time.Minute) // 10 min ago — well past threshold

	provider := &agentMockProvider{
		pollStatusFunc: func(_ context.Context, _ string) (*protocol.AgentStatus, error) {
			return &protocol.AgentStatus{
				Phase:     protocol.PhaseExecuting,
				UpdatedAt: staleTime,
			}, nil
		},
	}

	activities := NewAgentActivities(provider)

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()
	env.RegisterActivity(activities.WaitForAgentPhase)

	input := WaitForAgentPhaseInput{
		SandboxID:    "container-123",
		TargetPhases: []string{string(protocol.PhaseComplete)},
	}

	_, err := env.ExecuteActivity(activities.WaitForAgentPhase, input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "agent stale")
}

func TestReadAgentResult(t *testing.T) {
	agentResult := protocol.AgentResult{
		Status: protocol.PhaseComplete,
		Repositories: []protocol.RepoResult{
			{Name: "svc", Status: "success", FilesModified: []string{"main.go"}},
		},
		StartedAt: time.Now().UTC(),
	}
	resultBytes, _ := json.Marshal(agentResult)

	provider := &agentMockProvider{
		readResultFunc: func(_ context.Context, _ string) ([]byte, error) {
			return resultBytes, nil
		},
	}

	activities := NewAgentActivities(provider)

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()
	env.RegisterActivity(activities.ReadAgentResult)

	input := ReadAgentResultInput{SandboxID: "container-123"}
	result, err := env.ExecuteActivity(activities.ReadAgentResult, input)
	require.NoError(t, err)

	var decoded protocol.AgentResult
	require.NoError(t, result.Get(&decoded))
	assert.Equal(t, protocol.PhaseComplete, decoded.Status)
	assert.Len(t, decoded.Repositories, 1)
	assert.Equal(t, "svc", decoded.Repositories[0].Name)
}

func TestReadAgentResult_Error(t *testing.T) {
	provider := &agentMockProvider{
		readResultFunc: func(_ context.Context, _ string) ([]byte, error) {
			return nil, errors.New("file not found")
		},
	}

	activities := NewAgentActivities(provider)

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()
	env.RegisterActivity(activities.ReadAgentResult)

	input := ReadAgentResultInput{SandboxID: "container-123"}
	_, err := env.ExecuteActivity(activities.ReadAgentResult, input)
	assert.Error(t, err)
}

func TestSubmitSteeringAction(t *testing.T) {
	var captured []byte

	provider := &agentMockProvider{
		submitSteeringFunc: func(_ context.Context, _ string, instruction []byte) error {
			captured = instruction
			return nil
		},
	}

	activities := NewAgentActivities(provider)

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()
	env.RegisterActivity(activities.SubmitSteeringAction)

	input := SubmitSteeringActionInput{
		SandboxID: "container-123",
		Action:    string(protocol.SteeringActionSteer),
		Prompt:    "Also update test helpers",
		Iteration: 1,
	}

	_, err := env.ExecuteActivity(activities.SubmitSteeringAction, input)
	require.NoError(t, err)

	var decoded protocol.SteeringInstruction
	require.NoError(t, json.Unmarshal(captured, &decoded))
	assert.Equal(t, protocol.SteeringActionSteer, decoded.Action)
	assert.Equal(t, "Also update test helpers", decoded.Prompt)
	assert.Equal(t, 1, decoded.Iteration)
}

func TestSubmitSteeringAction_Approve(t *testing.T) {
	var captured []byte

	provider := &agentMockProvider{
		submitSteeringFunc: func(_ context.Context, _ string, instruction []byte) error {
			captured = instruction
			return nil
		},
	}

	activities := NewAgentActivities(provider)

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()
	env.RegisterActivity(activities.SubmitSteeringAction)

	input := SubmitSteeringActionInput{
		SandboxID: "container-123",
		Action:    string(protocol.SteeringActionApprove),
	}

	_, err := env.ExecuteActivity(activities.SubmitSteeringAction, input)
	require.NoError(t, err)

	var decoded protocol.SteeringInstruction
	require.NoError(t, json.Unmarshal(captured, &decoded))
	assert.Equal(t, protocol.SteeringActionApprove, decoded.Action)
}
