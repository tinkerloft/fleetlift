package handlers

import (
	"net/http"

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

	teamID := firstTeamID(claims)
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

	writeJSON(w, http.StatusOK, runs)
}

// Get returns the report output for a specific run.
func (h *ReportsHandler) Get(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")

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

// Export exports a report as a downloadable format.
func (h *ReportsHandler) Export(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")

	var steps []model.StepRun
	err := h.db.SelectContext(r.Context(), &steps,
		`SELECT * FROM step_runs WHERE run_id = $1 ORDER BY created_at`, runID)
	if err != nil {
		http.Error(w, "failed to export report", http.StatusInternalServerError)
		return
	}

	// Export as JSON for now; could add CSV/PDF later
	w.Header().Set("Content-Disposition", "attachment; filename=report-"+runID+".json")
	writeJSON(w, http.StatusOK, map[string]any{
		"run_id": runID,
		"steps":  steps,
	})
}
