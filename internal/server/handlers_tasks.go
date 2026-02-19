package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// TaskSummary is the API representation of a workflow for list/inbox responses.
type TaskSummary struct {
	WorkflowID string           `json:"workflow_id"`
	Status     model.TaskStatus `json:"status"`
	StartTime  string           `json:"start_time"`
	InboxType  string           `json:"inbox_type,omitempty"`
	IsPaused   bool             `json:"is_paused,omitempty"`
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	workflows, err := s.client.ListWorkflows(r.Context(), statusFilter, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	tasks := make([]TaskSummary, 0, len(workflows))
	for _, wf := range workflows {
		status, _ := s.client.GetWorkflowStatus(r.Context(), wf.WorkflowID)
		tasks = append(tasks, TaskSummary{
			WorkflowID: wf.WorkflowID,
			Status:     status,
			StartTime:  wf.StartTime,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (s *Server) handleGetInbox(w http.ResponseWriter, r *http.Request) {
	running, err := s.client.ListWorkflows(r.Context(), "Running", 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var items []TaskSummary
	for _, wf := range running {
		status, err := s.client.GetWorkflowStatus(r.Context(), wf.WorkflowID)
		if err != nil {
			continue
		}

		inboxType := ""
		isPaused := false

		switch status {
		case model.TaskStatusAwaitingApproval:
			inboxType = "awaiting_approval"
		case model.TaskStatusRunning:
			progress, err := s.client.GetExecutionProgress(r.Context(), wf.WorkflowID)
			if err == nil && progress != nil && progress.IsPaused {
				inboxType = "paused"
				isPaused = true
			}
		}

		if inboxType != "" {
			items = append(items, TaskSummary{
				WorkflowID: wf.WorkflowID,
				Status:     status,
				StartTime:  wf.StartTime,
				InboxType:  inboxType,
				IsPaused:   isPaused,
			})
		}
	}

	// Include recently completed tasks (last 24h) for passive review.
	completed, _ := s.client.ListWorkflows(r.Context(), "Completed", 20)
	for _, wf := range completed {
		t, err := time.Parse("2006-01-02 15:04:05", wf.StartTime)
		if err != nil || time.Since(t) > 24*time.Hour {
			continue
		}
		items = append(items, TaskSummary{
			WorkflowID: wf.WorkflowID,
			Status:     model.TaskStatusCompleted,
			StartTime:  wf.StartTime,
			InboxType:  "completed_review",
		})
	}

	if items == nil {
		items = []TaskSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	status, err := s.client.GetWorkflowStatus(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	writeJSON(w, http.StatusOK, TaskSummary{WorkflowID: id, Status: status})
}

func (s *Server) handleGetDiff(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	diffs, err := s.client.GetWorkflowDiff(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"diffs": diffs})
}

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	logs, err := s.client.GetWorkflowVerifierLogs(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

func (s *Server) handleGetSteering(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	state, err := s.client.GetSteeringState(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleGetProgress(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	progress, err := s.client.GetExecutionProgress(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, progress)
}
