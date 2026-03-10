# Phase 1 Completion Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Complete Phase 1 of web interface enrichment by adding the Retry API (1C) and Knowledge API (1F).

**Architecture:** Two new handler files in `internal/server/`. Knowledge API wraps the existing `internal/knowledge.Store`. Retry API reads prior workflow result, filters to failed groups, resubmits. Server struct gains a `knowledgeStore` field. CORS updated to allow PUT/DELETE.

**Tech Stack:** Go, chi router, `internal/knowledge`, `internal/model`

---

### Task 1: Retry API (1C)

**Files:**
- Modify: `internal/server/handlers_create.go` (add retry handler)
- Modify: `internal/server/server.go` (register route, add PUT/DELETE to CORS)
- Modify: `internal/server/server_test.go` (add tests)

**What it does:** `POST /api/v1/tasks/{id}/retry` — reads the task YAML + `failed_only` flag from request body. Gets workflow result for `{id}`, extracts failed group names, filters task groups, starts a new workflow.

**Step 1: Write the failing tests**

Add to `internal/server/server_test.go`:

```go
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
	s := server.New(mc, nil, nil)
	body := `{"yaml": "version: 1\ntitle: Test\nexecution:\n  agentic:\n    prompt: do stuff\nrepositories:\n  - url: https://github.com/org/repo.git\ngroups:\n  - name: group-a\n    repositories: [repo]\n  - name: group-b\n    repositories: [repo]\n", "failed_only": true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/transform-abc-123/retry", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "workflow_id")
}

func TestRetryTask_MissingYAML(t *testing.T) {
	s := server.New(&mockClient{}, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/transform-abc-123/retry", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
```

**Step 2: Run tests to verify they fail**

```bash
cd /Users/andrew/dev/code/projects/fleetlift-web && go test ./internal/server/... -run TestRetry -v
```
Expected: FAIL — no retry route registered.

**Step 3: Add retry handler to `internal/server/handlers_create.go`**

Add after `handleSubmitTask`:

```go
type retryRequest struct {
	YAML       string `json:"yaml"`
	FailedOnly bool   `json:"failed_only"`
}

// handleRetryTask handles POST /api/v1/tasks/{id}/retry.
func (s *Server) handleRetryTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req retryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.YAML == "" {
		writeError(w, http.StatusBadRequest, "yaml is required")
		return
	}

	task, err := create.ValidateTaskYAML(req.YAML)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.FailedOnly {
		result, err := s.client.GetWorkflowResult(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get workflow result: %v", err))
			return
		}
		failedNames := make(map[string]bool)
		for _, g := range result.Groups {
			if g.Status == "failed" {
				failedNames[g.GroupName] = true
			}
		}
		if len(failedNames) > 0 && len(task.Groups) > 0 {
			var filtered []model.Group
			for _, g := range task.Groups {
				if failedNames[g.Name] {
					filtered = append(filtered, g)
				}
			}
			task.Groups = filtered
		}
	}

	workflowID, err := s.client.StartTransform(r.Context(), task)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to start workflow: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, submitResponse{WorkflowID: workflowID})
}
```

**Step 4: Register route in `internal/server/server.go`**

Inside the `/api/v1/tasks/{id}` route group, add:
```go
r.Post("/retry", s.handleRetryTask)
```

Also update the CORS `AllowedMethods` to include `"PUT"` and `"DELETE"`:
```go
AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
```

**Step 5: Run tests to verify they pass**

```bash
cd /Users/andrew/dev/code/projects/fleetlift-web && go test ./internal/server/... -run TestRetry -v
```
Expected: PASS

**Step 6: Run full test suite**

```bash
cd /Users/andrew/dev/code/projects/fleetlift-web && go test ./...
```
Expected: all pass

---

### Task 2: Knowledge API Handler (1F)

**Files:**
- Create: `internal/server/handlers_knowledge.go`
- Modify: `internal/server/server.go` (add `knowledgeStore` field, update `New()`, register routes)
- Modify: `internal/server/server_test.go` (add knowledge tests)

**Endpoints to implement:**
```
GET    /api/v1/knowledge              — list all (filters: ?task_id, ?type, ?tag, ?status)
GET    /api/v1/knowledge/{id}         — get single item
POST   /api/v1/knowledge              — create item
PUT    /api/v1/knowledge/{id}         — update item
DELETE /api/v1/knowledge/{id}         — delete item
POST   /api/v1/knowledge/bulk         — bulk approve/delete [{id, action}]
POST   /api/v1/knowledge/commit       — copy approved items to repo path
```

**Step 1: Write the failing tests**

Add to `internal/server/server_test.go`:

```go
func TestKnowledgeList(t *testing.T) {
	dir := t.TempDir()
	s := server.NewWithKnowledge(nil, nil, nil, dir)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "items")
}

func TestKnowledgeCreate(t *testing.T) {
	dir := t.TempDir()
	s := server.NewWithKnowledge(nil, nil, nil, dir)
	body := `{"type":"pattern","summary":"Use slog","details":"Always use log/slog","source":"manual","confidence":0.9}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), "id")
}

func TestKnowledgeGetNotFound(t *testing.T) {
	dir := t.TempDir()
	s := server.NewWithKnowledge(nil, nil, nil, dir)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/nonexistent", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestKnowledgeBulkApprove(t *testing.T) {
	dir := t.TempDir()
	s := server.NewWithKnowledge(nil, nil, nil, dir)
	// Create an item first
	createBody := `{"type":"pattern","summary":"test","details":"details","source":"manual","confidence":0.8}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	s.ServeHTTP(createW, createReq)
	assert.Equal(t, http.StatusCreated, createW.Code)

	var created map[string]any
	json.Unmarshal(createW.Body.Bytes(), &created)
	id := created["id"].(string)

	// Bulk approve it
	bulkBody := fmt.Sprintf(`{"actions":[{"id":"%s","action":"approve"}]}`, id)
	bulkReq := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/bulk", strings.NewReader(bulkBody))
	bulkReq.Header.Set("Content-Type", "application/json")
	bulkW := httptest.NewRecorder()
	s.ServeHTTP(bulkW, bulkReq)
	assert.Equal(t, http.StatusOK, bulkW.Code)
}
```

**Step 2: Run tests to verify they fail**

```bash
cd /Users/andrew/dev/code/projects/fleetlift-web && go test ./internal/server/... -run TestKnowledge -v
```
Expected: compile error — `NewWithKnowledge` undefined.

**Step 3: Add `knowledgeStore` to Server and expose `NewWithKnowledge`**

In `internal/server/server.go`:

Add import:
```go
"github.com/tinkerloft/fleetlift/internal/knowledge"
```

Update `Server` struct:
```go
type Server struct {
    router         chi.Router
    client         TemporalClient
    staticFS       fs.FS
    gatherer       prometheus.Gatherer
    conversations  *create.ConversationStore
    knowledgeStore *knowledge.Store
}
```

Update `New()` to use the default store:
```go
func New(client TemporalClient, staticFS fs.FS, gatherer prometheus.Gatherer) *Server {
    return NewWithKnowledge(client, staticFS, gatherer, "")
}
```

Add `NewWithKnowledge()`:
```go
// NewWithKnowledge creates a Server with a custom knowledge store directory.
// If knowledgeDir is empty, the default (~/.fleetlift/knowledge) is used.
func NewWithKnowledge(client TemporalClient, staticFS fs.FS, gatherer prometheus.Gatherer, knowledgeDir string) *Server {
    var ks *knowledge.Store
    if knowledgeDir != "" {
        ks = knowledge.NewStore(knowledgeDir)
    } else {
        ks = knowledge.DefaultStore()
    }
    s := &Server{
        client:         client,
        staticFS:       staticFS,
        gatherer:       gatherer,
        conversations:  create.NewConversationStore(30 * time.Minute),
        knowledgeStore: ks,
    }
    s.router = s.buildRouter()
    return s
}
```

**Step 4: Register knowledge routes in `buildRouter`**

In `internal/server/server.go`, add inside `buildRouter`:
```go
// Knowledge routes
r.Get("/api/v1/knowledge", s.handleListKnowledge)
r.Post("/api/v1/knowledge/bulk", s.handleBulkKnowledge)
r.Post("/api/v1/knowledge/commit", s.handleCommitKnowledge)
r.Post("/api/v1/knowledge", s.handleCreateKnowledge)
r.Get("/api/v1/knowledge/{id}", s.handleGetKnowledge)
r.Put("/api/v1/knowledge/{id}", s.handleUpdateKnowledge)
r.Delete("/api/v1/knowledge/{id}", s.handleDeleteKnowledge)
```

Note: `bulk` and `commit` must be registered before `{id}` to avoid chi routing ambiguity.

**Step 5: Create `internal/server/handlers_knowledge.go`**

```go
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// handleListKnowledge handles GET /api/v1/knowledge
// Query params: task_id, type, tag, status
func (s *Server) handleListKnowledge(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task_id")
	filterType := r.URL.Query().Get("type")
	filterTag := r.URL.Query().Get("tag")
	filterStatus := r.URL.Query().Get("status")

	var items []model.KnowledgeItem
	var err error
	if taskID != "" {
		items, err = s.knowledgeStore.List(taskID)
	} else {
		items, err = s.knowledgeStore.ListAll()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Apply filters
	filtered := items[:0]
	for _, item := range items {
		if filterType != "" && string(item.Type) != filterType {
			continue
		}
		if filterStatus != "" && string(item.Status) != filterStatus {
			continue
		}
		if filterTag != "" {
			found := false
			for _, t := range item.Tags {
				if strings.EqualFold(t, filterTag) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	if filtered == nil {
		filtered = []model.KnowledgeItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": filtered})
}

// handleGetKnowledge handles GET /api/v1/knowledge/{id}
func (s *Server) handleGetKnowledge(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	items, err := s.knowledgeStore.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, item := range items {
		if item.ID == id {
			writeJSON(w, http.StatusOK, item)
			return
		}
	}
	writeError(w, http.StatusNotFound, fmt.Sprintf("knowledge item %q not found", id))
}

type createKnowledgeRequest struct {
	Type       model.KnowledgeType    `json:"type"`
	Summary    string                 `json:"summary"`
	Details    string                 `json:"details"`
	Source     model.KnowledgeSource  `json:"source"`
	Tags       []string               `json:"tags"`
	Confidence float64                `json:"confidence"`
	TaskID     string                 `json:"task_id"`
}

// handleCreateKnowledge handles POST /api/v1/knowledge
func (s *Server) handleCreateKnowledge(w http.ResponseWriter, r *http.Request) {
	var req createKnowledgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Summary == "" {
		writeError(w, http.StatusBadRequest, "summary is required")
		return
	}
	if req.Source == "" {
		req.Source = model.KnowledgeSourceManual
	}

	taskID := req.TaskID
	if taskID == "" {
		taskID = "manual"
	}

	item := model.KnowledgeItem{
		ID:         uuid.New().String()[:8],
		Type:       req.Type,
		Summary:    req.Summary,
		Details:    req.Details,
		Source:     req.Source,
		Tags:       req.Tags,
		Confidence: req.Confidence,
		CreatedAt:  time.Now(),
		Status:     model.KnowledgeStatusPending,
	}

	if err := s.knowledgeStore.Write(taskID, item); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleUpdateKnowledge handles PUT /api/v1/knowledge/{id}
func (s *Server) handleUpdateKnowledge(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Find existing item
	items, err := s.knowledgeStore.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var existing *model.KnowledgeItem
	for i, item := range items {
		if item.ID == id {
			existing = &items[i]
			break
		}
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("knowledge item %q not found", id))
		return
	}

	// Decode partial update — only allow updating mutable fields
	var req struct {
		Summary    *string                  `json:"summary"`
		Details    *string                  `json:"details"`
		Tags       []string                 `json:"tags"`
		Status     *model.KnowledgeStatus   `json:"status"`
		Confidence *float64                 `json:"confidence"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Summary != nil {
		existing.Summary = *req.Summary
	}
	if req.Details != nil {
		existing.Details = *req.Details
	}
	if req.Tags != nil {
		existing.Tags = req.Tags
	}
	if req.Status != nil {
		existing.Status = *req.Status
	}
	if req.Confidence != nil {
		existing.Confidence = *req.Confidence
	}

	if err := s.knowledgeStore.Update(*existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

// handleDeleteKnowledge handles DELETE /api/v1/knowledge/{id}
func (s *Server) handleDeleteKnowledge(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.knowledgeStore.Delete(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type bulkAction struct {
	ID     string `json:"id"`
	Action string `json:"action"` // "approve" or "delete"
}

type bulkRequest struct {
	Actions []bulkAction `json:"actions"`
}

// handleBulkKnowledge handles POST /api/v1/knowledge/bulk
func (s *Server) handleBulkKnowledge(w http.ResponseWriter, r *http.Request) {
	var req bulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	approved := model.KnowledgeStatusApproved
	var errs []string
	for _, a := range req.Actions {
		switch a.Action {
		case "approve":
			items, err := s.knowledgeStore.ListAll()
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", a.ID, err))
				continue
			}
			found := false
			for _, item := range items {
				if item.ID == a.ID {
					item.Status = approved
					if err := s.knowledgeStore.Update(item); err != nil {
						errs = append(errs, fmt.Sprintf("%s: %v", a.ID, err))
					}
					found = true
					break
				}
			}
			if !found {
				errs = append(errs, fmt.Sprintf("%s: not found", a.ID))
			}
		case "delete":
			if err := s.knowledgeStore.Delete(a.ID); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", a.ID, err))
			}
		default:
			errs = append(errs, fmt.Sprintf("%s: unknown action %q", a.ID, a.Action))
		}
	}

	if len(errs) > 0 {
		writeJSON(w, http.StatusMultiStatus, map[string]any{"errors": errs})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type commitRequest struct {
	RepoPath string `json:"repo_path"`
}

// handleCommitKnowledge handles POST /api/v1/knowledge/commit
// Copies approved knowledge items to {repo_path}/.fleetlift/knowledge/items/
func (s *Server) handleCommitKnowledge(w http.ResponseWriter, r *http.Request) {
	var req commitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RepoPath == "" {
		writeError(w, http.StatusBadRequest, "repo_path is required")
		return
	}

	approved, err := s.knowledgeStore.ListApproved()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Use a temporary store rooted at the repo path to write items there
	repoStore := newRepoKnowledgeStore(req.RepoPath)
	var committed int
	for _, item := range approved {
		if err := repoStore.Write("items", item); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("writing item %s: %v", item.ID, err))
			return
		}
		committed++
	}

	writeJSON(w, http.StatusOK, map[string]any{"committed": committed, "repo_path": req.RepoPath})
}
```

Note: `newRepoKnowledgeStore` is a small helper. The knowledge store writes to `{baseDir}/{taskID}/item-{id}.yaml`. For commit we want `{repoPath}/.fleetlift/knowledge/items/item-{id}.yaml`. We pass `repoPath + "/.fleetlift/knowledge"` as baseDir and `"items"` as taskID.

Add this helper at the bottom of `handlers_knowledge.go`:

```go
import "github.com/tinkerloft/fleetlift/internal/knowledge"

func newRepoKnowledgeStore(repoPath string) *knowledge.Store {
    return knowledge.NewStore(repoPath + "/.fleetlift/knowledge")
}
```

**Step 6: Run tests to verify they pass**

```bash
cd /Users/andrew/dev/code/projects/fleetlift-web && go test ./internal/server/... -run TestKnowledge -v
```
Expected: PASS

**Step 7: Run full test suite + lint**

```bash
cd /Users/andrew/dev/code/projects/fleetlift-web && go test ./... && make lint
```
Expected: all pass, no lint errors

---

### Task 3: Update CORS for PUT/DELETE

Already covered in Task 1, Step 4. Verify it's in place after both tasks are done.

---

## Verification Checklist

- [ ] `POST /api/v1/tasks/{id}/retry` returns `{workflow_id}` for valid YAML
- [ ] Retry with `failed_only: true` filters task groups to only failed ones
- [ ] `GET /api/v1/knowledge` returns `{items: []}` with empty store
- [ ] `POST /api/v1/knowledge` creates item, returns 201 with `id`
- [ ] `GET /api/v1/knowledge/{id}` returns item or 404
- [ ] `PUT /api/v1/knowledge/{id}` updates mutable fields
- [ ] `DELETE /api/v1/knowledge/{id}` removes item
- [ ] `POST /api/v1/knowledge/bulk` approves/deletes multiple items
- [ ] `POST /api/v1/knowledge/commit` writes approved items to repo path
- [ ] `go test ./...` passes
- [ ] `make lint` passes
