package auth

import (
	"context"
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
