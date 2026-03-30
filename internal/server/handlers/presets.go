package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"

	"github.com/tinkerloft/fleetlift/internal/auth"
)

// PresetHandlers handles CRUD for prompt presets.
type PresetHandlers struct {
	DB *sqlx.DB
}

type preset struct {
	ID        string  `db:"id" json:"id"`
	TeamID    string  `db:"team_id" json:"team_id"`
	CreatedBy *string `db:"created_by" json:"created_by"`
	Scope     string  `db:"scope" json:"scope"`
	Title     string  `db:"title" json:"title"`
	Prompt    string  `db:"prompt" json:"prompt"`
	CreatedAt string  `db:"created_at" json:"created_at"`
	UpdatedAt string  `db:"updated_at" json:"updated_at"`
}

// ListPresets returns team-scoped presets plus the user's personal presets.
func (h *PresetHandlers) ListPresets(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return
	}

	items := make([]preset, 0)
	err := h.DB.SelectContext(r.Context(), &items,
		`SELECT id, team_id, created_by, scope, title, prompt, created_at, updated_at
		 FROM prompt_presets
		 WHERE team_id = $1 AND (scope = 'team' OR created_by = $2)
		 ORDER BY created_at DESC
		 LIMIT 200`,
		teamID, claims.UserID)
	if err != nil {
		slog.Error("failed to list presets", "error", err, "team_id", teamID)
		writeJSONError(w, http.StatusInternalServerError, "failed to list presets")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type createPresetRequest struct {
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
	Scope  string `json:"scope"`
}

// CreatePreset inserts a new prompt preset.
func (h *PresetHandlers) CreatePreset(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return
	}

	var req createPresetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" || req.Prompt == "" {
		writeJSONError(w, http.StatusBadRequest, "title and prompt are required")
		return
	}
	if req.Scope != "personal" && req.Scope != "team" {
		writeJSONError(w, http.StatusBadRequest, "scope must be 'personal' or 'team'")
		return
	}
	if len(req.Title) > 200 {
		writeJSONError(w, http.StatusBadRequest, "title must be 200 characters or fewer")
		return
	}
	if len(req.Prompt) > 50000 {
		writeJSONError(w, http.StatusBadRequest, "prompt must be 50000 characters or fewer")
		return
	}

	var created preset
	err := h.DB.QueryRowxContext(r.Context(),
		`INSERT INTO prompt_presets (team_id, created_by, scope, title, prompt)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, team_id, created_by, scope, title, prompt, created_at, updated_at`,
		teamID, claims.UserID, req.Scope, req.Title, req.Prompt,
	).StructScan(&created)
	if err != nil {
		slog.Error("failed to create preset", "error", err, "team_id", teamID)
		writeJSONError(w, http.StatusInternalServerError, "failed to create preset")
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

type updatePresetRequest struct {
	Title  *string `json:"title"`
	Prompt *string `json:"prompt"`
	Scope  *string `json:"scope"`
}

// UpdatePreset modifies a preset owned by the calling user within the current team.
func (h *PresetHandlers) UpdatePreset(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return
	}

	id := chi.URLParam(r, "id")

	var req updatePresetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == nil && req.Prompt == nil && req.Scope == nil {
		writeJSONError(w, http.StatusBadRequest, "at least one field (title, prompt, scope) must be provided")
		return
	}
	if req.Scope != nil && *req.Scope != "personal" && *req.Scope != "team" {
		writeJSONError(w, http.StatusBadRequest, "scope must be 'personal' or 'team'")
		return
	}
	if req.Title != nil && *req.Title == "" {
		writeJSONError(w, http.StatusBadRequest, "title must not be empty")
		return
	}
	if req.Title != nil && len(*req.Title) > 200 {
		writeJSONError(w, http.StatusBadRequest, "title must be 200 characters or fewer")
		return
	}
	if req.Prompt != nil && *req.Prompt == "" {
		writeJSONError(w, http.StatusBadRequest, "prompt must not be empty")
		return
	}
	if req.Prompt != nil && len(*req.Prompt) > 50000 {
		writeJSONError(w, http.StatusBadRequest, "prompt must be 50000 characters or fewer")
		return
	}

	var updated preset
	err := h.DB.QueryRowxContext(r.Context(),
		`UPDATE prompt_presets
		 SET title      = COALESCE($4, title),
		     prompt     = COALESCE($5, prompt),
		     scope      = COALESCE($6, scope),
		     updated_at = NOW()
		 WHERE id = $1 AND created_by = $2 AND team_id = $3
		 RETURNING id, team_id, created_by, scope, title, prompt, created_at, updated_at`,
		id, claims.UserID, teamID, req.Title, req.Prompt, req.Scope,
	).StructScan(&updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "preset not found or not owned by you")
		} else {
			slog.Error("failed to update preset", "error", err, "id", id)
			writeJSONError(w, http.StatusInternalServerError, "failed to update preset")
		}
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

// DeletePreset removes a preset owned by the calling user within the current team.
func (h *PresetHandlers) DeletePreset(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return
	}

	id := chi.URLParam(r, "id")

	result, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM prompt_presets WHERE id = $1 AND created_by = $2 AND team_id = $3`,
		id, claims.UserID, teamID)
	if err != nil {
		slog.Error("failed to delete preset", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to delete preset")
		return
	}
	rows, err := result.RowsAffected()
	if err != nil {
		slog.Error("failed to get rows affected", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to delete preset")
		return
	}
	if rows == 0 {
		writeJSONError(w, http.StatusNotFound, "preset not found or not owned by you")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
