package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"go.temporal.io/sdk/client"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/server/notify"
	"github.com/tinkerloft/fleetlift/internal/template"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

// RunsHandler handles run lifecycle endpoints including SSE streaming.
type RunsHandler struct {
	db       *sqlx.DB
	temporal client.Client
	registry *template.Registry
	Notify   *notify.Listener
}

// NewRunsHandler creates a new RunsHandler.
func NewRunsHandler(db *sqlx.DB, temporal client.Client, registry *template.Registry, nl *notify.Listener) *RunsHandler {
	return &RunsHandler{db: db, temporal: temporal, registry: registry, Notify: nl}
}

type createRunRequest struct {
	WorkflowID string         `json:"workflow_id"`
	Parameters map[string]any `json:"parameters"`
}

// Create starts a new workflow run.
func (h *RunsHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}

	// Look up workflow template
	t, err := h.registry.Get(r.Context(), teamID, req.WorkflowID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "workflow not found")
		return
	}

	// Parse workflow definition
	var def model.WorkflowDef
	if err := model.ParseWorkflowYAML([]byte(t.YAMLBody), &def); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "invalid workflow definition")
		return
	}

	if errs := workflow.ValidateWorkflow(def, req.Parameters); len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":             "workflow validation failed",
			"validation_errors": errs,
		})
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
		writeJSONError(w, http.StatusInternalServerError, "failed to create run")
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
		writeJSONError(w, http.StatusInternalServerError, "failed to start workflow")
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
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
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
		writeJSONError(w, http.StatusInternalServerError, "failed to list runs")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": runs})
}

// Get returns a single run with its step runs.
func (h *RunsHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
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

	// Return the raw YAML so the frontend can parse it client-side.
	// We deliberately avoid serializing WorkflowDef (which has no json: tags —
	// they must stay stable for Temporal history deserialization).
	var workflowYAML string
	if t, err := h.registry.Get(r.Context(), teamID, run.WorkflowID); err == nil {
		workflowYAML = t.YAMLBody
	} else {
		var t model.WorkflowTemplate
		if dbErr := h.db.GetContext(r.Context(), &t,
			`SELECT yaml_body FROM workflow_templates WHERE id = $1`, run.WorkflowID,
		); dbErr == nil {
			workflowYAML = t.YAMLBody
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"run":           run,
		"steps":         steps,
		"workflow_yaml": workflowYAML,
	})
}

// Logs returns log lines for a run.
func (h *RunsHandler) Logs(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
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
		writeJSONError(w, http.StatusInternalServerError, "failed to get logs")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": logs})
}

// Diff returns the git diff for a run's transform steps.
func (h *RunsHandler) Diff(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
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
		writeJSONError(w, http.StatusInternalServerError, "failed to get diffs")
		return
	}

	writeJSON(w, http.StatusOK, diffs)
}

// Output returns the structured output for a run.
func (h *RunsHandler) Output(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
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
		writeJSONError(w, http.StatusInternalServerError, "failed to get outputs")
		return
	}

	writeJSON(w, http.StatusOK, outputs)
}

// streamFlush queries and emits new log lines and a status event for the run.
// Returns true if the run has reached a terminal state.
func (h *RunsHandler) streamFlush(w http.ResponseWriter, r *http.Request, flusher http.Flusher, runID string, cursor *int64) bool {
	// Stream new log lines
	var logs []model.StepRunLog
	err := h.db.SelectContext(r.Context(), &logs,
		`SELECT l.* FROM step_run_logs l
		 JOIN step_runs s ON l.step_run_id = s.id
		 WHERE s.run_id = $1 AND l.id > $2 ORDER BY l.id`, runID, *cursor)
	if err == nil {
		for _, log := range logs {
			fmt.Fprintf(w, "data: %s\n\n", mustJSON(log))
			if log.ID > *cursor {
				*cursor = log.ID
			}
		}
		if len(logs) > 0 {
			flusher.Flush()
		}
	}

	// Send status update
	var run model.Run
	if h.db.GetContext(r.Context(), &run, `SELECT * FROM runs WHERE id = $1`, runID) == nil {
		fmt.Fprintf(w, "event: status\ndata: %s\n\n", mustJSON(map[string]string{
			"status": string(run.Status),
		}))
		flusher.Flush()
		return isRunTerminal(run.Status)
	}
	return false
}

// Stream sends SSE events for a run's logs and status updates.
// Auth is handled by the router's auth middleware (cookie-based for EventSource).
func (h *RunsHandler) Stream(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
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
		writeJSONError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	var cursor int64

	// If the notify listener is not available, fall back to nil and the loop below
	// will block on context cancellation only (tests or degraded mode).
	if h.Notify == nil {
		// Nil listener — just do one flush and block until client disconnects.
		h.streamFlush(w, r, flusher, runID, &cursor)
		<-r.Context().Done()
		return
	}

	ch := h.Notify.Subscribe(runID)
	defer h.Notify.Unsubscribe(runID, ch)

	// Immediately flush current state before waiting for notifications.
	if h.streamFlush(w, r, flusher, runID, &cursor) {
		return
	}

	for {
		select {
		case <-ch:
			if h.streamFlush(w, r, flusher, runID, &cursor) {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

// stepLogsFlush queries and emits new log lines for the step run.
// Returns true if the step run has reached a terminal state.
func (h *RunsHandler) stepLogsFlush(w http.ResponseWriter, r *http.Request, flusher http.Flusher, stepRunID string, cursor *int64) bool {
	var logs []model.StepRunLog
	err := h.db.SelectContext(r.Context(), &logs,
		`SELECT * FROM step_run_logs WHERE step_run_id = $1 AND id > $2 ORDER BY id`,
		stepRunID, *cursor)
	if err == nil {
		for _, log := range logs {
			fmt.Fprintf(w, "data: %s\n\n", mustJSON(log))
			if log.ID > *cursor {
				*cursor = log.ID
			}
		}
		if len(logs) > 0 {
			flusher.Flush()
		}
	}

	// Stop once the step run is terminal
	var status string
	if h.db.QueryRowContext(r.Context(),
		`SELECT status FROM step_runs WHERE id = $1`, stepRunID).Scan(&status) == nil {
		switch status {
		case "complete", "failed", "cancelled":
			return true
		}
	}
	return false
}

// StepLogs sends SSE log lines for a specific step run.
// Auth is handled by the router's auth middleware (cookie-based for EventSource).
func (h *RunsHandler) StepLogs(w http.ResponseWriter, r *http.Request) {
	stepRunID := chi.URLParam(r, "id")
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}

	// Verify the step_run belongs to a run owned by this team, and retrieve the run_id
	// so we can subscribe to the run-level notify channel.
	var runID string
	if err := h.db.QueryRowContext(r.Context(),
		`SELECT r.id FROM step_runs s JOIN runs r ON s.run_id = r.id WHERE s.id = $1 AND r.team_id = $2`,
		stepRunID, teamID).Scan(&runID); err != nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	// Send an SSE comment immediately so the HTTP response headers are flushed
	// to the client. Without this, Go buffers the headers until the first real
	// write, leaving EventSource in a pending state with no onopen event.
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	var cursor int64

	if h.Notify == nil {
		// Nil listener — just do one flush and block until client disconnects.
		h.stepLogsFlush(w, r, flusher, stepRunID, &cursor)
		<-r.Context().Done()
		return
	}

	// Subscribe on the run_id — log insert and step status updates both notify on run_id.
	ch := h.Notify.Subscribe(runID)
	defer h.Notify.Unsubscribe(runID, ch)

	// Immediately flush current state before waiting for notifications.
	if h.stepLogsFlush(w, r, flusher, stepRunID, &cursor) {
		return
	}

	for {
		select {
		case <-ch:
			if h.stepLogsFlush(w, r, flusher, stepRunID, &cursor) {
				return
			}
		case <-r.Context().Done():
			return
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	h.signalRun(w, r, string(workflow.SignalSteer), payload)
}

// Cancel signals cancellation for a run.
func (h *RunsHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return
	}
	runID := chi.URLParam(r, "id")
	run := getRunForTeam(r.Context(), h.db, w, runID, teamID)
	if run == nil {
		return
	}

	// Cancel the parent DAGWorkflow via Temporal's CancelWorkflow API.
	// This propagates cancellation to all child workflows and activities.
	if err := h.temporal.CancelWorkflow(r.Context(), run.TemporalID, ""); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to cancel workflow")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func stepWorkflowID(runID, stepID string) string {
	return fmt.Sprintf("%s-%s", runID, stepID)
}

func (h *RunsHandler) signalRun(w http.ResponseWriter, r *http.Request, signalName string, payload any) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
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
			writeJSONError(w, http.StatusNotFound, "run not found")
			return
		}
		err = h.temporal.SignalWorkflow(r.Context(), parentTemporalID, "", signalName, payload)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to signal workflow")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "signaled"})
		return
	}

	// Signal the specific child StepWorkflow using its stored workflow ID.
	err = h.temporal.SignalWorkflow(r.Context(), temporalWFID, "", signalName, payload)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to signal workflow")
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
