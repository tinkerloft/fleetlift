import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'
import { ExternalLink } from 'lucide-react'

export function TemporalEmbed({ workflowId }: { workflowId: string }) {
  const { data: config } = useQuery({
    queryKey: ['config'],
    queryFn: () => api.getConfig(),
    staleTime: 60_000,
    retry: false,
  })

  const baseUrl = config?.temporal_ui_url
  if (!baseUrl) {
    // Fallback: try default Temporal UI URL
    const fallbackUrl = `${window.location.protocol}//${window.location.hostname}:8233`
    return <TemporalFrame url={fallbackUrl} workflowId={workflowId} />
  }

  return <TemporalFrame url={baseUrl} workflowId={workflowId} />
}

function TemporalFrame({ url, workflowId }: { url: string; workflowId: string }) {
  const fullUrl = `${url}/namespaces/default/workflows/${workflowId}`

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">
          Temporal workflow details for this task
        </p>
        <a
          href={fullUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1.5 text-xs text-blue-600 hover:underline"
        >
          Open in Temporal UI
          <ExternalLink className="h-3 w-3" />
        </a>
      </div>
      <div className="rounded-lg border overflow-hidden bg-white" style={{ height: 'calc(100vh - 280px)', minHeight: '500px' }}>
        <iframe
          src={fullUrl}
          title="Temporal UI"
          className="w-full h-full border-0"
          sandbox="allow-same-origin allow-scripts allow-popups allow-forms"
        />
      </div>
    </div>
  )
}
