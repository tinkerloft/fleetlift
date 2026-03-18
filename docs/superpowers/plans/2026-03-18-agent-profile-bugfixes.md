# Agent Profile Bugfixes Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 8 bugs found in code review of the agent profiles feature (PR #44).

**Architecture:** All fixes are surgical edits to existing files — no new files needed. Tasks are ordered by dependency: handler fixes first (independent), then activity fixes (build on each other), then the MCP credential injection (most complex, depends on understanding existing flow).

**Tech Stack:** Go, PostgreSQL, Temporal SDK, sqlx

---

## Files Modified

- `internal/server/handlers/profiles.go` — Tasks 1, 2
- `internal/activity/preflight.go` — Tasks 3, 4, 5, 6
- `internal/activity/profiles.go` — Task 6
- `internal/server/handlers/profiles_test.go` — Tasks 1, 2
- `internal/activity/preflight_test.go` — Tasks 3, 4, 5, 6, 7
- `internal/model/agent_profile.go` — Task 8

---

## Bug Reference

| # | Score | File | Issue |
|---|-------|------|-------|
| 1 | 100 | `handlers/profiles.go` | `DeleteProfile`/`DeleteMarketplace` return 204 even when 0 rows deleted |
| 2 | 85 | `handlers/profiles.go` | `GetProfile` returns 404 for any DB error, not just `sql.ErrNoRows` |
| 3 | 85 | `activity/preflight.go` | Marketplace DB errors silently swallowed (`if err == nil` pattern) |
| 4 | 85 | `activity/profiles.go` | "profile not found" is a permanent error — should be `NonRetryableApplicationError` |
| 5 | 75 | `activity/preflight.go` | `BuildEvalCloneCommands` has no `rm -rf` before `git clone` — fails on sandbox reuse |
| 6 | 75 | `activity/preflight.go` | `ParseGitHubTreeURL` doesn't validate host is `github.com` |
| 7 | 75 | `activity/preflight.go` | MCP `credentials` field declared but never resolved/injected |
| 8 | 75 | `model/agent_profile.go` | `AgentProfileBody` has `db:"body"` tag but no `sql.Scanner` impl — false contract |

---

## Task 1: Fix Delete handlers to check RowsAffected

`UpdateProfile` already does this correctly — bring `DeleteProfile` and `DeleteMarketplace` in line.

**Files:**
- Modify: `internal/server/handlers/profiles.go:286-294` (DeleteProfile)
- Modify: `internal/server/handlers/profiles.go:395-404` (DeleteMarketplace)
- Modify: `internal/server/handlers/profiles_test.go`

- [ ] **Step 1: Write failing tests**

In `internal/server/handlers/profiles_test.go`, add:

```go
func TestDeleteProfile_NotFound_Returns404(t *testing.T) {
    h, db := newTestProfilesHandler(t)
    defer db.Close()

    req := httptest.NewRequest(http.MethodDelete, "/api/agent-profiles/nonexistent-id", nil)
    req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{TeamID: "team-1"}))
    req.Header.Set("X-Team-ID", "team-1")
    w := httptest.NewRecorder()

    h.DeleteProfile(w, req)

    if w.Code != http.StatusNotFound {
        t.Errorf("expected 404, got %d", w.Code)
    }
}

func TestDeleteMarketplace_NotFound_Returns404(t *testing.T) {
    h, db := newTestProfilesHandler(t)
    defer db.Close()

    req := httptest.NewRequest(http.MethodDelete, "/api/marketplaces/nonexistent-id", nil)
    req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{TeamID: "team-1"}))
    req.Header.Set("X-Team-ID", "team-1")
    w := httptest.NewRecorder()

    h.DeleteMarketplace(w, req)

    if w.Code != http.StatusNotFound {
        t.Errorf("expected 404, got %d", w.Code)
    }
}
```

Look at the existing test file first to understand the test helper pattern (`newTestProfilesHandler`).

- [ ] **Step 2: Run tests to confirm they fail**

```
go test ./internal/server/handlers/... -run "TestDeleteProfile_NotFound|TestDeleteMarketplace_NotFound" -v
```

Expected: FAIL (currently returns 204 for missing rows)

- [ ] **Step 3: Fix DeleteProfile**

In `internal/server/handlers/profiles.go`, change `DeleteProfile`:

```go
// Before:
_, err := h.db.ExecContext(r.Context(),
    `DELETE FROM agent_profiles WHERE id = $1 AND team_id = $2`, id, teamID)
if err != nil {
    slog.Error("failed to delete agent profile", "error", err, "team_id", teamID, "id", id)
    writeJSONError(w, http.StatusInternalServerError, "failed to delete profile")
    return
}
w.WriteHeader(http.StatusNoContent)

// After:
result, err := h.db.ExecContext(r.Context(),
    `DELETE FROM agent_profiles WHERE id = $1 AND team_id = $2`, id, teamID)
if err != nil {
    slog.Error("failed to delete agent profile", "error", err, "team_id", teamID, "id", id)
    writeJSONError(w, http.StatusInternalServerError, "failed to delete profile")
    return
}
rows, err := result.RowsAffected()
if err != nil {
    slog.Error("failed to check delete result", "error", err)
    writeJSONError(w, http.StatusInternalServerError, "failed to delete profile")
    return
}
if rows == 0 {
    writeJSONError(w, http.StatusNotFound, "profile not found")
    return
}
w.WriteHeader(http.StatusNoContent)
```

- [ ] **Step 4: Fix DeleteMarketplace** (same pattern)

```go
result, err := h.db.ExecContext(r.Context(),
    `DELETE FROM marketplaces WHERE id = $1 AND team_id = $2`, id, teamID)
if err != nil {
    slog.Error("failed to delete marketplace", "error", err, "team_id", teamID, "id", id)
    writeJSONError(w, http.StatusInternalServerError, "failed to delete marketplace")
    return
}
rows, err := result.RowsAffected()
if err != nil {
    slog.Error("failed to check delete result", "error", err)
    writeJSONError(w, http.StatusInternalServerError, "failed to delete marketplace")
    return
}
if rows == 0 {
    writeJSONError(w, http.StatusNotFound, "marketplace not found")
    return
}
w.WriteHeader(http.StatusNoContent)
```

- [ ] **Step 5: Run tests**

```
go test ./internal/server/handlers/... -run "TestDeleteProfile|TestDeleteMarketplace" -v
```

Expected: PASS

- [ ] **Step 6: Lint + build**

```
make lint && go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/server/handlers/profiles.go internal/server/handlers/profiles_test.go
git commit -m "fix: DeleteProfile and DeleteMarketplace return 404 when no rows deleted"
```

---

## Task 2: Fix GetProfile to distinguish sql.ErrNoRows from other DB errors

**Files:**
- Modify: `internal/server/handlers/profiles.go:188-191` (GetProfile)
- Modify: `internal/server/handlers/profiles_test.go`

- [ ] **Step 1: Write failing test**

Add to `profiles_test.go` — a test that simulates a DB error (not ErrNoRows) and expects 500:

```go
func TestGetProfile_DBError_Returns500(t *testing.T) {
    // Use a closed DB to force a real connection error.
    db, err := sqlx.Open("postgres", "postgres://invalid:invalid@localhost:5999/nonexistent")
    if err != nil {
        t.Fatal(err)
    }
    h := NewProfilesHandler(db)

    req := httptest.NewRequest(http.MethodGet, "/api/agent-profiles/some-id", nil)
    req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{TeamID: "team-1"}))
    req.Header.Set("X-Team-ID", "team-1")
    rctx := chi.NewRouteContext()
    rctx.URLParams.Add("id", "some-id")
    req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
    w := httptest.NewRecorder()

    h.GetProfile(w, req)

    if w.Code != http.StatusInternalServerError {
        t.Errorf("expected 500 on DB error, got %d", w.Code)
    }
}
```

- [ ] **Step 2: Run test to confirm it fails**

```
go test ./internal/server/handlers/... -run "TestGetProfile_DBError" -v
```

Expected: FAIL (currently returns 404 for all errors)

- [ ] **Step 3: Fix GetProfile**

Add `"database/sql"` and `"errors"` imports if not present. Change the error handling:

```go
// Before:
err := h.db.QueryRowContext(...).Scan(...)
if err != nil {
    writeJSONError(w, http.StatusNotFound, "profile not found")
    return
}

// After:
err := h.db.QueryRowContext(...).Scan(...)
if errors.Is(err, sql.ErrNoRows) {
    writeJSONError(w, http.StatusNotFound, "profile not found")
    return
}
if err != nil {
    slog.Error("failed to get agent profile", "error", err, "id", id)
    writeJSONError(w, http.StatusInternalServerError, "failed to get profile")
    return
}
```

- [ ] **Step 4: Run tests**

```
go test ./internal/server/handlers/... -v
```

Expected: all PASS

- [ ] **Step 5: Lint + build**

```
make lint && go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/server/handlers/profiles.go internal/server/handlers/profiles_test.go
git commit -m "fix: GetProfile returns 500 on DB error instead of misleading 404"
```

---

## Task 3: Fix marketplace DB error swallowing in RunPreflight

The `if err == nil` pattern silently ignores DB errors.

**Files:**
- Modify: `internal/activity/preflight.go:24-37`

- [ ] **Step 1: Write failing test**

Add to `internal/activity/preflight_activity_test.go` (internal `package activity` — `preflightRecordingSandbox` is already defined here):

```go
func TestRunPreflight_MarketplaceDBError_ReturnsError(t *testing.T) {
    // sqlx.Open doesn't connect, so use a bad DSN to force errors on first query.
    db, _ := sqlx.Open("postgres", "postgres://invalid@localhost:5999/bad")
    acts := &Activities{
        DB:      db,
        Sandbox: &preflightRecordingSandbox{},
    }
    _, err := acts.RunPreflight(context.Background(), workflow.RunPreflightInput{
        SandboxID: "s1",
        TeamID:    "team-1",
        Profile:   model.AgentProfileBody{},
    })
    if err == nil {
        t.Fatal("expected error from DB failure, got nil")
    }
}
```

Note: add `"github.com/jmoiron/sqlx"` to imports in that file if not present.

- [ ] **Step 2: Run test to confirm it fails**

```
go test ./internal/activity/... -run "TestRunPreflight_MarketplaceDBError" -v
```

Expected: FAIL (currently returns no error — silently ignores DB failure)

- [ ] **Step 3: Fix the error handling**

In `internal/activity/preflight.go`, change lines 24-37:

```go
// Before:
err := a.DB.GetContext(ctx, &m, `...`, input.TeamID)
if err == nil {
    marketplaceURL = m.RepoURL
    ...
}

// After:
err := a.DB.GetContext(ctx, &m, `...`, input.TeamID)
if err != nil && !errors.Is(err, sql.ErrNoRows) {
    return workflow.RunPreflightOutput{}, fmt.Errorf("fetch marketplace config: %w", err)
}
if err == nil {
    marketplaceURL = m.RepoURL
    if m.Credential != nil && *m.Credential != "" && a.CredStore != nil {
        token, err := a.CredStore.Get(ctx, input.TeamID, *m.Credential)
        if err != nil {
            return workflow.RunPreflightOutput{}, fmt.Errorf("fetch marketplace credential: %w", err)
        }
        marketplaceToken = token
    }
}
```

Add imports: `"database/sql"` and `"errors"` to `preflight.go`.

- [ ] **Step 4: Run tests**

```
go test ./internal/activity/... -v
```

Expected: all PASS

- [ ] **Step 5: Lint + build**

```
make lint && go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/activity/preflight.go
git commit -m "fix: propagate marketplace DB errors in RunPreflight instead of silently ignoring"
```

---

## Task 4: Wrap permanent errors as NonRetryableApplicationError

Two permanent errors need this treatment:
1. `profiles.go:38` — "agent profile not found" (will never succeed on retry)
2. `preflight.go` — bad URL scheme / bad URL format returned from `RunPreflight` (the errors from `BuildEvalCloneCommands`)

**Files:**
- Modify: `internal/activity/profiles.go:38`
- Modify: `internal/activity/preflight.go:52-53` (where BuildEvalCloneCommands error is returned)

The Temporal import is already present in the codebase — check `credential.go` for the import path:
```go
temporal "go.temporal.io/sdk/temporal"
```

- [ ] **Step 1: Write tests for NonRetryable wrapping**

In `internal/activity/preflight_test.go`:

```go
func TestRunPreflight_InvalidURLScheme_IsNonRetryable(t *testing.T) {
    acts := &activity.Activities{
        Sandbox: &mockSandbox{},
    }
    _, err := acts.RunPreflight(context.Background(), workflow.RunPreflightInput{
        SandboxID:      "s1",
        TeamID:         "team-1",
        Profile:        model.AgentProfileBody{},
        EvalPluginURLs: []string{"git://github.com/org/repo/tree/main/plugins/foo"},
    })
    if err == nil {
        t.Fatal("expected error")
    }
    var appErr *temporal.ApplicationError
    if !errors.As(err, &appErr) || appErr.NonRetryable() == false {
        t.Errorf("expected NonRetryableApplicationError, got: %T %v", err, err)
    }
}
```

In `internal/activity/profiles_test.go` (look at existing file structure for mock `ProfileStore`):

```go
func TestResolveAgentProfile_NotFound_IsNonRetryable(t *testing.T) {
    store := &mockProfileStore{
        profiles: map[string]*model.AgentProfile{}, // empty — profile not found
    }
    acts := &activity.Activities{ProfileStore: store}
    _, err := acts.ResolveAgentProfile(context.Background(), workflow.ResolveProfileInput{
        TeamID:      "team-1",
        ProfileName: "nonexistent",
    })
    if err == nil {
        t.Fatal("expected error")
    }
    var appErr *temporal.ApplicationError
    if !errors.As(err, &appErr) || !appErr.NonRetryable() {
        t.Errorf("expected NonRetryableApplicationError, got: %T %v", err, err)
    }
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```
go test ./internal/activity/... -run "TestRunPreflight_InvalidURLScheme|TestResolveAgentProfile_NotFound" -v
```

- [ ] **Step 3: Fix profiles.go**

In `internal/activity/profiles.go`, add `temporal "go.temporal.io/sdk/temporal"` to the **existing** import block (do not replace it — the file already imports `"context"`, `"fmt"`, and two internal packages). Then change line 38:

```go
// Change:
return model.AgentProfileBody{}, fmt.Errorf("agent profile %q not found for team %s", name, input.TeamID)
// To:
return model.AgentProfileBody{}, temporal.NewNonRetryableApplicationError(
    fmt.Sprintf("agent profile %q not found for team %s", name, input.TeamID),
    "ProfileNotFound", nil,
)
```

- [ ] **Step 4: Fix preflight.go**

In `internal/activity/preflight.go`, change the `BuildEvalCloneCommands` error return:

```go
// Add import: temporal "go.temporal.io/sdk/temporal"

// Change:
cloneResults, err := BuildEvalCloneCommands(input.EvalPluginURLs)
if err != nil {
    return workflow.RunPreflightOutput{}, fmt.Errorf("build eval clone commands: %w", err)
}

// To:
cloneResults, err := BuildEvalCloneCommands(input.EvalPluginURLs)
if err != nil {
    return workflow.RunPreflightOutput{}, temporal.NewNonRetryableApplicationError(
        fmt.Sprintf("build eval clone commands: %s", err),
        "InvalidEvalPlugin", nil,
    )
}
```

- [ ] **Step 5: Run all activity tests**

```
go test ./internal/activity/... -v
```

- [ ] **Step 6: Lint + build**

```
make lint && go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/activity/profiles.go internal/activity/preflight.go internal/activity/preflight_test.go internal/activity/profiles_test.go
git commit -m "fix: wrap permanent errors as NonRetryableApplicationError to skip unnecessary retries"
```

---

## Task 5: Fix git clone idempotency — rm -rf before re-cloning

Without this, sandbox reuse causes `git clone` to fail because the directory already exists.

**Files:**
- Modify: `internal/activity/preflight.go:136-142` (BuildEvalCloneCommands)
- Modify: `internal/activity/preflight_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestBuildEvalCloneCommands_IncludesRmRf(t *testing.T) {
    results, err := activity.BuildEvalCloneCommands([]string{
        "https://github.com/org/repo/tree/main/plugins/foo",
    })
    if err != nil {
        t.Fatal(err)
    }
    if !strings.Contains(results[0].Command, "rm -rf") {
        t.Errorf("expected rm -rf in clone command for idempotency, got:\n%s", results[0].Command)
    }
}
```

- [ ] **Step 2: Run test to confirm it fails**

```
go test ./internal/activity/... -run "TestBuildEvalCloneCommands_IncludesRmRf" -v
```

- [ ] **Step 3: Fix BuildEvalCloneCommands**

In `internal/activity/preflight.go`, change the command construction:

```go
// Before:
cmd := fmt.Sprintf(
    "git clone --depth 1 --filter=blob:none --sparse %s %s && cd %s && git sparse-checkout set %s",
    shellquote.Quote(repoURL),
    shellquote.Quote(dir),
    shellquote.Quote(dir),
    shellquote.Quote(subPath),
)

// After:
cmd := fmt.Sprintf(
    "rm -rf %s && git clone --depth 1 --filter=blob:none --sparse %s %s && cd %s && git sparse-checkout set %s",
    shellquote.Quote(dir),
    shellquote.Quote(repoURL),
    shellquote.Quote(dir),
    shellquote.Quote(dir),
    shellquote.Quote(subPath),
)
```

- [ ] **Step 4: Run tests**

```
go test ./internal/activity/... -v
```

Expected: all PASS

- [ ] **Step 5: Lint + build**

```
make lint && go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/activity/preflight.go internal/activity/preflight_test.go
git commit -m "fix: rm -rf eval plugin dir before git clone to support sandbox reuse"
```

---

## Task 6: Fix ParseGitHubTreeURL to validate github.com host

**Files:**
- Modify: `internal/activity/preflight.go:153-166` (ParseGitHubTreeURL)
- Modify: `internal/activity/preflight_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestParseGitHubTreeURL_RejectsNonGitHubHost(t *testing.T) {
    _, _, err := activity.ParseGitHubTreeURL("https://github.example.com/org/repo/tree/main/plugins/foo")
    if err == nil {
        t.Fatal("expected error for non-github.com host")
    }
}
```

- [ ] **Step 2: Run test to confirm it fails**

```
go test ./internal/activity/... -run "TestParseGitHubTreeURL_RejectsNonGitHubHost" -v
```

- [ ] **Step 3: Fix ParseGitHubTreeURL**

```go
const githubPrefix = "https://github.com/"

func ParseGitHubTreeURL(u string) (string, string, error) {
    if !strings.HasPrefix(u, githubPrefix) {
        return "", "", fmt.Errorf("expected GitHub URL starting with %q, got %q", githubPrefix, u)
    }
    trimmed := strings.TrimPrefix(u, githubPrefix)
    parts := strings.SplitN(trimmed, "/tree/", 2)
    if len(parts) != 2 {
        return "", "", fmt.Errorf("expected GitHub tree URL with /tree/ component, got %q", u)
    }
    repoPath := parts[0]
    branchAndSub := parts[1]
    subParts := strings.SplitN(branchAndSub, "/", 2)
    if len(subParts) < 2 {
        return "", "", fmt.Errorf("expected branch and subpath after /tree/ in %q", u)
    }
    return githubPrefix + repoPath + ".git", subParts[1], nil
}
```

Note: The `BuildEvalCloneCommands` `https://` prefix check is still valid as a first gate. `ParseGitHubTreeURL` now adds the `github.com` host check as an additional guard.

- [ ] **Step 4: Run tests**

```
go test ./internal/activity/... -v
```

Expected: all PASS

- [ ] **Step 5: Lint + build**

```
make lint && go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/activity/preflight.go internal/activity/preflight_test.go
git commit -m "fix: ParseGitHubTreeURL validates host is github.com to prevent malformed clone URLs"
```

---

## Task 7: Inject MCP credentials at runtime

This is the most complex fix. MCP `credentials` field lists CredStore key names; these must be resolved and their values exported as env vars in the preflight script so MCP header values like `Bearer ${MY_TOKEN}` resolve correctly.

**Files:**
- Modify: `internal/activity/preflight.go` — `RunPreflight`, `BuildPreflightScript`
- Modify: `internal/activity/preflight_test.go`

**Design:**
- `RunPreflight` collects all unique credential names from `profile.MCPs[*].Credentials`
- Resolves each via `a.CredStore.Get()`
- Passes the resolved `map[string]string` to `BuildPreflightScript`
- `BuildPreflightScript` emits `export KEY=VALUE` lines at the top of the script

- [ ] **Step 1: Write failing tests**

Add to `internal/activity/preflight_test.go` (external `package activity_test`). Define `stubCredStore` and `capturingSandbox` at the top of the file — `profiles_test.go` shows the pattern for `stubProfileStore`. The sandbox mock needs to implement `sandbox.Client`:

```go
// capturingSandbox captures the script passed to Exec.
type capturingSandbox struct {
    captured string
}

func (s *capturingSandbox) Create(_ context.Context, _ sandbox.CreateOpts) (string, error) {
    return "sb-test", nil
}
func (s *capturingSandbox) Exec(_ context.Context, _, cmd, _ string) (string, string, error) {
    s.captured = cmd
    return "", "", nil
}
func (s *capturingSandbox) ExecStream(_ context.Context, _, _, _ string, _ func(string)) error {
    return nil
}
func (s *capturingSandbox) WriteFile(_ context.Context, _, _, _ string) error            { return nil }
func (s *capturingSandbox) WriteBytes(_ context.Context, _, _ string, _ []byte) error    { return nil }
func (s *capturingSandbox) ReadFile(_ context.Context, _, _ string) (string, error)      { return "", nil }
func (s *capturingSandbox) ReadBytes(_ context.Context, _, _ string) ([]byte, error)     { return nil, nil }
func (s *capturingSandbox) Kill(_ context.Context, _ string) error                       { return nil }
func (s *capturingSandbox) RenewExpiration(_ context.Context, _ string) error            { return nil }

// stubCredStore resolves credentials from an in-memory map.
type stubCredStore struct {
    creds map[string]string
}

func (s *stubCredStore) Get(_ context.Context, _, name string) (string, error) {
    if v, ok := s.creds[name]; ok {
        return v, nil
    }
    return "", fmt.Errorf("credential %q not found", name)
}

func TestBuildPreflightScript_MCPCredentialsExported(t *testing.T) {
    profile := model.AgentProfileBody{
        MCPs: []model.MCPConfig{{
            Name:        "auth-mcp",
            Transport:   "sse",
            URL:         "https://mcp.example.com/sse",
            Headers:     []model.Header{{Name: "Authorization", Value: "Bearer ${MY_TOKEN}"}},
            Credentials: []string{"MY_TOKEN"},
        }},
    }
    resolvedCreds := map[string]string{"MY_TOKEN": "secret-value"}
    script := activity.BuildPreflightScript(profile, "", "", resolvedCreds)
    if !strings.Contains(script, "export MY_TOKEN=") {
        t.Errorf("expected export MY_TOKEN in script, got:\n%s", script)
    }
    if !strings.Contains(script, "secret-value") {
        t.Errorf("expected resolved credential value in script, got:\n%s", script)
    }
}

func TestRunPreflight_ResolvesAndInjectsMCPCredentials(t *testing.T) {
    sb := &capturingSandbox{}
    acts := &activity.Activities{
        Sandbox:   sb,
        CredStore: &stubCredStore{creds: map[string]string{"MY_TOKEN": "resolved-secret"}},
    }
    _, err := acts.RunPreflight(context.Background(), workflow.RunPreflightInput{
        SandboxID: "s1",
        TeamID:    "team-1",
        Profile: model.AgentProfileBody{
            MCPs: []model.MCPConfig{{
                Name:        "auth-mcp",
                Transport:   "sse",
                URL:         "https://mcp.example.com/sse",
                Credentials: []string{"MY_TOKEN"},
            }},
        },
    })
    if err != nil {
        t.Fatal(err)
    }
    if !strings.Contains(sb.captured, "export MY_TOKEN=") {
        t.Errorf("expected MCP credential exported in script, got:\n%s", sb.captured)
    }
}
```

Add `"github.com/tinkerloft/fleetlift/internal/sandbox"` to imports in `preflight_test.go`. Check what interface methods `sandbox.Client` requires by reading `internal/sandbox/sandbox.go`.

- [ ] **Step 2: Run tests to confirm they fail**

```
go test ./internal/activity/... -run "TestBuildPreflightScript_MCPCredentials|TestRunPreflight_Resolves" -v
```

- [ ] **Step 3: Update BuildPreflightScript signature**

Change the function signature to accept resolved credentials. **Security note:** credential names are validated against `^[A-Z][A-Z0-9_]*$` by the project's existing `validateCredentialNames` function before they reach `RunPreflight` (see DAG preflight credential validation). As an additional defence-in-depth guard, skip any name that doesn't match the pattern rather than letting it corrupt the shell script.

```go
// BuildPreflightScript generates the shell script to install marketplace plugins and MCPs.
// marketplaceToken is the resolved credential value for private marketplace auth (empty = public).
// mcpCreds maps credential name → resolved value; these are exported as env vars at the top.
// Credential names must match ^[A-Z][A-Z0-9_]*$ — others are silently skipped.
func BuildPreflightScript(profile model.AgentProfileBody, marketplaceURL, marketplaceToken string, mcpCreds map[string]string) string {
    var b strings.Builder

    // Export resolved MCP credentials as env vars so header templates resolve at runtime.
    // Names are already validated upstream; the pattern check here is defence-in-depth.
    for name, value := range mcpCreds {
        if !validCredName(name) {
            continue
        }
        fmt.Fprintf(&b, "export %s=%s\n", name, shellquote.Quote(value))
    }

    // ... rest of existing function unchanged ...
```

Add a package-level helper at the bottom of `preflight.go`:

```go
import "regexp"

var credNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func validCredName(name string) bool {
    return credNameRe.MatchString(name)
}
```

- [ ] **Step 4: Update RunPreflight to resolve MCP credentials**

In `RunPreflight`, after resolving the marketplace credential, add:

```go
// Resolve MCP credentials.
mcpCreds := map[string]string{}
if a.CredStore != nil {
    seen := map[string]bool{}
    for _, mcp := range input.Profile.MCPs {
        for _, credName := range mcp.Credentials {
            if seen[credName] {
                continue
            }
            seen[credName] = true
            val, err := a.CredStore.Get(ctx, input.TeamID, credName)
            if err != nil {
                return workflow.RunPreflightOutput{}, fmt.Errorf("resolve MCP credential %q: %w", credName, err)
            }
            mcpCreds[credName] = val
        }
    }
}

script := BuildPreflightScript(input.Profile, marketplaceURL, marketplaceToken, mcpCreds)
```

- [ ] **Step 5: Update remaining test callers of BuildPreflightScript**

The production call in `RunPreflight` is already updated in Step 4. The remaining callers are all in `internal/activity/preflight_test.go`. Update each `activity.BuildPreflightScript(...)` call there to pass an extra `nil` (or `map[string]string{}`) as the fourth argument.

- [ ] **Step 6: Run all tests**

```
go test ./internal/activity/... -v
```

Expected: all PASS

- [ ] **Step 7: Lint + build**

```
make lint && go build ./...
```

- [ ] **Step 8: Commit**

```bash
git add internal/activity/preflight.go internal/activity/preflight_test.go
git commit -m "fix: resolve and inject MCP credentials as env vars in preflight script"
```

---

## Task 8: Fix AgentProfileBody db:"body" tag

The `db:"body"` tag on `AgentProfileBody` promises sqlx StructScan compatibility but the type doesn't implement `sql.Scanner`. This creates a false contract. Fix by implementing `Scan` and `Value` on `AgentProfileBody` so the tag actually works.

**Files:**
- Modify: `internal/model/agent_profile.go`
- Modify: `internal/activity/profiles_db.go` — remove manual scan workaround
- Modify: `internal/server/handlers/profiles.go` — remove manual scan workarounds

- [ ] **Step 1: Write failing test**

In `internal/model/agent_profile_test.go`, add:

```go
func TestAgentProfileBody_ImplementsScanner(t *testing.T) {
    var body model.AgentProfileBody
    // Simulate what sql.Scan does for a JSONB column: passes []byte.
    jsonData := []byte(`{"plugins":[{"plugin":"foo"}]}`)
    if err := body.Scan(jsonData); err != nil {
        t.Fatalf("Scan failed: %v", err)
    }
    if len(body.Plugins) != 1 || body.Plugins[0].Plugin != "foo" {
        t.Errorf("unexpected body after Scan: %+v", body)
    }
}

func TestAgentProfileBody_ImplementsValuer(t *testing.T) {
    body := model.AgentProfileBody{
        Plugins: []model.PluginSource{{Plugin: "foo"}},
    }
    val, err := body.Value()
    if err != nil {
        t.Fatalf("Value() failed: %v", err)
    }
    b, ok := val.([]byte)
    if !ok {
        t.Fatalf("expected []byte from Value(), got %T", val)
    }
    if !strings.Contains(string(b), "foo") {
        t.Errorf("unexpected Value output: %s", b)
    }
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```
go test ./internal/model/... -run "TestAgentProfileBody_Implements" -v
```

- [ ] **Step 3: Implement Scan and Value on AgentProfileBody**

Add to the **existing** import block in `internal/model/agent_profile.go` (the file already imports `"fmt"` and others — do not replace the block):

```go
"database/sql/driver"
"encoding/json"
```

Then add the methods:

```go
// Scan implements sql.Scanner so AgentProfileBody can be used directly in sqlx StructScan.
func (b *AgentProfileBody) Scan(src any) error {
    var data []byte
    switch v := src.(type) {
    case []byte:
        data = v
    case string:
        data = []byte(v)
    case nil:
        *b = AgentProfileBody{}
        return nil
    default:
        return fmt.Errorf("AgentProfileBody.Scan: unsupported type %T", src)
    }
    return json.Unmarshal(data, b)
}

// Value implements driver.Valuer so AgentProfileBody serializes correctly for PostgreSQL.
func (b AgentProfileBody) Value() (driver.Value, error) {
    return json.Marshal(b)
}
```

- [ ] **Step 4: Run tests**

```
go test ./internal/model/... -v
```

Expected: PASS

- [ ] **Step 5: Simplify profiles_db.go**

Now that `AgentProfileBody` implements `sql.Scanner`, `scanProfile` can scan `body` directly:

```go
func scanProfile(row *sql.Row) (*model.AgentProfile, error) {
    var p model.AgentProfile
    err := row.Scan(&p.ID, &p.TeamID, &p.Name, &p.Description, &p.Body, &p.CreatedAt, &p.UpdatedAt)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, nil
    }
    if err != nil {
        return nil, err
    }
    return &p, nil
}
```

Remove the `bodyJSON []byte` variable and the `json.Unmarshal` call. Also remove the `"encoding/json"` import from `profiles_db.go` — it will no longer be used (otherwise the build will fail with "imported and not used").

- [ ] **Step 6: Simplify handler scans**

In `internal/server/handlers/profiles.go`, all the `var bodyBytes []byte` + `json.Unmarshal` patterns can be replaced with direct scanning:

`ListProfiles`:
```go
// Remove: var bodyBytes []byte
// Change Scan: rows.Scan(&p.ID, &p.TeamID, &p.Name, &p.Description, &p.Body, &p.CreatedAt, &p.UpdatedAt)
// Remove: json.Unmarshal(bodyBytes, &p.Body) call
```

Same for `GetProfile`, `UpdateProfile` (re-fetch), `CreateProfile` (no scan needed there, already in memory).

- [ ] **Step 7: Run all tests**

```
go test ./... -v 2>&1 | tail -50
```

Expected: all PASS

- [ ] **Step 8: Lint + build**

```
make lint && go build ./...
```

- [ ] **Step 9: Commit**

```bash
git add internal/model/agent_profile.go internal/activity/profiles_db.go internal/server/handlers/profiles.go
git commit -m "fix: AgentProfileBody implements sql.Scanner/driver.Valuer for safe JSONB scanning"
```

---

## Final Verification

- [ ] Run full test suite: `go test ./...`
- [ ] Run linter: `make lint`
- [ ] Run build: `go build ./...`
