import { cn } from '@/lib/utils'
import { Badge } from './ui/badge'
import type { RunStatus, StepStatus } from '@/api/types'

type AnyStatus = RunStatus | StepStatus

const STATUS_CONFIG: Record<string, { variant: 'default' | 'secondary' | 'destructive' | 'success' | 'warning' | 'outline'; pulse?: boolean }> = {
  pending:        { variant: 'secondary' },
  cloning:        { variant: 'secondary', pulse: true },
  running:        { variant: 'secondary', pulse: true },
  verifying:      { variant: 'secondary', pulse: true },
  awaiting_input: { variant: 'warning' },
  complete:       { variant: 'success' },
  failed:         { variant: 'destructive' },
  skipped:        { variant: 'outline' },
  cancelled:      { variant: 'outline' },
}

export function StatusBadge({ status, className }: { status: AnyStatus; className?: string }) {
  const config = STATUS_CONFIG[status] ?? { variant: 'secondary' as const }
  return (
    <Badge variant={config.variant} className={cn('gap-1.5', className)}>
      {config.pulse && (
        <span className="relative flex h-2 w-2">
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-current opacity-50" />
          <span className="relative inline-flex h-2 w-2 rounded-full bg-current" />
        </span>
      )}
      {status.replaceAll('_', ' ')}
    </Badge>
  )
}
