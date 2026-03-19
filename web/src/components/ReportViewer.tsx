import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChevronRight, ChevronDown } from 'lucide-react'
import { api } from '@/api/client'
import { StatusBadge } from './StatusBadge'
import { ArtifactCard } from './ArtifactCard'
import type { Artifact, StepRun } from '@/api/types'

interface ReportViewerProps {
  runId: string
}

function formatDuration(startedAt?: string, completedAt?: string): string | null {
  if (!startedAt) return null
  const start = new Date(startedAt).getTime()
  const end = completedAt ? new Date(completedAt).getTime() : Date.now()
  const secs = Math.round((end - start) / 1000)
  if (secs < 60) return `${secs}s`
  const mins = Math.floor(secs / 60)
  const rem = secs % 60
  return rem > 0 ? `${mins}m ${rem}s` : `${mins}m`
}

function formatCost(usd?: number): string | null {
  if (usd == null) return null
  if (usd < 0.01) return `$${usd.toFixed(4)}`
  return `$${usd.toFixed(2)}`
}

function pickAutoExpand(artifacts: Artifact[]): string | null {
  if (artifacts.length === 0) return null
  const priority = artifacts.find(a =>
    /fleet[-_]?summary|report|summary/i.test(a.name)
  )
  if (priority) return priority.id
  // Fall back to largest artifact
  return artifacts.reduce((best, a) => (a.size_bytes > best.size_bytes ? a : best), artifacts[0]).id
}

function StepRow({ step }: { step: StepRun }) {
  const [expanded, setExpanded] = useState(false)
  const hasDetail = !!(
    (step.output && Object.keys(step.output).length > 0) ||
    step.diff ||
    step.pr_url
  )

  return (
    <div className="rounded-md border bg-card">
      <button
        className="flex w-full items-center gap-3 px-3 py-2 text-left hover:bg-muted/50 transition-colors"
        onClick={() => hasDetail && setExpanded((v) => !v)}
        aria-expanded={hasDetail ? expanded : undefined}
      >
        <span className="shrink-0 text-muted-foreground">
          {hasDetail
            ? (expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />)
            : <span className="inline-block h-3.5 w-3.5" />
          }
        </span>
        <span className="flex-1 truncate text-sm font-medium">
          {step.step_title || step.step_id}
        </span>
        <StatusBadge status={step.status} />
        {step.cost_usd != null && (
          <span className="shrink-0 text-xs text-muted-foreground font-mono">
            {formatCost(step.cost_usd)}
          </span>
        )}
      </button>

      {expanded && hasDetail && (
        <div className="border-t px-4 py-3 space-y-3">
          {step.output && Object.keys(step.output).length > 0 && (
            <pre className="max-h-48 overflow-auto rounded bg-muted p-3 text-xs font-mono">
              {JSON.stringify(step.output, null, 2)}
            </pre>
          )}
          {step.diff && (
            <details>
              <summary className="cursor-pointer text-sm text-blue-400">View diff</summary>
              <pre className="mt-2 max-h-48 overflow-auto rounded bg-muted p-3 text-xs font-mono">
                {step.diff}
              </pre>
            </details>
          )}
          {step.pr_url && (
            <a
              href={step.pr_url}
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm text-blue-400 hover:underline"
            >
              View PR
            </a>
          )}
        </div>
      )}
    </div>
  )
}

export function ReportViewer({ runId }: ReportViewerProps) {
  const [stepsOpen, setStepsOpen] = useState(false)

  const { data: run, error: runError } = useQuery({
    queryKey: ['report', runId],
    queryFn: () => api.getReport(runId),
  })

  const { data: artifactsData, error: artifactsError } = useQuery({
    queryKey: ['report-artifacts', runId],
    queryFn: () => api.getReportArtifacts(runId),
  })

  const artifacts = artifactsData?.items ?? []
  const autoExpandId = useMemo(() => pickAutoExpand(artifacts), [artifacts])

  // If no artifacts, show steps expanded by default on first render
  const hasArtifacts = artifacts.length > 0
  const effectiveStepsOpen = !hasArtifacts || stepsOpen

  if (runError && !run) {
    return <div className="text-destructive text-sm">Failed to load report: {(runError as Error).message}</div>
  }

  if (!run) {
    return <div className="text-muted-foreground text-sm">Loading report...</div>
  }

  const duration = formatDuration(run.started_at, run.completed_at)
  const cost = formatCost(run.total_cost_usd)
  const steps = run.steps ?? []

  return (
    <div className="space-y-6">
      {/* Run header */}
      <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
        <div className="space-y-1">
          <h2 className="text-xl font-semibold leading-tight">{run.workflow_title}</h2>
          <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            <span className="font-mono text-xs">{run.id}</span>
            {duration && <span>{duration}</span>}
            {cost && <span className="font-mono">{cost}</span>}
          </div>
        </div>
        <StatusBadge status={run.status} className="self-start" />
      </div>

      {/* Artifacts section */}
      {hasArtifacts && (
        <section className="space-y-3">
          <div className="flex items-center gap-2">
            <div className="h-px flex-1 bg-border" />
            <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground px-2">
              Artifacts
            </span>
            <div className="h-px flex-1 bg-border" />
          </div>
          {artifactsError && (
            <div className="text-xs text-destructive">Failed to load artifacts</div>
          )}
          <div className="space-y-2">
            {artifacts.map((artifact) => (
              <ArtifactCard
                key={artifact.id}
                artifact={artifact}
                defaultExpanded={artifact.id === autoExpandId}
              />
            ))}
          </div>
        </section>
      )}

      {/* Steps section */}
      {steps.length > 0 && (
        <section className="space-y-3">
          <div className="flex items-center gap-2">
            <div className="h-px flex-1 bg-border" />
            {hasArtifacts && (
              <button
                className="flex items-center gap-1.5 px-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground hover:text-foreground transition-colors"
                onClick={() => setStepsOpen((v) => !v)}
                aria-expanded={effectiveStepsOpen}
              >
                {effectiveStepsOpen
                  ? <ChevronDown className="h-3 w-3" />
                  : <ChevronRight className="h-3 w-3" />
                }
                Steps
              </button>
            )}
            {!hasArtifacts && (
              <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground px-2">
                Steps
              </span>
            )}
            <div className="h-px flex-1 bg-border" />
          </div>

          {effectiveStepsOpen && (
            <div className="space-y-2">
              {steps.map((step) => (
                <StepRow key={step.id} step={step} />
              ))}
            </div>
          )}
        </section>
      )}
    </div>
  )
}
