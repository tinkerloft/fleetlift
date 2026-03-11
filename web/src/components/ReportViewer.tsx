import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'

interface ReportViewerProps {
  runId: string
}

export function ReportViewer({ runId }: ReportViewerProps) {
  const { data: run } = useQuery({
    queryKey: ['report', runId],
    queryFn: () => api.getReport(runId),
  })

  const { data: artifactsData } = useQuery({
    queryKey: ['report-artifacts', runId],
    queryFn: () => api.getReportArtifacts(runId),
  })

  if (!run) {
    return <div className="text-muted-foreground text-sm">Loading report...</div>
  }

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold">{run.workflow_title}</h2>
        <p className="text-sm text-muted-foreground">
          Run {run.id} &mdash; {run.status}
        </p>
      </div>

      {run.steps && run.steps.length > 0 && (
        <div className="space-y-4">
          {run.steps.map((step) => (
            <div key={step.id} className="rounded-lg border p-4 space-y-2">
              <div className="flex items-center justify-between">
                <h3 className="font-medium">{step.step_title || step.step_id}</h3>
                <span className="text-xs text-muted-foreground">{step.status}</span>
              </div>

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
                <a href={step.pr_url} target="_blank" rel="noopener noreferrer"
                   className="text-sm text-blue-400 hover:underline">
                  View PR
                </a>
              )}
            </div>
          ))}
        </div>
      )}

      {artifactsData?.items && artifactsData.items.length > 0 && (
        <div className="space-y-2">
          <h3 className="font-medium">Artifacts</h3>
          <div className="space-y-1">
            {artifactsData.items.map((artifact) => (
              <div key={artifact.id} className="flex items-center justify-between rounded border px-3 py-2 text-sm">
                <span>{artifact.name}</span>
                <span className="text-xs text-muted-foreground">{artifact.size_bytes} bytes</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
