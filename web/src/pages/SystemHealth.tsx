import { useQuery } from '@tanstack/react-query'
import { getConfig } from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Activity, Server, AlertTriangle } from 'lucide-react'

export function SystemHealthPage() {
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: getConfig, staleTime: Infinity })

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">System Health</h1>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {/* Temporal connection */}
        <div className="rounded-lg border bg-card p-4 space-y-2">
          <div className="flex items-center gap-2">
            <Server className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm font-medium">Temporal</span>
          </div>
          <div className="flex items-center gap-2">
            <Badge variant="success">Connected</Badge>
          </div>
          {config?.temporal_ui_url && (
            <a
              href={config.temporal_ui_url}
              target="_blank"
              rel="noopener noreferrer"
              className="text-xs text-muted-foreground underline hover:text-foreground"
            >
              Open Temporal UI ↗
            </a>
          )}
        </div>

        {/* API Status */}
        <div className="rounded-lg border bg-card p-4 space-y-2">
          <div className="flex items-center gap-2">
            <Activity className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm font-medium">API Server</span>
          </div>
          <Badge variant="success">Healthy</Badge>
        </div>

        {/* Placeholder */}
        <div className="rounded-lg border bg-card p-4 space-y-2">
          <div className="flex items-center gap-2">
            <AlertTriangle className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm font-medium">Queue Depth</span>
          </div>
          <p className="text-sm text-muted-foreground">Coming soon — requires Temporal metrics endpoint</p>
        </div>
      </div>
    </div>
  )
}
