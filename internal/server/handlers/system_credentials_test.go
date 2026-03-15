package handlers

import (
	"bytes"
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/auth"
)

func newTestSystemCredentialsHandler(t *testing.T) *SystemCredentialsHandler {
	t.Helper()
	key := make([]byte, 32)
	h, err := NewSystemCredentialsHandler(nil, hex.EncodeToString(key))
	require.NoError(t, err)
	return h
}

func adminCtx(r *http.Request) *http.Request {
	claims := &auth.Claims{UserID: "user-1", PlatformAdmin: true}
	return r.WithContext(auth.SetClaimsInContext(r.Context(), claims))
}

func nonAdminCtx(r *http.Request) *http.Request {
	claims := &auth.Claims{UserID: "user-1", TeamRoles: map[string]string{"team-1": "member"}}
	return r.WithContext(auth.SetClaimsInContext(r.Context(), claims))
}

func TestSystemCredentials_List_RequiresAdmin(t *testing.T) {
	h := newTestSystemCredentialsHandler(t)
	req := httptest.NewRequest("GET", "/api/system-credentials", nil)
	req = nonAdminCtx(req)
	w := httptest.NewRecorder()
	h.List(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestSystemCredentials_Set_RequiresAdmin(t *testing.T) {
	h := newTestSystemCredentialsHandler(t)
	body := bytes.NewBufferString(`{"name":"MY_KEY","value":"v"}`)
	req := httptest.NewRequest("POST", "/api/system-credentials", body)
	req.Header.Set("Content-Type", "application/json")
	req = nonAdminCtx(req)
	w := httptest.NewRecorder()
	h.Set(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestSystemCredentials_Delete_RequiresAdmin(t *testing.T) {
	h := newTestSystemCredentialsHandler(t)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "MY_KEY")
	req := httptest.NewRequest("DELETE", "/api/system-credentials/MY_KEY", nil)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	req = nonAdminCtx(req)
	w := httptest.NewRecorder()
	h.Delete(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestSystemCredentials_Set_RejectsInvalidName(t *testing.T) {
	h := newTestSystemCredentialsHandler(t)
	cases := []string{`{"name":"lower","value":"v"}`, `{"name":"PATH","value":"v"}`}
	for _, body := range cases {
		req := httptest.NewRequest("POST", "/api/system-credentials",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = adminCtx(req)
		w := httptest.NewRecorder()
		h.Set(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	}
}

func TestSystemCredentials_Set_AcceptsValidName_PassesValidation(t *testing.T) {
	// Verify a valid name passes all validation checks.
	// validateCredentialName is shared between team and system handlers (same package).
	assert.NoError(t, validateCredentialName("DATADOG_API_KEY"))
}
