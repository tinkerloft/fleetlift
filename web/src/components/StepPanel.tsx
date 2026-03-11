import type { StepRun } from '@/api/types'
import { LogStream } from './LogStream'
import { Badge } from './ui/badge'

interface StepPanelProps {
  stepRun: StepRun
}

export function StepPanel({ stepRun }: StepPanelProps) {
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

      {stepRun.diff && (
        <div className="space-y-2">
          <h4 className="text-sm font-medium text-muted-foreground">Diff</h4>
          <pre className="max-h-64 overflow-auto rounded-md bg-muted p-3 text-xs font-mono">
            {stepRun.diff}
          </pre>
        </div>
      )}

      {stepRun.output && Object.keys(stepRun.output).length > 0 && (
        <div className="space-y-2">
          <h4 className="text-sm font-medium text-muted-foreground">Output</h4>
          <pre className="max-h-48 overflow-auto rounded-md bg-muted p-3 text-xs font-mono">
            {JSON.stringify(stepRun.output, null, 2)}
          </pre>
        </div>
      )}

      <div className="space-y-2">
        <h4 className="text-sm font-medium text-muted-foreground">Logs</h4>
        <LogStream stepRunId={stepRun.id} />
      </div>
    </div>
  )
}

function StatusBadge({ status }: { status: string }) {
  const variant = status === 'complete' ? 'default'
    : status === 'failed' ? 'destructive'
    : 'secondary'
  return <Badge variant={variant}>{status}</Badge>
}
