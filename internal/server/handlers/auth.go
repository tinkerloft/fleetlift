package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
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

	if err := h.ensurePersonalTeam(r.Context(), userID, identity); err != nil {
		slog.Error("personal team provisioning error", "error", err, "user_id", userID)
		writeJSONError(w, http.StatusInternalServerError, "failed to provision personal team")
		return
	}

	// Get team roles
	teamRoles, err := h.loadFilteredTeamRoles(r.Context(), h.db, userID)
	if err != nil {
		slog.Error("failed to load team roles", "error", err, "user_id", userID)
		writeJSONError(w, http.StatusInternalServerError, "failed to load team roles")
		return
	}

	// Check platform admin
	platformAdmin, err := h.loadPlatformAdmin(r.Context(), h.db, userID)
	if err != nil {
		slog.Error("failed to load platform admin", "error", err, "user_id", userID)
		writeJSONError(w, http.StatusInternalServerError, "failed to load platform admin")
		return
	}

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

	newRefreshToken, token, userID, err := auth.RefreshSession(r.Context(), h.db, cookie.Value, func(ctx context.Context, q sqlx.ExtContext, userID string) (string, error) {
		teamRoles, err := h.loadFilteredTeamRoles(ctx, q, userID)
		if err != nil {
			slog.Error("failed to load team roles", "error", err, "user_id", userID)
			return "", err
		}

		platformAdmin, err := h.loadPlatformAdmin(ctx, q, userID)
		if err != nil {
			slog.Error("failed to load platform admin", "error", err, "user_id", userID)
			return "", err
		}

		return auth.IssueToken(h.jwtSecret, userID, teamRoles, platformAdmin)
	})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrRefreshTokenInvalid),
			errors.Is(err, auth.ErrRefreshTokenReuseDetected),
			errors.Is(err, auth.ErrRefreshTokenExpired):
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		default:
			if userID != "" {
				slog.Error("failed to refresh session", "error", err, "user_id", userID)
			}
			writeJSONError(w, http.StatusInternalServerError, "failed to refresh session")
		}
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
	).Scan(&name, &email); err != nil {
		if err == sql.ErrNoRows {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		slog.Error("failed to fetch user profile", "error", err, "user_id", claims.UserID)
		writeJSONError(w, http.StatusInternalServerError, "failed to load user profile")
		return
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
		 JOIN users u ON u.id = tm.user_id
		 WHERE tm.user_id = $1
		   AND (u.personal_team_id IS NULL OR t.id <> u.personal_team_id)
		 ORDER BY t.name`, claims.UserID)
	if err != nil {
		slog.Error("failed to query user teams", "error", err, "user_id", claims.UserID)
		writeJSONError(w, http.StatusInternalServerError, "failed to load user teams")
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var t teamInfo
		if err := rows.StructScan(&t); err != nil {
			slog.Error("failed scanning team row", "error", err, "user_id", claims.UserID)
			writeJSONError(w, http.StatusInternalServerError, "failed to load user teams")
			return
		}
		teams = append(teams, t)
	}
	if err := rows.Err(); err != nil {
		slog.Error("error iterating team rows", "error", err, "user_id", claims.UserID)
		writeJSONError(w, http.StatusInternalServerError, "failed to load user teams")
		return
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

func (h *AuthHandler) ensurePersonalTeam(ctx context.Context, userID string, identity *auth.ExternalIdentity) error {
	tx, err := h.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var personalTeamID sql.NullString
	if err := tx.GetContext(ctx, &personalTeamID,
		`SELECT personal_team_id::text FROM users WHERE id = $1 FOR UPDATE`, userID,
	); err != nil {
		return fmt.Errorf("load personal team: %w", err)
	}
	if personalTeamID.Valid {
		return tx.Commit()
	}

	teamName := personalTeamName(identity)
	slug := personalTeamSlug(userID)

	if err := tx.GetContext(ctx, &personalTeamID,
		`INSERT INTO teams (name, slug)
		 VALUES ($1, $2)
		 ON CONFLICT (slug) DO UPDATE SET name = teams.name
		 RETURNING id::text`,
		teamName, slug,
	); err != nil {
		return fmt.Errorf("create personal team: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET personal_team_id = $1 WHERE id = $2`, personalTeamID.String, userID,
	); err != nil {
		return fmt.Errorf("link personal team: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO team_members (team_id, user_id, role)
		 VALUES ($1, $2, 'admin')
		 ON CONFLICT (team_id, user_id) DO NOTHING`, personalTeamID.String, userID,
	); err != nil {
		return fmt.Errorf("create personal team membership: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit personal team provisioning: %w", err)
	}

	return nil
}

func personalTeamSlug(userID string) string {
	return fmt.Sprintf("personal-%s", userID)
}

func personalTeamName(identity *auth.ExternalIdentity) string {
	if identity != nil && identity.Name != "" {
		return identity.Name + " Personal"
	}
	if identity != nil && identity.Email != "" {
		return identity.Email + " Personal"
	}
	return "Personal"
}

func (h *AuthHandler) loadFilteredTeamRoles(ctx context.Context, q sqlx.QueryerContext, userID string) (map[string]string, error) {
	teamRoles := map[string]string{}
	rows, err := q.QueryxContext(ctx,
		`SELECT tm.team_id, tm.role
		 FROM team_members tm
		 JOIN users u ON u.id = tm.user_id
		 WHERE tm.user_id = $1
		   AND (u.personal_team_id IS NULL OR tm.team_id <> u.personal_team_id)`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var teamID, role string
		if err := rows.Scan(&teamID, &role); err != nil {
			return nil, err
		}
		teamRoles[teamID] = role
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return teamRoles, nil
}

func (h *AuthHandler) loadPlatformAdmin(ctx context.Context, q sqlx.QueryerContext, userID string) (bool, error) {
	var platformAdmin bool
	if err := sqlx.GetContext(ctx, q, &platformAdmin,
		`SELECT platform_admin FROM users WHERE id = $1`, userID,
	); err != nil {
		return false, err
	}
	return platformAdmin, nil
}
