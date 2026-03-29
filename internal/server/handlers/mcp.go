package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// MCPHandler handles MCP sidecar API endpoints.
type MCPHandler struct {
	db             *sqlx.DB
	knowledgeStore knowledge.Store
}

// NewMCPHandler creates a new MCPHandler.
func NewMCPHandler(db *sqlx.DB, knowledgeStore knowledge.Store) *MCPHandler {
	return &MCPHandler{db: db, knowledgeStore: knowledgeStore}
}

func mcpClaims(w http.ResponseWriter, r *http.Request) *auth.MCPClaims {
	claims := auth.MCPClaimsFromContext(r.Context())
	if claims == nil {
		writeMCPErr(w, http.StatusUnauthorized, "unauthorized")
		return nil
	}
	return claims
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// truncateTitle returns the first line of s, truncated to maxLen runes with "..." if needed.
func truncateTitle(s string, maxLen int) string {
	// Take first line only
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	runes := []rune(s)
	if maxLen < 4 {
		maxLen = 4
	}
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

func writeMCPErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeMCPJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// activeStepRunID finds the running step for a given run.
func (h *MCPHandler) activeStepRunID(ctx context.Context, runID string) (string, error) {
	var id string
	err := h.db.GetContext(ctx, &id,
		`SELECT id FROM step_runs WHERE run_id = $1 AND status = 'running' LIMIT 1`, runID)
	return id, err
}

// HandleGetRun returns run details including step summaries.
// GET /api/mcp/run
func (h *MCPHandler) HandleGetRun(w http.ResponseWriter, r *http.Request) {
	claims := mcpClaims(w, r)
	if claims == nil {
		return
	}

	var run model.Run
	err := h.db.GetContext(r.Context(), &run,
		`SELECT * FROM runs WHERE id = $1 AND team_id = $2`, claims.RunID, claims.TeamID)
	if err != nil {
		writeMCPErr(w, http.StatusNotFound, "run not found")
		return
	}

	type stepSummary struct {
		ID     string           `json:"id" db:"id"`
		StepID string           `json:"step_id" db:"step_id"`
		Status model.StepStatus `json:"status" db:"status"`
	}
	var steps []stepSummary
	err = h.db.SelectContext(r.Context(), &steps,
		`SELECT id, step_id, status FROM step_runs WHERE run_id = $1 ORDER BY created_at`, claims.RunID)
	if err != nil {
		slog.Error("mcp: failed to query step_runs", "error", err)
		writeMCPErr(w, http.StatusInternalServerError, "failed to query steps")
		return
	}

	// Find current running step.
	var currentStep string
	for _, s := range steps {
		if s.Status == model.StepStatusRunning {
			currentStep = s.StepID
			break
		}
	}

	writeMCPJSON(w, http.StatusOK, map[string]any{
		"run_id":       run.ID,
		"workflow":     run.WorkflowTitle,
		"parameters":   run.Parameters,
		"status":       run.Status,
		"current_step": currentStep,
		"steps":        steps,
	})
}

// HandleGetStepOutput returns the output and diff for a step run.
// GET /api/mcp/steps/{stepID}/output
func (h *MCPHandler) HandleGetStepOutput(w http.ResponseWriter, r *http.Request) {
	claims := mcpClaims(w, r)
	if claims == nil {
		return
	}
	stepID := chi.URLParam(r, "stepID")

	var sr model.StepRun
	err := h.db.GetContext(r.Context(), &sr,
		`SELECT * FROM step_runs WHERE step_id = $1 AND run_id = $2 LIMIT 1`, stepID, claims.RunID)
	if err != nil {
		writeMCPErr(w, http.StatusNotFound, "step run not found")
		return
	}

	var diff string
	if sr.Diff != nil {
		diff = *sr.Diff
	}

	writeMCPJSON(w, http.StatusOK, map[string]any{
		"output": sr.Output,
		"diff":   diff,
	})
}

// HandleGetKnowledge returns approved knowledge items for the run's workflow.
// GET /api/mcp/knowledge?q=...&max=...
func (h *MCPHandler) HandleGetKnowledge(w http.ResponseWriter, r *http.Request) {
	claims := mcpClaims(w, r)
	if claims == nil {
		return
	}

	query := r.URL.Query().Get("q")
	maxItems := 10
	if m := r.URL.Query().Get("max"); m != "" {
		if v, err := strconv.Atoi(m); err == nil && v > 0 {
			maxItems = min(v, 100)
		}
	}

	// Resolve workflow_template UUID from the run's workflow slug.
	// Builtin templates may not have a workflow_templates row — return empty list in that case.
	var templateID string
	_ = h.db.GetContext(r.Context(), &templateID,
		`SELECT wt.id FROM workflow_templates wt JOIN runs r ON r.workflow_id = wt.slug AND wt.team_id = r.team_id
		 WHERE r.id = $1 AND r.team_id = $2`, claims.RunID, claims.TeamID)
	if templateID == "" {
		// No DB template — builtin workflow, no knowledge items to return.
		writeMCPJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}

	// When a text query is provided, fetch a larger batch so client-side filtering
	// doesn't silently under-deliver results. Without a query, use the exact limit.
	fetchLimit := maxItems
	if query != "" {
		fetchLimit = maxItems * 10 // fetch more to filter from
	}

	items, err := h.knowledgeStore.ListApprovedByWorkflow(r.Context(), claims.TeamID, templateID, fetchLimit)
	if err != nil {
		slog.Error("mcp: failed to list knowledge", "error", err)
		writeMCPErr(w, http.StatusInternalServerError, "failed to list knowledge")
		return
	}

	// Filter by query text client-side if provided, then cap at maxItems.
	if query != "" {
		queryLower := strings.ToLower(query)
		filtered := make([]model.KnowledgeItem, 0, len(items))
		for _, item := range items {
			if strings.Contains(strings.ToLower(item.Summary), queryLower) ||
				strings.Contains(strings.ToLower(item.Details), queryLower) {
				filtered = append(filtered, item)
				if len(filtered) >= maxItems {
					break
				}
			}
		}
		items = filtered
	}

	writeMCPJSON(w, http.StatusOK, map[string]any{"items": items})
}

// HandleCreateArtifact stores an artifact for the active step run.
// POST /api/mcp/artifacts
func (h *MCPHandler) HandleCreateArtifact(w http.ResponseWriter, r *http.Request) {
	claims := mcpClaims(w, r)
	if claims == nil {
		return
	}

	var body struct {
		Name        string `json:"name"`
		Content     string `json:"content"`
		ContentType string `json:"content_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeMCPErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Name == "" {
		writeMCPErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if strings.Contains(body.Name, "..") {
		writeMCPErr(w, http.StatusBadRequest, "name must not contain '..'")
		return
	}
	if len(body.Content) > 1024*1024 {
		writeMCPErr(w, http.StatusRequestEntityTooLarge, "content exceeds 1MB limit")
		return
	}
	if body.ContentType == "" {
		body.ContentType = "text/plain"
	}

	stepRunID, err := h.activeStepRunID(r.Context(), claims.RunID)
	if err != nil {
		writeMCPErr(w, http.StatusNotFound, "no active step run")
		return
	}

	artifactID := uuid.New().String()
	_, err = h.db.ExecContext(r.Context(),
		`INSERT INTO artifacts (id, step_run_id, name, path, size_bytes, content_type, storage, data)
		 VALUES ($1, $2, $3, $4, $5, $6, 'inline', $7)`,
		artifactID, stepRunID, body.Name, body.Name, len(body.Content), body.ContentType, []byte(body.Content))
	if err != nil {
		slog.Error("mcp: failed to insert artifact", "error", err)
		writeMCPErr(w, http.StatusInternalServerError, "failed to create artifact")
		return
	}

	writeMCPJSON(w, http.StatusCreated, map[string]string{"artifact_id": artifactID})
}

// HandleAddLearning saves a new knowledge item from the running agent.
// POST /api/mcp/knowledge
func (h *MCPHandler) HandleAddLearning(w http.ResponseWriter, r *http.Request) {
	claims := mcpClaims(w, r)
	if claims == nil {
		return
	}

	var body struct {
		Type       string   `json:"type"`
		Summary    string   `json:"summary"`
		Details    string   `json:"details"`
		Confidence float64  `json:"confidence"`
		Tags       []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeMCPErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	knowledgeType := model.KnowledgeType(body.Type)
	switch knowledgeType {
	case model.KnowledgeTypePattern, model.KnowledgeTypeCorrection,
		model.KnowledgeTypeGotcha, model.KnowledgeTypeContext:
		// valid
	default:
		writeMCPErr(w, http.StatusBadRequest, "type must be one of: pattern, correction, gotcha, context")
		return
	}
	if body.Summary == "" {
		writeMCPErr(w, http.StatusBadRequest, "summary is required")
		return
	}

	// Step run ID is optional — agent may add learnings between step transitions.
	stepRunID, _ := h.activeStepRunID(r.Context(), claims.RunID)

	// Resolve workflow_template UUID from the run's workflow slug.
	// Builtin templates may not have a workflow_templates row, so this is optional.
	var templateID string
	_ = h.db.GetContext(r.Context(), &templateID,
		`SELECT wt.id FROM workflow_templates wt JOIN runs r ON r.workflow_id = wt.slug AND wt.team_id = r.team_id
		 WHERE r.id = $1 AND r.team_id = $2`, claims.RunID, claims.TeamID)

	item := model.KnowledgeItem{
		TeamID:             claims.TeamID,
		WorkflowTemplateID: strPtr(templateID), // nil if builtin template
		StepRunID:          strPtr(stepRunID),
		Type:               knowledgeType,
		Summary:            body.Summary,
		Details:            body.Details,
		Source:             model.KnowledgeSourceAutoCaptured,
		Tags:               pq.StringArray(append([]string{}, body.Tags...)),
		Confidence:         body.Confidence,
		Status:             model.KnowledgeStatusPending,
	}

	saved, err := h.knowledgeStore.Save(r.Context(), item)
	if err != nil {
		slog.Error("mcp: failed to save knowledge item", "error", err)
		writeMCPErr(w, http.StatusInternalServerError, "failed to save knowledge item")
		return
	}

	writeMCPJSON(w, http.StatusCreated, map[string]string{
		"id":     saved.ID,
		"status": string(saved.Status),
	})
}

// HandleSearchKnowledge searches approved knowledge items by query and tags.
// GET /api/mcp/knowledge/search?q=...&tags=...&max=...
func (h *MCPHandler) HandleSearchKnowledge(w http.ResponseWriter, r *http.Request) {
	claims := mcpClaims(w, r)
	if claims == nil {
		return
	}

	query := r.URL.Query().Get("q")
	maxItems := 10
	if m := r.URL.Query().Get("max"); m != "" {
		if v, err := strconv.Atoi(m); err == nil && v > 0 {
			maxItems = min(v, 100)
		}
	}
	var tags []string
	if t := r.URL.Query().Get("tags"); t != "" {
		tags = strings.Split(t, ",")
	}

	items, err := h.knowledgeStore.SearchByTeam(r.Context(), claims.TeamID, query, tags, maxItems)
	if err != nil {
		slog.Error("mcp: failed to search knowledge", "error", err)
		writeMCPErr(w, http.StatusInternalServerError, "failed to search knowledge")
		return
	}

	writeMCPJSON(w, http.StatusOK, map[string]any{"items": items})
}

// HandleUpdateProgress updates the progress on the active step run.
// POST /api/mcp/progress
func (h *MCPHandler) HandleUpdateProgress(w http.ResponseWriter, r *http.Request) {
	claims := mcpClaims(w, r)
	if claims == nil {
		return
	}

	var body struct {
		Percentage float64 `json:"percentage"`
		Message    string  `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeMCPErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Percentage < 0 || body.Percentage > 100 {
		writeMCPErr(w, http.StatusBadRequest, "percentage must be between 0 and 100")
		return
	}
	pctInt := int(body.Percentage)

	stepRunID, err := h.activeStepRunID(r.Context(), claims.RunID)
	if err != nil {
		writeMCPErr(w, http.StatusNotFound, "no active step run")
		return
	}

	progress := map[string]any{
		"percentage": pctInt,
		"message":    body.Message,
	}
	progressJSON, err := json.Marshal(progress)
	if err != nil {
		writeMCPErr(w, http.StatusInternalServerError, "failed to marshal progress")
		return
	}

	_, err = h.db.ExecContext(r.Context(),
		`UPDATE step_runs SET output = jsonb_set(COALESCE(output, '{}'), '{progress}', $1::jsonb) WHERE id = $2`,
		string(progressJSON), stepRunID)
	if err != nil {
		slog.Error("mcp: failed to update progress", "error", err)
		writeMCPErr(w, http.StatusInternalServerError, "failed to update progress")
		return
	}

	writeMCPJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleInboxNotify creates a notification inbox item.
// POST /api/mcp/inbox/notify
func (h *MCPHandler) HandleInboxNotify(w http.ResponseWriter, r *http.Request) {
	claims := mcpClaims(w, r)
	if claims == nil {
		return
	}
	var req struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
		Urgency string `json:"urgency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMCPErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeMCPErr(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.Urgency == "" {
		req.Urgency = "normal"
	}
	switch req.Urgency {
	case "low", "normal", "high":
	default:
		writeMCPErr(w, http.StatusBadRequest, "urgency must be one of: low, normal, high")
		return
	}
	id := uuid.New().String()
	var summary *string
	if req.Summary != "" {
		summary = &req.Summary
	}
	_, err := h.db.ExecContext(r.Context(), `
		INSERT INTO inbox_items (id, team_id, run_id, kind, title, summary, urgency, created_at)
		VALUES ($1,$2,$3,'notify',$4,$5,$6,now())`,
		id, claims.TeamID, claims.RunID, req.Title, summary, req.Urgency,
	)
	if err != nil {
		slog.Error("inbox notify: insert", "err", err)
		writeMCPErr(w, http.StatusInternalServerError, "failed to create notification")
		return
	}
	writeMCPJSON(w, http.StatusCreated, map[string]string{"inbox_item_id": id})
}

var checkpointBranchRe = regexp.MustCompile(`^fleetlift/checkpoint/[a-zA-Z0-9_-]+$`)

// HandleInboxRequestInput creates a request_input inbox item and sets the step to awaiting_input.
// POST /api/mcp/inbox/request_input
func (h *MCPHandler) HandleInboxRequestInput(w http.ResponseWriter, r *http.Request) {
	claims := mcpClaims(w, r)
	if claims == nil {
		return
	}
	var req struct {
		Question         string   `json:"question"`
		StateSummary     string   `json:"state_summary"`
		Options          []string `json:"options"`
		CheckpointBranch string   `json:"checkpoint_branch"`
		Urgency          string   `json:"urgency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMCPErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Question == "" {
		writeMCPErr(w, http.StatusBadRequest, "question is required")
		return
	}
	if req.CheckpointBranch != "" && !checkpointBranchRe.MatchString(req.CheckpointBranch) {
		writeMCPErr(w, http.StatusBadRequest, "invalid checkpoint_branch: must match fleetlift/checkpoint/<alphanumeric-dash-underscore>")
		return
	}
	if req.Urgency == "" {
		req.Urgency = "normal"
	}
	switch req.Urgency {
	case "low", "normal", "high":
	default:
		writeMCPErr(w, http.StatusBadRequest, "urgency must be one of: low, normal, high")
		return
	}

	// Find the active step_run
	var stepRunID string
	err := h.db.QueryRowContext(r.Context(), `
		SELECT id FROM step_runs
		WHERE run_id = $1 AND status NOT IN ('complete','failed','skipped','awaiting_input')
		ORDER BY created_at DESC LIMIT 1`, claims.RunID,
	).Scan(&stepRunID)
	if err != nil {
		slog.Error("inbox request_input: find step_run", "err", err)
		writeMCPErr(w, http.StatusInternalServerError, "could not find active step")
		return
	}

	// Create checkpoint artifact if state_summary provided
	var artifactID *string
	if req.StateSummary != "" {
		aid := uuid.New().String()
		_, err = h.db.ExecContext(r.Context(), `
			INSERT INTO artifacts (id, step_run_id, name, path, size_bytes, content_type, storage, data, created_at)
			VALUES ($1,$2,'agent-checkpoint','/checkpoint.md',$3,'text/markdown','inline',$4,now())`,
			aid, stepRunID, len(req.StateSummary), req.StateSummary,
		)
		if err != nil {
			slog.Error("inbox request_input: create artifact", "err", err)
			writeMCPErr(w, http.StatusInternalServerError, "failed to save state summary")
			return
		}
		artifactID = &aid
	}

	// Create inbox item
	var stateSummary *string
	if req.StateSummary != "" {
		stateSummary = &req.StateSummary
	}
	itemID := uuid.New().String()
	_, err = h.db.ExecContext(r.Context(), `
		INSERT INTO inbox_items
			(id, team_id, run_id, step_run_id, kind, title, summary, question, options, urgency, created_at)
		VALUES ($1,$2,$3,$4,'request_input',$5,$6,$7,$8,$9,now())`,
		itemID, claims.TeamID, claims.RunID, stepRunID,
		truncateTitle(req.Question, 80), // title: short preview for list views
		stateSummary,
		req.Question, // question: full text shown in detail view
		pq.StringArray(req.Options),
		req.Urgency,
	)
	if err != nil {
		slog.Error("inbox request_input: insert item", "err", err)
		writeMCPErr(w, http.StatusInternalServerError, "failed to create inbox item")
		return
	}

	// Mark step as awaiting_input — critical for workflow to enter HITL cycle.
	if _, err := h.db.ExecContext(r.Context(),
		"UPDATE step_runs SET status='awaiting_input' WHERE id=$1", stepRunID,
	); err != nil {
		slog.Error("inbox request_input: update step status", "err", err)
		writeMCPErr(w, http.StatusInternalServerError, "failed to update step status")
		return
	}

	// Store checkpoint metadata on step_runs — needed for continuation to restore working state.
	if req.CheckpointBranch != "" || artifactID != nil {
		if _, err := h.db.ExecContext(r.Context(),
			"UPDATE step_runs SET checkpoint_branch=NULLIF($1,''), checkpoint_artifact_id=$2::uuid WHERE id=$3",
			req.CheckpointBranch,
			artifactID,
			stepRunID,
		); err != nil {
			slog.Error("inbox request_input: store checkpoint fields on step_run", "err", err)
			writeMCPErr(w, http.StatusInternalServerError, "failed to store checkpoint metadata")
			return
		}
	}

	writeMCPJSON(w, http.StatusCreated, map[string]string{
		"inbox_item_id": itemID,
		"status":        "input_requested",
	})
}
