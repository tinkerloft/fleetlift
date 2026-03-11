package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"go.temporal.io/sdk/client"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/template"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

// RunsHandler handles run lifecycle endpoints including SSE streaming.
type RunsHandler struct {
	db       *sqlx.DB
	temporal client.Client
	registry *template.Registry
}

// NewRunsHandler creates a new RunsHandler.
func NewRunsHandler(db *sqlx.DB, temporal client.Client, registry *template.Registry) *RunsHandler {
	return &RunsHandler{db: db, temporal: temporal, registry: registry}
}

type createRunRequest struct {
	WorkflowID string         `json:"workflow_id"`
	Parameters map[string]any `json:"parameters"`
}

// Create starts a new workflow run.
func (h *RunsHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req createRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	teamID := firstTeamID(claims)

	// Look up workflow template
	t, err := h.registry.Get(r.Context(), teamID, req.WorkflowID)
	if err != nil {
		http.Error(w, "workflow not found", http.StatusNotFound)
		return
	}

	// Parse workflow definition
	var def model.WorkflowDef
	if err := json.Unmarshal([]byte(t.YAMLBody), &def); err != nil {
		// Try YAML parse via model
		http.Error(w, "invalid workflow definition", http.StatusInternalServerError)
		return
	}

	runID := uuid.New().String()
	temporalID := fmt.Sprintf("fl-%s-%s", req.WorkflowID, runID[:8])

	// Insert run record
	_, err = h.db.ExecContext(r.Context(),
		`INSERT INTO runs (id, team_id, workflow_id, workflow_title, parameters, status, temporal_id, triggered_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		runID, teamID, req.WorkflowID, t.Title,
		mustMarshal(req.Parameters), string(model.RunStatusPending),
		temporalID, claims.UserID)
	if err != nil {
		http.Error(w, "failed to create run", http.StatusInternalServerError)
		return
	}

	// Start Temporal workflow
	_, err = h.temporal.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{
		ID:        temporalID,
		TaskQueue: "fleetlift",
	}, "DAGWorkflow", workflow.DAGInput{
		RunID:       runID,
		WorkflowDef: def,
		Parameters:  req.Parameters,
	})
	if err != nil {
		http.Error(w, "failed to start workflow", http.StatusInternalServerError)
		return
	}

	// Update status to running
	_, _ = h.db.ExecContext(r.Context(),
		`UPDATE runs SET status = $1, started_at = $2 WHERE id = $3`,
		string(model.RunStatusRunning), time.Now(), runID)

	writeJSON(w, http.StatusCreated, map[string]string{
		"id":          runID,
		"temporal_id": temporalID,
	})
}

// List returns runs for the user's team.
func (h *RunsHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	teamID := firstTeamID(claims)
	var runs []model.Run
	err := h.db.SelectContext(r.Context(), &runs,
		`SELECT * FROM runs WHERE team_id = $1 ORDER BY created_at DESC LIMIT 50`, teamID)
	if err != nil {
		http.Error(w, "failed to list runs", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, runs)
}

// Get returns a single run with its step runs.
func (h *RunsHandler) Get(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")

	var run model.Run
	err := h.db.GetContext(r.Context(), &run, `SELECT * FROM runs WHERE id = $1`, runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	var steps []model.StepRun
	_ = h.db.SelectContext(r.Context(), &steps,
		`SELECT * FROM step_runs WHERE run_id = $1 ORDER BY created_at`, runID)

	writeJSON(w, http.StatusOK, map[string]any{
		"run":   run,
		"steps": steps,
	})
}

// Logs returns log lines for a run.
func (h *RunsHandler) Logs(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")

	var logs []model.StepRunLog
	err := h.db.SelectContext(r.Context(), &logs,
		`SELECT l.* FROM step_run_logs l
		 JOIN step_runs s ON l.step_run_id = s.id
		 WHERE s.run_id = $1 ORDER BY l.seq`, runID)
	if err != nil {
		http.Error(w, "failed to get logs", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, logs)
}

// Diff returns the git diff for a run's transform steps.
func (h *RunsHandler) Diff(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")

	var diffs []struct {
		StepID string `db:"step_id" json:"step_id"`
		Diff   string `db:"diff" json:"diff"`
	}
	err := h.db.SelectContext(r.Context(), &diffs,
		`SELECT step_id, diff FROM step_runs WHERE run_id = $1 AND diff IS NOT NULL AND diff != ''`, runID)
	if err != nil {
		http.Error(w, "failed to get diffs", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, diffs)
}

// Output returns the structured output for a run.
func (h *RunsHandler) Output(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")

	var outputs []struct {
		StepID string         `db:"step_id" json:"step_id"`
		Output map[string]any `db:"output" json:"output"`
	}
	err := h.db.SelectContext(r.Context(), &outputs,
		`SELECT step_id, output FROM step_runs WHERE run_id = $1 AND output IS NOT NULL`, runID)
	if err != nil {
		http.Error(w, "failed to get outputs", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, outputs)
}

// Stream sends SSE events for a run's logs and status updates.
func (h *RunsHandler) Stream(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	var cursor int64
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			// Stream new log lines
			var logs []model.StepRunLog
			err := h.db.SelectContext(r.Context(), &logs,
				`SELECT l.* FROM step_run_logs l
				 JOIN step_runs s ON l.step_run_id = s.id
				 WHERE s.run_id = $1 AND l.id > $2 ORDER BY l.id`, runID, cursor)
			if err != nil {
				continue
			}
			for _, log := range logs {
				fmt.Fprintf(w, "data: %s\n\n", mustJSON(log))
				if log.ID > cursor {
					cursor = log.ID
				}
			}
			if len(logs) > 0 {
				flusher.Flush()
			}

			// Send status update
			var run model.Run
			if h.db.GetContext(r.Context(), &run, `SELECT * FROM runs WHERE id = $1`, runID) == nil {
				fmt.Fprintf(w, "event: status\ndata: %s\n\n", mustJSON(map[string]string{
					"status": string(run.Status),
				}))
				flusher.Flush()

				// Stop streaming if run is terminal
				if isRunTerminal(run.Status) {
					return
				}
			}
		}
	}
}

// Approve signals approval for a paused step.
func (h *RunsHandler) Approve(w http.ResponseWriter, r *http.Request) {
	h.signalRun(w, r, string(workflow.SignalApprove), nil)
}

// Reject signals rejection for a paused step.
func (h *RunsHandler) Reject(w http.ResponseWriter, r *http.Request) {
	h.signalRun(w, r, string(workflow.SignalReject), nil)
}

// Steer signals a steering instruction for a paused step.
func (h *RunsHandler) Steer(w http.ResponseWriter, r *http.Request) {
	var payload workflow.SteerPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	h.signalRun(w, r, string(workflow.SignalSteer), payload)
}

// Cancel signals cancellation for a run.
func (h *RunsHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	h.signalRun(w, r, string(workflow.SignalCancel), nil)
}

func (h *RunsHandler) signalRun(w http.ResponseWriter, r *http.Request, signalName string, payload any) {
	runID := chi.URLParam(r, "id")

	var temporalID string
	err := h.db.GetContext(r.Context(), &temporalID,
		`SELECT temporal_id FROM runs WHERE id = $1`, runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	err = h.temporal.SignalWorkflow(r.Context(), temporalID, "", signalName, payload)
	if err != nil {
		http.Error(w, "failed to signal workflow", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "signaled"})
}

func isRunTerminal(s model.RunStatus) bool {
	switch s {
	case model.RunStatusComplete, model.RunStatusFailed, model.RunStatusCancelled:
		return true
	}
	return false
}
