# Minion-Parity Design Spec

**Date:** 2026-03-25
**Status:** In progress — Phase 1 complete (PR #51 merged, PR #53 open), Phases 2–4 pending

---

## Goal

Add individual-developer task delegation to Fleetlift so it serves both fleet-wide operations (existing) and ad-hoc toil tasks (new), making it the single agentic platform for an SMB engineering organisation.

---

## Decisions

| Topic | Decision |
|---|---|
| Target audience | SMB — individual devs + platform teams, unified platform |
| UX entry point | Prompt-first Home screen at `/`; templates navigate to existing workflow pages |
| Quick run backend | New builtin `quick-run.yaml` passthrough workflow |
| Prompt improvement | Opt-in button → Minion-style side-by-side modal with quality scores |
| Prompt presets | Personal + team tiers, stored in DB |
| Personal quick-run space | Every user has a private personal team provisioned at auth time; quick-run uses this space |
| Personal team visibility | Personal teams are hidden from team pickers/JWT team role map and omitted from `/api/me` team list |
| Co-author attribution | Automatic — always inject triggering user's GitHub identity into sandbox |
| Model selection | Per-run override via run-level `model` param; applies to all workflows, not just quick-run |
| Implementation strategy | Phased — four independently shippable PRs |

---

## Implementation Progress

| Phase | PR | Status | Notes |
|---|---|---|---|
| 1 Backend | #51 | **Merged** | Auth foundation, model override flow, created_by filter, hidden flag, quick-run workflow |
| 1 Frontend | #53 | **Open** | HomePage, ModelSelect (backend-driven), nav, retry, log search. Design deviation: model list from `GET /api/models` (embedded YAML config) instead of hardcoded frontend constant |
| 2 Prompt improvement | — | Pending | |
| 3 Presets + saved repos | — | Pending | |
| 4 Co-author attribution | — | Pending | |

---

## Phases

### Phase 1 — Home + Quick Run

**Auth and tenancy foundation (backend-first, required for Home quick-run semantics)**

- Add `users.personal_team_id` and enforce one-personal-team-per-user with a unique partial index.
- On OAuth callback, provision a personal team transactionally when missing and persist `users.personal_team_id`.
- Exclude personal teams from team-role maps in issued JWTs and from `/api/me` response team lists so team UX remains focused on shared teams.
- Refresh flow rotates refresh token + rebuilds access token in a single DB transaction (`SELECT ... FOR UPDATE`) to avoid session burn on transient role/profile read failures.
- Harden auth handlers to fail closed on role/admin/profile read errors (`500`), with explicit `401` only for true auth failures.
- Refresh auth error routing uses typed/sentinel errors (`errors.Is`) instead of string matching so `401` vs `500` behavior is explicit and regression-testable.

**New page: `HomePage` at `/`**

- Top zone: prompt textarea, repo URL input, branch input (default `main`), "✦ Improve" button, "Run →" button.
- Bottom zone: template grid — existing builtin workflows as clickable cards. Clicking navigates to `/workflows/:id` unchanged. "View all →" link for custom workflows.
- Right/below: recent tasks list — last 10 runs scoped to `created_by = current user`, showing status badge, title, repo, elapsed time, Retry button per item.
- Nav change: add "Home" item at top; keep "Workflows" for platform teams.
- Current `/` redirect (`/` → `/workflows`) replaced by `HomePage`.

**New builtin workflow: `internal/template/workflows/quick-run.yaml`**

- Slug: `quick-run`
- Required params: `prompt` (string), `repo_url` (string)
- Optional params: `branch` (string, default `main`)
- Single execution step: runs Claude Code agent with the user's prompt against the cloned repo on a new branch (`agent/quick-run`).
- No `create_pr` param. PR creation is always the expected outcome — the agent opens a PR if the changes are of sufficient quality, and skips it otherwise. This is the Stripe Minions default: ship unless quality isn't there, not ship only if the user opts in.
- No output schema, no fan-out, no HITL.
- Frontend calls `api.createRun('quick-run', { prompt, repo_url, branch })` — the `quick-run` slug is looked up by the Home page directly.

**API additions**
- `GET /api/runs?created_by=me&limit=10` — existing runs endpoint gains `created_by=me` filter (resolves to JWT subject). Used by the recent tasks list.

**Retry**
- "Retry" on the recent tasks list re-POSTs the original run's params to `POST /api/runs`. No new API needed — just a frontend mutation that reads `run.params` and resubmits.
- Retry also added to the existing `RunDetail` page as a button alongside Cancel.

**Log search**
- Frontend-only enhancement to the existing `LogStream` component on `RunDetail`.
- A search input appears above the log stream. As the user types, log lines that don't match the filter are hidden (case-insensitive substring match). Matching text is highlighted.
- No backend change. Works on both live (SSE) and completed (historical) log streams.

**Model selection**

Model is a run-level override, not a workflow-level param — it applies to any workflow, including quick-run and all builtins.

Backend:
- Add `model` (optional string) to `POST /api/runs` request body. Server stores it on the `runs` row (new nullable `model` column, migration required).
- Add `ModelOverride string` to `workflow.StepInput`. DAGWorkflow reads `run.model` and populates it for every step.
- `ClaudeCodeRunner.Run()` passes `--model <value>` to the Claude Code CLI when `ModelOverride` is set. If unset, CLI uses its own default.

Frontend:
- Home page: model dropdown next to branch input. Options: `claude-opus-4-6` (default label: "Opus 4.6"), `claude-sonnet-4-6` ("Sonnet 4.6"), `claude-haiku-4-5` ("Haiku 4.5"). Stored in a constant list in the frontend — no API needed.
- WorkflowDetail run form: same model dropdown added alongside existing params.
- Selected model persists in `localStorage` per user as a default for future runs.

---

### Phase 2 — Prompt Improvement

**New server endpoint: `POST /api/prompt/improve`**

Request:
```json
{ "prompt": "Fix the null pointer in auth/handler.go line 42" }
```
Response:
```json
{
  "improved": "<task>Fix null pointer...</task><context>...</context>",
  "scores": {
    "clarity":   { "rating": "good",      "reason": "..." },
    "context":   { "rating": "good",      "reason": "..." },
    "structure": { "rating": "poor",      "reason": "..." },
    "guidance":  { "rating": "poor",      "reason": "..." }
  },
  "summary": "The rewritten prompt adds explicit role, context, and success criteria."
}
```
- Calls Claude API server-side (uses `ANTHROPIC_API_KEY` from env). Never exposes the key to the browser.
- Ratings: `excellent | good | poor`. Four dimensions: clarity, context, structure, guidance.
- Responds in < 5 s for typical prompts (target).

Backend implementation notes:
- Handler accepts a `PromptImprover` interface (not a concrete Anthropic client) so unit tests can inject a mock without hitting the real API.
- All error responses use the existing `writeJSONError()` helper — never raw `fmt.Sprintf` JSON (prevents JSON injection from error strings).
- Handler is wired through the `Deps` struct like all other handlers (`Prompt *handlers.PromptHandlers`), constructed in `cmd/server/main.go`.
- Route is registered inside the authenticated `r.Group` block in `router.go` alongside other `/api/` routes.

**Prompt improvement modal (frontend)**

- Triggered by "✦ Improve" button on Home page.
- Full-screen overlay. Two columns: Original (left, red header) | Improved (right, green header).
- Each column shows the prompt text and score badges below it (colour-coded by rating).
- Bottom bar: summary sentence + "Decline" (closes modal, keeps original) + "Use improved →" (replaces textarea content, closes modal).
- If the API call fails or times out: modal shows inline error with a Retry button. Declining closes the modal and leaves the textarea unchanged.
- `PromptZone` manages its own `showImproveModal` state. The Improve button sets this state + triggers the API call via `useMutation`. The modal receives the original prompt, mutation result, and accept/decline callbacks. Accept replaces the textarea content within `PromptZone`'s local state.

---

### Phase 3 — Prompt Presets + Saved Repos

**New DB migration: `NNN_prompt_presets.up.sql`**

```sql
CREATE TABLE prompt_presets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    scope       TEXT NOT NULL CHECK (scope IN ('personal', 'team')),
    title       TEXT NOT NULL,
    prompt      TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ON prompt_presets (team_id, scope, created_by);
```

**API routes**

| Method | Path | Description |
|---|---|---|
| GET | `/api/presets` | List presets for current user (personal + team) |
| POST | `/api/presets` | Create a preset (`scope: personal\|team`) |
| PUT | `/api/presets/:id` | Update title or prompt |
| DELETE | `/api/presets/:id` | Delete (own presets only; team presets require admin) |

**Frontend**

- Presets sidebar on Home page: two sections — "My Presets" and "Team Presets".
- Clicking a preset populates the textarea.
- "Save as preset" option appears below the textarea after typing (or after using an improved prompt).
- On save: modal asks for title + scope (personal / team).
- Team presets: visible to all team members, editable only by creator or team admin.

**Saved repos**

New DB migration alongside presets:
```sql
CREATE TABLE user_repos (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id    UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    url        TEXT NOT NULL,
    label      TEXT,                    -- optional display name, e.g. "backend"
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, url)
);
```

API routes:

| Method | Path | Description |
|---|---|---|
| GET | `/api/saved-repos` | List saved repos for current user |
| POST | `/api/saved-repos` | Add a repo (`url`, optional `label`) |
| DELETE | `/api/saved-repos/:id` | Remove a saved repo |

Frontend:
- Repo URL input on Home page becomes a combobox: dropdown lists saved repos (showing label or URL), with a "Add new URL..." option at the bottom that falls back to free-text entry.
- When a user successfully runs against a repo not yet saved, a subtle prompt appears: "Save this repo for quick access?" with a one-click confirm.
- Repos are per-user (not team-shared) — each dev manages their own list.

---

### Phase 4 — Co-Author Attribution

**Where:** `internal/activity/execute.go` — sandbox env injection before agent execution.

**How:**
- Add `CreatedBy string` to `workflow.StepInput` so the activity receives the user ID directly (avoids an extra DB round-trip to look up `run.created_by`). The DAGWorkflow already has this value from the run record and passes it through.
- Add a `LookupUserGitIdentity(ctx, userID)` DB query that returns `(name, email)` from the `users` table (populated during GitHub OAuth).
- Inject two env vars into the sandbox before agent execution:
  - `GIT_AUTHOR_NAME` = user's GitHub display name
  - `GIT_AUTHOR_EMAIL` = user's GitHub email (or `<login>@users.noreply.github.com` if private)
- The `CreatePullRequest` activity's commit step inherits these env vars — no change needed there.
- If `LookupUserGitIdentity` returns no result (service account runs, scheduled runs): fall back to existing `GIT_USER_NAME` / `GIT_USER_EMAIL` env vars. No failure.

---

## Follow Up (out of scope for this spec — tracked in ROADMAP.md)

- **"Follow Up" task chaining** — button on RunDetail that pre-populates the Home prompt box with context from the completed run (output summary, repo, branch). Tracked in roadmap as Track L.
- Inbound Slack trigger (`/fleetlift run ...`)
- GitHub webhook trigger (PR comment `@fleetlift fix this`)

---

## Resolved Decisions

1. **Team preset deletion:** Creator-only. No role system exists; adding one is out of scope. Team admins can delete via direct DB if needed until a role system exists.
2. **`quick-run` in Workflows list:** Hidden. Add a `hidden: true` top-level field to builtin YAML (requires a one-line model change in `internal/template/builtin.go` to skip hidden slugs when listing templates). Only accessible via Home's Run button.
3. **Recent tasks scope:** All runs by the current user, not just quick-run runs. Gives a unified view of their work across both ad-hoc and workflow-triggered runs.
