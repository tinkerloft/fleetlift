package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// InboxHandler handles inbox notification endpoints.
type InboxHandler struct {
	db *sqlx.DB
}

// NewInboxHandler creates a new InboxHandler.
func NewInboxHandler(db *sqlx.DB) *InboxHandler {
	return &InboxHandler{db: db}
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
