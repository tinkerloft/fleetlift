# Fleetlift Enhancement Backlog

## UI / Run Experience

- **Run duration in runs list** — show total duration (started_at → completed_at) in the runs list view. Data already exists on the `runs` table; needs a computed display column in the frontend.

- **Run cost tracking** — collect per-run cost from agent metadata output. Claude's `result` event includes `total_cost_usd` and per-model `costUSD` in `modelUsage`. Aggregate across steps, store on `runs` table (new `total_cost_usd` column), display in runs list and run detail.

- **Run detail as step sequence** — replace or supplement the DAG view with a sequential step list showing a running spinner on the active step, similar to Fleetlift v1's run progress display.

- **Cancel workflow from CLI** — cancel a running DAGWorkflow from the `fleetlift` CLI. Backend signal routing already exists; needs CLI command wiring.

- **Dark mode toggle** — add a theme toggle to the header bar. The `<header>` in `Layout.tsx` has a right-aligned slot designed for this.

- **Visual polish** — general UI improvements (no specific scope defined yet).

## Inbox Notifications

Basic run-completion inbox items are wired up. Remaining enhancements:

- **HITL inbox notifications** — when a step enters `awaiting_input`, create an inbox item so users don't have to poll the runs list. Include step title and approval context.

- **Per-step failure notifications** — when a non-optional step fails, create an inbox item immediately (don't wait for full DAG completion).

- **Inbox item detail view** — expandable inline preview of step output. For HITL items, show approve/reject/steer buttons directly in the inbox.

- **Inbox badge in sidebar** — show unread count on the Inbox nav link. Auto-refresh.

- **Notification preferences** — per-team/user settings for which events create inbox items. Optional webhook/email dispatch.

See `docs/plans/inbox-notifications.md` for the detailed implementation plan.

## Agent / Sandbox

- **Structured output enforcement** — Claude's `output.schema` in workflow YAML is currently documentation-only. Consider instructing the agent to output structured JSON matching the schema, or post-processing the text result to extract fields.

- **Agent output normalization** — step outputs currently contain the full Claude result event (with metadata like cost, duration, token counts). Extract the meaningful `result` text and agent metadata into separate fields so downstream templates and the UI get cleaner data.

## Miscellaneous
- Sort workflows by name
