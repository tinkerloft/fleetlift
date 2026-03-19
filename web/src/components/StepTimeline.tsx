import type { StepRun } from '@/api/types'
import { cn } from '@/lib/utils'
import { useLiveDuration } from '@/lib/use-live-duration'
import { formatCost } from '@/lib/format'

interface StepTimelineProps {
  stepRuns: StepRun[]
  selectedStepId?: string | null
  onSelect: (stepId: string) => void
}

/** Maps status → CSS glow utility defined in index.css */
const DOT: Record<string, string> = {
  pending:        'dot-pending',
  cloning:        'dot-cloning',
  running:        'dot-running',
  verifying:      'dot-verifying',
  awaiting_input: 'dot-awaiting_input',
  complete:       'dot-complete',
  failed:         'dot-failed',
  skipped:        'dot-skipped',
}

/** Strip fan-out index suffix, e.g. "assess-0" → "assess". */
function baseStepId(id: string): string {
  return id.replace(/-\d+$/, '')
}

/** Extract the last path segment of a GitHub URL as a short repo name. */
function repoName(url: string): string {
  try { return new URL(url).pathname.split('/').filter(Boolean).pop() ?? url }
  catch { return url }
}

export function StepTimeline({ stepRuns, selectedStepId, onSelect }: StepTimelineProps) {
  const rootSteps = stepRuns.filter(s => !s.parent_step_run_id)
  const continuationsByParent = new Map<string, StepRun[]>()
  stepRuns.filter(s => s.parent_step_run_id).forEach(s => {
    const list = continuationsByParent.get(s.parent_step_run_id!) ?? []
    list.push(s)
    continuationsByParent.set(s.parent_step_run_id!, list)
  })

  return (
    <div className="relative pl-7">
      {/* Vertical connector line */}
      <div className="absolute left-[7px] top-1 bottom-1 w-px bg-border rounded-full" />

      {rootSteps.map((sr) => (
        <div key={sr.id}>
          <StepTimelineItem sr={sr} selectedStepId={selectedStepId} onSelect={onSelect} />
          {continuationsByParent.get(sr.id)?.map(cont => (
            <div key={cont.id} className="ml-4">
              <StepTimelineItem sr={cont} selectedStepId={selectedStepId} onSelect={onSelect} isResume />
            </div>
          ))}
        </div>
      ))}
    </div>
  )
}

function StepTimelineItem({ sr, selectedStepId, onSelect, isResume }: {
  sr: StepRun
  selectedStepId?: string | null
  onSelect: (stepId: string) => void
  isResume?: boolean
}) {
  const elapsed = useLiveDuration(sr.started_at, sr.completed_at)
  const isSelected = selectedStepId === sr.step_id || selectedStepId === baseStepId(sr.step_id)
  const inputRepoUrl = sr.input?.repo_url as string | undefined
  const inputRepoName = inputRepoUrl ? repoName(inputRepoUrl) : null

  return (
    <button
      onClick={() => onSelect(sr.step_id)}
      className={cn(
        'relative flex w-full items-start gap-3 rounded-md px-3 py-2.5 text-left transition-colors',
        'border-l-2',
        isSelected
          ? 'bg-accent/5 border-l-accent'
          : 'border-l-transparent hover:bg-muted/60',
      )}
    >
      {/* Status dot — positioned on the vertical timeline */}
      <span className={cn(
        'absolute -left-5 top-3.5 status-dot h-2.5 w-2.5 border-2 border-card z-10',
        DOT[sr.status] ?? 'dot-pending',
      )} />

      <div className="flex-1 min-w-0">
        {/* Title + duration */}
        <div className="flex items-center justify-between gap-2">
          <span className="text-[12px] font-medium truncate text-foreground">
            {isResume && <span className="text-amber-400 mr-1 text-[10px]">↪</span>}
            {sr.step_title || sr.step_id}
          </span>
          <span className={cn(
            'text-[10px] font-mono tabular-nums text-muted-foreground shrink-0',
            (sr.status === 'running' || sr.status === 'cloning') && 'text-blue-400',
          )}>
            {elapsed ?? '—'}
          </span>
        </div>

        {/* Repo sub-label (Phase 1: from input.repo_url) */}
        {inputRepoName && (
          <div className="text-[10px] font-mono text-accent mt-px truncate">
            {inputRepoName}
          </div>
        )}

        {/* Status + cost */}
        <div className="flex items-center gap-2 mt-0.5">
          <span className="text-[10px] text-muted-foreground">
            {sr.status.replaceAll('_', ' ')}
          </span>
          {sr.cost_usd != null && sr.cost_usd > 0 && (
            <span className="text-[10px] font-mono text-muted-foreground">{formatCost(sr.cost_usd)}</span>
          )}
        </div>
      </div>
    </button>
  )
}
