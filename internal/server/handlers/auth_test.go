package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
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

// openTestDB opens a DB connection for integration tests.
// Skipped unless DATABASE_URL is set.
func openTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("requires DB: set DATABASE_URL")
	}
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestHandleMeEnriched(t *testing.T) {
	db := openTestDB(t)

	// Insert test user + team
	userID := "test-user-me-" + fmt.Sprintf("%d", os.Getpid())
	teamID := "test-team-me-" + fmt.Sprintf("%d", os.Getpid())
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM team_members WHERE user_id = $1`, userID)
		_, _ = db.Exec(`DELETE FROM teams WHERE id = $1`, teamID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	})

	db.MustExec(`INSERT INTO users (id, name, email, provider, provider_id) VALUES ($1, 'Alice', 'alice@example.com', 'github', $1) ON CONFLICT DO NOTHING`, userID)
	db.MustExec(`INSERT INTO teams (id, name, slug) VALUES ($1, 'Acme Corp', $1) ON CONFLICT DO NOTHING`, teamID)
	db.MustExec(`INSERT INTO team_members (team_id, user_id, role) VALUES ($1, $2, 'admin') ON CONFLICT DO NOTHING`, teamID, userID)

	h := handlers.NewAuthHandler(db, nil, []byte("test-secret"))

	claims := &auth.Claims{UserID: userID, TeamRoles: map[string]string{teamID: "admin"}}
	req := httptest.NewRequest("GET", "/api/me", nil)
	req = req.WithContext(auth.SetClaimsInContext(req.Context(), claims))
	w := httptest.NewRecorder()

	h.HandleMe(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	err := json.NewDecoder(w.Body).Decode(&body)
	assert.NoError(t, err)
	assert.Equal(t, "Alice", body["name"])
	assert.Equal(t, "alice@example.com", body["email"])

	teams, ok := body["teams"].([]any)
	assert.True(t, ok, "teams should be an array")
	if len(teams) > 0 {
		team := teams[0].(map[string]any)
		assert.Equal(t, "Acme Corp", team["name"])
	}
}

func TestHandleMe_Unauthorized(t *testing.T) {
	h := handlers.NewAuthHandler(nil, nil, []byte("test-secret"))
	req := httptest.NewRequest("GET", "/api/me", nil)
	w := httptest.NewRecorder()

	h.HandleMe(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
