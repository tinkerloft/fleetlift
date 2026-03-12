import { useState, useEffect, useCallback } from 'react'
import { useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, subscribeToRun } from '@/api/client'
import { DAGGraph } from '@/components/DAGGraph'
import { StepPanel } from '@/components/StepPanel'
import { HITLPanel } from '@/components/HITLPanel'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import type { StepDef, StepRun, Run, RunStatusUpdate, StepRunLog } from '@/api/types'

export function RunDetailPage() {
  const { id } = useParams<{ id: string }>()
  const queryClient = useQueryClient()
  const [selectedStepId, setSelectedStepId] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [sseConnected, setSseConnected] = useState(false)

  const { data: run } = useQuery({
    queryKey: ['run', id],
    queryFn: () => api.getRun(id!),
    enabled: !!id,
    refetchInterval: sseConnected ? false : 5000,
  })

  // SSE subscription for live updates
  useEffect(() => {
    if (!id) return
    let cleanup: (() => void) | undefined
    setSseConnected(false)

    subscribeToRun(
      id,
      (_update: RunStatusUpdate) => {
        queryClient.invalidateQueries({ queryKey: ['run', id] })
      },
      (_log: StepRunLog) => {
        // Logs are handled by LogStream component per step
      },
      () => setSseConnected(false),
    ).then((unsub) => {
      setSseConnected(true)
      cleanup = unsub
    })

    return () => cleanup?.()
  }, [id, queryClient])

  const handleApprove = useCallback(async () => {
    if (!id) return
    setActionError(null)
    try {
      await api.approveRun(id)
      queryClient.invalidateQueries({ queryKey: ['run', id] })
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Action failed')
    }
  }, [id, queryClient])

  const handleReject = useCallback(async () => {
    if (!id) return
    setActionError(null)
    try {
      await api.rejectRun(id)
      queryClient.invalidateQueries({ queryKey: ['run', id] })
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Action failed')
    }
  }, [id, queryClient])

  const handleSteer = useCallback(async (prompt: string) => {
    if (!id) return
    setActionError(null)
    try {
      await api.steerRun(id, prompt)
      queryClient.invalidateQueries({ queryKey: ['run', id] })
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Action failed')
    }
  }, [id, queryClient])

  const handleCancel = useCallback(async () => {
    if (!id) return
    setActionError(null)
    try {
      await api.cancelRun(id)
      queryClient.invalidateQueries({ queryKey: ['run', id] })
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Action failed')
    }
  }, [id, queryClient])

  if (!run) {
    return <p className="text-muted-foreground text-sm">Loading run...</p>
  }

  // Parse workflow def from template for DAG visualization
  let steps: StepDef[] = []
  try {
    // The run may have a workflow_def embedded, or we parse from the workflow
    // For now, extract steps from step_runs
    if (run.steps) {
      steps = run.steps.map((sr: StepRun) => ({
        id: sr.step_id,
        title: sr.step_title,
      }))
    }
  } catch {
    // Ignore parse errors
  }

  const stepRuns = run.steps ?? []
  const selectedStep = stepRuns.find((sr: StepRun) => sr.step_id === selectedStepId)
  const awaitingStep = stepRuns.find((sr: StepRun) => sr.status === 'awaiting_input')

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">{run.workflow_title}</h1>
          <p className="text-sm text-muted-foreground font-mono">
            {run.id}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <RunStatusBadge run={run} />
          {(run.status === 'running' || run.status === 'awaiting_input') && (
            <Button variant="destructive" size="sm" onClick={handleCancel}>
              Cancel
            </Button>
          )}
        </div>
      </div>

      {actionError && (
        <div className="rounded-md bg-red-500/10 border border-red-500/20 p-3 text-sm text-red-400">
          {actionError}
        </div>
      )}

      {/* DAG */}
      {steps.length > 0 && (
        <div className="rounded-lg border p-4">
          <DAGGraph
            steps={steps}
            stepRuns={stepRuns}
            onSelectStep={setSelectedStepId}
            selectedStepId={selectedStepId ?? undefined}
          />
        </div>
      )}

      {/* HITL Panel */}
      {awaitingStep && (
        <HITLPanel
          stepRun={awaitingStep}
          onApprove={handleApprove}
          onReject={handleReject}
          onSteer={handleSteer}
        />
      )}

      {/* Selected step detail */}
      {selectedStep && (
        <div className="rounded-lg border p-4">
          <StepPanel stepRun={selectedStep} />
        </div>
      )}

      {/* Step list */}
      {!selectedStep && stepRuns.length > 0 && (
        <div className="space-y-2">
          <h2 className="font-semibold">Steps</h2>
          <div className="rounded-lg border divide-y">
            {stepRuns.map((sr: StepRun) => (
              <button
                key={sr.id}
                onClick={() => setSelectedStepId(sr.step_id)}
                className="flex w-full items-center justify-between px-4 py-3 text-left hover:bg-muted/50 transition-colors"
              >
                <span className="text-sm font-medium">{sr.step_title || sr.step_id}</span>
                <Badge variant={sr.status === 'complete' ? 'default' : sr.status === 'failed' ? 'destructive' : 'secondary'}>
                  {sr.status}
                </Badge>
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Run parameters */}
      {run.parameters && Object.keys(run.parameters).length > 0 && (
        <details>
          <summary className="cursor-pointer text-sm text-muted-foreground">Parameters</summary>
          <pre className="mt-2 max-h-48 overflow-auto rounded-md bg-muted p-3 text-xs font-mono">
            {JSON.stringify(run.parameters, null, 2)}
          </pre>
        </details>
      )}
    </div>
  )
}

function RunStatusBadge({ run }: { run: Run }) {
  const variant = run.status === 'complete' ? 'default' as const
    : run.status === 'failed' ? 'destructive' as const
    : 'secondary' as const
  return <Badge variant={variant}>{run.status}</Badge>
}
