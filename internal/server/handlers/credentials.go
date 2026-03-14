package handlers

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"

	"github.com/tinkerloft/fleetlift/internal/auth"
	flcrypto "github.com/tinkerloft/fleetlift/internal/crypto"
)

// CredentialsHandler handles team credential management endpoints.
type CredentialsHandler struct {
	db            *sqlx.DB
	encryptionKey string // hex-encoded 32-byte AES-256 key
}

// NewCredentialsHandler creates a new CredentialsHandler.
func NewCredentialsHandler(db *sqlx.DB, encryptionKeyHex string) (*CredentialsHandler, error) {
	key, err := hex.DecodeString(encryptionKeyHex)
	if err != nil {
		return nil, fmt.Errorf("decode encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY must be exactly 32 bytes (64 hex chars), got %d bytes", len(key))
	}
	return &CredentialsHandler{db: db, encryptionKey: encryptionKeyHex}, nil
}

type credentialEntry struct {
	Name      string `db:"name" json:"name"`
	CreatedAt string `db:"created_at" json:"created_at"`
	UpdatedAt string `db:"updated_at" json:"updated_at"`
}

// List returns credential names (not values) for the user's team.
func (h *CredentialsHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}
	var creds []credentialEntry
	err := h.db.SelectContext(r.Context(), &creds,
		`SELECT name, created_at, updated_at FROM credentials WHERE team_id = $1 ORDER BY name`,
		teamID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list credentials")
		return
	}

	writeJSON(w, http.StatusOK, creds)
}

type setCredentialRequest struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Set creates or updates a team credential.
func (h *CredentialsHandler) Set(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req setCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.Value == "" {
		writeJSONError(w, http.StatusBadRequest, "name and value are required")
		return
	}

	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}

	encrypted, err := flcrypto.EncryptAESGCM(h.encryptionKey, req.Value)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "encryption failed")
		return
	}

	_, err = h.db.ExecContext(r.Context(),
		`INSERT INTO credentials (team_id, name, value_enc)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (team_id, name) DO UPDATE SET value_enc = $3, updated_at = now()`,
		teamID, req.Name, encrypted)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to save credential")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Delete removes a team credential.
func (h *CredentialsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}
	name := chi.URLParam(r, "name")

	result, err := h.db.ExecContext(r.Context(),
		`DELETE FROM credentials WHERE team_id = $1 AND name = $2`,
		teamID, name)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to delete credential")
		return
	}

	rows, err := result.RowsAffected()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to check deletion result")
		return
	}
	if rows == 0 {
		writeJSONError(w, http.StatusNotFound, "credential not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
