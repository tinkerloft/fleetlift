import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/api/client'
import type { TaskSummary, InboxType } from '@/api/types'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

const INBOX_CONFIG: Record<InboxType, { label: string; variant: 'default' | 'secondary' | 'destructive' | 'outline' }> = {
  awaiting_approval:  { label: 'Needs Approval', variant: 'default' },
  paused:             { label: 'Paused',          variant: 'destructive' },
  steering_requested: { label: 'Needs Steering',  variant: 'destructive' },
  completed_review:   { label: 'Review',          variant: 'secondary' },
}

function InboxItem({ item }: { item: TaskSummary }) {
  const config = item.inbox_type
    ? (INBOX_CONFIG[item.inbox_type] ?? { label: item.inbox_type, variant: 'outline' as const })
    : { label: '', variant: 'outline' as const }

  return (
    <Link to={`/tasks/${item.workflow_id}`}>
      <Card className="hover:border-foreground/30 transition-colors cursor-pointer">
        <CardContent className="pt-4 pb-4 flex items-center justify-between">
          <div>
            <p className="font-mono text-sm font-medium">{item.workflow_id}</p>
            <p className="text-xs text-muted-foreground mt-1">{item.start_time}</p>
          </div>
          <Badge variant={config.variant}>{config.label}</Badge>
        </CardContent>
      </Card>
    </Link>
  )
}

export function InboxPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['inbox'],
    queryFn: () => api.getInbox(),
    refetchInterval: 5000,
  })

  if (isLoading) return <p className="text-sm text-muted-foreground">Loading...</p>
  if (error)     return <p className="text-sm text-destructive">Error: {String(error)}</p>

  const items = data?.items ?? []

  return (
    <div className="max-w-2xl">
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Inbox</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2">
          {items.length === 0 ? (
            <p className="text-sm text-muted-foreground py-4 text-center">No pending actions</p>
          ) : (
            items.map((item) => <InboxItem key={item.workflow_id} item={item} />)
          )}
        </CardContent>
      </Card>
    </div>
  )
}
