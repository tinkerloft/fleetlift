# MCP Sidecar Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an MCP sidecar shim + backend API that gives sandbox agents real-time access to FleetLift platform capabilities (context, knowledge, artifacts, progress).

**Architecture:** Stateless Go binary uploaded to sandboxes at provision time. Speaks MCP protocol (SSE/HTTP + stdio) locally, proxies all tool calls to 7 new `/api/mcp/*` backend REST endpoints. Run-scoped JWT auth with separate middleware.

**Tech Stack:** Go, `github.com/mark3labs/mcp-go` (MCP SDK), `github.com/golang-jwt/jwt/v5`, chi router, PostgreSQL (existing tables), `github.com/tinkerloft/fleetlift` module.

**Spec:** `docs/superpowers/specs/2026-03-15-mcp-sidecar-design.md`

---

## Chunk 1: Foundation — Auth + Sandbox Interface + Knowledge Store

### Task 1: MCP JWT Auth (`internal/auth/mcp.go`)

**Files:**
- Create: `internal/auth/mcp.go`
- Create: `internal/auth/mcp_test.go`

- [ ] **Step 1: Write failing tests for IssueMCPToken and ValidateMCPToken**

```go
// internal/auth/mcp_test.go
package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssueMCPToken(t *testing.T) {
	secret := []byte("test-secret")
	token, err := IssueMCPToken(secret, "team-1", "run-1")
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestValidateMCPToken_Valid(t *testing.T) {
	secret := []byte("test-secret")
	token, err := IssueMCPToken(secret, "team-1", "run-1")
	require.NoError(t, err)

	claims, err := ValidateMCPToken(secret, token)
	require.NoError(t, err)
	assert.Equal(t, "team-1", claims.TeamID)
	assert.Equal(t, "run-1", claims.RunID)
}

func TestValidateMCPToken_WrongSecret(t *testing.T) {
	secret := []byte("test-secret")
	token, err := IssueMCPToken(secret, "team-1", "run-1")
	require.NoError(t, err)

	_, err = ValidateMCPToken([]byte("wrong-secret"), token)
	assert.Error(t, err)
}

func TestValidateMCPToken_WrongAudience(t *testing.T) {
	// Use a regular user token — should be rejected by ValidateMCPToken
	secret := []byte("test-secret")
	token, err := IssueToken(secret, "user-1", map[string]string{"team-1": "admin"}, false)
	require.NoError(t, err)

	_, err = ValidateMCPToken(secret, token)
	assert.Error(t, err)
}

func TestValidateMCPToken_UserTokenCannotUseMCP(t *testing.T) {
	// Ensure MCP tokens can't be used as user tokens
	secret := []byte("test-secret")
	token, err := IssueMCPToken(secret, "team-1", "run-1")
	require.NoError(t, err)

	_, err = ValidateToken(secret, token)
	assert.Error(t, err) // MCPClaims lacks UserID/TeamRoles — fails Claims parsing
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/auth/ -run TestIssueMCPToken -v`
Expected: FAIL — `IssueMCPToken` not defined

- [ ] **Step 3: Implement IssueMCPToken and ValidateMCPToken**

```go
// internal/auth/mcp.go
package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jmoiron/sqlx"
)

// MCPClaims holds the JWT payload for MCP sidecar sessions.
type MCPClaims struct {
	TeamID string `json:"team_id"`
	RunID  string `json:"run_id"`
	jwt.RegisteredClaims
}

// IssueMCPToken creates a signed JWT scoped to a specific run.
func IssueMCPToken(secret []byte, teamID, runID string) (string, error) {
	claims := MCPClaims{
		TeamID: teamID,
		RunID:  runID,
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{"mcp"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

// ValidateMCPToken parses and validates an MCP JWT, rejecting non-MCP tokens.
func ValidateMCPToken(secret []byte, tokenStr string) (*MCPClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &MCPClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return secret, nil
	}, jwt.WithAudience("mcp"))
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*MCPClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid MCP token")
	}
	return claims, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/auth/ -run TestIssueMCPToken -v && go test ./internal/auth/ -run TestValidateMCPToken -v`
Expected: PASS

- [ ] **Step 5: Write MCPAuth middleware + tests**

Add to `internal/auth/mcp.go`:

```go
// mcpClaimsKey is the context key for MCP claims (distinct from user claimsKey).
const mcpClaimsKey contextKey = "mcp_claims"

// SetMCPClaimsInContext stores MCP claims in the context (used by tests and dev bypass).
func SetMCPClaimsInContext(ctx context.Context, claims *MCPClaims) context.Context {
	return context.WithValue(ctx, mcpClaimsKey, claims)
}

// MCPAuth returns middleware that validates MCP JWT tokens and checks run liveness.
// The db is used to verify the run is not in a terminal state.
func MCPAuth(secret []byte, db *sqlx.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("Authorization")
			if !strings.HasPrefix(token, "Bearer ") {
				writeMCPError(w, http.StatusUnauthorized, "missing authorization header")
				return
			}
			claims, err := ValidateMCPToken(secret, strings.TrimPrefix(token, "Bearer "))
			if err != nil {
				writeMCPError(w, http.StatusUnauthorized, "invalid token")
				return
			}

			// Check run is still active (not terminal)
			var status string
			err = db.GetContext(r.Context(), &status,
				`SELECT status FROM runs WHERE id = $1 AND team_id = $2`,
				claims.RunID, claims.TeamID)
			if err != nil {
				writeMCPError(w, http.StatusUnauthorized, "run not found")
				return
			}
			switch status {
			case "complete", "failed", "cancelled":
				writeMCPError(w, http.StatusForbidden, "run is terminated")
				return
			}

			ctx := context.WithValue(r.Context(), mcpClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// MCPClaimsFromContext extracts MCP claims from the request context.
func MCPClaimsFromContext(ctx context.Context) *MCPClaims {
	c, _ := ctx.Value(mcpClaimsKey).(*MCPClaims)
	return c
}

func writeMCPError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":%q}`, msg)
}
```

Add to `internal/auth/mcp_test.go`:

```go
func TestMCPAuth_ValidToken(t *testing.T) {
	secret := []byte("test-secret")
	token, _ := IssueMCPToken(secret, "team-1", "run-1")

	// Create test DB with a running run
	db := setupTestDB(t) // helper that creates runs table + inserts a running run
	defer db.Close()
	insertRun(t, db, "run-1", "team-1", "running")

	handler := MCPAuth(secret, db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := MCPClaimsFromContext(r.Context())
		assert.Equal(t, "team-1", claims.TeamID)
		assert.Equal(t, "run-1", claims.RunID)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMCPAuth_TerminatedRun(t *testing.T) {
	secret := []byte("test-secret")
	token, _ := IssueMCPToken(secret, "team-1", "run-1")

	db := setupTestDB(t)
	defer db.Close()
	insertRun(t, db, "run-1", "team-1", "complete")

	handler := MCPAuth(secret, db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestMCPAuth_MissingToken(t *testing.T) {
	secret := []byte("test-secret")
	db := setupTestDB(t)
	defer db.Close()

	handler := MCPAuth(secret, db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
```

Note: `setupTestDB` and `insertRun` are test helpers. If the project already has a test DB pattern (check `internal/auth/` for existing test files), follow that pattern. Otherwise create a minimal helper using `sqlx` with an in-memory approach or mock the DB query. Given this project uses PostgreSQL, the middleware tests should mock the DB call — use an interface or extract the status check query into a function that can be swapped in tests.

- [ ] **Step 6: Run all auth tests**

Run: `go test ./internal/auth/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/auth/mcp.go internal/auth/mcp_test.go
git commit -m "feat(mcp): add MCP JWT auth — IssueMCPToken, ValidateMCPToken, MCPAuth middleware"
```

---

### Task 2: Sandbox WriteBytes (`internal/sandbox/`)

**Files:**
- Modify: `internal/sandbox/client.go:6-15`
- Modify: `internal/sandbox/opensandbox/client.go:279-323`
- Modify: `internal/sandbox/memory.go:34-37`

- [ ] **Step 1: Add WriteBytes to sandbox.Client interface**

In `internal/sandbox/client.go`, add after `WriteFile`:

```go
WriteBytes(ctx context.Context, id, path string, data []byte) error
```

- [ ] **Step 2: Implement WriteBytes in MemoryClient**

In `internal/sandbox/memory.go`, add:

```go
func (m *MemoryClient) WriteBytes(_ context.Context, _, path string, data []byte) error {
	m.files[path] = make([]byte, len(data))
	copy(m.files[path], data)
	return nil
}
```

- [ ] **Step 3: Implement WriteBytes in OpenSandbox client**

In `internal/sandbox/opensandbox/client.go`, add after the existing `WriteFile` method. Same multipart upload pattern but uses `io.Copy` from a `bytes.Reader` instead of `io.WriteString`:

```go
func (c *Client) WriteBytes(ctx context.Context, id, path string, data []byte) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	metadataJSON, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		return fmt.Errorf("opensandbox: marshal metadata: %w", err)
	}
	mw, err := w.CreateFormFile("metadata", "metadata.json")
	if err != nil {
		return fmt.Errorf("opensandbox: create metadata part: %w", err)
	}
	if _, err := mw.Write(metadataJSON); err != nil {
		return fmt.Errorf("opensandbox: write metadata: %w", err)
	}

	fw, err := w.CreateFormFile("file", "upload")
	if err != nil {
		return fmt.Errorf("opensandbox: create form file: %w", err)
	}
	if _, err := io.Copy(fw, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("opensandbox: write file content: %w", err)
	}
	w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sandboxProxyURL(id)+"/files/upload", &buf)
	if err != nil {
		return fmt.Errorf("opensandbox: write bytes request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("opensandbox: write bytes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("opensandbox: write bytes returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
```

- [ ] **Step 4: Verify build compiles**

Run: `go build ./...`
Expected: SUCCESS

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/client.go internal/sandbox/opensandbox/client.go internal/sandbox/memory.go
git commit -m "feat(sandbox): add WriteBytes method for binary file uploads"
```

---

### Task 3: Knowledge SearchByTeam (`internal/knowledge/`)

**Files:**
- Modify: `internal/knowledge/store.go:19-26`
- Modify: `internal/knowledge/store.go:152-247` (MemoryStore section)

- [ ] **Step 1: Write failing test**

Add to an appropriate test file (create `internal/knowledge/store_test.go` if none exists):

```go
package knowledge

import (
	"context"
	"testing"

	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestMemoryStore_SearchByTeam(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Seed data
	store.Save(ctx, model.KnowledgeItem{
		TeamID:     "team-1",
		Summary:    "React migration requires babel config",
		Tags:       pq.StringArray{"react", "migration"},
		Status:     model.KnowledgeStatusApproved,
		Confidence: 0.9,
	})
	store.Save(ctx, model.KnowledgeItem{
		TeamID:     "team-1",
		Summary:    "Go tests need -race flag",
		Tags:       pq.StringArray{"go", "testing"},
		Status:     model.KnowledgeStatusApproved,
		Confidence: 0.8,
	})
	store.Save(ctx, model.KnowledgeItem{
		TeamID:     "team-1",
		Summary:    "Pending item about React",
		Tags:       pq.StringArray{"react"},
		Status:     model.KnowledgeStatusPending,
		Confidence: 0.7,
	})
	store.Save(ctx, model.KnowledgeItem{
		TeamID:     "team-2",
		Summary:    "Other team React item",
		Tags:       pq.StringArray{"react"},
		Status:     model.KnowledgeStatusApproved,
		Confidence: 0.9,
	})

	t.Run("search by query", func(t *testing.T) {
		items, err := store.SearchByTeam(ctx, "team-1", "React", nil, 10)
		require.NoError(t, err)
		assert.Len(t, items, 1) // only approved items with "React" in summary
		assert.Equal(t, "React migration requires babel config", items[0].Summary)
	})

	t.Run("search by tags", func(t *testing.T) {
		items, err := store.SearchByTeam(ctx, "team-1", "", []string{"go"}, 10)
		require.NoError(t, err)
		assert.Len(t, items, 1)
		assert.Equal(t, "Go tests need -race flag", items[0].Summary)
	})

	t.Run("search with max_items", func(t *testing.T) {
		items, err := store.SearchByTeam(ctx, "team-1", "", nil, 1)
		require.NoError(t, err)
		assert.Len(t, items, 1)
	})

	t.Run("team isolation", func(t *testing.T) {
		items, err := store.SearchByTeam(ctx, "team-2", "React", nil, 10)
		require.NoError(t, err)
		assert.Len(t, items, 1)
		assert.Equal(t, "Other team React item", items[0].Summary)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/knowledge/ -run TestMemoryStore_SearchByTeam -v`
Expected: FAIL — `SearchByTeam` not defined

- [ ] **Step 3: Add SearchByTeam to Store interface**

In `internal/knowledge/store.go`, add to the `Store` interface:

```go
SearchByTeam(ctx context.Context, teamID, query string, tags []string, maxItems int) ([]model.KnowledgeItem, error)
```

- [ ] **Step 4: Implement SearchByTeam on MemoryStore**

In `internal/knowledge/store.go`, add to `MemoryStore`:

```go
func (s *MemoryStore) SearchByTeam(_ context.Context, teamID, query string, tags []string, maxItems int) ([]model.KnowledgeItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if maxItems <= 0 {
		maxItems = 10
	}
	queryLower := strings.ToLower(query)
	var out []model.KnowledgeItem
	for _, item := range s.items {
		if item.TeamID != teamID || item.Status != model.KnowledgeStatusApproved {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(item.Summary), queryLower) {
			continue
		}
		if len(tags) > 0 && !containsAll(item.Tags, tags) {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	if len(out) > maxItems {
		out = out[:maxItems]
	}
	return out, nil
}

func containsAll(haystack []string, needles []string) bool {
	set := make(map[string]bool, len(haystack))
	for _, h := range haystack {
		set[h] = true
	}
	for _, n := range needles {
		if !set[n] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 5: Implement SearchByTeam on DBStore**

In `internal/knowledge/store.go`, add to `DBStore`:

```go
func (s *DBStore) SearchByTeam(ctx context.Context, teamID, query string, tags []string, maxItems int) ([]model.KnowledgeItem, error) {
	if maxItems <= 0 {
		maxItems = 10
	}
	var items []model.KnowledgeItem
	var err error
	if query != "" && len(tags) > 0 {
		err = s.db.SelectContext(ctx, &items,
			`SELECT * FROM knowledge_items
			 WHERE team_id = $1 AND status = 'approved'
			   AND summary ILIKE '%' || $2 || '%'
			   AND tags @> $3::text[]
			 ORDER BY confidence DESC LIMIT $4`,
			teamID, query, pq.StringArray(tags), maxItems)
	} else if query != "" {
		err = s.db.SelectContext(ctx, &items,
			`SELECT * FROM knowledge_items
			 WHERE team_id = $1 AND status = 'approved'
			   AND summary ILIKE '%' || $2 || '%'
			 ORDER BY confidence DESC LIMIT $3`,
			teamID, query, maxItems)
	} else if len(tags) > 0 {
		err = s.db.SelectContext(ctx, &items,
			`SELECT * FROM knowledge_items
			 WHERE team_id = $1 AND status = 'approved'
			   AND tags @> $2::text[]
			 ORDER BY confidence DESC LIMIT $3`,
			teamID, pq.StringArray(tags), maxItems)
	} else {
		err = s.db.SelectContext(ctx, &items,
			`SELECT * FROM knowledge_items
			 WHERE team_id = $1 AND status = 'approved'
			 ORDER BY confidence DESC LIMIT $2`,
			teamID, maxItems)
	}
	return items, err
}
```

Note: add `"github.com/lib/pq"` to the imports in `store.go` if not already present.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/knowledge/ -run TestMemoryStore_SearchByTeam -v`
Expected: PASS

- [ ] **Step 7: Verify build compiles**

Run: `go build ./...`
Expected: SUCCESS

- [ ] **Step 8: Commit**

```bash
git add internal/knowledge/store.go internal/knowledge/store_test.go
git commit -m "feat(knowledge): add SearchByTeam method for cross-workflow knowledge search"
```

---

## Chunk 2: Backend MCP Handlers

### Task 4: MCP Handler — Helpers + GetRun (`internal/server/handlers/mcp.go`)

**Files:**
- Create: `internal/server/handlers/mcp.go`
- Create: `internal/server/handlers/mcp_test.go`

- [ ] **Step 1: Write failing test for HandleGetRun**

```go
// internal/server/handlers/mcp_test.go
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/auth"
)

// injectMCPClaims is a test helper that sets MCP claims in the request context.
func injectMCPClaims(r *http.Request, teamID, runID string) *http.Request {
	claims := &auth.MCPClaims{TeamID: teamID, RunID: runID}
	ctx := auth.SetMCPClaimsInContext(r.Context(), claims)
	return r.WithContext(ctx)
}

func TestHandleGetRun(t *testing.T) {
	// TODO: create MCPHandler with mock DB, call HandleGetRun,
	// verify it returns run metadata with step graph
}
```

Note: The exact test implementation depends on how the handler struct is constructed. The handler needs a `*sqlx.DB` and `knowledge.Store`. Follow the pattern used by existing handlers (e.g., `RunsHandler` uses `db *sqlx.DB`). For tests, either use a test DB or mock the queries.

- [ ] **Step 2: Create MCPHandler struct and HandleGetRun**

```go
// internal/server/handlers/mcp.go
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// MCPHandler handles MCP sidecar API endpoints.
type MCPHandler struct {
	db             *sqlx.DB
	knowledgeStore knowledge.Store
}

// NewMCPHandler creates a new MCPHandler.
func NewMCPHandler(db *sqlx.DB, knowledgeStore knowledge.Store) *MCPHandler {
	return &MCPHandler{db: db, knowledgeStore: knowledgeStore}
}

// mcpClaims extracts MCP claims from context, writing 401 if missing.
func mcpClaims(w http.ResponseWriter, r *http.Request) *auth.MCPClaims {
	claims := auth.MCPClaimsFromContext(r.Context())
	if claims == nil {
		writeMCPErr(w, http.StatusUnauthorized, "unauthorized")
		return nil
	}
	return claims
}

func writeMCPErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeMCPJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// HandleGetRun returns run metadata, parameters, and step graph.
func (h *MCPHandler) HandleGetRun(w http.ResponseWriter, r *http.Request) {
	claims := mcpClaims(w, r)
	if claims == nil {
		return
	}

	var run model.Run
	err := h.db.GetContext(r.Context(), &run,
		`SELECT * FROM runs WHERE id = $1 AND team_id = $2`,
		claims.RunID, claims.TeamID)
	if err != nil {
		slog.Error("mcp: get run", "error", err, "run_id", claims.RunID)
		writeMCPErr(w, http.StatusNotFound, "run not found")
		return
	}

	var steps []model.StepRun
	err = h.db.SelectContext(r.Context(), &steps,
		`SELECT * FROM step_runs WHERE run_id = $1 ORDER BY created_at`,
		claims.RunID)
	if err != nil {
		slog.Error("mcp: get steps", "error", err, "run_id", claims.RunID)
		writeMCPErr(w, http.StatusInternalServerError, "failed to load steps")
		return
	}

	type stepSummary struct {
		ID     string `json:"id"`
		StepID string `json:"step_id"`
		Status string `json:"status"`
	}
	summaries := make([]stepSummary, len(steps))
	for i, s := range steps {
		summaries[i] = stepSummary{ID: s.ID, StepID: s.StepID, Status: string(s.Status)}
	}

	// Find current running step
	currentStep := ""
	for _, s := range steps {
		if s.Status == model.StepStatusRunning {
			currentStep = s.StepID
			break
		}
	}

	writeMCPJSON(w, http.StatusOK, map[string]any{
		"run_id":       run.ID,
		"workflow":     run.WorkflowTitle,
		"parameters":   run.Parameters,
		"status":       run.Status,
		"current_step": currentStep,
		"steps":        summaries,
	})
}
```

- [ ] **Step 3: Run test**

Run: `go test ./internal/server/handlers/ -run TestHandleGetRun -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/server/handlers/mcp.go internal/server/handlers/mcp_test.go
git commit -m "feat(mcp): add MCPHandler with HandleGetRun endpoint"
```

---

### Task 5: MCP Handlers — GetStepOutput + GetKnowledge

**Files:**
- Modify: `internal/server/handlers/mcp.go`
- Modify: `internal/server/handlers/mcp_test.go`

- [ ] **Step 1: Write tests for HandleGetStepOutput and HandleGetKnowledge**

Add to `mcp_test.go`. Follow the same mock DB / test pattern established in Task 4.

- [ ] **Step 2: Implement HandleGetStepOutput**

```go
// HandleGetStepOutput returns the output + diff of a completed upstream step.
func (h *MCPHandler) HandleGetStepOutput(w http.ResponseWriter, r *http.Request) {
	claims := mcpClaims(w, r)
	if claims == nil {
		return
	}
	stepID := chi.URLParam(r, "stepID")
	if stepID == "" {
		writeMCPErr(w, http.StatusBadRequest, "step_id required")
		return
	}

	var step model.StepRun
	err := h.db.GetContext(r.Context(), &step,
		`SELECT * FROM step_runs WHERE step_id = $1 AND run_id = $2`,
		stepID, claims.RunID)
	if err != nil {
		writeMCPErr(w, http.StatusNotFound, "step not found in this run")
		return
	}

	resp := map[string]any{"output": step.Output}
	if step.Diff != nil {
		resp["diff"] = *step.Diff
	}
	writeMCPJSON(w, http.StatusOK, resp)
}
```

- [ ] **Step 3: Implement HandleGetKnowledge**

```go
// HandleGetKnowledge returns approved knowledge items for the current workflow template.
func (h *MCPHandler) HandleGetKnowledge(w http.ResponseWriter, r *http.Request) {
	claims := mcpClaims(w, r)
	if claims == nil {
		return
	}

	query := r.URL.Query().Get("q")
	maxItems := 10
	if m := r.URL.Query().Get("max"); m != "" {
		if v, err := strconv.Atoi(m); err == nil && v > 0 {
			maxItems = v
		}
	}

	// Resolve workflow_template_id from the run
	var workflowID string
	err := h.db.GetContext(r.Context(), &workflowID,
		`SELECT workflow_id FROM runs WHERE id = $1 AND team_id = $2`,
		claims.RunID, claims.TeamID)
	if err != nil {
		slog.Error("mcp: resolve workflow_id", "error", err, "run_id", claims.RunID)
		writeMCPErr(w, http.StatusInternalServerError, "failed to resolve workflow")
		return
	}

	items, err := h.knowledgeStore.ListApprovedByWorkflow(r.Context(), claims.TeamID, workflowID, maxItems)
	if err != nil {
		slog.Error("mcp: list knowledge", "error", err)
		writeMCPErr(w, http.StatusInternalServerError, "failed to list knowledge")
		return
	}

	// If a query was provided, filter client-side (ListApprovedByWorkflow doesn't support text search)
	if query != "" {
		filtered := items[:0]
		queryLower := strings.ToLower(query)
		for _, item := range items {
			if strings.Contains(strings.ToLower(item.Summary), queryLower) ||
				strings.Contains(strings.ToLower(item.Details), queryLower) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	writeMCPJSON(w, http.StatusOK, map[string]any{"items": items})
}
```

Add `"strings"` to imports.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/server/handlers/ -run "TestHandleGetStepOutput|TestHandleGetKnowledge" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/handlers/mcp.go internal/server/handlers/mcp_test.go
git commit -m "feat(mcp): add GetStepOutput + GetKnowledge handlers"
```

---

### Task 6: MCP Handlers — Write Tools (artifact, knowledge, progress)

**Files:**
- Modify: `internal/server/handlers/mcp.go`
- Modify: `internal/server/handlers/mcp_test.go`

- [ ] **Step 1: Write tests for HandleCreateArtifact, HandleAddLearning, HandleSearchKnowledge, HandleUpdateProgress**

Add to `mcp_test.go`. Each test should verify:
- Correct insertion into DB/store
- Team scoping from claims (not request body)
- Input validation (e.g., content size limit for artifacts)
- Error responses for invalid input

- [ ] **Step 2: Implement HandleCreateArtifact**

```go
// HandleCreateArtifact creates a new artifact for the current step.
func (h *MCPHandler) HandleCreateArtifact(w http.ResponseWriter, r *http.Request) {
	claims := mcpClaims(w, r)
	if claims == nil {
		return
	}

	var req struct {
		Name        string `json:"name"`
		Content     string `json:"content"`
		ContentType string `json:"content_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMCPErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeMCPErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.Content) > 1024*1024 {
		writeMCPErr(w, http.StatusRequestEntityTooLarge, "content exceeds 1 MB limit")
		return
	}
	if req.ContentType == "" {
		req.ContentType = "text/plain"
	}

	// Resolve current step_run_id
	stepRunID, err := h.activeStepRunID(r.Context(), claims.RunID)
	if err != nil {
		writeMCPErr(w, http.StatusInternalServerError, "no active step found")
		return
	}

	id := uuid.New().String()
	data := []byte(req.Content)
	_, err = h.db.ExecContext(r.Context(),
		`INSERT INTO artifacts (id, step_run_id, name, path, size_bytes, content_type, storage, data, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, 'inline', $7, NOW())`,
		id, stepRunID, req.Name, "mcp://"+req.Name, len(data), req.ContentType, data)
	if err != nil {
		slog.Error("mcp: create artifact", "error", err)
		writeMCPErr(w, http.StatusInternalServerError, "failed to create artifact")
		return
	}

	writeMCPJSON(w, http.StatusCreated, map[string]string{"artifact_id": id})
}

// activeStepRunID finds the running step for a given run.
func (h *MCPHandler) activeStepRunID(ctx context.Context, runID string) (string, error) {
	var id string
	err := h.db.GetContext(ctx, &id,
		`SELECT id FROM step_runs WHERE run_id = $1 AND status = 'running' LIMIT 1`,
		runID)
	return id, err
}
```

- [ ] **Step 3: Implement HandleAddLearning**

```go
// HandleAddLearning adds a knowledge item from the agent.
func (h *MCPHandler) HandleAddLearning(w http.ResponseWriter, r *http.Request) {
	claims := mcpClaims(w, r)
	if claims == nil {
		return
	}

	var req struct {
		Type       string   `json:"type"`
		Summary    string   `json:"summary"`
		Details    string   `json:"details"`
		Confidence float64  `json:"confidence"`
		Tags       []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMCPErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Summary == "" {
		writeMCPErr(w, http.StatusBadRequest, "summary is required")
		return
	}

	knowledgeType := model.KnowledgeType(req.Type)
	switch knowledgeType {
	case model.KnowledgeTypePattern, model.KnowledgeTypeCorrection,
		model.KnowledgeTypeGotcha, model.KnowledgeTypeContext:
		// valid
	default:
		writeMCPErr(w, http.StatusBadRequest, "type must be pattern, correction, gotcha, or context")
		return
	}

	stepRunID, _ := h.activeStepRunID(r.Context(), claims.RunID)

	// Resolve workflow_template_id for scoping
	var workflowID string
	_ = h.db.GetContext(r.Context(), &workflowID,
		`SELECT workflow_id FROM runs WHERE id = $1`, claims.RunID)

	item := model.KnowledgeItem{
		TeamID:             claims.TeamID,
		WorkflowTemplateID: workflowID,
		StepRunID:          stepRunID,
		Type:               knowledgeType,
		Summary:            req.Summary,
		Details:            req.Details,
		Source:             model.KnowledgeSourceAutoCaptured,
		Tags:               req.Tags,
		Confidence:         req.Confidence,
	}

	saved, err := h.knowledgeStore.Save(r.Context(), item)
	if err != nil {
		slog.Error("mcp: add learning", "error", err)
		writeMCPErr(w, http.StatusInternalServerError, "failed to save knowledge item")
		return
	}

	writeMCPJSON(w, http.StatusCreated, map[string]any{
		"id":     saved.ID,
		"status": saved.Status,
	})
}
```

- [ ] **Step 4: Implement HandleSearchKnowledge**

```go
// HandleSearchKnowledge searches approved knowledge across the team.
func (h *MCPHandler) HandleSearchKnowledge(w http.ResponseWriter, r *http.Request) {
	claims := mcpClaims(w, r)
	if claims == nil {
		return
	}

	query := r.URL.Query().Get("q")
	maxItems := 10
	if m := r.URL.Query().Get("max"); m != "" {
		if v, err := strconv.Atoi(m); err == nil && v > 0 {
			maxItems = v
		}
	}

	var tags []string
	if t := r.URL.Query().Get("tags"); t != "" {
		tags = strings.Split(t, ",")
	}

	items, err := h.knowledgeStore.SearchByTeam(r.Context(), claims.TeamID, query, tags, maxItems)
	if err != nil {
		slog.Error("mcp: search knowledge", "error", err)
		writeMCPErr(w, http.StatusInternalServerError, "failed to search knowledge")
		return
	}

	writeMCPJSON(w, http.StatusOK, map[string]any{"items": items})
}
```

- [ ] **Step 5: Implement HandleUpdateProgress**

```go
// HandleUpdateProgress updates the progress sub-key in step output.
func (h *MCPHandler) HandleUpdateProgress(w http.ResponseWriter, r *http.Request) {
	claims := mcpClaims(w, r)
	if claims == nil {
		return
	}

	var req struct {
		Percentage int    `json:"percentage"`
		Message    string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMCPErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	stepRunID, err := h.activeStepRunID(r.Context(), claims.RunID)
	if err != nil {
		writeMCPErr(w, http.StatusInternalServerError, "no active step found")
		return
	}

	progressJSON, _ := json.Marshal(map[string]any{
		"percentage": req.Percentage,
		"message":    req.Message,
	})

	_, err = h.db.ExecContext(r.Context(),
		`UPDATE step_runs SET output = jsonb_set(COALESCE(output, '{}'), '{progress}', $1::jsonb)
		 WHERE id = $2`,
		string(progressJSON), stepRunID)
	if err != nil {
		slog.Error("mcp: update progress", "error", err)
		writeMCPErr(w, http.StatusInternalServerError, "failed to update progress")
		return
	}

	writeMCPJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/server/handlers/ -run "TestHandleCreateArtifact|TestHandleAddLearning|TestHandleSearchKnowledge|TestHandleUpdateProgress" -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/server/handlers/mcp.go internal/server/handlers/mcp_test.go
git commit -m "feat(mcp): add write handlers — artifact.create, memory.add_learning, memory.search, progress.update"
```

---

### Task 7: Wire MCP Routes into Router

**Files:**
- Modify: `internal/server/router.go:20-31` (Deps struct)
- Modify: `internal/server/router.go:45-133` (NewRouter function)
- Modify: `cmd/server/main.go:85-95` (Deps construction)

- [ ] **Step 1: Add MCPHandler to Deps**

In `internal/server/router.go`, add to `Deps` struct:

```go
MCP *handlers.MCPHandler
```

- [ ] **Step 2: Register MCP routes**

In `NewRouter`, add after the authenticated API group (before `// Serve embedded React SPA`):

```go
// MCP API (separate auth — run-scoped JWT, not user JWT)
r.Route("/api/mcp", func(r chi.Router) {
	r.Use(auth.MCPAuth(deps.JWTSecret, deps.MCP.DB()))
	r.Get("/run", deps.MCP.HandleGetRun)
	r.Get("/steps/{stepID}/output", deps.MCP.HandleGetStepOutput)
	r.Get("/knowledge", deps.MCP.HandleGetKnowledge)
	r.Post("/artifacts", deps.MCP.HandleCreateArtifact)
	r.Post("/knowledge", deps.MCP.HandleAddLearning)
	r.Get("/knowledge/search", deps.MCP.HandleSearchKnowledge)
	r.Post("/progress", deps.MCP.HandleUpdateProgress)
})
```

Note: Add a `DB() *sqlx.DB` method to `MCPHandler` to expose the DB for the middleware, or pass the DB separately in `Deps`. The cleaner approach is to pass `db` directly:

```go
r.Route("/api/mcp", func(r chi.Router) {
	r.Use(auth.MCPAuth(deps.JWTSecret, deps.DB))
	// ... handlers
})
```

This requires adding `DB *sqlx.DB` to the `Deps` struct. Review which approach fits better with existing patterns.

- [ ] **Step 3: Wire MCPHandler in cmd/server/main.go**

After `knowledgeStore` initialization, add:

```go
mcpHandler := handlers.NewMCPHandler(database, knowledgeStore)
```

Add to `deps`:

```go
MCP: mcpHandler,
```

- [ ] **Step 4: Verify build compiles**

Run: `go build ./...`
Expected: SUCCESS

- [ ] **Step 5: Commit**

```bash
git add internal/server/router.go cmd/server/main.go
git commit -m "feat(mcp): wire MCP routes into server router"
```

---

## Chunk 3: MCP Shim Binary

### Task 8: Add mcp-go dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add dependency**

Run: `go get github.com/mark3labs/mcp-go@latest`

- [ ] **Step 2: Tidy**

Run: `go mod tidy`

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add mcp-go SDK"
```

---

### Task 9: MCP Shim Binary (`cmd/mcp-sidecar/`)

**Files:**
- Create: `cmd/mcp-sidecar/main.go`
- Create: `cmd/mcp-sidecar/main_test.go`

- [ ] **Step 1: Write failing test for shim HTTP proxy**

```go
// cmd/mcp-sidecar/main_test.go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShim_CallProxiesToBackend(t *testing.T) {
	// Mock backend that returns a canned run response
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/mcp/run", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"run_id":   "run-1",
			"workflow": "bug-fix",
		})
	}))
	defer backend.Close()

	shim := &Shim{
		apiURL:     backend.URL,
		token:      "test-token",
		httpClient: http.DefaultClient,
	}

	result, err := shim.call("GET", "/api/mcp/run", nil)
	require.NoError(t, err)
	assert.Equal(t, "run-1", result["run_id"])
	assert.Equal(t, "bug-fix", result["workflow"])
}

func TestShim_CallForwardsErrorResponse(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "run is terminated"})
	}))
	defer backend.Close()

	shim := &Shim{
		apiURL:     backend.URL,
		token:      "test-token",
		httpClient: http.DefaultClient,
	}

	_, err := shim.call("GET", "/api/mcp/run", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "run is terminated")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/mcp-sidecar/ -run TestShim -v`
Expected: FAIL — `Shim` not defined

- [ ] **Step 3: Implement the shim**

```go
// cmd/mcp-sidecar/main.go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Shim proxies MCP tool calls to the FleetLift backend API.
type Shim struct {
	apiURL     string
	token      string
	httpClient *http.Client
}

func main() {
	apiURL := flag.String("api-url", os.Getenv("FLEETLIFT_API_URL"), "FleetLift backend URL")
	token := flag.String("token", os.Getenv("FLEETLIFT_MCP_TOKEN"), "MCP JWT token")
	port := flag.Int("port", 8081, "SSE/HTTP listen port")
	transport := flag.String("transport", "sse", "Transport: sse or stdio")
	flag.Parse()

	if *apiURL == "" || *token == "" {
		log.Fatal("--api-url and --token are required")
	}

	shim := &Shim{
		apiURL:     *apiURL,
		token:      *token,
		httpClient: &http.Client{},
	}

	s := server.NewMCPServer("FleetLift MCP", "1.0.0")
	shim.registerTools(s)

	switch *transport {
	case "stdio":
		if err := server.ServeStdio(s); err != nil {
			log.Fatalf("stdio server error: %v", err)
		}
	default: // sse
		sseServer := server.NewSSEServer(s, server.WithBaseURL(fmt.Sprintf("http://localhost:%d", *port)))

		// Health endpoint for provisioning health check
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		})
		mux.Handle("/", sseServer)

		log.Printf("MCP SSE server listening on :%d", *port)
		if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), mux); err != nil {
			log.Fatalf("SSE server error: %v", err)
		}
	}
}

func (s *Shim) registerTools(srv *server.MCPServer) {
	// E1: Read-only context
	srv.AddTool(mcp.NewTool("context.get_run",
		mcp.WithDescription("Get metadata about the current run including workflow, parameters, and step graph"),
	), s.handleGetRun)

	srv.AddTool(mcp.NewTool("context.get_step_output",
		mcp.WithDescription("Get the output and diff of a completed upstream step"),
		mcp.WithString("step_id", mcp.Required(), mcp.Description("ID of the step to get output from")),
	), s.handleGetStepOutput)

	srv.AddTool(mcp.NewTool("context.get_knowledge",
		mcp.WithDescription("Search approved knowledge items for the current workflow"),
		mcp.WithString("query", mcp.Description("Search query")),
		mcp.WithNumber("max_items", mcp.Description("Maximum items to return (default 10)")),
	), s.handleGetKnowledge)

	// E2: Write tools
	srv.AddTool(mcp.NewTool("artifact.create",
		mcp.WithDescription("Create a named artifact from content (max 1 MB)"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Artifact name")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Artifact content")),
		mcp.WithString("content_type", mcp.Description("Content type (default text/plain)")),
	), s.handleCreateArtifact)

	srv.AddTool(mcp.NewTool("memory.add_learning",
		mcp.WithDescription("Record a knowledge item discovered during execution"),
		mcp.WithString("type", mcp.Required(), mcp.Description("Type: pattern, correction, gotcha, or context")),
		mcp.WithString("summary", mcp.Required(), mcp.Description("Brief summary of the learning")),
		mcp.WithString("details", mcp.Description("Detailed explanation")),
		mcp.WithNumber("confidence", mcp.Description("Confidence score 0.0-1.0")),
	), s.handleAddLearning)

	srv.AddTool(mcp.NewTool("memory.search",
		mcp.WithDescription("Search approved knowledge across the team (cross-workflow)"),
		mcp.WithString("query", mcp.Description("Search query")),
		mcp.WithString("tags", mcp.Description("Comma-separated tags to filter by")),
		mcp.WithNumber("max_items", mcp.Description("Maximum items to return (default 10)")),
	), s.handleSearchKnowledge)

	srv.AddTool(mcp.NewTool("progress.update",
		mcp.WithDescription("Report step progress to the UI"),
		mcp.WithNumber("percentage", mcp.Required(), mcp.Description("Progress percentage 0-100")),
		mcp.WithString("message", mcp.Description("Progress message")),
	), s.handleUpdateProgress)
}

// Tool handlers — each proxies to a backend endpoint

func (s *Shim) handleGetRun(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resp, err := s.call("GET", "/api/mcp/run", nil)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(resp)
}

func (s *Shim) handleGetStepOutput(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	stepID, _ := req.Params.Arguments["step_id"].(string)
	resp, err := s.call("GET", "/api/mcp/steps/"+stepID+"/output", nil)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(resp)
}

func (s *Shim) handleGetKnowledge(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, _ := req.Params.Arguments["query"].(string)
	path := "/api/mcp/knowledge?q=" + query
	if max, ok := req.Params.Arguments["max_items"].(float64); ok {
		path += fmt.Sprintf("&max=%d", int(max))
	}
	resp, err := s.call("GET", path, nil)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(resp)
}

func (s *Shim) handleCreateArtifact(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resp, err := s.call("POST", "/api/mcp/artifacts", req.Params.Arguments)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(resp)
}

func (s *Shim) handleAddLearning(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resp, err := s.call("POST", "/api/mcp/knowledge", req.Params.Arguments)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(resp)
}

func (s *Shim) handleSearchKnowledge(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, _ := req.Params.Arguments["query"].(string)
	tags, _ := req.Params.Arguments["tags"].(string)
	path := "/api/mcp/knowledge/search?q=" + query
	if tags != "" {
		path += "&tags=" + tags
	}
	if max, ok := req.Params.Arguments["max_items"].(float64); ok {
		path += fmt.Sprintf("&max=%d", int(max))
	}
	resp, err := s.call("GET", path, nil)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(resp)
}

func (s *Shim) handleUpdateProgress(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resp, err := s.call("POST", "/api/mcp/progress", req.Params.Arguments)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(resp)
}

// call makes an HTTP request to the backend and returns the JSON response.
func (s *Shim) call(method, path string, body any) (map[string]any, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, s.apiURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("backend request: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if resp.StatusCode >= 400 {
		errMsg, _ := result["error"].(string)
		if errMsg == "" {
			errMsg = fmt.Sprintf("backend returned %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	return result, nil
}

func jsonResult(data map[string]any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(b)), nil
}
```

Note: The exact `mcp-go` API may differ slightly — check the `github.com/mark3labs/mcp-go` package docs for the current `AddTool`, `NewTool`, `CallToolRequest`, and `CallToolResult` APIs. Adjust types and method signatures accordingly.

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/mcp-sidecar/ -v`
Expected: PASS

- [ ] **Step 5: Verify binary builds**

Run: `go build -o /dev/null ./cmd/mcp-sidecar/`
Expected: SUCCESS

- [ ] **Step 6: Commit**

```bash
git add cmd/mcp-sidecar/
git commit -m "feat(mcp): add MCP sidecar shim binary with 7 tool handlers"
```

---

### Task 10: Makefile target

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Add mcp-sidecar build target**

Add to Makefile after the `fleetlift:` target:

```makefile
# Build MCP sidecar (for sandbox upload)
mcp-sidecar:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/fleetlift-mcp ./cmd/mcp-sidecar
```

Update the `build:` target to include `mcp-sidecar`:

```makefile
build: build-web
	go build -o bin/fleetlift-worker ./cmd/worker
	go build -o bin/fleetlift ./cmd/cli
	go build -o bin/fleetlift-server ./cmd/server
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/fleetlift-mcp ./cmd/mcp-sidecar
```

Also add `mcp-sidecar` to `.PHONY`.

- [ ] **Step 2: Verify build**

Run: `make mcp-sidecar`
Expected: `bin/fleetlift-mcp` created

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "build: add mcp-sidecar Makefile target"
```

---

## Chunk 4: Provisioning Integration + Agent Runner

### Task 11: ProvisionSandbox MCP Setup (`internal/activity/provision.go`)

**Files:**
- Modify: `internal/activity/provision.go:22-82`
- Modify: `internal/activity/provision_test.go` (or create if not exists)

- [ ] **Step 1: Write failing test for MCP provisioning**

```go
func TestProvisionSandbox_MCPSetup(t *testing.T) {
	// Use MemoryClient to verify:
	// 1. Binary uploaded to correct path
	// 2. chmod +x executed
	// 3. Shim started with correct args
	// 4. Env vars set (FLEETLIFT_MCP_TOKEN, FLEETLIFT_MCP_PORT)
}

func TestProvisionSandbox_MCPSkippedWhenBinaryMissing(t *testing.T) {
	// Verify provisioning succeeds without MCP when FLEETLIFT_MCP_BINARY_PATH is unset
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/activity/ -run TestProvisionSandbox_MCP -v`
Expected: FAIL

- [ ] **Step 3: Add MCP setup to ProvisionSandbox**

In `internal/activity/provision.go`, add after the `mkdir -p /workspace` block:

```go
// MCP sidecar setup (optional — skip if binary not available)
if mcpBinaryPath := os.Getenv("FLEETLIFT_MCP_BINARY_PATH"); mcpBinaryPath != "" {
	if mcpData, err := os.ReadFile(mcpBinaryPath); err == nil {
		jwtSecret := []byte(os.Getenv("JWT_SECRET"))
		mcpToken, err := auth.IssueMCPToken(jwtSecret, input.TeamID, input.RunID)
		if err != nil {
			_ = a.Sandbox.Kill(ctx, sandboxID)
			return "", fmt.Errorf("issue MCP token: %w", err)
		}

		// Upload and start shim
		if err := a.Sandbox.WriteBytes(ctx, sandboxID, "/usr/local/bin/fleetlift-mcp", mcpData); err != nil {
			_ = a.Sandbox.Kill(ctx, sandboxID)
			return "", fmt.Errorf("upload MCP binary: %w", err)
		}
		if _, _, err := a.Sandbox.Exec(ctx, sandboxID, "chmod +x /usr/local/bin/fleetlift-mcp", "/"); err != nil {
			_ = a.Sandbox.Kill(ctx, sandboxID)
			return "", fmt.Errorf("chmod MCP binary: %w", err)
		}

		apiURL := os.Getenv("FLEETLIFT_API_URL")
		if apiURL == "" {
			apiURL = "http://host.docker.internal:8080"
		}
		mcpPort := "8081"
		startCmd := fmt.Sprintf(
			"FLEETLIFT_MCP_TOKEN=%s FLEETLIFT_MCP_PORT=%s nohup /usr/local/bin/fleetlift-mcp --api-url %s --token %s --port %s > /tmp/fleetlift-mcp.log 2>&1 &",
			shellquote.Quote(mcpToken), mcpPort,
			shellquote.Quote(apiURL), shellquote.Quote(mcpToken), mcpPort,
		)
		if _, _, err := a.Sandbox.Exec(ctx, sandboxID, startCmd, "/"); err != nil {
			// Non-fatal — log and continue without MCP
			slog.Warn("failed to start MCP sidecar", "error", err, "sandbox_id", sandboxID)
		} else {
			// Health check — retry for up to 5 seconds
			healthy := false
			for i := 0; i < 10; i++ {
				stdout, _, err := a.Sandbox.Exec(ctx, sandboxID,
					"curl -sf http://localhost:"+mcpPort+"/health", "/")
				if err == nil && strings.Contains(stdout, "ok") {
					healthy = true
					break
				}
				time.Sleep(500 * time.Millisecond)
			}
			if !healthy {
				slog.Warn("MCP sidecar health check failed after 5s", "sandbox_id", sandboxID)
			}
		}
	}
}
```

Add required imports: `"log/slog"`, `"strings"`, `"time"`, `"github.com/tinkerloft/fleetlift/internal/auth"`, `"github.com/tinkerloft/fleetlift/internal/shellquote"`.

Note: The health check uses `curl -sf` with `|| true` to avoid `ExecStream` error returns. The shim needs a `/health` endpoint — add a simple `http.HandleFunc("/health", ...)` in the shim's SSE mode that returns 200. This should be added back in Task 9 if not already there.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/activity/ -run TestProvisionSandbox_MCP -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/activity/provision.go internal/activity/provision_test.go
git commit -m "feat(mcp): add MCP sidecar upload + start to ProvisionSandbox"
```

---

### Task 12: ClaudeCodeRunner MCP Config (`internal/agent/claudecode.go`)

**Files:**
- Modify: `internal/agent/claudecode.go:37-39`

- [ ] **Step 1: Add MCP config to Claude CLI command**

In `ClaudeCodeRunner.Run()`, modify the command construction to include MCP server config when the env vars are present. The Claude Code CLI accepts `--mcp-config` with a JSON file path or inline JSON:

```go
func (r *ClaudeCodeRunner) Run(ctx context.Context, sandboxID string, opts RunOpts) (<-chan Event, error) {
	cmd := fmt.Sprintf("cd %s && claude -p %s --output-format stream-json --verbose --dangerously-skip-permissions --max-turns %d",
		shellquote.Quote(opts.WorkDir), shellquote.Quote(opts.Prompt), max(opts.MaxTurns, 20))

	// Add MCP sidecar config if available in sandbox env
	// The shim runs on localhost:$FLEETLIFT_MCP_PORT, started by ProvisionSandbox
	cmd += ` && if [ -n "$FLEETLIFT_MCP_PORT" ]; then export CLAUDE_MCP_SERVERS='{"fleetlift":{"type":"sse","url":"http://localhost:'$FLEETLIFT_MCP_PORT'/sse"}}'; fi`

	// Reconstruct with MCP awareness
	cmd = fmt.Sprintf(`sh -c 'if [ -n "$FLEETLIFT_MCP_PORT" ]; then MCP_FLAG="--mcp-config <(echo '"'"'{\"mcpServers\":{\"fleetlift\":{\"type\":\"sse\",\"url\":\"http://localhost:'"'"'$FLEETLIFT_MCP_PORT'"'"'/sse\"}}}'"'"')"; fi; cd %s && claude -p %s --output-format stream-json --verbose --dangerously-skip-permissions --max-turns %d $MCP_FLAG'`,
		shellquote.Quote(opts.WorkDir), shellquote.Quote(opts.Prompt), max(opts.MaxTurns, 20))

	// ... rest unchanged
```

Note: The exact Claude Code CLI flag for SSE MCP servers needs verification. Check Claude Code docs for the current `--mcp-config` format. The implementation above is a sketch — the actual flag may use a config file written to disk instead of process substitution. A simpler approach: write a `.mcp.json` file to the workspace before launching claude:

```go
// Simpler approach: write MCP config file before launching
mcpConfig := `{"mcpServers":{"fleetlift":{"type":"sse","url":"http://localhost:8081/sse"}}}`
// This would be done in ProvisionSandbox after starting the shim:
// sandbox.WriteFile(ctx, sandboxID, "/workspace/.mcp.json", mcpConfig)
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: SUCCESS

- [ ] **Step 3: Commit**

```bash
git add internal/agent/claudecode.go
git commit -m "feat(mcp): add MCP server config to ClaudeCodeRunner"
```

---

## Chunk 5: Verification

### Task 13: Full Build + Test Verification

- [ ] **Step 1: Run linter**

Run: `make lint`
Expected: PASS with no errors

- [ ] **Step 2: Run all tests**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 3: Build all binaries**

Run: `make build`
Expected: SUCCESS — produces `bin/fleetlift-worker`, `bin/fleetlift`, `bin/fleetlift-server`, `bin/fleetlift-mcp`

- [ ] **Step 4: Verify MCP sidecar binary is static**

Run: `file bin/fleetlift-mcp`
Expected: `ELF 64-bit LSB executable, x86-64, ... statically linked`

---

### Task 14: Update Roadmap

**Files:**
- Modify: `docs/plans/ROADMAP.md`

- [ ] **Step 1: Mark E1 and E2 as complete in the roadmap**

Update Track E table:

```markdown
| E1 | Read-only context tools | `context.get_run`, `context.get_step_output`, `context.get_knowledge` | ✅ |
| E2 | Write tools | `artifact.create`, `memory.add_learning`, `memory.search`, `progress.update` | ✅ |
```

- [ ] **Step 2: Commit**

```bash
git add docs/plans/ROADMAP.md
git commit -m "docs: mark Track E phases E1+E2 as complete"
```
