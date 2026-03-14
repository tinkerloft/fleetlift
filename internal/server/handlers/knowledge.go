package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// KnowledgeHandler handles knowledge item CRUD endpoints.
type KnowledgeHandler struct {
	store knowledge.Store
}

// NewKnowledgeHandler creates a new KnowledgeHandler.
func NewKnowledgeHandler(store knowledge.Store) *KnowledgeHandler {
	return &KnowledgeHandler{store: store}
}

func (h *KnowledgeHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}
	status := r.URL.Query().Get("status")

	items, err := h.store.ListByTeam(r.Context(), teamID, status)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list knowledge items")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *KnowledgeHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}
	id := chi.URLParam(r, "id")

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	status := model.KnowledgeStatus(body.Status)
	if status != model.KnowledgeStatusApproved && status != model.KnowledgeStatusRejected {
		writeJSONError(w, http.StatusBadRequest, "status must be 'approved' or 'rejected'")
		return
	}

	if err := h.store.UpdateStatus(r.Context(), id, teamID, status); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to update knowledge item")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *KnowledgeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}
	id := chi.URLParam(r, "id")
	if err := h.store.Delete(r.Context(), id, teamID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to delete knowledge item")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
