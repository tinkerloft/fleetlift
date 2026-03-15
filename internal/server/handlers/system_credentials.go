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

// SystemCredentialsHandler handles system-wide credential endpoints (admin only).
type SystemCredentialsHandler struct {
	db            *sqlx.DB
	encryptionKey string
}

// NewSystemCredentialsHandler creates a new SystemCredentialsHandler.
func NewSystemCredentialsHandler(db *sqlx.DB, encryptionKeyHex string) (*SystemCredentialsHandler, error) {
	key, err := hex.DecodeString(encryptionKeyHex)
	if err != nil {
		return nil, fmt.Errorf("decode encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY must be exactly 32 bytes (64 hex chars), got %d bytes", len(key))
	}
	return &SystemCredentialsHandler{db: db, encryptionKey: encryptionKeyHex}, nil
}

// List returns system credential names (not values). Requires PlatformAdmin.
func (h *SystemCredentialsHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil || !claims.PlatformAdmin {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	creds := make([]credentialEntry, 0)
	err := h.db.SelectContext(r.Context(), &creds,
		`SELECT name, created_at, updated_at FROM credentials WHERE team_id IS NULL ORDER BY name`)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list system credentials")
		return
	}

	writeJSON(w, http.StatusOK, creds)
}

// Set creates or updates a system credential. Requires PlatformAdmin.
func (h *SystemCredentialsHandler) Set(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil || !claims.PlatformAdmin {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req setCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Value == "" {
		writeJSONError(w, http.StatusBadRequest, "name and value are required")
		return
	}
	if err := validateCredentialName(req.Name); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	encrypted, err := flcrypto.EncryptAESGCM(h.encryptionKey, req.Value)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "encryption failed")
		return
	}

	_, err = h.db.ExecContext(r.Context(),
		`INSERT INTO credentials (team_id, name, value_enc)
		 VALUES (NULL, $1, $2)
		 ON CONFLICT (name) WHERE team_id IS NULL
		 DO UPDATE SET value_enc = $2, updated_at = now()`,
		req.Name, encrypted)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to save system credential")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Delete removes a system credential. Requires PlatformAdmin.
func (h *SystemCredentialsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil || !claims.PlatformAdmin {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	name := chi.URLParam(r, "name")

	result, err := h.db.ExecContext(r.Context(),
		`DELETE FROM credentials WHERE team_id IS NULL AND name = $1`,
		name)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to delete system credential")
		return
	}

	rows, err := result.RowsAffected()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to check deletion result")
		return
	}
	if rows == 0 {
		writeJSONError(w, http.StatusNotFound, "system credential not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
