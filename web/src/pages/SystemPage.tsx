import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'
import { ExternalLink, CheckCircle2, XCircle, Loader2 } from 'lucide-react'

function StatCard({ label, value, sub }: { label: string; value: string | number; sub?: string }) {
  return (
    <div className="rounded-lg border bg-card px-5 py-4">
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      <p className="text-2xl font-semibold mt-1">{value}</p>
      {sub && <p className="text-xs text-muted-foreground mt-0.5">{sub}</p>}
    </div>
  )
}

export function SystemPage() {
  const { data: health, isLoading: healthLoading, error: healthError } = useQuery({
    queryKey: ['health'],
    queryFn: () => api.getHealth(),
    refetchInterval: 15_000,
    retry: 1,
  })

  const { data: config } = useQuery({
    queryKey: ['config'],
    queryFn: () => api.getConfig(),
    staleTime: Infinity,
  })

  const { data: running } = useQuery({
    queryKey: ['tasks', 'running'],
    queryFn: () => api.listTasks('running'),
    refetchInterval: 10_000,
  })

  const { data: failed } = useQuery({
    queryKey: ['tasks', 'failed'],
    queryFn: () => api.listTasks('failed'),
    refetchInterval: 30_000,
  })

  const { data: completed } = useQuery({
    queryKey: ['tasks', 'completed'],
    queryFn: () => api.listTasks('completed'),
    refetchInterval: 30_000,
  })

  const isHealthy = !healthError && health?.status === 'ok'
  const temporalUrl = config?.temporal_ui_url ?? `${window.location.protocol}//${window.location.hostname}:8233`

  return (
    <div className="max-w-3xl">
      <div className="mb-6">
        <h1 className="text-lg font-semibold">System Health</h1>
        <p className="text-xs text-muted-foreground mt-0.5">Server status and workflow overview</p>
      </div>

      {/* Server status */}
      <div className="rounded-lg border bg-card px-5 py-4 mb-6 flex items-center gap-3">
        {healthLoading ? (
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        ) : isHealthy ? (
          <CheckCircle2 className="h-4 w-4 text-green-500" />
        ) : (
          <XCircle className="h-4 w-4 text-destructive" />
        )}
        <div>
          <p className="text-sm font-medium">
            {healthLoading ? 'Checking...' : isHealthy ? 'Server online' : 'Server unreachable'}
          </p>
          <p className="text-xs text-muted-foreground">
            {healthLoading ? '' : isHealthy ? 'API responding normally' : 'Cannot reach the Fleetlift server'}
          </p>
        </div>
      </div>

      {/* Workflow stats */}
      <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-3">
        Workflow Counts
      </h2>
      <div className="grid grid-cols-3 gap-3 mb-6">
        <StatCard
          label="Running"
          value={running?.tasks?.length ?? '—'}
          sub="active workflows"
        />
        <StatCard
          label="Completed"
          value={completed?.tasks?.length ?? '—'}
          sub="(recent)"
        />
        <StatCard
          label="Failed"
          value={failed?.tasks?.length ?? '—'}
          sub="(recent)"
        />
      </div>

      {/* Temporal UI link */}
      <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-3">
        External Tools
      </h2>
      <a
        href={temporalUrl}
        target="_blank"
        rel="noopener noreferrer"
        className="flex items-center justify-between rounded-lg border bg-card px-5 py-4 hover:bg-muted/30 transition-colors group"
      >
        <div>
          <p className="text-sm font-medium group-hover:text-blue-600 transition-colors">Temporal UI</p>
          <p className="text-xs text-muted-foreground mt-0.5">{temporalUrl}</p>
        </div>
        <ExternalLink className="h-4 w-4 text-muted-foreground group-hover:text-blue-600 transition-colors" />
      </a>
    </div>
  )
}
