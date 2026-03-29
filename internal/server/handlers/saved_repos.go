package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"

	"github.com/tinkerloft/fleetlift/internal/auth"
)

// SavedRepoHandlers handles CRUD for user-saved repository bookmarks.
type SavedRepoHandlers struct {
	DB *sqlx.DB
}

type savedRepo struct {
	ID        string  `db:"id" json:"id"`
	UserID    string  `db:"user_id" json:"user_id"`
	URL       string  `db:"url" json:"url"`
	Label     *string `db:"label" json:"label"`
	CreatedAt string  `db:"created_at" json:"created_at"`
}

// ListSavedRepos returns the calling user's saved repo bookmarks.
func (h *SavedRepoHandlers) ListSavedRepos(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	items := make([]savedRepo, 0)
	err := h.DB.SelectContext(r.Context(), &items,
		`SELECT id, user_id, url, label, created_at
		 FROM user_repos
		 WHERE user_id = $1
		 ORDER BY created_at DESC
		 LIMIT 100`,
		claims.UserID)
	if err != nil {
		slog.Error("failed to list saved repos", "error", err, "user_id", claims.UserID)
		writeJSONError(w, http.StatusInternalServerError, "failed to list saved repos")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type createSavedRepoRequest struct {
	URL   string  `json:"url"`
	Label *string `json:"label"`
}

// CreateSavedRepo bookmarks a repository URL for the calling user.
func (h *SavedRepoHandlers) CreateSavedRepo(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createSavedRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.URL == "" {
		writeJSONError(w, http.StatusBadRequest, "url is required")
		return
	}
	if len(req.URL) > 2048 {
		writeJSONError(w, http.StatusBadRequest, "url must be 2048 characters or fewer")
		return
	}
	parsed, parseErr := url.Parse(req.URL)
	if parseErr != nil || parsed.Scheme != "https" || parsed.Host == "" {
		writeJSONError(w, http.StatusBadRequest, "url must use https:// scheme")
		return
	}
	if req.Label != nil && len(*req.Label) > 200 {
		writeJSONError(w, http.StatusBadRequest, "label must be 200 characters or fewer")
		return
	}

	var created savedRepo
	err := h.DB.QueryRowxContext(r.Context(),
		`INSERT INTO user_repos (user_id, url, label)
		 VALUES ($1, $2, $3)
		 RETURNING id, user_id, url, label, created_at`,
		claims.UserID, req.URL, req.Label,
	).StructScan(&created)
	if err != nil {
		if isDuplicateError(err) {
			writeJSONError(w, http.StatusConflict, "repository already saved")
			return
		}
		slog.Error("failed to save repo", "error", err, "user_id", claims.UserID)
		writeJSONError(w, http.StatusInternalServerError, "failed to save repo")
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

// DeleteSavedRepo removes a saved repo owned by the calling user.
func (h *SavedRepoHandlers) DeleteSavedRepo(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := chi.URLParam(r, "id")

	result, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM user_repos WHERE id = $1 AND user_id = $2`,
		id, claims.UserID)
	if err != nil {
		slog.Error("failed to delete saved repo", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to delete saved repo")
		return
	}
	rows, err := result.RowsAffected()
	if err != nil {
		slog.Error("failed to get rows affected", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to delete saved repo")
		return
	}
	if rows == 0 {
		writeJSONError(w, http.StatusNotFound, "saved repo not found or not owned by you")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
