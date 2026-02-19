import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'

export function VerifierLogs({ workflowId }: { workflowId: string }) {
  const { data, isLoading } = useQuery({
    queryKey: ['logs', workflowId],
    queryFn: () => api.getLogs(workflowId),
    refetchInterval: 15_000,
  })

  if (isLoading) return <p className="text-sm text-muted-foreground">Loading...</p>
  if (!data?.logs?.length) return <p className="text-sm text-muted-foreground">No verifier output yet.</p>

  return (
    <div className="space-y-4 max-w-3xl">
      {data.logs.map((log, i) => (
        <div key={i} className="border rounded-lg overflow-hidden">
          <div className={`flex items-center gap-2 px-4 py-2 text-sm font-medium ${log.success ? 'bg-green-50' : 'bg-red-50'}`}>
            <span className={log.success ? 'text-green-700' : 'text-red-700'}>
              {log.success ? '✓' : '✗'} {log.verifier}
            </span>
            <span className="text-xs text-muted-foreground ml-auto">exit {log.exit_code}</span>
          </div>
          {(log.stdout || log.stderr) && (
            <pre className="text-xs p-4 bg-muted/30 overflow-auto max-h-64 font-mono whitespace-pre-wrap">
              {log.stdout}{log.stderr}
            </pre>
          )}
        </div>
      ))}
    </div>
  )
}
