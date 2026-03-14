# Web Interface Visual Polish Plan

**Date:** 2026-03-14
**Status:** Draft

## Diagnosis

The current UI is structurally sound — good component hierarchy, consistent use of Tailwind design tokens, proper dark mode support, clean sidebar navigation. But it reads like a scaffolded admin panel. Everything is the same visual weight: plain `rounded-lg border p-4` containers, uniform gray borders, no visual hierarchy beyond font size. The result is functional but flat.

Specific issues:

### 1. DAG Graph — Clunky

The DAG uses ReactFlow with inline styles and solid-color rectangles as nodes. Problems:

- **Nodes are colored blobs.** A solid `#3b82f6` rectangle with white text looks like a wireframe placeholder, not a finished component. No icon, no status text, no secondary information.
- **Fixed 400px height** regardless of graph complexity. A 2-step workflow wastes space; a 10-step fan-out gets cramped.
- **No edge labels or style variation.** All edges are identical gray lines — you can't tell dependency relationships from the visual alone.
- **Layout is naive.** `col * 220, level * 120` hardcoded spacing. Nodes at the same level don't center relative to the graph — they start at x=0.
- **No node content.** Nodes show only a title. No status label, no duration, no step mode indicator.

### 2. Workflow Pages — Clinical

**WorkflowList:** A grid of identical bordered boxes. No visual distinction between workflow types (bug-fix vs audit vs migration). No indication of recent activity or popularity. The tag badges are the only visual variety.

**WorkflowDetail:** Three stacked sections (DAG preview, parameters form, YAML dump) with identical visual treatment. The DAG preview shows an empty graph (no step runs). The parameter form is raw inputs with no grouping. The YAML view is a raw `<pre>` block in a `<details>` tag.

### 3. Run Detail — Functional but Dense

The run page is the most information-dense page and handles it reasonably, but:
- Step list is a plain `divide-y` list with no visual affordance for selection
- No timeline or duration information visible
- The "click a node, see the panel below" interaction has no visual connection between the DAG and the panel
- Output and diff are raw `<pre>` blocks with no syntax highlighting

### 4. Global Patterns

- **Loading states** are a generic spinner + "Loading..." text everywhere. No skeleton placeholders.
- **Empty states** are bare text ("No runs yet") — no illustration or call to action.
- **Status badges** work but are uniform in weight — "running" should feel more alive than "pending."
- **The log terminal** looks good (dark bg, green text) but is disconnected visually from the rest of the page.

---

## The "Whoa" Moment

FleetLift's power isn't obvious from a workflow list or a parameter form. The moment a user *gets it* is when they watch a run execute: agents fanning out across a fleet of repos, steps lighting up in parallel, diffs streaming in, results aggregating — all orchestrated automatically. The UI needs to make that moment *visible*, not just functional.

Today, watching a run execute looks like... a table with status badges updating. The DAG is colored blobs. The logs are a green-on-black box. There's no sense of orchestration happening. A user can't *feel* that 10 agents just spun up across 10 sandboxes and are working simultaneously.

The goal is: a user shows FleetLift to a colleague, kicks off a fleet-wide audit, and the colleague watches the DAG light up step by step, sees the fan-out explode into parallel lanes, watches results stream in — and immediately understands what this thing does. That's the whoa moment. It comes from making the *execution visible*, not from adding decorative elements.

This means the run detail page is the highest-impact target, not the workflow list.

## Design Principles

1. **Professional, not flashy.** Think Linear, Vercel dashboard, Railway — tools that look serious. No gradients, no glow effects, no AI-slop aesthetic. The visual language should say "infrastructure platform," not "AI demo."
2. **Information density over decoration.** Every visual change should communicate something. No purely decorative elements. If a node pulses, it's because the agent is running. If an edge animates, it's because data is flowing.
3. **Make orchestration visible.** The unique value of FleetLift is parallel agent orchestration. The UI should make you *see* the parallelism, the dependencies, the fan-out, the aggregation. This is what creates the whoa moment — not polish for its own sake.
4. **No new UI libraries.** Work within Tailwind + Radix + ReactFlow. Shadcn is already the implicit base.
5. **Preserve the structure.** Keep the sidebar, the page hierarchy, the component composition. This is polish, not redesign.

---

## Changes

### Phase 1: DAG Graph Overhaul

The DAG is the centerpiece of both the workflow detail and run detail pages. Making it look good has outsized impact.

**Custom node component** replacing inline styles:

```
┌─────────────────────────┐
│ ○ Analyze bug           │  ← status dot (colored) + title
│ report · 2m 34s         │  ← mode badge + duration (if running/complete)
└─────────────────────────┘
```

- Replace solid-color rectangles with bordered nodes that have a **left accent bar** in the status color (like a Kanban card). White/dark background, not colored background.
- Add a small status dot (pulsing for `running`) next to the title.
- Show step mode as a subtle label (`report`, `transform`, `shell`).
- Show elapsed duration for running/completed steps.
- Selected node: heavier border + subtle shadow instead of the current white border hack.

**Improved edges:**
- Use `smoothstep` edge type instead of default straight lines (ReactFlow supports this natively).
- Animated dashed stroke for edges feeding a running node.
- Completed edges fade to a lighter color.

**Better layout:**
- Center nodes at each level relative to the graph width.
- Dynamic height: `min(400px, levels * 140 + 100)` — shorter graphs use less space.
- Increase vertical spacing slightly for readability.

**Files:** `web/src/components/DAGGraph.tsx` (major rewrite), new `web/src/components/DAGNode.tsx` custom node component.

### Phase 2: Workflow Pages

**WorkflowList — add visual identity per workflow:**
- Add a subtle **icon or color accent** per workflow type. Map workflow tags to an icon from Lucide (e.g., `Shield` for audit, `Bug` for bug-fix, `GitBranch` for migration, `Search` for research). Display in the card header.
- Show **step count** and **mode summary** (e.g., "4 steps · transform") as a small detail line.
- Add a subtle hover elevation (shadow increase) instead of just border color change.

**WorkflowDetail — structure the page better:**
- **Hero section:** Workflow title + description with more breathing room. Show the icon and a summary line ("5 steps, 2 approval gates, fan-out on step 3").
- **DAG preview:** Use the improved DAG component. Show step titles and modes even without runs.
- **Parameters form:** Group required vs optional parameters. Add a subtle card background to the form area to distinguish it from the DAG.
- **YAML view:** Use CodeMirror (already a dependency for YAML editing elsewhere) with syntax highlighting instead of raw `<pre>`. Keep it in a collapsible section but make the toggle more prominent.

**Files:** `web/src/pages/WorkflowList.tsx`, `web/src/pages/WorkflowDetail.tsx`.

### Phase 3: Run Detail — The Whoa Moment Page

This is the page that sells FleetLift. When a user watches a run execute, they need to *see* the orchestration.

**Header improvement:**
- Add a **live duration counter** next to the status badge (e.g., "running · 4m 12s" ticking up, or "completed in 12m 03s"). This creates a sense of time and progress that the current static badges lack.
- Show a compact progress indicator: `3 of 7 steps` with a subtle segmented bar (one segment per step, colored by status). This gives instant context for how far through the run we are.

**DAG as the primary view (not the step list):**
- The DAG should be the default way to navigate a run, not the step list. Make it taller (responsive to content) and give it prominence.
- Fan-out visualization: When a step fans out, show it expanding into multiple parallel lanes. This is the single most impressive visual in the product — a step that was one node becomes 10 parallel execution lanes. Today this isn't visualized at all because the DAG is built from `step_runs` which flattens fan-out.
- Add a subtle **data flow animation** on edges when a step completes and its output feeds the next step. A brief pulse along the edge, like data moving through a pipeline. This makes the DAG feel alive during execution.

**Step list becomes a vertical timeline:**
- Replace the flat `divide-y` list with a timeline component: vertical line connecting status dots, step title and duration on the right. This echoes CI/CD pipeline UIs (GitHub Actions, GitLab pipelines) which users already understand.
- Running steps show elapsed time ticking. Completed steps show total duration.
- The timeline and DAG stay in sync — clicking either navigates to the step panel.

**Step detail panel — connected to the DAG:**
- When a step is selected (via DAG click or timeline click), slide the panel in below the DAG with a brief transition. This creates a visual cause-and-effect: "I clicked a node, here's what it produced."
- Add a subtle breadcrumb or back link at the top of the panel to return to the overview.

**Output improvements:**
- **Diff view:** Color `+` lines green, `-` lines red, `@@` headers blue. Simple CSS classes applied during render — no library needed. Diffs are one of the most tangible outputs of a transform step; making them look like a real diff viewer (GitHub-style) reinforces that FleetLift produced real code changes.
- **JSON output:** Subtle syntax coloring (keys one color, strings another, numbers another). A simple recursive renderer is enough.

**Log terminal refinement:**
- Add a thin header bar: step name, pulsing status dot, "scroll to bottom" button when the user has scrolled up.
- Auto-scroll behavior is already good — keep it.

**Files:** `web/src/pages/RunDetail.tsx`, `web/src/components/StepPanel.tsx`, `web/src/components/LogStream.tsx`, `web/src/components/StepTimeline.tsx` (new).

### Phase 4: Global Polish

**Status badges — add visual weight:**
- `running`: Add a subtle pulsing dot before the text (like the HITL panel already does).
- `awaiting_input`: Amber/yellow variant (currently uses `default` which is neutral).
- `complete`: Green tint.
- `failed`: Already uses `destructive` — good.

**Loading states:**
- Replace "Loading..." text with skeleton placeholders on the workflow list (3 gray card outlines) and run list (3 gray table rows). Simple CSS animation, no library needed.

**Empty states:**
- Add a muted icon above empty state text. Inbox empty: `InboxIcon` from Lucide with muted color. Runs empty: `ActivityIcon`. Knowledge empty: `BookOpenIcon`.
- Add a call-to-action where relevant ("No runs yet" → button to go to workflows).

**Spacing and typography micro-adjustments:**
- Page titles: Add a subtle description line or breadcrumb below the title.
- Section headers within pages: Use a bottom border or subtle background stripe to separate sections visually.
- Monospace text (IDs, YAML, JSON): Use a slightly different background (`bg-muted/50`) so code blocks feel embedded rather than floating.

**Files:** `web/src/components/ui/badge.tsx`, `web/src/index.css` (minor additions), multiple page files.

---

## What NOT to Change

- **Sidebar navigation** — works well, clean, good use of icons. Don't touch it.
- **HITL panel** — already has good visual treatment (yellow border, pulsing dot, clear actions).
- **Color palette** — the HSL design tokens are well-chosen. No need for new colors.
- **Dark mode** — works, don't break it.
- **Login page** — fine, it's seen once.

---

## Implementation Order

| Phase | Effort | Impact | Dependencies |
|-------|--------|--------|-------------|
| 1. DAG overhaul | Medium | **Highest** | None — self-contained component |
| 3. Run detail | Medium | **Highest** | Benefits from Phase 1 DAG |
| 4. Global polish | Small | Medium | None — independent changes |
| 2. Workflow pages | Small | Medium | Benefits from Phase 1 DAG |

**Start with Phase 1 + 3 together.** The DAG and the run detail page are where the whoa moment lives. A polished workflow list is nice but it's not what makes someone understand FleetLift's power. Phase 4 (global polish) can be interleaved as cleanup.

Total scope: ~15-18 files touched, no new dependencies, no backend changes.

---

## What This Is Not

This plan avoids:

- **AI-demo aesthetic** — no gradient meshes, no "magic sparkle" animations, no purple-to-blue hero gradients. FleetLift is infrastructure tooling for engineering teams. It should look like it was built by people who care about software, not by a marketing team.
- **Feature additions disguised as polish** — no new functionality. The backend and API are unchanged. This is purely visual.
- **Component library migration** — we're not switching to a different component system. Tailwind + Radix + the existing ui/ components are the foundation.
- **Excessive animation** — motion is used to communicate state (agent running, data flowing, step completing), not for decoration. Every animation should be removable without losing functionality.
