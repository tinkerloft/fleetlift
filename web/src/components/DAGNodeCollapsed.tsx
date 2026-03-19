import { memo } from 'react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { cn } from '@/lib/utils'
import type { StepStatus } from '@/api/types'

export interface DAGNodeCollapsedData {
  label: string
  count: number
  statusCounts: Record<string, number>
  worstStatus: StepStatus
  selected?: boolean
  [key: string]: unknown
}

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

const ACTIVE_STATUSES = new Set(['running', 'cloning', 'verifying'])

function segmentColor(status: string): string {
  if (status === 'complete') return 'bg-green-500/70'
  if (status === 'running' || status === 'cloning' || status === 'verifying') return 'bg-blue-500/70'
  if (status === 'awaiting_input') return 'bg-amber-500/70'
  if (status === 'failed') return 'bg-red-500/70'
  return 'bg-border'
}

const STATUS_ORDER: StepStatus[] = [
  'failed', 'awaiting_input', 'running', 'cloning', 'verifying', 'pending', 'skipped', 'complete',
]

const STATUS_LABEL: Record<string, string> = {
  complete: 'done',
  failed: 'failed',
  running: 'running',
  cloning: 'cloning',
  verifying: 'verifying',
  awaiting_input: 'waiting',
  pending: 'pending',
  skipped: 'skipped',
}

function DAGNodeCollapsedInner({ data }: NodeProps) {
  const d = data as DAGNodeCollapsedData
  const { label, count, statusCounts, worstStatus, selected } = d

  const isActive = ACTIVE_STATUSES.has(worstStatus)

  const countsText = STATUS_ORDER
    .filter(s => (statusCounts[s] ?? 0) > 0)
    .map(s => `${statusCounts[s]} ${STATUS_LABEL[s] ?? s}`)
    .join(' · ')

  return (
    <>
      <Handle type="target" position={Position.Top} className="!bg-transparent !border-0 !w-2 !h-px" />

      <div className={cn(
        'relative w-[180px] rounded border bg-card px-2 py-1.5',
        'transition-all duration-150 cursor-pointer select-none',
        selected && 'border-accent shadow-[0_0_0_1px_hsl(var(--accent)/0.4)]',
        !selected && worstStatus === 'failed' && 'border-red-500/30',
        !selected && worstStatus === 'complete' && 'border-green-500/30',
        !selected && isActive && 'dag-node-running',
        !selected && worstStatus === 'awaiting_input' && 'dag-node-waiting',
        !selected && !isActive && worstStatus !== 'failed' && worstStatus !== 'complete' && worstStatus !== 'awaiting_input' && 'border-border',
        'hover:brightness-110',
      )}>

        {/* Title row */}
        <div className="flex items-center gap-1.5">
          <span className={cn('status-dot h-[6px] w-[6px] shrink-0', DOT[worstStatus] ?? 'dot-pending')} />
          <span className="text-[11px] font-medium leading-tight truncate text-foreground flex-1">
            {label}
          </span>
          {/* Count badge */}
          <span className="text-[9px] font-mono text-muted-foreground bg-muted px-1 py-px rounded-sm shrink-0 leading-tight">
            ×{count}
          </span>
        </div>

        {/* Status bar */}
        <div className="flex h-[4px] rounded-full overflow-hidden mt-1.5 gap-px">
          {STATUS_ORDER.filter(s => (statusCounts[s] ?? 0) > 0).map(s => (
            <div
              key={s}
              className={cn('rounded-full', segmentColor(s))}
              style={{ flex: statusCounts[s] }}
            />
          ))}
        </div>

        {/* Counts row */}
        {countsText && (
          <div className="mt-1 text-[8px] font-mono text-muted-foreground truncate leading-tight">
            {countsText}
          </div>
        )}
      </div>

      <Handle type="source" position={Position.Bottom} className="!bg-transparent !border-0 !w-2 !h-px" />
    </>
  )
}

export const DAGNodeCollapsed = memo(DAGNodeCollapsedInner)
