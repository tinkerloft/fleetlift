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
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	teamID := firstTeamID(claims)
	status := r.URL.Query().Get("status")

	items, err := h.store.ListByTeam(r.Context(), teamID, status)
	if err != nil {
		http.Error(w, "failed to list knowledge items", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *KnowledgeHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	teamID := firstTeamID(claims)
	id := chi.URLParam(r, "id")

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	status := model.KnowledgeStatus(body.Status)
	if status != model.KnowledgeStatusApproved && status != model.KnowledgeStatusRejected {
		http.Error(w, "status must be 'approved' or 'rejected'", http.StatusBadRequest)
		return
	}

	if err := h.store.UpdateStatus(r.Context(), id, teamID, status); err != nil {
		http.Error(w, "failed to update knowledge item", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *KnowledgeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	teamID := firstTeamID(claims)
	id := chi.URLParam(r, "id")
	if err := h.store.Delete(r.Context(), id, teamID); err != nil {
		http.Error(w, "failed to delete knowledge item", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
