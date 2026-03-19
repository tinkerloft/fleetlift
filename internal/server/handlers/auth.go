package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

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
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/auth/github/callback",
		MaxAge:   600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
	http.Redirect(w, r, h.provider.AuthURL(state), http.StatusTemporaryRedirect)
}

// HandleGitHubCallback handles the OAuth callback from GitHub.
func (h *AuthHandler) HandleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	// Validate OAuth state to prevent CSRF
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || stateCookie.Value == "" {
		writeJSONError(w, http.StatusBadRequest, "missing oauth state")
		return
	}
	// Clear the cookie regardless
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", Value: "", Path: "/auth/github/callback", MaxAge: -1})

	returnedState := r.URL.Query().Get("state")
	if returnedState == "" || returnedState != stateCookie.Value {
		writeJSONError(w, http.StatusBadRequest, "invalid oauth state")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		writeJSONError(w, http.StatusBadRequest, "missing code parameter")
		return
	}

	identity, err := h.provider.Exchange(r.Context(), code)
	if err != nil {
		slog.Error("oauth exchange error", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "oauth exchange failed")
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
		writeJSONError(w, http.StatusInternalServerError, "failed to upsert user")
		return
	}

	// Auto-provision a personal team on first login if the user has none.
	var teamCount int
	_ = h.db.GetContext(r.Context(), &teamCount,
		`SELECT COUNT(*) FROM team_members WHERE user_id = $1`, userID)
	if teamCount == 0 {
		slug := identity.Name
		if slug == "" {
			slug = identity.Email
		}
		if _, err := h.db.ExecContext(r.Context(),
			`WITH t AS (
				INSERT INTO teams (name, slug)
				VALUES ($1, $2)
				ON CONFLICT (slug) DO UPDATE SET name = $1
				RETURNING id
			)
			INSERT INTO team_members (team_id, user_id, role)
			SELECT id, $3, 'admin' FROM t
			ON CONFLICT DO NOTHING`,
			identity.Name, slug, userID,
		); err != nil {
			slog.Error("auto-provision team error", "error", err, "user_id", userID)
		}
	}

	// Get team roles
	teamRoles := map[string]string{}
	rows, err := h.db.QueryxContext(r.Context(),
		`SELECT team_id, role FROM team_members WHERE user_id = $1`, userID)
	if err == nil {
		defer func() { _ = rows.Close() }()
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
		writeJSONError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "fl_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   3600,
	})

	refreshToken, err := auth.IssueRefreshToken(r.Context(), h.db, userID)
	if err == nil {
		http.SetCookie(w, &http.Cookie{
			Name:     "refresh_token",
			Value:    refreshToken,
			Path:     "/auth/refresh",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   int(30 * 24 * time.Hour / time.Second),
		})
	}

	http.Redirect(w, r, "/auth/callback?token="+token, http.StatusTemporaryRedirect)
}

// HandleRefresh rotates the refresh token and issues a new access JWT.
func (h *AuthHandler) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("refresh_token")
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "missing refresh token")
		return
	}

	newRefreshToken, userID, err := auth.RotateRefreshToken(r.Context(), h.db, cookie.Value)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Re-query current roles from DB to prevent stale claim perpetuation
	teamRoles := map[string]string{}
	rows, err := h.db.QueryxContext(r.Context(),
		`SELECT team_id, role FROM team_members WHERE user_id = $1`, userID)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var teamID, role string
			if rows.Scan(&teamID, &role) == nil {
				teamRoles[teamID] = role
			}
		}
	}

	var platformAdmin bool
	_ = h.db.GetContext(r.Context(), &platformAdmin,
		`SELECT platform_admin FROM users WHERE id = $1`, userID)

	token, err := auth.IssueToken(h.jwtSecret, userID, teamRoles, platformAdmin)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    newRefreshToken,
		Path:     "/auth/refresh",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(30 * 24 * time.Hour / time.Second),
	})

	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

// HandleMe returns the authenticated user's identity with enriched profile data.
func (h *AuthHandler) HandleMe(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Fetch user profile from DB
	var name, email string
	if err := h.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(name, ''), COALESCE(email, '') FROM users WHERE id = $1`,
		claims.UserID,
	).Scan(&name, &email); err != nil && err != sql.ErrNoRows {
		slog.Warn("failed to fetch user profile", "err", err, "user_id", claims.UserID)
	}

	// Fetch team details
	type teamInfo struct {
		ID   string `json:"id" db:"id"`
		Name string `json:"name" db:"name"`
		Slug string `json:"slug" db:"slug"`
		Role string `json:"role" db:"role"`
	}
	var teams []teamInfo
	rows, err := h.db.QueryxContext(r.Context(),
		`SELECT t.id, t.name, t.slug, tm.role
		 FROM teams t JOIN team_members tm ON t.id = tm.team_id
		 WHERE tm.user_id = $1 ORDER BY t.name`, claims.UserID)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var t teamInfo
			if rows.StructScan(&t) == nil {
				teams = append(teams, t)
			}
		}
		if err := rows.Err(); err != nil {
			slog.Warn("error iterating team rows", "err", err, "user_id", claims.UserID)
		}
	}
	if teams == nil {
		teams = []teamInfo{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":        claims.UserID,
		"name":           name,
		"email":          email,
		"teams":          teams,
		"team_roles":     claims.TeamRoles,
		"platform_admin": claims.PlatformAdmin,
	})
}

func randomState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
