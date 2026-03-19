import { useState, useCallback } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/EmptyState'
import { Skeleton } from '@/components/Skeleton'
import { formatTimeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Inbox as InboxIcon } from 'lucide-react'
import type { InboxItem } from '@/api/types'

type Filter = 'all' | 'awaiting_input' | 'failure' | 'output_ready' | 'notify' | 'request_input' | 'fan_out_partial_failure'

const FILTERS: { key: Filter; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'awaiting_input', label: 'Action Required' },
  { key: 'fan_out_partial_failure', label: 'Fan-out Issues' },
  { key: 'request_input', label: 'Input Requested' },
  { key: 'failure', label: 'Failures' },
  { key: 'output_ready', label: 'Output Ready' },
  { key: 'notify', label: 'Notifications' },
]

function KindBadge({ kind }: { kind: string }) {
  if (kind === 'awaiting_input') {
    return (
      <Badge variant="warning" className="gap-1">
        <span className="relative flex h-1.5 w-1.5">
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-current opacity-50" />
          <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-current" />
        </span>
        Action Required
      </Badge>
    )
  }
  if (kind === 'request_input') {
    return (
      <Badge variant="warning" className="gap-1">
        <span className="relative flex h-1.5 w-1.5">
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-current opacity-50" />
          <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-current" />
        </span>
        Input Requested
      </Badge>
    )
  }
  if (kind === 'fan_out_partial_failure') {
    return (
      <Badge variant="destructive" className="gap-1">
        <span className="relative flex h-1.5 w-1.5">
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-current opacity-50" />
          <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-current" />
        </span>
        Fan-out Partial Failure
      </Badge>
    )
  }
  if (kind === 'failure') {
    return <Badge variant="destructive">Step Failed</Badge>
  }
  if (kind === 'notify') {
    return <Badge variant="secondary">Notification</Badge>
  }
  return <Badge variant="success">Output Ready</Badge>
}

export function InboxPage() {
  const queryClient = useQueryClient()
  const [filter, setFilter] = useState<Filter>('all')
  const [steerOpenId, setSteerOpenId] = useState<string | null>(null)
  const [steerText, setSteerText] = useState('')
  const [respondOpenId, setRespondOpenId] = useState<string | null>(null)
  const [respondText, setRespondText] = useState('')

  const { data, isLoading } = useQuery({
    queryKey: ['inbox'],
    queryFn: () => api.listInbox(),
    refetchInterval: 5000,
  })

  const markReadMutation = useMutation({
    mutationFn: (id: string) => api.markInboxRead(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['inbox'] }),
  })

  const [actionError, setActionError] = useState<string | null>(null)
  const [resolvingId, setResolvingId] = useState<string | null>(null)

  const handleResolveFanOut = useCallback(async (item: InboxItem, action: 'proceed' | 'terminate') => {
    setActionError(null)
    setResolvingId(item.id)
    try {
      await api.resolveFanOut(item.run_id, action, item.step_id ?? '')
      queryClient.invalidateQueries({ queryKey: ['inbox'] })
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Action failed')
    } finally {
      setResolvingId(null)
    }
  }, [queryClient])

  const handleApprove = useCallback(async (item: InboxItem) => {
    setActionError(null)
    try {
      await api.approveRun(item.run_id)
      queryClient.invalidateQueries({ queryKey: ['inbox'] })
    } catch (err) { setActionError(err instanceof Error ? err.message : 'Action failed') }
  }, [queryClient])

  const handleReject = useCallback(async (item: InboxItem) => {
    setActionError(null)
    try {
      await api.rejectRun(item.run_id)
      queryClient.invalidateQueries({ queryKey: ['inbox'] })
    } catch (err) { setActionError(err instanceof Error ? err.message : 'Action failed') }
  }, [queryClient])

  const handleRespond = useCallback(async (item: InboxItem, answer: string) => {
    setActionError(null)
    try {
      await api.respondToInbox(item.id, answer)
      setRespondOpenId(null)
      setRespondText('')
      queryClient.invalidateQueries({ queryKey: ['inbox'] })
    } catch (err) { setActionError(err instanceof Error ? err.message : 'Action failed') }
  }, [queryClient])

  const handleSteer = useCallback(async (item: InboxItem) => {
    if (!steerText.trim()) return
    setActionError(null)
    try {
      await api.steerRun(item.run_id, steerText)
      setSteerText('')
      setSteerOpenId(null)
      queryClient.invalidateQueries({ queryKey: ['inbox'] })
    } catch (err) { setActionError(err instanceof Error ? err.message : 'Action failed') }
  }, [steerText, queryClient])

  const items = data?.items ?? []
  const filtered = filter === 'all' ? items : items.filter(i => i.kind === filter)

  const counts: Record<Filter, number> = {
    all: items.length,
    awaiting_input: items.filter(i => i.kind === 'awaiting_input').length,
    fan_out_partial_failure: items.filter(i => i.kind === 'fan_out_partial_failure').length,
    request_input: items.filter(i => i.kind === 'request_input').length,
    failure: items.filter(i => i.kind === 'failure').length,
    output_ready: items.filter(i => i.kind === 'output_ready').length,
    notify: items.filter(i => i.kind === 'notify').length,
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Inbox</h1>
        {items.length > 0 && (
          <p className="text-sm text-muted-foreground mt-1">{items.filter(i => !i.read).length} unread</p>
        )}
      </div>

      {/* Filter tabs */}
      <div className="flex gap-0 border-b">
        {FILTERS.map(f => (
          <button
            key={f.key}
            onClick={() => setFilter(f.key)}
            className={cn(
              'px-4 py-2 text-sm border-b-2 transition-colors',
              filter === f.key
                ? 'border-foreground text-foreground font-medium'
                : 'border-transparent text-muted-foreground hover:text-foreground',
            )}
          >
            {f.label}
            <span className={cn(
              'ml-1.5 text-[11px] px-1.5 py-px rounded-full',
              filter === f.key ? 'bg-foreground text-background' : 'bg-muted',
            )}>
              {counts[f.key]}
            </span>
          </button>
        ))}
      </div>

      {actionError && (
        <div className="rounded-md bg-red-500/10 border border-red-500/20 p-3 text-sm text-red-400">
          {actionError}
        </div>
      )}

      {isLoading && (
        <div className="space-y-3">
          <Skeleton className="h-20 rounded-lg" />
          <Skeleton className="h-20 rounded-lg" />
          <Skeleton className="h-20 rounded-lg" />
        </div>
      )}

      {!isLoading && filtered.length === 0 && (
        <EmptyState icon={InboxIcon} title="All caught up" description="No pending notifications." />
      )}

      <div className="space-y-2">
        {filtered.map(item => (
          <div
            key={item.id}
            className={cn(
              'rounded-lg border bg-card p-4 transition-all',
              !item.read && item.kind === 'awaiting_input' && 'border-l-4 border-l-amber-500',
              !item.read && item.kind === 'request_input' && 'border-l-4 border-l-amber-500',
              !item.read && item.kind === 'fan_out_partial_failure' && 'border-l-4 border-l-red-500',
              !item.read && item.kind !== 'awaiting_input' && item.kind !== 'request_input' && item.kind !== 'fan_out_partial_failure' && 'border-l-4 border-l-blue-500',
            )}
          >
            <div className="flex items-start justify-between gap-4">
              <div className="flex-1 min-w-0 space-y-1">
                <div className="flex items-center gap-2">
                  <KindBadge kind={item.kind} />
                </div>
                <Link to={`/runs/${item.run_id}`} className="text-sm font-medium hover:underline block">
                  {item.title}
                </Link>
                {item.kind === 'request_input' && item.question && (
                  <p className="text-sm font-medium text-amber-400">{item.question}</p>
                )}
                {item.summary && (
                  <p className="text-[13px] text-muted-foreground line-clamp-2">{item.summary}</p>
                )}
                <p className="text-xs text-muted-foreground">{formatTimeAgo(item.created_at)}</p>
              </div>
              <div className="flex flex-col items-end gap-2 shrink-0">
                {item.kind === 'fan_out_partial_failure' && !item.answer && (
                  <div className="flex gap-1.5">
                    <Button
                      size="sm" variant="default"
                      className="h-7 text-xs bg-green-600 hover:bg-green-700"
                      disabled={resolvingId === item.id}
                      onClick={() => handleResolveFanOut(item, 'proceed')}
                    >
                      Proceed with results
                    </Button>
                    <Button
                      size="sm" variant="destructive"
                      className="h-7 text-xs"
                      disabled={resolvingId === item.id}
                      onClick={() => handleResolveFanOut(item, 'terminate')}
                    >
                      Terminate
                    </Button>
                  </div>
                )}
                {item.kind === 'fan_out_partial_failure' && item.answer && (
                  <Badge variant="secondary">{item.answer === 'proceed' ? 'Proceeded' : 'Terminated'}</Badge>
                )}
                {item.kind === 'awaiting_input' && (
                  <div className="flex gap-1.5">
                    <Button size="sm" variant="default" className="h-7 text-xs bg-green-600 hover:bg-green-700" onClick={() => handleApprove(item)}>
                      Approve
                    </Button>
                    <Button size="sm" variant="destructive" className="h-7 text-xs" onClick={() => handleReject(item)}>
                      Reject
                    </Button>
                    <Button size="sm" variant="secondary" className="h-7 text-xs" onClick={() => {
                      setSteerOpenId(steerOpenId === item.id ? null : item.id)
                      setSteerText('')
                    }}>
                      Steer
                    </Button>
                  </div>
                )}
                {item.kind === 'request_input' && !item.answer && (
                  <div className="flex flex-col gap-2">
                    {item.options && item.options.length > 0 ? (
                      <div className="flex gap-1.5 flex-wrap">
                        {item.options.map(opt => (
                          <Button key={opt} size="sm" variant="secondary" className="h-7 text-xs" onClick={() => handleRespond(item, opt)}>
                            {opt}
                          </Button>
                        ))}
                      </div>
                    ) : (
                      <Button size="sm" variant="secondary" className="h-7 text-xs" onClick={() => {
                        setRespondOpenId(respondOpenId === item.id ? null : item.id)
                        setRespondText('')
                      }}>
                        Respond
                      </Button>
                    )}
                  </div>
                )}
                {item.kind === 'request_input' && item.answer && (
                  <Badge variant="secondary">Answered</Badge>
                )}
                {item.kind !== 'awaiting_input' && item.kind !== 'request_input' && (
                  <div className="flex gap-1.5">
                    <Button size="sm" variant="secondary" className="h-7 text-xs" asChild>
                      <Link to={`/runs/${item.run_id}`}>View</Link>
                    </Button>
                    {(item.kind === 'output_ready' || item.kind === 'notify') && (
                      <Button size="sm" variant="secondary" className="h-7 text-xs" asChild>
                        <Link to={`/reports/${item.run_id}`}>View Report →</Link>
                      </Button>
                    )}
                    {!item.read && (
                      <Button size="sm" variant="secondary" className="h-7 text-xs" onClick={() => markReadMutation.mutate(item.id)}>
                        Mark Read
                      </Button>
                    )}
                  </div>
                )}
              </div>
            </div>
            {/* Steer form */}
            {steerOpenId === item.id && (
              <div className="flex gap-2 mt-3 pt-3 border-t">
                <input
                  className="flex-1 rounded-md border bg-background px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                  placeholder="Provide alternative instructions..."
                  value={steerText}
                  onChange={e => setSteerText(e.target.value)}
                  onKeyDown={e => e.key === 'Enter' && handleSteer(item)}
                />
                <Button size="sm" onClick={() => handleSteer(item)} disabled={!steerText.trim()}>
                  Send
                </Button>
              </div>
            )}
            {/* Respond form */}
            {respondOpenId === item.id && (
              <div className="flex gap-2 mt-3 pt-3 border-t">
                <input
                  className="flex-1 rounded-md border bg-background px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                  placeholder="Type your response..."
                  value={respondText}
                  onChange={e => setRespondText(e.target.value)}
                  onKeyDown={e => e.key === 'Enter' && handleRespond(item, respondText)}
                />
                <Button size="sm" onClick={() => handleRespond(item, respondText)} disabled={!respondText.trim()}>
                  Submit
                </Button>
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
