import type { TaskStatus } from '@/api/types'
import {
  Clock, Loader2, GitBranch, Play, Eye, GitPullRequest,
  CheckCircle2, XCircle, Ban, AlertTriangle, Pause,
} from 'lucide-react'
import { cn } from '@/lib/utils'

const STATUS_CONFIG: Record<string, {
  icon: React.FC<{ className?: string }>
  label: string
  color: string
  bgColor: string
  animate?: boolean
}> = {
  pending:            { icon: Clock,          label: 'Pending',           color: 'text-muted-foreground', bgColor: 'bg-muted' },
  provisioning:       { icon: Loader2,        label: 'Provisioning',      color: 'text-blue-600',         bgColor: 'bg-blue-50',    animate: true },
  cloning:            { icon: GitBranch,      label: 'Cloning',           color: 'text-blue-600',         bgColor: 'bg-blue-50',    animate: true },
  running:            { icon: Play,           label: 'Running',           color: 'text-blue-600',         bgColor: 'bg-blue-50',    animate: true },
  awaiting_approval:  { icon: Eye,            label: 'Awaiting Approval', color: 'text-amber-600',        bgColor: 'bg-amber-50' },
  creating_prs:       { icon: GitPullRequest, label: 'Creating PRs',      color: 'text-violet-600',       bgColor: 'bg-violet-50',  animate: true },
  completed:          { icon: CheckCircle2,   label: 'Completed',         color: 'text-emerald-600',      bgColor: 'bg-emerald-50' },
  failed:             { icon: XCircle,        label: 'Failed',            color: 'text-red-600',          bgColor: 'bg-red-50' },
  cancelled:          { icon: Ban,            label: 'Cancelled',         color: 'text-gray-500',         bgColor: 'bg-gray-50' },
}

export function getStatusConfig(status: string) {
  return STATUS_CONFIG[status] ?? STATUS_CONFIG.pending
}

export function StatusIcon({ status, size = 'sm' }: { status: TaskStatus | string; size?: 'sm' | 'md' }) {
  const config = getStatusConfig(status)
  const Icon = config.icon
  const sizeClass = size === 'md' ? 'h-5 w-5' : 'h-4 w-4'
  return <Icon className={cn(sizeClass, config.color, config.animate && 'animate-spin')} />
}

export function StatusBadge({ status }: { status: TaskStatus | string }) {
  const config = getStatusConfig(status)
  const Icon = config.icon
  return (
    <span className={cn(
      'inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium',
      config.color, config.bgColor,
    )}>
      <Icon className={cn('h-3 w-3', config.animate && 'animate-spin')} />
      {config.label}
    </span>
  )
}

export function InboxBadge({ type, isPaused }: { type?: string; isPaused?: boolean }) {
  if (isPaused || type === 'paused') {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium text-amber-700 bg-amber-50">
        <Pause className="h-3 w-3" /> Paused
      </span>
    )
  }
  if (type === 'awaiting_approval') {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium text-blue-700 bg-blue-50">
        <Eye className="h-3 w-3" /> Needs Approval
      </span>
    )
  }
  if (type === 'steering_requested') {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium text-violet-700 bg-violet-50">
        <AlertTriangle className="h-3 w-3" /> Needs Steering
      </span>
    )
  }
  if (type === 'completed_review') {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium text-emerald-700 bg-emerald-50">
        <CheckCircle2 className="h-3 w-3" /> Review
      </span>
    )
  }
  return null
}
