package handlers_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/server/handlers"
)

// mockProvider satisfies auth.Provider for testing without real GitHub OAuth.
type mockProvider struct{}

func (m *mockProvider) Name() string { return "github" }

func (m *mockProvider) AuthURL(state string) string {
	return "https://github.com/login/oauth/authorize?state=" + state
}

func (m *mockProvider) Exchange(_ context.Context, _ string) (*auth.ExternalIdentity, error) {
	return nil, fmt.Errorf("mock: exchange not implemented")
}

func newAuthRouter() http.Handler {
	h := handlers.NewAuthHandler(nil, &mockProvider{}, []byte("test-secret"))
	r := chi.NewRouter()
	r.Get("/auth/github", h.HandleGitHubRedirect)
	r.Get("/auth/github/callback", h.HandleGitHubCallback)
	return r
}

func TestOAuthCallback_MissingStateCookie(t *testing.T) {
	router := newAuthRouter()
	req := httptest.NewRequest("GET", "/auth/github/callback?state=abc&code=xyz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "missing oauth state")
}

func TestOAuthCallback_EmptyReturnedState(t *testing.T) {
	router := newAuthRouter()
	req := httptest.NewRequest("GET", "/auth/github/callback?code=xyz", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "expected-state"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid oauth state")
}

func TestOAuthCallback_MismatchedState(t *testing.T) {
	router := newAuthRouter()
	req := httptest.NewRequest("GET", "/auth/github/callback?state=wrong&code=xyz", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "expected-state"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid oauth state")
}

func TestOAuthCallback_MissingCode(t *testing.T) {
	router := newAuthRouter()
	req := httptest.NewRequest("GET", "/auth/github/callback?state=good", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "good"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "missing code")
}

func TestOAuthCallback_ValidStatePassesCSRFCheck(t *testing.T) {
	router := newAuthRouter()
	req := httptest.NewRequest("GET", "/auth/github/callback?state=good&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "good"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	// Should get past CSRF and fail on exchange (500), not on state validation (400).
	assert.NotEqual(t, http.StatusBadRequest, w.Code)
}

func TestOAuthRedirect_SetsStateCookie(t *testing.T) {
	router := newAuthRouter()
	req := httptest.NewRequest("GET", "/auth/github", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
	cookies := w.Result().Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == "oauth_state" {
			found = true
			assert.True(t, c.HttpOnly)
			assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
			assert.NotEmpty(t, c.Value)
		}
	}
	assert.True(t, found, "oauth_state cookie should be set")
}
