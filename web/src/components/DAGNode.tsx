import { memo } from 'react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { cn } from '@/lib/utils'
import type { StepStatus } from '@/api/types'
import { formatDuration } from '@/lib/format'

export interface DAGNodeData {
  label: string
  status: StepStatus
  mode?: string
  startedAt?: string
  completedAt?: string
  selected?: boolean
  [key: string]: unknown
}

const STATUS_DOT_COLOR: Record<string, string> = {
  pending: 'bg-gray-400',
  cloning: 'bg-blue-500',
  running: 'bg-blue-500',
  verifying: 'bg-violet-500',
  awaiting_input: 'bg-amber-500',
  complete: 'bg-green-500',
  failed: 'bg-red-500',
  skipped: 'bg-gray-400',
}

const STATUS_ACCENT_COLOR: Record<string, string> = {
  pending: 'bg-gray-300',
  cloning: 'bg-blue-500',
  running: 'bg-blue-500',
  verifying: 'bg-violet-500',
  awaiting_input: 'bg-amber-500',
  complete: 'bg-green-500',
  failed: 'bg-red-500',
  skipped: 'bg-gray-300',
}

const PULSING = new Set(['running', 'cloning', 'verifying'])

function DAGNodeInner({ data }: NodeProps) {
  const d = data as DAGNodeData
  const elapsed = d.startedAt
    ? formatDuration(
        (d.completedAt ? new Date(d.completedAt).getTime() : Date.now()) -
        new Date(d.startedAt).getTime()
      )
    : null

  return (
    <>
      <Handle type="target" position={Position.Top} className="!bg-transparent !border-0 !w-3 !h-1" />
      <div className={cn(
        'relative rounded-lg border bg-card px-3 py-2 min-w-[180px] transition-shadow',
        d.selected && 'border-blue-500 shadow-[0_0_0_2px_hsl(221_83%_53%/0.2)]',
        !d.selected && 'hover:shadow-md',
      )}>
        {/* Left accent bar */}
        <div className={cn(
          'absolute left-0 top-0 bottom-0 w-1 rounded-l-lg',
          STATUS_ACCENT_COLOR[d.status] ?? 'bg-gray-300',
        )} />

        {/* Title row */}
        <div className="flex items-center gap-1.5 pl-2">
          <span className={cn(
            'inline-block h-2 w-2 rounded-full shrink-0',
            STATUS_DOT_COLOR[d.status] ?? 'bg-gray-400',
            PULSING.has(d.status) && 'animate-pulse',
          )} />
          <span className="text-[13px] font-medium truncate">{d.label}</span>
        </div>

        {/* Meta row */}
        <div className="flex items-center gap-2 pl-5 mt-0.5">
          {d.mode && (
            <span className="text-[10px] font-medium bg-muted px-1.5 py-px rounded">
              {d.mode}
            </span>
          )}
          {elapsed && (
            <span className="text-[11px] text-muted-foreground tabular-nums">
              {elapsed}
            </span>
          )}
          {d.status === 'awaiting_input' && !elapsed && (
            <span className="text-[11px] text-amber-600">awaiting input</span>
          )}
        </div>
      </div>
      <Handle type="source" position={Position.Bottom} className="!bg-transparent !border-0 !w-3 !h-1" />
    </>
  )
}

export const DAGNode = memo(DAGNodeInner)
