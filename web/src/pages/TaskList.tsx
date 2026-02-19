import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/api/client'
import { Badge } from '@/components/ui/badge'

const STATUS_OPTIONS = ['', 'Running', 'Completed', 'Failed'] as const
const STATUS_LABELS: Record<string, string> = { '': 'All', Running: 'Running', Completed: 'Completed', Failed: 'Failed' }
const STATUS_VARIANT: Record<string, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  running: 'default', awaiting_approval: 'default',
  completed: 'secondary', failed: 'destructive', cancelled: 'outline',
}

export function TaskListPage() {
  const [filter, setFilter] = useState('')
  const { data, isLoading } = useQuery({
    queryKey: ['tasks', filter],
    queryFn: () => api.listTasks(filter || undefined),
    refetchInterval: 10_000,
  })

  return (
    <div className="max-w-3xl">
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-lg font-semibold">All Tasks</h1>
        <div className="flex gap-2">
          {STATUS_OPTIONS.map((opt) => (
            <button
              key={opt}
              onClick={() => setFilter(opt)}
              className={`px-3 py-1 rounded text-sm border transition-colors ${
                filter === opt ? 'bg-foreground text-background' : 'hover:bg-muted'
              }`}
            >
              {STATUS_LABELS[opt]}
            </button>
          ))}
        </div>
      </div>

      {isLoading ? (
        <p className="text-sm text-muted-foreground">Loading...</p>
      ) : (
        <div className="space-y-2">
          {(data?.tasks ?? []).map((task) => (
            <Link
              key={task.workflow_id}
              to={`/tasks/${task.workflow_id}`}
              className="flex items-center justify-between border rounded-lg px-4 py-3 hover:border-foreground/30 transition-colors"
            >
              <div>
                <p className="font-mono text-sm font-medium">{task.workflow_id}</p>
                <p className="text-xs text-muted-foreground">{task.start_time}</p>
              </div>
              <Badge variant={STATUS_VARIANT[task.status] ?? 'outline'}>{task.status}</Badge>
            </Link>
          ))}
          {data?.tasks?.length === 0 && (
            <p className="text-sm text-muted-foreground text-center py-8">No tasks found</p>
          )}
        </div>
      )}
    </div>
  )
}
