package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	flclient "github.com/tinkerloft/fleetlift/internal/client"
	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/server"
)

// mockClient is a test double for TemporalClient.
type mockClient struct {
	workflows          []flclient.WorkflowInfo
	completedWorkflows []flclient.WorkflowInfo // returned when statusFilter == "Completed"
	status             model.TaskStatus
	result             *model.TaskResult
	diffs              []model.DiffOutput
	verifierLogs       []model.VerifierOutput
	steeringState      *model.SteeringState
	progress           *model.ExecutionProgress
	err                error
	lastTask           model.Task
}

func (m *mockClient) StartTransform(_ context.Context, task model.Task) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	m.lastTask = task
	return "transform-test-123", nil
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
func (m *mockClient) GetWorkflowResult(_ context.Context, _ string) (*model.TaskResult, error) {
	return m.result, m.err
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

func newTestServer(t *testing.T) *server.Server {
	t.Helper()
	return server.New(&mockClient{}, nil, nil, nil)
}

func TestHealthEndpoint(t *testing.T) {
	s := server.New(nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"status":"ok"`)
}

func TestListTasks(t *testing.T) {
	mc := &mockClient{
		workflows: []flclient.WorkflowInfo{
			{WorkflowID: "transform-abc-123", Status: "Running", StartTime: time.Date(2026, 2, 18, 10, 0, 0, 0, time.UTC)},
		},
		status: model.TaskStatusRunning,
	}
	s := server.New(mc, nil, nil, nil)
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
			{WorkflowID: "transform-abc-123", Status: "Running", StartTime: time.Now().Add(-1 * time.Hour)},
		},
		status: model.TaskStatusAwaitingApproval,
	}
	s := server.New(mc, nil, nil, nil)
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
	s := server.New(&mockClient{}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/transform-abc-123/approve", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSteerWorkflow(t *testing.T) {
	s := server.New(&mockClient{}, nil, nil, nil)
	body := strings.NewReader(`{"prompt":"use slog instead"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/transform-abc-123/steer", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSteerWorkflow_MissingPrompt(t *testing.T) {
	s := server.New(&mockClient{}, nil, nil, nil)
	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/transform-abc-123/steer", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSSEEndpoint(t *testing.T) {
	mc := &mockClient{status: model.TaskStatusRunning}
	s := server.New(mc, nil, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/transform-abc-123/events", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "event: status")
}

func TestGetConfig(t *testing.T) {
	s := server.New(&mockClient{}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "temporal_ui_url")
}

func TestGetResult(t *testing.T) {
	result := &model.TaskResult{
		TaskID: "test-task",
		Status: model.TaskStatusCompleted,
		Mode:   model.TaskModeTransform,
	}
	mc := &mockClient{result: result}
	s := server.New(mc, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/transform-abc-123/result", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp model.TaskResult
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "test-task", resp.TaskID)
	assert.Equal(t, model.TaskStatusCompleted, resp.Status)
}

func TestListTemplates(t *testing.T) {
	s := server.New(&mockClient{}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	templates := resp["templates"].([]any)
	assert.GreaterOrEqual(t, len(templates), 4)
}

func TestGetTemplate(t *testing.T) {
	s := server.New(&mockClient{}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates/dependency-upgrade", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "dependency-upgrade")
}

func TestGetTemplate_NotFound(t *testing.T) {
	s := server.New(&mockClient{}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates/nonexistent", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSubmitTask(t *testing.T) {
	mc := &mockClient{}
	s := server.New(mc, nil, nil, nil)
	yamlBody := `{"yaml": "version: 1\ntitle: Test\nexecution:\n  agentic:\n    prompt: do stuff\nrepositories:\n  - url: https://github.com/org/repo.git\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(yamlBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "workflow_id")
}

func TestSubmitTask_InvalidYAML(t *testing.T) {
	s := server.New(&mockClient{}, nil, nil, nil)
	body := `{"yaml": "title: missing execution"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestValidateYAML(t *testing.T) {
	s := server.New(&mockClient{}, nil, nil, nil)
	body := `{"yaml": "version: 1\ntitle: Test\nexecution:\n  agentic:\n    prompt: do stuff\nrepositories:\n  - url: https://github.com/org/repo.git\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/create/validate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"valid":true`)
}

func TestRetryTask(t *testing.T) {
	mc := &mockClient{
		result: &model.TaskResult{
			TaskID: "test-task",
			Status: model.TaskStatusCompleted,
			Groups: []model.GroupResult{
				{GroupName: "group-a", Status: "failed"},
				{GroupName: "group-b", Status: "success"},
			},
		},
	}
	s := server.New(mc, nil, nil, nil)
	yamlBody := `{"yaml": "version: 1\ntitle: Test\nexecution:\n  agentic:\n    prompt: do stuff\nrepositories:\n  - url: https://github.com/org/repo.git\n", "failed_only": true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/transform-abc-123/retry", strings.NewReader(yamlBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "workflow_id")
}

func TestRetryTask_MissingYAML(t *testing.T) {
	s := server.New(&mockClient{}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/transform-abc-123/retry", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRetryTask_ResultError(t *testing.T) {
	mc := &mockClient{err: fmt.Errorf("temporal error")}
	s := server.New(mc, nil, nil, nil)
	yamlBody := `{"yaml": "version: 1\ntitle: Test\nexecution:\n  agentic:\n    prompt: do stuff\nrepositories:\n  - url: https://github.com/org/repo.git\n    name: repo\ngroups:\n  - name: group-a\n    repositories:\n      - url: https://github.com/org/repo.git\n", "failed_only": true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/transform-abc-123/retry", strings.NewReader(yamlBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRetryTask_FailedGroupsFiltered(t *testing.T) {
	mc := &mockClient{
		result: &model.TaskResult{
			TaskID: "test-task",
			Status: model.TaskStatusCompleted,
			Groups: []model.GroupResult{
				{GroupName: "group-a", Status: "failed"},
				{GroupName: "group-b", Status: "success"},
			},
		},
	}
	s := server.New(mc, nil, nil, nil)
	yamlBody := `{"yaml": "version: 1\ntitle: Test\nexecution:\n  agentic:\n    prompt: do stuff\nrepositories:\n  - url: https://github.com/org/repo.git\n    name: repo\ngroups:\n  - name: group-a\n    repositories:\n      - url: https://github.com/org/repo.git\n  - name: group-b\n    repositories:\n      - url: https://github.com/org/repo.git\n", "failed_only": true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/transform-abc-123/retry", strings.NewReader(yamlBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "workflow_id")
	assert.Len(t, mc.lastTask.Groups, 1)
	assert.Equal(t, "group-a", mc.lastTask.Groups[0].Name)
}

func TestKnowledgeList_Empty(t *testing.T) {
	dir := t.TempDir()
	s := server.NewWithKnowledge(nil, nil, nil, nil, dir)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp["items"])
}

func TestKnowledgeCreate(t *testing.T) {
	dir := t.TempDir()
	s := server.NewWithKnowledge(nil, nil, nil, nil, dir)
	body := `{"type":"pattern","summary":"Use slog","details":"Always use log/slog","source":"manual","confidence":0.9}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)
	var item map[string]any
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &item))
	assert.NotEmpty(t, item["id"])
	assert.Equal(t, "Use slog", item["summary"])
}

func TestKnowledgeCreate_MissingSummary(t *testing.T) {
	dir := t.TempDir()
	s := server.NewWithKnowledge(nil, nil, nil, nil, dir)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge", strings.NewReader(`{"type":"pattern"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestKnowledgeGet_NotFound(t *testing.T) {
	dir := t.TempDir()
	s := server.NewWithKnowledge(nil, nil, nil, nil, dir)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/nonexistent", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestKnowledgeGet(t *testing.T) {
	dir := t.TempDir()
	s := server.NewWithKnowledge(nil, nil, nil, nil, dir)

	// Create
	createBody := `{"type":"pattern","summary":"test summary","details":"details","source":"manual","confidence":0.8}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	s.ServeHTTP(createW, createReq)
	assert.Equal(t, http.StatusCreated, createW.Code)
	var created map[string]any
	json.Unmarshal(createW.Body.Bytes(), &created)
	id := created["id"].(string)

	// Get
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/"+id, nil)
	getW := httptest.NewRecorder()
	s.ServeHTTP(getW, getReq)
	assert.Equal(t, http.StatusOK, getW.Code)
	assert.Contains(t, getW.Body.String(), "test summary")
}

func TestKnowledgeUpdate(t *testing.T) {
	dir := t.TempDir()
	s := server.NewWithKnowledge(nil, nil, nil, nil, dir)

	// Create
	createBody := `{"type":"pattern","summary":"original","details":"details","source":"manual","confidence":0.8}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	s.ServeHTTP(createW, createReq)
	assert.Equal(t, http.StatusCreated, createW.Code)
	var created map[string]any
	json.Unmarshal(createW.Body.Bytes(), &created)
	id := created["id"].(string)

	// Update
	updateBody := `{"summary":"updated summary"}`
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/knowledge/"+id, strings.NewReader(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	s.ServeHTTP(updateW, updateReq)
	assert.Equal(t, http.StatusOK, updateW.Code)
	assert.Contains(t, updateW.Body.String(), "updated summary")
}

func TestKnowledgeDelete(t *testing.T) {
	dir := t.TempDir()
	s := server.NewWithKnowledge(nil, nil, nil, nil, dir)

	// Create
	createBody := `{"type":"pattern","summary":"to delete","source":"manual","confidence":0.5}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	s.ServeHTTP(createW, createReq)
	var created map[string]any
	json.Unmarshal(createW.Body.Bytes(), &created)
	id := created["id"].(string)

	// Delete
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/knowledge/"+id, nil)
	deleteW := httptest.NewRecorder()
	s.ServeHTTP(deleteW, deleteReq)
	assert.Equal(t, http.StatusOK, deleteW.Code)

	// Verify gone
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/"+id, nil)
	getW := httptest.NewRecorder()
	s.ServeHTTP(getW, getReq)
	assert.Equal(t, http.StatusNotFound, getW.Code)
}

func TestKnowledgeBulk_Approve(t *testing.T) {
	dir := t.TempDir()
	s := server.NewWithKnowledge(nil, nil, nil, nil, dir)

	// Create
	createBody := `{"type":"pattern","summary":"bulk test","source":"manual","confidence":0.7}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	s.ServeHTTP(createW, createReq)
	var created map[string]any
	json.Unmarshal(createW.Body.Bytes(), &created)
	id := created["id"].(string)

	// Bulk approve
	bulkBody := fmt.Sprintf(`{"actions":[{"id":"%s","action":"approve"}]}`, id)
	bulkReq := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/bulk", strings.NewReader(bulkBody))
	bulkReq.Header.Set("Content-Type", "application/json")
	bulkW := httptest.NewRecorder()
	s.ServeHTTP(bulkW, bulkReq)
	assert.Equal(t, http.StatusOK, bulkW.Code)

	// Verify approved
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/"+id, nil)
	getW := httptest.NewRecorder()
	s.ServeHTTP(getW, getReq)
	assert.Contains(t, getW.Body.String(), `"approved"`)
}

func TestKnowledgeBulk_Delete(t *testing.T) {
	dir := t.TempDir()
	s := server.NewWithKnowledge(nil, nil, nil, nil, dir)

	// Create
	createBody := `{"type":"pattern","summary":"to bulk delete","source":"manual","confidence":0.5}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	s.ServeHTTP(createW, createReq)
	var created map[string]any
	json.Unmarshal(createW.Body.Bytes(), &created)
	id := created["id"].(string)

	// Bulk delete
	bulkBody := fmt.Sprintf(`{"actions":[{"id":"%s","action":"delete"}]}`, id)
	bulkReq := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/bulk", strings.NewReader(bulkBody))
	bulkReq.Header.Set("Content-Type", "application/json")
	bulkW := httptest.NewRecorder()
	s.ServeHTTP(bulkW, bulkReq)
	assert.Equal(t, http.StatusOK, bulkW.Code)

	// Verify gone
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/"+id, nil)
	getW := httptest.NewRecorder()
	s.ServeHTTP(getW, getReq)
	assert.Equal(t, http.StatusNotFound, getW.Code)
}

func TestKnowledgeCommit(t *testing.T) {
	dir := t.TempDir()
	repoDir := t.TempDir()
	s := server.NewWithKnowledge(nil, nil, nil, nil, dir)

	// Create + approve an item
	createBody := `{"type":"pattern","summary":"commit test","source":"manual","confidence":0.9}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	s.ServeHTTP(createW, createReq)
	var created map[string]any
	json.Unmarshal(createW.Body.Bytes(), &created)
	id := created["id"].(string)

	bulkBody := fmt.Sprintf(`{"actions":[{"id":"%s","action":"approve"}]}`, id)
	bulkReq := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/bulk", strings.NewReader(bulkBody))
	bulkReq.Header.Set("Content-Type", "application/json")
	s.ServeHTTP(httptest.NewRecorder(), bulkReq)

	// Commit
	commitBody := fmt.Sprintf(`{"repo_path":"%s"}`, repoDir)
	commitReq := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/commit", strings.NewReader(commitBody))
	commitReq.Header.Set("Content-Type", "application/json")
	commitW := httptest.NewRecorder()
	s.ServeHTTP(commitW, commitReq)
	assert.Equal(t, http.StatusOK, commitW.Code)
	assert.Contains(t, commitW.Body.String(), `"committed":1`)
}

func TestKnowledgeCommit_MissingRepoPath(t *testing.T) {
	dir := t.TempDir()
	s := server.NewWithKnowledge(nil, nil, nil, nil, dir)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/commit", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestKnowledgeList_Filters(t *testing.T) {
	dir := t.TempDir()
	s := server.NewWithKnowledge(nil, nil, nil, nil, dir)

	// Create two items: one pattern/approved, one correction/pending
	s.ServeHTTP(httptest.NewRecorder(), func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge",
			strings.NewReader(`{"type":"pattern","summary":"pattern item","source":"manual","confidence":0.9,"tags":["go"]}`))
		r.Header.Set("Content-Type", "application/json")
		return r
	}())

	// Approve the pattern item via bulk
	listW := httptest.NewRecorder()
	s.ServeHTTP(listW, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge", nil))
	var listResp map[string]any
	json.Unmarshal(listW.Body.Bytes(), &listResp)
	items := listResp["items"].([]any)
	patternID := items[0].(map[string]any)["id"].(string)
	bulkBody := fmt.Sprintf(`{"actions":[{"id":"%s","action":"approve"}]}`, patternID)
	s.ServeHTTP(httptest.NewRecorder(), func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/bulk", strings.NewReader(bulkBody))
		r.Header.Set("Content-Type", "application/json")
		return r
	}())

	s.ServeHTTP(httptest.NewRecorder(), func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge",
			strings.NewReader(`{"type":"correction","summary":"correction item","source":"manual","confidence":0.5,"tags":["python"]}`))
		r.Header.Set("Content-Type", "application/json")
		return r
	}())

	// Filter by type
	typeW := httptest.NewRecorder()
	s.ServeHTTP(typeW, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge?type=pattern", nil))
	assert.Equal(t, http.StatusOK, typeW.Code)
	var typeResp map[string]any
	json.Unmarshal(typeW.Body.Bytes(), &typeResp)
	assert.Len(t, typeResp["items"].([]any), 1)

	// Filter by status
	statusW := httptest.NewRecorder()
	s.ServeHTTP(statusW, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge?status=approved", nil))
	assert.Equal(t, http.StatusOK, statusW.Code)
	var statusResp map[string]any
	json.Unmarshal(statusW.Body.Bytes(), &statusResp)
	assert.Len(t, statusResp["items"].([]any), 1)

	// Filter by tag
	tagW := httptest.NewRecorder()
	s.ServeHTTP(tagW, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge?tag=go", nil))
	assert.Equal(t, http.StatusOK, tagW.Code)
	var tagResp map[string]any
	json.Unmarshal(tagW.Body.Bytes(), &tagResp)
	assert.Len(t, tagResp["items"].([]any), 1)
}

func TestMetricsEndpoint(t *testing.T) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	s := server.New(&mockClient{}, nil, reg, nil)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
}

func TestHandleSteer_RejectsOversizedBody(t *testing.T) {
	mc := &mockClient{}
	s := server.New(mc, nil, nil, nil)

	// Build a body larger than 1 MB
	hugebody := strings.Repeat("x", 2*1024*1024)
	body := fmt.Sprintf(`{"prompt":%q}`, hugebody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/wf-123/steer", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestCORSOriginRestriction(t *testing.T) {
	mc := &mockClient{}
	s := server.New(mc, nil, nil, []string{"https://app.example.com"})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/health", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	assert.NotEqual(t, "https://evil.example.com", w.Header().Get("Access-Control-Allow-Origin"))
}

type countingClient struct {
	mockClient
	getStatusCalls int
}

func (c *countingClient) GetWorkflowStatus(ctx context.Context, id string) (model.TaskStatus, error) {
	c.getStatusCalls++
	return c.mockClient.GetWorkflowStatus(ctx, id)
}

func TestListTasks_NoPerWorkflowStatusQuery(t *testing.T) {
	mc := &countingClient{
		mockClient: mockClient{
			workflows: []flclient.WorkflowInfo{
				{WorkflowID: "w1", Status: "Running", StartTime: time.Now()},
				{WorkflowID: "w2", Status: "Completed", StartTime: time.Now()},
				{WorkflowID: "w3", Status: "Failed", StartTime: time.Now()},
			},
		},
	}
	s := server.New(mc, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 0, mc.getStatusCalls, "handleListTasks must not issue per-workflow status queries")
}

func TestGetTaskYAML(t *testing.T) {
	s := newTestServer(t)
	yamlBody := `{"yaml": "version: 1\nid: test\ntitle: Test\nmode: transform\nrepositories:\n  - url: https://github.com/org/repo\nexecution:\n  agentic:\n    prompt: do something\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(yamlBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var submitResp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &submitResp))
	wfID := submitResp["workflow_id"]

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+wfID+"/yaml", nil)
	w2 := httptest.NewRecorder()
	s.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	var yamlResp map[string]string
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &yamlResp))
	assert.Contains(t, yamlResp["yaml"], "title: Test")
}

func TestGetTaskYAMLNotFound(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/nonexistent/yaml", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
