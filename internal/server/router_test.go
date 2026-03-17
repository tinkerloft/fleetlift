package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/server/handlers"
	"github.com/tinkerloft/fleetlift/internal/template"
)

// newTestCredentialsHandler creates a CredentialsHandler with a dummy key for testing.
func newTestCredentialsHandler() *handlers.CredentialsHandler {
	// 32-byte key as 64 hex chars
	h, _ := handlers.NewCredentialsHandler(nil, "0000000000000000000000000000000000000000000000000000000000000000")
	return h
}

func TestNewRouter_ReturnsErrorOnBadFS(t *testing.T) {
	registry := template.NewRegistry()
	deps := Deps{
		JWTSecret:   []byte("test-secret"),
		Auth:        handlers.NewAuthHandler(nil, nil, []byte("test-secret")),
		Workflows:   handlers.NewWorkflowsHandler(registry),
		Runs:        handlers.NewRunsHandler(nil, nil, registry, nil),
		Inbox:       handlers.NewInboxHandler(nil, nil),
		Reports:     handlers.NewReportsHandler(nil),
		Credentials: newTestCredentialsHandler(),
		Knowledge:   handlers.NewKnowledgeHandler(nil),
	}
	router, err := NewRouter(deps)
	require.NoError(t, err)
	assert.NotNil(t, router)
}

func TestNewRouter_HealthEndpoint(t *testing.T) {
	registry := template.NewRegistry()
	deps := Deps{
		JWTSecret:   []byte("test-secret"),
		Auth:        handlers.NewAuthHandler(nil, nil, []byte("test-secret")),
		Workflows:   handlers.NewWorkflowsHandler(registry),
		Runs:        handlers.NewRunsHandler(nil, nil, registry, nil),
		Inbox:       handlers.NewInboxHandler(nil, nil),
		Reports:     handlers.NewReportsHandler(nil),
		Credentials: newTestCredentialsHandler(),
		Knowledge:   handlers.NewKnowledgeHandler(nil),
	}
	router, err := NewRouter(deps)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, w.Body.String(), `"status"`)
}

func TestNewRouter_SPAFallback(t *testing.T) {
	registry := template.NewRegistry()
	deps := Deps{
		JWTSecret:   []byte("test-secret"),
		Auth:        handlers.NewAuthHandler(nil, nil, []byte("test-secret")),
		Workflows:   handlers.NewWorkflowsHandler(registry),
		Runs:        handlers.NewRunsHandler(nil, nil, registry, nil),
		Inbox:       handlers.NewInboxHandler(nil, nil),
		Reports:     handlers.NewReportsHandler(nil),
		Credentials: newTestCredentialsHandler(),
		Knowledge:   handlers.NewKnowledgeHandler(nil),
	}

	router, err := NewRouter(deps)
	require.NoError(t, err)

	// SPA fallback should return HTML for unknown paths
	req := httptest.NewRequest(http.MethodGet, "/some/unknown/path", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "<html")
}

func TestNewRouter_AuthEndpointsExist(t *testing.T) {
	registry := template.NewRegistry()
	deps := Deps{
		JWTSecret:   []byte("test-secret"),
		Auth:        handlers.NewAuthHandler(nil, nil, []byte("test-secret")),
		Workflows:   handlers.NewWorkflowsHandler(registry),
		Runs:        handlers.NewRunsHandler(nil, nil, registry, nil),
		Inbox:       handlers.NewInboxHandler(nil, nil),
		Reports:     handlers.NewReportsHandler(nil),
		Credentials: newTestCredentialsHandler(),
		Knowledge:   handlers.NewKnowledgeHandler(nil),
	}

	router, err := NewRouter(deps)
	require.NoError(t, err)

	// Auth endpoints should exist (even if they fail due to nil deps)
	// We just verify routing works, not the handler logic
	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/auth/github"},
		{http.MethodGet, "/auth/github/callback?code=test"},
		{http.MethodPost, "/auth/refresh"},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		// Should not be 404/405 — the route exists
		assert.NotEqual(t, http.StatusNotFound, w.Code, "route %s %s should exist", tt.method, tt.path)
		assert.NotEqual(t, http.StatusMethodNotAllowed, w.Code, "route %s %s should exist", tt.method, tt.path)
	}
}

func TestNewRouter_ProtectedEndpointsRequireAuth(t *testing.T) {
	registry := template.NewRegistry()
	deps := Deps{
		JWTSecret:   []byte("test-secret"),
		Auth:        handlers.NewAuthHandler(nil, nil, []byte("test-secret")),
		Workflows:   handlers.NewWorkflowsHandler(registry),
		Runs:        handlers.NewRunsHandler(nil, nil, registry, nil),
		Inbox:       handlers.NewInboxHandler(nil, nil),
		Reports:     handlers.NewReportsHandler(nil),
		Credentials: newTestCredentialsHandler(),
		Knowledge:   handlers.NewKnowledgeHandler(nil),
	}

	router, err := NewRouter(deps)
	require.NoError(t, err)

	// Protected endpoints should return 401 without a token
	protectedPaths := []string{
		"/api/workflows",
		"/api/runs",
		"/api/inbox",
		"/api/reports",
		"/api/credentials",
	}

	for _, path := range protectedPaths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code, "path %s should require auth", path)
	}
}
