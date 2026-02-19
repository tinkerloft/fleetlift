import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'

export function SteeringPanel({ workflowId }: { workflowId: string }) {
  const [prompt, setPrompt] = useState('')
  const qc = useQueryClient()

  const { data: steering } = useQuery({
    queryKey: ['steering', workflowId],
    queryFn: () => api.getSteering(workflowId),
  })

  const approveMutation = useMutation({
    mutationFn: () => api.approve(workflowId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['task', workflowId] }),
  })
  const rejectMutation = useMutation({
    mutationFn: () => api.reject(workflowId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['task', workflowId] }),
  })
  const steerMutation = useMutation({
    mutationFn: () => api.steer(workflowId, prompt),
    onSuccess: () => {
      setPrompt('')
      qc.invalidateQueries({ queryKey: ['steering', workflowId] })
    },
  })

  const busy = approveMutation.isPending || rejectMutation.isPending || steerMutation.isPending

  return (
    <div className="space-y-6 max-w-2xl">
      {/* Approve / Reject */}
      <div>
        <h3 className="text-sm font-medium mb-3">Decision</h3>
        <div className="flex gap-3">
          <Button onClick={() => approveMutation.mutate()} disabled={busy}
            className="bg-green-600 hover:bg-green-700">
            Approve &amp; Create PRs
          </Button>
          <Button variant="outline" onClick={() => rejectMutation.mutate()} disabled={busy}>
            Reject
          </Button>
        </div>
        {approveMutation.isSuccess && (
          <p className="text-sm text-green-600 mt-2">Approved — PRs being created...</p>
        )}
        {rejectMutation.isSuccess && (
          <p className="text-sm text-muted-foreground mt-2">Changes rejected.</p>
        )}
      </div>

      <Separator />

      {/* Steering */}
      <div>
        <h3 className="text-sm font-medium mb-1">
          Steer the agent
          {steering && (
            <span className="text-muted-foreground font-normal ml-2 text-xs">
              ({steering.current_iteration}/{steering.max_iterations} iterations used)
            </span>
          )}
        </h3>
        <p className="text-xs text-muted-foreground mb-3">
          The agent will make another attempt incorporating your guidance.
        </p>
        <textarea
          className="w-full border rounded-md px-3 py-2 text-sm min-h-[80px] resize-y focus:outline-none focus:ring-1 focus:ring-ring bg-background"
          placeholder="e.g. Use slog instead of log. Also update the test helpers that wrap the logger..."
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
        />
        <Button
          className="mt-2"
          onClick={() => steerMutation.mutate()}
          disabled={busy || !prompt.trim()}
        >
          {steerMutation.isPending ? 'Sending...' : 'Send Steering Prompt'}
        </Button>
      </div>

      {/* History */}
      {!!steering?.history?.length && (
        <>
          <Separator />
          <div>
            <h3 className="text-sm font-medium mb-3">Steering history</h3>
            <div className="space-y-2">
              {steering.history.map((item, i) => (
                <div key={i} className="text-sm border rounded-md px-3 py-2 bg-muted/30">
                  <span className="text-xs text-muted-foreground">Iteration {item.iteration_number}: </span>
                  {item.prompt}
                </div>
              ))}
            </div>
          </div>
        </>
      )}
    </div>
  )
}
