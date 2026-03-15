package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var testMCPSecret = []byte("test-mcp-secret-key")

func TestIssueMCPToken(t *testing.T) {
	token, err := IssueMCPToken(testMCPSecret, "team-1", "run-1")
	if err != nil {
		t.Fatalf("IssueMCPToken returned error: %v", err)
	}
	if token == "" {
		t.Fatal("IssueMCPToken returned empty token")
	}
}

func TestValidateMCPToken_Valid(t *testing.T) {
	token, err := IssueMCPToken(testMCPSecret, "team-abc", "run-xyz")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	claims, err := ValidateMCPToken(testMCPSecret, token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.TeamID != "team-abc" {
		t.Errorf("TeamID = %q, want %q", claims.TeamID, "team-abc")
	}
	if claims.RunID != "run-xyz" {
		t.Errorf("RunID = %q, want %q", claims.RunID, "run-xyz")
	}
	// Check audience
	aud, _ := claims.GetAudience()
	if len(aud) != 1 || aud[0] != "mcp" {
		t.Errorf("Audience = %v, want [mcp]", aud)
	}
	// Check expiry is ~24h from now
	exp, _ := claims.GetExpirationTime()
	if exp == nil {
		t.Fatal("no expiry set")
	}
	diff := time.Until(exp.Time)
	if diff < 23*time.Hour || diff > 25*time.Hour {
		t.Errorf("expiry diff = %v, want ~24h", diff)
	}
}

func TestValidateMCPToken_WrongSecret(t *testing.T) {
	token, err := IssueMCPToken(testMCPSecret, "team-1", "run-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	_, err = ValidateMCPToken([]byte("wrong-secret"), token)
	if err == nil {
		t.Fatal("expected error for wrong secret, got nil")
	}
}

func TestValidateMCPToken_WrongAudience(t *testing.T) {
	// A user token (from IssueToken) should be rejected by ValidateMCPToken
	// because it lacks aud:"mcp"
	userToken, err := IssueToken(testMCPSecret, "user-1", map[string]string{"team-1": "admin"}, false)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	_, err = ValidateMCPToken(testMCPSecret, userToken)
	if err == nil {
		t.Fatal("expected error when validating user token as MCP token, got nil")
	}
}

func TestValidateMCPToken_UserTokenCannotUseMCP(t *testing.T) {
	// An MCP token should be rejected by the user ValidateToken
	mcpToken, err := IssueMCPToken(testMCPSecret, "team-1", "run-1")
	if err != nil {
		t.Fatalf("IssueMCPToken: %v", err)
	}

	_, err = ValidateToken(testMCPSecret, mcpToken)
	if err == nil {
		t.Fatal("expected error when validating MCP token as user token, got nil")
	}
}

func TestMCPClaimsFromContext(t *testing.T) {
	claims := &MCPClaims{
		TeamID: "team-1",
		RunID:  "run-1",
		RegisteredClaims: jwt.RegisteredClaims{
			Audience: jwt.ClaimStrings{"mcp"},
		},
	}

	ctx := SetMCPClaimsInContext(context.Background(), claims)
	got := MCPClaimsFromContext(ctx)
	if got == nil {
		t.Fatal("MCPClaimsFromContext returned nil")
	}
	if got.TeamID != "team-1" {
		t.Errorf("TeamID = %q, want %q", got.TeamID, "team-1")
	}
	if got.RunID != "run-1" {
		t.Errorf("RunID = %q, want %q", got.RunID, "run-1")
	}
}

func TestMCPClaimsFromContext_Empty(t *testing.T) {
	got := MCPClaimsFromContext(context.Background())
	if got != nil {
		t.Errorf("expected nil from empty context, got %+v", got)
	}
}

// --- MCPAuth middleware HTTP tests ---

func dummyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := MCPClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"team_id": claims.TeamID,
			"run_id":  claims.RunID,
		})
	})
}

func TestMCPAuth_MissingAuthHeader(t *testing.T) {
	// nil DB is fine — middleware returns before DB query when header is missing.
	handler := MCPAuth(testMCPSecret, nil)(dummyHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/mcp/run", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] == "" {
		t.Fatal("expected error message in response")
	}
}

func TestMCPAuth_InvalidToken(t *testing.T) {
	// nil DB is fine — middleware returns before DB query when token is invalid.
	handler := MCPAuth(testMCPSecret, nil)(dummyHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/mcp/run", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-string")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestMCPAuth_WrongSecret(t *testing.T) {
	// Issue token with one secret, validate with another.
	token, err := IssueMCPToken([]byte("other-secret"), "team-1", "run-1")
	if err != nil {
		t.Fatal(err)
	}

	handler := MCPAuth(testMCPSecret, nil)(dummyHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/mcp/run", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestMCPAuth_NonBearerScheme(t *testing.T) {
	handler := MCPAuth(testMCPSecret, nil)(dummyHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/mcp/run", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
