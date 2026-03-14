package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func TestArtifacts_RequiresAuth(t *testing.T) {
	h := NewReportsHandler(nil)
	r := chi.NewRouter()
	r.Get("/api/reports/{runID}/artifacts", h.Artifacts)

	req := httptest.NewRequest("GET", "/api/reports/run-1/artifacts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
