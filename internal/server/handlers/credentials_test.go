package handlers

import (
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/auth"
)

func newTestCredentialsHandler(t *testing.T) *CredentialsHandler {
	t.Helper()
	key := make([]byte, 32)
	h, err := NewCredentialsHandler(nil, hex.EncodeToString(key))
	require.NoError(t, err)
	return h
}

func claimsCtx(r *http.Request, teamID string) *http.Request {
	claims := &auth.Claims{
		UserID:    "user-1",
		TeamRoles: map[string]string{teamID: "admin"},
	}
	return r.WithContext(auth.SetClaimsInContext(r.Context(), claims))
}

func TestCredentials_Set_RejectsInvalidName(t *testing.T) {
	h := newTestCredentialsHandler(t)
	cases := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{"lowercase rejected", `{"name":"my_token","value":"v"}`, "invalid credential name"},
		{"starts with digit rejected", `{"name":"1TOKEN","value":"v"}`, "invalid credential name"},
		{"reserved name rejected", `{"name":"PATH","value":"v"}`, "reserved"},
		{"empty name rejected", `{"name":"","value":"v"}`, "required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/credentials",
				strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = claimsCtx(req, "team-1")
			req.Header.Set("X-Team-ID", "team-1")
			w := httptest.NewRecorder()
			h.Set(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), tc.wantMsg)
		})
	}
}

func TestCredentials_Set_AcceptsValidName_PassesValidation(t *testing.T) {
	// Verify a valid name passes all validation checks.
	// We confirm this by checking the name against the regex and reserved list directly.
	assert.NoError(t, validateCredentialName("GITHUB_TOKEN"))
	assert.NoError(t, validateCredentialName("MY_API_KEY_123"))
}
