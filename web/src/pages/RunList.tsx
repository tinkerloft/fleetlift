import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/api/client'
import { Run } from '@/api/types'
import { StatusBadge } from '@/components/StatusBadge'
import { SkeletonRow } from '@/components/Skeleton'
import { EmptyState } from '@/components/EmptyState'
import { useLiveDuration } from '@/lib/use-live-duration'
import { Activity } from 'lucide-react'

function RunRow({ run }: { run: Run }) {
  const duration = useLiveDuration(run.started_at, run.completed_at)
  return (
    <tr className="border-b last:border-0 hover:bg-muted/50">
      <td className="px-4 py-3">
        <Link to={`/runs/${run.id}`} className="font-medium hover:underline">
          {run.workflow_title}
        </Link>
      </td>
      <td className="px-4 py-3">
        <StatusBadge status={run.status} />
      </td>
      <td className="px-4 py-3 text-muted-foreground">
        {run.started_at ? new Date(run.started_at).toLocaleString() : '-'}
      </td>
      <td className="px-4 py-3 text-muted-foreground tabular-nums">
        {duration ?? '-'}
      </td>
      <td className="px-4 py-3 font-mono text-xs text-muted-foreground">
        {run.id.slice(0, 8)}
      </td>
    </tr>
  )
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

      {isLoading && (
        <div className="rounded-lg border">
          <SkeletonRow /><SkeletonRow /><SkeletonRow />
        </div>
      )}

      <div className="rounded-lg border">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b text-left text-muted-foreground">
              <th className="px-4 py-3 font-medium">Workflow</th>
              <th className="px-4 py-3 font-medium">Status</th>
              <th className="px-4 py-3 font-medium">Started</th>
              <th className="px-4 py-3 font-medium">Duration</th>
              <th className="px-4 py-3 font-medium">ID</th>
            </tr>
          </thead>
          <tbody>
            {data?.items?.map((run) => (
              <RunRow key={run.id} run={run} />
            ))}
            {data?.items?.length === 0 && (
              <tr>
                <td colSpan={5} className="p-0">
                  <EmptyState icon={Activity} title="No runs yet" description="Start a workflow to see runs here." action={{ label: 'Browse Workflows', href: '/workflows' }} />
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
