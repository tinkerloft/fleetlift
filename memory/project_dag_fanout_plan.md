---
name: DAG fan-out + UI redesign — full implementation plan
description: Complete phased plan for DAG visualisation improvements, per-step inputs, partial failure handling, and UI visual redesign
type: project
---

Agreed in session 2026-03-19. Mockup at `/tmp/dag-mockup.html`.

---

## Phase 0 — Visual redesign (no backend changes)

Pure CSS/component work. Ships independently, improves whole app immediately.

1. **`index.css` token update** — deepen dark mode surface stack, add indigo accent, sharpen muted text contrast:
   - `--background: 222 20% 7%` · `--card: 225 18% 11%` · `--muted: 223 16% 14%`
   - `--border: 224 14% 16%` · `--accent: 234 89% 73%` (indigo) · `--muted-foreground: 224 12% 50%`

2. **Typography** — add IBM Plex Sans + IBM Plex Mono to `index.html`, update `body` font-family and `.font-mono` rule.

3. **`DAGNode.tsx` rewrite** — match mockup: dot + title + repo sub-label + meta row. Add `DAGNodeCollapsed` variant for aggregated fan-out (Phase 3). Remove left accent bar in favour of dot glow.

4. **Status glow CSS** — `box-shadow` glow on status dots matching status colour; pulse animation on running/cloning.

---

## Phase 1 — Per-step resolved inputs (data foundation)

Every step_run records exactly what it received. Enables Phase 2 and Phase 3 labels.

- Migration: add `input JSONB` column to `step_runs`
- Extend `CreateStepRunActivity(runID, stepID, title, temporalWFID, input map[string]any)`
- **Fan-out path** (`dag.go`): pass `{"repo_url": repo.URL, "ref": repo.Ref}` for each fan-out instance
- **Single-step with repos**: same shape
- **Action / no-repo steps**: nil
- Add `Input map[string]any` to `StepRun` model + API response (`SELECT *` already covers it)

---

## Phase 2 — Step detail panel

- `StepPanel`: show `stepRun.input` as "Step inputs" section (specific repo for fan-out; fall back to run params for steps with no input)
- Fix prior-outputs filter: fan-out siblings (`assess-0`, `assess-1`, …) must not appear as "prior step outputs" for each other — filter by base step ID
- Step list items: add `input.repo_url` (last path component) as sub-label under step title
- Detail panel becomes slide-up overlay (`position: absolute; bottom: 0` + CSS transform transition)

---

## Phase 3 — DAG collapse + identity labels

**`FAN_OUT_COLLAPSE_THRESHOLD = 6`**

- N ≤ 6: individual nodes. Label = `{step title} · {repo name} ({i}/{N})` using `input.repo_url`.
- N > 6: single aggregated node — `{step title} ({N} repos)` — with:
  - Inline status bar (green/red/grey segments, labelled counts)
  - Colour = worst status
  - Click → filters step list panel to that step's fan-out instances (no DAG expansion, no modal)
- "Show all" clears the filter

---

## Phase 4 — Partial fan-out failure → inbox resolution

**Problem**: `aggregateFanOut` hard-fails if any child fails. With 3/25 repos failing, 22 valid results are discarded.

**New behaviour**:
1. After `fanWg.Wait()`: detect partial failure (`successCount > 0 && failCount > 0`)
2. Create inbox item (on DAGWorkflow) with kind `fan_out_partial_failure`:
   - Title: `Fan-out partial failure: {step.ID} ({failCount}/{total} repos failed)`
   - Body: failed repo names + error reasons + **link to `/runs/{runID}`**
3. Wait on new signal `fan_out_resolve` (targets DAGWorkflow via `temporal_id` on `runs`, not child StepWorkflows)
4. Payload `{"action": "proceed" | "terminate"}`
   - `proceed`: strip failed results, mark aggregate complete, continue downstream with successful outputs only
   - `terminate`: return error as today
5. UI: inbox item shows "Proceed with N results" + "Terminate" buttons; run detail shows banner with link to inbox item

**Signal routing**: new route in server signal handler — `fan_out_resolve` → `runs.temporal_id`.

**Integration with `failure_threshold`**: if `failCount > failure_threshold` (YAML field already exists), hard-fail immediately without raising inbox item. Only raise inbox item when failure is within threshold.

---

## Open questions (resolved)

- Q: Store per-step inputs for non-fan-out steps too? **A: Yes — all steps.**
- Q: Individual node label format? **A: Both repo name suffix AND index.**
- Q: Collapsed node click → expand or filter? **A: Filter step list only.**
- Q: Inbox alert link from run detail? **A: Yes.**
