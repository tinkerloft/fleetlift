import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/api/client'
import { Badge } from '@/components/ui/badge'
import type { RunStatus } from '@/api/types'

const STATUS_VARIANT: Record<RunStatus, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  pending: 'outline',
  running: 'secondary',
  awaiting_input: 'default',
  complete: 'default',
  failed: 'destructive',
  cancelled: 'outline',
}

export function RunListPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['runs'],
    queryFn: () => api.listRuns(),
    refetchInterval: 5000,
  })

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Runs</h1>

      {isLoading && <p className="text-muted-foreground text-sm">Loading...</p>}

      <div className="rounded-lg border">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b text-left text-muted-foreground">
              <th className="px-4 py-3 font-medium">Workflow</th>
              <th className="px-4 py-3 font-medium">Status</th>
              <th className="px-4 py-3 font-medium">Started</th>
              <th className="px-4 py-3 font-medium">ID</th>
            </tr>
          </thead>
          <tbody>
            {data?.items?.map((run) => (
              <tr key={run.id} className="border-b last:border-0 hover:bg-muted/50">
                <td className="px-4 py-3">
                  <Link to={`/runs/${run.id}`} className="font-medium hover:underline">
                    {run.workflow_title}
                  </Link>
                </td>
                <td className="px-4 py-3">
                  <Badge variant={STATUS_VARIANT[run.status]}>{run.status}</Badge>
                </td>
                <td className="px-4 py-3 text-muted-foreground">
                  {run.started_at ? new Date(run.started_at).toLocaleString() : '-'}
                </td>
                <td className="px-4 py-3 font-mono text-xs text-muted-foreground">
                  {run.id.slice(0, 8)}
                </td>
              </tr>
            ))}
            {data?.items?.length === 0 && (
              <tr>
                <td colSpan={4} className="px-4 py-8 text-center text-muted-foreground">
                  No runs yet
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
