import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/api/client'
import { Badge } from '@/components/ui/badge'

export function WorkflowListPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['workflows'],
    queryFn: () => api.listWorkflows(),
  })

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Workflow Library</h1>

      {isLoading && <p className="text-muted-foreground text-sm">Loading...</p>}

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {data?.items?.map((wf) => (
          <Link
            key={wf.id}
            to={`/workflows/${wf.slug}`}
            className="rounded-lg border p-4 hover:border-foreground/30 transition-colors space-y-2"
          >
            <div className="flex items-center justify-between">
              <h3 className="font-semibold">{wf.title}</h3>
              {wf.builtin && <Badge variant="secondary">builtin</Badge>}
            </div>
            <p className="text-sm text-muted-foreground line-clamp-2">{wf.description}</p>
            <div className="flex flex-wrap gap-1">
              {wf.tags?.map((tag) => (
                <Badge key={tag} variant="outline" className="text-xs">{tag}</Badge>
              ))}
            </div>
          </Link>
        ))}
      </div>
    </div>
  )
}
