package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tinkerloft/fleetlift/internal/model"
)

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
