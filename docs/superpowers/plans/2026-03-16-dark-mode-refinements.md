# Dark Mode Refinements Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Lighten the dark theme to charcoal, and replace the cycling icon toggle with a full-width dropdown that lets users pick System / Light / Dark directly.

**Architecture:** Two independent changes: (1) CSS variable updates in `index.css` — no tests needed, just visual verification via build; (2) rewrite `ThemeToggle.tsx` to use `DropdownMenu` (already used by `UserMenu`), update its tests, and remove the now-unused `TooltipProvider` from `App.tsx`.

**Tech Stack:** Tailwind CSS (HSL variables), React 19, Radix UI DropdownMenu, Lucide React, Vitest + Testing Library

---

## Chunk 1: Dark mode colour update

### Task 1: Update `.dark` CSS variables to charcoal palette

**Context:** The current `.dark` block in `web/src/index.css` (lines 39–72) uses near-black values (background lightness ~7%). This task bumps all background/surface/border variables to a charcoal range (~14–26% lightness). Foreground, chart, and status colour variables are unchanged.

**Files:**
- Modify: `web/src/index.css` (lines 39–72)

- [ ] **Step 1: Replace the `.dark` block**

In `web/src/index.css`, replace the entire `.dark { … }` block with:

```css
  .dark {
    --background: 220 13% 16%;
    --foreground: 210 20% 95%;
    --card: 220 13% 19%;
    --card-foreground: 210 20% 95%;
    --popover: 220 13% 19%;
    --popover-foreground: 210 20% 95%;
    --primary: 210 20% 95%;
    --primary-foreground: 220 13% 16%;
    --secondary: 220 10% 23%;
    --secondary-foreground: 210 20% 95%;
    --muted: 220 10% 23%;
    --muted-foreground: 220 10% 60%;
    --accent: 220 10% 23%;
    --accent-foreground: 210 20% 95%;
    --destructive: 0 62% 30%;
    --destructive-foreground: 0 0% 98%;
    --success: 142 60% 35%;
    --success-foreground: 0 0% 98%;
    --warning: 38 80% 45%;
    --warning-foreground: 0 0% 98%;
    --border: 220 10% 26%;
    --input: 220 10% 26%;
    --ring: 210 20% 85%;
    --chart-1: 221 70% 60%;
    --chart-2: 142 60% 50%;
    --chart-3: 38 80% 55%;
    --chart-4: 262 70% 65%;
    --chart-5: 0 60% 55%;
    --sidebar: 220 13% 14%;
    --sidebar-foreground: 210 20% 95%;
    --sidebar-border: 220 10% 26%;
    --sidebar-accent: 220 10% 21%;
  }
```

- [ ] **Step 2: Verify build**

```bash
cd web && npm run build
```
Expected: build succeeds with no errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/index.css
git commit -m "fix: lighten dark mode palette to charcoal"
```

---

## Chunk 2: ThemeToggle dropdown rewrite

### Task 2: Rewrite ThemeToggle as a dropdown and clean up TooltipProvider

**Context:** `ThemeToggle.tsx` is currently a cycling icon button that uses `Tooltip`. Replace it with a full-width `DropdownMenu` trigger (matching `UserMenu`'s layout) that opens a menu with three items (System, Light, Dark). The active item shows a `Check` icon. `useTheme` is unchanged — `setTheme` is called directly with the selected value.

After the rewrite:
- The `TooltipProvider` in `App.tsx` (line 16 import, lines 26/50 wrapper) is no longer needed and must be removed.
- The `TooltipProvider` wrapper (`Wrapper` function) in `ThemeToggle.test.tsx` must be removed.
- `tooltip.tsx` in `ui/` is kept (may be used in future).

**Files:**
- Modify: `web/src/components/ThemeToggle.tsx` (full rewrite)
- Modify: `web/src/components/__tests__/ThemeToggle.test.tsx` (full rewrite)
- Modify: `web/src/App.tsx` (remove TooltipProvider)

- [ ] **Step 1: Write the failing tests**

Replace the entire contents of `web/src/components/__tests__/ThemeToggle.test.tsx` with:

```tsx
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ThemeToggle } from '../ThemeToggle'

let mockTheme: 'system' | 'light' | 'dark' = 'system'
const mockSetTheme = vi.fn((next: 'system' | 'light' | 'dark') => { mockTheme = next })

vi.mock('@/hooks/useTheme', () => ({
  useTheme: () => ({ theme: mockTheme, setTheme: mockSetTheme }),
}))

beforeEach(() => {
  mockTheme = 'system'
  mockSetTheme.mockClear()
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })),
  })
})

describe('ThemeToggle', () => {
  // Trigger label tests — no need to open dropdown
  it('shows "System" label in trigger when theme is "system"', () => {
    render(<ThemeToggle />)
    expect(screen.getByText('System')).toBeTruthy()
  })

  it('shows "Light" label in trigger when theme is "light"', () => {
    mockTheme = 'light'
    render(<ThemeToggle />)
    expect(screen.getByText('Light')).toBeTruthy()
  })

  it('shows "Dark" label in trigger when theme is "dark"', () => {
    mockTheme = 'dark'
    render(<ThemeToggle />)
    expect(screen.getByText('Dark')).toBeTruthy()
  })

  // Trigger icon tests — button contains 2 SVGs: theme icon + ChevronUp
  it('renders theme icon and ChevronUp in trigger when theme is "system"', () => {
    render(<ThemeToggle />)
    expect(screen.getByRole('button').querySelectorAll('svg').length).toBe(2)
  })

  it('renders theme icon and ChevronUp in trigger when theme is "light"', () => {
    mockTheme = 'light'
    render(<ThemeToggle />)
    expect(screen.getByRole('button').querySelectorAll('svg').length).toBe(2)
  })

  it('renders theme icon and ChevronUp in trigger when theme is "dark"', () => {
    mockTheme = 'dark'
    render(<ThemeToggle />)
    expect(screen.getByRole('button').querySelectorAll('svg').length).toBe(2)
  })

  // Dropdown item interaction — open menu then click items
  // Radix DropdownMenu requires full pointer sequence to open in jsdom
  function openDropdown() {
    const trigger = screen.getByRole('button')
    fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false, pointerType: 'mouse', pointerId: 1 })
    fireEvent.pointerUp(trigger)
    fireEvent.click(trigger)
  }

  it('calls setTheme("system") when System item is clicked', () => {
    render(<ThemeToggle />)
    openDropdown()
    fireEvent.click(screen.getByRole('menuitem', { name: /system/i }))
    expect(mockSetTheme).toHaveBeenCalledWith('system')
  })

  it('calls setTheme("light") when Light item is clicked', () => {
    render(<ThemeToggle />)
    openDropdown()
    fireEvent.click(screen.getByRole('menuitem', { name: /light/i }))
    expect(mockSetTheme).toHaveBeenCalledWith('light')
  })

  it('calls setTheme("dark") when Dark item is clicked', () => {
    render(<ThemeToggle />)
    openDropdown()
    fireEvent.click(screen.getByRole('menuitem', { name: /dark/i }))
    expect(mockSetTheme).toHaveBeenCalledWith('dark')
  })

  it('shows check icon on the active theme item only', () => {
    mockTheme = 'dark'
    render(<ThemeToggle />)
    openDropdown()
    // Active item (Dark) has 2 SVGs: theme icon + Check icon
    const darkItem = screen.getByRole('menuitem', { name: /dark/i })
    expect(darkItem.querySelectorAll('svg').length).toBe(2)
    // Inactive items have 1 SVG: theme icon only
    const systemItem = screen.getByRole('menuitem', { name: /system/i })
    expect(systemItem.querySelectorAll('svg').length).toBe(1)
    const lightItem = screen.getByRole('menuitem', { name: /light/i })
    expect(lightItem.querySelectorAll('svg').length).toBe(1)
  })
})
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
cd web && npm test -- --reporter=verbose 2>&1 | grep -E "FAIL|PASS|✓|✗|×|ThemeToggle"
```
Expected: ThemeToggle tests fail (wrong component structure).

- [ ] **Step 3: Implement the new ThemeToggle**

Replace the entire contents of `web/src/components/ThemeToggle.tsx` with:

```tsx
import { SunMoon, Sun, Moon, Check, ChevronUp } from 'lucide-react'
import { useTheme, type Theme } from '@/hooks/useTheme'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

const LABELS: Record<Theme, string> = {
  system: 'System',
  light: 'Light',
  dark: 'Dark',
}

const ICONS: Record<Theme, React.FC<{ className?: string }>> = {
  system: SunMoon,
  light: Sun,
  dark: Moon,
}

const ITEMS: Theme[] = ['system', 'light', 'dark']

export function ThemeToggle() {
  const { theme, setTheme } = useTheme()
  const Icon = ICONS[theme]

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-muted-foreground transition-colors hover:bg-sidebar-accent hover:text-foreground">
          <Icon className="h-4 w-4 shrink-0" />
          <span className="flex-1 text-[13px] font-medium">{LABELS[theme]}</span>
          <ChevronUp className="h-3 w-3 opacity-50" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent side="top" align="start" className="w-44">
        {ITEMS.map((t) => {
          const ItemIcon = ICONS[t]
          return (
            <DropdownMenuItem key={t} onClick={() => setTheme(t)}>
              <ItemIcon className="mr-2 h-4 w-4" />
              <span>{LABELS[t]}</span>
              {theme === t && <Check className="ml-auto h-4 w-4" />}
            </DropdownMenuItem>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
cd web && npm test
```
Expected: all 55+ tests pass (6 old ThemeToggle tests replaced by 10 new ones).

- [ ] **Step 5: Remove TooltipProvider from App.tsx**

Replace `web/src/App.tsx` with:

```tsx
import type React from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { Layout } from './components/Layout'
import { WorkflowListPage } from './pages/WorkflowList'
import { WorkflowDetailPage } from './pages/WorkflowDetail'
import { RunListPage } from './pages/RunList'
import { RunDetailPage } from './pages/RunDetail'
import { InboxPage } from './pages/Inbox'
import { ReportListPage } from './pages/ReportList'
import { ReportDetailPage } from './pages/ReportDetail'
import { KnowledgePage } from './pages/KnowledgePage'
import { SystemHealthPage } from './pages/SystemHealth'
import { CredentialsPage } from './pages/CredentialsPage'
import { LoginPage } from './pages/Login'
import { AuthCallbackPage } from './pages/AuthCallback'

function RequireAuth({ children }: { children: React.ReactNode }) {
  const token = localStorage.getItem('token')
  if (!token) return <Navigate to="/login" replace />
  return <>{children}</>
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/auth/callback" element={<AuthCallbackPage />} />
      <Route path="/*" element={
        <RequireAuth>
          <Layout>
            <Routes>
              <Route path="/" element={<Navigate to="/runs" replace />} />
              <Route path="/workflows" element={<WorkflowListPage />} />
              <Route path="/workflows/:id" element={<WorkflowDetailPage />} />
              <Route path="/runs" element={<RunListPage />} />
              <Route path="/runs/:id" element={<RunDetailPage />} />
              <Route path="/inbox" element={<InboxPage />} />
              <Route path="/reports" element={<ReportListPage />} />
              <Route path="/reports/:runId" element={<ReportDetailPage />} />
              <Route path="/knowledge" element={<KnowledgePage />} />
              <Route path="system" element={<SystemHealthPage />} />
              <Route path="/settings" element={<CredentialsPage />} />
            </Routes>
          </Layout>
        </RequireAuth>
      } />
    </Routes>
  )
}
```

- [ ] **Step 6: Run all tests and verify build**

```bash
cd web && npm test && npm run build
```
Expected: all tests pass, build succeeds.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/ThemeToggle.tsx \
        web/src/components/__tests__/ThemeToggle.test.tsx \
        web/src/App.tsx
git commit -m "feat: replace cycling icon with dropdown theme selector, remove TooltipProvider"
```
