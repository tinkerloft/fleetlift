package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// ProfilesHandler handles agent profile and marketplace CRUD endpoints.
type ProfilesHandler struct {
	db *sqlx.DB
}

// NewProfilesHandler creates a new ProfilesHandler.
func NewProfilesHandler(db *sqlx.DB) *ProfilesHandler {
	return &ProfilesHandler{db: db}
}

// validateAgentProfileBody validates all plugin and skill sources in the body.
func validateAgentProfileBody(body *model.AgentProfileBody) error {
	for i, p := range body.Plugins {
		if err := p.Validate(); err != nil {
			return fmt.Errorf("plugins[%d]: %w", i, err)
		}
	}
	for i, s := range body.Skills {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("skills[%d]: %w", i, err)
		}
	}
	for i, m := range body.MCPs {
		if m.Name == "" {
			return fmt.Errorf("mcps[%d]: name is required", i)
		}
		if m.URL == "" {
			return fmt.Errorf("mcps[%d]: url is required", i)
		}
		if !strings.HasPrefix(m.URL, "https://") && !strings.HasPrefix(m.URL, "http://") {
			return fmt.Errorf("mcps[%d]: url must use http:// or https:// scheme", i)
		}
	}
	return nil
}

// --- Agent Profiles ---

type createProfileRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Body        model.AgentProfileBody `json:"body"`
}

// ListProfiles returns all agent profiles visible to the team.
func (h *ProfilesHandler) ListProfiles(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return
	}

	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, team_id, name, description, body, created_at, updated_at
		 FROM agent_profiles WHERE team_id = $1 OR team_id IS NULL
		 ORDER BY name`, teamID)
	if err != nil {
		slog.Error("failed to list agent profiles", "error", err, "team_id", teamID)
		writeJSONError(w, http.StatusInternalServerError, "failed to list agent profiles")
		return
	}
	defer rows.Close()

	profiles := make([]model.AgentProfile, 0)
	for rows.Next() {
		var p model.AgentProfile
		var bodyBytes []byte
		if err := rows.Scan(&p.ID, &p.TeamID, &p.Name, &p.Description, &bodyBytes, &p.CreatedAt, &p.UpdatedAt); err != nil {
			slog.Error("failed to scan agent profile", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "failed to list agent profiles")
			return
		}
		if err := json.Unmarshal(bodyBytes, &p.Body); err != nil {
			slog.Error("failed to unmarshal agent profile body", "error", err, "id", p.ID)
			writeJSONError(w, http.StatusInternalServerError, "failed to list agent profiles")
			return
		}
		profiles = append(profiles, p)
	}
	if err := rows.Err(); err != nil {
		slog.Error("rows iteration error", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to list agent profiles")
		return
	}

	writeJSON(w, http.StatusOK, profiles)
}

// CreateProfile creates a new agent profile.
func (h *ProfilesHandler) CreateProfile(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return
	}

	var req createProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := validateAgentProfileBody(&req.Body); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	bodyJSON, err := json.Marshal(req.Body)
	if err != nil {
		slog.Error("failed to marshal profile body", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to create profile")
		return
	}

	now := time.Now()
	profile := model.AgentProfile{
		ID:          uuid.New().String(),
		TeamID:      &teamID,
		Name:        req.Name,
		Description: req.Description,
		Body:        req.Body,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	_, err = h.db.ExecContext(r.Context(),
		`INSERT INTO agent_profiles (id, team_id, name, description, body, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		profile.ID, profile.TeamID, profile.Name, profile.Description, bodyJSON, profile.CreatedAt, profile.UpdatedAt)
	if err != nil {
		slog.Error("failed to insert agent profile", "error", err, "team_id", teamID)
		writeJSONError(w, http.StatusInternalServerError, "failed to create profile")
		return
	}

	writeJSON(w, http.StatusCreated, profile)
}

// GetProfile returns a single agent profile by ID.
func (h *ProfilesHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
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

	var p model.AgentProfile
	var bodyBytes []byte
	err := h.db.QueryRowContext(r.Context(),
		`SELECT id, team_id, name, description, body, created_at, updated_at
		 FROM agent_profiles WHERE id = $1 AND (team_id = $2 OR team_id IS NULL)`,
		id, teamID).Scan(&p.ID, &p.TeamID, &p.Name, &p.Description, &bodyBytes, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "profile not found")
		return
	}
	if err := json.Unmarshal(bodyBytes, &p.Body); err != nil {
		slog.Error("failed to unmarshal agent profile body", "error", err, "id", id)
		writeJSONError(w, http.StatusInternalServerError, "failed to get profile")
		return
	}

	writeJSON(w, http.StatusOK, p)
}

// UpdateProfile updates an existing agent profile.
func (h *ProfilesHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
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

	var req createProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validateAgentProfileBody(&req.Body); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	bodyJSON, err := json.Marshal(req.Body)
	if err != nil {
		slog.Error("failed to marshal profile body", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to update profile")
		return
	}

	result, err := h.db.ExecContext(r.Context(),
		`UPDATE agent_profiles SET description = $1, body = $2, updated_at = $3
		 WHERE id = $4 AND team_id = $5`,
		req.Description, bodyJSON, time.Now(), id, teamID)
	if err != nil {
		slog.Error("failed to update agent profile", "error", err, "team_id", teamID, "id", id)
		writeJSONError(w, http.StatusInternalServerError, "failed to update profile")
		return
	}

	rows, err := result.RowsAffected()
	if err != nil {
		slog.Error("failed to check update result", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to update profile")
		return
	}
	if rows == 0 {
		writeJSONError(w, http.StatusNotFound, "profile not found")
		return
	}

	// Return the updated profile by re-fetching.
	var p model.AgentProfile
	var bodyBytesOut []byte
	err = h.db.QueryRowContext(r.Context(),
		`SELECT id, team_id, name, description, body, created_at, updated_at
		 FROM agent_profiles WHERE id = $1 AND team_id = $2`,
		id, teamID).Scan(&p.ID, &p.TeamID, &p.Name, &p.Description, &bodyBytesOut, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		slog.Error("failed to re-fetch updated profile", "error", err, "id", id)
		writeJSONError(w, http.StatusInternalServerError, "profile updated but failed to re-fetch")
		return
	}
	if err := json.Unmarshal(bodyBytesOut, &p.Body); err != nil {
		slog.Error("failed to unmarshal updated profile body", "error", err, "id", id)
		writeJSONError(w, http.StatusInternalServerError, "profile updated but body corrupt")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// DeleteProfile deletes an agent profile.
func (h *ProfilesHandler) DeleteProfile(w http.ResponseWriter, r *http.Request) {
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
	result, err := h.db.ExecContext(r.Context(),
		`DELETE FROM agent_profiles WHERE id = $1 AND team_id = $2`, id, teamID)
	if err != nil {
		slog.Error("failed to delete agent profile", "error", err, "team_id", teamID, "id", id)
		writeJSONError(w, http.StatusInternalServerError, "failed to delete profile")
		return
	}

	rows, err := result.RowsAffected()
	if err != nil {
		slog.Error("failed to check delete result", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to delete profile")
		return
	}
	if rows == 0 {
		writeJSONError(w, http.StatusNotFound, "profile not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Marketplaces ---

type createMarketplaceRequest struct {
	Name       string `json:"name"`
	RepoURL    string `json:"repo_url"`
	Credential string `json:"credential"`
}

// ListMarketplaces returns all marketplaces visible to the team.
func (h *ProfilesHandler) ListMarketplaces(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return
	}

	marketplaces := make([]model.Marketplace, 0)
	err := h.db.SelectContext(r.Context(), &marketplaces,
		`SELECT id, name, repo_url, credential, team_id, created_at
		 FROM marketplaces WHERE team_id = $1 OR team_id IS NULL
		 ORDER BY name`, teamID)
	if err != nil {
		slog.Error("failed to list marketplaces", "error", err, "team_id", teamID)
		writeJSONError(w, http.StatusInternalServerError, "failed to list marketplaces")
		return
	}

	writeJSON(w, http.StatusOK, marketplaces)
}

// CreateMarketplace creates a new marketplace.
func (h *ProfilesHandler) CreateMarketplace(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return
	}

	var req createMarketplaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.RepoURL == "" {
		writeJSONError(w, http.StatusBadRequest, "repo_url is required")
		return
	}
	if !strings.HasPrefix(req.RepoURL, "https://") {
		writeJSONError(w, http.StatusBadRequest, "repo_url must use https:// scheme")
		return
	}

	m := model.Marketplace{
		ID:         uuid.New().String(),
		Name:       req.Name,
		RepoURL:    req.RepoURL,
		Credential: req.Credential,
		TeamID:     &teamID,
		CreatedAt:  time.Now(),
	}

	_, err := h.db.ExecContext(r.Context(),
		`INSERT INTO marketplaces (id, name, repo_url, credential, team_id, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		m.ID, m.Name, m.RepoURL, m.Credential, m.TeamID, m.CreatedAt)
	if err != nil {
		slog.Error("failed to insert marketplace", "error", err, "team_id", teamID)
		writeJSONError(w, http.StatusInternalServerError, "failed to create marketplace")
		return
	}

	writeJSON(w, http.StatusCreated, m)
}

// DeleteMarketplace deletes a marketplace.
func (h *ProfilesHandler) DeleteMarketplace(w http.ResponseWriter, r *http.Request) {
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
	result, err := h.db.ExecContext(r.Context(),
		`DELETE FROM marketplaces WHERE id = $1 AND team_id = $2`, id, teamID)
	if err != nil {
		slog.Error("failed to delete marketplace", "error", err, "team_id", teamID, "id", id)
		writeJSONError(w, http.StatusInternalServerError, "failed to delete marketplace")
		return
	}

	rows, err := result.RowsAffected()
	if err != nil {
		slog.Error("failed to check delete result", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to delete marketplace")
		return
	}
	if rows == 0 {
		writeJSONError(w, http.StatusNotFound, "marketplace not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
