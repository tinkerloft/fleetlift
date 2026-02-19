package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleTaskEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send initial status immediately.
	s.pushStatusEvent(w, flusher, r, id)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			s.pushStatusEvent(w, flusher, r, id)
		}
	}
}

func (s *Server) pushStatusEvent(w http.ResponseWriter, flusher http.Flusher, r *http.Request, id string) {
	status, err := s.client.GetWorkflowStatus(r.Context(), id)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return
	}
	data, _ := json.Marshal(map[string]any{"status": status})
	fmt.Fprintf(w, "event: status\ndata: %s\n\n", data)
	flusher.Flush()
}
