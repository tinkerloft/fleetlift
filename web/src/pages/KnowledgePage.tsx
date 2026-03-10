// web/src/pages/KnowledgePage.tsx
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import type {
  KnowledgeItem, KnowledgeType, KnowledgeStatus,
  CreateKnowledgeRequest, UpdateKnowledgeRequest,
} from '@/api/types'
import { Button } from '@/components/ui/button'
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
          className={cn(
            'h-full rounded-full',
            value >= 0.7 ? 'bg-green-500' : value >= 0.4 ? 'bg-amber-500' : 'bg-red-400',
          )}
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
              <option key={t} value={t}>{t}</option>
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
        <label className="text-xs font-medium text-muted-foreground">
          Summary <span className="text-destructive">*</span>
        </label>
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
        <td className="py-2.5 pr-3 text-xs text-muted-foreground capitalize">
          {item.source.replace(/_/g, ' ')}
        </td>
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
                  <pre className="text-xs text-muted-foreground whitespace-pre-wrap font-mono bg-muted rounded px-3 py-2">
                    {item.details}
                  </pre>
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
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-lg font-semibold">Knowledge</h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            Reusable insights extracted from task executions
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" asChild>
            <Link to="/knowledge/review">Review Queue</Link>
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
            <option key={t} value={t}>{t}</option>
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
              <tr>
                <td colSpan={7} className="py-12 text-center text-sm text-muted-foreground">Loading...</td>
              </tr>
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
                isPending={
                  approveMutation.isPending ||
                  deleteMutation.isPending ||
                  updateMutation.isPending
                }
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
