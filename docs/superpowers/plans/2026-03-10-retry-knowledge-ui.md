# Retry UI + Knowledge Management UI Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the existing retry backend + build the knowledge list and review queue pages in the web UI.

**Architecture:** Pure frontend work except for one small backend addition (store task YAML on submit so retry can retrieve it). Knowledge pages follow the TemplatesPage two-panel pattern; retry is a button+dialog added to TaskDetail. All data fetching uses React Query; mutations use `useMutation`.

**Tech Stack:** React, TypeScript, React Query, react-router-dom, Tailwind CSS, shadcn/ui (Button, Badge, Tabs), lucide-react

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `internal/server/server.go` | Modify | Add `taskYAMLs map[string]string` to Server struct; register new `GET /api/v1/tasks/{id}/yaml` route |
| `internal/server/handlers_create.go` | Modify | Store YAML on submit; add `handleGetTaskYAML` handler |
| `internal/server/server_test.go` | Modify | Add test for YAML storage and retrieval |
| `web/src/api/types.ts` | Modify | Add `KnowledgeItem`, `KnowledgeListResponse` types |
| `web/src/api/client.ts` | Modify | Add `getTaskYAML`, `retryTask`, knowledge CRUD methods |
| `web/src/pages/TaskDetail.tsx` | Modify | Add `RetryButton` inline; fetch progress to detect failed groups |
| `web/src/pages/KnowledgePage.tsx` | Create | Knowledge list with filters, inline expand, CRUD actions, "Add" modal |
| `web/src/pages/KnowledgeReview.tsx` | Create | Review queue — one item at a time, keyboard shortcuts, bulk mode, commit modal |
| `web/src/App.tsx` | Modify | Add `/knowledge` and `/knowledge/review` routes |
| `web/src/components/Layout.tsx` | Modify | Add Knowledge nav item |

---

## Chunk 1: Backend — Task YAML Storage

### Task 1: Store YAML on submit + expose retrieval endpoint

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/handlers_create.go`
- Modify: `internal/server/server_test.go`

- [ ] **Step 1: Write failing test for YAML retrieval**

In `internal/server/server_test.go`, add after existing submit tests:

```go
func TestGetTaskYAML(t *testing.T) {
    s := newTestServer(t)
    // Submit a task first
    yamlBody := `{"yaml": "version: 1\nid: test\ntitle: Test\nmode: transform\nrepositories:\n  - url: https://github.com/org/repo\nexecution:\n  agentic:\n    prompt: do something\n"}`
    req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(yamlBody))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    s.router.ServeHTTP(w, req)
    require.Equal(t, http.StatusOK, w.Code)

    var submitResp map[string]string
    require.NoError(t, json.Unmarshal(w.Body.Bytes(), &submitResp))
    wfID := submitResp["workflow_id"]

    // Now retrieve the YAML
    req2 := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+wfID+"/yaml", nil)
    w2 := httptest.NewRecorder()
    s.router.ServeHTTP(w2, req2)
    require.Equal(t, http.StatusOK, w2.Code)

    var yamlResp map[string]string
    require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &yamlResp))
    assert.Contains(t, yamlResp["yaml"], "title: Test")
}

func TestGetTaskYAMLNotFound(t *testing.T) {
    s := newTestServer(t)
    req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/nonexistent/yaml", nil)
    w := httptest.NewRecorder()
    s.router.ServeHTTP(w, req)
    assert.Equal(t, http.StatusNotFound, w.Code)
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/server/... -run TestGetTaskYAML -v
```
Expected: FAIL — `handleGetTaskYAML` not defined

- [ ] **Step 3: Add `taskYAMLs` map to Server struct**

In `internal/server/server.go`, add field to `Server` struct:
```go
taskYAMLs map[string]string // workflow_id → original task YAML
```

And initialise in `NewServer` (or wherever the struct is created):
```go
taskYAMLs: make(map[string]string),
```

Add route in the router block alongside the existing `/tasks/{id}/...` routes:
```go
r.Get("/yaml", s.handleGetTaskYAML)
```

- [ ] **Step 4: Store YAML on submit + add retrieval handler**

In `internal/server/handlers_create.go`, in `handleSubmitTask` after `StartTransform` succeeds, add:
```go
s.taskYAMLs[workflowID] = req.YAML
```

Add new handler:
```go
// handleGetTaskYAML handles GET /api/v1/tasks/{id}/yaml.
func (s *Server) handleGetTaskYAML(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")
    yaml, ok := s.taskYAMLs[id]
    if !ok {
        writeError(w, http.StatusNotFound, "no YAML found for task "+id)
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"yaml": yaml})
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/server/... -run TestGetTaskYAML -v
go test ./internal/server/... -v
```
Expected: all PASS

- [ ] **Step 6: Verify build**

```bash
go build ./...
```

- [ ] **Step 7: Lint**

```bash
make lint
```

- [ ] **Step 8: Commit**

```bash
git add internal/server/server.go internal/server/handlers_create.go internal/server/server_test.go
git commit -m "feat(server): store task YAML on submit; expose GET /tasks/{id}/yaml"
```

---

## Chunk 2: Frontend — API Client + Types

### Task 2: Add knowledge types and API methods

**Files:**
- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: Add KnowledgeItem types to `web/src/api/types.ts`**

Append to the file:
```typescript
// Knowledge types

export type KnowledgeType = 'pattern' | 'correction' | 'gotcha' | 'context'
export type KnowledgeSource = 'auto_captured' | 'steering_extracted' | 'manual'
export type KnowledgeStatus = 'pending' | 'approved'

export interface KnowledgeOrigin {
  task_id: string
  repository?: string
  steering_prompt?: string
  iteration?: number
}

export interface KnowledgeItem {
  id: string
  type: KnowledgeType
  summary: string
  details: string
  source: KnowledgeSource
  tags?: string[]
  confidence: number
  created_from?: KnowledgeOrigin
  created_at: string
  status: KnowledgeStatus
}

export interface KnowledgeFilters {
  task_id?: string
  type?: KnowledgeType
  tag?: string
  status?: KnowledgeStatus
}

export interface CreateKnowledgeRequest {
  type: KnowledgeType
  summary: string
  details?: string
  tags?: string[]
  confidence?: number
  task_id?: string
}

export interface UpdateKnowledgeRequest {
  summary?: string
  details?: string
  tags?: string[]
  status?: KnowledgeStatus
  confidence?: number
}

export interface BulkAction {
  id: string
  action: 'approve' | 'delete'
}
```

- [ ] **Step 2: Add API methods to `web/src/api/client.ts`**

Add `put` helper after the existing `post` function:
```typescript
async function put<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error ?? res.statusText)
  }
  return res.json()
}

async function del<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`, { method: 'DELETE' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error ?? res.statusText)
  }
  return res.json()
}
```

Add to the `api` object:
```typescript
  // Retry
  getTaskYAML: (id: string) =>
    get<{ yaml: string }>(`/tasks/${id}/yaml`),
  retryTask: (id: string, yaml: string, failedOnly: boolean) =>
    post<{ workflow_id: string }>(`/tasks/${id}/retry`, { yaml, failed_only: failedOnly }),

  // Knowledge
  listKnowledge: (filters?: KnowledgeFilters) => {
    const params = new URLSearchParams()
    if (filters?.task_id) params.set('task_id', filters.task_id)
    if (filters?.type) params.set('type', filters.type)
    if (filters?.tag) params.set('tag', filters.tag)
    if (filters?.status) params.set('status', filters.status)
    const qs = params.toString()
    return get<{ items: KnowledgeItem[] }>(`/knowledge${qs ? `?${qs}` : ''}`)
  },
  getKnowledge: (id: string) => get<KnowledgeItem>(`/knowledge/${id}`),
  createKnowledge: (req: CreateKnowledgeRequest) =>
    post<KnowledgeItem>('/knowledge', req),
  updateKnowledge: (id: string, req: UpdateKnowledgeRequest) =>
    put<KnowledgeItem>(`/knowledge/${id}`, req),
  deleteKnowledge: (id: string) =>
    del<{ status: string }>(`/knowledge/${id}`),
  bulkKnowledge: (actions: BulkAction[]) =>
    post<{ status: string }>('/knowledge/bulk', { actions }),
  commitKnowledge: (repoPath: string) =>
    post<{ committed: number; repo_path: string }>('/knowledge/commit', { repo_path: repoPath }),
```

Also add the new types to the import at the top of `client.ts`:
```typescript
import type {
  TaskSummary, DiffOutput, VerifierOutput,
  SteeringState, ExecutionProgress, TaskResult, AppConfig,
  Template, KnowledgeItem, KnowledgeFilters, CreateKnowledgeRequest,
  UpdateKnowledgeRequest, BulkAction,
} from './types'
```

- [ ] **Step 3: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit
```
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add web/src/api/types.ts web/src/api/client.ts
git commit -m "feat(web): add knowledge types and API client methods; add retry API"
```

---

## Chunk 3: Retry UI

### Task 3: Add Retry Failed Groups button to TaskDetail

**Files:**
- Modify: `web/src/pages/TaskDetail.tsx`

The retry flow:
1. Fetch `getProgress` to detect `failed_group_names`
2. Show "Retry Failed Groups" button when `status` is `completed` or `failed` and there are failed groups
3. Click → confirmation dialog listing failed groups
4. Confirm → fetch YAML → call `retryTask(id, yaml, true)` → navigate to new workflow

- [ ] **Step 1: Update TaskDetail to show Retry button**

Replace `web/src/pages/TaskDetail.tsx` with the updated version:

```typescript
import { useEffect, useState } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { useQuery, useMutation } from '@tanstack/react-query'
import { api, subscribeToTask } from '@/api/client'
import type { TaskStatus } from '@/api/types'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Button } from '@/components/ui/button'
import { StatusBadge } from '@/components/StatusIcon'
import { ExecutionTimeline } from '@/components/ExecutionTimeline'
import { DiffViewer } from '@/components/DiffViewer'
import { VerifierLogs } from '@/components/VerifierLogs'
import { SteeringPanel } from '@/components/SteeringPanel'
import { GroupProgress } from '@/components/GroupProgress'
import { ResultView } from '@/components/ResultView'
import { TemporalEmbed } from '@/components/TemporalEmbed'
import { ArrowLeft, XCircle, RefreshCw } from 'lucide-react'

const TERMINAL_STATUSES: TaskStatus[] = ['completed', 'failed', 'cancelled']

export function TaskDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [liveStatus, setLiveStatus] = useState<TaskStatus | null>(null)
  const [showRetryDialog, setShowRetryDialog] = useState(false)
  const [retryError, setRetryError] = useState<string | null>(null)

  const { data: task } = useQuery({
    queryKey: ['task', id],
    queryFn: () => api.getTask(id!),
    enabled: !!id,
  })

  const { data: progress } = useQuery({
    queryKey: ['progress', id],
    queryFn: () => api.getProgress(id!),
    enabled: !!id,
  })

  const cancelMutation = useMutation({
    mutationFn: () => api.cancel(id!),
  })

  const retryMutation = useMutation({
    mutationFn: async () => {
      const { yaml } = await api.getTaskYAML(id!)
      return api.retryTask(id!, yaml, true)
    },
    onSuccess: (data) => {
      setShowRetryDialog(false)
      navigate(`/tasks/${data.workflow_id}`)
    },
    onError: (err: Error) => {
      setRetryError(err.message)
    },
  })

  useEffect(() => {
    if (!id) return
    return subscribeToTask(id, (s) => setLiveStatus(s as TaskStatus))
  }, [id])

  const status = liveStatus ?? task?.status
  const isAwaitingApproval = status === 'awaiting_approval'
  const isTerminal = status ? TERMINAL_STATUSES.includes(status) : false
  const isRunning = status && !isTerminal
  const showResult = status === 'completed' || status === 'failed'
  const failedGroups = progress?.failed_group_names ?? []
  const canRetry = isTerminal && failedGroups.length > 0

  return (
    <div className="max-w-6xl">
      {/* Back nav + header */}
      <div className="mb-6">
        <Link
          to="/tasks"
          className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground mb-3 transition-colors"
        >
          <ArrowLeft className="h-3 w-3" />
          Back to tasks
        </Link>

        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <div className="flex items-center gap-3">
              <h1 className="text-lg font-semibold font-mono truncate">{id}</h1>
              {status && <StatusBadge status={status} />}
            </div>
            {task?.start_time && (
              <p className="text-xs text-muted-foreground mt-1">{task.start_time}</p>
            )}
          </div>

          {/* Actions */}
          <div className="flex items-center gap-2 shrink-0">
            {canRetry && (
              <Button
                variant="outline"
                size="sm"
                onClick={() => { setRetryError(null); setShowRetryDialog(true) }}
              >
                <RefreshCw className="h-3.5 w-3.5 mr-1" />
                Retry Failed Groups
              </Button>
            )}
            {isRunning && !isAwaitingApproval && (
              <Button
                variant="outline"
                size="sm"
                className="text-red-600 hover:text-red-700 hover:bg-red-50"
                onClick={() => cancelMutation.mutate()}
                disabled={cancelMutation.isPending}
              >
                <XCircle className="h-3.5 w-3.5 mr-1" />
                Cancel
              </Button>
            )}
          </div>
        </div>
      </div>

      {/* Retry confirmation dialog */}
      {showRetryDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="bg-background rounded-lg border shadow-lg p-6 w-full max-w-md mx-4">
            <h2 className="text-sm font-semibold mb-1">Retry Failed Groups</h2>
            <p className="text-xs text-muted-foreground mb-4">
              A new workflow will be started for the following failed groups:
            </p>
            <ul className="mb-4 space-y-1">
              {failedGroups.map((g) => (
                <li key={g} className="text-xs font-mono bg-muted rounded px-2 py-1">{g}</li>
              ))}
            </ul>
            {retryError && (
              <p className="text-xs text-destructive mb-3">{retryError}</p>
            )}
            <div className="flex gap-2 justify-end">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowRetryDialog(false)}
                disabled={retryMutation.isPending}
              >
                Cancel
              </Button>
              <Button
                size="sm"
                onClick={() => retryMutation.mutate()}
                disabled={retryMutation.isPending}
              >
                {retryMutation.isPending ? 'Starting...' : 'Retry'}
              </Button>
            </div>
          </div>
        </div>
      )}

      {/* Execution timeline */}
      {status && (
        <div className="mb-6 rounded-lg border bg-card px-4 py-3 overflow-x-auto">
          <ExecutionTimeline status={status} />
        </div>
      )}

      {/* Tabs */}
      <Tabs defaultValue={showResult ? 'result' : 'diff'}>
        <TabsList className="flex-wrap">
          {showResult && <TabsTrigger value="result">Result</TabsTrigger>}
          <TabsTrigger value="diff">Diff</TabsTrigger>
          <TabsTrigger value="logs">Verifier Logs</TabsTrigger>
          <TabsTrigger value="progress">Groups</TabsTrigger>
          {isAwaitingApproval && (
            <TabsTrigger value="steer">Approve / Steer</TabsTrigger>
          )}
          <TabsTrigger value="temporal">Temporal</TabsTrigger>
        </TabsList>

        {showResult && (
          <TabsContent value="result" className="mt-4">
            <ResultView workflowId={id!} />
          </TabsContent>
        )}
        <TabsContent value="diff" className="mt-4">
          <DiffViewer workflowId={id!} />
        </TabsContent>
        <TabsContent value="logs" className="mt-4">
          <VerifierLogs workflowId={id!} />
        </TabsContent>
        <TabsContent value="progress" className="mt-4">
          <GroupProgress workflowId={id!} />
        </TabsContent>
        {isAwaitingApproval && (
          <TabsContent value="steer" className="mt-4">
            <SteeringPanel workflowId={id!} />
          </TabsContent>
        )}
        <TabsContent value="temporal" className="mt-4">
          <TemporalEmbed workflowId={id!} />
        </TabsContent>
      </Tabs>
    </div>
  )
}
```

- [ ] **Step 2: TypeScript check**

```bash
cd web && npx tsc --noEmit
```
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/TaskDetail.tsx
git commit -m "feat(web): add Retry Failed Groups button to task detail"
```

---

## Chunk 4: Knowledge List Page

### Task 4: Knowledge list page with CRUD

**Files:**
- Create: `web/src/pages/KnowledgePage.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/Layout.tsx`

The page has:
- Filters bar: type dropdown, status toggle (all/pending/approved), tag search input
- Table with columns: type badge, summary, confidence bar, source, status badge, created date
- Click row to expand inline with full details + action buttons (approve, edit, delete)
- "Add Knowledge" button → inline modal form
- Edit inline (summary, details, tags, confidence)

- [ ] **Step 1: Create `web/src/pages/KnowledgePage.tsx`**

```typescript
import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import type {
  KnowledgeItem, KnowledgeType, KnowledgeStatus,
  CreateKnowledgeRequest, UpdateKnowledgeRequest,
} from '@/api/types'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Plus, ChevronDown, ChevronRight, Check, Trash2, Pencil, X, BookOpen,
} from 'lucide-react'
import { cn } from '@/lib/utils'

const TYPE_COLORS: Record<KnowledgeType, string> = {
  pattern:    'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400',
  correction: 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400',
  gotcha:     'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400',
  context:    'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400',
}

function TypeBadge({ type }: { type: KnowledgeType }) {
  return (
    <span className={cn('rounded px-1.5 py-0.5 text-[10px] font-medium capitalize', TYPE_COLORS[type])}>
      {type}
    </span>
  )
}

function StatusBadge({ status }: { status: KnowledgeStatus }) {
  return (
    <span className={cn(
      'rounded px-1.5 py-0.5 text-[10px] font-medium',
      status === 'approved'
        ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400'
        : 'bg-muted text-muted-foreground',
    )}>
      {status}
    </span>
  )
}

function ConfidenceBar({ value }: { value: number }) {
  return (
    <div className="flex items-center gap-1.5">
      <div className="h-1.5 w-16 rounded-full bg-muted overflow-hidden">
        <div
          className={cn('h-full rounded-full', value >= 0.7 ? 'bg-green-500' : value >= 0.4 ? 'bg-amber-500' : 'bg-red-400')}
          style={{ width: `${Math.round(value * 100)}%` }}
        />
      </div>
      <span className="text-[10px] text-muted-foreground">{Math.round(value * 100)}%</span>
    </div>
  )
}

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

// ---- Add/Edit form ----

interface KnowledgeFormProps {
  initial?: Partial<KnowledgeItem>
  onSave: (data: CreateKnowledgeRequest | UpdateKnowledgeRequest) => void
  onCancel: () => void
  isPending: boolean
}

function KnowledgeForm({ initial, onSave, onCancel, isPending }: KnowledgeFormProps) {
  const [type, setType] = useState<KnowledgeType>(initial?.type ?? 'pattern')
  const [summary, setSummary] = useState(initial?.summary ?? '')
  const [details, setDetails] = useState(initial?.details ?? '')
  const [tags, setTags] = useState((initial?.tags ?? []).join(', '))
  const [confidence, setConfidence] = useState(String(Math.round((initial?.confidence ?? 0.8) * 100)))

  const handleSubmit = () => {
    const parsedTags = tags.split(',').map(t => t.trim()).filter(Boolean)
    const parsedConf = Math.min(1, Math.max(0, Number(confidence) / 100))
    onSave({ type, summary, details, tags: parsedTags, confidence: parsedConf })
  }

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="text-xs font-medium text-muted-foreground">Type</label>
          <select
            value={type}
            onChange={e => setType(e.target.value as KnowledgeType)}
            className="mt-1 w-full rounded-md border bg-background px-2 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
          >
            {(['pattern', 'correction', 'gotcha', 'context'] as KnowledgeType[]).map(t => (
              <option key={t} value={t} className="capitalize">{t}</option>
            ))}
          </select>
        </div>
        <div>
          <label className="text-xs font-medium text-muted-foreground">Confidence (%)</label>
          <input
            type="number"
            min={0}
            max={100}
            value={confidence}
            onChange={e => setConfidence(e.target.value)}
            className="mt-1 w-full rounded-md border bg-background px-2 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
          />
        </div>
      </div>
      <div>
        <label className="text-xs font-medium text-muted-foreground">Summary <span className="text-destructive">*</span></label>
        <input
          type="text"
          value={summary}
          onChange={e => setSummary(e.target.value)}
          placeholder="One-line summary"
          className="mt-1 w-full rounded-md border bg-background px-2 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
        />
      </div>
      <div>
        <label className="text-xs font-medium text-muted-foreground">Details</label>
        <textarea
          value={details}
          onChange={e => setDetails(e.target.value)}
          rows={3}
          placeholder="Extended notes, examples, context..."
          className="mt-1 w-full rounded-md border bg-background px-2 py-1.5 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring"
        />
      </div>
      <div>
        <label className="text-xs font-medium text-muted-foreground">Tags (comma-separated)</label>
        <input
          type="text"
          value={tags}
          onChange={e => setTags(e.target.value)}
          placeholder="go, slog, migration"
          className="mt-1 w-full rounded-md border bg-background px-2 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
        />
      </div>
      <div className="flex gap-2 justify-end pt-1">
        <Button variant="outline" size="sm" onClick={onCancel} disabled={isPending}>Cancel</Button>
        <Button size="sm" onClick={handleSubmit} disabled={isPending || !summary.trim()}>
          {isPending ? 'Saving...' : 'Save'}
        </Button>
      </div>
    </div>
  )
}

// ---- Row ----

interface RowProps {
  item: KnowledgeItem
  onApprove: () => void
  onDelete: () => void
  onUpdate: (req: UpdateKnowledgeRequest) => void
  isPending: boolean
}

function KnowledgeRow({ item, onApprove, onDelete, onUpdate, isPending }: RowProps) {
  const [expanded, setExpanded] = useState(false)
  const [editing, setEditing] = useState(false)

  return (
    <>
      <tr
        className="border-b hover:bg-muted/30 cursor-pointer"
        onClick={() => { if (!editing) setExpanded(e => !e) }}
      >
        <td className="py-2.5 pl-4 pr-2">
          {expanded
            ? <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
            : <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />}
        </td>
        <td className="py-2.5 pr-3"><TypeBadge type={item.type} /></td>
        <td className="py-2.5 pr-4 text-sm max-w-xs truncate">{item.summary}</td>
        <td className="py-2.5 pr-3"><ConfidenceBar value={item.confidence} /></td>
        <td className="py-2.5 pr-3 text-xs text-muted-foreground capitalize">{item.source.replace('_', ' ')}</td>
        <td className="py-2.5 pr-3"><StatusBadge status={item.status} /></td>
        <td className="py-2.5 pr-4 text-xs text-muted-foreground">{formatDate(item.created_at)}</td>
      </tr>
      {expanded && (
        <tr className="border-b bg-muted/20">
          <td colSpan={7} className="px-4 py-3">
            {editing ? (
              <KnowledgeForm
                initial={item}
                onSave={(req) => { onUpdate(req as UpdateKnowledgeRequest); setEditing(false) }}
                onCancel={() => setEditing(false)}
                isPending={isPending}
              />
            ) : (
              <div className="space-y-2">
                {item.details && (
                  <p className="text-xs text-muted-foreground whitespace-pre-wrap font-mono bg-muted rounded px-3 py-2">
                    {item.details}
                  </p>
                )}
                {item.tags && item.tags.length > 0 && (
                  <div className="flex gap-1 flex-wrap">
                    {item.tags.map(t => (
                      <span key={t} className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">{t}</span>
                    ))}
                  </div>
                )}
                {item.created_from && (
                  <p className="text-[10px] text-muted-foreground">
                    Source: task {item.created_from.task_id}
                    {item.created_from.repository && ` / ${item.created_from.repository}`}
                  </p>
                )}
                <div className="flex gap-2 pt-1">
                  {item.status === 'pending' && (
                    <Button variant="outline" size="sm" className="h-7 text-xs" onClick={onApprove} disabled={isPending}>
                      <Check className="h-3 w-3 mr-1" /> Approve
                    </Button>
                  )}
                  <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => setEditing(true)}>
                    <Pencil className="h-3 w-3 mr-1" /> Edit
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 text-xs text-destructive hover:bg-destructive/10"
                    onClick={onDelete}
                    disabled={isPending}
                  >
                    <Trash2 className="h-3 w-3 mr-1" /> Delete
                  </Button>
                </div>
              </div>
            )}
          </td>
        </tr>
      )}
    </>
  )
}

// ---- Page ----

export function KnowledgePage() {
  const qc = useQueryClient()
  const [typeFilter, setTypeFilter] = useState<KnowledgeType | ''>('')
  const [statusFilter, setStatusFilter] = useState<KnowledgeStatus | ''>('')
  const [tagSearch, setTagSearch] = useState('')
  const [showAddModal, setShowAddModal] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: ['knowledge', typeFilter, statusFilter, tagSearch],
    queryFn: () => api.listKnowledge({
      type: typeFilter || undefined,
      status: statusFilter || undefined,
      tag: tagSearch || undefined,
    }),
  })

  const items = data?.items ?? []

  const invalidate = () => qc.invalidateQueries({ queryKey: ['knowledge'] })

  const approveMutation = useMutation({
    mutationFn: (id: string) => api.updateKnowledge(id, { status: 'approved' }),
    onSuccess: invalidate,
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deleteKnowledge(id),
    onSuccess: invalidate,
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, req }: { id: string; req: UpdateKnowledgeRequest }) =>
      api.updateKnowledge(id, req),
    onSuccess: invalidate,
  })

  const createMutation = useMutation({
    mutationFn: (req: CreateKnowledgeRequest) => api.createKnowledge(req),
    onSuccess: () => { invalidate(); setShowAddModal(false) },
  })

  return (
    <div className="max-w-5xl">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-lg font-semibold">Knowledge</h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            Reusable insights extracted from task executions
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" asChild>
            <a href="/knowledge/review">Review Queue</a>
          </Button>
          <Button size="sm" onClick={() => setShowAddModal(true)}>
            <Plus className="h-3.5 w-3.5 mr-1" /> Add Knowledge
          </Button>
        </div>
      </div>

      {/* Filters */}
      <div className="flex gap-2 mb-4 flex-wrap">
        <select
          value={typeFilter}
          onChange={e => setTypeFilter(e.target.value as KnowledgeType | '')}
          className="rounded-md border bg-background px-2.5 py-1.5 text-xs focus:outline-none focus:ring-2 focus:ring-ring"
        >
          <option value="">All types</option>
          {(['pattern', 'correction', 'gotcha', 'context'] as KnowledgeType[]).map(t => (
            <option key={t} value={t} className="capitalize">{t}</option>
          ))}
        </select>
        <select
          value={statusFilter}
          onChange={e => setStatusFilter(e.target.value as KnowledgeStatus | '')}
          className="rounded-md border bg-background px-2.5 py-1.5 text-xs focus:outline-none focus:ring-2 focus:ring-ring"
        >
          <option value="">All statuses</option>
          <option value="pending">Pending</option>
          <option value="approved">Approved</option>
        </select>
        <input
          type="text"
          placeholder="Filter by tag..."
          value={tagSearch}
          onChange={e => setTagSearch(e.target.value)}
          className="rounded-md border bg-background px-2.5 py-1.5 text-xs focus:outline-none focus:ring-2 focus:ring-ring"
        />
      </div>

      {/* Table */}
      <div className="rounded-lg border overflow-hidden">
        <table className="w-full text-sm">
          <thead className="border-b bg-muted/50">
            <tr>
              <th className="py-2.5 pl-4 pr-2 w-6" />
              <th className="py-2.5 pr-3 text-left text-xs font-medium text-muted-foreground">Type</th>
              <th className="py-2.5 pr-4 text-left text-xs font-medium text-muted-foreground">Summary</th>
              <th className="py-2.5 pr-3 text-left text-xs font-medium text-muted-foreground">Confidence</th>
              <th className="py-2.5 pr-3 text-left text-xs font-medium text-muted-foreground">Source</th>
              <th className="py-2.5 pr-3 text-left text-xs font-medium text-muted-foreground">Status</th>
              <th className="py-2.5 pr-4 text-left text-xs font-medium text-muted-foreground">Created</th>
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              <tr><td colSpan={7} className="py-12 text-center text-sm text-muted-foreground">Loading...</td></tr>
            )}
            {!isLoading && items.length === 0 && (
              <tr>
                <td colSpan={7} className="py-16 text-center">
                  <BookOpen className="h-8 w-8 mx-auto mb-2 text-muted-foreground opacity-40" />
                  <p className="text-sm text-muted-foreground">No knowledge items found</p>
                </td>
              </tr>
            )}
            {items.map(item => (
              <KnowledgeRow
                key={item.id}
                item={item}
                onApprove={() => approveMutation.mutate(item.id)}
                onDelete={() => deleteMutation.mutate(item.id)}
                onUpdate={(req) => updateMutation.mutate({ id: item.id, req })}
                isPending={approveMutation.isPending || deleteMutation.isPending || updateMutation.isPending}
              />
            ))}
          </tbody>
        </table>
      </div>

      {/* Add modal */}
      {showAddModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="bg-background rounded-lg border shadow-lg p-6 w-full max-w-lg mx-4">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-sm font-semibold">Add Knowledge</h2>
              <button onClick={() => setShowAddModal(false)} className="text-muted-foreground hover:text-foreground">
                <X className="h-4 w-4" />
              </button>
            </div>
            <KnowledgeForm
              onSave={(req) => createMutation.mutate(req as CreateKnowledgeRequest)}
              onCancel={() => setShowAddModal(false)}
              isPending={createMutation.isPending}
            />
          </div>
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Add route + nav item**

In `web/src/App.tsx`, add import and route:
```typescript
import { KnowledgePage } from './pages/KnowledgePage'
// inside Routes:
<Route path="/knowledge" element={<KnowledgePage />} />
```

In `web/src/components/Layout.tsx`, add to imports:
```typescript
import { LayoutDashboard, Inbox, List, ExternalLink, Plus, LayoutTemplate, BookOpen } from 'lucide-react'
```

Add to `NAV_ITEMS`:
```typescript
{ href: '/knowledge', label: 'Knowledge', icon: BookOpen },
```

- [ ] **Step 3: TypeScript check**

```bash
cd web && npx tsc --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add web/src/pages/KnowledgePage.tsx web/src/App.tsx web/src/components/Layout.tsx
git commit -m "feat(web): add Knowledge list page with CRUD and filters"
```

---

## Chunk 5: Knowledge Review Queue + Commit

### Task 5: Knowledge review queue page

**Files:**
- Create: `web/src/pages/KnowledgeReview.tsx`
- Modify: `web/src/App.tsx`

Review queue shows pending items one at a time (or in a scrollable list). Keyboard shortcuts: `a`=approve, `d`=delete, `s`=skip. Progress indicator. Bulk checkbox mode. "Commit to Repo" modal at the top.

- [ ] **Step 1: Create `web/src/pages/KnowledgeReview.tsx`**

```typescript
import { useCallback, useEffect, useRef, useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/api/client'
import type { KnowledgeItem, UpdateKnowledgeRequest } from '@/api/types'
import { Button } from '@/components/ui/button'
import { ArrowLeft, Check, Trash2, SkipForward, GitBranch, X } from 'lucide-react'
import { cn } from '@/lib/utils'

const TYPE_COLORS: Record<string, string> = {
  pattern:    'bg-blue-100 text-blue-700',
  correction: 'bg-amber-100 text-amber-700',
  gotcha:     'bg-red-100 text-red-700',
  context:    'bg-purple-100 text-purple-700',
}

// ---- Commit modal ----

function CommitModal({ onClose }: { onClose: () => void }) {
  const [repoPath, setRepoPath] = useState('')
  const [result, setResult] = useState<{ committed: number } | null>(null)
  const [error, setError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: () => api.commitKnowledge(repoPath),
    onSuccess: (data) => setResult(data),
    onError: (e: Error) => setError(e.message),
  })

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-background rounded-lg border shadow-lg p-6 w-full max-w-md mx-4">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-sm font-semibold">Commit Knowledge to Repo</h2>
          <button onClick={onClose} className="text-muted-foreground hover:text-foreground">
            <X className="h-4 w-4" />
          </button>
        </div>
        {result ? (
          <div className="text-center py-4">
            <Check className="h-8 w-8 text-green-500 mx-auto mb-2" />
            <p className="text-sm font-medium">Committed {result.committed} item{result.committed !== 1 ? 's' : ''}</p>
            <p className="text-xs text-muted-foreground mt-1">Written to {repoPath}/.fleetlift/knowledge/items/</p>
            <Button className="mt-4" size="sm" onClick={onClose}>Done</Button>
          </div>
        ) : (
          <>
            <p className="text-xs text-muted-foreground mb-4">
              Copies all approved knowledge items to the target repository's <code>.fleetlift/knowledge/items/</code> directory.
            </p>
            <label className="text-xs font-medium text-muted-foreground">Repository path</label>
            <input
              type="text"
              value={repoPath}
              onChange={e => setRepoPath(e.target.value)}
              placeholder="/home/user/projects/my-service"
              className="mt-1 w-full rounded-md border bg-background px-2.5 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            />
            {error && <p className="text-xs text-destructive mt-2">{error}</p>}
            <div className="flex gap-2 justify-end mt-4">
              <Button variant="outline" size="sm" onClick={onClose} disabled={mutation.isPending}>Cancel</Button>
              <Button
                size="sm"
                onClick={() => mutation.mutate()}
                disabled={mutation.isPending || !repoPath.trim()}
              >
                {mutation.isPending ? 'Committing...' : 'Commit'}
              </Button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

// ---- Review card ----

interface ReviewCardProps {
  item: KnowledgeItem
  index: number
  total: number
  onApprove: () => void
  onDelete: () => void
  onSkip: () => void
  isPending: boolean
}

function ReviewCard({ item, index, total, onApprove, onDelete, onSkip, isPending }: ReviewCardProps) {
  return (
    <div className="rounded-lg border bg-card p-5">
      <div className="flex items-start justify-between gap-4 mb-3">
        <div className="flex items-center gap-2 flex-wrap">
          <span className={cn('rounded px-1.5 py-0.5 text-[10px] font-medium capitalize', TYPE_COLORS[item.type] ?? 'bg-muted text-muted-foreground')}>
            {item.type}
          </span>
          <span className="text-[10px] text-muted-foreground">{index + 1} of {total}</span>
        </div>
        <span className="text-[10px] text-muted-foreground shrink-0">
          {new Date(item.created_at).toLocaleDateString()}
        </span>
      </div>

      <p className="text-sm font-medium mb-2">{item.summary}</p>

      {item.details && (
        <pre className="text-xs text-muted-foreground whitespace-pre-wrap font-mono bg-muted rounded px-3 py-2 mb-3">
          {item.details}
        </pre>
      )}

      {item.tags && item.tags.length > 0 && (
        <div className="flex gap-1 flex-wrap mb-3">
          {item.tags.map(t => (
            <span key={t} className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">{t}</span>
          ))}
        </div>
      )}

      {item.created_from && (
        <p className="text-[10px] text-muted-foreground mb-4">
          From task <span className="font-mono">{item.created_from.task_id}</span>
          {item.created_from.repository && ` › ${item.created_from.repository}`}
        </p>
      )}

      <div className="flex gap-2">
        <Button
          size="sm"
          className="bg-green-600 hover:bg-green-700 text-white"
          onClick={onApprove}
          disabled={isPending}
          title="Approve (a)"
        >
          <Check className="h-3.5 w-3.5 mr-1" /> Approve
        </Button>
        <Button
          variant="outline"
          size="sm"
          className="text-destructive hover:bg-destructive/10"
          onClick={onDelete}
          disabled={isPending}
          title="Delete (d)"
        >
          <Trash2 className="h-3.5 w-3.5 mr-1" /> Delete
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={onSkip}
          disabled={isPending}
          title="Skip (s)"
        >
          <SkipForward className="h-3.5 w-3.5 mr-1" /> Skip
        </Button>
      </div>
    </div>
  )
}

// ---- Page ----

export function KnowledgeReviewPage() {
  const qc = useQueryClient()
  const [cursor, setCursor] = useState(0)
  const [showCommit, setShowCommit] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['knowledge', '', 'pending', ''],
    queryFn: () => api.listKnowledge({ status: 'pending' }),
  })

  const pending = data?.items ?? []

  const invalidate = () => qc.invalidateQueries({ queryKey: ['knowledge'] })

  const approveMutation = useMutation({
    mutationFn: (id: string) => api.updateKnowledge(id, { status: 'approved' }),
    onSuccess: () => { invalidate(); setCursor(c => Math.min(c, pending.length - 2)) },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deleteKnowledge(id),
    onSuccess: () => { invalidate(); setCursor(c => Math.min(c, pending.length - 2)) },
  })

  const skip = useCallback(() => {
    setCursor(c => Math.min(c + 1, pending.length - 1))
  }, [pending.length])

  const currentItem = pending[cursor]

  // Keyboard shortcuts
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (!currentItem) return
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return
      if (e.key === 'a') approveMutation.mutate(currentItem.id)
      if (e.key === 'd') deleteMutation.mutate(currentItem.id)
      if (e.key === 's') skip()
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [currentItem, approveMutation, deleteMutation, skip])

  return (
    <div className="max-w-2xl" ref={containerRef}>
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <div className="flex items-center gap-2 mb-1">
            <Link
              to="/knowledge"
              className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
              <ArrowLeft className="h-3 w-3" /> Knowledge
            </Link>
          </div>
          <h1 className="text-lg font-semibold">Review Queue</h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            {pending.length} pending item{pending.length !== 1 ? 's' : ''}
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={() => setShowCommit(true)}>
          <GitBranch className="h-3.5 w-3.5 mr-1" /> Commit to Repo
        </Button>
      </div>

      {isLoading && (
        <div className="py-16 text-center text-sm text-muted-foreground">Loading...</div>
      )}

      {!isLoading && pending.length === 0 && (
        <div className="py-16 text-center">
          <Check className="h-10 w-10 text-green-500 mx-auto mb-3" />
          <p className="text-sm font-medium">All caught up!</p>
          <p className="text-xs text-muted-foreground mt-1">No pending knowledge items to review.</p>
          <Button variant="outline" size="sm" className="mt-4" asChild>
            <Link to="/knowledge">View all knowledge</Link>
          </Button>
        </div>
      )}

      {currentItem && (
        <>
          {/* Progress bar */}
          <div className="mb-4">
            <div className="flex justify-between text-[10px] text-muted-foreground mb-1">
              <span>Item {cursor + 1} of {pending.length}</span>
              <span className="text-[10px] text-muted-foreground">a=approve · d=delete · s=skip</span>
            </div>
            <div className="h-1 rounded-full bg-muted overflow-hidden">
              <div
                className="h-full rounded-full bg-primary transition-all"
                style={{ width: `${((cursor + 1) / pending.length) * 100}%` }}
              />
            </div>
          </div>

          <ReviewCard
            item={currentItem}
            index={cursor}
            total={pending.length}
            onApprove={() => approveMutation.mutate(currentItem.id)}
            onDelete={() => deleteMutation.mutate(currentItem.id)}
            onSkip={skip}
            isPending={approveMutation.isPending || deleteMutation.isPending}
          />

          {/* Remaining items preview */}
          {pending.length > cursor + 1 && (
            <div className="mt-4 space-y-2">
              <p className="text-[10px] text-muted-foreground uppercase font-medium tracking-wide">Up next</p>
              {pending.slice(cursor + 1, cursor + 3).map((item, i) => (
                <div
                  key={item.id}
                  className="rounded-lg border bg-muted/30 px-4 py-2.5 flex items-center gap-3 cursor-pointer opacity-60 hover:opacity-100 transition-opacity"
                  onClick={() => setCursor(cursor + 1 + i)}
                >
                  <span className={cn('rounded px-1.5 py-0.5 text-[10px] font-medium capitalize', TYPE_COLORS[item.type] ?? 'bg-muted text-muted-foreground')}>
                    {item.type}
                  </span>
                  <span className="text-xs truncate">{item.summary}</span>
                </div>
              ))}
            </div>
          )}
        </>
      )}

      {showCommit && <CommitModal onClose={() => setShowCommit(false)} />}
    </div>
  )
}
```

- [ ] **Step 2: Add route to `web/src/App.tsx`**

```typescript
import { KnowledgeReviewPage } from './pages/KnowledgeReview'
// inside Routes:
<Route path="/knowledge/review" element={<KnowledgeReviewPage />} />
```

Note: add `/knowledge/review` route BEFORE `/knowledge` if using exact matching (react-router v6 handles this automatically).

- [ ] **Step 3: TypeScript check**

```bash
cd web && npx tsc --noEmit
```

- [ ] **Step 4: Build check**

```bash
go build ./...
```

- [ ] **Step 5: Lint**

```bash
make lint
```

- [ ] **Step 6: Run tests**

```bash
go test ./...
```

- [ ] **Step 7: Commit**

```bash
git add web/src/pages/KnowledgeReview.tsx web/src/App.tsx
git commit -m "feat(web): add Knowledge review queue with keyboard shortcuts and commit modal"
```

---

## Summary

| Chunk | Deliverable | Key files |
|-------|-------------|-----------|
| 1 | Backend YAML storage + retrieval endpoint | `server.go`, `handlers_create.go` |
| 2 | Frontend API client + TypeScript types | `types.ts`, `client.ts` |
| 3 | Retry Failed Groups button on task detail | `TaskDetail.tsx` |
| 4 | Knowledge list page | `KnowledgePage.tsx`, `App.tsx`, `Layout.tsx` |
| 5 | Knowledge review queue + commit modal | `KnowledgeReview.tsx`, `App.tsx` |

## Decisions

- YAML persistence: in-memory only for now; disk persistence deferred.
- Knowledge nav badge: YES — show count of pending items, like Inbox.
- Review Queue link: use react-router `<Link>` (no full-page reload).

**Additional change required in Chunk 4 (KnowledgePage):** Replace `<a href="/knowledge/review">` with `<Link to="/knowledge/review">` and import `Link` from react-router-dom.

**Additional change required in Layout.tsx (Chunk 4):** Fetch `listKnowledge({ status: 'pending' })` and show badge on Knowledge nav item.
