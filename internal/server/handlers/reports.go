package handlers

import (
	"database/sql"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// ReportsHandler handles report-mode workflow output endpoints.
type ReportsHandler struct {
	db *sqlx.DB
}

// NewReportsHandler creates a new ReportsHandler.
func NewReportsHandler(db *sqlx.DB) *ReportsHandler {
	return &ReportsHandler{db: db}
}

// List returns runs that produced report output for the user's team.
func (h *ReportsHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}
	var runs []model.Run
	err := h.db.SelectContext(r.Context(), &runs,
		`SELECT r.* FROM runs r
		 WHERE r.team_id = $1
		 AND r.status = $2
		 ORDER BY r.completed_at DESC LIMIT 50`,
		teamID, string(model.RunStatusComplete))
	if err != nil {
		slog.Error("failed to list reports", "error", err, "team_id", teamID)
		writeJSONError(w, http.StatusInternalServerError, "failed to list reports")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": runs})
}

// Get returns the report output for a specific run.
func (h *ReportsHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}
	runID := chi.URLParam(r, "runID")

	var count int
	if err := h.db.GetContext(r.Context(), &count,
		`SELECT COUNT(*) FROM runs WHERE id = $1 AND team_id = $2`, runID, teamID); err != nil || count == 0 {
		writeJSONError(w, http.StatusNotFound, "run not found")
		return
	}

	var steps []model.StepRun
	err := h.db.SelectContext(r.Context(), &steps,
		`SELECT * FROM step_runs WHERE run_id = $1 ORDER BY created_at`, runID)
	if err != nil {
		slog.Error("failed to get report steps", "error", err, "run_id", runID)
		writeJSONError(w, http.StatusInternalServerError, "failed to get report")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"run_id": runID,
		"steps":  steps,
	})
}

// Export exports a report as a downloadable format. Supports ?format=markdown.
func (h *ReportsHandler) Export(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}
	runID := chi.URLParam(r, "runID")
	format := r.URL.Query().Get("format")

	var run model.Run
	if err := h.db.GetContext(r.Context(), &run, `SELECT * FROM runs WHERE id=$1 AND team_id=$2`, runID, teamID); err != nil {
		writeJSONError(w, http.StatusNotFound, "run not found")
		return
	}

	var steps []model.StepRun
	if err := h.db.SelectContext(r.Context(), &steps,
		`SELECT * FROM step_runs WHERE run_id=$1 ORDER BY created_at`, runID); err != nil {
		slog.Error("failed to get export steps", "error", err, "run_id", runID)
		writeJSONError(w, http.StatusInternalServerError, "failed to get steps")
		return
	}

	if format == "markdown" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=report-"+runID+".md")
		renderMarkdownReport(w, run, steps)
		return
	}

	w.Header().Set("Content-Disposition", "attachment; filename=report-"+runID+".json")
	writeJSON(w, http.StatusOK, map[string]any{"run_id": runID, "run": run, "steps": steps})
}

// Artifacts returns artifacts associated with a run's steps.
func (h *ReportsHandler) Artifacts(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return
	}
	runID := chi.URLParam(r, "runID")

	// Verify run belongs to team
	var count int
	if err := h.db.GetContext(r.Context(), &count,
		`SELECT COUNT(*) FROM runs WHERE id = $1 AND team_id = $2`, runID, teamID); err != nil || count == 0 {
		writeJSONError(w, http.StatusNotFound, "run not found")
		return
	}

	type artifact struct {
		ID          string `db:"id" json:"id"`
		StepRunID   string `db:"step_run_id" json:"step_run_id"`
		Name        string `db:"name" json:"name"`
		Path        string `db:"path" json:"path"`
		SizeBytes   int64  `db:"size_bytes" json:"size_bytes"`
		ContentType string `db:"content_type" json:"content_type"`
		Storage     string `db:"storage" json:"storage"`
		CreatedAt   string `db:"created_at" json:"created_at"`
	}
	var artifacts []artifact
	err := h.db.SelectContext(r.Context(), &artifacts,
		`SELECT a.id, a.step_run_id, a.name, a.path, a.size_bytes, a.content_type, a.storage, a.created_at
		 FROM artifacts a
		 JOIN step_runs s ON a.step_run_id = s.id
		 WHERE s.run_id = $1
		 ORDER BY a.created_at`, runID)
	if err != nil {
		slog.Error("failed to list artifacts", "error", err, "run_id", runID)
		writeJSONError(w, http.StatusInternalServerError, "failed to list artifacts")
		return
	}
	if artifacts == nil {
		artifacts = []artifact{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": artifacts})
}

// ArtifactContent serves the raw bytes of a single artifact identified by {id}.
// Auth: JWT + team ownership verified via step_runs → runs → team_id.
// Query param: ?download=1 switches Content-Disposition to attachment.
// Storage "object_store" returns 501 Not Implemented.
func (h *ReportsHandler) ArtifactContent(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	teamID := teamIDFromRequest(w, r, claims)
	if teamID == "" {
		return // error already written
	}
	artifactID := chi.URLParam(r, "id")

	var artifact model.Artifact
	err := h.db.GetContext(r.Context(), &artifact,
		`SELECT a.id, a.step_run_id, a.name, a.path, a.size_bytes, a.content_type, a.storage, a.data, a.object_key, a.created_at
		 FROM artifacts a
		 JOIN step_runs s ON a.step_run_id = s.id
		 JOIN runs ru ON s.run_id = ru.id
		 WHERE a.id = $1 AND ru.team_id = $2`,
		artifactID, teamID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "artifact not found")
		} else {
			slog.Error("failed to get artifact", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	if artifact.Storage == "object_store" {
		writeJSONError(w, http.StatusNotImplemented, "object_store artifacts are not yet supported")
		return
	}

	const maxArtifactSize = 50 * 1024 * 1024 // 50MB
	if int64(len(artifact.Data)) > maxArtifactSize {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "artifact too large to serve inline")
		return
	}

	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}

	// Allowlist safe content types to prevent stored XSS if content_type is
	// ever influenced by user/agent input in the future.
	contentType := artifact.ContentType
	switch {
	case contentType == "":
		contentType = "application/octet-stream"
	case contentType == "text/html" || contentType == "text/xml" ||
		strings.HasPrefix(contentType, "text/html;") || strings.HasPrefix(contentType, "text/xml;"):
		contentType = "application/octet-stream"
	}

	params := map[string]string{"filename": artifact.Name}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, params))
	w.Header().Set("Content-Length", strconv.Itoa(len(artifact.Data)))
	_, _ = w.Write(artifact.Data)
}

const markdownReportTmpl = `# Report: {{.Run.WorkflowTitle}}

**Run ID:** {{.Run.ID}}
**Status:** {{.Run.Status}}
**Started:** {{.Run.StartedAt}}
**Completed:** {{.Run.CompletedAt}}

---

## Steps

| Step | Status | Duration |
|------|--------|----------|
{{- range .Steps}}
| {{.StepTitle}} | {{.Status}} | {{stepDuration .}} |
{{- end}}

{{range .Steps}}
### {{.StepTitle}}

**Status:** {{.Status}}
{{if .ErrorMessage}}**Error:** {{.ErrorMessage}}{{end}}
{{if .PRUrl}}**PR:** {{.PRUrl}}{{end}}
{{end}}
`

var reportTmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"stepDuration": func(s model.StepRun) string {
		if s.StartedAt == nil || s.CompletedAt == nil {
			return "—"
		}
		return s.CompletedAt.Sub(*s.StartedAt).Round(time.Second).String()
	},
}).Parse(markdownReportTmpl))

func renderMarkdownReport(w http.ResponseWriter, run model.Run, steps []model.StepRun) {
	_ = reportTmpl.Execute(w, map[string]any{"Run": run, "Steps": steps})
}
