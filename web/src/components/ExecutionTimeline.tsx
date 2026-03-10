import type { TaskStatus } from '@/api/types'
import { getStatusConfig } from '@/components/StatusIcon'
import { cn } from '@/lib/utils'

const PHASE_ORDER: TaskStatus[] = [
  'pending', 'provisioning', 'cloning', 'running',
  'awaiting_approval', 'creating_prs', 'completed',
]

const TERMINAL_STATUSES: TaskStatus[] = ['failed', 'cancelled']

function getPhaseIndex(status: TaskStatus): number {
  if (TERMINAL_STATUSES.includes(status)) {
    return PHASE_ORDER.indexOf('running')
  }
  return PHASE_ORDER.indexOf(status)
}

function formatElapsed(ms: number): string {
  if (ms < 60_000) return `${Math.round(ms / 1000)}s`
  return `${Math.round(ms / 60_000)}m`
}

export function ExecutionTimeline({
  status,
  phaseTimestamps = {},
}: {
  status: TaskStatus
  phaseTimestamps?: Partial<Record<TaskStatus, Date>>
}) {
  const currentIdx = getPhaseIndex(status)
  const isFailed = status === 'failed'
  const isCancelled = status === 'cancelled'
  const isTerminal = isFailed || isCancelled

  return (
    <div className="flex items-center gap-0 overflow-x-auto pb-1">
      {PHASE_ORDER.map((phase, idx) => {
        const config = getStatusConfig(phase)
        const isActive = idx === currentIdx && !isTerminal
        const isPast = idx < currentIdx || (isTerminal && idx < currentIdx)
        const isCurrent = idx === currentIdx

        let displayConfig = config
        if (isCurrent && isTerminal) {
          displayConfig = getStatusConfig(status)
        }

        const Icon = displayConfig.icon

        // Compute elapsed: time from this phase's timestamp to the next phase's timestamp (or now if active)
        const phaseTime = phaseTimestamps[phase]
        const nextPhase = PHASE_ORDER[idx + 1]
        const nextPhaseTime = nextPhase ? phaseTimestamps[nextPhase] : undefined
        let elapsed: string | null = null
        if (phaseTime) {
          const end = nextPhaseTime ?? (isActive ? new Date() : undefined)
          if (end) {
            elapsed = formatElapsed(end.getTime() - phaseTime.getTime())
          }
        }

        return (
          <div key={phase} className="flex items-center">
            {/* Phase node */}
            <div className={cn(
              'flex flex-col items-center gap-0.5',
            )}>
              <div className={cn(
                'flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[11px] font-medium whitespace-nowrap transition-all',
                isActive && 'bg-blue-50 text-blue-700 ring-1 ring-blue-200',
                isPast && !isCurrent && 'text-emerald-600',
                isCurrent && isFailed && 'bg-red-50 text-red-700 ring-1 ring-red-200',
                isCurrent && isCancelled && 'bg-gray-100 text-gray-600 ring-1 ring-gray-200',
                !isActive && !isPast && !isCurrent && 'text-muted-foreground/50',
              )}>
                <Icon className={cn(
                  'h-3 w-3',
                  isActive && 'animate-spin text-blue-600',
                  isPast && !isCurrent && 'text-emerald-500',
                  isCurrent && isFailed && 'text-red-500',
                  isCurrent && isCancelled && 'text-gray-400',
                  !isActive && !isPast && !isCurrent && 'text-muted-foreground/30',
                )} />
                {isCurrent && isTerminal ? displayConfig.label : config.label}
              </div>
              {elapsed && (
                <span className="text-[9px] text-muted-foreground leading-none">{elapsed}</span>
              )}
            </div>

            {/* Connector line */}
            {idx < PHASE_ORDER.length - 1 && (
              <div className={cn(
                'h-px w-4 shrink-0 mb-3',
                isPast && idx < currentIdx - 1 ? 'bg-emerald-300' :
                isPast ? 'bg-emerald-200' :
                'bg-border',
              )} />
            )}
          </div>
        )
      })}
    </div>
  )
}
