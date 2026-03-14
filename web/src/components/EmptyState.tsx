import type { LucideIcon } from 'lucide-react'
import { Button } from './ui/button'

interface EmptyStateProps {
  icon: LucideIcon
  title: string
  description?: string
  action?: { label: string; href: string }
}

export function EmptyState({ icon: Icon, title, description, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 rounded-lg border-2 border-dashed py-16 text-center">
      <Icon className="h-12 w-12 text-muted-foreground/40" strokeWidth={1.5} />
      <p className="text-sm font-medium text-muted-foreground">{title}</p>
      {description && <p className="text-sm text-muted-foreground/70">{description}</p>}
      {action && (
        <Button variant="default" size="sm" className="mt-2" asChild>
          <a href={action.href}>{action.label}</a>
        </Button>
      )}
    </div>
  )
}
