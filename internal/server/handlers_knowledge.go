package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/tinkerloft/fleetlift/internal/knowledge"
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

	filtered := make([]model.KnowledgeItem, 0, len(items))
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
	Type       model.KnowledgeType   `json:"type"`
	Summary    string                `json:"summary"`
	Details    string                `json:"details"`
	Source     model.KnowledgeSource `json:"source"`
	Tags       []string              `json:"tags"`
	Confidence float64               `json:"confidence"`
	TaskID     string                `json:"task_id"`
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

type updateKnowledgeRequest struct {
	Summary    *string                `json:"summary"`
	Details    *string                `json:"details"`
	Tags       []string               `json:"tags"`
	Status     *model.KnowledgeStatus `json:"status"`
	Confidence *float64               `json:"confidence"`
}

// handleUpdateKnowledge handles PUT /api/v1/knowledge/{id}
func (s *Server) handleUpdateKnowledge(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

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

	var req updateKnowledgeRequest
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
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
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

	// Load all items once for approve lookups
	allItems, err := s.knowledgeStore.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	itemByID := make(map[string]model.KnowledgeItem, len(allItems))
	for _, item := range allItems {
		itemByID[item.ID] = item
	}

	var errs []string
	for _, a := range req.Actions {
		switch a.Action {
		case "approve":
			item, ok := itemByID[a.ID]
			if !ok {
				errs = append(errs, fmt.Sprintf("%s: not found", a.ID))
				continue
			}
			item.Status = model.KnowledgeStatusApproved
			if err := s.knowledgeStore.Update(item); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", a.ID, err))
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
// Copies approved items to {repo_path}/.fleetlift/knowledge/items/
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

	repoStore := knowledge.NewStore(req.RepoPath + "/.fleetlift/knowledge")
	var committed int
	var writeErrs []string
	for _, item := range approved {
		if err := repoStore.Write("items", item); err != nil {
			writeErrs = append(writeErrs, fmt.Sprintf("item %s: %v", item.ID, err))
			continue
		}
		committed++
	}

	status := http.StatusOK
	resp := map[string]any{"committed": committed, "repo_path": req.RepoPath}
	if len(writeErrs) > 0 {
		status = http.StatusMultiStatus
		resp["errors"] = writeErrs
	}
	writeJSON(w, status, resp)
}
