import { useState, useEffect } from 'react'
import type { StepRun, Artifact } from '@/api/types'
import { LogStream } from './LogStream'
import { DiffViewer } from './DiffViewer'
import { JsonViewer } from './JsonViewer'
import { StatusBadge } from './StatusBadge'
import { ArtifactCard } from './ArtifactCard'

interface StepPanelProps {
  stepRun: StepRun
  runParameters?: Record<string, unknown>
  allStepRuns?: StepRun[]
  artifacts?: Artifact[]
}

/** Strip the fan-out index suffix (e.g. "assess-2" → "assess"). */
function baseStepId(stepId: string): string {
  return stepId.replace(/-\d+$/, '')
}

export function StepPanel({ stepRun, runParameters, allStepRuns, artifacts = [] }: StepPanelProps) {
  const [outputExpanded, setOutputExpanded] = useState(artifacts.length === 0)

  // Reset output collapse state when the selected step or its artifacts change
  useEffect(() => {
    setOutputExpanded(artifacts.length === 0)
  }, [stepRun.id, artifacts.length])
  const myBase = baseStepId(stepRun.step_id)

  // Prior completed steps whose output is available as template context.
  // Exclude fan-out siblings (same base step ID) — they're peers, not inputs.
  const priorOutputs = (allStepRuns ?? [])
    .filter(sr =>
      baseStepId(sr.step_id) !== myBase &&
      sr.output && Object.keys(sr.output).length > 0 &&
      (sr.status === 'complete' || sr.status === 'failed'),
    )

  const hasStepInput = stepRun.input && Object.keys(stepRun.input).length > 0
  const hasRunParams = runParameters && Object.keys(runParameters).length > 0

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold">{stepRun.step_title || stepRun.step_id}</h3>
        <StatusBadge status={stepRun.status} />
      </div>

      {stepRun.error_message && (
        <div className="rounded border border-red-500/20 bg-red-500/8 p-2.5 text-xs text-red-400 font-mono">
          {stepRun.error_message}
        </div>
      )}

      {stepRun.pr_url && (
        <div className="text-xs">
          <span className="text-muted-foreground">PR: </span>
          <a href={stepRun.pr_url} target="_blank" rel="noopener noreferrer"
             className="text-accent hover:underline font-mono">
            {stepRun.pr_url}
          </a>
        </div>
      )}

      {artifacts.length > 0 && (
        <div className="space-y-2">
          <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">Artifacts</h4>
          <div className="space-y-2">
            {artifacts.map(a => (
              <ArtifactCard key={a.id} artifact={a} />
            ))}
          </div>
        </div>
      )}

      {/* Step inputs — specific to this step instance */}
      {hasStepInput && (
        <div className="space-y-1.5">
          <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">Step inputs</h4>
          <div className="rounded border border-border bg-muted/40 divide-y divide-border">
            {Object.entries(stepRun.input!).map(([k, v]) => (
              <div key={k} className="flex items-baseline gap-3 px-2.5 py-1.5">
                <span className="text-[10px] font-mono text-muted-foreground shrink-0 w-16">{k}</span>
                <span className="text-[10px] font-mono text-accent break-all">{String(v)}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Prior step outputs used as template context */}
      {priorOutputs.length > 0 && (
        <details>
          <summary className="cursor-pointer text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
            Context from prior steps
          </summary>
          <div className="mt-2 space-y-2">
            {priorOutputs.map(sr => (
              <div key={sr.id}>
                <span className="text-[10px] text-muted-foreground font-mono">{sr.step_title || sr.step_id}</span>
                <pre className="mt-1 max-h-28 overflow-auto rounded border border-border bg-muted p-2 text-[10px] font-mono">
                  {JSON.stringify(sr.output, null, 2)}
                </pre>
              </div>
            ))}
          </div>
        </details>
      )}

      {/* Workflow parameters (collapsed by default — for reference) */}
      {hasRunParams && (
        <details>
          <summary className="cursor-pointer text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
            Workflow parameters
          </summary>
          <pre className="mt-2 max-h-28 overflow-auto rounded border border-border bg-muted p-2 text-[10px] font-mono">
            {JSON.stringify(runParameters, null, 2)}
          </pre>
        </details>
      )}

      {stepRun.diff && (
        <div className="space-y-1.5">
          <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">Diff</h4>
          <DiffViewer diff={stepRun.diff} />
        </div>
      )}

      {stepRun.output && Object.keys(stepRun.output).length > 0 && (
        <div className="space-y-1.5">
          <button
            onClick={() => setOutputExpanded(v => !v)}
            className="flex items-center gap-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground hover:text-foreground transition-colors"
          >
            <span>Output</span>
            <span className="text-[9px]">{outputExpanded ? '▲' : '▼'}</span>
          </button>
          {outputExpanded && <JsonViewer data={stepRun.output} />}
        </div>
      )}

      <div className="space-y-1.5">
        <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">Logs</h4>
        <LogStream stepRunId={stepRun.id} />
      </div>
    </div>
  )
}
