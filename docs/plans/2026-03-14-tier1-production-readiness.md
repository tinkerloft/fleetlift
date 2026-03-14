# Tier 1: Production Readiness

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development with an agent team or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix broken contracts, remove dead code, add missing tests, and close operational gaps before moving to UI polish.

**Architecture:** All changes are additive or corrective — no new features, no rearchitecting. Backend fixes align response formats with what the frontend expects. Dead knowledge-capture code is removed from worker/activities (server-side knowledge CRUD stays). A shared `shellquote` package replaces two identical copies. Test coverage is added for areas CLAUDE.md explicitly requires.

**Tech Stack:** Go, chi, PostgreSQL, Temporal SDK testsuite, testify, httptest

---

## Chunk 1: Track A — Fix What's Broken

### Task 1: Fix spaHandler panic → return error

**Files:**
- Modify: `internal/server/router.go:165-169`
- Modify: `internal/server/router.go:44` (NewRouter signature)
- Test: `internal/server/router_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/server/router_test.go`:

```go
func TestNewRouter_ReturnsErrorOnBadFS(t *testing.T) {
	// This test just verifies the function signature accepts error return.
	// The actual fs.Sub failure can't be triggered with the embedded FS,
	// but the panic removal is verified by compilation.
	registry := template.NewRegistry()
	deps := Deps{
		JWTSecret:   []byte("test-secret"),
		Auth:        handlers.NewAuthHandler(nil, nil, []byte("test-secret")),
		Workflows:   handlers.NewWorkflowsHandler(registry),
		Runs:        handlers.NewRunsHandler(nil, nil, registry, nil),
		Inbox:       handlers.NewInboxHandler(nil),
		Reports:     handlers.NewReportsHandler(nil),
		Credentials: newTestCredentialsHandler(),
		Knowledge:   handlers.NewKnowledgeHandler(nil),
	}
	router, err := NewRouter(deps)
	require.NoError(t, err)
	assert.NotNil(t, router)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/server/ -run TestNewRouter_ReturnsErrorOnBadFS -v
```

Expected: FAIL — `NewRouter` returns `http.Handler`, not `(http.Handler, error)`.

- [ ] **Step 3: Change NewRouter to return (http.Handler, error)**

In `internal/server/router.go`, change:

```go
func NewRouter(deps Deps) (http.Handler, error) {
```

Replace the `spaHandler()` call site:

```go
	spa, err := spaHandler()
	if err != nil {
		return nil, fmt.Errorf("web: %w", err)
	}
	r.Handle("/*", spa)

	return r, nil
```

Change `spaHandler`:

```go
func spaHandler() (http.Handler, error) {
	fsys, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		return nil, fmt.Errorf("failed to sub dist: %w", err)
	}
	// ... rest unchanged, return handler, nil at end
```

- [ ] **Step 4: Update all callers of NewRouter**

In `cmd/server/main.go`, update to:

```go
	handler, err := server.NewRouter(deps)
	if err != nil {
		log.Fatalf("build router: %v", err)
	}
	srv := &http.Server{
		Handler: handler,
		// ...
	}
```

Update all existing tests in `router_test.go` to:
1. Use `router, err := NewRouter(deps)` and `require.NoError(t, err)`.
2. Add `Knowledge: handlers.NewKnowledgeHandler(nil)` to every `Deps{}` literal that's missing it.

- [ ] **Step 5: Run tests**

```bash
go test ./internal/server/ -v && go build ./cmd/server/
```

Expected: all pass, builds clean.

- [ ] **Step 6: Commit**

```bash
git add internal/server/router.go internal/server/router_test.go cmd/server/main.go
git commit -m "fix: replace panic in spaHandler with returned error"
```

---

### Task 2: Add GET /health endpoint

**Files:**
- Modify: `internal/server/router.go`
- Test: `internal/server/router_test.go`

- [ ] **Step 1: Write failing test**

Add to `router_test.go`:

```go
func TestNewRouter_HealthEndpoint(t *testing.T) {
	registry := template.NewRegistry()
	deps := Deps{
		JWTSecret:   []byte("test-secret"),
		Auth:        handlers.NewAuthHandler(nil, nil, []byte("test-secret")),
		Workflows:   handlers.NewWorkflowsHandler(registry),
		Runs:        handlers.NewRunsHandler(nil, nil, registry, nil),
		Inbox:       handlers.NewInboxHandler(nil),
		Reports:     handlers.NewReportsHandler(nil),
		Credentials: newTestCredentialsHandler(),
		Knowledge:   handlers.NewKnowledgeHandler(nil),
	}
	router, err := NewRouter(deps)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, w.Body.String(), `"status"`)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/server/ -run TestNewRouter_HealthEndpoint -v
```

Expected: FAIL — returns SPA HTML fallback, not JSON.

- [ ] **Step 3: Add health endpoint**

In `router.go`, add before the authenticated group:

```go
	// Health check (no auth required)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/server/ -run TestNewRouter_HealthEndpoint -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/router.go internal/server/router_test.go
git commit -m "feat: add GET /health endpoint for load balancer probes"
```

---

### Task 3: Add writeJSONError helper and convert handlers

**Files:**
- Modify: `internal/server/handlers/helpers.go`
- Modify: `internal/server/handlers/runs.go`
- Modify: `internal/server/handlers/workflows.go`
- Modify: `internal/server/handlers/auth.go`
- Modify: `internal/server/handlers/reports.go`
- Modify: `internal/server/handlers/inbox.go`
- Modify: `internal/server/handlers/credentials.go`
- Modify: `internal/server/handlers/knowledge.go`
- Test: `internal/server/handlers/helpers_test.go` (new)

- [ ] **Step 1: Write test for writeJSONError**

Create `internal/server/handlers/helpers_test.go`:

```go
package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWriteJSONError(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSONError(w, http.StatusBadRequest, "invalid input")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.JSONEq(t, `{"error":"invalid input"}`, w.Body.String())
}

func TestWriteJSONError_InternalServer(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSONError(w, http.StatusInternalServerError, "something broke")

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.JSONEq(t, `{"error":"something broke"}`, w.Body.String())
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/server/handlers/ -run TestWriteJSONError -v
```

Expected: FAIL — `writeJSONError` undefined.

- [ ] **Step 3: Add writeJSONError to helpers.go**

```go
// writeJSONError writes a JSON error response: {"error": "message"}.
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/server/handlers/ -run TestWriteJSONError -v
```

Expected: PASS.

- [ ] **Step 5: Replace all `http.Error(...)` calls with `writeJSONError(...)` across all handler files**

Use find-and-replace in each handler file. The mapping is:

- `http.Error(w, "message", http.StatusXxx)` → `writeJSONError(w, http.StatusXxx, "message")`

Files to update: `runs.go`, `workflows.go`, `auth.go`, `reports.go`, `inbox.go`, `credentials.go`, `knowledge.go`, `helpers.go` (`teamIDFromRequest` and `getRunForTeam`).

- [ ] **Step 6: Build and test**

```bash
go build ./internal/server/... && go test ./internal/server/... -v
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/server/handlers/
git commit -m "fix: standardize error responses to JSON format"
```

---

### Task 4: Fix frontend-backend contract mismatches

**Files:**
- Modify: `web/src/api/client.ts` — fix `getRunDiff` and `getRunOutput` return types
- Modify: `web/src/api/types.ts` — add `StepDiff` and `StepOutput` types
- Modify: `internal/server/handlers/reports.go` — add artifacts handler
- Modify: `internal/server/router.go` — register artifacts route

- [ ] **Step 1: Fix getRunDiff type**

The backend returns `[{step_id, diff}]`. Update `client.ts`:

```ts
getRunDiff: (id: string) => get<{ step_id: string; diff: string }[]>(`/runs/${id}/diff`),
```

- [ ] **Step 2: Fix getRunOutput type**

The backend returns `[{step_id, output}]`. Update `client.ts`:

```ts
getRunOutput: (id: string) => get<{ step_id: string; output: Record<string, unknown> }[]>(`/runs/${id}/output`),
```

- [ ] **Step 3: Add artifacts handler to reports.go**

Add to `internal/server/handlers/reports.go`:

```go
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
		writeJSONError(w, http.StatusInternalServerError, "failed to list artifacts")
		return
	}
	if artifacts == nil {
		artifacts = []artifact{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": artifacts})
}
```

- [ ] **Step 4: Register artifacts route in router.go**

Add inside the authenticated group, after the existing reports routes:

```go
		r.Get("/api/reports/{runID}/artifacts", deps.Reports.Artifacts)
```

- [ ] **Step 5: Add test for artifacts endpoint auth**

Add to `internal/server/handlers/reports_test.go` (create if needed):

```go
package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
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
```

- [ ] **Step 6: Build frontend and backend**

```bash
go build ./... && go test ./internal/server/handlers/ -run TestArtifacts -v && cd web && npx tsc --noEmit 2>&1 | head -20
```

Expected: no errors, test passes.

- [ ] **Step 7: Commit**

```bash
git add internal/server/handlers/reports.go internal/server/handlers/reports_test.go internal/server/router.go web/src/api/client.ts
git commit -m "fix: align frontend-backend contracts for diff, output, and artifacts endpoints"
```

---

### Task 5: Consolidate duplicate shellQuote

**Files:**
- Create: `internal/shellquote/quote.go`
- Create: `internal/shellquote/quote_test.go`
- Modify: `internal/activity/util.go` — remove `shellQuote`, import shared
- Modify: `internal/activity/util_test.go` — move shellQuote tests
- Modify: `internal/activity/execute.go` — use `shellquote.Quote`
- Modify: `internal/activity/pr.go` — use `shellquote.Quote`
- Modify: `internal/agent/quote.go` — remove, replace with import
- Modify: `internal/agent/claudecode.go` — use `shellquote.Quote`
- Modify: `internal/agent/shell.go` — use `shellquote.Quote`

- [ ] **Step 1: Create shared package with test**

Create `internal/shellquote/quote.go`:

```go
// Package shellquote provides shell-safe string quoting.
package shellquote

import "strings"

// Quote wraps s in single quotes, escaping any single quotes within.
func Quote(s string) string {
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}
```

Create `internal/shellquote/quote_test.go`:

```go
package shellquote_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tinkerloft/fleetlift/internal/shellquote"
)

func TestQuote(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "'hello'"},
		{"hello world", "'hello world'"},
		{"it's", `'it'"'"'s'`},
		{"", "''"},
		{"path/to/file", "'path/to/file'"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, shellquote.Quote(tt.input), "input: %q", tt.input)
	}
}
```

- [ ] **Step 2: Run test**

```bash
go test ./internal/shellquote/ -v
```

Expected: PASS.

- [ ] **Step 3: Replace usage in activity package**

In `internal/activity/util.go`, remove the `shellQuote` function entirely.

In `internal/activity/execute.go` and `internal/activity/pr.go`, add import:

```go
"github.com/tinkerloft/fleetlift/internal/shellquote"
```

Replace all `shellQuote(` with `shellquote.Quote(`.

In `internal/activity/util_test.go`, remove shellQuote tests (they're now in the shared package).

- [ ] **Step 4: Replace usage in agent package**

Delete `internal/agent/quote.go`.

In `internal/agent/claudecode.go` and `internal/agent/shell.go`, add import:

```go
"github.com/tinkerloft/fleetlift/internal/shellquote"
```

Replace all `shellQuote(` with `shellquote.Quote(`.

- [ ] **Step 5: Build and test**

```bash
go build ./... && go test ./internal/activity/ ./internal/agent/ ./internal/shellquote/ -v
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/shellquote/ internal/activity/ internal/agent/
git commit -m "refactor: consolidate duplicate shellQuote into shared shellquote package"
```

---

### Task 6: Surface goroutine panics in DAG as step failures

**Files:**
- Modify: `internal/workflow/dag.go:344-346`

- [ ] **Step 1: Fix the nil result handling**

In `internal/workflow/dag.go`, the results collection loop (around line 344) currently skips nil results silently. Replace:

```go
		for _, r := range results {
			if r == nil {
				continue // goroutine panicked before setting result; skip
			}
```

With:

```go
		for idx, r := range results {
			if r == nil {
				// Goroutine panicked or failed to set result — surface as failure.
				stepID := readySteps[idx].ID
				r = &model.StepOutput{
					StepID: stepID,
					Status: model.StepStatusFailed,
					Error:  "step goroutine exited without producing a result",
				}
			}
```

This requires capturing `readySteps` — the sorted slice of ready steps for this iteration. Check how `ready` is used: `findReady` returns `[]model.StepDef`, and we iterate with `for i, step := range ready`. The `results` slice has the same length/indexing. So add `readySteps := ready` (or just use `ready[idx]`) in the collection loop.

The `ready` variable from the `findReady` call above is still in scope and has the same length/indexing as `results`. Replace the entire results collection block (lines 344-354) with:

```go
		// Collect results
		for idx, r := range results {
			if r == nil {
				// Goroutine panicked or failed to set result — surface as failure.
				r = &model.StepOutput{
					StepID: ready[idx].ID,
					Status: model.StepStatusFailed,
					Error:  "step goroutine exited without producing a result",
				}
			}
			outputs[r.StepID] = r
			delete(pending, r.StepID)

			if r.Status == model.StepStatusFailed && !isOptional(steps, r.StepID) {
				skipDownstream(pending, r.StepID, steps, outputs)
			}
		}
```

- [ ] **Step 2: Build and test**

```bash
go build ./internal/workflow/ && go test ./internal/workflow/ -v
```

Expected: all pass.

- [ ] **Step 3: Commit**

```bash
git add internal/workflow/dag.go
git commit -m "fix: surface nil step results as failures instead of silently skipping"
```

---

### Task 7: Remove dead knowledge-capture code from worker/activities

The worker never assigns `KnowledgeStore` to the `Activities` struct, so `CaptureKnowledge` always no-ops and knowledge enrichment in `ExecuteStep` is always skipped. Remove this dead code path. Server-side knowledge CRUD handlers are unaffected — they use `knowledge.DBStore` directly.

**Files:**
- Modify: `internal/activity/activities.go` — remove `KnowledgeStore` field and `knowledge` import
- Delete: `internal/activity/knowledge.go`
- Delete: `internal/activity/knowledge_test.go`
- Delete: `internal/activity/execute_knowledge_test.go`
- Modify: `internal/activity/execute.go` — remove enrichment block (lines 70-89) and `knowledge` import
- Modify: `internal/activity/constants.go` — remove `ActivityCaptureKnowledge` and `ActivityEnrichPrompt`
- Modify: `internal/workflow/step.go` — remove `CaptureKnowledgeActivity` var and capture block (lines 211-226)
- Modify: `internal/workflow/step_test.go` — remove `CaptureKnowledge` mock method and registration
- Modify: `internal/model/knowledge.go` — remove `CaptureKnowledgeInput` struct (keep everything else — used by server handlers)

**Keep untouched:**
- `internal/knowledge/` package — used by server handlers
- `internal/server/handlers/knowledge.go` — CRUD endpoints work
- `internal/model/knowledge.go` — `KnowledgeItem`, `KnowledgeDef`, types all stay
- `cmd/server/main.go` — knowledge store init for handlers stays

- [ ] **Step 1: Remove KnowledgeStore from Activities struct**

In `internal/activity/activities.go`, remove:

```go
	KnowledgeStore knowledge.Store
```

And remove the `"github.com/tinkerloft/fleetlift/internal/knowledge"` import.

- [ ] **Step 2: Delete knowledge.go and its tests**

```bash
rm internal/activity/knowledge.go internal/activity/knowledge_test.go internal/activity/execute_knowledge_test.go
```

- [ ] **Step 3: Remove knowledge enrichment/capture from execute.go**

In `internal/activity/execute.go`, remove lines 70-90 (the two knowledge blocks: enrichment at 70-85 and capture prompt suffix at 87-90). Remove the `"github.com/tinkerloft/fleetlift/internal/knowledge"` import.

- [ ] **Step 4: Remove knowledge constants**

In `internal/activity/constants.go`, remove:

```go
	ActivityCaptureKnowledge = "CaptureKnowledge"
	ActivityEnrichPrompt     = "EnrichPrompt"
```

And the comment `// Knowledge capture and prompt enrichment activities`.

- [ ] **Step 5: Remove CaptureKnowledge from step.go workflow**

In `internal/workflow/step.go`, remove:
- The `CaptureKnowledgeActivity` var declaration (line 68)
- The capture block (lines 211-226: `// 7. Capture knowledge if configured` through the closing `}`)

Renumber the subsequent comments (8 → 7, 9 → 8).

- [ ] **Step 6: Remove CaptureKnowledge from step_test.go mocks**

In `internal/workflow/step_test.go`:
- Remove the `CaptureKnowledge` method from `stepMockActivities`
- Remove `env.RegisterActivity(mocks.CaptureKnowledge)` from `newStepWorkflowEnv`

- [ ] **Step 7: Remove CaptureKnowledgeInput from model**

In `internal/model/knowledge.go`, remove the `CaptureKnowledgeInput` struct and its comment (lines 60-66).

- [ ] **Step 8: Build and test everything**

```bash
go build ./... && go test ./internal/activity/ ./internal/workflow/ ./internal/server/... -v
```

Expected: all pass, no compilation errors.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "refactor: remove dead knowledge-capture code path from worker/activities

The worker never assigned KnowledgeStore to Activities, so CaptureKnowledge
always no-oped and prompt enrichment was always skipped. Server-side knowledge
CRUD (handlers + DB store) is unaffected."
```

---

## Chunk 2: Track B — Test Coverage Gaps

### Task 8: Add OAuth CSRF state validation tests

CLAUDE.md explicitly requires tests for OAuth CSRF state validation in `handlers/auth.go`.

**Files:**
- Create: `internal/server/handlers/auth_test.go`

The OAuth callback handler (lines 43-57 of auth.go) validates the state parameter against a cookie. We need to test:
1. Missing state cookie → 400
2. Empty returned state → 400
3. Mismatched state → 400
4. Valid matching state proceeds (will fail on exchange, but CSRF check passes)

- [ ] **Step 1: Create auth_test.go**

```go
package handlers_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/server/handlers"
)

// mockProvider satisfies auth.Provider for testing without real GitHub OAuth.
type mockProvider struct{}

func (m *mockProvider) Name() string { return "github" }

func (m *mockProvider) AuthURL(state string) string {
	return "https://github.com/login/oauth/authorize?state=" + state
}

func (m *mockProvider) Exchange(_ context.Context, _ string) (*auth.ExternalIdentity, error) {
	return nil, fmt.Errorf("mock: exchange not implemented")
}

func newAuthRouter() http.Handler {
	h := handlers.NewAuthHandler(nil, &mockProvider{}, []byte("test-secret"))
	r := chi.NewRouter()
	r.Get("/auth/github", h.HandleGitHubRedirect)
	r.Get("/auth/github/callback", h.HandleGitHubCallback)
	return r
}

func TestOAuthCallback_MissingStateCookie(t *testing.T) {
	router := newAuthRouter()
	req := httptest.NewRequest("GET", "/auth/github/callback?state=abc&code=xyz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "missing oauth state")
}

func TestOAuthCallback_EmptyReturnedState(t *testing.T) {
	router := newAuthRouter()
	req := httptest.NewRequest("GET", "/auth/github/callback?code=xyz", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "expected-state"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid oauth state")
}

func TestOAuthCallback_MismatchedState(t *testing.T) {
	router := newAuthRouter()
	req := httptest.NewRequest("GET", "/auth/github/callback?state=wrong&code=xyz", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "expected-state"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid oauth state")
}

func TestOAuthCallback_MissingCode(t *testing.T) {
	router := newAuthRouter()
	req := httptest.NewRequest("GET", "/auth/github/callback?state=good", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "good"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "missing code")
}

func TestOAuthCallback_ValidStatePassesCSRFCheck(t *testing.T) {
	router := newAuthRouter()
	// State matches, code present — will fail on exchange (no real GitHub), but CSRF check passes.
	req := httptest.NewRequest("GET", "/auth/github/callback?state=good&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "good"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	// Should get past CSRF and fail on exchange (500), not on state validation (400).
	assert.NotEqual(t, http.StatusBadRequest, w.Code)
}

func TestOAuthRedirect_SetsStateCookie(t *testing.T) {
	router := newAuthRouter()
	req := httptest.NewRequest("GET", "/auth/github", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
	cookies := w.Result().Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == "oauth_state" {
			found = true
			assert.True(t, c.HttpOnly)
			assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
			assert.NotEmpty(t, c.Value)
		}
	}
	assert.True(t, found, "oauth_state cookie should be set")
}
```

Note: The `mockProvider` must satisfy `auth.Provider`. Check `internal/auth/provider.go` for the interface and adjust imports. The test uses `handlers_test` (external test package) so it can only access exported symbols.

- [ ] **Step 2: Run tests**

```bash
go test ./internal/server/handlers/ -run TestOAuth -v
```

Expected: all PASS. The CSRF validation tests cover the callback flow.

- [ ] **Step 3: Commit**

```bash
git add internal/server/handlers/auth_test.go
git commit -m "test: add OAuth CSRF state validation tests (CLAUDE.md requirement)"
```

---

### Task 9: Add multi-tenant isolation tests

Verify that User A from Team A cannot access Team B's runs.

**Files:**
- Create: `internal/server/handlers/isolation_test.go`

These tests use `teamIDFromRequest` + `getRunForTeam` which are already the enforcement points. We test at the handler level by sending requests with JWT claims for one team and trying to access another team's resources.

- [ ] **Step 1: Create isolation_test.go**

```go
package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tinkerloft/fleetlift/internal/auth"
)

func TestTeamIDFromRequest_RejectsCrossTeamAccess(t *testing.T) {
	claims := &auth.Claims{
		UserID:    "user-1",
		TeamRoles: map[string]string{"team-A": "member"},
	}
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Team-ID", "team-B")
	w := httptest.NewRecorder()

	teamID := teamIDFromRequest(w, req, claims)
	assert.Empty(t, teamID)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestTeamIDFromRequest_AllowsOwnTeam(t *testing.T) {
	claims := &auth.Claims{
		UserID:    "user-1",
		TeamRoles: map[string]string{"team-A": "member"},
	}
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Team-ID", "team-A")
	w := httptest.NewRecorder()

	teamID := teamIDFromRequest(w, req, claims)
	assert.Equal(t, "team-A", teamID)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTeamIDFromRequest_SingleTeamFallback(t *testing.T) {
	claims := &auth.Claims{
		UserID:    "user-1",
		TeamRoles: map[string]string{"team-only": "admin"},
	}
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	teamID := teamIDFromRequest(w, req, claims)
	assert.Equal(t, "team-only", teamID)
}

func TestTeamIDFromRequest_MultiTeamRequiresHeader(t *testing.T) {
	claims := &auth.Claims{
		UserID:    "user-1",
		TeamRoles: map[string]string{"team-A": "member", "team-B": "admin"},
	}
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	teamID := teamIDFromRequest(w, req, claims)
	assert.Empty(t, teamID)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
```

Uses `package handlers` (internal test) so unexported `teamIDFromRequest` is accessible directly. Tests call the function with crafted claims and verify the returned teamID and HTTP status codes.

- [ ] **Step 2: Run tests**

```bash
go test ./internal/server/handlers/ -run TestTeamIDFromRequest -v
```

Expected: all PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/server/handlers/isolation_test.go
git commit -m "test: add multi-tenant isolation tests for cross-team access denial"
```

---

### Task 10: Add SSE event stream auth guard tests

Test that the SSE stream endpoints require authentication. Full SSE integration tests (header verification, event delivery) require a real DB and are out of scope for this tier — documented as a TODO.

**Files:**
- Modify: `internal/server/handlers/runs_test.go`

- [ ] **Step 1: Add SSE auth tests**

Add to `internal/server/handlers/runs_test.go`:

```go
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
```

- [ ] **Step 2: Run tests**

```bash
go test ./internal/server/handlers/ -run "TestStream|TestStepLogs" -v
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/server/handlers/runs_test.go
git commit -m "test: add SSE stream auth guard tests for run events and step logs"
```

---

## Verification

After all tasks are complete:

- [ ] **Full build**

```bash
go build ./... 2>&1
```

- [ ] **Full lint**

```bash
make lint 2>&1
```

- [ ] **Full test suite**

```bash
go test ./... 2>&1
```

- [ ] **Frontend build**

```bash
cd web && npm run build 2>&1
```

All must pass with zero errors.
