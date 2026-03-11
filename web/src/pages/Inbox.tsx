import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/api/client'
import { Badge } from '@/components/ui/badge'

export function InboxPage() {
  const queryClient = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ['inbox'],
    queryFn: () => api.listInbox(),
    refetchInterval: 5000,
  })

  const markReadMutation = useMutation({
    mutationFn: (id: string) => api.markInboxRead(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['inbox'] }),
  })

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Inbox</h1>

      {isLoading && <p className="text-muted-foreground text-sm">Loading...</p>}

      {data?.items?.length === 0 && (
        <p className="text-muted-foreground text-sm">No inbox items</p>
      )}

      <div className="space-y-2">
        {data?.items?.map((item) => (
          <div
            key={item.id}
            className="flex items-center justify-between rounded-lg border p-4 hover:bg-muted/50 transition-colors"
          >
            <div className="space-y-1">
              <div className="flex items-center gap-2">
                <Badge variant={item.kind === 'awaiting_input' ? 'default' : 'secondary'}>
                  {item.kind === 'awaiting_input' ? 'Action Required' : 'Output Ready'}
                </Badge>
                <Link
                  to={`/runs/${item.run_id}`}
                  className="font-medium hover:underline"
                >
                  {item.title}
                </Link>
              </div>
              {item.summary && (
                <p className="text-sm text-muted-foreground">{item.summary}</p>
              )}
              <p className="text-xs text-muted-foreground">
                {new Date(item.created_at).toLocaleString()}
              </p>
            </div>
            <div className="flex items-center gap-2">
              <Link to={`/runs/${item.run_id}`}>
                <button className="rounded-md border px-3 py-1.5 text-xs hover:bg-muted transition-colors">
                  View
                </button>
              </Link>
              {!item.read && (
                <button
                  onClick={() => markReadMutation.mutate(item.id)}
                  className="rounded-md border px-3 py-1.5 text-xs hover:bg-muted transition-colors"
                >
                  Mark Read
                </button>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
