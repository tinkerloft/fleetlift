import type { StepRun } from '@/api/types'
import { LogStream } from './LogStream'
import { DiffViewer } from './DiffViewer'
import { JsonViewer } from './JsonViewer'
import { StatusBadge } from './StatusBadge'

interface StepPanelProps {
  stepRun: StepRun
  runParameters?: Record<string, unknown>
  allStepRuns?: StepRun[]
}

export function StepPanel({ stepRun, runParameters, allStepRuns }: StepPanelProps) {
  // Collect inputs: run parameters + outputs from prior completed steps.
  const priorOutputs = (allStepRuns ?? [])
    .filter(sr => sr.step_id !== stepRun.step_id && sr.output && Object.keys(sr.output).length > 0)
    .filter(sr => sr.status === 'complete' || sr.status === 'failed')

  const hasRunParams = runParameters && Object.keys(runParameters).length > 0
  const hasInputs = hasRunParams || priorOutputs.length > 0

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold">{stepRun.step_title || stepRun.step_id}</h3>
        <StatusBadge status={stepRun.status} />
      </div>

      {stepRun.error_message && (
        <div className="rounded-md bg-red-500/10 border border-red-500/20 p-3 text-sm text-red-400">
          {stepRun.error_message}
        </div>
      )}

      {stepRun.pr_url && (
        <div className="text-sm">
          <span className="text-muted-foreground">PR: </span>
          <a href={stepRun.pr_url} target="_blank" rel="noopener noreferrer" className="text-blue-400 hover:underline">
            {stepRun.pr_url}
          </a>
        </div>
      )}

      {hasInputs && (
        <details>
          <summary className="cursor-pointer text-sm font-medium text-muted-foreground">Inputs</summary>
          <div className="mt-2 space-y-2">
            {hasRunParams && (
              <div>
                <span className="text-xs text-muted-foreground">Run parameters</span>
                <pre className="mt-1 max-h-32 overflow-auto rounded-md bg-muted p-2 text-xs font-mono">
                  {JSON.stringify(runParameters, null, 2)}
                </pre>
              </div>
            )}
            {priorOutputs.map(sr => (
              <div key={sr.id}>
                <span className="text-xs text-muted-foreground">{sr.step_title || sr.step_id} output</span>
                <pre className="mt-1 max-h-32 overflow-auto rounded-md bg-muted p-2 text-xs font-mono">
                  {JSON.stringify(sr.output, null, 2)}
                </pre>
              </div>
            ))}
          </div>
        </details>
      )}

      {stepRun.diff && (
        <div className="space-y-2">
          <h4 className="text-sm font-medium text-muted-foreground">Diff</h4>
          <DiffViewer diff={stepRun.diff} />
        </div>
      )}

      {stepRun.output && Object.keys(stepRun.output).length > 0 && (
        <div className="space-y-2">
          <h4 className="text-sm font-medium text-muted-foreground">Output</h4>
          <JsonViewer data={stepRun.output} />
        </div>
      )}

      <div className="space-y-2">
        <h4 className="text-sm font-medium text-muted-foreground">Logs</h4>
        <LogStream stepRunId={stepRun.id} />
      </div>
    </div>
  )
}

