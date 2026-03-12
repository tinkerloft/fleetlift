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

	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}

	// Look up workflow template
	t, err := h.registry.Get(r.Context(), teamID, req.WorkflowID)
	if err != nil {
		http.Error(w, "workflow not found", http.StatusNotFound)
		return
	}

	// Parse workflow definition
	var def model.WorkflowDef
	if err := model.ParseWorkflowYAML([]byte(t.YAMLBody), &def); err != nil {
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
		RunID:              runID,
		TeamID:             teamID,
		WorkflowTemplateID: t.ID,
		WorkflowDef:        def,
		Parameters:         req.Parameters,
	})
	if err != nil {
		http.Error(w, "failed to start workflow", http.StatusInternalServerError)
		return
	}

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

	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}
	var runs []model.Run
	err := h.db.SelectContext(r.Context(), &runs,
		`SELECT * FROM runs WHERE team_id = $1 ORDER BY created_at DESC LIMIT 50`, teamID)
	if err != nil {
		http.Error(w, "failed to list runs", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": runs})
}

// Get returns a single run with its step runs.
func (h *RunsHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}
	runID := chi.URLParam(r, "id")

	run := getRunForTeam(r.Context(), h.db, w, runID, teamID)
	if run == nil {
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
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}
	runID := chi.URLParam(r, "id")

	if getRunForTeam(r.Context(), h.db, w, runID, teamID) == nil {
		return
	}

	var logs []model.StepRunLog
	err := h.db.SelectContext(r.Context(), &logs,
		`SELECT l.* FROM step_run_logs l
		 JOIN step_runs s ON l.step_run_id = s.id
		 WHERE s.run_id = $1 ORDER BY l.seq`, runID)
	if err != nil {
		http.Error(w, "failed to get logs", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": logs})
}

// Diff returns the git diff for a run's transform steps.
func (h *RunsHandler) Diff(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}
	runID := chi.URLParam(r, "id")

	if getRunForTeam(r.Context(), h.db, w, runID, teamID) == nil {
		return
	}

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
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}
	runID := chi.URLParam(r, "id")

	if getRunForTeam(r.Context(), h.db, w, runID, teamID) == nil {
		return
	}

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

// IssueSSETicket issues a short-lived single-use ticket for SSE connections.
// EventSource does not support custom headers, so Bearer auth cannot be used directly.
// The ticket is bound to the resource ID in the URL ({id} path param).
func (h *RunsHandler) IssueSSETicket(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	resourceID := chi.URLParam(r, "id")
	ticket := auth.IssueSSETicket(claims, resourceID)
	writeJSON(w, http.StatusOK, map[string]string{"ticket": ticket})
}

// claimsFromSSERequest resolves claims from a ticket query param (for SSE) or from the request context.
// resourceID is the run or step run ID the ticket must be bound to.
func claimsFromSSERequest(r *http.Request, resourceID string) (*auth.Claims, bool) {
	if ticket := r.URL.Query().Get("ticket"); ticket != "" {
		c, ok := auth.ConsumeSSETicket(ticket, resourceID)
		return c, ok
	}
	c := auth.ClaimsFromContext(r.Context())
	return c, c != nil
}

// Stream sends SSE events for a run's logs and status updates.
func (h *RunsHandler) Stream(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	claims, ok := claimsFromSSERequest(r, runID)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}
	if getRunForTeam(r.Context(), h.db, w, runID, teamID) == nil {
		return
	}
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

// StepLogs sends SSE log lines for a specific step run.
func (h *RunsHandler) StepLogs(w http.ResponseWriter, r *http.Request) {
	stepRunID := chi.URLParam(r, "id")
	claims, ok := claimsFromSSERequest(r, stepRunID)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}

	// Verify the step_run belongs to a run owned by this team.
	var count int
	if err := h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM step_runs s JOIN runs r ON s.run_id = r.id WHERE s.id = $1 AND r.team_id = $2`,
		stepRunID, teamID).Scan(&count); err != nil || count == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

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
			var logs []model.StepRunLog
			err := h.db.SelectContext(r.Context(), &logs,
				`SELECT * FROM step_run_logs WHERE step_run_id = $1 AND id > $2 ORDER BY id`,
				stepRunID, cursor)
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

			// Stop once the step run is terminal
			var status string
			if h.db.QueryRowContext(r.Context(),
				`SELECT status FROM step_runs WHERE id = $1`, stepRunID).Scan(&status) == nil {
				switch status {
				case "complete", "failed", "cancelled":
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

func stepWorkflowID(runID, stepID string) string {
	return fmt.Sprintf("%s-%s", runID, stepID)
}

func (h *RunsHandler) signalRun(w http.ResponseWriter, r *http.Request, signalName string, payload any) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}
	runID := chi.URLParam(r, "id")

	if getRunForTeam(r.Context(), h.db, w, runID, teamID) == nil {
		return
	}

	// Find the currently paused step and use its stored temporal_workflow_id for precise routing.
	// Using temporal_workflow_id avoids reconstructing the ID from step_id, which fails for fan-out
	// steps (which use indexed IDs like {runID}-{stepID}-{index}).
	var temporalWFID string
	err := h.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(temporal_workflow_id, '') FROM step_runs
		 WHERE run_id = $1 AND status = 'awaiting_input'
		 ORDER BY created_at DESC LIMIT 1`,
		runID,
	).Scan(&temporalWFID)
	if err != nil || temporalWFID == "" {
		// No awaiting_input step found — fall back to signalling the parent DAGWorkflow.
		// Note: cancel is registered on StepWorkflow, not DAGWorkflow, so cancel-while-running
		// will be silently dropped here. Fix: query status='running' step for cancel path.
		var parentTemporalID string
		if dbErr := h.db.GetContext(r.Context(), &parentTemporalID,
			`SELECT temporal_id FROM runs WHERE id = $1`, runID); dbErr != nil {
			http.Error(w, "run not found", http.StatusNotFound)
			return
		}
		err = h.temporal.SignalWorkflow(r.Context(), parentTemporalID, "", signalName, payload)
		if err != nil {
			http.Error(w, "failed to signal workflow", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "signaled"})
		return
	}

	// Signal the specific child StepWorkflow using its stored workflow ID.
	err = h.temporal.SignalWorkflow(r.Context(), temporalWFID, "", signalName, payload)
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
