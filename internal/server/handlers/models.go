package handlers

import "net/http"

// ModelEntry represents an available AI model.
type ModelEntry struct {
	Value    string `json:"value"`
	Label    string `json:"label"`
	Provider string `json:"provider"`
}

// ModelsHandler serves the static model list.
type ModelsHandler struct {
	entries []ModelEntry
}

// NewModelsHandler creates a ModelsHandler with the given model entries.
func NewModelsHandler(entries []ModelEntry) *ModelsHandler {
	return &ModelsHandler{entries: entries}
}

// List returns all available models as {"items": [...]}.
func (h *ModelsHandler) List(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"items": h.entries,
	})
}
