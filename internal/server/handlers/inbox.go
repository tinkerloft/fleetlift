package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// TemporalSignaler is the interface for sending Temporal signals.
// Extracted as interface for testability.
type TemporalSignaler interface {
	SignalWorkflow(ctx context.Context, workflowID string, runID string, signalName string, arg interface{}) error
}

// InboxHandler handles inbox notification endpoints.
type InboxHandler struct {
	db             *sqlx.DB
	temporalClient TemporalSignaler
}

// NewInboxHandler creates a new InboxHandler.
func NewInboxHandler(db *sqlx.DB, tc TemporalSignaler) *InboxHandler {
	return &InboxHandler{db: db, temporalClient: tc}
}

// List returns unread inbox items for the user's team.
func (h *InboxHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}
	var items []model.InboxItem
	err := h.db.SelectContext(r.Context(), &items,
		`SELECT i.* FROM inbox_items i
		 WHERE i.team_id = $1
		 AND NOT EXISTS (
			SELECT 1 FROM inbox_reads ir
			WHERE ir.inbox_item_id = i.id AND ir.user_id = $2
		 )
		 ORDER BY i.created_at DESC LIMIT 50`,
		teamID, claims.UserID)
	if err != nil {
		slog.Error("inbox list query failed", "error", err, "team_id", teamID)
		writeJSONError(w, http.StatusInternalServerError, "failed to list inbox")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// MarkRead marks an inbox item as read by the current user.
func (h *InboxHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	itemID := chi.URLParam(r, "id")
	_, err := h.db.ExecContext(r.Context(),
		`INSERT INTO inbox_reads (inbox_item_id, user_id, read_at)
		 VALUES ($1, $2, $3)
		 ON CONFLICT DO NOTHING`,
		itemID, claims.UserID, time.Now())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to mark read")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Respond handles a human response to a request_input inbox item.
// POST /api/inbox/{id}/respond
func (h *InboxHandler) Respond(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "id")
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return
	}

	var req struct {
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Answer == "" {
		writeJSONError(w, http.StatusBadRequest, "answer is required")
		return
	}

	// Fetch inbox item — validate ownership and kind
	var item model.InboxItem
	err := h.db.GetContext(r.Context(), &item,
		`SELECT * FROM inbox_items WHERE id=$1 AND team_id=$2`, itemID, teamID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "inbox item not found")
		return
	}
	if item.Kind != "request_input" {
		writeJSONError(w, http.StatusBadRequest, "item is not a request_input")
		return
	}
	if item.AnsweredAt != nil {
		writeJSONError(w, http.StatusConflict, "already answered")
		return
	}

	// Persist answer
	now := time.Now()
	_, err = h.db.ExecContext(r.Context(), `
		UPDATE inbox_items SET answer=$1, answered_at=$2, answered_by=$3 WHERE id=$4`,
		req.Answer, now, claims.UserID, itemID,
	)
	if err != nil {
		slog.Error("inbox respond: store answer", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to store answer")
		return
	}

	// Signal the Temporal workflow
	if item.StepRunID != nil && h.temporalClient != nil {
		var workflowID string
		if err := h.db.QueryRowContext(r.Context(),
			"SELECT temporal_workflow_id FROM step_runs WHERE id=$1", *item.StepRunID,
		).Scan(&workflowID); err == nil && workflowID != "" {
			signal := model.InboxAnswer{Answer: req.Answer, Responder: claims.UserID}
			if err := h.temporalClient.SignalWorkflow(r.Context(), workflowID, "",
				"respond", signal,
			); err != nil {
				slog.Error("inbox respond: signal workflow", "err", err, "workflow_id", workflowID)
				// Non-fatal: answer is stored; workflow will see it on next poll
			}
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
