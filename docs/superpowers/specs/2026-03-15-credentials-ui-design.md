# Credentials UI Design

**Date:** 2026-03-15
**Status:** Approved

## Overview

Extend the web UI to let users view, add, and delete credentials. Credentials are scoped to a team. Platform admins additionally see and manage system-wide credentials (no team owner). Values are never displayed; only names and timestamps are exposed.

---

## Backend

### Schema Migration

Drop the existing `UNIQUE(team_id, name)` constraint and allow `team_id` to be NULL. Replace with two partial unique indexes:

```sql
ALTER TABLE credentials ALTER COLUMN team_id DROP NOT NULL;

DROP INDEX IF EXISTS credentials_team_id_name_key; -- or equivalent constraint name

CREATE UNIQUE INDEX credentials_team_name_unique
  ON credentials (team_id, name) WHERE team_id IS NOT NULL;

CREATE UNIQUE INDEX credentials_system_name_unique
  ON credentials (name) WHERE team_id IS NULL;
```

System-wide credentials are stored in the same `credentials` table with `team_id = NULL`.

**Important — upsert syntax change:** PostgreSQL's `ON CONFLICT (col, col)` requires an exact-match constraint or an index with the same column list _and_ predicate. After this migration, the existing team credential Set handler must change its upsert from:

```sql
INSERT ... ON CONFLICT (team_id, name) DO UPDATE ...
```

to the index-inference form:

```sql
INSERT ... ON CONFLICT (team_id, name) WHERE team_id IS NOT NULL DO UPDATE ...
```

The new system credential Set handler must use:

```sql
INSERT ... ON CONFLICT (name) WHERE team_id IS NULL DO UPDATE ...
```

### Model

`TeamID` on the `Credential` struct changes from `uuid.UUID` to `*uuid.UUID` (nullable pointer). No other model changes needed.

### New API Routes (System Credentials)

All system credential endpoints require `claims.PlatformAdmin == true`; return `403` otherwise. No `X-Team-ID` header is needed or used.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/system-credentials` | List system credential metadata (name, created_at, updated_at) |
| `POST` | `/api/system-credentials` | Upsert a system credential `{name, value}` → 204 |
| `DELETE` | `/api/system-credentials/{name}` | Delete a system credential → 204, 404 if not found |

Response shape for list is identical to team credentials: `[{name, created_at, updated_at}]`. Values are never returned.

### Existing Team Credential Routes

Unchanged. Still require `X-Team-ID` header or `?team_id=` param. Handlers filter by `team_id IS NOT NULL` to avoid accidentally touching system credentials.

---

## Frontend

### Navigation

Add a **Settings** nav item to the sidebar in `Layout.tsx`, positioned below the existing nav items and above the UserMenu at the bottom. Uses the `Settings` icon from lucide-react. Links to `/settings`.

### Routing

Add a lazy `CredentialsPage` component registered at `/settings` in the React Router config.

### Types (`web/src/api/types.ts`)

```typescript
export interface Credential {
  name: string;
  created_at: string;
  updated_at: string;
}
```

### API Client (`web/src/api/client.ts`)

The existing `post()` helper always calls `res.json()`, which throws on a 204 response. Update `post()` to mirror the 204 guard already present in `del()`:

```typescript
async function post<T = void>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, { ... })
  if (!res.ok) { ... }
  if (res.status === 204 || res.headers.get('content-length') === '0') {
    return undefined as T
  }
  return res.json()
}
```

Add six methods to the `api` object:

```typescript
listCredentials: () => get<Credential[]>('/credentials'),
setCredential: (name: string, value: string) => post('/credentials', { name, value }),
deleteCredential: (name: string) => del(`/credentials/${name}`),

listSystemCredentials: () => get<Credential[]>('/system-credentials'),
setSystemCredential: (name: string, value: string) => post('/system-credentials', { name, value }),
deleteSystemCredential: (name: string) => del(`/system-credentials/${name}`),
```

**Router registration:** The three new system credential routes must be registered in the chi router (alongside existing credential routes). The system credential handlers should be mounted under the same auth middleware group as team credential handlers.

### `CredentialsPage` Component

A single page at `/settings` with two stacked sections. Both sections share the same visual pattern.

#### Section Pattern

Each section (Team and System) renders:

1. **Header row** — section title on the left, "+ Add" button on the right
2. **Inline add form** — hidden by default; expands above the list when "+ Add" is clicked
   - Two inputs: Name (monospace, uppercase placeholder e.g. `GITHUB_TOKEN`) and Value (type=password)
   - Save and Cancel buttons; collapses on either action
   - Save calls the appropriate `setCredential` mutation; on success invalidates the query and collapses the form
3. **Credential list** — table rows, each showing:
   - Credential name in monospace
   - Relative timestamp ("updated 2d ago")
   - Delete button on the right
4. **Inline delete confirmation** — clicking Delete replaces that row's action area with "Are you sure? [Confirm] [Cancel]"; no modal overlay; Confirm calls the delete mutation
5. **Empty state** — when the list is empty, show a muted placeholder message: "No credentials yet. Click + Add to create one."

#### System Section Visibility

The system credentials section is only rendered when `currentUser.platform_admin === true`. The `listSystemCredentials` query is only enabled when the user is a platform admin (React Query `enabled` option).

#### State

- Add form: local `useState` — `{ isOpen: boolean; name: string; value: string }` per section
- Delete confirmation: local `useState` — `pendingDelete: string | null` (the credential name awaiting confirmation) per section
- Data fetching: React Query `useQuery` with standard stale time config
- Mutations: `useMutation` with `queryClient.invalidateQueries` on success

#### Error Handling

- All fetch errors display an inline error message (red alert box, consistent with existing pages)
- All mutation errors surface inline near the relevant form/button
- No silent failures — every promise has error handling

---

## Auth & Security

- System credential routes check `claims.PlatformAdmin`; non-admins receive `403`
- Team credential routes validate `team_id` against `claims.TeamRoles` as before
- Credential names validated server-side against `^[A-Z][A-Z0-9_]*$` and reserved name blocklist — **both** the team Set handler (existing) and the new system Set handler must apply this validation
- `UserProfile` type already includes `platform_admin: boolean`; no type change needed
- Values are AES-256-GCM encrypted at rest; never returned to clients

---

## Out of Scope

- Editing credential values in-place (use delete + re-add)
- Listing which workflows reference a credential
- Per-credential audit log
- Additional Settings sub-pages (nav item routes directly to Credentials; can be extended later)
