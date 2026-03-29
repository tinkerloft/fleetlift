package handlers_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/server/handlers"
)

// mockProvider satisfies auth.Provider for testing without real GitHub OAuth.
type mockProvider struct{}

type staticProvider struct {
	identity *auth.ExternalIdentity
}

func (m *mockProvider) Name() string { return "github" }

func (m *mockProvider) AuthURL(state string) string {
	return "https://github.com/login/oauth/authorize?state=" + state
}

func (m *mockProvider) Exchange(_ context.Context, _ string) (*auth.ExternalIdentity, error) {
	return nil, fmt.Errorf("mock: exchange not implemented")
}

func (m *staticProvider) Name() string { return "github" }

func (m *staticProvider) AuthURL(state string) string {
	return "https://github.com/login/oauth/authorize?state=" + state
}

func (m *staticProvider) Exchange(_ context.Context, _ string) (*auth.ExternalIdentity, error) {
	return m.identity, nil
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

func TestOAuthCallback_TeamRoleLoadFailureReturns500AndNoToken(t *testing.T) {
	db, mock := newMockAuthDB(t)
	h := handlers.NewAuthHandler(db, &staticProvider{identity: &auth.ExternalIdentity{
		Email:      "fail-team-roles@example.com",
		Name:       "Fail Team Roles",
		Provider:   "github",
		ProviderID: "fail-team-roles",
	}}, []byte("test-secret"))

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO users (email, name, provider, provider_id)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (provider, provider_id) DO UPDATE SET email = $1, name = $2
		 RETURNING id`)).
		WithArgs("fail-team-roles@example.com", "Fail Team Roles", "github", "fail-team-roles").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("user-1"))
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT personal_team_id::text FROM users WHERE id = $1 FOR UPDATE`)).
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows([]string{"personal_team_id"}).AddRow("team-personal"))
	mock.ExpectCommit()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT tm.team_id, tm.role
		 FROM team_members tm
		 JOIN users u ON u.id = tm.user_id
		 WHERE tm.user_id = $1
		   AND (u.personal_team_id IS NULL OR tm.team_id <> u.personal_team_id)`)).
		WithArgs("user-1").
		WillReturnError(fmt.Errorf("team role load failed"))

	req := httptest.NewRequest("GET", "/auth/github/callback?state=good&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "good"})
	w := httptest.NewRecorder()

	h.HandleGitHubCallback(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Empty(t, w.Header().Get("Location"))
	assert.NoError(t, mock.ExpectationsWereMet())
	assertNoCookie(t, w.Result().Cookies(), "fl_token")
	assertNoCookie(t, w.Result().Cookies(), "refresh_token")
}

func TestOAuthCallback_PlatformAdminLoadFailureReturns500AndNoToken(t *testing.T) {
	db, mock := newMockAuthDB(t)
	h := handlers.NewAuthHandler(db, &staticProvider{identity: &auth.ExternalIdentity{
		Email:      "fail-platform-admin@example.com",
		Name:       "Fail Platform Admin",
		Provider:   "github",
		ProviderID: "fail-platform-admin",
	}}, []byte("test-secret"))

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO users (email, name, provider, provider_id)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (provider, provider_id) DO UPDATE SET email = $1, name = $2
		 RETURNING id`)).
		WithArgs("fail-platform-admin@example.com", "Fail Platform Admin", "github", "fail-platform-admin").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("user-2"))
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT personal_team_id::text FROM users WHERE id = $1 FOR UPDATE`)).
		WithArgs("user-2").
		WillReturnRows(sqlmock.NewRows([]string{"personal_team_id"}).AddRow("team-personal"))
	mock.ExpectCommit()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT tm.team_id, tm.role
		 FROM team_members tm
		 JOIN users u ON u.id = tm.user_id
		 WHERE tm.user_id = $1
		   AND (u.personal_team_id IS NULL OR tm.team_id <> u.personal_team_id)`)).
		WithArgs("user-2").
		WillReturnRows(sqlmock.NewRows([]string{"team_id", "role"}).AddRow("team-2", "admin"))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT platform_admin FROM users WHERE id = $1`)).
		WithArgs("user-2").
		WillReturnError(fmt.Errorf("platform admin load failed"))

	req := httptest.NewRequest("GET", "/auth/github/callback?state=good&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "good"})
	w := httptest.NewRecorder()

	h.HandleGitHubCallback(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Empty(t, w.Header().Get("Location"))
	assert.NoError(t, mock.ExpectationsWereMet())
	assertNoCookie(t, w.Result().Cookies(), "fl_token")
	assertNoCookie(t, w.Result().Cookies(), "refresh_token")
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

func TestOAuthCallback_ProvisionsPersonalTeamAndExcludesItFromJWTAndMe(t *testing.T) {
	db := openTestDB(t)

	providerID := fmt.Sprintf("personal-team-%d", os.Getpid())
	email := providerID + "@example.com"
	name := "Personal Team User"

	var userID string
	err := db.Get(&userID,
		`INSERT INTO users (email, name, provider, provider_id)
		 VALUES ($1, $2, 'github', $3)
		 RETURNING id`,
		email, name, providerID,
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		var personalTeamID string
		_ = db.Get(&personalTeamID, `SELECT COALESCE(personal_team_id::text, '') FROM users WHERE id = $1`, userID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, userID)
		if personalTeamID != "" {
			_, _ = db.Exec(`DELETE FROM teams WHERE id = $1`, personalTeamID)
		}
	})

	h := handlers.NewAuthHandler(db, &staticProvider{identity: &auth.ExternalIdentity{
		Email:      email,
		Name:       name,
		Provider:   "github",
		ProviderID: providerID,
	}}, []byte("test-secret"))

	req := httptest.NewRequest("GET", "/auth/github/callback?state=good&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "good"})
	w := httptest.NewRecorder()

	h.HandleGitHubCallback(w, req)

	assert.Equal(t, http.StatusTemporaryRedirect, w.Code)

	var personalTeamID string
	err = db.Get(&personalTeamID, `SELECT personal_team_id::text FROM users WHERE id = $1`, userID)
	assert.NoError(t, err)
	assert.NotEmpty(t, personalTeamID)

	var teamExists bool
	err = db.Get(&teamExists, `SELECT EXISTS(SELECT 1 FROM teams WHERE id = $1)`, personalTeamID)
	assert.NoError(t, err)
	assert.True(t, teamExists)

	var membershipCount int
	err = db.Get(&membershipCount,
		`SELECT COUNT(*) FROM team_members WHERE user_id = $1 AND team_id = $2`,
		userID, personalTeamID,
	)
	assert.NoError(t, err)
	assert.Equal(t, 1, membershipCount)

	location, err := w.Result().Location()
	assert.NoError(t, err)
	token := location.Query().Get("token")
	if token == "" {
		parsedURL, parseErr := url.Parse(w.Header().Get("Location"))
		assert.NoError(t, parseErr)
		token = parsedURL.Query().Get("token")
	}
	assert.NotEmpty(t, token)

	claims, err := auth.ValidateToken([]byte("test-secret"), token)
	assert.NoError(t, err)
	assert.NotContains(t, claims.TeamRoles, personalTeamID)

	meReq := httptest.NewRequest("GET", "/api/me", nil)
	meReq = meReq.WithContext(auth.SetClaimsInContext(meReq.Context(), claims))
	meW := httptest.NewRecorder()

	h.HandleMe(meW, meReq)

	assert.Equal(t, http.StatusOK, meW.Code)

	var body struct {
		Teams     []map[string]any  `json:"teams"`
		TeamRoles map[string]string `json:"team_roles"`
	}
	err = json.NewDecoder(meW.Body).Decode(&body)
	assert.NoError(t, err)
	assert.NotContains(t, body.TeamRoles, personalTeamID)
	assert.Len(t, body.Teams, 0)
	assert.False(t, strings.Contains(meW.Body.String(), personalTeamID))
}

func TestHandleRefresh_ExcludesPersonalTeamFromJWTTeamRoles(t *testing.T) {
	db := openTestDB(t)

	providerID := fmt.Sprintf("refresh-personal-team-%d", os.Getpid())
	var userID string
	err := db.Get(&userID,
		`INSERT INTO users (email, name, provider, provider_id)
		 VALUES ($1, $2, 'github', $3)
		 RETURNING id`,
		providerID+"@example.com", "Refresh Personal Team User", providerID,
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		var personalTeamID string
		_ = db.Get(&personalTeamID, `SELECT COALESCE(personal_team_id::text, '') FROM users WHERE id = $1`, userID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, userID)
		if personalTeamID != "" {
			_, _ = db.Exec(`DELETE FROM teams WHERE id = $1`, personalTeamID)
		}
	})

	personalTeamID := ""
	err = db.Get(&personalTeamID,
		`INSERT INTO teams (name, slug) VALUES ($1, $2) RETURNING id::text`,
		"Refresh Personal", "personal-"+userID,
	)
	if err != nil {
		t.Fatalf("insert personal team: %v", err)
	}
	_, err = db.Exec(`UPDATE users SET personal_team_id = $1 WHERE id = $2`, personalTeamID, userID)
	if err != nil {
		t.Fatalf("link personal team: %v", err)
	}
	_, err = db.Exec(`INSERT INTO team_members (team_id, user_id, role) VALUES ($1, $2, 'admin')`, personalTeamID, userID)
	if err != nil {
		t.Fatalf("insert personal team membership: %v", err)
	}

	sharedTeamID := ""
	err = db.Get(&sharedTeamID,
		`INSERT INTO teams (name, slug) VALUES ($1, $2) RETURNING id::text`,
		"Shared Team", "shared-"+userID,
	)
	if err != nil {
		t.Fatalf("insert shared team: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM teams WHERE id = $1`, sharedTeamID)
	})
	_, err = db.Exec(`INSERT INTO team_members (team_id, user_id, role) VALUES ($1, $2, 'member')`, sharedTeamID, userID)
	if err != nil {
		t.Fatalf("insert shared team membership: %v", err)
	}

	refreshToken, err := auth.IssueRefreshToken(context.Background(), db, userID)
	if err != nil {
		t.Fatalf("issue refresh token: %v", err)
	}

	h := handlers.NewAuthHandler(db, nil, []byte("test-secret"))
	req := httptest.NewRequest("POST", "/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})
	w := httptest.NewRecorder()

	h.HandleRefresh(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]string
	err = json.NewDecoder(w.Body).Decode(&body)
	assert.NoError(t, err)

	claims, err := auth.ValidateToken([]byte("test-secret"), body["token"])
	assert.NoError(t, err)
	assert.Equal(t, "member", claims.TeamRoles[sharedTeamID])
	assert.NotContains(t, claims.TeamRoles, personalTeamID)
}

func TestHandleRefresh_TeamRoleLoadFailureReturns500AndNoToken(t *testing.T) {
	db, mock := newMockAuthDB(t)
	h := handlers.NewAuthHandler(db, nil, []byte("test-secret"))

	rawToken := "refresh-token"
	hash := authTestSHA256Hex(rawToken)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, expires_at, used_at FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`)).
		WithArgs(hash).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "expires_at", "used_at"}).AddRow("rt-1", "user-3", time.Now().Add(time.Hour), nil))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT tm.team_id, tm.role
		 FROM team_members tm
		 JOIN users u ON u.id = tm.user_id
		 WHERE tm.user_id = $1
		   AND (u.personal_team_id IS NULL OR tm.team_id <> u.personal_team_id)`)).
		WithArgs("user-3").
		WillReturnError(fmt.Errorf("team role load failed"))
	mock.ExpectRollback()

	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: rawToken})
	w := httptest.NewRecorder()

	h.HandleRefresh(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.NoError(t, json.NewDecoder(w.Body).Decode(&map[string]any{}))
	assertNoCookie(t, w.Result().Cookies(), "refresh_token")
}

func TestHandleRefresh_FailedRoleLoadDoesNotBurnSession(t *testing.T) {
	db, mock := newMockAuthDB(t)
	h := handlers.NewAuthHandler(db, nil, []byte("test-secret"))

	rawToken := "refresh-token-retry"
	hash := authTestSHA256Hex(rawToken)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, expires_at, used_at FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`)).
		WithArgs(hash).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "expires_at", "used_at"}).AddRow("rt-retry", "user-retry", time.Now().Add(time.Hour), nil))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT tm.team_id, tm.role
		 FROM team_members tm
		 JOIN users u ON u.id = tm.user_id
		 WHERE tm.user_id = $1
		   AND (u.personal_team_id IS NULL OR tm.team_id <> u.personal_team_id)`)).
		WithArgs("user-retry").
		WillReturnError(fmt.Errorf("team role load failed"))
	mock.ExpectRollback()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, expires_at, used_at FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`)).
		WithArgs(hash).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "expires_at", "used_at"}).AddRow("rt-retry", "user-retry", time.Now().Add(time.Hour), nil))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT tm.team_id, tm.role
		 FROM team_members tm
		 JOIN users u ON u.id = tm.user_id
		 WHERE tm.user_id = $1
		   AND (u.personal_team_id IS NULL OR tm.team_id <> u.personal_team_id)`)).
		WithArgs("user-retry").
		WillReturnRows(sqlmock.NewRows([]string{"team_id", "role"}).AddRow("team-retry", "member"))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT platform_admin FROM users WHERE id = $1`)).
		WithArgs("user-retry").
		WillReturnRows(sqlmock.NewRows([]string{"platform_admin"}).AddRow(false))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE refresh_tokens SET used_at = NOW() WHERE id = $1`)).
		WithArgs("rt-retry").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`)).
		WithArgs("user-retry", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	firstReq := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	firstReq.AddCookie(&http.Cookie{Name: "refresh_token", Value: rawToken})
	firstW := httptest.NewRecorder()
	h.HandleRefresh(firstW, firstReq)
	assert.Equal(t, http.StatusInternalServerError, firstW.Code)
	assertNoCookie(t, firstW.Result().Cookies(), "refresh_token")

	secondReq := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	secondReq.AddCookie(&http.Cookie{Name: "refresh_token", Value: rawToken})
	secondW := httptest.NewRecorder()
	h.HandleRefresh(secondW, secondReq)
	assert.Equal(t, http.StatusOK, secondW.Code)

	var body map[string]string
	err := json.NewDecoder(secondW.Body).Decode(&body)
	assert.NoError(t, err)
	assert.NotEmpty(t, body["token"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestHandleRefresh_PlatformAdminLoadFailureReturns500AndNoToken(t *testing.T) {
	db, mock := newMockAuthDB(t)
	h := handlers.NewAuthHandler(db, nil, []byte("test-secret"))

	rawToken := "refresh-token-admin"
	hash := authTestSHA256Hex(rawToken)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, expires_at, used_at FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`)).
		WithArgs(hash).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "expires_at", "used_at"}).AddRow("rt-2", "user-4", time.Now().Add(time.Hour), nil))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT tm.team_id, tm.role
		 FROM team_members tm
		 JOIN users u ON u.id = tm.user_id
		 WHERE tm.user_id = $1
		   AND (u.personal_team_id IS NULL OR tm.team_id <> u.personal_team_id)`)).
		WithArgs("user-4").
		WillReturnRows(sqlmock.NewRows([]string{"team_id", "role"}).AddRow("team-4", "member"))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT platform_admin FROM users WHERE id = $1`)).
		WithArgs("user-4").
		WillReturnError(fmt.Errorf("platform admin load failed"))
	mock.ExpectRollback()

	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: rawToken})
	w := httptest.NewRecorder()

	h.HandleRefresh(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.NoError(t, json.NewDecoder(w.Body).Decode(&map[string]any{}))
	assertNoCookie(t, w.Result().Cookies(), "refresh_token")
}

func TestHandleRefresh_InvalidTokenReturns401(t *testing.T) {
	db, mock := newMockAuthDB(t)
	h := handlers.NewAuthHandler(db, nil, []byte("test-secret"))

	rawToken := "missing-refresh-token"
	hash := authTestSHA256Hex(rawToken)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, expires_at, used_at FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`)).
		WithArgs(hash).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: rawToken})
	w := httptest.NewRecorder()

	h.HandleRefresh(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
	assertNoCookie(t, w.Result().Cookies(), "refresh_token")
}

func TestHandleRefresh_RefreshLookupFailureReturns500(t *testing.T) {
	db, mock := newMockAuthDB(t)
	h := handlers.NewAuthHandler(db, nil, []byte("test-secret"))

	rawToken := "db-error-refresh-token"
	hash := authTestSHA256Hex(rawToken)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, expires_at, used_at FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`)).
		WithArgs(hash).
		WillReturnError(fmt.Errorf("db unavailable"))
	mock.ExpectRollback()

	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: rawToken})
	w := httptest.NewRecorder()

	h.HandleRefresh(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
	assertNoCookie(t, w.Result().Cookies(), "refresh_token")
}

func TestHandleRefresh_ExpiredTokenReturns401(t *testing.T) {
	db, mock := newMockAuthDB(t)
	h := handlers.NewAuthHandler(db, nil, []byte("test-secret"))

	rawToken := "expired-refresh-token"
	hash := authTestSHA256Hex(rawToken)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, expires_at, used_at FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`)).
		WithArgs(hash).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "expires_at", "used_at"}).AddRow("rt-expired", "user-expired", time.Now().Add(-time.Minute), nil))
	mock.ExpectRollback()

	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: rawToken})
	w := httptest.NewRecorder()

	h.HandleRefresh(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
	assertNoCookie(t, w.Result().Cookies(), "refresh_token")
}

func TestHandleRefresh_ReuseDetectedReturns401(t *testing.T) {
	db, mock := newMockAuthDB(t)
	h := handlers.NewAuthHandler(db, nil, []byte("test-secret"))

	rawToken := "reused-refresh-token"
	hash := authTestSHA256Hex(rawToken)
	usedAt := time.Now().Add(-time.Minute)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, expires_at, used_at FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`)).
		WithArgs(hash).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "expires_at", "used_at"}).AddRow("rt-reused", "user-reused", time.Now().Add(time.Hour), usedAt))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM refresh_tokens WHERE user_id = $1`)).
		WithArgs("user-reused").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: rawToken})
	w := httptest.NewRecorder()

	h.HandleRefresh(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
	assertNoCookie(t, w.Result().Cookies(), "refresh_token")
}

func TestHandleMe_Unauthorized(t *testing.T) {
	h := handlers.NewAuthHandler(nil, nil, []byte("test-secret"))
	req := httptest.NewRequest("GET", "/api/me", nil)
	w := httptest.NewRecorder()

	h.HandleMe(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleMe_ProfileQueryFailureReturns500(t *testing.T) {
	db, mock := newMockAuthDB(t)
	h := handlers.NewAuthHandler(db, nil, []byte("test-secret"))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(name, ''), COALESCE(email, '') FROM users WHERE id = $1`)).
		WithArgs("user-1").
		WillReturnError(fmt.Errorf("profile query failed"))

	req := httptest.NewRequest("GET", "/api/me", nil)
	req = req.WithContext(auth.SetClaimsInContext(req.Context(), &auth.Claims{UserID: "user-1"}))
	w := httptest.NewRecorder()

	h.HandleMe(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestHandleMe_TeamsQueryFailureReturns500(t *testing.T) {
	db, mock := newMockAuthDB(t)
	h := handlers.NewAuthHandler(db, nil, []byte("test-secret"))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(name, ''), COALESCE(email, '') FROM users WHERE id = $1`)).
		WithArgs("user-2").
		WillReturnRows(sqlmock.NewRows([]string{"coalesce", "coalesce"}).AddRow("Alice", "alice@example.com"))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT t.id, t.name, t.slug, tm.role
		 FROM teams t JOIN team_members tm ON t.id = tm.team_id
		 JOIN users u ON u.id = tm.user_id
		 WHERE tm.user_id = $1
		   AND (u.personal_team_id IS NULL OR t.id <> u.personal_team_id)
		 ORDER BY t.name`)).
		WithArgs("user-2").
		WillReturnError(fmt.Errorf("teams query failed"))

	req := httptest.NewRequest("GET", "/api/me", nil)
	req = req.WithContext(auth.SetClaimsInContext(req.Context(), &auth.Claims{UserID: "user-2"}))
	w := httptest.NewRecorder()

	h.HandleMe(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func newMockAuthDB(t *testing.T) (*sqlx.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlx.NewDb(sqlDB, "sqlmock"), mock
}

func assertNoCookie(t *testing.T, cookies []*http.Cookie, name string) {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name && cookie.Value != "" && cookie.MaxAge >= 0 {
			t.Fatalf("unexpected cookie %q present", name)
		}
	}
}

func authTestSHA256Hex(s string) string {
	tokenHash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(tokenHash[:])
}
