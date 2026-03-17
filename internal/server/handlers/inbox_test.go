package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/tinkerloft/fleetlift/internal/auth"
)

func TestInboxRespond_NilClaims(t *testing.T) {
	h := NewInboxHandler(nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/inbox/item-1/respond", strings.NewReader(`{"answer":"yes"}`))
	w := httptest.NewRecorder()
	// Need chi router for URL params
	r := chi.NewRouter()
	r.Post("/api/inbox/{id}/respond", h.Respond)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestInboxRespond_AnswerRequired(t *testing.T) {
	h := NewInboxHandler(nil, nil)
	body := `{"answer":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/inbox/item-1/respond", strings.NewReader(body))
	ctx := auth.SetClaimsInContext(req.Context(), &auth.Claims{
		UserID:    "user-1",
		TeamRoles: map[string]string{"team-1": "member"},
	})
	req = req.WithContext(ctx)
	req.Header.Set("X-Team-ID", "team-1")
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Post("/api/inbox/{id}/respond", h.Respond)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
