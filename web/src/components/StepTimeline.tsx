import type { StepRun, StepStatus } from '@/api/types'
import { cn } from '@/lib/utils'
import { useLiveDuration } from '@/lib/use-live-duration'

interface StepTimelineProps {
  stepRuns: StepRun[]
  selectedStepId?: string | null
  onSelect: (stepId: string) => void
}

const DOT_COLOR: Record<string, string> = {
  pending: 'bg-gray-300',
  cloning: 'bg-blue-500',
  running: 'bg-blue-500',
  verifying: 'bg-violet-500',
  awaiting_input: 'bg-amber-500',
  complete: 'bg-green-500',
  failed: 'bg-red-500',
  skipped: 'bg-gray-300',
}

const PULSING = new Set<StepStatus>(['running', 'cloning', 'verifying'])

export function StepTimeline({ stepRuns, selectedStepId, onSelect }: StepTimelineProps) {
  // Group continuation steps under their parent
  const rootSteps = stepRuns.filter(s => !s.parent_step_run_id)
  const continuationsByParent = new Map<string, StepRun[]>()
  stepRuns.filter(s => s.parent_step_run_id).forEach(s => {
    const list = continuationsByParent.get(s.parent_step_run_id!) ?? []
    list.push(s)
    continuationsByParent.set(s.parent_step_run_id!, list)
  })

  return (
    <div className="relative pl-7">
      {/* Vertical line */}
      <div className="absolute left-[7px] top-1 bottom-1 w-0.5 bg-border rounded-full" />

      {rootSteps.map((sr) => (
        <div key={sr.id}>
          <StepTimelineItem sr={sr} selectedStepId={selectedStepId} onSelect={onSelect} />
          {continuationsByParent.get(sr.id)?.map(continuation => (
            <div key={continuation.id} className="ml-4">
              <StepTimelineItem sr={continuation} selectedStepId={selectedStepId} onSelect={onSelect} isResume />
            </div>
          ))}
        </div>
      ))}
    </div>
  )
}

function StepTimelineItem({ sr, selectedStepId, onSelect, isResume }: {
  sr: StepRun; selectedStepId?: string | null; onSelect: (stepId: string) => void; isResume?: boolean
}) {
  const elapsed = useLiveDuration(sr.started_at, sr.completed_at)

  return (
    <button
      onClick={() => onSelect(sr.step_id)}
      className={cn(
        'relative flex w-full items-start gap-3 rounded-lg px-3 py-2.5 text-left transition-colors',
        selectedStepId === sr.step_id && 'bg-blue-500/5',
        selectedStepId !== sr.step_id && 'hover:bg-muted',
      )}
    >
      {/* Status dot */}
      <span className={cn(
        'absolute -left-5 top-3.5 h-2.5 w-2.5 rounded-full border-2 border-card z-10',
        DOT_COLOR[sr.status] ?? 'bg-gray-300',
        PULSING.has(sr.status as StepStatus) && 'animate-pulse',
      )} />

      <div className="flex-1 min-w-0">
        <div className="flex items-center justify-between">
          <span className="text-[13px] font-medium truncate">
            {isResume && <span className="text-xs text-amber-400 mr-1">↪</span>}
            {sr.step_title || sr.step_id}
          </span>
          <span className={cn(
            'text-xs tabular-nums text-muted-foreground ml-2 shrink-0',
            PULSING.has(sr.status as StepStatus) && 'text-blue-500',
          )}>
            {elapsed ?? '—'}
          </span>
        </div>
        <span className="text-[11px] text-muted-foreground">
          {sr.status.replaceAll('_', ' ')}
        </span>
      </div>
    </button>
  )
}
