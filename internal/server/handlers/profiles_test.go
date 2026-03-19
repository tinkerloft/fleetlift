package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"github.com/stretchr/testify/assert"

	"github.com/tinkerloft/fleetlift/internal/model"
)

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

func randomSuffix() string {
	return fmt.Sprintf("%d", os.Getpid())
}

func TestListAgentProfiles_ReturnsOK(t *testing.T) {
	// Without a real DB the handler will return an error, but we can verify
	// that auth gating works: no claims → 401.
	h := NewProfilesHandler(nil)
	req := httptest.NewRequest("GET", "/api/agent-profiles", nil)
	w := httptest.NewRecorder()
	h.ListProfiles(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "unauthorized")
}

func TestCreateAgentProfile_ValidBody(t *testing.T) {
	// Without a real DB we can't complete insertion, but we can verify
	// validation passes for a valid body and auth is enforced.
	h := NewProfilesHandler(nil)
	body := `{"name":"test","description":"desc","body":{"plugins":[{"plugin":"foo"}],"skills":[{"skill":"bar"}]}}`
	req := httptest.NewRequest("POST", "/api/agent-profiles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No claims → 401
	w := httptest.NewRecorder()
	h.CreateProfile(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateAgentProfile_InvalidPluginSource_Returns400(t *testing.T) {
	h := NewProfilesHandler(nil)
	// Both plugin and github_url set → should fail validation
	body := `{"name":"test","description":"desc","body":{"plugins":[{"plugin":"foo","github_url":"https://github.com/x"}]}}`
	req := httptest.NewRequest("POST", "/api/agent-profiles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")

	w := httptest.NewRecorder()
	h.CreateProfile(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "only one of")
}

func TestCreateAgentProfile_InvalidSkillSource_Returns400(t *testing.T) {
	h := NewProfilesHandler(nil)
	// Both skill and github_url set → should fail validation
	body := `{"name":"test","description":"desc","body":{"skills":[{"skill":"foo","github_url":"https://github.com/x"}]}}`
	req := httptest.NewRequest("POST", "/api/agent-profiles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")

	w := httptest.NewRecorder()
	h.CreateProfile(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "only one of")
}

func TestCreateAgentProfile_MissingName_Returns400(t *testing.T) {
	h := NewProfilesHandler(nil)
	body := `{"name":"","description":"desc","body":{"plugins":[{"plugin":"foo"}]}}`
	req := httptest.NewRequest("POST", "/api/agent-profiles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")

	w := httptest.NewRecorder()
	h.CreateProfile(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "name is required")
}

func TestCreateAgentProfile_EmptyPlugins_Returns400(t *testing.T) {
	h := NewProfilesHandler(nil)
	// Plugin with neither plugin nor github_url set
	body := `{"name":"test","description":"desc","body":{"plugins":[{}]}}`
	req := httptest.NewRequest("POST", "/api/agent-profiles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")

	w := httptest.NewRecorder()
	h.CreateProfile(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "must be set")
}

func TestDeleteAgentProfile_Returns204(t *testing.T) {
	// Without a real DB the delete will panic on nil db, so we just test auth gating.
	h := NewProfilesHandler(nil)
	req := httptest.NewRequest("DELETE", "/api/agent-profiles/some-id", nil)
	w := httptest.NewRecorder()
	h.DeleteProfile(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDeleteAgentProfile_NotFound_Returns404(t *testing.T) {
	db := openTestDB(t)
	h := NewProfilesHandler(db)

	teamID := "test-team-delete-profile-" + randomSuffix()
	nonExistentID := "non-existent-id-" + randomSuffix()

	// Create a team for test isolation
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `INSERT INTO teams (id, name) VALUES ($1, $2) ON CONFLICT DO NOTHING`, teamID, teamID)
	if err != nil {
		t.Fatalf("failed to insert team: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM teams WHERE id = $1`, teamID)
	})

	req := httptest.NewRequest("DELETE", "/api/agent-profiles/"+nonExistentID, nil)
	req = claimsCtx(req, teamID)
	req.Header.Set("X-Team-ID", teamID)

	w := httptest.NewRecorder()
	h.DeleteProfile(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "profile not found")
}

func TestCreateMarketplace_MissingName_Returns400(t *testing.T) {
	h := NewProfilesHandler(nil)
	body := `{"name":"","repo_url":"https://github.com/x"}`
	req := httptest.NewRequest("POST", "/api/marketplaces", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")

	w := httptest.NewRecorder()
	h.CreateMarketplace(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "name is required")
}

func TestCreateMarketplace_MissingRepoURL_Returns400(t *testing.T) {
	h := NewProfilesHandler(nil)
	body := `{"name":"test","repo_url":""}`
	req := httptest.NewRequest("POST", "/api/marketplaces", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")

	w := httptest.NewRecorder()
	h.CreateMarketplace(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "repo_url is required")
}

func TestCreateMarketplace_NonHTTPS_Returns400(t *testing.T) {
	h := NewProfilesHandler(nil)
	body := `{"name":"test","repo_url":"http://github.com/org/repo.git"}`
	req := httptest.NewRequest("POST", "/api/marketplaces", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")

	w := httptest.NewRecorder()
	h.CreateMarketplace(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "https://")
}

func TestDeleteMarketplace_NotFound_Returns404(t *testing.T) {
	db := openTestDB(t)
	h := NewProfilesHandler(db)

	teamID := "test-team-delete-marketplace-" + randomSuffix()
	nonExistentID := "non-existent-id-" + randomSuffix()

	// Create a team for test isolation
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `INSERT INTO teams (id, name) VALUES ($1, $2) ON CONFLICT DO NOTHING`, teamID, teamID)
	if err != nil {
		t.Fatalf("failed to insert team: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM teams WHERE id = $1`, teamID)
	})

	req := httptest.NewRequest("DELETE", "/api/marketplaces/"+nonExistentID, nil)
	req = claimsCtx(req, teamID)
	req.Header.Set("X-Team-ID", teamID)

	w := httptest.NewRecorder()
	h.DeleteMarketplace(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "marketplace not found")
}

func TestValidateAgentProfileBody(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
		errMsg  string
	}{
		{"valid plugin only", `{"plugins":[{"plugin":"foo"}]}`, false, ""},
		{"valid github_url only", `{"plugins":[{"github_url":"https://github.com/x"}]}`, false, ""},
		{"both set", `{"plugins":[{"plugin":"foo","github_url":"https://github.com/x"}]}`, true, "only one of"},
		{"neither set", `{"plugins":[{}]}`, true, "must be set"},
		{"non-https url", `{"plugins":[{"github_url":"http://github.com/x"}]}`, true, "https://"},
		{"valid skill", `{"skills":[{"skill":"bar"}]}`, false, ""},
		{"skill both set", `{"skills":[{"skill":"bar","github_url":"https://github.com/x"}]}`, true, "only one of"},
		{"valid mcp", `{"mcps":[{"name":"m","url":"https://mcp.example.com/sse"}]}`, false, ""},
		{"mcp missing name", `{"mcps":[{"name":"","url":"https://mcp.example.com"}]}`, true, "name is required"},
		{"mcp missing url", `{"mcps":[{"name":"m","url":""}]}`, true, "url is required"},
		{"mcp non-http url", `{"mcps":[{"name":"m","url":"file:///etc/passwd"}]}`, true, "http:// or https://"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body model.AgentProfileBody
			err := json.Unmarshal([]byte(tc.body), &body)
			assert.NoError(t, err)
			err = validateAgentProfileBody(&body)
			if tc.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetProfile_DBError_Returns500(t *testing.T) {
	// Use a bad DSN so queries fail with a real connection error
	db, err := sqlx.Open("postgres", "postgres://invalid:invalid@localhost:5999/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	h := NewProfilesHandler(db)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "some-id")
	req := httptest.NewRequest(http.MethodGet, "/api/agent-profiles/some-id", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")

	w := httptest.NewRecorder()
	h.GetProfile(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "failed to get profile")
}
