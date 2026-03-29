package handlers

import "net/http"

type ModelEntry struct {
	Value    string `json:"value"`
	Label    string `json:"label"`
	Provider string `json:"provider"`
}

type ModelsHandler struct {
	entries []ModelEntry
}

func NewModelsHandler(entries []ModelEntry) *ModelsHandler {
	return &ModelsHandler{entries: entries}
}

func (h *ModelsHandler) List(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"items": h.entries,
	})
}
