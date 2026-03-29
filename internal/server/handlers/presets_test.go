package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func TestListPresets_Unauthorized(t *testing.T) {
	h := &PresetHandlers{}
	req := httptest.NewRequest("GET", "/api/presets", nil)
	w := httptest.NewRecorder()
	h.ListPresets(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreatePreset_Validation(t *testing.T) {
	h := &PresetHandlers{}

	longTitle := strings.Repeat("a", 201)
	longPrompt := strings.Repeat("a", 50001)
	cases := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{"empty title", `{"title":"","prompt":"do thing","scope":"personal"}`, "title and prompt are required"},
		{"empty prompt", `{"title":"My Preset","prompt":"","scope":"personal"}`, "title and prompt are required"},
		{"bad scope", `{"title":"My Preset","prompt":"do thing","scope":"global"}`, "scope must be"},
		{"title too long", `{"title":"` + longTitle + `","prompt":"do thing","scope":"personal"}`, "title must be 200"},
		{"prompt too long", `{"title":"My Preset","prompt":"` + longPrompt + `","scope":"personal"}`, "prompt must be 50000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/presets", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = claimsCtx(req, "team-1")
			req.Header.Set("X-Team-ID", "team-1")

			w := httptest.NewRecorder()
			h.CreatePreset(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), tc.wantMsg)
		})
	}
}

func TestUpdatePreset_Unauthorized(t *testing.T) {
	h := &PresetHandlers{}
	req := httptest.NewRequest("PUT", "/api/presets/some-id", strings.NewReader(`{"title":"new"}`))
	w := httptest.NewRecorder()
	h.UpdatePreset(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUpdatePreset_BadScope(t *testing.T) {
	h := &PresetHandlers{}
	body := `{"scope":"global"}`
	req := httptest.NewRequest("PUT", "/api/presets/some-id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = claimsCtx(req, "team-1")

	// Add chi URL param
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "some-id")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.UpdatePreset(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "scope must be")
}

func TestDeletePreset_Unauthorized(t *testing.T) {
	h := &PresetHandlers{}
	req := httptest.NewRequest("DELETE", "/api/presets/some-id", nil)
	w := httptest.NewRecorder()
	h.DeletePreset(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
