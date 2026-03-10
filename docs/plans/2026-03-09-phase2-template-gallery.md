# Phase 2 Template Gallery Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Complete Phase 2 (Frontend — Task Creation) by adding a standalone `/templates` gallery page and wiring `?template=<name>` pre-fill into `/create`.

**Architecture:** New `TemplatesPage.tsx` with search/filter/preview panel. `TaskCreate.tsx` reads `useSearchParams` on mount to pre-fill YAML. Layout gains a Templates nav link. No backend changes.

**Tech Stack:** React 19, react-router-dom v7, @tanstack/react-query, lucide-react, Tailwind CSS v4, TypeScript

---

### Task 1: Add Templates nav link to Layout

**Files:**
- Modify: `web/src/components/Layout.tsx`

**Step 1: Add the LayoutTemplate icon import and nav entry**

In `web/src/components/Layout.tsx`, change:

```tsx
import {
  LayoutDashboard, Inbox, List, ExternalLink, Plus,
} from 'lucide-react'
```

to:

```tsx
import {
  LayoutDashboard, Inbox, List, ExternalLink, Plus, LayoutTemplate,
} from 'lucide-react'
```

**Step 2: Add Templates to NAV_ITEMS**

Change:
```tsx
const NAV_ITEMS = [
  { href: '/',      label: 'Dashboard', icon: LayoutDashboard },
  { href: '/inbox', label: 'Inbox',     icon: Inbox },
  { href: '/tasks', label: 'Tasks',     icon: List },
]
```

to:
```tsx
const NAV_ITEMS = [
  { href: '/',          label: 'Dashboard', icon: LayoutDashboard },
  { href: '/inbox',     label: 'Inbox',     icon: Inbox },
  { href: '/tasks',     label: 'Tasks',     icon: List },
  { href: '/templates', label: 'Templates', icon: LayoutTemplate },
]
```

**Step 3: Fix isActive for /create and /templates**

The existing guard `if (href === '/tasks' && pathname === '/create') return false` already prevents `/create` from highlighting Tasks. No changes needed here — `/templates` will be highlighted when `pathname.startsWith('/templates')`.

**Step 4: Verify TypeScript compiles**

```bash
cd /Users/andrew/dev/code/projects/fleetlift-web/web && npm run build 2>&1 | tail -20
```
Expected: build succeeds, no TypeScript errors.

**Step 5: Commit**

```bash
cd /Users/andrew/dev/code/projects/fleetlift-web
git add web/src/components/Layout.tsx
git commit -m "feat(web): add Templates nav link to sidebar"
```

---

### Task 2: Create TemplatesPage.tsx

**Files:**
- Create: `web/src/pages/TemplatesPage.tsx`

**Step 1: Write the full component**

Create `web/src/pages/TemplatesPage.tsx`:

```tsx
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { Template } from '@/api/types'
import { FileCode, Search, Loader2, AlertCircle, ArrowRight } from 'lucide-react'
import { cn } from '@/lib/utils'

type ModeFilter = 'all' | 'transform' | 'report'

export function TemplatesPage() {
  const navigate = useNavigate()
  const [search, setSearch] = useState('')
  const [modeFilter, setModeFilter] = useState<ModeFilter>('all')
  const [selected, setSelected] = useState<Template | null>(null)
  const [loadingPreview, setLoadingPreview] = useState(false)
  const [previewError, setPreviewError] = useState<string | null>(null)

  const { data, isLoading, error } = useQuery({
    queryKey: ['templates'],
    queryFn: () => api.listTemplates(),
  })

  const templates = data?.templates ?? []

  const filtered = templates.filter((t) => {
    const matchesSearch =
      search === '' ||
      t.name.toLowerCase().includes(search.toLowerCase()) ||
      t.description.toLowerCase().includes(search.toLowerCase())
    const matchesMode =
      modeFilter === 'all' ||
      t.description.toLowerCase().includes(modeFilter)
    return matchesSearch && matchesMode
  })

  const handleSelect = async (t: Template) => {
    if (selected?.name === t.name) return
    setSelected(t)
    setPreviewError(null)
    if (!t.content) {
      setLoadingPreview(true)
      try {
        const full = await api.getTemplate(t.name)
        setSelected(full)
      } catch {
        setPreviewError('Failed to load template content.')
      } finally {
        setLoadingPreview(false)
      }
    }
  }

  const handleUse = () => {
    if (!selected) return
    navigate(`/create?template=${encodeURIComponent(selected.name)}`)
  }

  return (
    <div className="flex h-[calc(100vh-3rem)] gap-0 -mx-8 -my-6">
      {/* Left panel */}
      <div className="flex w-96 shrink-0 flex-col border-r">
        {/* Header */}
        <div className="border-b px-4 py-4">
          <h1 className="text-base font-semibold">Templates</h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            Start from a pre-built configuration
          </p>
        </div>

        {/* Search */}
        <div className="px-4 pt-3 pb-2">
          <div className="relative">
            <Search className="absolute left-2.5 top-2.5 h-3.5 w-3.5 text-muted-foreground" />
            <input
              type="text"
              placeholder="Search templates..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full rounded-md border bg-background pl-8 pr-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
            />
          </div>
        </div>

        {/* Mode filter */}
        <div className="flex gap-1 px-4 pb-3">
          {(['all', 'transform', 'report'] as ModeFilter[]).map((m) => (
            <button
              key={m}
              onClick={() => setModeFilter(m)}
              className={cn(
                'rounded-md px-2.5 py-1 text-xs font-medium capitalize transition-colors',
                modeFilter === m
                  ? 'bg-primary text-primary-foreground'
                  : 'text-muted-foreground hover:bg-muted',
              )}
            >
              {m}
            </button>
          ))}
        </div>

        {/* Template list */}
        <div className="flex-1 overflow-y-auto px-3 pb-3">
          {isLoading && (
            <div className="flex items-center gap-2 py-8 justify-center text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              <span className="text-sm">Loading templates...</span>
            </div>
          )}
          {error && (
            <div className="flex items-center gap-2 py-8 justify-center text-destructive text-sm">
              <AlertCircle className="h-4 w-4" />
              Failed to load templates
            </div>
          )}
          {!isLoading && filtered.length === 0 && (
            <div className="flex flex-col items-center py-12 text-muted-foreground">
              <FileCode className="h-8 w-8 mb-2 opacity-40" />
              <p className="text-sm">No templates match your search</p>
            </div>
          )}
          <div className="space-y-1">
            {filtered.map((t) => (
              <button
                key={t.name}
                onClick={() => handleSelect(t)}
                className={cn(
                  'w-full flex items-start gap-3 rounded-lg px-3 py-3 text-left transition-colors',
                  selected?.name === t.name
                    ? 'bg-accent'
                    : 'hover:bg-muted/50',
                )}
              >
                <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-muted mt-0.5">
                  <FileCode className="h-4 w-4 text-muted-foreground" />
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium truncate">
                      {formatName(t.name)}
                    </span>
                    <ModeBadge description={t.description} />
                  </div>
                  <p className="text-xs text-muted-foreground mt-0.5 line-clamp-2">
                    {t.description}
                  </p>
                </div>
              </button>
            ))}
          </div>
        </div>
      </div>

      {/* Right panel — preview */}
      <div className="flex flex-1 flex-col">
        {selected ? (
          <>
            <div className="flex items-center justify-between border-b px-6 py-4">
              <div>
                <h2 className="text-sm font-semibold">{formatName(selected.name)}</h2>
                <p className="text-xs text-muted-foreground mt-0.5">{selected.description}</p>
              </div>
              <button
                onClick={handleUse}
                className="flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-xs font-medium text-primary-foreground hover:bg-primary/90 transition-colors"
              >
                Use template
                <ArrowRight className="h-3.5 w-3.5" />
              </button>
            </div>
            <div className="flex-1 overflow-y-auto bg-muted/30 px-6 py-4">
              {loadingPreview && (
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  <span className="text-sm">Loading...</span>
                </div>
              )}
              {previewError && (
                <div className="flex items-center gap-2 text-destructive text-sm">
                  <AlertCircle className="h-4 w-4" />
                  {previewError}
                </div>
              )}
              {!loadingPreview && !previewError && selected.content && (
                <pre className="text-xs font-mono leading-relaxed whitespace-pre-wrap text-foreground">
                  {selected.content}
                </pre>
              )}
            </div>
          </>
        ) : (
          <div className="flex flex-1 flex-col items-center justify-center text-muted-foreground">
            <FileCode className="h-12 w-12 mb-3 opacity-30" />
            <p className="text-sm">Select a template to preview</p>
          </div>
        )}
      </div>
    </div>
  )
}

function formatName(name: string): string {
  return name.split('-').map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(' ')
}

function ModeBadge({ description }: { description: string }) {
  const lower = description.toLowerCase()
  if (lower.includes('report')) {
    return (
      <span className="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
        report
      </span>
    )
  }
  if (lower.includes('transform') || lower.includes('migrate') || lower.includes('upgrade') || lower.includes('add') || lower.includes('fix')) {
    return (
      <span className="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400">
        transform
      </span>
    )
  }
  return null
}
```

**Step 2: Verify TypeScript compiles**

```bash
cd /Users/andrew/dev/code/projects/fleetlift-web/web && npm run build 2>&1 | tail -20
```
Expected: build succeeds, no TypeScript errors.

**Step 3: Commit**

```bash
cd /Users/andrew/dev/code/projects/fleetlift-web
git add web/src/pages/TemplatesPage.tsx
git commit -m "feat(web): add standalone Templates gallery page"
```

---

### Task 3: Register /templates route in App.tsx

**Files:**
- Modify: `web/src/App.tsx`

**Step 1: Add the import and route**

Change:
```tsx
import { TaskCreatePage } from './pages/TaskCreate'
```
to:
```tsx
import { TaskCreatePage } from './pages/TaskCreate'
import { TemplatesPage } from './pages/TemplatesPage'
```

Add the route inside `<Routes>`:
```tsx
<Route path="/templates" element={<TemplatesPage />} />
```

Full updated `App.tsx`:
```tsx
import { Routes, Route } from 'react-router-dom'
import { Layout } from './components/Layout'
import { DashboardPage } from './pages/Dashboard'
import { InboxPage } from './pages/Inbox'
import { TaskListPage } from './pages/TaskList'
import { TaskDetailPage } from './pages/TaskDetail'
import { TaskCreatePage } from './pages/TaskCreate'
import { TemplatesPage } from './pages/TemplatesPage'

export default function App() {
  return (
    <Layout>
      <Routes>
        <Route path="/" element={<DashboardPage />} />
        <Route path="/inbox" element={<InboxPage />} />
        <Route path="/tasks" element={<TaskListPage />} />
        <Route path="/tasks/:id" element={<TaskDetailPage />} />
        <Route path="/create" element={<TaskCreatePage />} />
        <Route path="/templates" element={<TemplatesPage />} />
      </Routes>
    </Layout>
  )
}
```

**Step 2: Verify TypeScript compiles**

```bash
cd /Users/andrew/dev/code/projects/fleetlift-web/web && npm run build 2>&1 | tail -20
```
Expected: build succeeds.

**Step 3: Commit**

```bash
cd /Users/andrew/dev/code/projects/fleetlift-web
git add web/src/App.tsx
git commit -m "feat(web): register /templates route"
```

---

### Task 4: Update TaskCreate.tsx — pre-fill + "Browse all" link + mode badges

**Files:**
- Modify: `web/src/pages/TaskCreate.tsx`

**Step 1: Add useSearchParams for ?template pre-fill**

At the top of `TaskCreatePage`, add the import and effect:

Add to imports:
```tsx
import { useNavigate, useSearchParams } from 'react-router-dom'
```
(replace `import { useNavigate } from 'react-router-dom'`)

Add inside `TaskCreatePage`, after the existing `useQuery` call:
```tsx
const [searchParams] = useSearchParams()

useEffect(() => {
  const templateName = searchParams.get('template')
  if (!templateName) return
  api.getTemplate(templateName)
    .then((t) => {
      if (t.content) {
        setGeneratedYAML(t.content)
        setYamlWarning(null)
      }
    })
    .catch(() => {
      // silently ignore unknown template param
    })
// eslint-disable-next-line react-hooks/exhaustive-deps
}, []) // run once on mount
```

**Step 2: Add "Browse all templates →" link to TemplateGallery**

In the `TemplateGallery` function (in the same file), add a header link to `/templates`.

Change the `TemplateGallery` function header section from:
```tsx
<div className="mb-4">
  <h2 className="text-sm font-medium">Task Templates</h2>
  <p className="text-xs text-muted-foreground mt-0.5">
    Start from a pre-built template and customize for your repos
  </p>
</div>
```

to:
```tsx
<div className="mb-4 flex items-start justify-between">
  <div>
    <h2 className="text-sm font-medium">Task Templates</h2>
    <p className="text-xs text-muted-foreground mt-0.5">
      Start from a pre-built template and customize for your repos
    </p>
  </div>
  <Link to="/templates" className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors">
    Browse all
    <ChevronRight className="h-3 w-3" />
  </Link>
</div>
```

Add to the imports at the top of the file:
```tsx
import { Link } from 'react-router-dom'
```

**Step 3: Add mode badges to inline template cards**

In `TemplateGallery`, update each card to include a mode badge. Change the card content from:
```tsx
<div className="flex items-center gap-2">
  <span className="text-sm font-medium">{formatTemplateName(t.name)}</span>
  <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/50 group-hover:text-muted-foreground transition-colors" />
</div>
```

to:
```tsx
<div className="flex items-center gap-2">
  <span className="text-sm font-medium">{formatTemplateName(t.name)}</span>
  <InlineModeBadge description={t.description} />
  <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/50 group-hover:text-muted-foreground ml-auto transition-colors" />
</div>
```

Add this helper at the bottom of the file (alongside `formatTemplateName`):
```tsx
function InlineModeBadge({ description }: { description: string }) {
  const lower = description.toLowerCase()
  if (lower.includes('report')) {
    return (
      <span className="rounded px-1.5 py-0.5 text-[10px] font-medium bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
        report
      </span>
    )
  }
  if (lower.includes('transform') || lower.includes('migrate') || lower.includes('upgrade')) {
    return (
      <span className="rounded px-1.5 py-0.5 text-[10px] font-medium bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400">
        transform
      </span>
    )
  }
  return null
}
```

**Step 4: Verify TypeScript compiles and lint passes**

```bash
cd /Users/andrew/dev/code/projects/fleetlift-web/web && npm run build 2>&1 | tail -20
```
Expected: build succeeds, no TypeScript errors.

```bash
cd /Users/andrew/dev/code/projects/fleetlift-web/web && npm run lint 2>&1 | tail -20
```
Expected: no lint errors.

**Step 5: Commit**

```bash
cd /Users/andrew/dev/code/projects/fleetlift-web
git add web/src/pages/TaskCreate.tsx
git commit -m "feat(web): pre-fill YAML from ?template param + enhance inline gallery"
```

---

### Task 5: Manual Verification

Start the dev server:
```bash
cd /Users/andrew/dev/code/projects/fleetlift-web/web && npm run dev
```

Checklist:
- [ ] Sidebar shows "Templates" nav link
- [ ] `/templates` loads the gallery with left panel (cards) and empty right panel placeholder
- [ ] Clicking a template card shows YAML preview on the right
- [ ] "Use template" button navigates to `/create?template=<name>` with YAML pre-filled
- [ ] `/create` with `?template=nonexistent` loads normally (empty chat, no YAML panel)
- [ ] Inline template panel in `/create` shows "Browse all →" link to `/templates`
- [ ] Template cards in inline panel show mode badges
- [ ] Search in `/templates` filters by name and description
- [ ] Mode filter (all/transform/report) filters the card list

---

## Verification Checklist

- [ ] `npm run build` passes with no TypeScript errors
- [ ] `npm run lint` passes with no errors
- [ ] `/templates` route renders gallery page
- [ ] Selecting template shows YAML preview
- [ ] "Use template" pre-fills `/create`
- [ ] `?template=` param on `/create` pre-fills YAML on mount
- [ ] "Browse all →" link appears in inline template panel
- [ ] Mode badges appear on template cards
- [ ] Templates nav link is active when on `/templates`
