import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api, subscribeToTask } from '@/api/client'
import type { TaskStatus } from '@/api/types'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { DiffViewer } from '@/components/DiffViewer'
import { VerifierLogs } from '@/components/VerifierLogs'
import { SteeringPanel } from '@/components/SteeringPanel'
import { GroupProgress } from '@/components/GroupProgress'

const STATUS_VARIANT: Record<string, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  running: 'default', awaiting_approval: 'default',
  completed: 'secondary', failed: 'destructive',
}

export function TaskDetailPage() {
  const { id } = useParams<{ id: string }>()
  const [liveStatus, setLiveStatus] = useState<TaskStatus | null>(null)

  const { data: task } = useQuery({
    queryKey: ['task', id],
    queryFn: () => api.getTask(id!),
    enabled: !!id,
  })

  useEffect(() => {
    if (!id) return
    return subscribeToTask(id, (s) => setLiveStatus(s as TaskStatus))
  }, [id])

  const status = liveStatus ?? task?.status
  const isAwaitingApproval = status === 'awaiting_approval'

  return (
    <div className="max-w-5xl">
      <div className="flex items-center gap-3 mb-6">
        <h1 className="text-lg font-semibold font-mono">{id}</h1>
        {status && (
          <Badge variant={STATUS_VARIANT[status] ?? 'outline'}>{status}</Badge>
        )}
      </div>

      <Tabs defaultValue="diff">
        <TabsList>
          <TabsTrigger value="diff">Diff</TabsTrigger>
          <TabsTrigger value="logs">Verifier Logs</TabsTrigger>
          <TabsTrigger value="progress">Group Progress</TabsTrigger>
          {isAwaitingApproval && (
            <TabsTrigger value="steer">Approve / Steer</TabsTrigger>
          )}
        </TabsList>
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
      </Tabs>
    </div>
  )
}
