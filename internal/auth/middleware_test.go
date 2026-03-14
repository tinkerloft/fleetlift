package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMiddleware_BlocksNoToken(t *testing.T) {
	h := Middleware([]byte("secret"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestMiddleware_BlocksInvalidToken(t *testing.T) {
	h := Middleware([]byte("secret"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-token")
	h.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestMiddleware_AllowsValidBearerToken(t *testing.T) {
	secret := []byte("test-secret")
	tokenStr, err := IssueToken(secret, "user-1", map[string]string{"team-1": "member"}, false)
	require.NoError(t, err)

	var gotUserID string
	h := Middleware(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := ClaimsFromContext(r.Context())
		if c != nil {
			gotUserID = c.UserID
		}
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	h.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "user-1", gotUserID)
}

func TestMiddleware_AllowsValidCookieToken(t *testing.T) {
	secret := []byte("test-secret")
	tokenStr, err := IssueToken(secret, "user-2", map[string]string{}, false)
	require.NoError(t, err)

	h := Middleware(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "fl_token", Value: tokenStr})
	h.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestMiddleware_BlocksCookieOnMutatingMethod(t *testing.T) {
	secret := []byte("test-secret")
	tokenStr, err := IssueToken(secret, "user-3", map[string]string{}, false)
	require.NoError(t, err)

	h := Middleware(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	req.AddCookie(&http.Cookie{Name: "fl_token", Value: tokenStr})
	h.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}
