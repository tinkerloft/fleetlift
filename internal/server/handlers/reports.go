package handlers

import (
	"net/http"
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
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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
		http.Error(w, "failed to list reports", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": runs})
}

// Get returns the report output for a specific run.
func (h *ReportsHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	var steps []model.StepRun
	err := h.db.SelectContext(r.Context(), &steps,
		`SELECT * FROM step_runs WHERE run_id = $1 ORDER BY created_at`, runID)
	if err != nil {
		http.Error(w, "failed to get report", http.StatusInternalServerError)
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
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	var steps []model.StepRun
	if err := h.db.SelectContext(r.Context(), &steps,
		`SELECT * FROM step_runs WHERE run_id=$1 ORDER BY created_at`, runID); err != nil {
		http.Error(w, "failed to get steps", http.StatusInternalServerError)
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
