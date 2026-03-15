package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
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
		ID     string           `json:"id"`
		StepID string           `json:"step_id"`
		Status model.StepStatus `json:"status"`
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
			maxItems = v
		}
	}

	// Resolve workflow_id from the run.
	var workflowID string
	err := h.db.GetContext(r.Context(), &workflowID,
		`SELECT workflow_id FROM runs WHERE id = $1 AND team_id = $2`, claims.RunID, claims.TeamID)
	if err != nil {
		writeMCPErr(w, http.StatusNotFound, "run not found")
		return
	}

	items, err := h.knowledgeStore.ListApprovedByWorkflow(r.Context(), claims.TeamID, workflowID, maxItems)
	if err != nil {
		slog.Error("mcp: failed to list knowledge", "error", err)
		writeMCPErr(w, http.StatusInternalServerError, "failed to list knowledge")
		return
	}

	// Filter by query text client-side if provided.
	if query != "" {
		queryLower := strings.ToLower(query)
		filtered := make([]model.KnowledgeItem, 0, len(items))
		for _, item := range items {
			if strings.Contains(strings.ToLower(item.Summary), queryLower) ||
				strings.Contains(strings.ToLower(item.Details), queryLower) {
				filtered = append(filtered, item)
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

	stepRunID, err := h.activeStepRunID(r.Context(), claims.RunID)
	if err != nil {
		writeMCPErr(w, http.StatusNotFound, "no active step run")
		return
	}

	// Resolve workflow_id from the run.
	var workflowID string
	err = h.db.GetContext(r.Context(), &workflowID,
		`SELECT workflow_id FROM runs WHERE id = $1 AND team_id = $2`, claims.RunID, claims.TeamID)
	if err != nil {
		writeMCPErr(w, http.StatusNotFound, "run not found")
		return
	}

	item := model.KnowledgeItem{
		TeamID:             claims.TeamID,
		WorkflowTemplateID: workflowID,
		StepRunID:          stepRunID,
		Type:               knowledgeType,
		Summary:            body.Summary,
		Details:            body.Details,
		Source:             model.KnowledgeSourceAutoCaptured,
		Tags:               pq.StringArray(body.Tags),
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
			maxItems = v
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
		Percentage int    `json:"percentage"`
		Message    string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeMCPErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Percentage < 0 || body.Percentage > 100 {
		writeMCPErr(w, http.StatusBadRequest, "percentage must be between 0 and 100")
		return
	}

	stepRunID, err := h.activeStepRunID(r.Context(), claims.RunID)
	if err != nil {
		writeMCPErr(w, http.StatusNotFound, "no active step run")
		return
	}

	progress := map[string]any{
		"percentage": body.Percentage,
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
