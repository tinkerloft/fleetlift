import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import { Button } from '@/components/ui/button'

export function GroupProgress({ workflowId }: { workflowId: string }) {
  const qc = useQueryClient()
  const { data: progress, isLoading } = useQuery({
    queryKey: ['progress', workflowId],
    queryFn: () => api.getProgress(workflowId),
    refetchInterval: 3000,
  })
  const continueMutation = useMutation({
    mutationFn: (skipRemaining: boolean) => api.continue(workflowId, skipRemaining),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['progress', workflowId] }),
  })

  if (isLoading) return <p className="text-sm text-muted-foreground">Loading...</p>
  if (!progress || progress.total_groups === 0)
    return <p className="text-sm text-muted-foreground">This task doesn't use grouped execution.</p>

  const { total_groups, completed_groups, failed_groups, failure_percent, is_paused, paused_reason, failed_group_names } = progress

  return (
    <div className="max-w-lg space-y-4">
      <div className="flex gap-6 text-sm">
        <span><span className="text-muted-foreground">Total:</span> {total_groups}</span>
        <span><span className="text-muted-foreground">Done:</span> {completed_groups}</span>
        <span className={failed_groups > 0 ? 'text-red-600' : ''}>
          <span className="text-muted-foreground">Failed:</span> {failed_groups} ({failure_percent.toFixed(1)}%)
        </span>
      </div>

      <div className="w-full bg-muted rounded-full h-2">
        <div
          className="bg-foreground h-2 rounded-full transition-all"
          style={{ width: `${(completed_groups / total_groups) * 100}%` }}
        />
      </div>

      {is_paused && (
        <div className="border border-yellow-400 rounded-lg p-4 bg-yellow-50">
          <p className="text-sm font-medium text-yellow-800 mb-1">Execution paused</p>
          {paused_reason && <p className="text-xs text-yellow-700 mb-3">{paused_reason}</p>}
          <div className="flex gap-2">
            <Button size="sm" onClick={() => continueMutation.mutate(false)} disabled={continueMutation.isPending}>
              Continue
            </Button>
            <Button size="sm" variant="outline" onClick={() => continueMutation.mutate(true)} disabled={continueMutation.isPending}>
              Skip remaining
            </Button>
          </div>
        </div>
      )}

      {!!failed_group_names?.length && (
        <div>
          <p className="text-sm font-medium mb-1">Failed groups:</p>
          <ul className="text-sm text-red-600 list-disc list-inside">
            {failed_group_names.map((name) => <li key={name}>{name}</li>)}
          </ul>
        </div>
      )}
    </div>
  )
}
