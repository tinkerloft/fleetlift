package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func TestStepWorkflowID(t *testing.T) {
	runID := "run-abc123"
	stepID := "analyze"
	got := stepWorkflowID(runID, stepID)
	assert.Equal(t, "run-abc123-analyze", got)
}

func TestStream_RequiresAuth(t *testing.T) {
	h := NewRunsHandler(nil, nil, nil, nil)
	r := chi.NewRouter()
	r.Get("/api/runs/{id}/events", h.Stream)

	req := httptest.NewRequest("GET", "/api/runs/run-1/events", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestStepLogs_RequiresAuth(t *testing.T) {
	h := NewRunsHandler(nil, nil, nil, nil)
	r := chi.NewRouter()
	r.Get("/api/runs/steps/{id}/logs", h.StepLogs)

	req := httptest.NewRequest("GET", "/api/runs/steps/sr-1/logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TODO: Full SSE integration tests (header verification, event delivery, terminal
// state closing) require a running test DB. Track as a future integration test task.
