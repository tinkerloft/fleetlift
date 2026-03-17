package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jmoiron/sqlx"
)

const mcpClaimsKey contextKey = "mcp_claims"

// MCPClaims holds the JWT payload for MCP sidecar tokens, scoped to a run.
type MCPClaims struct {
	TeamID string `json:"team_id"`
	RunID  string `json:"run_id"`
	jwt.RegisteredClaims
}

// IssueMCPToken creates a signed JWT with MCP claims scoped to a specific run.
// The token has audience "mcp" and expires in 24 hours.
func IssueMCPToken(secret []byte, teamID, runID string) (string, error) {
	claims := MCPClaims{
		TeamID: teamID,
		RunID:  runID,
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{"mcp"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

// ValidateMCPToken parses and validates an MCP JWT string, returning the claims.
// It rejects tokens that do not have audience "mcp".
func ValidateMCPToken(secret []byte, tokenStr string) (*MCPClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &MCPClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return secret, nil
	}, jwt.WithAudience("mcp"))
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*MCPClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid MCP token")
	}
	return claims, nil
}

// MCPAuth returns an HTTP middleware that validates MCP JWT tokens.
// It extracts a Bearer token from the Authorization header, validates it,
// checks that the associated run is not in a terminal state, and injects
// MCPClaims into the request context.
func MCPAuth(secret []byte, db *sqlx.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				writeMCPError(w, http.StatusUnauthorized, "missing or invalid Authorization header")
				return
			}
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

			claims, err := ValidateMCPToken(secret, tokenStr)
			if err != nil {
				writeMCPError(w, http.StatusUnauthorized, "invalid token")
				return
			}

			// Check that the run is not in a terminal state.
			var status string
			err = db.QueryRowContext(r.Context(),
				`SELECT status FROM runs WHERE id = $1 AND team_id = $2`,
				claims.RunID, claims.TeamID,
			).Scan(&status)
			if err != nil {
				writeMCPError(w, http.StatusUnauthorized, "run not found")
				return
			}
			switch status {
			case "complete", "failed", "cancelled":
				writeMCPError(w, http.StatusForbidden, "run is in terminal state")
				return
			}

			ctx := context.WithValue(r.Context(), mcpClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// MCPClaimsFromContext extracts the MCP JWT claims from the request context.
func MCPClaimsFromContext(ctx context.Context) *MCPClaims {
	c, _ := ctx.Value(mcpClaimsKey).(*MCPClaims)
	return c
}

// SetMCPClaimsInContext stores MCP claims in the context (for tests to inject claims).
func SetMCPClaimsInContext(ctx context.Context, claims *MCPClaims) context.Context {
	return context.WithValue(ctx, mcpClaimsKey, claims)
}

// writeMCPError writes a JSON error response.
func writeMCPError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
