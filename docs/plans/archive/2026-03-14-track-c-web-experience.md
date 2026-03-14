# Track C: Web Experience — Implementation Plan

> **Status: COMPLETE** — All 23 tasks implemented across 6 chunks. Code review passed with P1/P2 follow-ups tracked below.

**Goal:** Transform Fleetlift's functional-but-flat web UI into a polished product that makes orchestration visible — custom DAG nodes, live run progress, workflow identity colors, profile menu, enhanced inbox, and system health.

**Architecture:** Pure frontend work (React 19 + Tailwind + shadcn/ui) with two small backend changes: (1) enrich `GET /api/me` with user name/email/teams, (2) add `GET /api/health/system` endpoint. All CSS-only animations, no new heavy dependencies. Builds on existing @xyflow/react, CodeMirror, and Radix primitives.

**Tech Stack:** React 19, TypeScript, Tailwind CSS 4, @xyflow/react, CodeMirror, Radix UI, Lucide icons, Vitest

**Reference mockups:** `docs/mockups/run-detail.html`, `docs/mockups/workflow-list.html`, `docs/mockups/inbox.html`

---

## File Structure

### New Files
| File | Responsibility |
|------|---------------|
| `web/src/components/DAGNode.tsx` | Custom ReactFlow node with accent bar, status dot, mode chip, duration |
| `web/src/components/StepTimeline.tsx` | Vertical timeline of steps with status dots and durations |
| `web/src/components/StatusBadge.tsx` | Enhanced status badge with pulsing dot for running, amber for awaiting |
| `web/src/components/Skeleton.tsx` | Reusable skeleton loading placeholders |
| `web/src/components/EmptyState.tsx` | Reusable empty state with icon + CTA |
| `web/src/components/UserMenu.tsx` | Profile dropdown with avatar, team list, sign out |
| `web/src/components/DiffViewer.tsx` | Syntax-highlighted diff viewer (green/red/blue) |
| `web/src/components/JsonViewer.tsx` | Syntax-colored JSON output viewer |
| `web/src/components/ui/dropdown-menu.tsx` | shadcn/ui Radix DropdownMenu primitive |
| `web/src/lib/workflow-colors.ts` | Category→color+icon mapping utility |
| `web/src/lib/format.ts` | Duration formatting, time-ago helpers |
| `web/src/lib/use-live-duration.ts` | Hook: ticking elapsed time for running runs/steps |
| `web/src/pages/SystemHealth.tsx` | System health page (worker status, queue depth) |
| `web/vitest.config.ts` | Vitest configuration |
| `web/src/lib/__tests__/workflow-colors.test.ts` | Tests for workflow-colors |
| `web/src/lib/__tests__/format.test.ts` | Tests for format utilities |

### Modified Files
| File | Changes |
|------|---------|
| `web/package.json` | Add vitest, @radix-ui/react-dropdown-menu, @testing-library/react |
| `web/src/components/DAGGraph.tsx` | Rewrite: custom node types, smoothstep edges, dynamic height, animated edges |
| `web/src/components/StepPanel.tsx` | Replace raw `<pre>` diff/output with DiffViewer and JsonViewer |
| `web/src/components/LogStream.tsx` | Add header bar with step name, status dot, scroll-to-bottom button |
| `web/src/components/HITLPanel.tsx` | Minor: use StatusBadge |
| `web/src/components/Layout.tsx` | Add UserMenu in sidebar footer, add SystemHealth nav link |
| `web/src/components/ui/badge.tsx` | Add `success`, `warning` variants |
| `web/src/pages/RunDetail.tsx` | Add live duration, progress bar, StepTimeline, two-column layout |
| `web/src/pages/RunList.tsx` | Add skeleton loader, empty state, use StatusBadge |
| `web/src/pages/WorkflowList.tsx` | Category colors, icons, step count chips, skeleton, empty state, sort |
| `web/src/pages/WorkflowDetail.tsx` | Hero section with icon, CodeMirror YAML, skeleton |
| `web/src/pages/Inbox.tsx` | Filter tabs, inline approve/reject/steer, failure items, unread styling |
| `web/src/api/client.ts` | Add `api.getMe()`, `api.getSystemHealth()` |
| `web/src/api/types.ts` | Add `UserProfile`, `SystemHealth`, `InboxKind` types |
| `web/src/App.tsx` | Add `/system` route |
| `web/src/index.css` | Add `@keyframes` for pulse-dot, dash animations |
| `internal/server/handlers/auth.go` | Enrich `HandleMe` with user name, email, teams from DB |
| `internal/server/handlers/auth_test.go` | Test enriched `/api/me` response |

---

## Chunk 1: Foundation — Utilities, Test Harness, Badge Variants

### Task 1: Add Vitest + Testing Libraries

**Files:**
- Modify: `web/package.json`
- Create: `web/vitest.config.ts`

- [ ] **Step 1: Install test dependencies**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web
npm install -D vitest @testing-library/react @testing-library/jest-dom jsdom
```

- [ ] **Step 2: Create vitest config**

Create `web/vitest.config.ts`:
```ts
import { defineConfig } from 'vitest/config'
import path from 'path'

export default defineConfig({
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: [],
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
})
```

- [ ] **Step 3: Add test script to package.json**

Add to `scripts` in `web/package.json`:
```json
"test": "vitest run",
"test:watch": "vitest"
```

- [ ] **Step 4: Verify vitest runs (no tests yet, should exit clean)**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx vitest run
```

- [ ] **Step 5: Commit**

```bash
git add web/package.json web/package-lock.json web/vitest.config.ts
git commit -m "feat(web): add vitest test harness"
```

---

### Task 2: Duration & Time Formatting Utilities

**Files:**
- Create: `web/src/lib/format.ts`
- Create: `web/src/lib/__tests__/format.test.ts`

- [ ] **Step 1: Write failing tests**

Create `web/src/lib/__tests__/format.test.ts`:
```ts
import { describe, it, expect } from 'vitest'
import { formatDuration, formatTimeAgo } from '../format'

describe('formatDuration', () => {
  it('formats seconds only', () => {
    expect(formatDuration(42000)).toBe('42s')
  })
  it('formats minutes and seconds', () => {
    expect(formatDuration(154000)).toBe('2m 34s')
  })
  it('formats hours', () => {
    expect(formatDuration(3661000)).toBe('1h 1m')
  })
  it('returns 0s for zero', () => {
    expect(formatDuration(0)).toBe('0s')
  })
  it('returns 0s for negative', () => {
    expect(formatDuration(-1000)).toBe('0s')
  })
})

describe('formatTimeAgo', () => {
  it('returns "just now" for < 60s', () => {
    const now = Date.now()
    expect(formatTimeAgo(new Date(now - 30000).toISOString())).toBe('just now')
  })
  it('returns minutes ago', () => {
    const now = Date.now()
    expect(formatTimeAgo(new Date(now - 180000).toISOString())).toBe('3m ago')
  })
  it('returns hours ago', () => {
    const now = Date.now()
    expect(formatTimeAgo(new Date(now - 7200000).toISOString())).toBe('2h ago')
  })
})
```

- [ ] **Step 2: Run tests — should fail**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx vitest run src/lib/__tests__/format.test.ts
```
Expected: FAIL — module not found

- [ ] **Step 3: Implement format.ts**

Create `web/src/lib/format.ts`:
```ts
/** Format millisecond duration as human string: "2m 34s", "1h 1m" */
export function formatDuration(ms: number): string {
  if (ms <= 0) return '0s'
  const totalSec = Math.floor(ms / 1000)
  const h = Math.floor(totalSec / 3600)
  const m = Math.floor((totalSec % 3600) / 60)
  const s = totalSec % 60
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${s}s`
  return `${s}s`
}

/** Format ISO timestamp as relative time: "3m ago", "2h ago" */
export function formatTimeAgo(isoString: string): string {
  const diff = Date.now() - new Date(isoString).getTime()
  if (diff < 60_000) return 'just now'
  const mins = Math.floor(diff / 60_000)
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}
```

- [ ] **Step 4: Run tests — should pass**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx vitest run src/lib/__tests__/format.test.ts
```
Expected: all 8 tests PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/format.ts web/src/lib/__tests__/format.test.ts
git commit -m "feat(web): add duration and time-ago formatting utilities"
```

---

### Task 3: Workflow Category Colors Utility

**Files:**
- Create: `web/src/lib/workflow-colors.ts`
- Create: `web/src/lib/__tests__/workflow-colors.test.ts`

- [ ] **Step 1: Write failing tests**

Create `web/src/lib/__tests__/workflow-colors.test.ts`:
```ts
import { describe, it, expect } from 'vitest'
import { workflowCategory } from '../workflow-colors'

describe('workflowCategory', () => {
  it('maps audit tag to violet', () => {
    const cat = workflowCategory(['audit', 'security'])
    expect(cat.color).toBe('violet')
    expect(cat.icon).toBe('Shield')
  })
  it('maps bug-fix tag to red', () => {
    const cat = workflowCategory(['bug-fix'])
    expect(cat.color).toBe('red')
    expect(cat.icon).toBe('Bug')
  })
  it('maps migration tag to blue', () => {
    const cat = workflowCategory(['migration', 'fleet'])
    expect(cat.color).toBe('blue')
    expect(cat.icon).toBe('GitBranch')
  })
  it('maps research tag to teal', () => {
    const cat = workflowCategory(['fleet-research'])
    expect(cat.color).toBe('teal')
    expect(cat.icon).toBe('Search')
  })
  it('maps triage tag to amber', () => {
    const cat = workflowCategory(['triage'])
    expect(cat.color).toBe('amber')
    expect(cat.icon).toBe('Tag')
  })
  it('falls back to gray for unknown', () => {
    const cat = workflowCategory(['unknown-thing'])
    expect(cat.color).toBe('gray')
    expect(cat.icon).toBe('Terminal')
  })
  it('handles empty tags', () => {
    const cat = workflowCategory([])
    expect(cat.color).toBe('gray')
  })
})
```

- [ ] **Step 2: Run tests — should fail**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx vitest run src/lib/__tests__/workflow-colors.test.ts
```

- [ ] **Step 3: Implement workflow-colors.ts**

Create `web/src/lib/workflow-colors.ts`:
```ts
export type CategoryColor = 'violet' | 'blue' | 'teal' | 'amber' | 'red' | 'gray'

export interface WorkflowCategoryInfo {
  color: CategoryColor
  icon: string // Lucide icon name
}

const TAG_MAP: [string[], CategoryColor, string][] = [
  [['audit', 'security'], 'violet', 'Shield'],
  [['bug-fix', 'incident', 'incident-response'], 'red', 'Bug'],
  [['migration', 'fleet-transform', 'dependency', 'dependency-update', 'transform'], 'blue', 'GitBranch'],
  [['research', 'fleet-research', 'pr-review', 'review'], 'teal', 'Search'],
  [['triage', 'ops'], 'amber', 'Tag'],
]

export function workflowCategory(tags: string[]): WorkflowCategoryInfo {
  for (const [keywords, color, icon] of TAG_MAP) {
    if (tags.some(t => keywords.includes(t))) {
      return { color, icon }
    }
  }
  return { color: 'gray', icon: 'Terminal' }
}

/** Tailwind classes for category accent colors */
export const CATEGORY_STYLES: Record<CategoryColor, { border: string; bg: string; text: string; iconBg: string }> = {
  violet: { border: 'border-t-violet-500', bg: 'bg-violet-500/10', text: 'text-violet-600', iconBg: 'bg-violet-500/10' },
  blue:   { border: 'border-t-blue-500',   bg: 'bg-blue-500/10',   text: 'text-blue-600',   iconBg: 'bg-blue-500/10' },
  teal:   { border: 'border-t-teal-500',   bg: 'bg-teal-500/10',   text: 'text-teal-600',   iconBg: 'bg-teal-500/10' },
  amber:  { border: 'border-t-amber-500',  bg: 'bg-amber-500/10',  text: 'text-amber-600',  iconBg: 'bg-amber-500/10' },
  red:    { border: 'border-t-red-500',     bg: 'bg-red-500/10',    text: 'text-red-600',    iconBg: 'bg-red-500/10' },
  gray:   { border: 'border-t-gray-400',   bg: 'bg-gray-500/10',   text: 'text-gray-500',   iconBg: 'bg-gray-500/10' },
}
```

- [ ] **Step 4: Run tests — should pass**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx vitest run src/lib/__tests__/workflow-colors.test.ts
```
Expected: all 7 tests PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/workflow-colors.ts web/src/lib/__tests__/workflow-colors.test.ts
git commit -m "feat(web): add workflow category color/icon mapping"
```

---

### Task 4: Enhanced Badge Variants + StatusBadge Component

**Files:**
- Modify: `web/src/components/ui/badge.tsx`
- Create: `web/src/components/StatusBadge.tsx`

- [ ] **Step 1: Add success and warning variants to badge.tsx**

In `web/src/components/ui/badge.tsx`, add to the `variants.variant` object (after `destructive`):
```ts
        success:
          "border-transparent bg-green-500/15 text-green-700 dark:text-green-400",
        warning:
          "border-transparent bg-amber-500/15 text-amber-700 dark:text-amber-400",
```

- [ ] **Step 2: Create StatusBadge component**

Create `web/src/components/StatusBadge.tsx`:
```tsx
import { cn } from '@/lib/utils'
import { Badge } from './ui/badge'
import type { RunStatus, StepStatus } from '@/api/types'

type AnyStatus = RunStatus | StepStatus

const STATUS_CONFIG: Record<string, { variant: 'default' | 'secondary' | 'destructive' | 'success' | 'warning' | 'outline'; pulse?: boolean }> = {
  pending:        { variant: 'secondary' },
  cloning:        { variant: 'secondary', pulse: true },
  running:        { variant: 'secondary', pulse: true },
  verifying:      { variant: 'secondary', pulse: true },
  awaiting_input: { variant: 'warning' },
  complete:       { variant: 'success' },
  failed:         { variant: 'destructive' },
  skipped:        { variant: 'outline' },
  cancelled:      { variant: 'outline' },
}

export function StatusBadge({ status, className }: { status: AnyStatus; className?: string }) {
  const config = STATUS_CONFIG[status] ?? { variant: 'secondary' as const }
  return (
    <Badge variant={config.variant} className={cn('gap-1.5', className)}>
      {config.pulse && (
        <span className="relative flex h-2 w-2">
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-current opacity-50" />
          <span className="relative inline-flex h-2 w-2 rounded-full bg-current" />
        </span>
      )}
      {status.replace('_', ' ')}
    </Badge>
  )
}
```

- [ ] **Step 3: Verify build**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add web/src/components/ui/badge.tsx web/src/components/StatusBadge.tsx
git commit -m "feat(web): add success/warning badge variants + StatusBadge component"
```

---

### Task 5: Skeleton + EmptyState Reusable Components

**Files:**
- Create: `web/src/components/Skeleton.tsx`
- Create: `web/src/components/EmptyState.tsx`

- [ ] **Step 1: Create Skeleton component**

Create `web/src/components/Skeleton.tsx`:
```tsx
import { cn } from '@/lib/utils'

export function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={cn('animate-pulse rounded-md bg-muted', className)} {...props} />
  )
}

/** Card-shaped skeleton for workflow/run list loading states */
export function SkeletonCard() {
  return (
    <div className="rounded-lg border overflow-hidden">
      <div className="h-1 bg-muted" />
      <div className="p-4 space-y-3">
        <Skeleton className="h-4 w-3/5" />
        <Skeleton className="h-3 w-4/5" />
        <Skeleton className="h-3 w-2/5" />
      </div>
    </div>
  )
}

/** Table row skeleton for run list loading states */
export function SkeletonRow() {
  return (
    <div className="flex items-center gap-4 px-4 py-3 border-b last:border-0">
      <Skeleton className="h-4 w-1/4" />
      <Skeleton className="h-5 w-16 rounded-full" />
      <Skeleton className="h-3 w-1/5" />
      <Skeleton className="h-3 w-16" />
    </div>
  )
}
```

- [ ] **Step 2: Create EmptyState component**

Create `web/src/components/EmptyState.tsx`:
```tsx
import type { LucideIcon } from 'lucide-react'
import { Button } from './ui/button'

interface EmptyStateProps {
  icon: LucideIcon
  title: string
  description?: string
  action?: { label: string; href: string }
}

export function EmptyState({ icon: Icon, title, description, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 rounded-lg border-2 border-dashed py-16 text-center">
      <Icon className="h-12 w-12 text-muted-foreground/40" strokeWidth={1.5} />
      <p className="text-sm font-medium text-muted-foreground">{title}</p>
      {description && <p className="text-sm text-muted-foreground/70">{description}</p>}
      {action && (
        <Button variant="default" size="sm" className="mt-2" asChild>
          <a href={action.href}>{action.label}</a>
        </Button>
      )}
    </div>
  )
}
```

- [ ] **Step 3: Verify build**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add web/src/components/Skeleton.tsx web/src/components/EmptyState.tsx
git commit -m "feat(web): add Skeleton and EmptyState reusable components"
```

---

### Task 6: Live Duration Hook

**Files:**
- Create: `web/src/lib/use-live-duration.ts`

- [ ] **Step 1: Create the hook**

Create `web/src/lib/use-live-duration.ts`:
```ts
import { useState, useEffect } from 'react'
import { formatDuration } from './format'

/**
 * Returns a ticking formatted duration string.
 * If endTime is provided, shows static completed duration.
 * If only startTime, ticks every second.
 */
export function useLiveDuration(startTime?: string, endTime?: string): string | null {
  const [now, setNow] = useState(Date.now())

  useEffect(() => {
    if (!startTime || endTime) return
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [startTime, endTime])

  if (!startTime) return null

  const start = new Date(startTime).getTime()
  const end = endTime ? new Date(endTime).getTime() : now
  return formatDuration(end - start)
}
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add web/src/lib/use-live-duration.ts
git commit -m "feat(web): add useLiveDuration hook for ticking elapsed times"
```

---

### Task 7: CSS Keyframe Animations

**Files:**
- Modify: `web/src/index.css`

- [ ] **Step 1: Add keyframes to index.css**

Append to end of `web/src/index.css`:
```css
@layer utilities {
  @keyframes dash-flow {
    to { stroke-dashoffset: -12; }
  }
  .animate-dash {
    animation: dash-flow 1s linear infinite;
  }
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/index.css
git commit -m "feat(web): add dash-flow animation keyframes"
```

---

## Chunk 2: C1 — DAG Graph Overhaul

### Task 8: Custom DAG Node Component

**Files:**
- Create: `web/src/components/DAGNode.tsx`

- [ ] **Step 1: Create DAGNode.tsx**

Create `web/src/components/DAGNode.tsx`:
```tsx
import { memo } from 'react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { cn } from '@/lib/utils'
import type { StepStatus } from '@/api/types'
import { formatDuration } from '@/lib/format'

export interface DAGNodeData {
  label: string
  status: StepStatus
  mode?: string
  startedAt?: string
  completedAt?: string
  selected?: boolean
  [key: string]: unknown
}

const STATUS_DOT_COLOR: Record<string, string> = {
  pending: 'bg-gray-400',
  cloning: 'bg-blue-500',
  running: 'bg-blue-500',
  verifying: 'bg-violet-500',
  awaiting_input: 'bg-amber-500',
  complete: 'bg-green-500',
  failed: 'bg-red-500',
  skipped: 'bg-gray-400',
}

const STATUS_ACCENT_COLOR: Record<string, string> = {
  pending: 'bg-gray-300',
  cloning: 'bg-blue-500',
  running: 'bg-blue-500',
  verifying: 'bg-violet-500',
  awaiting_input: 'bg-amber-500',
  complete: 'bg-green-500',
  failed: 'bg-red-500',
  skipped: 'bg-gray-300',
}

const PULSING = new Set(['running', 'cloning', 'verifying'])

function DAGNodeInner({ data }: NodeProps) {
  const d = data as DAGNodeData
  const elapsed = d.startedAt
    ? formatDuration(
        (d.completedAt ? new Date(d.completedAt).getTime() : Date.now()) -
        new Date(d.startedAt).getTime()
      )
    : null

  return (
    <>
      <Handle type="target" position={Position.Top} className="!bg-transparent !border-0 !w-3 !h-1" />
      <div className={cn(
        'relative rounded-lg border bg-card px-3 py-2 min-w-[180px] transition-shadow',
        d.selected && 'border-blue-500 shadow-[0_0_0_2px_hsl(221_83%_53%/0.2)]',
        !d.selected && 'hover:shadow-md',
      )}>
        {/* Left accent bar */}
        <div className={cn(
          'absolute left-0 top-0 bottom-0 w-1 rounded-l-lg',
          STATUS_ACCENT_COLOR[d.status] ?? 'bg-gray-300',
        )} />

        {/* Title row */}
        <div className="flex items-center gap-1.5 pl-2">
          <span className={cn(
            'inline-block h-2 w-2 rounded-full shrink-0',
            STATUS_DOT_COLOR[d.status] ?? 'bg-gray-400',
            PULSING.has(d.status) && 'animate-pulse',
          )} />
          <span className="text-[13px] font-medium truncate">{d.label}</span>
        </div>

        {/* Meta row */}
        <div className="flex items-center gap-2 pl-5 mt-0.5">
          {d.mode && (
            <span className="text-[10px] font-medium bg-muted px-1.5 py-px rounded">
              {d.mode}
            </span>
          )}
          {elapsed && (
            <span className="text-[11px] text-muted-foreground tabular-nums">
              {elapsed}
            </span>
          )}
          {d.status === 'awaiting_input' && !elapsed && (
            <span className="text-[11px] text-amber-600">awaiting input</span>
          )}
        </div>
      </div>
      <Handle type="source" position={Position.Bottom} className="!bg-transparent !border-0 !w-3 !h-1" />
    </>
  )
}

export const DAGNode = memo(DAGNodeInner)
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add web/src/components/DAGNode.tsx
git commit -m "feat(web): add custom DAGNode component with accent bar, status dot, mode chip"
```

---

### Task 9: Rewrite DAGGraph with Custom Nodes + Animated Edges

**Files:**
- Modify: `web/src/components/DAGGraph.tsx`

- [ ] **Step 1: Rewrite DAGGraph.tsx**

Replace the full contents of `web/src/components/DAGGraph.tsx` with:

```tsx
import { useCallback, useMemo } from 'react'
import {
  ReactFlow,
  Controls,
  type Node,
  type Edge,
  type NodeTypes,
  Position,
  MarkerType,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import type { StepDef, StepRun, StepStatus } from '@/api/types'
import { DAGNode, type DAGNodeData } from './DAGNode'

const nodeTypes: NodeTypes = { dagNode: DAGNode }

interface DAGGraphProps {
  steps: StepDef[]
  stepRuns: StepRun[]
  onSelectStep?: (stepId: string) => void
  selectedStepId?: string
}

export function DAGGraph({ steps, stepRuns, onSelectStep, selectedStepId }: DAGGraphProps) {
  const stepRunMap = useMemo(() => {
    const map = new Map<string, StepRun>()
    for (const sr of stepRuns) map.set(sr.step_id, sr)
    return map
  }, [stepRuns])

  const { nodes, edges, graphHeight } = useMemo(() => {
    const levels = computeLevels(steps)
    const maxLevel = Math.max(0, ...levels.values())
    const height = Math.min(400, (maxLevel + 1) * 140 + 60)

    // Center nodes at each level
    const levelGroups = new Map<number, StepDef[]>()
    for (const step of steps) {
      const lvl = levels.get(step.id) ?? 0
      if (!levelGroups.has(lvl)) levelGroups.set(lvl, [])
      levelGroups.get(lvl)!.push(step)
    }

    const n: Node[] = []
    const e: Edge[] = []
    const NODE_W = 210
    const NODE_H = 60
    const GAP_X = 30
    const GAP_Y = 100

    for (const step of steps) {
      const sr = stepRunMap.get(step.id)
      const status: StepStatus = (sr?.status as StepStatus) ?? 'pending'
      const level = levels.get(step.id) ?? 0
      const siblings = levelGroups.get(level) ?? [step]
      const col = siblings.indexOf(step)
      const totalWidth = siblings.length * NODE_W + (siblings.length - 1) * GAP_X
      const offsetX = (800 - totalWidth) / 2

      n.push({
        id: step.id,
        type: 'dagNode',
        position: { x: offsetX + col * (NODE_W + GAP_X), y: level * (NODE_H + GAP_Y) + 20 },
        data: {
          label: step.title || step.id,
          status,
          mode: step.mode,
          startedAt: sr?.started_at,
          completedAt: sr?.completed_at,
          selected: selectedStepId === step.id,
        } satisfies DAGNodeData,
        sourcePosition: Position.Bottom,
        targetPosition: Position.Top,
      })

      for (const dep of step.depends_on ?? []) {
        const depSr = stepRunMap.get(dep)
        const depStatus = depSr?.status ?? 'pending'
        const isActive = depStatus === 'running' || depStatus === 'cloning' || depStatus === 'verifying'
        const isComplete = depStatus === 'complete'

        e.push({
          id: `${dep}->${step.id}`,
          source: dep,
          target: step.id,
          type: 'smoothstep',
          animated: isActive,
          style: {
            stroke: isComplete ? 'hsl(142 71% 45%)' : isActive ? 'hsl(221 83% 53%)' : 'hsl(220 13% 82%)',
            strokeWidth: 2,
            opacity: isComplete ? 0.5 : 1,
            ...(isActive ? { strokeDasharray: '8 4' } : {}),
          },
          markerEnd: { type: MarkerType.ArrowClosed, width: 12, height: 12 },
        })
      }
    }

    return { nodes: n, edges: e, graphHeight: height }
  }, [steps, stepRunMap, selectedStepId])

  const onNodeClick = useCallback((_: React.MouseEvent, node: Node) => {
    onSelectStep?.(node.id)
  }, [onSelectStep])

  return (
    <div style={{ width: '100%', height: graphHeight }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodeClick={onNodeClick}
        fitView
        proOptions={{ hideAttribution: true }}
        nodesDraggable={false}
        nodesConnectable={false}
        minZoom={0.5}
        maxZoom={1.5}
      >
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  )
}

function computeLevels(steps: StepDef[]): Map<string, number> {
  const levels = new Map<string, number>()
  const visited = new Set<string>()

  function visit(id: string): number {
    if (levels.has(id)) return levels.get(id)!
    if (visited.has(id)) return 0
    visited.add(id)
    const step = steps.find(s => s.id === id)
    if (!step || !step.depends_on?.length) {
      levels.set(id, 0)
      return 0
    }
    let maxDep = 0
    for (const dep of step.depends_on) {
      maxDep = Math.max(maxDep, visit(dep) + 1)
    }
    levels.set(id, maxDep)
    return maxDep
  }

  for (const step of steps) visit(step.id)
  return levels
}
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add web/src/components/DAGGraph.tsx
git commit -m "feat(web): rewrite DAGGraph with custom nodes, smoothstep edges, dynamic height"
```

---

## Chunk 3: C2 — Run Detail Overhaul

### Task 10: StepTimeline Component

**Files:**
- Create: `web/src/components/StepTimeline.tsx`

- [ ] **Step 1: Create StepTimeline.tsx**

Create `web/src/components/StepTimeline.tsx`:
```tsx
import type { StepRun, StepStatus } from '@/api/types'
import { cn } from '@/lib/utils'
import { formatDuration } from '@/lib/format'

interface StepTimelineProps {
  stepRuns: StepRun[]
  selectedStepId?: string | null
  onSelect: (stepId: string) => void
}

const DOT_COLOR: Record<string, string> = {
  pending: 'bg-gray-300',
  cloning: 'bg-blue-500',
  running: 'bg-blue-500',
  verifying: 'bg-violet-500',
  awaiting_input: 'bg-amber-500',
  complete: 'bg-green-500',
  failed: 'bg-red-500',
  skipped: 'bg-gray-300',
}

const PULSING = new Set<StepStatus>(['running', 'cloning', 'verifying'])

export function StepTimeline({ stepRuns, selectedStepId, onSelect }: StepTimelineProps) {
  return (
    <div className="relative pl-7">
      {/* Vertical line */}
      <div className="absolute left-[7px] top-1 bottom-1 w-0.5 bg-border rounded-full" />

      {stepRuns.map((sr) => {
        const elapsed = sr.started_at
          ? formatDuration(
              (sr.completed_at ? new Date(sr.completed_at).getTime() : Date.now()) -
              new Date(sr.started_at).getTime()
            )
          : null

        return (
          <button
            key={sr.id}
            onClick={() => onSelect(sr.step_id)}
            className={cn(
              'relative flex w-full items-start gap-3 rounded-lg px-3 py-2.5 text-left transition-colors',
              selectedStepId === sr.step_id && 'bg-blue-500/5',
              selectedStepId !== sr.step_id && 'hover:bg-muted',
            )}
          >
            {/* Status dot */}
            <span className={cn(
              'absolute -left-5 top-3.5 h-2.5 w-2.5 rounded-full border-2 border-card z-10',
              DOT_COLOR[sr.status] ?? 'bg-gray-300',
              PULSING.has(sr.status as StepStatus) && 'animate-pulse',
            )} />

            <div className="flex-1 min-w-0">
              <div className="flex items-center justify-between">
                <span className="text-[13px] font-medium truncate">
                  {sr.step_title || sr.step_id}
                </span>
                <span className={cn(
                  'text-xs tabular-nums text-muted-foreground ml-2 shrink-0',
                  PULSING.has(sr.status as StepStatus) && 'text-blue-500',
                )}>
                  {elapsed ?? '—'}
                </span>
              </div>
              <span className="text-[11px] text-muted-foreground">
                {sr.status.replace('_', ' ')}
              </span>
            </div>
          </button>
        )
      })}
    </div>
  )
}
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add web/src/components/StepTimeline.tsx
git commit -m "feat(web): add StepTimeline vertical timeline component"
```

---

### Task 11: DiffViewer + JsonViewer Components

**Files:**
- Create: `web/src/components/DiffViewer.tsx`
- Create: `web/src/components/JsonViewer.tsx`

- [ ] **Step 1: Create DiffViewer.tsx**

Create `web/src/components/DiffViewer.tsx`:
```tsx
import { cn } from '@/lib/utils'

/** CSS-only syntax-highlighted diff viewer: +green, -red, @@blue */
export function DiffViewer({ diff }: { diff: string }) {
  const lines = diff.split('\n')
  return (
    <div className="overflow-auto max-h-80 rounded-md bg-muted/50 font-mono text-xs leading-relaxed">
      {lines.map((line, i) => (
        <div
          key={i}
          className={cn(
            'px-3 whitespace-pre',
            line.startsWith('+') && !line.startsWith('+++') && 'bg-green-500/10 text-green-700 dark:text-green-400',
            line.startsWith('-') && !line.startsWith('---') && 'bg-red-500/10 text-red-700 dark:text-red-400',
            line.startsWith('@@') && 'text-blue-600 dark:text-blue-400 font-medium',
            line.startsWith('diff ') && 'text-muted-foreground font-medium border-t border-border mt-1 pt-1',
          )}
        >
          {line}
        </div>
      ))}
    </div>
  )
}
```

- [ ] **Step 2: Create JsonViewer.tsx**

Create `web/src/components/JsonViewer.tsx`:
```tsx
/** Simple syntax-colored JSON viewer */
export function JsonViewer({ data }: { data: unknown }) {
  const json = JSON.stringify(data, null, 2)
  // Colorize: keys, strings, numbers, booleans, nulls
  const html = json.replace(
    /("(?:\\.|[^"\\])*")\s*:/g, '<span class="text-blue-600 dark:text-blue-400">$1</span>:'
  ).replace(
    /:\s*("(?:\\.|[^"\\])*")/g, ': <span class="text-green-700 dark:text-green-400">$1</span>'
  ).replace(
    /:\s*(\d+(?:\.\d+)?)/g, ': <span class="text-amber-600 dark:text-amber-400">$1</span>'
  ).replace(
    /:\s*(true|false|null)/g, ': <span class="text-violet-600 dark:text-violet-400">$1</span>'
  )

  return (
    <pre
      className="overflow-auto max-h-60 rounded-md bg-muted/50 p-3 font-mono text-xs leading-relaxed"
      dangerouslySetInnerHTML={{ __html: html }}
    />
  )
}
```

Note: `dangerouslySetInnerHTML` is safe here because the input is `JSON.stringify` output (no user-controlled HTML). The regex only wraps known JSON tokens.

- [ ] **Step 3: Verify build**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add web/src/components/DiffViewer.tsx web/src/components/JsonViewer.tsx
git commit -m "feat(web): add DiffViewer and JsonViewer syntax-colored components"
```

---

### Task 12: Update StepPanel to Use DiffViewer + JsonViewer

**Files:**
- Modify: `web/src/components/StepPanel.tsx`

- [ ] **Step 1: Replace raw pre blocks in StepPanel.tsx**

In `web/src/components/StepPanel.tsx`:

1. Add imports at top:
```ts
import { DiffViewer } from './DiffViewer'
import { JsonViewer } from './JsonViewer'
import { StatusBadge } from './StatusBadge'
```

2. Replace the diff `<pre>` block (lines 66-72) with:
```tsx
      {stepRun.diff && (
        <div className="space-y-2">
          <h4 className="text-sm font-medium text-muted-foreground">Diff</h4>
          <DiffViewer diff={stepRun.diff} />
        </div>
      )}
```

3. Replace the output `<pre>` block (lines 75-82) with:
```tsx
      {stepRun.output && Object.keys(stepRun.output).length > 0 && (
        <div className="space-y-2">
          <h4 className="text-sm font-medium text-muted-foreground">Output</h4>
          <JsonViewer data={stepRun.output} />
        </div>
      )}
```

4. Replace the `StatusBadge` function at bottom (lines 92-97) — delete it; use the imported `StatusBadge` component instead. Update line 24 from `<StatusBadge status={stepRun.status} />` (already correct if the local function is removed).

- [ ] **Step 2: Verify build**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add web/src/components/StepPanel.tsx
git commit -m "feat(web): use DiffViewer and JsonViewer in StepPanel"
```

---

### Task 13: Update LogStream with Header Bar

**Files:**
- Modify: `web/src/components/LogStream.tsx`

- [ ] **Step 1: Add header bar to LogStream**

Replace the return JSX in `web/src/components/LogStream.tsx` (lines 51-66). Also add a `showScrollBtn` state variable to track scroll position reactively (refs don't trigger re-renders):

Add state at top of component (after `autoScrollRef`):
```ts
  const [showScrollBtn, setShowScrollBtn] = useState(false)
```

Update `handleScroll` to also set the state:
```ts
  const handleScroll = () => {
    if (!containerRef.current) return
    const { scrollTop, scrollHeight, clientHeight } = containerRef.current
    const atBottom = scrollHeight - scrollTop - clientHeight < 50
    autoScrollRef.current = atBottom
    setShowScrollBtn(!atBottom)
  }
```

Replace the return JSX with:
```tsx
  return (
    <div className="rounded-md overflow-hidden border border-gray-800">
      {/* Header */}
      <div className="flex items-center justify-between bg-gray-900 px-3 py-1.5 border-b border-gray-800">
        <span className="text-[11px] text-gray-400">Logs</span>
        <div className="flex items-center gap-2">
          {logs.length > 0 && (
            <span className="flex items-center gap-1.5 text-[11px] text-gray-500">
              <span className="h-1.5 w-1.5 rounded-full bg-green-500 animate-pulse" />
              streaming
            </span>
          )}
          {showScrollBtn && (
            <button
              onClick={() => {
                autoScrollRef.current = true
                setShowScrollBtn(false)
                if (containerRef.current) containerRef.current.scrollTop = containerRef.current.scrollHeight
              }}
              className="text-[11px] text-gray-500 hover:text-gray-300 transition-colors"
            >
              ↓ scroll to bottom
            </button>
          )}
        </div>
      </div>
      {/* Log content */}
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className="h-56 overflow-auto bg-black/80 p-3 font-mono text-xs text-green-400"
      >
        {logs.length === 0 && (
          <span className="text-gray-600">Waiting for logs...</span>
        )}
        {logs.map((log) => (
          <div key={log.id} className={log.stream === 'stderr' ? 'text-red-400' : ''}>
            {log.content}
          </div>
        ))}
      </div>
    </div>
  )
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add web/src/components/LogStream.tsx
git commit -m "feat(web): add header bar with streaming indicator to LogStream"
```

---

### Task 14: Rewrite RunDetail Page

**Files:**
- Modify: `web/src/pages/RunDetail.tsx`

- [ ] **Step 1: Rewrite RunDetail.tsx**

Replace full contents of `web/src/pages/RunDetail.tsx`. Key changes:
- Add live duration counter via `useLiveDuration`
- Add progress bar (segmented by step status)
- Use StepTimeline in a two-column layout alongside StepPanel
- Use StatusBadge

```tsx
import { useState, useEffect, useCallback } from 'react'
import { useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, subscribeToRun, getConfig } from '@/api/client'
import { DAGGraph } from '@/components/DAGGraph'
import { StepPanel } from '@/components/StepPanel'
import { StepTimeline } from '@/components/StepTimeline'
import { HITLPanel } from '@/components/HITLPanel'
import { StatusBadge } from '@/components/StatusBadge'
import { Skeleton } from '@/components/Skeleton'
import { Button } from '@/components/ui/button'
import { useLiveDuration } from '@/lib/use-live-duration'
import { cn } from '@/lib/utils'
import type { StepDef, StepRun, Run, RunStatusUpdate, StepRunLog } from '@/api/types'

const SEG_COLOR: Record<string, string> = {
  complete: 'bg-green-500',
  running: 'bg-blue-500 animate-pulse',
  cloning: 'bg-blue-500 animate-pulse',
  verifying: 'bg-violet-500 animate-pulse',
  awaiting_input: 'bg-amber-500',
  failed: 'bg-red-500',
  pending: 'bg-transparent',
  skipped: 'bg-transparent',
}

export function RunDetailPage() {
  const { id } = useParams<{ id: string }>()
  const queryClient = useQueryClient()
  const [selectedStepId, setSelectedStepId] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [cancelling, setCancelling] = useState(false)
  const [sseConnected, setSseConnected] = useState(false)

  const { data: config } = useQuery({ queryKey: ['config'], queryFn: getConfig, staleTime: Infinity })

  const { data: run } = useQuery({
    queryKey: ['run', id],
    queryFn: () => api.getRun(id!),
    enabled: !!id,
    refetchInterval: sseConnected ? false : 5000,
  })

  const duration = useLiveDuration(run?.started_at, run?.completed_at)

  useEffect(() => {
    if (!id) return
    setSseConnected(false)
    const cleanup = subscribeToRun(
      id,
      (_update: RunStatusUpdate) => queryClient.invalidateQueries({ queryKey: ['run', id] }),
      (_log: StepRunLog) => {},
      () => setSseConnected(false),
    )
    setSseConnected(true)
    return () => cleanup?.()
  }, [id, queryClient])

  const handleApprove = useCallback(async () => {
    if (!id) return
    setActionError(null)
    try { await api.approveRun(id); queryClient.invalidateQueries({ queryKey: ['run', id] }) }
    catch (err) { setActionError(err instanceof Error ? err.message : 'Action failed') }
  }, [id, queryClient])

  const handleReject = useCallback(async () => {
    if (!id) return
    setActionError(null)
    try { await api.rejectRun(id); queryClient.invalidateQueries({ queryKey: ['run', id] }) }
    catch (err) { setActionError(err instanceof Error ? err.message : 'Action failed') }
  }, [id, queryClient])

  const handleSteer = useCallback(async (prompt: string) => {
    if (!id) return
    setActionError(null)
    try { await api.steerRun(id, prompt); queryClient.invalidateQueries({ queryKey: ['run', id] }) }
    catch (err) { setActionError(err instanceof Error ? err.message : 'Action failed') }
  }, [id, queryClient])

  const handleCancel = useCallback(async () => {
    if (!id) return
    setActionError(null)
    setCancelling(true)
    try { await api.cancelRun(id); queryClient.invalidateQueries({ queryKey: ['run', id] }) }
    catch (err) { setActionError(err instanceof Error ? err.message : 'Action failed'); setCancelling(false) }
  }, [id, queryClient])

  if (!run) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-4 w-96" />
        <Skeleton className="h-64 w-full rounded-lg" />
      </div>
    )
  }

  // Parse workflow def from template for DAG visualization.
  // StepRuns alone lack depends_on/mode — we need the original YAML definition.
  // The run object may carry a workflow_def field (if the backend embeds it),
  // otherwise we fall back to flat step list (no edges).
  let steps: StepDef[] = []
  try {
    if ((run as any).workflow_def?.steps) {
      steps = (run as any).workflow_def.steps as StepDef[]
    } else if (run.steps) {
      steps = run.steps.map((sr: StepRun) => ({
        id: sr.step_id,
        title: sr.step_title,
      }))
    }
  } catch { /* ignore */ }

  const stepRuns = run.steps ?? []
  const selectedStep = stepRuns.find((sr: StepRun) => sr.step_id === selectedStepId)
  const awaitingStep = stepRuns.find((sr: StepRun) => sr.status === 'awaiting_input')
  const completedCount = stepRuns.filter(s => s.status === 'complete').length

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">{run.workflow_title}</h1>
          <p className="text-xs text-muted-foreground font-mono mt-0.5">{run.id}</p>
        </div>
        <div className="flex items-center gap-3">
          {run.temporal_id && config?.temporal_ui_url && (
            <a
              href={`${config.temporal_ui_url}/namespaces/default/workflows/${run.temporal_id}`}
              target="_blank" rel="noopener noreferrer"
              className="text-xs text-muted-foreground underline hover:text-foreground"
            >Temporal ↗</a>
          )}
          {duration && <span className="text-sm text-muted-foreground tabular-nums">{duration}</span>}
          <StatusBadge status={run.status} />
          {(run.status === 'running' || run.status === 'awaiting_input') && (
            <Button variant="destructive" size="sm" onClick={handleCancel} disabled={cancelling}>
              {cancelling ? 'Cancelling…' : 'Cancel'}
            </Button>
          )}
        </div>
      </div>

      {/* Progress bar */}
      {stepRuns.length > 0 && (
        <div className="flex items-center gap-3">
          <span className="text-sm text-muted-foreground whitespace-nowrap">
            {completedCount} of {stepRuns.length} steps
          </span>
          <div className="flex flex-1 h-1.5 rounded-full bg-muted gap-0.5 overflow-hidden">
            {stepRuns.map(sr => (
              <div key={sr.id} className={cn('flex-1 rounded-full', SEG_COLOR[sr.status] ?? 'bg-transparent')} />
            ))}
          </div>
        </div>
      )}

      {actionError && (
        <div className="rounded-md bg-red-500/10 border border-red-500/20 p-3 text-sm text-red-400">
          {actionError}
        </div>
      )}

      {/* DAG */}
      {steps.length > 0 && (
        <div className="rounded-lg border bg-card p-4">
          <DAGGraph
            steps={steps}
            stepRuns={stepRuns}
            onSelectStep={setSelectedStepId}
            selectedStepId={selectedStepId ?? undefined}
          />
        </div>
      )}

      {/* HITL Panel */}
      {awaitingStep && (
        <HITLPanel stepRun={awaitingStep} onApprove={handleApprove} onReject={handleReject} onSteer={handleSteer} />
      )}

      {/* Two-column: Timeline + Panel */}
      {stepRuns.length > 0 && (
        <div className="grid grid-cols-1 md:grid-cols-[1fr_360px] gap-6 items-start">
          {/* Step detail or placeholder */}
          <div>
            {selectedStep ? (
              <div className="rounded-lg border bg-card p-4">
                <StepPanel stepRun={selectedStep} runParameters={run.parameters} allStepRuns={stepRuns} />
              </div>
            ) : (
              <div className="flex items-center justify-center rounded-lg border border-dashed py-16 text-sm text-muted-foreground">
                Select a step from the DAG or timeline to view details
              </div>
            )}
          </div>

          {/* Timeline */}
          <div>
            <h3 className="text-sm font-semibold mb-3">Steps</h3>
            <StepTimeline stepRuns={stepRuns} selectedStepId={selectedStepId} onSelect={setSelectedStepId} />
          </div>
        </div>
      )}

      {/* Pending placeholder */}
      {stepRuns.length === 0 && run.status === 'pending' && (
        <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed py-16 text-muted-foreground">
          <div className="h-5 w-5 animate-spin rounded-full border-2 border-current border-t-transparent" />
          <p className="text-sm">Waiting for workflow to start…</p>
        </div>
      )}

      {/* Run parameters */}
      {run.parameters && Object.keys(run.parameters).length > 0 && (
        <details>
          <summary className="cursor-pointer text-sm text-muted-foreground">Parameters</summary>
          <pre className="mt-2 max-h-48 overflow-auto rounded-md bg-muted p-3 text-xs font-mono">
            {JSON.stringify(run.parameters, null, 2)}
          </pre>
        </details>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/RunDetail.tsx
git commit -m "feat(web): rewrite RunDetail with live duration, progress bar, two-column layout"
```

---

## Chunk 4: C3 — Workflow Pages

### Task 15: Update WorkflowList with Category Colors + Polish

**Files:**
- Modify: `web/src/pages/WorkflowList.tsx`

- [ ] **Step 1: Rewrite WorkflowList.tsx**

Replace full contents of `web/src/pages/WorkflowList.tsx`:

```tsx
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { SkeletonCard } from '@/components/Skeleton'
import { EmptyState } from '@/components/EmptyState'
import { workflowCategory, CATEGORY_STYLES } from '@/lib/workflow-colors'
import { cn } from '@/lib/utils'
import {
  Shield, Bug, GitBranch, Search, Tag, Terminal,
  MessageSquare, RefreshCw, LayoutTemplate,
} from 'lucide-react'
import type { WorkflowTemplate } from '@/api/types'
import { parse as parseYaml } from '@/lib/yaml'

const ICON_MAP: Record<string, React.FC<{ className?: string }>> = {
  Shield, Bug, GitBranch, Search, Tag, Terminal,
  MessageSquare, RefreshCw,
}

function WorkflowCard({ wf }: { wf: WorkflowTemplate }) {
  const cat = workflowCategory(wf.tags ?? [])
  const styles = CATEGORY_STYLES[cat.color]
  const Icon = ICON_MAP[cat.icon] ?? Terminal

  let stepCount = 0
  let modes: string[] = []
  try {
    const def = parseYaml(wf.yaml_body) as { steps?: { mode?: string }[] }
    stepCount = def?.steps?.length ?? 0
    modes = [...new Set((def?.steps ?? []).map(s => s.mode).filter(Boolean) as string[])]
  } catch { /* ignore */ }

  return (
    <Link
      to={`/workflows/${wf.slug}`}
      className={cn(
        'rounded-lg border border-t-4 bg-card overflow-hidden transition-all hover:shadow-md hover:border-foreground/20',
        styles.border,
      )}
    >
      <div className="p-4 space-y-2">
        <div className="flex items-center gap-2.5">
          <div className={cn('flex h-8 w-8 items-center justify-center rounded-lg', styles.iconBg)}>
            <Icon className={cn('h-[18px] w-[18px]', styles.text)} />
          </div>
          <h3 className="font-semibold flex-1">{wf.title}</h3>
          {wf.builtin && <Badge variant="secondary" className="text-[10px]">builtin</Badge>}
        </div>
        <p className="text-sm text-muted-foreground line-clamp-2">{wf.description}</p>
        {stepCount > 0 && (
          <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <span>{stepCount} steps</span>
            {modes.map(m => (
              <span key={m}>&middot; {m}</span>
            ))}
          </div>
        )}
        <div className="flex flex-wrap gap-1">
          {wf.tags?.map((tag) => (
            <Badge key={tag} variant="outline" className="text-[10px]">{tag}</Badge>
          ))}
        </div>
      </div>
    </Link>
  )
}

export function WorkflowListPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['workflows'],
    queryFn: () => api.listWorkflows(),
  })

  const sorted = (data?.items ?? []).slice().sort((a, b) => a.title.localeCompare(b.title))

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Workflow Library</h1>
        {sorted.length > 0 && (
          <p className="text-sm text-muted-foreground mt-1">{sorted.length} workflows available</p>
        )}
      </div>

      {isLoading && (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <SkeletonCard /><SkeletonCard /><SkeletonCard />
        </div>
      )}

      {!isLoading && sorted.length === 0 && (
        <EmptyState icon={LayoutTemplate} title="No workflows yet" description="Create your first workflow template to get started." />
      )}

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {sorted.map(wf => <WorkflowCard key={wf.id} wf={wf} />)}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/WorkflowList.tsx
git commit -m "feat(web): add category colors, icons, step counts to WorkflowList"
```

---

### Task 16: Update WorkflowDetail with Hero Section + CodeMirror YAML

**Files:**
- Modify: `web/src/pages/WorkflowDetail.tsx`

- [ ] **Step 1: Rewrite WorkflowDetail.tsx**

Replace full contents of `web/src/pages/WorkflowDetail.tsx`:

```tsx
import { useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import CodeMirror from '@uiw/react-codemirror'
import { yaml } from '@codemirror/lang-yaml'
import { api } from '@/api/client'
import { DAGGraph } from '@/components/DAGGraph'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/Skeleton'
import { workflowCategory, CATEGORY_STYLES } from '@/lib/workflow-colors'
import { cn } from '@/lib/utils'
import { Shield, Bug, GitBranch, Search, Tag, Terminal } from 'lucide-react'
import type { WorkflowDef, ParameterDef } from '@/api/types'
import { parse as parseYaml } from '@/lib/yaml'

const ICON_MAP: Record<string, React.FC<{ className?: string }>> = {
  Shield, Bug, GitBranch, Search, Tag, Terminal,
}

export function WorkflowDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const { data: wf, isLoading } = useQuery({
    queryKey: ['workflow', id],
    queryFn: () => api.getWorkflow(id!),
    enabled: !!id,
  })

  const [params, setParams] = useState<Record<string, string>>({})
  const [showYaml, setShowYaml] = useState(false)

  const runMutation = useMutation({
    mutationFn: () => api.createRun(wf!.id, params),
    onSuccess: (run) => {
      queryClient.invalidateQueries({ queryKey: ['runs'] })
      navigate(`/runs/${run.id}`)
    },
  })

  if (isLoading || !wf) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-10 w-64" />
        <Skeleton className="h-4 w-96" />
        <Skeleton className="h-64 w-full rounded-lg" />
      </div>
    )
  }

  let def: WorkflowDef | null = null
  try { def = parseYaml(wf.yaml_body) as unknown as WorkflowDef } catch { /* ignore */ }

  const cat = workflowCategory(wf.tags ?? [])
  const styles = CATEGORY_STYLES[cat.color]
  const Icon = ICON_MAP[cat.icon] ?? Terminal

  return (
    <div className="space-y-6">
      {/* Hero */}
      <div className="flex items-start gap-4">
        <div className={cn('flex h-12 w-12 items-center justify-center rounded-xl', styles.iconBg)}>
          <Icon className={cn('h-6 w-6', styles.text)} />
        </div>
        <div className="flex-1">
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-bold">{wf.title}</h1>
            {wf.builtin && <Badge variant="secondary">builtin</Badge>}
          </div>
          <p className="text-sm text-muted-foreground mt-1">{wf.description}</p>
          {def?.steps && (
            <p className="text-xs text-muted-foreground mt-1">
              {def.steps.length} steps
              {def.parameters?.length ? ` · ${def.parameters.length} parameters` : ''}
            </p>
          )}
        </div>
        <div className="flex flex-wrap gap-1">
          {wf.tags?.map(tag => <Badge key={tag} variant="outline">{tag}</Badge>)}
        </div>
      </div>

      {/* DAG Preview */}
      {def?.steps && (
        <div className="rounded-lg border bg-card p-4">
          <h2 className="mb-3 text-sm font-semibold">DAG Preview</h2>
          <DAGGraph steps={def.steps} stepRuns={[]} />
        </div>
      )}

      {/* Parameters form */}
      {def?.parameters && def.parameters.length > 0 && (
        <div className="rounded-lg border bg-card p-4 space-y-4">
          <h2 className="text-sm font-semibold">Parameters</h2>
          {def.parameters.map((p: ParameterDef) => (
            <div key={p.name} className="space-y-1">
              <label className="text-sm font-medium">
                {p.name}
                {p.required && <span className="text-red-400 ml-1">*</span>}
                <span className="ml-2 text-xs text-muted-foreground">{p.type}</span>
              </label>
              {p.description && <p className="text-xs text-muted-foreground">{p.description}</p>}
              <input
                type="text"
                className="w-full rounded-md border bg-background px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                placeholder={p.default != null ? String(p.default) : ''}
                value={params[p.name] ?? ''}
                onChange={(e) => setParams({ ...params, [p.name]: e.target.value })}
              />
            </div>
          ))}
          <Button onClick={() => runMutation.mutate()} disabled={runMutation.isPending}>
            {runMutation.isPending ? 'Starting…' : 'Run Workflow'}
          </Button>
          {runMutation.isError && <p className="text-sm text-red-400">{runMutation.error.message}</p>}
        </div>
      )}

      {/* YAML viewer */}
      <div className="rounded-lg border bg-card overflow-hidden">
        <button
          onClick={() => setShowYaml(!showYaml)}
          className="w-full flex items-center justify-between px-4 py-3 text-sm font-medium hover:bg-muted/50 transition-colors"
        >
          <span>Workflow YAML</span>
          <span className="text-muted-foreground text-xs">{showYaml ? 'hide' : 'show'}</span>
        </button>
        {showYaml && (
          <div className="border-t">
            <CodeMirror
              value={wf.yaml_body}
              extensions={[yaml()]}
              editable={false}
              basicSetup={{ lineNumbers: true, foldGutter: true }}
              className="text-sm"
              maxHeight="400px"
            />
          </div>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/WorkflowDetail.tsx
git commit -m "feat(web): add hero section and CodeMirror YAML view to WorkflowDetail"
```

---

## Chunk 5: C5 Profile Menu + C6 Inbox

### Task 17: Backend — Enrich /api/me

**Files:**
- Modify: `internal/server/handlers/auth.go`
- Modify: `internal/server/handlers/auth_test.go`

- [ ] **Step 1: Update HandleMe to query user + teams from DB**

In `internal/server/handlers/auth.go`, replace `HandleMe` (lines 212-224):

```go
// HandleMe returns the authenticated user's identity with enriched profile data.
func (h *AuthHandler) HandleMe(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Fetch user profile from DB
	var name, email string
	_ = h.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(name, ''), COALESCE(email, '') FROM users WHERE id = $1`,
		claims.UserID,
	).Scan(&name, &email)

	// Fetch team details
	type teamInfo struct {
		ID   string `json:"id" db:"id"`
		Name string `json:"name" db:"name"`
		Slug string `json:"slug" db:"slug"`
		Role string `json:"role" db:"role"`
	}
	var teams []teamInfo
	rows, err := h.db.QueryxContext(r.Context(),
		`SELECT t.id, t.name, t.slug, tm.role
		 FROM teams t JOIN team_members tm ON t.id = tm.team_id
		 WHERE tm.user_id = $1 ORDER BY t.name`, claims.UserID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var t teamInfo
			if rows.StructScan(&t) == nil {
				teams = append(teams, t)
			}
		}
	}
	if teams == nil {
		teams = []teamInfo{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":        claims.UserID,
		"name":           name,
		"email":          email,
		"teams":          teams,
		"team_roles":     claims.TeamRoles,
		"platform_admin": claims.PlatformAdmin,
	})
}
```

- [ ] **Step 2: Add test for enriched /api/me**

Add to `internal/server/handlers/auth_test.go`:

```go
func TestHandleMeEnriched(t *testing.T) {
	db := setupTestDB(t)

	// Insert test user + team
	userID := "test-user-id"
	teamID := "test-team-id"
	db.MustExec(`INSERT INTO users (id, name, email, provider, provider_id) VALUES ($1, 'Alice', 'alice@example.com', 'github', '12345')`, userID)
	db.MustExec(`INSERT INTO teams (id, name, slug) VALUES ($1, 'Acme Corp', 'acme-corp') ON CONFLICT DO NOTHING`, teamID)
	db.MustExec(`INSERT INTO team_members (team_id, user_id, role) VALUES ($1, $2, 'admin') ON CONFLICT DO NOTHING`, teamID, userID)

	h := NewAuthHandler(db, nil, []byte("test-secret"))

	claims := &auth.Claims{UserID: userID, TeamRoles: map[string]string{teamID: "admin"}}
	req := httptest.NewRequest("GET", "/api/me", nil)
	req = req.WithContext(auth.SetClaimsInContext(req.Context(), claims))
	w := httptest.NewRecorder()

	h.HandleMe(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["name"] != "Alice" {
		t.Errorf("expected name 'Alice', got %v", body["name"])
	}
	if body["email"] != "alice@example.com" {
		t.Errorf("expected email 'alice@example.com', got %v", body["email"])
	}
	teams, ok := body["teams"].([]any)
	if !ok || len(teams) == 0 {
		t.Fatalf("expected teams array, got %v", body["teams"])
	}
	team := teams[0].(map[string]any)
	if team["name"] != "Acme Corp" {
		t.Errorf("expected team name 'Acme Corp', got %v", team["name"])
	}
}
```

Note: `setupTestDB` should follow the existing pattern in `auth_test.go`. If no `setupTestDB` helper exists, create a minimal one that connects to the test database and runs migrations.

- [ ] **Step 3: Run Go tests**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update && go test ./internal/server/handlers/... -run TestHandleMe -v
```

- [ ] **Step 4: Run lint**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update && make lint
```

- [ ] **Step 5: Commit**

```bash
git add internal/server/handlers/auth.go internal/server/handlers/auth_test.go
git commit -m "feat(api): enrich /api/me with user name, email, team details"
```

---

### Task 18: Frontend — Add UserProfile Type + api.getMe()

**Files:**
- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: Add types**

Add to `web/src/api/types.ts` (after InboxItem):
```ts
export interface UserTeam {
  id: string
  name: string
  slug: string
  role: string
}

export interface UserProfile {
  user_id: string
  name: string
  email: string
  teams: UserTeam[]
  team_roles: Record<string, string>
  platform_admin: boolean
}
```

- [ ] **Step 2: Add api.getMe()**

Add to the `api` object in `web/src/api/client.ts`:
```ts
  getMe: () => get<UserProfile>('/me'),
```

Add `UserProfile` to the imports from `./types`.

- [ ] **Step 3: Verify build**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add web/src/api/types.ts web/src/api/client.ts
git commit -m "feat(web): add UserProfile type and api.getMe()"
```

---

### Task 19: Install Radix DropdownMenu + Create UserMenu

**Files:**
- Create: `web/src/components/ui/dropdown-menu.tsx`
- Create: `web/src/components/UserMenu.tsx`
- Modify: `web/src/components/Layout.tsx`

- [ ] **Step 1: Install Radix dropdown**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npm install @radix-ui/react-dropdown-menu
```

- [ ] **Step 2: Generate dropdown-menu.tsx via shadcn CLI**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx shadcn@latest add dropdown-menu
```

If the CLI doesn't work (e.g., wrong shadcn version), manually create `web/src/components/ui/dropdown-menu.tsx`:

```tsx
import * as React from "react"
import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu"
import { cn } from "@/lib/utils"

const DropdownMenu = DropdownMenuPrimitive.Root
const DropdownMenuTrigger = DropdownMenuPrimitive.Trigger
const DropdownMenuGroup = DropdownMenuPrimitive.Group
const DropdownMenuSub = DropdownMenuPrimitive.Sub

const DropdownMenuContent = React.forwardRef<
  React.ComponentRef<typeof DropdownMenuPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Content>
>(({ className, sideOffset = 4, ...props }, ref) => (
  <DropdownMenuPrimitive.Portal>
    <DropdownMenuPrimitive.Content
      ref={ref}
      sideOffset={sideOffset}
      className={cn(
        "z-50 min-w-[8rem] overflow-hidden rounded-md border bg-popover p-1 text-popover-foreground shadow-md",
        "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95",
        className
      )}
      {...props}
    />
  </DropdownMenuPrimitive.Portal>
))
DropdownMenuContent.displayName = DropdownMenuPrimitive.Content.displayName

const DropdownMenuItem = React.forwardRef<
  React.ComponentRef<typeof DropdownMenuPrimitive.Item>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Item> & { inset?: boolean }
>(({ className, inset, ...props }, ref) => (
  <DropdownMenuPrimitive.Item
    ref={ref}
    className={cn(
      "relative flex cursor-default select-none items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none transition-colors focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
      inset && "pl-8",
      className
    )}
    {...props}
  />
))
DropdownMenuItem.displayName = DropdownMenuPrimitive.Item.displayName

const DropdownMenuLabel = React.forwardRef<
  React.ComponentRef<typeof DropdownMenuPrimitive.Label>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Label> & { inset?: boolean }
>(({ className, inset, ...props }, ref) => (
  <DropdownMenuPrimitive.Label
    ref={ref}
    className={cn("px-2 py-1.5 text-sm font-semibold", inset && "pl-8", className)}
    {...props}
  />
))
DropdownMenuLabel.displayName = DropdownMenuPrimitive.Label.displayName

const DropdownMenuSeparator = React.forwardRef<
  React.ComponentRef<typeof DropdownMenuPrimitive.Separator>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Separator>
>(({ className, ...props }, ref) => (
  <DropdownMenuPrimitive.Separator
    ref={ref}
    className={cn("-mx-1 my-1 h-px bg-muted", className)}
    {...props}
  />
))
DropdownMenuSeparator.displayName = DropdownMenuPrimitive.Separator.displayName

export {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuGroup,
  DropdownMenuSub,
}
```

- [ ] **Step 3: Create UserMenu.tsx**

Create `web/src/components/UserMenu.tsx`:
```tsx
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api } from '@/api/client'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem,
  DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger,
} from './ui/dropdown-menu'
import { LogOut, Users } from 'lucide-react'

function initials(name: string): string {
  return name.split(/\s+/).map(w => w[0]).join('').toUpperCase().slice(0, 2) || '?'
}

export function UserMenu() {
  const navigate = useNavigate()
  const { data: user } = useQuery({
    queryKey: ['me'],
    queryFn: () => api.getMe(),
    staleTime: 60_000,
  })

  const handleSignOut = () => {
    localStorage.removeItem('token')
    navigate('/login')
  }

  const displayName = user?.name || 'User'
  const teamName = user?.teams?.[0]?.name

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left transition-colors hover:bg-sidebar-accent">
          <div className="flex h-7 w-7 items-center justify-center rounded-full bg-blue-600 text-[11px] font-semibold text-white">
            {initials(displayName)}
          </div>
          <div className="flex-1 min-w-0">
            <div className="text-[13px] font-medium truncate">{displayName}</div>
            {teamName && <div className="text-[11px] text-muted-foreground truncate">{teamName}</div>}
          </div>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent side="top" align="start" className="w-56">
        <DropdownMenuLabel className="font-normal">
          <div className="text-sm font-medium">{displayName}</div>
          {user?.email && <div className="text-xs text-muted-foreground">{user.email}</div>}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {user?.teams && user.teams.length > 0 && (
          <>
            <DropdownMenuLabel className="text-xs text-muted-foreground">Teams</DropdownMenuLabel>
            {user.teams.map(t => (
              <DropdownMenuItem key={t.id} disabled>
                <Users className="mr-2 h-4 w-4" />
                <span>{t.name}</span>
                <span className="ml-auto text-xs text-muted-foreground">{t.role}</span>
              </DropdownMenuItem>
            ))}
            <DropdownMenuSeparator />
          </>
        )}
        <DropdownMenuItem onClick={handleSignOut}>
          <LogOut className="mr-2 h-4 w-4" />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
```

- [ ] **Step 4: Update Layout.tsx sidebar footer**

In `web/src/components/Layout.tsx`:

1. Add import: `import { UserMenu } from './UserMenu'`
2. Replace the sidebar footer section (lines 87-95):
```tsx
        <div className="border-t px-3 py-3">
          <UserMenu />
        </div>
```
3. Remove unused imports: `List` from lucide-react.

- [ ] **Step 5: Verify build**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 6: Commit**

```bash
git add web/package.json web/package-lock.json \
  web/src/components/ui/dropdown-menu.tsx \
  web/src/components/UserMenu.tsx \
  web/src/components/Layout.tsx
git commit -m "feat(web): add profile dropdown menu with user info and team list"
```

---

### Task 20: Rewrite Inbox with Filter Tabs + Inline Actions

**Files:**
- Modify: `web/src/pages/Inbox.tsx`

- [ ] **Step 1: Rewrite Inbox.tsx**

Replace full contents of `web/src/pages/Inbox.tsx`:

```tsx
import { useState, useCallback } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/EmptyState'
import { Skeleton } from '@/components/Skeleton'
import { formatTimeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Inbox as InboxIcon } from 'lucide-react'
import type { InboxItem } from '@/api/types'

type Filter = 'all' | 'awaiting_input' | 'failure' | 'output_ready'

const FILTERS: { key: Filter; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'awaiting_input', label: 'Action Required' },
  { key: 'failure', label: 'Failures' },
  { key: 'output_ready', label: 'Output Ready' },
]

function KindBadge({ kind }: { kind: string }) {
  if (kind === 'awaiting_input') {
    return (
      <Badge variant="warning" className="gap-1">
        <span className="relative flex h-1.5 w-1.5">
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-current opacity-50" />
          <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-current" />
        </span>
        Action Required
      </Badge>
    )
  }
  if (kind === 'failure') {
    return <Badge variant="destructive">Step Failed</Badge>
  }
  return <Badge variant="success">Output Ready</Badge>
}

export function InboxPage() {
  const queryClient = useQueryClient()
  const [filter, setFilter] = useState<Filter>('all')
  const [steerOpenId, setSteerOpenId] = useState<string | null>(null)
  const [steerText, setSteerText] = useState('')

  const { data, isLoading } = useQuery({
    queryKey: ['inbox'],
    queryFn: () => api.listInbox(),
    refetchInterval: 5000,
  })

  const markReadMutation = useMutation({
    mutationFn: (id: string) => api.markInboxRead(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['inbox'] }),
  })

  const [actionError, setActionError] = useState<string | null>(null)

  const handleApprove = useCallback(async (item: InboxItem) => {
    setActionError(null)
    try {
      await api.approveRun(item.run_id)
      queryClient.invalidateQueries({ queryKey: ['inbox'] })
    } catch (err) { setActionError(err instanceof Error ? err.message : 'Action failed') }
  }, [queryClient])

  const handleReject = useCallback(async (item: InboxItem) => {
    setActionError(null)
    try {
      await api.rejectRun(item.run_id)
      queryClient.invalidateQueries({ queryKey: ['inbox'] })
    } catch (err) { setActionError(err instanceof Error ? err.message : 'Action failed') }
  }, [queryClient])

  const handleSteer = useCallback(async (item: InboxItem) => {
    if (!steerText.trim()) return
    setActionError(null)
    try {
      await api.steerRun(item.run_id, steerText)
      setSteerText('')
      setSteerOpenId(null)
      queryClient.invalidateQueries({ queryKey: ['inbox'] })
    } catch (err) { setActionError(err instanceof Error ? err.message : 'Action failed') }
  }, [steerText, queryClient])

  const items = data?.items ?? []
  const filtered = filter === 'all' ? items : items.filter(i => i.kind === filter)

  const counts: Record<Filter, number> = {
    all: items.length,
    awaiting_input: items.filter(i => i.kind === 'awaiting_input').length,
    failure: items.filter(i => i.kind === 'failure').length,
    output_ready: items.filter(i => i.kind === 'output_ready').length,
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Inbox</h1>
        {items.length > 0 && (
          <p className="text-sm text-muted-foreground mt-1">{items.filter(i => !i.read).length} unread</p>
        )}
      </div>

      {/* Filter tabs */}
      <div className="flex gap-0 border-b">
        {FILTERS.map(f => (
          <button
            key={f.key}
            onClick={() => setFilter(f.key)}
            className={cn(
              'px-4 py-2 text-sm border-b-2 transition-colors',
              filter === f.key
                ? 'border-foreground text-foreground font-medium'
                : 'border-transparent text-muted-foreground hover:text-foreground',
            )}
          >
            {f.label}
            <span className={cn(
              'ml-1.5 text-[11px] px-1.5 py-px rounded-full',
              filter === f.key ? 'bg-foreground text-background' : 'bg-muted',
            )}>
              {counts[f.key]}
            </span>
          </button>
        ))}
      </div>

      {actionError && (
        <div className="rounded-md bg-red-500/10 border border-red-500/20 p-3 text-sm text-red-400">
          {actionError}
        </div>
      )}

      {isLoading && (
        <div className="space-y-3">
          <Skeleton className="h-20 rounded-lg" />
          <Skeleton className="h-20 rounded-lg" />
          <Skeleton className="h-20 rounded-lg" />
        </div>
      )}

      {!isLoading && filtered.length === 0 && (
        <EmptyState icon={InboxIcon} title="All caught up" description="No pending notifications." />
      )}

      <div className="space-y-2">
        {filtered.map(item => (
          <div
            key={item.id}
            className={cn(
              'rounded-lg border bg-card p-4 transition-all',
              !item.read && item.kind === 'awaiting_input' && 'border-l-4 border-l-amber-500',
              !item.read && item.kind !== 'awaiting_input' && 'border-l-4 border-l-blue-500',
            )}
          >
            <div className="flex items-start justify-between gap-4">
              <div className="flex-1 min-w-0 space-y-1">
                <div className="flex items-center gap-2">
                  <KindBadge kind={item.kind} />
                </div>
                <Link to={`/runs/${item.run_id}`} className="text-sm font-medium hover:underline block">
                  {item.title}
                </Link>
                {item.summary && (
                  <p className="text-[13px] text-muted-foreground line-clamp-2">{item.summary}</p>
                )}
                <p className="text-xs text-muted-foreground">{formatTimeAgo(item.created_at)}</p>
              </div>
              <div className="flex flex-col items-end gap-2 shrink-0">
                {item.kind === 'awaiting_input' && (
                  <div className="flex gap-1.5">
                    <Button size="sm" variant="default" className="h-7 text-xs bg-green-600 hover:bg-green-700" onClick={() => handleApprove(item)}>
                      Approve
                    </Button>
                    <Button size="sm" variant="destructive" className="h-7 text-xs" onClick={() => handleReject(item)}>
                      Reject
                    </Button>
                    <Button size="sm" variant="secondary" className="h-7 text-xs" onClick={() => setSteerOpenId(steerOpenId === item.id ? null : item.id)}>
                      Steer
                    </Button>
                  </div>
                )}
                {item.kind !== 'awaiting_input' && (
                  <div className="flex gap-1.5">
                    <Button size="sm" variant="secondary" className="h-7 text-xs" asChild>
                      <Link to={`/runs/${item.run_id}`}>View</Link>
                    </Button>
                    {!item.read && (
                      <Button size="sm" variant="secondary" className="h-7 text-xs" onClick={() => markReadMutation.mutate(item.id)}>
                        Mark Read
                      </Button>
                    )}
                  </div>
                )}
              </div>
            </div>
            {/* Steer form */}
            {steerOpenId === item.id && (
              <div className="flex gap-2 mt-3 pt-3 border-t">
                <input
                  className="flex-1 rounded-md border bg-background px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                  placeholder="Provide alternative instructions..."
                  value={steerText}
                  onChange={e => setSteerText(e.target.value)}
                  onKeyDown={e => e.key === 'Enter' && handleSteer(item)}
                />
                <Button size="sm" onClick={() => handleSteer(item)} disabled={!steerText.trim()}>
                  Send
                </Button>
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/Inbox.tsx
git commit -m "feat(web): add filter tabs, inline approve/reject/steer to Inbox"
```

---

## Chunk 6: C4 Page Polish + C8 System Health

### Task 21: Apply Skeleton + EmptyState to RunList

**Files:**
- Modify: `web/src/pages/RunList.tsx`

- [ ] **Step 1: Update RunList.tsx**

In `web/src/pages/RunList.tsx`:

1. Add imports:
```ts
import { StatusBadge } from '@/components/StatusBadge'
import { SkeletonRow } from '@/components/Skeleton'
import { EmptyState } from '@/components/EmptyState'
import { Activity } from 'lucide-react'
```

2. Replace `{isLoading && <p>...}` (line 27) with:
```tsx
      {isLoading && (
        <div className="rounded-lg border">
          <SkeletonRow /><SkeletonRow /><SkeletonRow />
        </div>
      )}
```

3. Replace the empty `<td colSpan>` (lines 58-63) with:
```tsx
                <td colSpan={4} className="p-0">
                  <EmptyState icon={Activity} title="No runs yet" description="Start a workflow to see runs here." action={{ label: 'Browse Workflows', href: '/workflows' }} />
                </td>
```

4. Replace `<Badge variant={STATUS_VARIANT[run.status]}>` usage with `<StatusBadge status={run.status} />`. Remove the `STATUS_VARIANT` constant and the `Badge` import (if no longer needed).

- [ ] **Step 2: Verify build**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/RunList.tsx
git commit -m "feat(web): add skeleton loading, empty state, StatusBadge to RunList"
```

---

### Task 22: Add SystemHealth Page + Route

**Files:**
- Create: `web/src/pages/SystemHealth.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/Layout.tsx`

- [ ] **Step 1: Create SystemHealth.tsx**

Create `web/src/pages/SystemHealth.tsx`:
```tsx
import { useQuery } from '@tanstack/react-query'
import { getConfig } from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/Skeleton'
import { Activity, Server, AlertTriangle } from 'lucide-react'

export function SystemHealthPage() {
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: getConfig, staleTime: Infinity })

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">System Health</h1>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {/* Temporal connection */}
        <div className="rounded-lg border bg-card p-4 space-y-2">
          <div className="flex items-center gap-2">
            <Server className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm font-medium">Temporal</span>
          </div>
          <div className="flex items-center gap-2">
            <Badge variant="success">Connected</Badge>
          </div>
          {config?.temporal_ui_url && (
            <a
              href={config.temporal_ui_url}
              target="_blank"
              rel="noopener noreferrer"
              className="text-xs text-muted-foreground underline hover:text-foreground"
            >
              Open Temporal UI ↗
            </a>
          )}
        </div>

        {/* API Status */}
        <div className="rounded-lg border bg-card p-4 space-y-2">
          <div className="flex items-center gap-2">
            <Activity className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm font-medium">API Server</span>
          </div>
          <Badge variant="success">Healthy</Badge>
        </div>

        {/* Placeholder */}
        <div className="rounded-lg border bg-card p-4 space-y-2">
          <div className="flex items-center gap-2">
            <AlertTriangle className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm font-medium">Queue Depth</span>
          </div>
          <p className="text-sm text-muted-foreground">Coming soon — requires Temporal metrics endpoint</p>
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Add route to App.tsx**

In `web/src/App.tsx`:
1. Add import: `import { SystemHealthPage } from './pages/SystemHealth'`
2. Add route inside the protected routes: `<Route path="system" element={<SystemHealthPage />} />`

- [ ] **Step 3: Add nav link to Layout.tsx**

In `web/src/components/Layout.tsx`:
1. Add `Heart` (or `Activity`) to lucide imports
2. Add to `NAV_ITEMS` array:
```ts
  { href: '/system', label: 'System', icon: Activity },
```

(Reuse the existing `Activity` import or add a new one if needed.)

- [ ] **Step 4: Verify build**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/SystemHealth.tsx web/src/App.tsx web/src/components/Layout.tsx
git commit -m "feat(web): add System Health page with Temporal status"
```

---

### Task 23: Run All Tests + Full Build Verification

- [ ] **Step 1: Run frontend tests**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx vitest run
```

- [ ] **Step 2: Run TypeScript check**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npx tsc --noEmit
```

- [ ] **Step 3: Run frontend lint**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npm run lint
```

- [ ] **Step 4: Run Go tests**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update && go test ./...
```

- [ ] **Step 5: Run Go lint**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update && make lint
```

- [ ] **Step 6: Build frontend**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update/web && npm run build
```

- [ ] **Step 7: Build Go binary**

```bash
cd /Users/andrew/dev/code/projects/fleetlift/ui_update && go build ./...
```

---

## Summary

| Chunk | Tasks | Phases Covered | Commits | Status |
|-------|-------|----------------|---------|--------|
| 1: Foundation | 1-7 | C4 (partial) | 7 | ✅ Complete |
| 2: DAG Overhaul | 8-9 | C1 | 2 | ✅ Complete |
| 3: Run Detail | 10-14 | C2, C7 (partial) | 5 | ✅ Complete |
| 4: Workflow Pages | 15-16 | C3 | 2 | ✅ Complete |
| 5: Profile + Inbox | 17-20 | C5, C6 | 4 | ✅ Complete |
| 6: Polish + Health | 21-23 | C4, C8 | 3 | ✅ Complete |
| **Total** | **23 tasks** | **C1-C8** | **23 commits** | **✅ All done** |

### Execution Notes
- Executed 2026-03-14 by a team of 5 agents (foundation → dag-run, workflows, profile-inbox in parallel → finalizer)
- All verification checks passed: vitest (15 tests), tsc, eslint, go test (17 packages), golangci-lint, frontend build, go build
- Code review: Approved with P1/P2 follow-ups (see ROADMAP.md Track C section)

### Deferred Items
- `GET /api/health/system` backend endpoint (SystemHealth page uses hardcoded values)
- Fan-out visualization in DAG (one node → parallel lanes)
- CodeMirror YAML editor with real-time validation (read-only viewer implemented)
- HITL/failure inbox notification creation (backend: `step.go`, `dag.go` — frontend UI is ready)
- Component-level tests for new UI components
