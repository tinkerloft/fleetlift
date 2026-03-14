# Inbox Notifications Enhancement Plan

> **For agentic workers:** Use superpowers:subagent-driven-development or superpowers:executing-plans to implement.

**Goal:** Rich, actionable inbox notifications for all workflow lifecycle events.

**Architecture:** Extend CreateInboxItem calls throughout DAG/Step workflows. Add inbox item detail view in frontend. Support notification preferences per-team.

---

## Current State

- `CreateInboxItem` activity exists, creates rows in `inbox_items` table
- Inbox UI lists items with "Action Required" / "Output Ready" badges
- Basic: run completion now creates an "output_ready" inbox item (just merged)
- Missing: HITL notifications, per-step notifications, rich detail, notification preferences

## Proposed Enhancements

### Task 1: HITL Inbox Notifications

**Files:**
- Modify: `internal/workflow/step.go` (awaiting_input section)

When a step enters `awaiting_input` state, create an "awaiting_input" inbox item with the step title and a summary of what's needed. This surfaces HITL requests in the inbox so users don't have to poll the runs list.

- [ ] Add `CreateInboxItemActivity` call after setting step status to `awaiting_input`
- [ ] Include step title and approval policy in the summary
- [ ] Test with a workflow that has `approval_policy: always`

### Task 2: Per-Step Failure Notifications

**Files:**
- Modify: `internal/workflow/dag.go` (result collection loop)

When a non-optional step fails, create an inbox item immediately (don't wait for run completion). This gives faster feedback than waiting for the full DAG to finish.

- [ ] After detecting a failed step in the results loop, call `CreateInboxItemActivity`
- [ ] Include step name and error message in summary
- [ ] Only for non-optional steps (optional failures are expected)

### Task 3: Inbox Item Detail View

**Files:**
- Modify: `web/src/pages/Inbox.tsx`
- Modify: `web/src/api/types.ts`
- Modify: `internal/server/handlers/inbox.go`

Currently inbox items link to the run page. Enhance the inbox to show a preview of the step output inline, and for HITL items, show approve/reject buttons directly in the inbox.

- [ ] Add expandable detail section to inbox items
- [ ] For "output_ready": show step output summary inline
- [ ] For "awaiting_input": show approve/reject/steer buttons inline
- [ ] Add API endpoint to fetch inbox item detail (step output, run context)

### Task 4: Notification Preferences

**Files:**
- Create: `internal/db/migrations/XXX_notification_prefs.sql`
- Create: `internal/server/handlers/notifications.go`
- Modify: `web/src/pages/Settings.tsx` (or create)

Per-team/user notification preferences: which events generate inbox items, optional webhook/email dispatch.

- [ ] Schema: `notification_preferences` table (team_id, user_id, event_type, enabled, webhook_url)
- [ ] API: CRUD endpoints for preferences
- [ ] UI: Settings page with toggles per event type
- [ ] Check preferences before creating inbox items in workflows

### Task 5: Inbox Badge / Count in Sidebar

**Files:**
- Modify: `web/src/components/Sidebar.tsx` (or layout component)
- Modify: `web/src/api/client.ts`

Show unread inbox count as a badge on the Inbox nav item. Auto-refresh.

- [ ] Add unread count API endpoint (or derive from existing list)
- [ ] Show badge on Inbox nav link
- [ ] Auto-refresh count on interval

## Unanswered Questions

1. Should failed runs also create "action_required" items (prompting the user to investigate), or just "output_ready"?
2. Should there be a "run_started" notification type, or is that too noisy?
3. Email/Slack notification dispatch — should that be a separate plan or included here?
4. Should inbox items auto-dismiss when the run they reference reaches a terminal state?
