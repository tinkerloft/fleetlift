# Plan: Artifact & Output UX

**Date:** 2026-03-19
**Status:** Complete
**Motivation:** Workflow outputs (especially doc-assessment) are technically correct but practically unreadable. All the data is there — artifacts stored in DB, rich structured JSON output, fleet summary markdown — but the display layer buries it behind raw JSON and tiny artifact cards with no way to view or download content.

---

## Root cause summary

| Problem | Root cause |
|---------|-----------|
| Artifacts can't be viewed or downloaded | No `GET /api/artifacts/{id}/content` endpoint; frontend has no markdown renderer |
| Step panel shows raw JSON output | The schema-filtered JSON is pipeline data, not meant for humans; the readable artifact (`repo-report.md`) is invisible from the step panel |
| Run detail has no "result" surface | Final step's primary artifact not surfaced anywhere prominent |
| Inbox `output_ready` entry is useless | Links to run, not to the fleet-summary artifact |
| Inbox `notify` entry has no action | `kind: notify` shows summary text but no "View" button |
| ReportViewer shows artifact names only | No content fetch, no rendering, no download |

---

## Phase 1 — Artifact content endpoint (backend)

**Single new route:** `GET /api/artifacts/{id}/content`

- Auth: JWT + team ownership check (via `step_runs → runs → team_id`)
- Serve `data` bytes with `Content-Type` from the `content_type` column
- Add `Content-Disposition: inline; filename="{name}.md"` (or `attachment` if `?download=1`)
- For `storage: 'object_store'` (future): redirect to signed URL

Also add `GET /api/runs/{runID}/artifacts` (separate from the reports route) so the run detail page can list artifacts without going through the reports path. Or just reuse the reports endpoint — they're the same query.

**API client additions:**
```ts
getArtifactContent: (id: string) =>
  fetch(`/api/artifacts/${id}/content`).then(r => r.text())
getRunArtifacts: (runId: string) =>
  get<ListResponse<Artifact>>(`/reports/${runId}/artifacts`)
```

---

## Phase 2 — Markdown renderer + ArtifactViewer component

**New dependency:** `react-markdown` + `remark-gfm` (GitHub Flavoured Markdown — tables, task lists).

**New component: `ArtifactCard`**

Replaces the current "name + size" row in ReportViewer. Two states:

**Collapsed (default):**
```
┌────────────────────────────────────────────────────────┐
│ 📄 fleet-summary          text/markdown  5.7 KB        │
│                                    [Expand] [Download] │
└────────────────────────────────────────────────────────┘
```

**Expanded (inline):**
- Renders markdown using `react-markdown` + `remark-gfm` with Tailwind `prose` styling
- Max height 600px with scroll
- "Collapse" + "Download" buttons in header
- Download triggers `?download=1` fetch → `<a download>` click

**Full-screen modal (optional, Phase 3):** A `Dialog` variant for focus reading.

---

## Phase 3 — ReportViewer redesign

Replace the current `ReportViewer` layout (step cards with raw JSON + artifacts at the bottom) with an artifact-first layout:

```
Run: Documentation Assessment  [status badge]  [duration]  [cost]

── Artifacts ─────────────────────────────────────────────────────
  [ArtifactCard: fleet-summary.md]          ← expanded by default
  [ArtifactCard: repo-report (devex-gateway)]
  [ArtifactCard: repo-report (geppetto)]
  ...

── Steps (collapsed by default) ──────────────────────────────────
  ▸ assess-0  complete  £0.12
  ▸ assess-1  complete  £0.09
  ▸ collate   complete  £0.04
```

The `fleet-summary` artifact (or whichever artifact is on the final step, or the largest) auto-expands. Step output JSON is available but de-emphasised — behind a `▸` disclosure.

---

## Phase 4 — StepPanel artifact awareness

When a step has produced artifacts (query via step_run_id), surface them at the top of `StepPanel` above the output JSON:

```
Artifacts
  📄 repo-report.md  3.1 KB  [Expand] [Download]

Output
  { ... }  ← collapsed by default when artifact present
```

Requires fetching `GET /api/reports/{runId}/artifacts` and filtering by `step_run_id` client-side. Or add `GET /api/step-runs/{id}/artifacts` for directness.

This means when you click `assess-0` in the DAG, you see the readable report immediately — not the JSON.

---

## Phase 5 — Run detail hero panel

On `RunDetail`, when the run is complete and has artifacts:

1. Query `GET /api/reports/{runId}/artifacts`
2. Pick the "primary" artifact: prefer the one named `fleet-summary`, `report`, or `summary`; fall back to the artifact on the last completed step
3. Show it as a panel above the DAG, auto-expanded:

```
┌─ Documentation Assessment Result ───────────────────────────────┐
│ 📄 fleet-summary.md                         [Expand] [Download] │
│                                                                  │
│ ## Fleet Documentation Assessment                               │
│ *5 repositories assessed · avg score 2.9/5*                    │
│ ...                                                              │
└──────────────────────────────────────────────────────────────────┘

[DAG below]
```

This is the "result at a glance" experience. The run detail becomes a document, not a workflow trace.

---

## Phase 6 — Inbox improvements

**`output_ready` item:** When a run completes with artifacts, store the primary artifact's ID on the inbox item (add `artifact_id UUID` to `inbox_items` or embed in `summary`). The inbox card shows:

```
[output_ready badge]  Documentation Assessment
Fleet avg score 2.9/5, 5 repos need attention
[View Report →]  [View Run]
```

"View Report →" deep-links to `/reports/{runId}` with the artifact auto-expanded.

**`notify` item:** This already has a good summary. Add an action button:

```
[notify badge]  Fleet Documentation Assessment Complete — 5 repos, avg score 2.9/5
Assessment of 5 repos is complete. Fleet average...
[View Report →]
```

Link target: `/reports/{runId}`.

---

## Summary of changes

| Phase | Scope | Effort |
|-------|-------|--------|
| 1 | Backend: `GET /api/artifacts/{id}/content` endpoint | Small |
| 2 | Frontend: `react-markdown` dep + `ArtifactCard` component | Small |
| 3 | Frontend: `ReportViewer` redesign (artifact-first) | Small |
| 4 | Frontend: `StepPanel` artifact section | Small |
| 5 | Frontend: Run detail hero panel | Medium |
| 6 | Inbox: `output_ready` deep link + `notify` action button | Small |

Phases 1–4 can be done in a single session. Phase 5 is independent. Phase 6 has a small backend component (artifact_id on inbox_items).

---

## Open question

**Grouping inbox items by run:** The user noted inbox entries feel like "a long list of Review Output entries". For the doc-assessment run there are only 2 (one `notify`, one `output_ready`) so this isn't currently a problem. But at scale (running across 50 repos, collate + per-repo notify calls), grouping inbox items by run would be valuable. Deferred — not needed today.
