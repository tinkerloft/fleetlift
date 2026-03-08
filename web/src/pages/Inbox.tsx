import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/api/client'
import type { TaskSummary } from '@/api/types'
import { InboxBadge } from '@/components/StatusIcon'
import { CheckCircle2 } from 'lucide-react'

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

function InboxItem({ item }: { item: TaskSummary }) {
  return (
    <Link
      to={`/tasks/${item.workflow_id}`}
      className="flex items-center gap-3 rounded-lg border px-4 py-3 hover:bg-muted/30 transition-colors group"
    >
      <div className="flex-1 min-w-0">
        <p className="font-mono text-sm font-medium truncate group-hover:text-blue-600 transition-colors">
          {item.workflow_id}
        </p>
        <p className="text-xs text-muted-foreground mt-0.5">
          {formatTimeAgo(item.start_time)}
        </p>
      </div>
      <InboxBadge type={item.inbox_type} isPaused={item.is_paused} />
    </Link>
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

  const items = data?.items ?? []

  return (
    <div className="max-w-3xl">
      <div className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight">Inbox</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Workflows that need your attention
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
