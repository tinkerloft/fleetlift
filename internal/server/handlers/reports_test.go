package handlers

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestArtifacts_RequiresAuth(t *testing.T) {
	h := NewReportsHandler(nil)
	r := chi.NewRouter()
	r.Get("/api/reports/{runID}/artifacts", h.Artifacts)

	req := httptest.NewRequest("GET", "/api/reports/run-1/artifacts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestArtifactContent_RequiresAuth(t *testing.T) {
	h := NewReportsHandler(nil)
	r := chi.NewRouter()
	r.Get("/api/artifacts/{id}/content", h.ArtifactContent)

	req := httptest.NewRequest("GET", "/api/artifacts/artifact-1/content", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func newReportsMockDB(t *testing.T) (*sqlx.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return sqlx.NewDb(db, "sqlmock"), mock
}

func TestArtifactContent_NotFound(t *testing.T) {
	sqlxDB, mock := newReportsMockDB(t)
	mock.ExpectQuery(`SELECT a\.id`).
		WithArgs("nonexistent-id", "team-1").
		WillReturnError(sql.ErrNoRows)

	h := NewReportsHandler(sqlxDB)
	r := chi.NewRouter()
	r.Get("/api/artifacts/{id}/content", h.ArtifactContent)

	req := httptest.NewRequest("GET", "/api/artifacts/nonexistent-id/content", nil)
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "artifact not found")
}

func TestArtifactContent_CrossTeamIsolation(t *testing.T) {
	sqlxDB, mock := newReportsMockDB(t)
	// team-B requests artifact owned by team-A; the JOIN returns no rows.
	mock.ExpectQuery(`SELECT a\.id`).
		WithArgs("artifact-team-a", "team-b").
		WillReturnError(sql.ErrNoRows)

	h := NewReportsHandler(sqlxDB)
	r := chi.NewRouter()
	r.Get("/api/artifacts/{id}/content", h.ArtifactContent)

	req := httptest.NewRequest("GET", "/api/artifacts/artifact-team-a/content", nil)
	req = claimsCtx(req, "team-b")
	req.Header.Set("X-Team-ID", "team-b")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "artifact not found")
}

func TestArtifactContent_ServesContent(t *testing.T) {
	sqlxDB, mock := newReportsMockDB(t)

	artifactContent := []byte("hello, world")
	rows := sqlmock.NewRows([]string{
		"id", "step_run_id", "name", "path", "size_bytes", "content_type", "storage", "data", "object_key", "created_at",
	}).AddRow(
		"artifact-1", "step-1", "report.txt", "/workspace/report.txt",
		int64(len(artifactContent)), "text/plain", "inline", artifactContent, "", time.Now(),
	)
	mock.ExpectQuery(`SELECT a\.id`).
		WithArgs("artifact-1", "team-1").
		WillReturnRows(rows)

	h := NewReportsHandler(sqlxDB)
	r := chi.NewRouter()
	r.Get("/api/artifacts/{id}/content", h.ArtifactContent)

	req := httptest.NewRequest("GET", "/api/artifacts/artifact-1/content", nil)
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
	assert.Contains(t, w.Header().Get("Content-Disposition"), "inline")
	assert.Equal(t, string(artifactContent), w.Body.String())
}

func TestArtifactContent_DownloadParam(t *testing.T) {
	sqlxDB, mock := newReportsMockDB(t)

	artifactContent := []byte("hello, world")
	rows := sqlmock.NewRows([]string{
		"id", "step_run_id", "name", "path", "size_bytes", "content_type", "storage", "data", "object_key", "created_at",
	}).AddRow(
		"artifact-1", "step-1", "report.txt", "/workspace/report.txt",
		int64(len(artifactContent)), "text/plain", "inline", artifactContent, "", time.Now(),
	)
	mock.ExpectQuery(`SELECT a\.id`).
		WithArgs("artifact-1", "team-1").
		WillReturnRows(rows)

	h := NewReportsHandler(sqlxDB)
	r := chi.NewRouter()
	r.Get("/api/artifacts/{id}/content", h.ArtifactContent)

	req := httptest.NewRequest("GET", "/api/artifacts/artifact-1/content?download=1", nil)
	req = claimsCtx(req, "team-1")
	req.Header.Set("X-Team-ID", "team-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
}
