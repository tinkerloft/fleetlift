# Profile Menu Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a profile dropdown menu to the top-right of the app header showing current user info, team details, and a sign-out action.

**Architecture:** Add a persistent top header bar to `Layout.tsx` alongside the existing sidebar. Enrich `/api/me` to return user name/email + team names from the DB. Build a `UserMenu` dropdown component using Radix `DropdownMenu`. The header is designed to accept additional icon buttons (e.g. dark mode toggle) to its right-side slot in future.

**Tech Stack:** React 19, TypeScript, `@radix-ui/react-dropdown-menu` (new dep), lucide-react, existing shadcn-style ui primitives, Go/chi backend.

---

## Task 1: Install dropdown dependency & scaffold ui primitive

**Files:**
- Modify: `web/package.json` (via npm install)
- Create: `web/src/components/ui/dropdown-menu.tsx`

- [ ] **Step 1: Install Radix dropdown**

```bash
cd web && npm install @radix-ui/react-dropdown-menu
```

Expected: package added to `node_modules` and `package.json`.

- [ ] **Step 2: Create the ui primitive**

Create `web/src/components/ui/dropdown-menu.tsx`:

```tsx
import * as DropdownMenuPrimitive from '@radix-ui/react-dropdown-menu'
import { cn } from '@/lib/utils'

export const DropdownMenu = DropdownMenuPrimitive.Root
export const DropdownMenuTrigger = DropdownMenuPrimitive.Trigger
export const DropdownMenuPortal = DropdownMenuPrimitive.Portal

export function DropdownMenuContent({
  className,
  sideOffset = 4,
  ...props
}: React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Content>) {
  return (
    <DropdownMenuPrimitive.Portal>
      <DropdownMenuPrimitive.Content
        sideOffset={sideOffset}
        className={cn(
          'z-50 min-w-48 overflow-hidden rounded-md border bg-popover p-1 text-popover-foreground shadow-md',
          'data-[state=open]:animate-in data-[state=closed]:animate-out',
          'data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0',
          'data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95',
          'data-[side=bottom]:slide-in-from-top-2',
          className,
        )}
        {...props}
      />
    </DropdownMenuPrimitive.Portal>
  )
}

export function DropdownMenuItem({
  className,
  ...props
}: React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Item>) {
  return (
    <DropdownMenuPrimitive.Item
      className={cn(
        'relative flex cursor-default select-none items-center rounded-sm px-2 py-1.5 text-sm outline-none',
        'transition-colors focus:bg-accent focus:text-accent-foreground',
        'data-[disabled]:pointer-events-none data-[disabled]:opacity-50',
        className,
      )}
      {...props}
    />
  )
}

export function DropdownMenuSeparator({
  className,
  ...props
}: React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Separator>) {
  return (
    <DropdownMenuPrimitive.Separator
      className={cn('-mx-1 my-1 h-px bg-muted', className)}
      {...props}
    />
  )
}

export function DropdownMenuLabel({
  className,
  ...props
}: React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Label>) {
  return (
    <DropdownMenuPrimitive.Label
      className={cn('px-2 py-1.5 text-xs font-medium text-muted-foreground', className)}
      {...props}
    />
  )
}
```

- [ ] **Step 3: Verify build**

```bash
cd web && npm run build 2>&1 | tail -5
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/package.json web/package-lock.json web/src/components/ui/dropdown-menu.tsx
git commit -m "feat: add DropdownMenu ui primitive (Radix)"
```

---

## Task 2: Enrich /api/me backend endpoint

**Files:**
- Modify: `internal/server/handlers/auth.go` — `HandleMe` to join users + teams from DB
- Modify: `web/src/api/client.ts` — add `getMe()` function
- Modify: `web/src/api/types.ts` — add `MeResponse` type

- [ ] **Step 1: Update `HandleMe` to return enriched user + team info**

In `internal/server/handlers/auth.go`, replace `HandleMe`:

```go
// HandleMe returns the authenticated user's identity including name, email, and team details.
func (h *AuthHandler) HandleMe(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Fetch user name + email from DB
	var user struct {
		Name  string  `db:"name"`
		Email *string `db:"email"`
	}
	if err := h.db.GetContext(r.Context(), &user,
		`SELECT name, email FROM users WHERE id = $1`, claims.UserID,
	); err != nil {
		http.Error(w, "user not found", http.StatusInternalServerError)
		return
	}

	// Fetch team names for the teams in this JWT
	type teamInfo struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Role string `json:"role"`
	}
	var teams []teamInfo
	if len(claims.TeamRoles) > 0 {
		ids := make([]string, 0, len(claims.TeamRoles))
		for id := range claims.TeamRoles {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		rows, err := h.db.QueryContext(r.Context(),
			`SELECT id, name FROM teams WHERE id = ANY($1)`, pq.Array(ids))
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var t teamInfo
				if rows.Scan(&t.ID, &t.Name) == nil {
					t.Role = claims.TeamRoles[t.ID]
					teams = append(teams, t)
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":        claims.UserID,
		"name":           user.Name,
		"email":          user.Email,
		"platform_admin": claims.PlatformAdmin,
		"teams":          teams,
	})
}
```

Add required imports to the file if not present: `"sort"`, `"github.com/lib/pq"`.

- [ ] **Step 2: Build to verify**

```bash
go build ./internal/server/... 2>&1
```

Expected: no errors.

- [ ] **Step 3: Add `MeResponse` type to `web/src/api/types.ts`**

Append to `web/src/api/types.ts`:

```ts
export interface TeamInfo {
  id: string
  name: string
  role: string
}

export interface MeResponse {
  user_id: string
  name: string
  email?: string
  platform_admin: boolean
  teams: TeamInfo[]
}
```

- [ ] **Step 4: Add `getMe()` to `web/src/api/client.ts`**

Add to the `api` object:

```ts
getMe: () => get<MeResponse>('/me'),
```

And add the import at the top:
```ts
import type { ..., MeResponse } from './types'
```

- [ ] **Step 5: Commit**

```bash
git add internal/server/handlers/auth.go web/src/api/types.ts web/src/api/client.ts
git commit -m "feat: enrich /api/me with user name, email, and team details"
```

---

## Task 3: Build UserMenu component

**Files:**
- Create: `web/src/components/UserMenu.tsx`

- [ ] **Step 1: Create `UserMenu.tsx`**

Create `web/src/components/UserMenu.tsx`:

```tsx
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { LogOut, User } from 'lucide-react'
import { api } from '@/api/client'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'

export function UserMenu() {
  const navigate = useNavigate()
  const { data: me } = useQuery({
    queryKey: ['me'],
    queryFn: () => api.getMe(),
    staleTime: 5 * 60 * 1000,
  })

  const initials = me?.name
    ? me.name.split(' ').map((p) => p[0]).join('').toUpperCase().slice(0, 2)
    : '?'

  function handleSignOut() {
    localStorage.removeItem('token')
    navigate('/login', { replace: true })
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          className="flex h-8 w-8 items-center justify-center rounded-full bg-muted text-sm font-semibold text-foreground hover:bg-muted/80 focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          aria-label="User menu"
        >
          {initials}
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        {/* User identity */}
        <div className="px-2 py-2">
          <p className="text-sm font-medium leading-none">{me?.name ?? '—'}</p>
          {me?.email && (
            <p className="mt-1 text-xs text-muted-foreground truncate">{me.email}</p>
          )}
        </div>
        <DropdownMenuSeparator />

        {/* Teams */}
        {me?.teams && me.teams.length > 0 && (
          <>
            <DropdownMenuLabel>Teams</DropdownMenuLabel>
            {me.teams.map((team) => (
              <div key={team.id} className="flex items-center justify-between px-2 py-1.5">
                <span className="text-sm">{team.name}</span>
                <span className="text-xs text-muted-foreground capitalize">{team.role}</span>
              </div>
            ))}
            <DropdownMenuSeparator />
          </>
        )}

        {/* Sign out */}
        <DropdownMenuItem
          className="text-destructive focus:text-destructive cursor-pointer"
          onSelect={handleSignOut}
        >
          <LogOut className="mr-2 h-4 w-4" />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
```

- [ ] **Step 2: Verify types compile**

```bash
cd web && npx tsc --noEmit 2>&1 | head -20
```

Expected: no errors (or only pre-existing ones).

- [ ] **Step 3: Commit**

```bash
git add web/src/components/UserMenu.tsx
git commit -m "feat: add UserMenu dropdown component"
```

---

## Task 4: Add header bar to Layout

**Files:**
- Modify: `web/src/components/Layout.tsx`

The header sits above the main content area (not spanning the sidebar). It has a right-aligned slot for icon buttons — `UserMenu` goes there first, leaving space for future additions (dark mode toggle etc.).

- [ ] **Step 1: Update `Layout.tsx`**

Replace the `{/* Main content */}` section with a header + content structure:

```tsx
{/* Main content */}
<div className="flex flex-1 flex-col min-w-0">
  {/* Top header */}
  <header className="sticky top-0 z-10 flex h-14 items-center justify-end border-b bg-background px-6 gap-2">
    <UserMenu />
  </header>
  <main className="flex-1 px-8 py-6">
    {children}
  </main>
</div>
```

Add import at top of `Layout.tsx`:
```tsx
import { UserMenu } from '@/components/UserMenu'
```

- [ ] **Step 2: Build and spot-check in browser**

```bash
cd web && npm run build 2>&1 | tail -5
```

Then restart server and verify the header appears with avatar initials, dropdown opens, shows name + team + sign out.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Layout.tsx
git commit -m "feat: add header bar with UserMenu to Layout"
```

---

## Task 5: Full build + lint verification

- [ ] **Step 1: Go build + lint**

```bash
go build ./... && make lint 2>&1 | tail -20
```

Expected: no errors.

- [ ] **Step 2: Frontend build**

```bash
cd web && npm run build 2>&1 | tail -10
```

Expected: no errors.

- [ ] **Step 3: Run tests**

```bash
go test ./internal/server/... 2>&1
```

Expected: all pass.

- [ ] **Step 4: Final restart and smoke test**

```bash
./scripts/restart.sh
```

Verify in browser:
- Header appears on all authenticated pages
- Avatar shows correct initials
- Dropdown opens showing name, email, team name + role
- Sign out clears token and redirects to `/login`
