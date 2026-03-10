# Phase 2 Completion: Template Gallery Design

**Date:** 2026-03-09
**Goal:** Complete Phase 2 (Frontend — Task Creation) by adding the Template Gallery (2C).

---

## Context

Phase 2 of the web interface enrichment is mostly done:
- 2A (Task wizard): implemented as AI chat UI at `/create`
- 2B (AI chat mode): done — streaming chat + YAML preview in `TaskCreate.tsx`
- 2C (Template gallery): partial — inline panel toggle exists, no standalone `/templates` route

Remaining work: build the standalone gallery page and enhance the inline quick-picker.

---

## Approach

**Option B — Full Gallery + Inline Quick-picker**

- Standalone `/templates` page with search, mode filter, richer cards, YAML preview panel
- Inline panel in `/create` kept as quick-picker, enhanced with "Browse all →" link and mode badges
- Pre-fill: `/create?template=<name>` loads template YAML on mount

---

## Architecture

### New: `web/src/pages/TemplatesPage.tsx`

Route: `/templates`

- Fetches templates via `api.listTemplates()`; fetches full YAML via `api.getTemplate(name)` on selection
- State: `search` (string), `modeFilter` (`all | transform | report`), `selectedTemplate`
- Layout: left panel = search bar + mode filter tabs + card grid; right panel = YAML preview (shown when a template is selected)
- Card shows: name, description, mode badge
- "Use template" button: `navigate('/create?template=' + encodeURIComponent(name))`

### Updated: `web/src/pages/TaskCreate.tsx`

- On mount: read `useSearchParams()` for `template` query param → call `api.getTemplate(name)` → set `generatedYAML`
- Inline `TemplateGallery` panel: add "Browse all templates →" link to `/templates`
- Template cards: add mode badge

### Updated: `web/src/App.tsx`

Add route: `<Route path="/templates" element={<TemplatesPage />} />`

### Updated: `web/src/components/Layout.tsx`

Add "Templates" nav link.

---

## Data Flow

```
/templates page
  → api.listTemplates() → card grid
  → click card → api.getTemplate(name) → YAML preview panel
  → "Use template" → navigate('/create?template=<name>')

/create?template=foo
  → mount effect: api.getTemplate('foo') → setGeneratedYAML(content)
  → YAML panel opens automatically

/create inline panel
  → existing toggle, unchanged except mode badge + "Browse all →" link
```

---

## Error Handling

- Unknown `?template` param: silently ignore (normal empty chat state)
- `api.getTemplate()` failure in TemplatesPage: inline error in preview panel
- Empty templates list: "No templates available" empty state
- YAML preview loading: spinner while fetching

---

## Testing

- `TaskCreate` mounts with `?template=foo` → `getTemplate` called → YAML panel populated
- `TemplatesPage` search filters cards by name/description
- `TemplatesPage` mode filter shows only matching templates

---

## Files Changed

| File | Change |
|------|--------|
| `web/src/pages/TemplatesPage.tsx` | Create |
| `web/src/pages/TaskCreate.tsx` | Add `?template` pre-fill + "Browse all" link + mode badges |
| `web/src/App.tsx` | Add `/templates` route |
| `web/src/components/Layout.tsx` | Add Templates nav link |

No backend changes needed.
