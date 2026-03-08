import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/api/client'
import type { TaskSummary } from '@/api/types'
import { StatusBadge, InboxBadge } from '@/components/StatusIcon'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Activity, AlertCircle, CheckCircle2, Clock, Eye,
} from 'lucide-react'
import { cn } from '@/lib/utils'

function StatCard({ title, value, icon: Icon, description, variant }: {
  title: string
  value: number
  icon: React.FC<{ className?: string }>
  description?: string
  variant?: 'default' | 'success' | 'warning' | 'danger'
}) {
  const iconColor = {
    default: 'text-muted-foreground',
    success: 'text-emerald-600',
    warning: 'text-amber-600',
    danger: 'text-red-600',
  }[variant ?? 'default']

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between pb-2 space-y-0">
        <CardTitle className="text-sm font-medium text-muted-foreground">{title}</CardTitle>
        <Icon className={cn('h-4 w-4', iconColor)} />
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-semibold tabular-nums">{value}</div>
        {description && (
          <p className="text-xs text-muted-foreground mt-1">{description}</p>
        )}
      </CardContent>
    </Card>
  )
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

function TaskRow({ task, showInbox }: { task: TaskSummary; showInbox?: boolean }) {
  return (
    <Link
      to={`/tasks/${task.workflow_id}`}
      className="flex items-center gap-3 rounded-lg border px-4 py-3 hover:bg-muted/30 transition-colors group"
    >
      <div className="flex-1 min-w-0">
        <p className="font-mono text-sm font-medium truncate group-hover:text-blue-600 transition-colors">
          {task.workflow_id}
        </p>
        <p className="text-xs text-muted-foreground mt-0.5">
          {formatTimeAgo(task.start_time)}
        </p>
      </div>
      {showInbox && task.inbox_type ? (
        <InboxBadge type={task.inbox_type} isPaused={task.is_paused} />
      ) : (
        <StatusBadge status={task.status} />
      )}
    </Link>
  )
}

function FailedTaskRow({ task }: { task: TaskSummary }) {
  return (
    <Link
      to={`/tasks/${task.workflow_id}`}
      className="flex items-center gap-3 rounded-lg border border-red-100 bg-red-50/30 px-4 py-3 hover:bg-red-50/60 transition-colors group"
    >
      <AlertCircle className="h-4 w-4 text-red-500 shrink-0" />
      <div className="flex-1 min-w-0">
        <p className="font-mono text-sm font-medium truncate group-hover:text-red-700 transition-colors">
          {task.workflow_id}
        </p>
        <p className="text-xs text-muted-foreground mt-0.5">
          Failed {formatTimeAgo(task.start_time)}
        </p>
      </div>
      <StatusBadge status="failed" />
    </Link>
  )
}

export function DashboardPage() {
  const { data: allData } = useQuery({
    queryKey: ['tasks'],
    queryFn: () => api.listTasks(),
    refetchInterval: 5000,
  })

  const { data: runningData } = useQuery({
    queryKey: ['tasks', 'Running'],
    queryFn: () => api.listTasks('Running'),
    refetchInterval: 5000,
  })

  const { data: inboxData } = useQuery({
    queryKey: ['inbox'],
    queryFn: () => api.getInbox(),
    refetchInterval: 5000,
  })

  const allTasks = allData?.tasks ?? []
  const runningTasks = runningData?.tasks ?? []
  const inboxItems = inboxData?.items ?? []

  const completedTasks = allTasks.filter(t => t.status === 'completed')
  const failedTasks = allTasks.filter(t => t.status === 'failed')
  const awaitingApproval = inboxItems.filter(t => t.inbox_type === 'awaiting_approval')
  const pausedTasks = inboxItems.filter(t => t.inbox_type === 'paused' || t.is_paused)

  // Recent completed (last 5)
  const recentCompleted = completedTasks.slice(0, 5)
  // Recent failed (last 5)
  const recentFailed = failedTasks.slice(0, 5)

  return (
    <div className="space-y-8">
      {/* Page header */}
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Dashboard</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Overview of your Fleetlift workflows
        </p>
      </div>

      {/* Stats row */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          title="Running"
          value={runningTasks.length}
          icon={Activity}
          variant={runningTasks.length > 0 ? 'default' : 'default'}
          description={runningTasks.length > 0 ? 'Active workflows' : 'No active workflows'}
        />
        <StatCard
          title="Needs Attention"
          value={awaitingApproval.length + pausedTasks.length}
          icon={Eye}
          variant={awaitingApproval.length + pausedTasks.length > 0 ? 'warning' : 'default'}
          description={[
            awaitingApproval.length > 0 ? `${awaitingApproval.length} awaiting approval` : '',
            pausedTasks.length > 0 ? `${pausedTasks.length} paused` : '',
          ].filter(Boolean).join(', ') || 'All clear'}
        />
        <StatCard
          title="Completed"
          value={completedTasks.length}
          icon={CheckCircle2}
          variant="success"
        />
        <StatCard
          title="Failed"
          value={failedTasks.length}
          icon={AlertCircle}
          variant={failedTasks.length > 0 ? 'danger' : 'default'}
        />
      </div>

      {/* Content columns */}
      <div className="grid gap-6 lg:grid-cols-2">
        {/* Action items */}
        <Card>
          <CardHeader className="pb-3">
            <div className="flex items-center justify-between">
              <CardTitle className="text-sm font-medium">Action Required</CardTitle>
              {inboxItems.length > 0 && (
                <Link to="/inbox" className="text-xs text-blue-600 hover:underline">
                  View all
                </Link>
              )}
            </div>
          </CardHeader>
          <CardContent className="space-y-2">
            {inboxItems.length === 0 ? (
              <div className="flex flex-col items-center py-8 text-center">
                <CheckCircle2 className="h-8 w-8 text-emerald-500 mb-2" />
                <p className="text-sm text-muted-foreground">No pending actions</p>
              </div>
            ) : (
              inboxItems.slice(0, 5).map(task => (
                <TaskRow key={task.workflow_id} task={task} showInbox />
              ))
            )}
          </CardContent>
        </Card>

        {/* Currently running */}
        <Card>
          <CardHeader className="pb-3">
            <div className="flex items-center justify-between">
              <CardTitle className="text-sm font-medium">Running</CardTitle>
              {runningTasks.length > 0 && (
                <Link to="/tasks?status=Running" className="text-xs text-blue-600 hover:underline">
                  View all
                </Link>
              )}
            </div>
          </CardHeader>
          <CardContent className="space-y-2">
            {runningTasks.length === 0 ? (
              <div className="flex flex-col items-center py-8 text-center">
                <Clock className="h-8 w-8 text-muted-foreground/40 mb-2" />
                <p className="text-sm text-muted-foreground">No running workflows</p>
              </div>
            ) : (
              runningTasks.slice(0, 5).map(task => (
                <TaskRow key={task.workflow_id} task={task} />
              ))
            )}
          </CardContent>
        </Card>

        {/* Recent failures */}
        {recentFailed.length > 0 && (
          <Card>
            <CardHeader className="pb-3">
              <div className="flex items-center justify-between">
                <CardTitle className="text-sm font-medium text-red-700">Recent Failures</CardTitle>
                <Link to="/tasks?status=Failed" className="text-xs text-blue-600 hover:underline">
                  View all
                </Link>
              </div>
            </CardHeader>
            <CardContent className="space-y-2">
              {recentFailed.map(task => (
                <FailedTaskRow key={task.workflow_id} task={task} />
              ))}
            </CardContent>
          </Card>
        )}

        {/* Recent completions */}
        {recentCompleted.length > 0 && (
          <Card>
            <CardHeader className="pb-3">
              <div className="flex items-center justify-between">
                <CardTitle className="text-sm font-medium">Recently Completed</CardTitle>
                <Link to="/tasks?status=Completed" className="text-xs text-blue-600 hover:underline">
                  View all
                </Link>
              </div>
            </CardHeader>
            <CardContent className="space-y-2">
              {recentCompleted.map(task => (
                <TaskRow key={task.workflow_id} task={task} />
              ))}
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  )
}
