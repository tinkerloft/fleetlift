# Dark Mode Toggle Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a three-state (System / Light / Dark) theme toggle to the sidebar, persisted in localStorage with no-flash on load.

**Architecture:** A `useTheme` hook owns localStorage read/write and `.dark` class management on `<html>`. A `ThemeToggle` button in the sidebar cycles through states and shows a tooltip. A blocking inline script in `index.html` applies the saved theme before first paint.

**Tech Stack:** React 19, TypeScript, Tailwind CSS (darkMode: "class"), Lucide React, Radix UI Tooltip, Vitest + Testing Library (jsdom)

---

## Chunk 1: Foundation — Tooltip component + no-flash script

### Task 1: Install Radix Tooltip and add `<Tooltip>` UI component

**Context:** The app uses Radix UI primitives wrapped in thin `src/components/ui/` components (see `button.tsx`, `dropdown-menu.tsx` for the pattern). `@radix-ui/react-tooltip` is not yet installed.

**Files:**
- Create: `web/src/components/ui/tooltip.tsx`
- Modify: `web/package.json` (dependency added via npm)

- [ ] **Step 1: Install the package**

Run from `web/`:
```bash
npm install @radix-ui/react-tooltip
```
Expected: package added to `dependencies` in `package.json`.

- [ ] **Step 2: Create the Tooltip UI component**

Create `web/src/components/ui/tooltip.tsx`:
```tsx
import * as TooltipPrimitive from '@radix-ui/react-tooltip'
import { cn } from '@/lib/utils'

const TooltipProvider = TooltipPrimitive.Provider

const Tooltip = TooltipPrimitive.Root

const TooltipTrigger = TooltipPrimitive.Trigger

function TooltipContent({
  className,
  sideOffset = 4,
  ...props
}: React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Content>) {
  return (
    <TooltipPrimitive.Portal>
      <TooltipPrimitive.Content
        sideOffset={sideOffset}
        className={cn(
          'z-50 overflow-hidden rounded-md bg-popover px-3 py-1.5 text-xs text-popover-foreground shadow-md',
          'animate-in fade-in-0 zoom-in-95',
          'data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95',
          'data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2',
          'data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2',
          className,
        )}
        {...props}
      />
    </TooltipPrimitive.Portal>
  )
}

export { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider }
```

- [ ] **Step 3: Verify build compiles**

Run from `web/`:
```bash
npm run build
```
Expected: build succeeds with no errors.

- [ ] **Step 4: Commit**

```bash
git add web/package.json web/package-lock.json web/src/components/ui/tooltip.tsx
git commit -m "feat: add Radix Tooltip UI component"
```

---

### Task 2: Add no-flash inline script to `index.html`

**Context:** Without this script, users with dark mode OS preference see a white flash before React mounts and applies the `.dark` class. The script must run synchronously as the first child of `<head>`, before any stylesheets. It reads `localStorage` key `"fleetlift:theme"` and applies `.dark` to `<html>` if needed.

**Files:**
- Modify: `web/index.html`

- [ ] **Step 1: Add the inline script as first child of `<head>`**

Edit `web/index.html` — insert the script block **immediately after `<head>`**, before the `<meta charset>` tag:
```html
<!doctype html>
<html lang="en">
  <head>
    <script>
      (function() {
        try {
          var t = localStorage.getItem('fleetlift:theme');
          var dark = t === 'dark' || (t !== 'light' && window.matchMedia('(prefers-color-scheme: dark)').matches);
          if (dark) document.documentElement.classList.add('dark');
        } catch (e) {}
      })();
    </script>
    <meta charset="UTF-8" />
    <link rel="icon" type="image/svg+xml" href="/vite.svg" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>web</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 2: Verify build compiles**

```bash
npm run build
```
Expected: build succeeds.

- [ ] **Step 3: Commit**

```bash
git add web/index.html
git commit -m "feat: add no-flash dark mode inline script"
```

---

## Chunk 2: useTheme hook

### Task 3: Implement and test the `useTheme` hook

**Context:** This hook owns all theme logic. It reads from `localStorage`, applies/removes the `.dark` class on `<html>`, and attaches a `matchMedia` listener when in system mode. All `localStorage` access is wrapped in try/catch for Safari private browsing compatibility. There is no hooks directory yet — create it.

**Files:**
- Create: `web/src/hooks/useTheme.ts`
- Create: `web/src/hooks/__tests__/useTheme.test.ts`

- [ ] **Step 1: Create directory and write failing tests**

Create `web/src/hooks/__tests__/useTheme.test.ts`:
```ts
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useTheme } from '../useTheme'

// matchMedia mock
function mockMatchMedia(matches: boolean) {
  const listeners: ((e: { matches: boolean }) => void)[] = []
  const mql = {
    matches,
    addEventListener: vi.fn((_: string, fn: (e: { matches: boolean }) => void) => listeners.push(fn)),
    removeEventListener: vi.fn((_: string, fn: (e: { matches: boolean }) => void) => {
      const i = listeners.indexOf(fn)
      if (i !== -1) listeners.splice(i, 1)
    }),
    _listeners: listeners,
    _fire: (matches: boolean) => listeners.forEach(fn => fn({ matches })),
  }
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn(() => mql),
  })
  return mql
}

beforeEach(() => {
  localStorage.clear()
  document.documentElement.classList.remove('dark')
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useTheme', () => {
  it('defaults to "system" when localStorage is empty', () => {
    mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    expect(result.current.theme).toBe('system')
  })

  it('restores saved "light" from localStorage', () => {
    localStorage.setItem('fleetlift:theme', 'light')
    mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    expect(result.current.theme).toBe('light')
  })

  it('restores saved "dark" from localStorage', () => {
    localStorage.setItem('fleetlift:theme', 'dark')
    mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    expect(result.current.theme).toBe('dark')
  })

  it('applies .dark class when theme is "dark"', () => {
    mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    act(() => result.current.setTheme('dark'))
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('removes .dark class when theme is "light"', () => {
    document.documentElement.classList.add('dark')
    mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    act(() => result.current.setTheme('light'))
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('applies .dark class in system mode when OS is dark', () => {
    mockMatchMedia(true)
    renderHook(() => useTheme())
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('does not apply .dark class in system mode when OS is light', () => {
    mockMatchMedia(false)
    renderHook(() => useTheme())
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('attaches matchMedia listener in system mode', () => {
    const mql = mockMatchMedia(false)
    renderHook(() => useTheme())
    expect(mql.addEventListener).toHaveBeenCalledWith('change', expect.any(Function))
  })

  it('detaches matchMedia listener when switching away from system', () => {
    const mql = mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    act(() => result.current.setTheme('light'))
    expect(mql.removeEventListener).toHaveBeenCalledWith('change', expect.any(Function))
  })

  it('re-attaches matchMedia listener when switching back to system', () => {
    const mql = mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    act(() => result.current.setTheme('light'))
    act(() => result.current.setTheme('system'))
    // addEventListener called twice: initial mount + re-attach
    expect(mql.addEventListener).toHaveBeenCalledTimes(2)
  })

  it('detaches listener on unmount', () => {
    const mql = mockMatchMedia(false)
    const { unmount } = renderHook(() => useTheme())
    unmount()
    expect(mql.removeEventListener).toHaveBeenCalledWith('change', expect.any(Function))
  })

  it('updates .dark class when OS theme changes while in system mode', () => {
    const mql = mockMatchMedia(false)
    renderHook(() => useTheme())
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    act(() => mql._fire(true))
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('persists theme to localStorage on change', () => {
    mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    act(() => result.current.setTheme('dark'))
    expect(localStorage.getItem('fleetlift:theme')).toBe('dark')
  })

  it('does not throw when localStorage.getItem throws', () => {
    mockMatchMedia(false)
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('SecurityError') })
    expect(() => renderHook(() => useTheme())).not.toThrow()
  })

  it('does not throw when localStorage.setItem throws', () => {
    mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('SecurityError') })
    expect(() => act(() => result.current.setTheme('dark'))).not.toThrow()
  })
})
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
npm test
```
Expected: tests fail with "Cannot find module '../useTheme'" or similar.

- [ ] **Step 3: Implement the hook**

Create `web/src/hooks/useTheme.ts`:
```ts
import { useState, useEffect, useCallback } from 'react'

export type Theme = 'system' | 'light' | 'dark'

const STORAGE_KEY = 'fleetlift:theme'

function readStorage(): Theme {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v === 'light' || v === 'dark' || v === 'system') return v
  } catch (_) {}
  return 'system'
}

function writeStorage(theme: Theme) {
  try {
    localStorage.setItem(STORAGE_KEY, theme)
  } catch (_) {}
}

function applyTheme(theme: Theme, mql: MediaQueryList) {
  const dark = theme === 'dark' || (theme === 'system' && mql.matches)
  document.documentElement.classList.toggle('dark', dark)
}

export function useTheme(): { theme: Theme; setTheme: (theme: Theme) => void } {
  const [theme, setThemeState] = useState<Theme>(readStorage)

  const setTheme = useCallback((next: Theme) => {
    setThemeState(next)
    writeStorage(next)
  }, [])

  useEffect(() => {
    const mql = window.matchMedia('(prefers-color-scheme: dark)')
    applyTheme(theme, mql)

    if (theme !== 'system') return

    const handler = (e: { matches: boolean }) => {
      document.documentElement.classList.toggle('dark', e.matches)
    }
    mql.addEventListener('change', handler)
    return () => mql.removeEventListener('change', handler)
  }, [theme])

  return { theme, setTheme }
}
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
npm test
```
Expected: all `useTheme` tests pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/hooks/useTheme.ts web/src/hooks/__tests__/useTheme.test.ts
git commit -m "feat: add useTheme hook with localStorage persistence and system preference support"
```

---

## Chunk 3: ThemeToggle component + Layout wiring

### Task 4: Implement and test the `ThemeToggle` component

**Context:** The component renders a button cycling through System → Light → Dark → System. It uses the Lucide icons `SunMoon` (system), `Sun` (light), `Moon` (dark) and wraps the button in a `<Tooltip>` that shows the current state label. The `TooltipProvider` must wrap the whole app (or the component); the simplest approach is to add it in `App.tsx`. See `src/components/__tests__/StatusBadge.test.tsx` for the test style.

**Files:**
- Create: `web/src/components/ThemeToggle.tsx`
- Create: `web/src/components/__tests__/ThemeToggle.test.tsx`
- Modify: `web/src/App.tsx` (add `TooltipProvider` wrapper)

- [ ] **Step 1: Write failing tests**

Note: `screen.getByTitle('Theme: System')` finds the `<button>` element by its `title` attribute (not an SVG title). This verifies that the correct state label is shown for each theme, which is the observable outcome of the correct icon rendering.

Create `web/src/components/__tests__/ThemeToggle.test.tsx`:
```tsx
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ThemeToggle } from '../ThemeToggle'

// Stub useTheme so tests control state directly
let mockTheme: 'system' | 'light' | 'dark' = 'system'
const mockSetTheme = vi.fn((next: 'system' | 'light' | 'dark') => { mockTheme = next })

vi.mock('@/hooks/useTheme', () => ({
  useTheme: () => ({ theme: mockTheme, setTheme: mockSetTheme }),
}))

beforeEach(() => {
  mockTheme = 'system'
  mockSetTheme.mockClear()
  // matchMedia stub (required by jsdom)
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })),
  })
})

describe('ThemeToggle', () => {
  it('renders the system icon when theme is "system"', () => {
    render(<ThemeToggle />)
    expect(screen.getByTitle('Theme: System')).toBeTruthy()
  })

  it('renders the light icon when theme is "light"', () => {
    mockTheme = 'light'
    render(<ThemeToggle />)
    expect(screen.getByTitle('Theme: Light')).toBeTruthy()
  })

  it('renders the dark icon when theme is "dark"', () => {
    mockTheme = 'dark'
    render(<ThemeToggle />)
    expect(screen.getByTitle('Theme: Dark')).toBeTruthy()
  })

  it('cycles system → light on click', () => {
    render(<ThemeToggle />)
    fireEvent.click(screen.getByRole('button'))
    expect(mockSetTheme).toHaveBeenCalledWith('light')
  })

  it('cycles light → dark on click', () => {
    mockTheme = 'light'
    render(<ThemeToggle />)
    fireEvent.click(screen.getByRole('button'))
    expect(mockSetTheme).toHaveBeenCalledWith('dark')
  })

  it('cycles dark → system on click', () => {
    mockTheme = 'dark'
    render(<ThemeToggle />)
    fireEvent.click(screen.getByRole('button'))
    expect(mockSetTheme).toHaveBeenCalledWith('system')
  })
})
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
npm test
```
Expected: fail with "Cannot find module '../ThemeToggle'".

- [ ] **Step 3: Implement ThemeToggle**

Create `web/src/components/ThemeToggle.tsx`. Note: Lucide v0.574.0 exports icons under short names (`Sun`, `Moon`, `SunMoon`) — use those, not the `Icon`-suffixed variants mentioned in the spec.

```tsx
import { SunMoon, Sun, Moon } from 'lucide-react'
import { useTheme, type Theme } from '@/hooks/useTheme'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

const CYCLE: Theme[] = ['system', 'light', 'dark']

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

export function ThemeToggle() {
  const { theme, setTheme } = useTheme()
  const Icon = ICONS[theme]
  const label = LABELS[theme]

  function handleClick() {
    const next = CYCLE[(CYCLE.indexOf(theme) + 1) % CYCLE.length]
    setTheme(next)
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          onClick={handleClick}
          title={`Theme: ${label}`}
          className={cn(
            'flex h-9 w-9 items-center justify-center rounded-lg',
            'text-muted-foreground hover:bg-sidebar-accent/50 hover:text-foreground transition-colors',
          )}
          aria-label={`Theme: ${label}`}
        >
          <Icon className="h-4 w-4" />
        </button>
      </TooltipTrigger>
      <TooltipContent side="right">
        Theme: {label}
      </TooltipContent>
    </Tooltip>
  )
}
```

- [ ] **Step 4: Add `TooltipProvider` to `App.tsx`**

Edit `web/src/App.tsx`. Add the import and wrap the `<Routes>` element with `<TooltipProvider>`:

```tsx
// Add this import (after existing imports):
import { TooltipProvider } from '@/components/ui/tooltip'

// Change the App() return from:
//   return (
//     <Routes>
//       ...
//     </Routes>
//   )
// to:
export default function App() {
  return (
    <TooltipProvider>
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
    </TooltipProvider>
  )
}
```

- [ ] **Step 5: Run tests — verify they pass**

```bash
npm test
```
Expected: all tests pass.

- [ ] **Step 6: Verify build**

```bash
npm run build
```
Expected: build succeeds with no errors.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/ThemeToggle.tsx web/src/components/__tests__/ThemeToggle.test.tsx web/src/App.tsx
git commit -m "feat: add ThemeToggle component (system/light/dark cycling with tooltip)"
```

---

### Task 5: Wire ThemeToggle into the sidebar Layout

**Context:** The sidebar footer in `Layout.tsx` (line 91–93) currently renders only `<UserMenu />`. Add `<ThemeToggle />` directly above it in the footer `<div>`.

**Files:**
- Modify: `web/src/components/Layout.tsx`

- [ ] **Step 1: Import and render ThemeToggle**

In `web/src/components/Layout.tsx`, add the import and update the sidebar footer.

Add import (after the existing `UserMenu` import line):
```tsx
import { ThemeToggle } from './ThemeToggle'
```

Change the sidebar footer section. Before:
```tsx
{/* Footer */}
<div className="border-t px-3 py-3">
  <UserMenu />
</div>
```

After:
```tsx
{/* Footer */}
<div className="border-t px-3 py-3 flex flex-col gap-1">
  <ThemeToggle />
  <UserMenu />
</div>
```

- [ ] **Step 2: Verify build compiles**

```bash
npm run build
```
Expected: build succeeds with no errors.

- [ ] **Step 3: Run all tests**

```bash
npm test
```
Expected: all tests pass.

- [ ] **Step 4: Run linter (from repo root)**

```bash
cd .. && make lint
```
Expected: no lint errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/Layout.tsx
git commit -m "feat: add ThemeToggle to sidebar — dark mode toggle complete"
```
