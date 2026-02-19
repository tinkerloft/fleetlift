package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	flclient "github.com/tinkerloft/fleetlift/internal/client"
	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/server"
)

// mockClient is a test double for TemporalClient.
type mockClient struct {
	workflows          []flclient.WorkflowInfo
	completedWorkflows []flclient.WorkflowInfo // returned when statusFilter == "Completed"
	status             model.TaskStatus
	diffs              []model.DiffOutput
	verifierLogs       []model.VerifierOutput
	steeringState      *model.SteeringState
	progress           *model.ExecutionProgress
	err                error
}

func (m *mockClient) ListWorkflows(_ context.Context, statusFilter string, _ int) ([]flclient.WorkflowInfo, error) {
	if statusFilter == "Completed" {
		return m.completedWorkflows, m.err
	}
	return m.workflows, m.err
}
func (m *mockClient) GetWorkflowStatus(_ context.Context, _ string) (model.TaskStatus, error) {
	return m.status, m.err
}
func (m *mockClient) GetWorkflowDiff(_ context.Context, _ string) ([]model.DiffOutput, error) {
	return m.diffs, m.err
}
func (m *mockClient) GetWorkflowVerifierLogs(_ context.Context, _ string) ([]model.VerifierOutput, error) {
	return m.verifierLogs, m.err
}
func (m *mockClient) GetSteeringState(_ context.Context, _ string) (*model.SteeringState, error) {
	return m.steeringState, m.err
}
func (m *mockClient) GetExecutionProgress(_ context.Context, _ string) (*model.ExecutionProgress, error) {
	return m.progress, m.err
}
func (m *mockClient) ApproveWorkflow(_ context.Context, _ string) error          { return m.err }
func (m *mockClient) RejectWorkflow(_ context.Context, _ string) error            { return m.err }
func (m *mockClient) CancelWorkflow(_ context.Context, _ string) error            { return m.err }
func (m *mockClient) SteerWorkflow(_ context.Context, _, _ string) error          { return m.err }
func (m *mockClient) ContinueWorkflow(_ context.Context, _ string, _ bool) error { return m.err }
func (m *mockClient) Close()                                                      {}

func TestHealthEndpoint(t *testing.T) {
	s := server.New(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"status":"ok"`)
}

func TestListTasks(t *testing.T) {
	mc := &mockClient{
		workflows: []flclient.WorkflowInfo{
			{WorkflowID: "transform-abc-123", Status: "Running", StartTime: "2026-02-18 10:00:00"},
		},
		status: model.TaskStatusRunning,
	}
	s := server.New(mc, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	tasks := resp["tasks"].([]any)
	assert.Len(t, tasks, 1)
}

func TestGetInbox_AwaitingApproval(t *testing.T) {
	mc := &mockClient{
		workflows: []flclient.WorkflowInfo{
			{WorkflowID: "transform-abc-123", Status: "Running", StartTime: "2026-02-18 10:00:00"},
		},
		status: model.TaskStatusAwaitingApproval,
	}
	s := server.New(mc, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/inbox", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	items := resp["items"].([]any)
	assert.Len(t, items, 1)
	item := items[0].(map[string]any)
	assert.Equal(t, "awaiting_approval", item["inbox_type"])
}

func TestApproveWorkflow(t *testing.T) {
	s := server.New(&mockClient{}, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/transform-abc-123/approve", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSteerWorkflow(t *testing.T) {
	s := server.New(&mockClient{}, nil, nil)
	body := strings.NewReader(`{"prompt":"use slog instead"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/transform-abc-123/steer", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSteerWorkflow_MissingPrompt(t *testing.T) {
	s := server.New(&mockClient{}, nil, nil)
	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/transform-abc-123/steer", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSSEEndpoint(t *testing.T) {
	mc := &mockClient{status: model.TaskStatusRunning}
	s := server.New(mc, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/transform-abc-123/events", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "event: status")
}

func TestMetricsEndpoint(t *testing.T) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGoCollector())
	s := server.New(&mockClient{}, nil, reg)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
}
