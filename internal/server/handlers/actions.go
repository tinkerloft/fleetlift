package handlers

import (
	"net/http"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// ActionsHandler exposes the action type registry via API.
type ActionsHandler struct {
	registry *model.ActionRegistry
}

// NewActionsHandler creates a new ActionsHandler.
func NewActionsHandler(registry *model.ActionRegistry) *ActionsHandler {
	return &ActionsHandler{registry: registry}
}

// List returns all registered action types with their contracts.
func (h *ActionsHandler) List(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"items": h.registry.All(),
	})
}
