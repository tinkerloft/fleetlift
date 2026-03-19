import { memo } from 'react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { cn } from '@/lib/utils'
import type { StepStatus } from '@/api/types'
import { useLiveDuration } from '@/lib/use-live-duration'

export interface DAGNodeData {
  label: string
  status: StepStatus
  mode?: string
  startedAt?: string
  completedAt?: string
  selected?: boolean
  /** Phase 3: repo name extracted from step_run.input.repo_url */
  repoName?: string
  /** Phase 3: positional label e.g. "(2/5)" */
  repoIndex?: string
  [key: string]: unknown
}

const ACTIVE = new Set(['running', 'cloning', 'verifying', 'awaiting_input'])

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

function DAGNodeInner({ data }: NodeProps) {
  const d = data as DAGNodeData
  const elapsed = useLiveDuration(d.startedAt, d.completedAt)

  return (
    <>
      <Handle type="target" position={Position.Top} className="!bg-transparent !border-0 !w-2 !h-px" />

      <div className={cn(
        'relative w-[150px] rounded border bg-card px-2 py-1.5',
        'transition-all duration-150 cursor-pointer select-none',
        // Selected overrides everything
        d.selected && 'border-accent shadow-[0_0_0_1px_hsl(var(--accent)/0.4),0_2px_16px_hsl(var(--accent)/0.12)]',
        // Active: animated blue border glow
        !d.selected && ACTIVE.has(d.status) && d.status !== 'awaiting_input' && 'dag-node-running',
        !d.selected && d.status === 'awaiting_input' && 'dag-node-waiting',
        // Complete: subtle green border
        !d.selected && d.status === 'complete' && 'border-green-500/30',
        // Failed: subtle red border
        !d.selected && d.status === 'failed' && 'border-red-500/30',
        // Default
        !d.selected && !ACTIVE.has(d.status) && d.status !== 'complete' && d.status !== 'failed' && 'border-border',
        'hover:brightness-110',
      )}>

        {/* Title row */}
        <div className="flex items-center gap-1.5">
          <span className={cn('status-dot h-[6px] w-[6px] shrink-0', DOT[d.status] ?? 'dot-pending')} />
          <span className="text-[11px] font-medium leading-tight truncate text-foreground">
            {d.label}
          </span>
        </div>

        {/* Repo sub-label — fan-out individual nodes (Phase 3) */}
        {d.repoName && (
          <div className="mt-px pl-[10px] flex items-baseline gap-1 min-w-0">
            <span className="text-[9px] font-mono text-accent truncate leading-tight">· {d.repoName}</span>
            {d.repoIndex && (
              <span className="text-[9px] font-mono text-muted-foreground shrink-0 leading-tight">{d.repoIndex}</span>
            )}
          </div>
        )}

        {/* Meta row */}
        <div className="flex items-center gap-1 mt-1 pl-[10px]">
          {d.mode && (
            <span className="text-[8px] font-mono px-1 py-px rounded-sm border border-border bg-muted text-muted-foreground tracking-wide leading-tight">
              {d.mode}
            </span>
          )}
          {elapsed && (
            <span className="text-[8px] font-mono text-muted-foreground tabular-nums leading-tight">
              {elapsed}
            </span>
          )}
          {d.status === 'awaiting_input' && !elapsed && (
            <span className="text-[8px] font-mono text-amber-400 leading-tight">waiting</span>
          )}
        </div>
      </div>

      <Handle type="source" position={Position.Bottom} className="!bg-transparent !border-0 !w-2 !h-px" />
    </>
  )
}

export const DAGNode = memo(DAGNodeInner)
