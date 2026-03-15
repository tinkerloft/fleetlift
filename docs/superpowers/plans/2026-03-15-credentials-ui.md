# Credentials UI Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a web UI for viewing, creating, and deleting team and system-wide credentials, backed by a schema migration and new system-credential API endpoints.

**Architecture:** The existing `credentials` table is extended to allow `team_id = NULL` for system-wide credentials, enforced by two partial unique indexes. A new `SystemCredentialsHandler` mirrors the team handler but requires `PlatformAdmin` claims. The frontend adds a `/settings` page with two sections (team / system), an inline add form, and inline delete confirmation.

**Tech Stack:** Go + chi + sqlx + PostgreSQL (backend); React 19 + TypeScript + React Query + Tailwind + Radix UI (frontend)

---

## Chunk 1: Backend

### Task 1: Schema migration

**Files:**
- Modify: `internal/db/schema.sql`

- [ ] **Step 1: Append the migration to schema.sql**

Add these lines at the very end of `internal/db/schema.sql`:

```sql
-- Added 2026-03-15: allow system-wide credentials (team_id = NULL)
-- Wrapped in a DO block so re-running schema.sql is idempotent.
DO $$ BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name = 'credentials' AND column_name = 'team_id' AND is_nullable = 'NO'
  ) THEN
    ALTER TABLE credentials ALTER COLUMN team_id DROP NOT NULL;
  END IF;
END $$;
ALTER TABLE credentials DROP CONSTRAINT IF EXISTS credentials_team_id_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS credentials_team_name_unique
  ON credentials (team_id, name) WHERE team_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS credentials_system_name_unique
  ON credentials (name) WHERE team_id IS NULL;
```

- [ ] **Step 2: Verify the existing constraint name on your local DB**

Run this before applying the migration to confirm the constraint name matches what the migration will drop:

```bash
psql $DATABASE_URL -c "SELECT conname FROM pg_constraint WHERE conrelid = 'credentials'::regclass AND contype = 'u';"
```

Expected output should include `credentials_team_id_name_key`. If the name differs, update the `DROP CONSTRAINT IF EXISTS` line in the migration accordingly. (On a fresh DB built from schema.sql, this name is guaranteed by PostgreSQL naming conventions.)

- [ ] **Step 3: Build to verify schema compiles**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Apply migration to local DB**

```bash
psql $DATABASE_URL -f internal/db/schema.sql
```

Expected: no ERROR lines. The script outputs `DO`, `ALTER TABLE`, `CREATE INDEX`, `CREATE INDEX` (plus `CREATE TABLE`, `CREATE INDEX`, etc. from earlier idempotent statements). Re-running the script a second time should also produce no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/db/schema.sql
git commit -m "feat: allow system-wide credentials via nullable team_id"
```

---

### Task 2: Name validation + fix ON CONFLICT in team credentials handler

**Files:**
- Modify: `internal/server/handlers/credentials.go`

- [ ] **Step 1: Write the failing test for name validation**

Create `internal/server/handlers/credentials_test.go`:

```go
package handlers

import (
	"bytes"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/auth"
)

func newTestCredentialsHandler(t *testing.T) *CredentialsHandler {
	t.Helper()
	key := make([]byte, 32)
	h, err := NewCredentialsHandler(nil, hex.EncodeToString(key))
	require.NoError(t, err)
	return h
}

func claimsCtx(r *http.Request, teamID string) *http.Request {
	claims := &auth.Claims{
		UserID:    "user-1",
		TeamRoles: map[string]string{teamID: "admin"},
	}
	return r.WithContext(auth.SetClaimsInContext(r.Context(), claims))
}

func TestCredentials_Set_RejectsInvalidName(t *testing.T) {
	h := newTestCredentialsHandler(t)
	cases := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{"lowercase rejected", `{"name":"my_token","value":"v"}`, "invalid credential name"},
		{"starts with digit rejected", `{"name":"1TOKEN","value":"v"}`, "invalid credential name"},
		{"reserved name rejected", `{"name":"PATH","value":"v"}`, "reserved"},
		{"empty name rejected", `{"name":"","value":"v"}`, "required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/credentials",
				strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = claimsCtx(req, "team-1")
			req.Header.Set("X-Team-ID", "team-1")
			w := httptest.NewRecorder()
			h.Set(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), tc.wantMsg)
		})
	}
}

func TestCredentials_Set_AcceptsValidName_PassesValidation(t *testing.T) {
	// Verify a valid name passes all validation checks.
	// We confirm this by checking the name against the regex and reserved list directly.
	assert.NoError(t, validateCredentialName("GITHUB_TOKEN"))
	assert.NoError(t, validateCredentialName("MY_API_KEY_123"))
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/server/handlers/ -run TestCredentials_Set -v
```

Expected: FAIL — `TestCredentials_Set_RejectsInvalidName` fails because validation isn't implemented yet.

- [ ] **Step 3: Add name validation + fix ON CONFLICT in credentials.go**

Add `"regexp"` to the existing import block in `credentials.go` (the block already has `"encoding/hex"`, `"encoding/json"`, `"fmt"`, `"net/http"`). The updated stdlib section of the import block should look like:

```go
import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	// ... rest of imports unchanged
)
```

Then, directly after the import block, add the package-level variables and validation helper:

```go
var (
	credentialNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	reservedCredentialNames = map[string]bool{
		"PATH": true, "LD_PRELOAD": true, "LD_LIBRARY_PATH": true,
		"HOME": true, "USER": true, "SHELL": true,
		"TMPDIR": true, "TMP": true, "TEMP": true,
	}
)

// validateCredentialName checks that name matches ^[A-Z][A-Z0-9_]*$ and is not reserved.
func validateCredentialName(name string) error {
	if name == "" {
		return fmt.Errorf("name and value are required")
	}
	if !credentialNameRe.MatchString(name) {
		return fmt.Errorf("invalid credential name: must match ^[A-Z][A-Z0-9_]*$")
	}
	if reservedCredentialNames[name] {
		return fmt.Errorf("credential name %q is reserved", name)
	}
	return nil
}
```

Then in the `Set` method, replace the existing validation block (lines 83–86) and the ON CONFLICT SQL:

```go
// Replace:
if req.Name == "" || req.Value == "" {
    writeJSONError(w, http.StatusBadRequest, "name and value are required")
    return
}

// With:
if req.Value == "" {
    writeJSONError(w, http.StatusBadRequest, "name and value are required")
    return
}
if err := validateCredentialName(req.Name); err != nil {
    writeJSONError(w, http.StatusBadRequest, err.Error())
    return
}
```

And replace the ON CONFLICT SQL (line 100-102):

```go
// Replace:
`INSERT INTO credentials (team_id, name, value_enc)
 VALUES ($1, $2, $3)
 ON CONFLICT (team_id, name) DO UPDATE SET value_enc = $3, updated_at = now()`,

// With:
`INSERT INTO credentials (team_id, name, value_enc)
 VALUES ($1, $2, $3)
 ON CONFLICT (team_id, name) WHERE team_id IS NOT NULL
 DO UPDATE SET value_enc = $3, updated_at = now()`,
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/server/handlers/ -run TestCredentials_Set -v
```

Expected: PASS.

- [ ] **Step 5: Build check**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/server/handlers/credentials.go \
        internal/server/handlers/credentials_test.go
git commit -m "feat: add credential name validation and fix ON CONFLICT for partial index"
```

---

### Task 3: System credentials handler

**Files:**
- Create: `internal/server/handlers/system_credentials.go`
- Create: `internal/server/handlers/system_credentials_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/server/handlers/system_credentials_test.go`:

```go
package handlers

import (
	"bytes"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/auth"
)

func newTestSystemCredentialsHandler(t *testing.T) *SystemCredentialsHandler {
	t.Helper()
	key := make([]byte, 32)
	h, err := NewSystemCredentialsHandler(nil, hex.EncodeToString(key))
	require.NoError(t, err)
	return h
}

func adminCtx(r *http.Request) *http.Request {
	claims := &auth.Claims{UserID: "user-1", PlatformAdmin: true}
	return r.WithContext(auth.SetClaimsInContext(r.Context(), claims))
}

func nonAdminCtx(r *http.Request) *http.Request {
	claims := &auth.Claims{UserID: "user-1", TeamRoles: map[string]string{"team-1": "member"}}
	return r.WithContext(auth.SetClaimsInContext(r.Context(), claims))
}

func TestSystemCredentials_List_RequiresAdmin(t *testing.T) {
	h := newTestSystemCredentialsHandler(t)
	req := httptest.NewRequest("GET", "/api/system-credentials", nil)
	req = nonAdminCtx(req)
	w := httptest.NewRecorder()
	h.List(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestSystemCredentials_Set_RequiresAdmin(t *testing.T) {
	h := newTestSystemCredentialsHandler(t)
	body := bytes.NewBufferString(`{"name":"MY_KEY","value":"v"}`)
	req := httptest.NewRequest("POST", "/api/system-credentials", body)
	req.Header.Set("Content-Type", "application/json")
	req = nonAdminCtx(req)
	w := httptest.NewRecorder()
	h.Set(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestSystemCredentials_Delete_RequiresAdmin(t *testing.T) {
	h := newTestSystemCredentialsHandler(t)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "MY_KEY")
	req := httptest.NewRequest("DELETE", "/api/system-credentials/MY_KEY", nil)
	req = req.WithContext(chi.NewContext(req.Context(), rctx))
	req = nonAdminCtx(req)
	w := httptest.NewRecorder()
	h.Delete(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestSystemCredentials_Set_RejectsInvalidName(t *testing.T) {
	h := newTestSystemCredentialsHandler(t)
	cases := []string{`{"name":"lower","value":"v"}`, `{"name":"PATH","value":"v"}`}
	for _, body := range cases {
		req := httptest.NewRequest("POST", "/api/system-credentials",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = adminCtx(req)
		w := httptest.NewRecorder()
		h.Set(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	}
}

func TestSystemCredentials_Set_AcceptsValidName_PassesValidation(t *testing.T) {
	// Verify a valid name passes all validation checks.
	// validateCredentialName is shared between team and system handlers (same package).
	assert.NoError(t, validateCredentialName("DATADOG_API_KEY"))
}
```

- [ ] **Step 2: Run tests to verify they fail (type not found)**

```bash
go test ./internal/server/handlers/ -run TestSystemCredentials -v
```

Expected: compile error — `SystemCredentialsHandler` undefined.

- [ ] **Step 3: Create the system credentials handler**

Create `internal/server/handlers/system_credentials.go`:

```go
package handlers

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"

	"github.com/tinkerloft/fleetlift/internal/auth"
	flcrypto "github.com/tinkerloft/fleetlift/internal/crypto"
)

// SystemCredentialsHandler handles system-wide credential endpoints (admin only).
type SystemCredentialsHandler struct {
	db            *sqlx.DB
	encryptionKey string
}

// NewSystemCredentialsHandler creates a new SystemCredentialsHandler.
func NewSystemCredentialsHandler(db *sqlx.DB, encryptionKeyHex string) (*SystemCredentialsHandler, error) {
	key, err := hex.DecodeString(encryptionKeyHex)
	if err != nil {
		return nil, fmt.Errorf("decode encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY must be exactly 32 bytes (64 hex chars), got %d bytes", len(key))
	}
	return &SystemCredentialsHandler{db: db, encryptionKey: encryptionKeyHex}, nil
}

// List returns system credential names (not values). Requires PlatformAdmin.
func (h *SystemCredentialsHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil || !claims.PlatformAdmin {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	var creds []credentialEntry
	err := h.db.SelectContext(r.Context(), &creds,
		`SELECT name, created_at, updated_at FROM credentials WHERE team_id IS NULL ORDER BY name`)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list system credentials")
		return
	}

	writeJSON(w, http.StatusOK, creds)
}

// Set creates or updates a system credential. Requires PlatformAdmin.
func (h *SystemCredentialsHandler) Set(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil || !claims.PlatformAdmin {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req setCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Value == "" {
		writeJSONError(w, http.StatusBadRequest, "name and value are required")
		return
	}
	if err := validateCredentialName(req.Name); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	encrypted, err := flcrypto.EncryptAESGCM(h.encryptionKey, req.Value)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "encryption failed")
		return
	}

	_, err = h.db.ExecContext(r.Context(),
		`INSERT INTO credentials (team_id, name, value_enc)
		 VALUES (NULL, $1, $2)
		 ON CONFLICT (name) WHERE team_id IS NULL
		 DO UPDATE SET value_enc = $2, updated_at = now()`,
		req.Name, encrypted)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to save system credential")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Delete removes a system credential. Requires PlatformAdmin.
func (h *SystemCredentialsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil || !claims.PlatformAdmin {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	name := chi.URLParam(r, "name")

	result, err := h.db.ExecContext(r.Context(),
		`DELETE FROM credentials WHERE team_id IS NULL AND name = $1`,
		name)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to delete system credential")
		return
	}

	rows, err := result.RowsAffected()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to check deletion result")
		return
	}
	if rows == 0 {
		writeJSONError(w, http.StatusNotFound, "system credential not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/server/handlers/ -run TestSystemCredentials -v
```

Expected: all PASS.

- [ ] **Step 5: Run all handler tests**

```bash
go test ./internal/server/handlers/ -v
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/server/handlers/system_credentials.go \
        internal/server/handlers/system_credentials_test.go
git commit -m "feat: add system credentials handler (admin-only)"
```

---

### Task 4: Wire system credentials into router and server

**Files:**
- Modify: `internal/server/router.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Add SystemCredentials to Deps and register routes in router.go**

In `router.go`, add `SystemCredentials` to the `Deps` struct (after line 28):

```go
// In Deps struct, after Credentials field:
SystemCredentials *handlers.SystemCredentialsHandler
```

In the authenticated API group (after the existing Credentials routes, lines 117-119), add:

```go
// System Credentials (admin only)
r.Get("/api/system-credentials", deps.SystemCredentials.List)
r.Post("/api/system-credentials", deps.SystemCredentials.Set)
r.Delete("/api/system-credentials/{name}", deps.SystemCredentials.Delete)
```

- [ ] **Step 2: Wire handler in cmd/server/main.go**

After the `credHandler` construction (line 68-71), add:

```go
sysCredHandler, err := handlers.NewSystemCredentialsHandler(database, encKey)
if err != nil {
    log.Fatalf("invalid credential encryption key for system credentials: %v", err)
}
```

Then in the `deps` struct literal (after `Credentials: credHandler,`), add:

```go
SystemCredentials: sysCredHandler,
```

- [ ] **Step 3: Build to verify**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Run all tests**

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/router.go cmd/server/main.go
git commit -m "feat: register system credentials routes"
```

---

## Chunk 2: Frontend

### Task 5: Add Credential type and fix API client

**Files:**
- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: Add the Credential type to types.ts**

In `web/src/api/types.ts`, add after the `UserTeam` and `UserProfile` interfaces (after line 121):

```typescript
export interface Credential {
  name: string
  created_at: string
  updated_at: string
}
```

- [ ] **Step 2: Fix the post() helper in client.ts to handle 204**

In `web/src/api/client.ts`, replace the `post` function (lines 33-44):

```typescript
export async function post<T = void>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeaders() },
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error ?? res.statusText)
  }
  if (res.status === 204 || res.headers.get('content-length') === '0') {
    return undefined as T
  }
  return res.json()
}
```

- [ ] **Step 3: Add credential methods to the api object in client.ts**

Add the `Credential` import at the top of `client.ts`:

```typescript
import type {
  WorkflowTemplate, Run, StepRunLog,
  InboxItem, Artifact, ListResponse, RunStatusUpdate,
  UserProfile, Credential,
} from './types'
```

Add six methods to the `api` object (after the existing `getMe` entry):

```typescript
// Credentials
listCredentials: () => get<Credential[]>('/credentials'),
setCredential: (name: string, value: string) => post('/credentials', { name, value }),
deleteCredential: (name: string) => del(`/credentials/${name}`),

// System credentials (admin only)
listSystemCredentials: () => get<Credential[]>('/system-credentials'),
setSystemCredential: (name: string, value: string) => post('/system-credentials', { name, value }),
deleteSystemCredential: (name: string) => del(`/system-credentials/${name}`),
```

- [ ] **Step 4: Build the frontend to verify**

```bash
cd web && npm run build
```

Expected: no TypeScript errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/api/types.ts web/src/api/client.ts
git commit -m "feat: add Credential type and credential API methods"
```

---

### Task 6: CredentialsPage component

**Files:**
- Create: `web/src/pages/CredentialsPage.tsx`

- [ ] **Step 1: Create the CredentialsPage component**

Create `web/src/pages/CredentialsPage.tsx`:

```tsx
import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { Credential } from '@/api/types'

function formatRelativeTime(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime()
  const minutes = Math.floor(diff / 60000)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  if (days < 7) return `${days}d ago`
  return `${Math.floor(days / 7)}w ago`
}

interface CredentialSectionProps {
  title: string
  queryKey: readonly string[]
  fetchFn: () => Promise<Credential[]>
  setFn: (name: string, value: string) => Promise<void>
  deleteFn: (name: string) => Promise<void>
}

function CredentialSection({ title, queryKey, fetchFn, setFn, deleteFn }: CredentialSectionProps) {
  const queryClient = useQueryClient()
  const [addForm, setAddForm] = useState({ isOpen: false, name: '', value: '' })
  const [pendingDelete, setPendingDelete] = useState<string | null>(null)
  const [formError, setFormError] = useState<string | null>(null)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  const { data: creds = [], isLoading, error } = useQuery({
    queryKey: [...queryKey],
    queryFn: fetchFn,
  })

  const setMutation = useMutation({
    mutationFn: ({ name, value }: { name: string; value: string }) => setFn(name, value),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...queryKey] })
      setAddForm({ isOpen: false, name: '', value: '' })
      setFormError(null)
    },
    onError: (err: Error) => setFormError(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (name: string) => deleteFn(name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...queryKey] })
      setPendingDelete(null)
      setDeleteError(null)
    },
    onError: (err: Error) => setDeleteError(err.message),
  })

  const handleSave = () => {
    if (!addForm.name || !addForm.value) {
      setFormError('Name and value are required')
      return
    }
    setMutation.mutate({ name: addForm.name, value: addForm.value })
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">{title}</h2>
        {!addForm.isOpen && (
          <button
            onClick={() => setAddForm({ isOpen: true, name: '', value: '' })}
            className="text-sm px-3 py-1.5 bg-primary text-primary-foreground rounded-md hover:bg-primary/90"
          >
            + Add
          </button>
        )}
      </div>

      {addForm.isOpen && (
        <div className="rounded-lg border border-primary/50 bg-primary/5 p-4 space-y-3">
          {formError && (
            <div className="rounded-md bg-red-500/10 border border-red-500/20 p-2 text-xs text-red-400">
              {formError}
            </div>
          )}
          <div className="flex gap-3">
            <input
              className="flex-1 rounded-md border bg-background px-3 py-1.5 text-sm font-mono"
              placeholder="CREDENTIAL_NAME"
              value={addForm.name}
              onChange={e => setAddForm(f => ({ ...f, name: e.target.value.toUpperCase() }))}
            />
            <input
              type="password"
              className="flex-1 rounded-md border bg-background px-3 py-1.5 text-sm"
              placeholder="Value"
              value={addForm.value}
              onChange={e => setAddForm(f => ({ ...f, value: e.target.value }))}
            />
          </div>
          <div className="flex justify-end gap-2">
            <button
              onClick={() => { setAddForm({ isOpen: false, name: '', value: '' }); setFormError(null) }}
              className="text-sm px-3 py-1.5 rounded-md border text-muted-foreground hover:bg-muted"
            >
              Cancel
            </button>
            <button
              onClick={handleSave}
              disabled={setMutation.isPending}
              className="text-sm px-3 py-1.5 bg-primary text-primary-foreground rounded-md hover:bg-primary/90 disabled:opacity-50"
            >
              {setMutation.isPending ? 'Saving...' : 'Save'}
            </button>
          </div>
        </div>
      )}

      {isLoading && <p className="text-sm text-muted-foreground">Loading...</p>}

      {error && (
        <div className="rounded-md bg-red-500/10 border border-red-500/20 p-3 text-sm text-red-400">
          {(error as Error).message}
        </div>
      )}

      {deleteError && (
        <div className="rounded-md bg-red-500/10 border border-red-500/20 p-3 text-sm text-red-400">
          Delete failed: {deleteError}
        </div>
      )}

      {!isLoading && !error && creds.length === 0 && (
        <p className="text-sm text-muted-foreground">
          No credentials yet. Click + Add to create one.
        </p>
      )}

      {creds.length > 0 && (
        <div className="rounded-lg border overflow-hidden">
          {creds.map((cred, i) => (
            <div
              key={cred.name}
              className={`flex items-center justify-between px-4 py-3${i < creds.length - 1 ? ' border-b' : ''}`}
            >
              <div>
                <span className="font-mono text-sm">{cred.name}</span>
                <span className="ml-3 text-xs text-muted-foreground">
                  updated {formatRelativeTime(cred.updated_at)}
                </span>
              </div>

              {pendingDelete === cred.name ? (
                <div className="flex items-center gap-2">
                  <span className="text-xs text-muted-foreground">Are you sure?</span>
                  <button
                    onClick={() => deleteMutation.mutate(cred.name)}
                    disabled={deleteMutation.isPending}
                    className="px-2 py-1 bg-red-600 text-white rounded text-xs hover:bg-red-700 disabled:opacity-50"
                  >
                    Confirm
                  </button>
                  <button
                    onClick={() => { setPendingDelete(null); setDeleteError(null) }}
                    className="px-2 py-1 rounded text-xs border text-muted-foreground hover:bg-muted"
                  >
                    Cancel
                  </button>
                </div>
              ) : (
                <button
                  onClick={() => setPendingDelete(cred.name)}
                  className="text-xs px-2 py-1 rounded text-muted-foreground hover:text-red-400 hover:bg-red-500/10"
                >
                  Delete
                </button>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

export function CredentialsPage() {
  const { data: me } = useQuery({
    queryKey: ['me'],
    queryFn: () => api.getMe(),
  })

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-bold">Settings</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Manage credentials available to your workflows.
        </p>
      </div>

      <CredentialSection
        title="Team Credentials"
        queryKey={['credentials', 'team']}
        fetchFn={() => api.listCredentials()}
        setFn={(name, value) => api.setCredential(name, value)}
        deleteFn={(name) => api.deleteCredential(name)}
      />

      {me?.platform_admin && (
        <CredentialSection
          title="System Credentials"
          queryKey={['credentials', 'system']}
          fetchFn={() => api.listSystemCredentials()}
          setFn={(name, value) => api.setSystemCredential(name, value)}
          deleteFn={(name) => api.deleteSystemCredential(name)}
        />
      )}
    </div>
  )
}
```

- [ ] **Step 2: Build to verify no TypeScript errors**

```bash
cd web && npm run build
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/CredentialsPage.tsx
git commit -m "feat: add CredentialsPage with team and system credential sections"
```

---

### Task 7: Add Settings nav item and route

**Files:**
- Modify: `web/src/components/Layout.tsx`
- Modify: `web/src/App.tsx`

- [ ] **Step 1: Add Settings to the nav in Layout.tsx**

In `web/src/components/Layout.tsx`, add `Settings` to the lucide-react import (line 6):

```typescript
import {
  Inbox, LayoutTemplate, FileText, Activity, BookOpen, Heart, Settings,
} from 'lucide-react'
```

Add Settings to `NAV_ITEMS` (after the System entry):

```typescript
const NAV_ITEMS = [
  { href: '/runs',      label: 'Runs',      icon: Activity },
  { href: '/workflows', label: 'Workflows', icon: LayoutTemplate },
  { href: '/inbox',     label: 'Inbox',     icon: Inbox },
  { href: '/reports',   label: 'Reports',   icon: FileText },
  { href: '/knowledge', label: 'Knowledge', icon: BookOpen },
  { href: '/system',    label: 'System',    icon: Heart },
  { href: '/settings',  label: 'Settings',  icon: Settings },
]
```

- [ ] **Step 2: Add the /settings route to App.tsx**

In `web/src/App.tsx`, add the import:

```typescript
import { CredentialsPage } from './pages/CredentialsPage'
```

Add the route inside the authenticated `<Routes>` (after the `/knowledge` route):

```tsx
<Route path="/settings" element={<CredentialsPage />} />
```

- [ ] **Step 3: Build to verify**

```bash
cd web && npm run build
```

Expected: no errors.

- [ ] **Step 4: Run backend tests**

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 5: Run linter**

```bash
make lint
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/Layout.tsx web/src/App.tsx
git commit -m "feat: add Settings nav item linking to credentials page"
```
