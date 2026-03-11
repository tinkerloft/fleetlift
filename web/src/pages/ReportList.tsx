import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/api/client'
import { Badge } from '@/components/ui/badge'

export function ReportListPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['reports'],
    queryFn: () => api.listReports(),
  })

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Reports</h1>

      {isLoading && <p className="text-muted-foreground text-sm">Loading...</p>}

      {data?.items?.length === 0 && (
        <p className="text-muted-foreground text-sm">No completed runs with reports</p>
      )}

      <div className="rounded-lg border divide-y">
        {data?.items?.map((run) => (
          <Link
            key={run.id}
            to={`/reports/${run.id}`}
            className="flex items-center justify-between px-4 py-3 hover:bg-muted/50 transition-colors"
          >
            <div>
              <span className="font-medium">{run.workflow_title}</span>
              <p className="text-xs text-muted-foreground">
                {run.completed_at ? new Date(run.completed_at).toLocaleString() : run.id.slice(0, 8)}
              </p>
            </div>
            <Badge variant={run.status === 'complete' ? 'default' : 'destructive'}>
              {run.status}
            </Badge>
          </Link>
        ))}
      </div>
    </div>
  )
}
