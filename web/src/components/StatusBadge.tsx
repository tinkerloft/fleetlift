import { cn } from '@/lib/utils'
import type { RunStatus, StepStatus } from '@/api/types'

type AnyStatus = RunStatus | StepStatus

const STATUS_STYLE: Record<string, string> = {
  pending:        'bg-muted            text-muted-foreground border-border',
  cloning:        'bg-blue-500/10      text-blue-400         border-blue-500/20',
  running:        'bg-blue-500/10      text-blue-400         border-blue-500/20',
  verifying:      'bg-violet-500/10    text-violet-400       border-violet-500/20',
  awaiting_input: 'bg-amber-500/10     text-amber-400        border-amber-500/20',
  complete:       'bg-green-500/10     text-green-400        border-green-500/20',
  failed:         'bg-red-500/10       text-red-400          border-red-500/20',
  skipped:        'bg-muted            text-muted-foreground border-border',
  cancelled:      'bg-muted            text-muted-foreground border-border',
}

const PULSE = new Set(['running', 'cloning', 'verifying', 'awaiting_input'])

export function StatusBadge({ status, className }: { status: AnyStatus; className?: string }) {
  return (
    <span className={cn(
      'inline-flex items-center gap-1.5 rounded border px-2 py-0.5',
      'text-[10px] font-mono font-medium tracking-wide whitespace-nowrap',
      STATUS_STYLE[status] ?? STATUS_STYLE.pending,
      className,
    )}>
      {PULSE.has(status) && (
        <span className="relative flex h-1.5 w-1.5 shrink-0">
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-current opacity-60" />
          <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-current" />
        </span>
      )}
      {status?.replaceAll('_', ' ')}
    </span>
  )
}
