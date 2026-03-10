import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/api/client'
import type { TaskSummary } from '@/api/types'
import { InboxBadge } from '@/components/StatusIcon'
import { CheckCircle2, Check, X } from 'lucide-react'
import { Button } from '@/components/ui/button'

function formatTimeAgo(dateStr: string): string {
  const date = new Date(dateStr.replace(' ', 'T') + 'Z')
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMin = Math.floor(diffMs / 60_000)
  if (diffMin < 1) return 'just now'
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHrs = Math.floor(diffMin / 60)
  if (diffHrs < 24) return `${diffHrs}h ago`
  const diffDays = Math.floor(diffHrs / 24)
  return `${diffDays}d ago`
}

function inboxSubtitle(item: TaskSummary): string {
  if (item.inbox_type === 'awaiting_approval') return 'Awaiting your approval'
  if (item.is_paused) return 'Paused — pending input'
  if (item.inbox_type === 'completed_review') return 'Completed — review result'
  return 'Needs attention'
}

function InboxItem({ item }: { item: TaskSummary }) {
  const qc = useQueryClient()
  const invalidate = () => qc.invalidateQueries({ queryKey: ['inbox'] })

  const approveMutation = useMutation({
    mutationFn: () => api.approve(item.workflow_id),
    onSuccess: invalidate,
  })
  const rejectMutation = useMutation({
    mutationFn: () => api.reject(item.workflow_id),
    onSuccess: invalidate,
  })

  const isAwaitingApproval = item.inbox_type === 'awaiting_approval'
  const isPending = approveMutation.isPending || rejectMutation.isPending

  return (
    <div className="flex items-center gap-3 rounded-lg border px-4 py-3 hover:bg-muted/20 transition-colors">
      {/* Main link area */}
      <Link
        to={`/tasks/${item.workflow_id}`}
        className="flex-1 min-w-0 group"
      >
        <p className="font-mono text-sm font-medium truncate group-hover:text-blue-600 transition-colors">
          {item.workflow_id}
        </p>
        <p className="text-xs text-muted-foreground mt-0.5">
          {inboxSubtitle(item)} · {formatTimeAgo(item.start_time)}
        </p>
      </Link>

      {/* Badge */}
      <InboxBadge type={item.inbox_type} isPaused={item.is_paused} />

      {/* Inline actions for awaiting approval */}
      {isAwaitingApproval && (
        <div className="flex items-center gap-1.5 shrink-0 ml-1">
          <Button
            size="sm"
            className="h-7 px-2.5 text-xs bg-green-600 hover:bg-green-700 text-white"
            onClick={(e) => { e.preventDefault(); approveMutation.mutate() }}
            disabled={isPending}
            title="Approve"
          >
            <Check className="h-3.5 w-3.5 mr-1" />
            Approve
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="h-7 px-2.5 text-xs text-destructive hover:bg-destructive/10"
            onClick={(e) => { e.preventDefault(); rejectMutation.mutate() }}
            disabled={isPending}
            title="Reject"
          >
            <X className="h-3.5 w-3.5 mr-1" />
            Reject
          </Button>
        </div>
      )}
    </div>
  )
}

export function InboxPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['inbox'],
    queryFn: () => api.getInbox(),
    refetchInterval: 5000,
  })

  if (isLoading) {
    return (
      <div className="max-w-3xl space-y-2">
        {[...Array(3)].map((_, i) => (
          <div key={i} className="h-16 rounded-lg border bg-muted/30 animate-pulse" />
        ))}
      </div>
    )
  }
  if (error) return <p className="text-sm text-destructive">Error: {String(error)}</p>

  // Sort by urgency: longest-waiting first (ascending start_time)
  const items = [...(data?.items ?? [])].sort(
    (a, b) => new Date(a.start_time).getTime() - new Date(b.start_time).getTime()
  )

  return (
    <div className="max-w-3xl">
      <div className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight">Inbox</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Workflows that need your attention
          {items.length > 0 && ` · ${items.length} item${items.length !== 1 ? 's' : ''}`}
        </p>
      </div>
      {items.length === 0 ? (
        <div className="flex flex-col items-center py-16 text-center">
          <div className="flex h-12 w-12 items-center justify-center rounded-full bg-emerald-50 mb-3">
            <CheckCircle2 className="h-6 w-6 text-emerald-500" />
          </div>
          <p className="text-sm font-medium">All clear</p>
          <p className="text-xs text-muted-foreground mt-1">No workflows need your attention right now</p>
        </div>
      ) : (
        <div className="space-y-1.5">
          {items.map((item) => <InboxItem key={item.workflow_id} item={item} />)}
        </div>
      )}
    </div>
  )
}
