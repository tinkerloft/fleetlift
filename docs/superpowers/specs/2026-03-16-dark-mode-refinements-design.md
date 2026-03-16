# Dark Mode Refinements — Design Spec

**Date:** 2026-03-16
**PR:** feat/dark-mode-toggle

## Overview

Three refinements to the dark mode toggle:
1. Lighten the dark theme from near-black to a softer charcoal.
2. Resize and align the theme toggle to match the `UserMenu` trigger.
3. Replace the cycling icon button with a full-width dropdown that lets users pick a mode directly.

---

## 1. Dark Mode Colour Update

### Problem

The current `.dark` palette uses `--background: 222 20% 7%` — almost pure black. This reads as too harsh; a charcoal (~15–17% lightness) is easier on the eyes and more in line with common design systems.

### Changes to `web/src/index.css` (`.dark` block)

| Variable | Before | After |
|----------|--------|-------|
| `--background` | `222 20% 7%` | `220 13% 16%` |
| `--card` | `222 18% 10%` | `220 13% 19%` |
| `--card-foreground` | `210 20% 95%` | unchanged |
| `--popover` | `222 18% 10%` | `220 13% 19%` |
| `--popover-foreground` | `210 20% 95%` | unchanged |
| `--primary` | `210 20% 95%` | unchanged |
| `--primary-foreground` | `222 20% 7%` | `220 13% 16%` |
| `--secondary` | `222 14% 15%` | `220 10% 23%` |
| `--secondary-foreground` | `210 20% 95%` | unchanged |
| `--muted` | `222 14% 15%` | `220 10% 23%` |
| `--muted-foreground` | `218 10% 55%` | `220 10% 60%` |
| `--accent` | `222 14% 15%` | `220 10% 23%` |
| `--accent-foreground` | `210 20% 95%` | unchanged |
| `--border` | `222 14% 18%` | `220 10% 26%` |
| `--input` | `222 14% 18%` | `220 10% 26%` |
| `--ring` | `210 20% 85%` | unchanged |
| `--sidebar` | `222 18% 9%` | `220 13% 14%` |
| `--sidebar-foreground` | `210 20% 95%` | unchanged |
| `--sidebar-border` | `222 14% 18%` | `220 10% 26%` |
| `--sidebar-accent` | `222 14% 14%` | `220 10% 21%` |

All foreground, chart, and status (destructive/success/warning) colours are unchanged — they are already calibrated for contrast and not listed in the table above.

---

## 2. ThemeToggle Redesign

### Problem

The current `ThemeToggle` is a 36×36px icon button that sits above `UserMenu`. It is visually inconsistent with `UserMenu` (which is full-width with text) and provides no label, making the current mode unclear at a glance.

### New design

Replace the cycling icon button with a full-width dropdown trigger that visually matches `UserMenu`.

#### Trigger

```
[ ☀ Light          ⌄ ]
```

- Full-width button: `flex w-full items-center gap-2.5 rounded-lg px-3 py-2`
- Left: Lucide icon for current theme (`Sun`, `Moon`, `SunMoon`)
- Middle: text label ("Light", "Dark", "System"), `flex-1`
- Right: `ChevronUp` icon (static — no animation on open/close)
- Hover state: `hover:bg-sidebar-accent` (matching `UserMenu` trigger)

#### Dropdown

Uses the existing `DropdownMenu` / `DropdownMenuContent` / `DropdownMenuItem` components (no new dependencies).

- Opens upward: `side="top" align="start"`
- Width: `w-44` (explicit fixed width — Radix portal does not inherit trigger width)
- Three items in order: System, Light, Dark
- Each item: icon + label
- Active item: highlighted text + `Check` icon on the right
- Clicking an item calls `setTheme(value)` directly (no cycling)

#### Behaviour

- `useTheme` hook is unchanged — still owns localStorage persistence and `.dark` class management
- `ThemeToggle` is the only consumer, calling `setTheme` with an explicit value
- No `Tooltip` needed — label is always visible in the trigger

#### `TooltipProvider` cleanup

`TooltipProvider` was added to `App.tsx` solely for `ThemeToggle`. Since `ThemeToggle` no longer uses `Tooltip`, remove the `TooltipProvider` import and wrapper from `App.tsx`. The `web/src/components/ui/tooltip.tsx` file can remain for future use, but is no longer imported anywhere.

**Note for future developers:** Any component that uses `<Tooltip>` must have `<TooltipProvider>` as an ancestor. If `Tooltip` is used again in future, restore `TooltipProvider` in `App.tsx`.

Also remove the `TooltipProvider` wrapper (`Wrapper` component) and its import from `web/src/components/__tests__/ThemeToggle.test.tsx` — it was only needed for the old Tooltip-based implementation.

---

## Files Changed

| File | Change |
|------|--------|
| `web/src/index.css` | Update `.dark` CSS variable lightness values |
| `web/src/components/ThemeToggle.tsx` | Rewrite as full-width dropdown |
| `web/src/components/__tests__/ThemeToggle.test.tsx` | Update tests for dropdown interaction |
| `web/src/App.tsx` | Remove `TooltipProvider` import and wrapper |

---

## Testing

**`ThemeToggle` unit tests (replace existing 6 with 7 new tests; remove `TooltipProvider` wrapper):**
- Renders trigger with correct label for each theme state ("System", "Light", "Dark")
- Renders correct icon in trigger for each theme state
- Clicking a menu item calls `setTheme` with the correct value (one test per item: system, light, dark)
- Active item (current theme) shows `Check` icon

**No changes needed** to `useTheme` tests — hook is unchanged.

---

## Out of Scope

- Removing `web/src/components/ui/tooltip.tsx` (keep for future use)
- Changing the `useTheme` hook
- Changing the `index.html` no-flash script
