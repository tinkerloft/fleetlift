package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/template"
)

func TestStepWorkflowID(t *testing.T) {
	runID := "run-abc123"
	stepID := "analyze"
	got := stepWorkflowID(runID, stepID)
	assert.Equal(t, "run-abc123-analyze", got)
}

func TestStream_RequiresAuth(t *testing.T) {
	h := NewRunsHandler(nil, nil, nil, nil)
	r := chi.NewRouter()
	r.Get("/api/runs/{id}/events", h.Stream)

	req := httptest.NewRequest("GET", "/api/runs/run-1/events", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestStepLogs_RequiresAuth(t *testing.T) {
	h := NewRunsHandler(nil, nil, nil, nil)
	r := chi.NewRouter()
	r.Get("/api/runs/steps/{id}/logs", h.StepLogs)

	req := httptest.NewRequest("GET", "/api/runs/steps/sr-1/logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TODO: Full SSE integration tests (header verification, event delivery, terminal
// state closing) require a running test DB. Track as a future integration test task.

// stubProvider is a template.Provider that returns a fixed WorkflowTemplate by slug.
type stubProvider struct {
	tmpl *model.WorkflowTemplate
}

func (s *stubProvider) Name() string   { return "stub" }
func (s *stubProvider) Writable() bool { return false }
func (s *stubProvider) List(_ context.Context, _ string) ([]*model.WorkflowTemplate, error) {
	return []*model.WorkflowTemplate{s.tmpl}, nil
}
func (s *stubProvider) Get(_ context.Context, _, slug string) (*model.WorkflowTemplate, error) {
	if slug == s.tmpl.Slug {
		return s.tmpl, nil
	}
	return nil, template.ErrNotFound
}
func (s *stubProvider) Save(_ context.Context, _ string, _ *model.WorkflowTemplate) error {
	return nil
}
func (s *stubProvider) Delete(_ context.Context, _, _ string) error { return nil }

// circularDepYAML defines a workflow with a circular dependency (a→b, b→a),
// which ValidateWorkflow will reject with a cycle error.
const circularDepYAML = `
version: 1
id: test-workflow
steps:
  - id: a
    depends_on: [b]
    execution:
      agent: claude-code
      prompt: hello
  - id: b
    depends_on: [a]
    execution:
      agent: claude-code
      prompt: world
`

// validWorkflowYAML defines a minimal valid workflow with no circular deps,
// valid step IDs, and a single execution step.
const validWorkflowYAML = `
version: 1
id: valid-workflow
steps:
  - id: analyze
    execution:
      agent: claude-code
      prompt: analyze the code
`

func TestCreate_ValidWorkflow_PassesValidation(t *testing.T) {
	tmpl := &model.WorkflowTemplate{
		ID:       "wf-valid",
		Slug:     "valid-workflow",
		Title:    "Valid Workflow",
		YAMLBody: validWorkflowYAML,
	}
	reg := template.NewRegistry(&stubProvider{tmpl: tmpl})
	h := NewRunsHandler(nil, nil, reg, nil)

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Post("/api/runs", h.Create)

	body := `{"workflow_id":"valid-workflow","parameters":{}}`
	req := httptest.NewRequest("POST", "/api/runs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Team-ID", "team-1")

	claims := &auth.Claims{
		UserID:    "user-1",
		TeamRoles: map[string]string{"team-1": "member"},
	}
	req = req.WithContext(auth.SetClaimsInContext(req.Context(), claims))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Validation passes — the handler proceeds to DB/Temporal and panics on nil
	// DB (recovered to 500 by middleware). A 400 would mean validation incorrectly
	// rejected a valid workflow.
	assert.NotEqual(t, http.StatusBadRequest, w.Code,
		"valid workflow should pass validation and not return 400")
}

func TestCreate_ValidationError_Returns400(t *testing.T) {
	tmpl := &model.WorkflowTemplate{
		ID:       "wf-circular",
		Slug:     "test-workflow",
		Title:    "Test Circular Workflow",
		YAMLBody: circularDepYAML,
	}
	reg := template.NewRegistry(&stubProvider{tmpl: tmpl})
	h := NewRunsHandler(nil, nil, reg, nil)

	r := chi.NewRouter()
	r.Post("/api/runs", h.Create)

	body := `{"workflow_id":"test-workflow","parameters":{}}`
	req := httptest.NewRequest("POST", "/api/runs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Team-ID", "team-1")

	claims := &auth.Claims{
		UserID:    "user-1",
		TeamRoles: map[string]string{"team-1": "member"},
	}
	req = req.WithContext(auth.SetClaimsInContext(req.Context(), claims))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "workflow validation failed", resp["error"])

	valErrs, ok := resp["validation_errors"].([]any)
	assert.True(t, ok, "validation_errors should be an array")
	assert.NotEmpty(t, valErrs, "validation_errors should contain at least one error")
}

// Fix 2: malformed YAML stored in a template must return 400, not 500.
func TestCreate_InvalidYAML_Returns400(t *testing.T) {
	tmpl := &model.WorkflowTemplate{
		ID:       "wf-bad-yaml",
		Slug:     "bad-yaml-workflow",
		Title:    "Bad YAML Workflow",
		YAMLBody: "this: is: not: valid: yaml: [unclosed",
	}
	reg := template.NewRegistry(&stubProvider{tmpl: tmpl})
	h := NewRunsHandler(nil, nil, reg, nil)

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Post("/api/runs", h.Create)

	body := `{"workflow_id":"bad-yaml-workflow","parameters":{}}`
	req := httptest.NewRequest("POST", "/api/runs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Team-ID", "team-1")

	claims := &auth.Claims{
		UserID:    "user-1",
		TeamRoles: map[string]string{"team-1": "member"},
	}
	req = req.WithContext(auth.SetClaimsInContext(req.Context(), claims))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code,
		"malformed YAML in template should return 400, not 500")
}

func TestCreate_IncludesModelInRunInsert(t *testing.T) {
	tmpl := &model.WorkflowTemplate{
		ID:       "wf-valid",
		Slug:     "valid-workflow",
		Title:    "Valid Workflow",
		YAMLBody: validWorkflowYAML,
	}
	reg := template.NewRegistry(&stubProvider{tmpl: tmpl})
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	h := NewRunsHandler(sqlx.NewDb(sqlDB, "sqlmock"), nil, reg, nil)

	r := chi.NewRouter()
	r.Post("/api/runs", h.Create)

	body := `{"workflow_id":"valid-workflow","model":"gpt-5","parameters":{}}`
	req := httptest.NewRequest("POST", "/api/runs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Team-ID", "team-1")
	req = req.WithContext(auth.SetClaimsInContext(req.Context(), &auth.Claims{
		UserID:    "user-1",
		TeamRoles: map[string]string{"team-1": "member"},
	}))

	mock.ExpectExec(`INSERT INTO runs \(id, team_id, workflow_id, workflow_title, parameters, model, status, temporal_id, triggered_by\)`).
		WithArgs(sqlmock.AnyArg(), "team-1", "valid-workflow", "Valid Workflow", sqlmock.AnyArg(), "gpt-5", "pending", sqlmock.AnyArg(), "user-1").
		WillReturnError(assert.AnError)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestList_FilterCreatedByMe(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	h := NewRunsHandler(sqlx.NewDb(sqlDB, "sqlmock"), nil, nil, nil)
	r := chi.NewRouter()
	r.Get("/api/runs", h.List)

	rows := sqlmock.NewRows([]string{"id", "team_id", "workflow_id", "workflow_title", "parameters", "status", "model", "temporal_id", "triggered_by", "started_at", "completed_at", "error_message", "total_cost_usd", "created_at"}).
		AddRow("run-1", "team-1", "wf-1", "Workflow", []byte(`{}`), "pending", nil, "temporal-1", "user-1", nil, nil, nil, nil, time.Now().UTC())

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM runs WHERE team_id = $1 AND triggered_by = $2 ORDER BY created_at DESC LIMIT 50`)).
		WithArgs("team-1", "user-1").
		WillReturnRows(rows)

	req := httptest.NewRequest("GET", "/api/runs?created_by=me", nil)
	req.Header.Set("X-Team-ID", "team-1")
	req = req.WithContext(auth.SetClaimsInContext(req.Context(), &auth.Claims{
		UserID:    "user-1",
		TeamRoles: map[string]string{"team-1": "member"},
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestList_RespectsLimitParam(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	h := NewRunsHandler(sqlx.NewDb(sqlDB, "sqlmock"), nil, nil, nil)
	r := chi.NewRouter()
	r.Get("/api/runs", h.List)

	rows := sqlmock.NewRows([]string{"id", "team_id", "workflow_id", "workflow_title", "parameters", "status", "model", "temporal_id", "triggered_by", "started_at", "completed_at", "error_message", "total_cost_usd", "created_at"})
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM runs WHERE team_id = $1 ORDER BY created_at DESC LIMIT 10`)).
		WithArgs("team-1").
		WillReturnRows(rows)

	req := httptest.NewRequest("GET", "/api/runs?limit=10", nil)
	req.Header.Set("X-Team-ID", "team-1")
	req = req.WithContext(auth.SetClaimsInContext(req.Context(), &auth.Claims{
		UserID:    "user-1",
		TeamRoles: map[string]string{"team-1": "member"},
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}
