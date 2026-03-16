# Dark Mode Toggle — Design Spec

**Date:** 2026-03-16

## Overview

Add a three-state theme toggle (System / Light / Dark) to the Fleetlift web UI. The toggle lives in the sidebar nav column above the user avatar and persists the user's choice in `localStorage`.

---

## Behaviour

| State | Icon | Description |
|-------|------|-------------|
| System *(default)* | ⚙️ | Mirrors OS `prefers-color-scheme`. Updates live if OS setting changes. |
| Light | ☀️ | Forces light mode regardless of OS. |
| Dark | 🌙 | Forces dark mode regardless of OS. |

- Clicking the toggle cycles: System → Light → Dark → System …
- The icon shown reflects the **current** state (not the next state).
- A Radix `<Tooltip>` on the button shows the current state label (e.g. "Theme: System"). The tooltip intentionally shows the current state rather than the next action — the cycling behaviour is discoverable by interaction.
- On first visit, state defaults to **System**.
- On subsequent visits the saved `localStorage` value is restored before first paint (no flash).

---

## Architecture

### `useTheme` hook (`src/hooks/useTheme.ts`)

Owns all theme logic. **Designed for a single consumer** (`ThemeToggle`). If a second component ever needs to read theme state, wrap this in a React context at that point.

- Reads initial value from `localStorage` key `"fleetlift:theme"` (`"system"` | `"light"` | `"dark"`); falls back to `"system"`. All `localStorage` reads/writes are wrapped in `try/catch` to guard against `SecurityError` (thrown in Safari private browsing).
- Applies the `.dark` class to `<html>` based on resolved mode:
  - `"light"` → remove `.dark`
  - `"dark"` → add `.dark`
  - `"system"` → add/remove `.dark` based on `window.matchMedia('(prefers-color-scheme: dark)')`, and attach a `change` listener to keep it in sync.
- **`matchMedia` listener lifecycle:** Listener is attached when state is `"system"` and detached when state changes away from `"system"`. Switching back to `"system"` re-attaches it. Listener is always cleaned up on unmount.
- Persists chosen value to `localStorage` on every change (inside try/catch).
- Returns `{ theme, setTheme }` where:
  - `theme: "system" | "light" | "dark"` — the stored preference (not the resolved dark/light state)
  - `setTheme: (theme: "system" | "light" | "dark") => void`

**Note:** There is a narrow window between the no-flash inline script running and the hook mounting where an OS theme change would not be reflected. This is an acceptable gap given the millisecond-scale duration on any real device.

### `ThemeToggle` component (`src/components/ThemeToggle.tsx`)

- Calls `useTheme()`.
- Renders a single `<button>` wrapped in a Radix `<Tooltip>` showing the current state label (consistent with existing UI tooltip patterns).
- Contains a `nextTheme` helper that encodes the cycling order (System → Light → Dark → System). Cycling is a `ThemeToggle` concern; `useTheme` is called with explicit values via `setTheme`.
- Icons use Lucide React: `SunIcon` (light), `MoonIcon` (dark), `SunMoonIcon` (system).
- Styled consistently with other sidebar icon buttons.

### Layout integration (`src/components/Layout.tsx`)

- `<ThemeToggle />` inserted in the sidebar column between the nav links and the user avatar area.

---

## Infrastructure already in place

- `index.css` already defines `.dark` CSS variable overrides.
- `tailwind.config.js` already has `darkMode: ["class"]`.
- No new CSS variables, no new Tailwind config changes needed.

---

## No-flash strategy

The `.dark` class must be applied **before** the browser first paints. A small inline `<script>` is added as the **first child of `<head>`** in `index.html`, before any stylesheet links, so it runs synchronously before any rendering:

```html
<script>
  (function() {
    try {
      var t = localStorage.getItem('fleetlift:theme');
      var dark = t === 'dark' || (t !== 'light' && window.matchMedia('(prefers-color-scheme: dark)').matches);
      if (dark) document.documentElement.classList.add('dark');
    } catch (e) {}
  })();
</script>
```

- Treats any non-`'light'`, non-`'dark'` value (including `null`, `'system'`, or corrupt data) as "follow the OS".
- Wrapped in `try/catch` so `localStorage` access failures (Safari private browsing) are silent.
- Runs before React hydrates, eliminating a light-mode flash for dark-mode users.

---

## Files changed

| File | Change |
|------|--------|
| `web/src/hooks/useTheme.ts` | New — theme hook |
| `web/src/components/ThemeToggle.tsx` | New — toggle button component |
| `web/src/components/Layout.tsx` | Add `<ThemeToggle />` above user avatar |
| `web/index.html` | Add no-flash inline script |

---

## Testing

- **`useTheme` unit tests:**
  - Initialises to `"system"` when `localStorage` is empty.
  - Restores saved value from `localStorage` on mount.
  - Cycling: system → light → dark → system.
  - Applies/removes `.dark` class on `<html>` correctly for each state.
  - Attaches `matchMedia` listener when state is `"system"`; detaches it when switching away from `"system"`; re-attaches when switching back.
  - Detaches listener on unmount.
  - Does not throw when `localStorage.getItem` throws on init (Safari private browsing).
  - Does not throw when `localStorage.setItem` throws on write (storage quota exceeded or private browsing).
- **`ThemeToggle` unit tests:**
  - Renders correct icon for each state.
  - Cycles to next state on click.

---

## Out of scope

- Server-side persistence of theme preference.
- Per-page or per-component theme overrides.
- Animated transition between themes.
- Vite SSR / prerendering (`window` and `localStorage` guards not needed for current SPA deployment).
- React context wrapper (add only if/when a second consumer of theme state is needed).
