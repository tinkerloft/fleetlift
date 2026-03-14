import { cn } from '@/lib/utils'

export function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={cn('animate-pulse rounded-md bg-muted', className)} {...props} />
  )
}

/** Card-shaped skeleton for workflow/run list loading states */
export function SkeletonCard() {
  return (
    <div className="rounded-lg border overflow-hidden">
      <div className="h-1 bg-muted" />
      <div className="p-4 space-y-3">
        <Skeleton className="h-4 w-3/5" />
        <Skeleton className="h-3 w-4/5" />
        <Skeleton className="h-3 w-2/5" />
      </div>
    </div>
  )
}

/** Table row skeleton for run list loading states */
export function SkeletonRow() {
  return (
    <div className="flex items-center gap-4 px-4 py-3 border-b last:border-0">
      <Skeleton className="h-4 w-1/4" />
      <Skeleton className="h-5 w-16 rounded-full" />
      <Skeleton className="h-3 w-1/5" />
      <Skeleton className="h-3 w-16" />
    </div>
  )
}
