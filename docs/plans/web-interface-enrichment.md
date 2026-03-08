# Web Interface Enrichment Plan

Make the web interface fully capable — everything a user can do via the CLI should be doable through the browser.

## Gap Analysis

| CLI Command | Current Web Support | Work Needed |
|---|---|---|
| `run` (start workflow) | None | New API + task creation UI |
| `create` (AI-assisted YAML) | None | New API + chat UI |
| `templates list` | None | New API + gallery UI |
| `status` | Partial (detail page) | Already covered |
| `result` (final result + PRs) | None | New API + result view |
| `reports` (report mode) | None | New API + report viewer |
| `diff` | Yes | Already covered |
| `logs` | Yes | Already covered |
| `approve/reject/cancel` | Yes | Already covered |
| `steer` | Yes | Already covered |
| `continue` | Yes | Already covered |
| `list` | Yes | Already covered |
| `retry` (failed groups) | None | New API + retry button |
| `knowledge list/show/add/delete` | None | New API + knowledge UI |
| `knowledge review` | None | New API + review queue UI |
| `knowledge commit` | None | New API + commit action |
| Temporal UI | None | iframe embed |

---

## Phase 1: Backend API Additions

All new endpoints added to `internal/server/`. New handler files to keep things organized.

### 1A. Task Submission API

**File:** `internal/server/handlers_submit.go`

```
POST /api/v1/tasks
```

Accept a task definition (JSON matching model.Task) and start a Temporal workflow. This is what `fleetlift run` does internally via `client.StartTransform()`.

Request body mirrors the Task YAML schema but in JSON:
```json
{
  "version": 1,
  "id": "my-task",
  "title": "Migrate to slog",
  "mode": "transform",
  "repositories": [{"url": "https://...", "branch": "main"}],
  "execution": {
    "agentic": {
      "prompt": "...",
      "verifiers": [{"name": "build", "command": ["go", "build", "./..."]}]
    }
  },
  "require_approval": true,
  "timeout": "30m"
}
```

Response: `{"workflow_id": "transform-xxx-123"}`

**Implementation:**
- Add `StartTransform(ctx, task model.Task) (string, error)` to the `TemporalClient` interface in `client_iface.go`
- Handler: parse JSON → validate required fields (title, execution, repos/groups) → call `client.StartTransform()` → return workflow ID
- Reuse validation logic from `cmd/cli/main.go` run command

### 1B. Task Result API

**File:** `internal/server/handlers_tasks.go` (extend existing)

```
GET /api/v1/tasks/{id}/result
```

Returns the final workflow result (what `fleetlift result` shows). Calls `client.GetWorkflowResult()`.

Response (transform mode):
```json
{
  "task_id": "my-task",
  "status": "completed",
  "duration_seconds": 342,
  "repositories": [
    {
      "name": "service-a",
      "status": "completed",
      "files_modified": 5,
      "pull_request": {
        "number": 42,
        "url": "https://github.com/org/service-a/pull/42",
        "branch": "auto/slog-migration"
      }
    }
  ]
}
```

Response (report mode):
```json
{
  "task_id": "audit",
  "status": "completed",
  "repositories": [
    {
      "name": "service-a",
      "status": "completed",
      "report": {
        "frontmatter": {"auth_library": "JWT", "score": 8},
        "body": "## Analysis\n..."
      },
      "for_each_results": [...]
    }
  ]
}
```

**Implementation:**
- Add `GetWorkflowResult(ctx, workflowID) (*model.TaskResult, error)` to `TemporalClient` interface
- This blocks until workflow completes — for the web, make it non-blocking: return current result if available, or 202 Accepted with status if still running

### 1C. Retry API

**File:** `internal/server/handlers_submit.go`

```
POST /api/v1/tasks/{id}/retry
```

Request body:
```json
{
  "task": { ... },
  "failed_only": true
}
```

The task JSON is the original task definition. Server reads the completed workflow result, filters groups to failed-only, starts a new workflow.

Response: `{"workflow_id": "transform-xxx-456"}`

**Implementation:**
- Get result from original workflow
- Extract failed group names
- Filter task.Groups to only include failures
- Call `StartTransform()` with filtered task

### 1D. Templates API

**File:** `internal/server/handlers_templates.go`

```
GET /api/v1/templates
```

Returns all built-in + user templates.

Response:
```json
{
  "templates": [
    {
      "name": "dependency-upgrade",
      "description": "Upgrade a dependency across repositories",
      "content": "version: 1\ntitle: ..."
    }
  ]
}
```

```
GET /api/v1/templates/{name}
```

Returns a single template's full content.

**Implementation:**
- Extract template definitions from `cmd/cli/create.go` into a shared `internal/templates/` package
- Server loads built-in templates + scans `~/.fleetlift/templates/`

### 1E. AI-Assisted Task Creation API

**File:** `internal/server/handlers_create.go`

```
POST /api/v1/tasks/generate
```

Request:
```json
{
  "description": "Migrate all Go services from log to slog",
  "repositories": ["https://github.com/org/service-a"],
  "conversation_id": null
}
```

Response (first turn):
```json
{
  "conversation_id": "conv-abc",
  "yaml": "version: 1\ntitle: ...",
  "message": "I've generated a task to migrate from log to slog. The task includes build and test verifiers.",
  "complete": true
}
```

For interactive multi-turn mode, subsequent requests include `conversation_id` and the user's follow-up message:

```json
{
  "conversation_id": "conv-abc",
  "message": "Also add a lint verifier"
}
```

**Implementation:**
- Extract Claude API logic from `cmd/cli/create.go` into `internal/create/generator.go`
- Server manages conversation state in memory (map[string][]messages with TTL)
- Reuse system prompt and YAML extraction logic
- Return both the generated YAML and Claude's explanatory message

### 1F. Knowledge API

**File:** `internal/server/handlers_knowledge.go`

```
GET    /api/v1/knowledge                     — list all items (filters: ?task_id, ?type, ?tag, ?status)
GET    /api/v1/knowledge/{id}                — show single item
POST   /api/v1/knowledge                     — create item
PUT    /api/v1/knowledge/{id}                — update item (edit summary, approve, etc.)
DELETE /api/v1/knowledge/{id}                — delete item
POST   /api/v1/knowledge/bulk                — bulk approve/delete (for review workflow)
POST   /api/v1/knowledge/commit              — commit approved items to repo
```

**Implementation:**
- Import `internal/knowledge` store package directly
- Handlers are thin wrappers around store CRUD operations
- Bulk endpoint accepts array of `{id, action}` for review workflow
- Commit endpoint accepts `{repo_path}` and copies approved items

### 1G. YAML Validation API

**File:** `internal/server/handlers_create.go`

```
POST /api/v1/tasks/validate
```

Request: `{"yaml": "version: 1\n..."}`
Response: `{"valid": true}` or `{"valid": false, "errors": ["missing required field: title"]}`

Useful for the YAML editor to provide real-time validation feedback.

---

## Phase 2: Frontend — Task Creation

### 2A. Task Creation Wizard Page

**Route:** `/tasks/new`

Multi-step form with these panels:

1. **Mode & Template** — Choose transform/report, optionally start from template
2. **Repositories** — Add repo URLs with branch, name, setup commands. Group repos into named groups if desired
3. **Execution** — Toggle agentic vs deterministic
   - Agentic: prompt textarea (large, monospace), verifier list builder
   - Deterministic: image, args, env var list builder
4. **Settings** — timeout, require_approval toggle, max_parallel, failure thresholds, PR config (branch prefix, title, labels, reviewers)
5. **Review & Submit** — show generated YAML (editable), validate, submit

**Components:**
- `RepositoryInput.tsx` — URL + branch + name fields with add/remove
- `VerifierBuilder.tsx` — name + command array builder
- `GroupBuilder.tsx` — drag repos into named groups
- `TaskYamlEditor.tsx` — Monaco-style YAML editor with validation (can use CodeMirror 6 for lighter weight)
- `TaskWizard.tsx` — orchestrates the multi-step flow

**State:** Local React state (useReducer) for form data, convert to task JSON on submit.

### 2B. AI-Assisted Creation (Chat Mode)

**Route:** `/tasks/new?mode=ai` (or toggle within the wizard)

Chat-style interface:
- User types a natural language description
- Claude generates YAML, displayed in a side panel
- User can continue the conversation to refine
- "Use this" button copies YAML into the wizard for final review/edit
- Conversation persists via `conversation_id`

**Components:**
- `CreateChat.tsx` — message list + input, calls `/api/v1/tasks/generate`
- `YamlPreview.tsx` — syntax-highlighted YAML preview panel with copy/edit buttons

### 2C. Template Gallery

**Route:** `/templates`

Grid of template cards (name, description, category icon). Click to pre-fill the task wizard.

**Components:**
- `TemplateGallery.tsx` — grid layout with search/filter
- `TemplateCard.tsx` — individual template preview

---

## Phase 3: Frontend — Results & Reports

### 3A. Enhanced Task Detail — Result Tab

Add a "Result" tab to the existing TaskDetail page (visible when workflow is completed).

**Transform mode result view:**
- Summary card: status, duration, total files changed
- PR list: repo name, PR number as clickable link, branch name
- Per-repo expandable sections showing files modified

**Report mode result view:**
- Per-repository report cards
- Frontmatter rendered as key-value table
- Markdown body rendered with proper formatting (use react-markdown)
- For forEach mode: sub-tabs per target within each repo
- Export buttons: JSON download, CSV download (frontmatter fields)

**Components:**
- `ResultView.tsx` — dispatches to TransformResult or ReportResult based on mode
- `TransformResult.tsx` — PR link cards, file stats
- `ReportResult.tsx` — structured data + markdown rendering
- `ForEachResults.tsx` — tabbed target results

### 3B. Retry Failed Groups

On the task detail page for completed grouped workflows with failures:
- "Retry Failed Groups" button
- Confirmation dialog showing which groups failed
- Starts new workflow, navigates to its detail page

---

## Phase 4: Frontend — Knowledge Management

### 4A. Knowledge List Page

**Route:** `/knowledge`

Table/list view with:
- Columns: type (badge), summary, confidence, tags, source, status, created
- Filters: type dropdown, tag search, status (pending/approved), task ID
- Search bar across summary + details
- Click row to expand inline detail
- Actions: edit, delete, approve (inline buttons)
- "Add Knowledge" button → modal form

**Components:**
- `KnowledgePage.tsx` — page with filters + list
- `KnowledgeRow.tsx` — expandable row with inline actions
- `KnowledgeForm.tsx` — create/edit form (summary, type, details, tags)
- `KnowledgeFilters.tsx` — filter controls

### 4B. Knowledge Review Queue

**Route:** `/knowledge/review`

Queue-style interface for pending items:
- Show one item at a time (or scrollable list with action buttons)
- For each item: summary, details, origin (task ID, steering prompt)
- Action buttons: Approve, Edit & Approve, Delete, Skip
- Progress indicator: "3 of 12 reviewed"
- Bulk mode: checkbox select + bulk approve/delete
- Keyboard shortcuts: a=approve, d=delete, s=skip, e=edit

**Components:**
- `KnowledgeReview.tsx` — review queue page
- `ReviewCard.tsx` — single item with action buttons

### 4C. Knowledge Commit

Button on knowledge page: "Commit to Repo"
- Modal: enter repo path
- Shows count of approved items to commit
- Calls POST `/api/v1/knowledge/commit`
- Success/failure toast notification

---

## Phase 5: Frontend — Navigation & Dashboard

### 5A. Updated Navigation

Expand the header nav from current `[Inbox, All Tasks]` to:

```
[Dashboard, Tasks ▾, Knowledge, Templates]
         ├── Inbox
         ├── All Tasks
         └── New Task
```

Or simpler flat nav:
```
[Inbox, Tasks, New Task, Knowledge, Templates]
```

### 5B. Dashboard Page

**Route:** `/`  (replaces current Inbox as home)

- **Active workflows** — count by status (running, awaiting approval, paused)
- **Pending actions** — HITL items needing attention (link to inbox)
- **Recent completions** — last 5 completed with quick result summary
- **Knowledge pending review** — count with link to review queue
- **Quick actions** — "New Task" and "New Task (AI)" buttons

### 5C. Inbox Enhancement

**Route:** `/inbox`

Keep current inbox but enrich:
- Show more context per item (task title, repo count, how long waiting)
- Quick-action buttons inline (approve/reject without navigating to detail)
- Sort by urgency (longest waiting first)

---

## Phase 6: Temporal UI Embed

### 6A. Temporal Tab on Task Detail

Add a "Temporal" tab to the task detail page that embeds the Temporal UI for that specific workflow.

**Implementation:**
- iframe pointing to `{TEMPORAL_UI_URL}/namespaces/default/workflows/{workflow_id}`
- `TEMPORAL_UI_URL` configurable via env var (default: `http://localhost:8233`)
- Server exposes config endpoint: `GET /api/v1/config` returning `{"temporal_ui_url": "..."}`
- Fallback message if Temporal UI is not configured/reachable

**Component:**
- `TemporalEmbed.tsx` — iframe wrapper with loading state

### 6B. System Health Page (Optional)

**Route:** `/system`

- Worker status (connected workers, task queue depth) via Temporal API
- Recent workflow failure rate
- Links to full Temporal UI

---

## Phase 7: Enhanced Existing Components

### 7A. Diff Viewer Improvements

- Syntax highlighting by file extension (use Prism or highlight.js)
- Toggle side-by-side vs unified view
- File tree sidebar for navigating large diffs
- Search within diffs
- Collapse/expand all files

### 7B. Execution Timeline

Visual timeline on task detail showing phases:
```
[Provisioning] → [Cloning] → [Executing] → [Verifying] → [Awaiting Input] → [Creating PRs] → [Done]
     2s              5s          45s           12s            waiting...
```

Uses the SSE status events to track phase transitions with timestamps.

**Component:** `ExecutionTimeline.tsx`

### 7C. YAML Editor Component

Reusable CodeMirror 6 component with:
- YAML syntax highlighting
- Real-time validation via `/api/v1/tasks/validate`
- Auto-complete for known task schema fields
- Error gutters showing validation errors

Used in: task creation wizard, template editing, AI-assisted creation preview.

---

## Implementation Order

| Order | Phase | Effort | Value |
|-------|-------|--------|-------|
| 1 | 1A + 1B (Submit + Result APIs) | Medium | High — enables task creation |
| 2 | 2A (Task Wizard) | Large | High — primary missing feature |
| 3 | 1D + 2C (Templates API + Gallery) | Small | Medium — speeds up task creation |
| 4 | 3A (Result/Report views) | Medium | High — completes the output story |
| 5 | 1F + 4A (Knowledge API + List) | Medium | Medium — unique feature |
| 6 | 1E + 2B (AI Create API + Chat) | Medium | High — differentiator |
| 7 | 6A (Temporal embed) | Small | Medium — low effort high value |
| 8 | 5A + 5B (Nav + Dashboard) | Small | Medium — polish |
| 9 | 1C + 3B (Retry API + UI) | Small | Medium — completes grouped workflows |
| 10 | 4B + 4C (Knowledge Review + Commit) | Medium | Medium — completes knowledge |
| 11 | 7A-7C (Enhanced components) | Medium | Low — polish |
| 12 | 5C + 6B (Inbox enhance + System) | Small | Low — nice to have |

## New Dependencies

**Frontend:**
- `codemirror` + `@codemirror/lang-yaml` — YAML editor
- `react-markdown` + `remark-gfm` — report rendering
- `lucide-react` — already installed, use for icons

**Backend:**
- `github.com/anthropics/anthropic-sdk-go` — already used in CLI for create command
- No other new dependencies expected

## Files to Create

**Backend (Go):**
- `internal/server/handlers_submit.go` — task submission + retry
- `internal/server/handlers_create.go` — AI generation + YAML validation
- `internal/server/handlers_templates.go` — template CRUD
- `internal/server/handlers_knowledge.go` — knowledge CRUD + review + commit
- `internal/templates/templates.go` — shared template definitions (extracted from CLI)
- `internal/create/generator.go` — shared Claude YAML generation (extracted from CLI)

**Frontend (TypeScript/React):**
- `web/src/pages/TaskCreate.tsx` — wizard page
- `web/src/pages/TemplatGallery.tsx` — template browsing
- `web/src/pages/KnowledgePage.tsx` — knowledge list
- `web/src/pages/KnowledgeReview.tsx` — review queue
- `web/src/pages/Dashboard.tsx` — home dashboard
- `web/src/components/TaskWizard.tsx` — multi-step form orchestrator
- `web/src/components/RepositoryInput.tsx` — repo URL builder
- `web/src/components/VerifierBuilder.tsx` — verifier list builder
- `web/src/components/GroupBuilder.tsx` — group assignment
- `web/src/components/TaskYamlEditor.tsx` — YAML editor with validation
- `web/src/components/CreateChat.tsx` — AI-assisted chat interface
- `web/src/components/YamlPreview.tsx` — YAML preview panel
- `web/src/components/ResultView.tsx` — final result dispatcher
- `web/src/components/TransformResult.tsx` — PR links + stats
- `web/src/components/ReportResult.tsx` — report data + markdown
- `web/src/components/ForEachResults.tsx` — per-target tabs
- `web/src/components/KnowledgeRow.tsx` — expandable knowledge item
- `web/src/components/KnowledgeForm.tsx` — create/edit knowledge
- `web/src/components/KnowledgeFilters.tsx` — filter controls
- `web/src/components/ReviewCard.tsx` — knowledge review card
- `web/src/components/ExecutionTimeline.tsx` — phase timeline
- `web/src/components/TemporalEmbed.tsx` — iframe wrapper

**Updated existing files:**
- `internal/server/server.go` — register new routes
- `internal/server/client_iface.go` — extend TemporalClient interface
- `web/src/App.tsx` — add new routes
- `web/src/components/Layout.tsx` — expanded navigation
- `web/src/pages/TaskDetail.tsx` — add Result tab + Temporal tab
- `web/src/api/client.ts` — new API methods
- `web/src/api/types.ts` — new TypeScript types
