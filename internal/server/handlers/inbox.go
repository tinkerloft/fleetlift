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
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	teamID := firstTeamID(claims)
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
		http.Error(w, "failed to list inbox", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, items)
}

// MarkRead marks an inbox item as read by the current user.
func (h *InboxHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	itemID := chi.URLParam(r, "id")
	_, err := h.db.ExecContext(r.Context(),
		`INSERT INTO inbox_reads (inbox_item_id, user_id, read_at)
		 VALUES ($1, $2, $3)
		 ON CONFLICT DO NOTHING`,
		itemID, claims.UserID, time.Now())
	if err != nil {
		http.Error(w, "failed to mark read", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
