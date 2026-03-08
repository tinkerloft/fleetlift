import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { api } from '@/api/client'
import { StatusBadge } from '@/components/StatusIcon'
import { cn } from '@/lib/utils'
import { Search } from 'lucide-react'

const STATUS_OPTIONS = ['', 'Running', 'Completed', 'Failed', 'Canceled'] as const
const STATUS_LABELS: Record<string, string> = {
  '': 'All', Running: 'Running', Completed: 'Completed', Failed: 'Failed', Canceled: 'Canceled',
}

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

export function TaskListPage() {
  const [searchParams] = useSearchParams()
  const initialFilter = searchParams.get('status') ?? ''
  const [filter, setFilter] = useState(initialFilter)
  const [search, setSearch] = useState('')

  const { data, isLoading } = useQuery({
    queryKey: ['tasks', filter],
    queryFn: () => api.listTasks(filter || undefined),
    refetchInterval: 10_000,
  })

  const tasks = (data?.tasks ?? []).filter(t =>
    !search || t.workflow_id.toLowerCase().includes(search.toLowerCase())
  )

  return (
    <div className="max-w-4xl">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Tasks</h1>
          <p className="text-sm text-muted-foreground mt-1">
            {tasks.length} workflow{tasks.length !== 1 ? 's' : ''}
          </p>
        </div>
      </div>

      {/* Filters */}
      <div className="flex items-center gap-3 mb-4">
        <div className="flex gap-1 rounded-lg bg-muted p-1">
          {STATUS_OPTIONS.map((opt) => (
            <button
              key={opt}
              onClick={() => setFilter(opt)}
              className={cn(
                'px-3 py-1.5 rounded-md text-xs font-medium transition-colors',
                filter === opt
                  ? 'bg-background text-foreground shadow-sm'
                  : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {STATUS_LABELS[opt]}
            </button>
          ))}
        </div>
        <div className="relative flex-1 max-w-xs">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
          <input
            type="text"
            placeholder="Filter by ID..."
            value={search}
            onChange={e => setSearch(e.target.value)}
            className="w-full rounded-lg border bg-background pl-9 pr-3 py-1.5 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
          />
        </div>
      </div>

      {/* Task list */}
      {isLoading ? (
        <div className="space-y-2">
          {[...Array(3)].map((_, i) => (
            <div key={i} className="h-16 rounded-lg border bg-muted/30 animate-pulse" />
          ))}
        </div>
      ) : (
        <div className="space-y-1.5">
          {tasks.map((task) => (
            <Link
              key={task.workflow_id}
              to={`/tasks/${task.workflow_id}`}
              className="flex items-center gap-4 rounded-lg border px-4 py-3 hover:bg-muted/30 transition-colors group"
            >
              <div className="flex-1 min-w-0">
                <p className="font-mono text-sm font-medium truncate group-hover:text-blue-600 transition-colors">
                  {task.workflow_id}
                </p>
                <p className="text-xs text-muted-foreground mt-0.5">
                  Started {formatTimeAgo(task.start_time)}
                  <span className="mx-1.5 text-border">|</span>
                  {task.start_time}
                </p>
              </div>
              <StatusBadge status={task.status} />
            </Link>
          ))}
          {tasks.length === 0 && (
            <div className="flex flex-col items-center py-12 text-center">
              <p className="text-sm text-muted-foreground">No tasks found</p>
              {(filter || search) && (
                <button
                  onClick={() => { setFilter(''); setSearch('') }}
                  className="text-xs text-blue-600 hover:underline mt-2"
                >
                  Clear filters
                </button>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
