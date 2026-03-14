package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// writeJSON encodes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONError writes a JSON error response: {"error": "message"}.
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
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

// teamIDFromRequest extracts and validates the team ID from the request.
// Accepts X-Team-ID header or ?team_id= query param.
// Falls back to the sole team for single-team users.
// Returns "" and writes 400/403 if ambiguous or the team is not in the JWT.
func teamIDFromRequest(w http.ResponseWriter, r *http.Request, claims *auth.Claims) string {
	teamID := r.Header.Get("X-Team-ID")
	if teamID == "" {
		teamID = r.URL.Query().Get("team_id")
	}
	if teamID != "" {
		if _, ok := claims.TeamRoles[teamID]; !ok {
			writeJSONError(w, http.StatusForbidden, "team not found in token")
			return ""
		}
		return teamID
	}
	// Convenience: single-team users don't need the header/param.
	if len(claims.TeamRoles) == 1 {
		for id := range claims.TeamRoles {
			return id
		}
	}
	writeJSONError(w, http.StatusBadRequest, "X-Team-ID header (or ?team_id= param) required for multi-team accounts")
	return ""
}

// getRunForTeam fetches a run by ID and verifies it belongs to teamID.
// Returns nil + writes 404 if not found or not owned by the team.
func getRunForTeam(ctx context.Context, db *sqlx.DB, w http.ResponseWriter, runID, teamID string) *model.Run {
	var run model.Run
	err := db.GetContext(ctx, &run, `SELECT * FROM runs WHERE id = $1 AND team_id = $2`, runID, teamID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "run not found")
		return nil
	}
	return &run
}
