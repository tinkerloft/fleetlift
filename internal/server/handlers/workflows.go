package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/template"
)

// WorkflowsHandler handles CRUD operations on workflow templates.
type WorkflowsHandler struct {
	registry *template.Registry
	writable template.Provider
}

// NewWorkflowsHandler creates a new WorkflowsHandler.
func NewWorkflowsHandler(registry *template.Registry) *WorkflowsHandler {
	return &WorkflowsHandler{
		registry: registry,
		writable: registry.WritableProvider(),
	}
}

// List returns all workflow templates visible to the user's team.
func (h *WorkflowsHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	teamID := firstTeamID(claims)
	templates, err := h.registry.List(r.Context(), teamID)
	if err != nil {
		http.Error(w, "failed to list workflows", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, templates)
}

// Get returns a single workflow template by ID/slug.
func (h *WorkflowsHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	slug := chi.URLParam(r, "id")
	teamID := firstTeamID(claims)
	t, err := h.registry.Get(r.Context(), teamID, slug)
	if err != nil {
		http.Error(w, "workflow not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, t)
}

// Create creates a new workflow template.
func (h *WorkflowsHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.writable == nil {
		http.Error(w, "no writable template store", http.StatusNotImplemented)
		return
	}

	var t model.WorkflowTemplate
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var def model.WorkflowDef
	if err := model.ParseWorkflowYAML([]byte(t.YAMLBody), &def); err != nil {
		http.Error(w, fmt.Sprintf("invalid workflow YAML: %s", err.Error()), http.StatusBadRequest)
		return
	}

	teamID := firstTeamID(claims)
	t.TeamID = teamID
	if err := h.writable.Save(r.Context(), teamID, &t); err != nil {
		http.Error(w, "failed to save workflow", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, t)
}

// Update updates an existing workflow template.
func (h *WorkflowsHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.writable == nil {
		http.Error(w, "no writable template store", http.StatusNotImplemented)
		return
	}

	var t model.WorkflowTemplate
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var def model.WorkflowDef
	if err := model.ParseWorkflowYAML([]byte(t.YAMLBody), &def); err != nil {
		http.Error(w, fmt.Sprintf("invalid workflow YAML: %s", err.Error()), http.StatusBadRequest)
		return
	}

	teamID := firstTeamID(claims)
	t.TeamID = teamID
	t.Slug = chi.URLParam(r, "id")
	if err := h.writable.Save(r.Context(), teamID, &t); err != nil {
		http.Error(w, "failed to update workflow", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, t)
}

// Delete removes a workflow template.
func (h *WorkflowsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.writable == nil {
		http.Error(w, "no writable template store", http.StatusNotImplemented)
		return
	}

	teamID := firstTeamID(claims)
	slug := chi.URLParam(r, "id")
	if err := h.writable.Delete(r.Context(), teamID, slug); err != nil {
		http.Error(w, "failed to delete workflow", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Fork creates a copy of a builtin workflow template as a team-owned template.
func (h *WorkflowsHandler) Fork(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.writable == nil {
		http.Error(w, "no writable template store", http.StatusNotImplemented)
		return
	}

	teamID := firstTeamID(claims)
	slug := chi.URLParam(r, "id")
	t, err := h.registry.Get(r.Context(), teamID, slug)
	if err != nil {
		http.Error(w, "workflow not found", http.StatusNotFound)
		return
	}

	// Create a copy owned by the team
	forked := *t
	forked.ID = ""
	forked.TeamID = teamID
	forked.Builtin = false
	forked.Slug = slug + "-fork"

	if err := h.writable.Save(r.Context(), teamID, &forked); err != nil {
		http.Error(w, "failed to fork workflow", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, forked)
}
