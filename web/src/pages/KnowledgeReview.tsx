// web/src/pages/KnowledgeReview.tsx
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { KnowledgeItem, UpdateKnowledgeRequest } from '@/api/types'
import { Button } from '@/components/ui/button'
import { ArrowLeft, Check, Trash2, SkipForward, GitBranch, X } from 'lucide-react'
import { cn } from '@/lib/utils'

const TYPE_COLORS: Record<string, string> = {
  pattern:    'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400',
  correction: 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400',
  gotcha:     'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400',
  context:    'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400',
}

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
            <p className="text-sm font-medium">
              Committed {result.committed} item{result.committed !== 1 ? 's' : ''}
            </p>
            <p className="text-xs text-muted-foreground mt-1">
              Written to {repoPath}/.fleetlift/knowledge/items/
            </p>
            <Button className="mt-4" size="sm" onClick={onClose}>Done</Button>
          </div>
        ) : (
          <>
            <p className="text-xs text-muted-foreground mb-4">
              Copies all approved knowledge items to the target repository&apos;s{' '}
              <code className="font-mono">.fleetlift/knowledge/items/</code> directory.
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
              <Button variant="outline" size="sm" onClick={onClose} disabled={mutation.isPending}>
                Cancel
              </Button>
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
          <span className={cn(
            'rounded px-1.5 py-0.5 text-[10px] font-medium capitalize',
            TYPE_COLORS[item.type] ?? 'bg-muted text-muted-foreground',
          )}>
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
            <span key={t} className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
              {t}
            </span>
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

export function KnowledgeReviewPage() {
  const qc = useQueryClient()
  const [cursor, setCursor] = useState(0)
  const [showCommit, setShowCommit] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: ['knowledge-review'],
    queryFn: () => api.listKnowledge({ status: 'pending' }),
  })

  const pending = data?.items ?? []

  const invalidate = useCallback(() => {
    qc.invalidateQueries({ queryKey: ['knowledge-review'] })
    qc.invalidateQueries({ queryKey: ['knowledge-pending-count'] })
    qc.invalidateQueries({ queryKey: ['knowledge'] })
  }, [qc])

  const advanceCursor = useCallback((listLength: number) => {
    setCursor(c => Math.min(c, Math.max(0, listLength - 2)))
  }, [])

  const approveMutation = useMutation({
    mutationFn: (id: string) => api.updateKnowledge(id, { status: 'approved' } as UpdateKnowledgeRequest),
    onSuccess: () => { invalidate(); advanceCursor(pending.length) },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deleteKnowledge(id),
    onSuccess: () => { invalidate(); advanceCursor(pending.length) },
  })

  const skip = useCallback(() => {
    setCursor(c => Math.min(c + 1, Math.max(0, pending.length - 1)))
  }, [pending.length])

  const currentItem = pending[cursor]

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
    <div className="max-w-2xl">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <Link
            to="/knowledge"
            className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground mb-1 transition-colors"
          >
            <ArrowLeft className="h-3 w-3" /> Knowledge
          </Link>
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
          {/* Progress */}
          <div className="mb-4">
            <div className="flex justify-between text-[10px] text-muted-foreground mb-1">
              <span>Item {cursor + 1} of {pending.length}</span>
              <span>a=approve · d=delete · s=skip</span>
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

          {/* Up next preview */}
          {pending.length > cursor + 1 && (
            <div className="mt-4 space-y-2">
              <p className="text-[10px] text-muted-foreground uppercase font-medium tracking-wide">Up next</p>
              {pending.slice(cursor + 1, cursor + 3).map((item, i) => (
                <div
                  key={item.id}
                  className="rounded-lg border bg-muted/30 px-4 py-2.5 flex items-center gap-3 cursor-pointer opacity-60 hover:opacity-100 transition-opacity"
                  onClick={() => setCursor(cursor + 1 + i)}
                >
                  <span className={cn(
                    'rounded px-1.5 py-0.5 text-[10px] font-medium capitalize',
                    TYPE_COLORS[item.type] ?? 'bg-muted text-muted-foreground',
                  )}>
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
