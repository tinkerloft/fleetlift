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

// getRunForTeam fetches a run by ID and verifies it belongs to teamID.
// Returns nil + writes 404 if not found or not owned by the team.
func getRunForTeam(ctx context.Context, db *sqlx.DB, w http.ResponseWriter, runID, teamID string) *model.Run {
	var run model.Run
	err := db.GetContext(ctx, &run, `SELECT * FROM runs WHERE id = $1 AND team_id = $2`, runID, teamID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return nil
	}
	return &run
}
