package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/tinkerloft/fleetlift/internal/auth"
)

// writeJSON encodes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// mustJSON marshals v to a JSON string, returning "null" on error.
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

// mustMarshal marshals v to JSON bytes, returning nil on error.
func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// firstTeamID extracts the first team ID from JWT claims.
// In a multi-team setup, the client would specify which team via header/query param.
func firstTeamID(claims *auth.Claims) string {
	for teamID := range claims.TeamRoles {
		return teamID
	}
	return ""
}
