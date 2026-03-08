import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
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
import { ArrowLeft, XCircle } from 'lucide-react'

const TERMINAL_STATUSES: TaskStatus[] = ['completed', 'failed', 'cancelled']

export function TaskDetailPage() {
  const { id } = useParams<{ id: string }>()
  const [liveStatus, setLiveStatus] = useState<TaskStatus | null>(null)

  const { data: task } = useQuery({
    queryKey: ['task', id],
    queryFn: () => api.getTask(id!),
    enabled: !!id,
  })

  const cancelMutation = useMutation({
    mutationFn: () => api.cancel(id!),
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
          {isRunning && !isAwaitingApproval && (
            <Button
              variant="outline"
              size="sm"
              className="text-red-600 hover:text-red-700 hover:bg-red-50 shrink-0"
              onClick={() => cancelMutation.mutate()}
              disabled={cancelMutation.isPending}
            >
              <XCircle className="h-3.5 w-3.5 mr-1" />
              Cancel
            </Button>
          )}
        </div>
      </div>

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
