import { useState, useEffect, useCallback } from 'react'
import { useParams, Link } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, subscribeToRun, getConfig } from '@/api/client'
import { DAGGraph } from '@/components/DAGGraph'
import { StepPanel } from '@/components/StepPanel'
import { StepTimeline } from '@/components/StepTimeline'
import { HITLPanel } from '@/components/HITLPanel'
import { StatusBadge } from '@/components/StatusBadge'
import { Skeleton } from '@/components/Skeleton'
import { Button } from '@/components/ui/button'
import { useLiveDuration } from '@/lib/use-live-duration'
import { formatCost } from '@/lib/format'
import { cn } from '@/lib/utils'
import { parse as parseYaml } from '@/lib/yaml'
import { ArtifactCard } from '@/components/ArtifactCard'
import type { StepDef, StepRun, WorkflowDef, Artifact } from '@/api/types'

const SEG_COLOR: Record<string, string> = {
  complete: 'bg-green-500',
  running: 'bg-blue-500 animate-pulse',
  cloning: 'bg-blue-500 animate-pulse',
  verifying: 'bg-violet-500 animate-pulse',
  awaiting_input: 'bg-amber-500',
  failed: 'bg-red-500',
  pending: 'bg-transparent',
  skipped: 'bg-transparent',
}

export function RunDetailPage() {
  const { id } = useParams<{ id: string }>()
  const queryClient = useQueryClient()
  const [selectedStepId, setSelectedStepId] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [cancelling, setCancelling] = useState(false)
  const [sseConnected, setSseConnected] = useState(false)
  const [resolvingFanOut, setResolvingFanOut] = useState(false)

  const { data: config } = useQuery({ queryKey: ['config'], queryFn: getConfig, staleTime: Infinity })

  const { data: inboxData } = useQuery({
    queryKey: ['inbox'],
    queryFn: () => api.listInbox(),
    refetchInterval: sseConnected ? false : 5000,
    enabled: !!id,
  })
  const fanOutFailureItem = inboxData?.items?.find(
    i => i.run_id === id && i.kind === 'fan_out_partial_failure' && !i.answer,
  )

  const { data: run } = useQuery({
    queryKey: ['run', id],
    queryFn: () => api.getRun(id!),
    enabled: !!id,
    refetchInterval: sseConnected ? false : 5000,
  })

  const { data: artifactsData } = useQuery({
    queryKey: ['run-artifacts', id],
    queryFn: () => api.getReportArtifacts(id!),
    enabled: !!id,
    refetchInterval: sseConnected ? false : 10000,
  })
  const allArtifacts: Artifact[] = artifactsData?.items ?? []

  const duration = useLiveDuration(run?.started_at, run?.completed_at)

  useEffect(() => {
    if (!id) return
    setSseConnected(false)
    const cleanup = subscribeToRun(
      id,
      () => {
        queryClient.invalidateQueries({ queryKey: ['run', id] })
        queryClient.invalidateQueries({ queryKey: ['run-artifacts', id] })
      },
      () => {},
      () => setSseConnected(false),
    )
    setSseConnected(true)
    return () => cleanup?.()
  }, [id, queryClient])

  const handleApprove = useCallback(async () => {
    if (!id) return
    setActionError(null)
    try { await api.approveRun(id); queryClient.invalidateQueries({ queryKey: ['run', id] }) }
    catch (err) { setActionError(err instanceof Error ? err.message : 'Action failed') }
  }, [id, queryClient])

  const handleReject = useCallback(async () => {
    if (!id) return
    setActionError(null)
    try { await api.rejectRun(id); queryClient.invalidateQueries({ queryKey: ['run', id] }) }
    catch (err) { setActionError(err instanceof Error ? err.message : 'Action failed') }
  }, [id, queryClient])

  const handleSteer = useCallback(async (prompt: string) => {
    if (!id) return
    setActionError(null)
    try { await api.steerRun(id, prompt); queryClient.invalidateQueries({ queryKey: ['run', id] }) }
    catch (err) { setActionError(err instanceof Error ? err.message : 'Action failed') }
  }, [id, queryClient])

  const handleResolveFanOut = useCallback(async (action: 'proceed' | 'terminate') => {
    if (!id || !fanOutFailureItem) return
    setActionError(null)
    setResolvingFanOut(true)
    try {
      await api.resolveFanOut(id, action, fanOutFailureItem.step_id ?? '')
      queryClient.invalidateQueries({ queryKey: ['run', id] })
      queryClient.invalidateQueries({ queryKey: ['inbox'] })
    } catch (err) { setActionError(err instanceof Error ? err.message : 'Action failed') }
    finally { setResolvingFanOut(false) }
  }, [id, queryClient])

  const handleCancel = useCallback(async () => {
    if (!id) return
    setActionError(null)
    setCancelling(true)
    try { await api.cancelRun(id); queryClient.invalidateQueries({ queryKey: ['run', id] }) }
    catch (err) { setActionError(err instanceof Error ? err.message : 'Action failed'); setCancelling(false) }
  }, [id, queryClient])

  if (!run) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-4 w-96" />
        <Skeleton className="h-64 w-full rounded-lg" />
      </div>
    )
  }

  // Parse the workflow YAML to get the full step list with depends_on edges.
  // We use raw YAML (not a serialized struct) to avoid Temporal history coupling.
  // Fall back to step runs only if yaml is unavailable.
  let steps: StepDef[] = []
  if (run.workflow_yaml) {
    try {
      const def = parseYaml(run.workflow_yaml) as unknown as WorkflowDef
      if (def?.steps) steps = def.steps
    } catch { /* ignore parse errors */ }
  }
  if (steps.length === 0) {
    steps = run.steps?.map((sr: StepRun) => ({ id: sr.step_id, title: sr.step_title })) ?? []
  }

  const stepRuns = run.steps ?? []
  const selectedStep = stepRuns.find((sr: StepRun) => sr.step_id === selectedStepId)
  const awaitingStep = stepRuns.find((sr: StepRun) => sr.status === 'awaiting_input')
  const completedCount = stepRuns.filter(s => s.status === 'complete').length

  // Derive primary artifact for hero panel
  const isRunComplete = run.status === 'complete'
  const heroPrimaryArtifact: Artifact | null = (() => {
    if (!isRunComplete || allArtifacts.length === 0) return null
    const PRIORITY_NAMES = ['fleet-summary', 'fleet_summary', 'report', 'summary']
    const byName = allArtifacts.find(a =>
      PRIORITY_NAMES.some(kw => a.name.toLowerCase().includes(kw))
    )
    if (byName) return byName
    // Fallback: artifact on the last completed step (latest completed_at)
    const completedStepRuns = stepRuns
      .filter(sr => sr.status === 'complete' && sr.completed_at)
      .sort((a, b) => (b.completed_at! > a.completed_at! ? 1 : -1))
    for (const sr of completedStepRuns) {
      const a = allArtifacts.find(art => art.step_run_id === sr.id)
      if (a) return a
    }
    // Final fallback: largest artifact by size
    return allArtifacts.reduce((max, a) => (a.size_bytes > max.size_bytes ? a : max), allArtifacts[0])
  })()

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">{run.workflow_title}</h1>
          <p className="text-xs text-muted-foreground font-mono mt-0.5">{run.id}</p>
        </div>
        <div className="flex items-center gap-3">
          {run.temporal_id && config?.temporal_ui_url && (
            <a
              href={`${config.temporal_ui_url}/namespaces/default/workflows/${run.temporal_id}`}
              target="_blank" rel="noopener noreferrer"
              className="text-xs text-muted-foreground underline hover:text-foreground"
            >Temporal ↗</a>
          )}
          {duration && <span className="text-sm text-muted-foreground tabular-nums">{duration}</span>}
          {run.total_cost_usd != null && run.total_cost_usd > 0 && (
            <span className="text-sm text-muted-foreground">
              Cost: {formatCost(run.total_cost_usd)}
            </span>
          )}
          <StatusBadge status={run.status} />
          {(run.status === 'running' || run.status === 'awaiting_input') && (
            <Button variant="destructive" size="sm" onClick={handleCancel} disabled={cancelling}>
              {cancelling ? 'Cancelling…' : 'Cancel'}
            </Button>
          )}
        </div>
      </div>

      {/* Progress bar */}
      {stepRuns.length > 0 && (
        <div className="flex items-center gap-3">
          <span className="text-sm text-muted-foreground whitespace-nowrap">
            {completedCount} of {stepRuns.length} steps
          </span>
          <div className="flex flex-1 h-1.5 rounded-full bg-muted gap-0.5 overflow-hidden">
            {stepRuns.map(sr => (
              <div key={sr.id} className={cn('flex-1 rounded-full', SEG_COLOR[sr.status] ?? 'bg-transparent')} />
            ))}
          </div>
        </div>
      )}

      {actionError && (
        <div className="rounded-md bg-red-500/10 border border-red-500/20 p-3 text-sm text-red-400">
          {actionError}
        </div>
      )}

      {run.status === 'failed' && run.error_message && (
        <div className="rounded-md bg-red-500/10 border border-red-500/20 p-3 text-sm text-red-400">
          <span className="font-semibold">Run failed: </span>{run.error_message}
        </div>
      )}

      {fanOutFailureItem && (
        <div className="rounded-md border border-amber-500/30 bg-amber-500/8 p-3">
          <div className="flex items-start justify-between gap-4">
            <div className="min-w-0">
              <p className="text-sm font-semibold text-amber-400">{fanOutFailureItem.title}</p>
              {fanOutFailureItem.summary && (
                <pre className="mt-1 text-xs text-muted-foreground font-mono whitespace-pre-wrap line-clamp-3">
                  {fanOutFailureItem.summary}
                </pre>
              )}
              <Link to="/inbox" className="mt-1 text-xs text-accent hover:underline block">
                View in inbox ↗
              </Link>
            </div>
            <div className="flex gap-2 shrink-0">
              <Button
                size="sm" variant="default"
                className="h-7 text-xs bg-green-600 hover:bg-green-700"
                disabled={resolvingFanOut}
                onClick={() => handleResolveFanOut('proceed')}
              >
                Proceed with results
              </Button>
              <Button
                size="sm" variant="destructive"
                className="h-7 text-xs"
                disabled={resolvingFanOut}
                onClick={() => handleResolveFanOut('terminate')}
              >
                Terminate
              </Button>
            </div>
          </div>
        </div>
      )}

      {/* Hero panel — primary artifact for completed runs */}
      {heroPrimaryArtifact && (
        <div className="rounded-lg border bg-card p-4 space-y-3">
          <h2 className="text-base font-semibold">
            {run.workflow_title || run.workflow_id} Result
          </h2>
          <ArtifactCard artifact={heroPrimaryArtifact} defaultExpanded={true} />
        </div>
      )}

      {/* DAG */}
      {steps.length > 0 && (
        <div className="rounded-lg border bg-card p-4">
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
        <HITLPanel stepRun={awaitingStep} onApprove={handleApprove} onReject={handleReject} onSteer={handleSteer} />
      )}

      {/* Two-column: Timeline + Panel */}
      {stepRuns.length > 0 && (
        <div className="grid grid-cols-1 md:grid-cols-[1fr_360px] gap-6 items-start">
          {/* Step detail or placeholder */}
          <div>
            {selectedStep ? (
              <div className="rounded-lg border bg-card p-4">
                <StepPanel
                  stepRun={selectedStep}
                  runParameters={run.parameters}
                  artifacts={allArtifacts.filter(a => a.step_run_id === selectedStep.id)}
                />
              </div>
            ) : (
              <div className="flex items-center justify-center rounded-lg border border-dashed py-16 text-sm text-muted-foreground">
                Select a step from the DAG or timeline to view details
              </div>
            )}
          </div>

          {/* Timeline */}
          <div>
            <h3 className="text-sm font-semibold mb-3">Steps</h3>
            <StepTimeline stepRuns={stepRuns} selectedStepId={selectedStepId} onSelect={setSelectedStepId} />
          </div>
        </div>
      )}

      {/* Pending placeholder */}
      {stepRuns.length === 0 && run.status === 'pending' && (
        <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed py-16 text-muted-foreground">
          <div className="h-5 w-5 animate-spin rounded-full border-2 border-current border-t-transparent" />
          <p className="text-sm">Waiting for workflow to start…</p>
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
