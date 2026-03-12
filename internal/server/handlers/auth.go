package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/jmoiron/sqlx"

	"github.com/tinkerloft/fleetlift/internal/auth"
)

// AuthHandler handles OAuth and token refresh endpoints.
type AuthHandler struct {
	db        *sqlx.DB
	provider  auth.Provider
	jwtSecret []byte
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(db *sqlx.DB, provider auth.Provider, jwtSecret []byte) *AuthHandler {
	return &AuthHandler{db: db, provider: provider, jwtSecret: jwtSecret}
}

// HandleGitHubRedirect redirects the user to GitHub for OAuth.
func (h *AuthHandler) HandleGitHubRedirect(w http.ResponseWriter, r *http.Request) {
	state := randomState()
	url := h.provider.AuthURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// HandleGitHubCallback handles the OAuth callback from GitHub.
func (h *AuthHandler) HandleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code parameter", http.StatusBadRequest)
		return
	}

	identity, err := h.provider.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "oauth exchange failed", http.StatusInternalServerError)
		return
	}

	// Upsert user
	var userID string
	err = h.db.GetContext(r.Context(), &userID,
		`INSERT INTO users (email, name, provider, provider_id)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (provider, provider_id) DO UPDATE SET email = $1, name = $2
		 RETURNING id`,
		identity.Email, identity.Name, identity.Provider, identity.ProviderID)
	if err != nil {
		http.Error(w, "failed to upsert user", http.StatusInternalServerError)
		return
	}

	// Get team roles
	teamRoles := map[string]string{}
	rows, err := h.db.QueryxContext(r.Context(),
		`SELECT team_id, role FROM team_members WHERE user_id = $1`, userID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var teamID, role string
			if rows.Scan(&teamID, &role) == nil {
				teamRoles[teamID] = role
			}
		}
	}

	// Check platform admin
	var platformAdmin bool
	_ = h.db.GetContext(r.Context(), &platformAdmin,
		`SELECT platform_admin FROM users WHERE id = $1`, userID)

	token, err := auth.IssueToken(h.jwtSecret, userID, teamRoles, platformAdmin)
	if err != nil {
		http.Error(w, "failed to issue token", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

// HandleRefresh refreshes an expired JWT token.
func (h *AuthHandler) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	// For now, require a valid token and re-issue
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	token, err := auth.IssueToken(h.jwtSecret, claims.UserID, claims.TeamRoles, claims.PlatformAdmin)
	if err != nil {
		http.Error(w, "failed to issue token", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

// HandleMe returns the authenticated user's identity.
func (h *AuthHandler) HandleMe(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":        claims.UserID,
		"team_roles":     claims.TeamRoles,
		"platform_admin": claims.PlatformAdmin,
	})
}

func randomState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
