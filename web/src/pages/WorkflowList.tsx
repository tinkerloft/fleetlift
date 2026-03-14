import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { SkeletonCard } from '@/components/Skeleton'
import { EmptyState } from '@/components/EmptyState'
import { workflowCategory, CATEGORY_STYLES } from '@/lib/workflow-colors'
import { cn } from '@/lib/utils'
import {
  Shield, Bug, GitBranch, Search, Tag, Terminal,
  MessageSquare, RefreshCw, LayoutTemplate,
} from 'lucide-react'
import type { WorkflowTemplate } from '@/api/types'
import { parse as parseYaml } from '@/lib/yaml'

const ICON_MAP: Record<string, React.FC<{ className?: string }>> = {
  Shield, Bug, GitBranch, Search, Tag, Terminal,
  MessageSquare, RefreshCw,
}

function WorkflowCard({ wf }: { wf: WorkflowTemplate }) {
  const cat = workflowCategory(wf.tags ?? [])
  const styles = CATEGORY_STYLES[cat.color]
  const Icon = ICON_MAP[cat.icon] ?? Terminal

  let stepCount = 0
  let modes: string[] = []
  try {
    const def = parseYaml(wf.yaml_body) as { steps?: { mode?: string }[] }
    stepCount = def?.steps?.length ?? 0
    modes = [...new Set((def?.steps ?? []).map(s => s.mode).filter(Boolean) as string[])]
  } catch { /* ignore */ }

  return (
    <Link
      to={`/workflows/${wf.slug}`}
      className={cn(
        'rounded-lg border border-t-4 bg-card overflow-hidden transition-all hover:shadow-md hover:border-foreground/20',
        styles.border,
      )}
    >
      <div className="p-4 space-y-2">
        <div className="flex items-center gap-2.5">
          <div className={cn('flex h-8 w-8 items-center justify-center rounded-lg', styles.iconBg)}>
            <Icon className={cn('h-[18px] w-[18px]', styles.text)} />
          </div>
          <h3 className="font-semibold flex-1">{wf.title}</h3>
          {wf.builtin && <Badge variant="secondary" className="text-[10px]">builtin</Badge>}
        </div>
        <p className="text-sm text-muted-foreground line-clamp-2">{wf.description}</p>
        {stepCount > 0 && (
          <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <span>{stepCount} steps</span>
            {modes.map(m => (
              <span key={m}>&middot; {m}</span>
            ))}
          </div>
        )}
        <div className="flex flex-wrap gap-1">
          {wf.tags?.map((tag) => (
            <Badge key={tag} variant="outline" className="text-[10px]">{tag}</Badge>
          ))}
        </div>
      </div>
    </Link>
  )
}

export function WorkflowListPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['workflows'],
    queryFn: () => api.listWorkflows(),
  })

  const sorted = (data?.items ?? []).slice().sort((a, b) => a.title.localeCompare(b.title))

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Workflow Library</h1>
        {sorted.length > 0 && (
          <p className="text-sm text-muted-foreground mt-1">{sorted.length} workflows available</p>
        )}
      </div>

      {isLoading && (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <SkeletonCard /><SkeletonCard /><SkeletonCard />
        </div>
      )}

      {!isLoading && sorted.length === 0 && (
        <EmptyState icon={LayoutTemplate} title="No workflows yet" description="Create your first workflow template to get started." />
      )}

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {sorted.map(wf => <WorkflowCard key={wf.id} wf={wf} />)}
      </div>
    </div>
  )
}
