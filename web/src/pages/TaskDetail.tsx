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
          </div>
        </div>
      </div>

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
