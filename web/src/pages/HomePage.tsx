import { useState } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { Run, WorkflowTemplate } from '@/api/types'
import { ModelSelect, getPreferredModel } from '@/components/ModelSelect'
import { StatusBadge } from '@/components/StatusBadge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { workflowCategory, CATEGORY_STYLES, WORKFLOW_ICON_MAP } from '@/lib/workflow-colors'
import { formatTimeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Sparkles, Play, RotateCcw, ArrowRight, Inbox, Terminal } from 'lucide-react'

function PromptZone({
  onSubmit,
  isSubmitting,
  error,
}: {
  onSubmit: (prompt: string, repoUrl: string, branch: string, model: string) => void
  isSubmitting: boolean
  error?: string
}) {
  const [prompt, setPrompt] = useState('')
  const [repoUrl, setRepoUrl] = useState('')
  const [branch, setBranch] = useState('main')
  const [model, setModel] = useState(getPreferredModel)

  const canSubmit = prompt.trim().length > 0 && repoUrl.trim().length > 0

  return (
    <div className="relative rounded-xl border border-violet-500/20 bg-gradient-to-b from-violet-500/[0.03] to-transparent p-[1px]">
      <div className="rounded-[11px] bg-card p-5 space-y-4">
        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          placeholder="Describe what you want to do..."
          rows={4}
          className="w-full resize-none rounded-lg border border-zinc-700/60 bg-zinc-900/60 px-4 py-3 text-sm text-zinc-100 placeholder:text-zinc-500 focus:border-violet-500/40 focus:outline-none focus:ring-1 focus:ring-violet-500/20"
        />

        <div className="flex flex-wrap items-end gap-3">
          <div className="flex-1 min-w-[200px]">
            <label className="mb-1 block text-xs text-muted-foreground">Repository URL</label>
            <input
              type="text"
              value={repoUrl}
              onChange={(e) => setRepoUrl(e.target.value)}
              placeholder="https://github.com/org/repo"
              className="w-full rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-200 placeholder:text-zinc-500 focus:border-violet-500/40 focus:outline-none focus:ring-1 focus:ring-violet-500/20"
            />
          </div>

          <div className="w-32">
            <label className="mb-1 block text-xs text-muted-foreground">Branch</label>
            <input
              type="text"
              value={branch}
              onChange={(e) => setBranch(e.target.value)}
              className="w-full rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-200 focus:border-violet-500/40 focus:outline-none focus:ring-1 focus:ring-violet-500/20"
            />
          </div>

          <div>
            <label className="mb-1 block text-xs text-muted-foreground">Model</label>
            <ModelSelect value={model} onChange={setModel} />
          </div>

          <div className="flex items-end gap-2">
            <Button variant="outline" size="default" disabled className="gap-1.5 text-muted-foreground">
              <Sparkles className="h-3.5 w-3.5" />
              Improve
            </Button>

            <Button
              size="default"
              disabled={!canSubmit || isSubmitting}
              onClick={() => onSubmit(prompt, repoUrl, branch, model)}
              className="gap-1.5 bg-violet-600 text-white hover:bg-violet-500 disabled:opacity-50"
            >
              <Play className="h-3.5 w-3.5" />
              {isSubmitting ? 'Starting...' : 'Run'}
            </Button>
          </div>
        </div>
        {error && <p className="text-sm text-red-400">{error}</p>}
      </div>
    </div>
  )
}

function TemplateCard({ wf }: { wf: WorkflowTemplate }) {
  const cat = workflowCategory(wf.tags ?? [])
  const styles = CATEGORY_STYLES[cat.color]
  const Icon = WORKFLOW_ICON_MAP[cat.icon] ?? Terminal

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
          <h3 className="font-semibold flex-1 text-sm">{wf.title}</h3>
        </div>
        <p className="text-xs text-muted-foreground line-clamp-2">{wf.description}</p>
        <div className="flex flex-wrap gap-1">
          {wf.tags?.map((tag) => (
            <Badge key={tag} variant="outline" className="text-[10px]">{tag}</Badge>
          ))}
        </div>
      </div>
    </Link>
  )
}

function TemplateGrid({ workflows }: { workflows: WorkflowTemplate[] }) {
  const sorted = workflows.slice().sort((a, b) => a.title.localeCompare(b.title))
  const display = sorted.slice(0, 6)

  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Workflows</h2>
        <Link to="/workflows" className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors">
          View all <ArrowRight className="h-3 w-3" />
        </Link>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {display.map(wf => <TemplateCard key={wf.id} wf={wf} />)}
      </div>
    </section>
  )
}

function RecentTasks({
  runs,
  onRetry,
  isRetrying,
}: {
  runs: Run[]
  onRetry: (run: Run) => void
  isRetrying: boolean
}) {
  if (runs.length === 0) {
    return (
      <section className="space-y-3">
        <h2 className="text-lg font-semibold">Recent Tasks</h2>
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed border-zinc-700 py-10 text-center">
          <Inbox className="h-8 w-8 text-muted-foreground mb-2" />
          <p className="text-sm text-muted-foreground">No runs yet. Start one above!</p>
        </div>
      </section>
    )
  }

  return (
    <section className="space-y-3">
      <h2 className="text-lg font-semibold">Recent Tasks</h2>
      <div className="space-y-2">
        {runs.map((run) => (
          <Link
            key={run.id}
            to={`/runs/${run.id}`}
            className="flex items-center gap-3 rounded-lg border bg-card px-4 py-3 transition-colors hover:border-foreground/20"
          >
            <StatusBadge status={run.status} />
            <span className="flex-1 truncate text-sm">{run.workflow_title}</span>
            <span className="text-xs text-muted-foreground whitespace-nowrap">
              {formatTimeAgo(run.created_at)}
            </span>
            {(run.status === 'failed' || run.status === 'complete') && (
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 shrink-0"
                disabled={isRetrying}
                onClick={(e) => {
                  e.preventDefault()
                  e.stopPropagation()
                  onRetry(run)
                }}
                title="Retry"
              >
                <RotateCcw className="h-3.5 w-3.5" />
              </Button>
            )}
          </Link>
        ))}
      </div>
    </section>
  )
}

export function HomePage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const { data: workflowData } = useQuery({
    queryKey: ['workflows'],
    queryFn: () => api.listWorkflows(),
  })

  const { data: runData } = useQuery({
    queryKey: ['my-runs'],
    queryFn: () => api.listMyRuns(10),
  })

  const createRun = useMutation({
    mutationFn: ({ workflowId, parameters, model }: { workflowId: string; parameters: Record<string, unknown>; model?: string }) =>
      api.createRun(workflowId, parameters, model),
    onSuccess: (resp) => {
      queryClient.invalidateQueries({ queryKey: ['my-runs'] })
      navigate(`/runs/${resp.id}`)
    },
  })

  const retryRun = useMutation({
    mutationFn: (run: Run) =>
      api.createRun(run.workflow_id, run.parameters ?? {}, run.model ?? undefined),
    onSuccess: (resp) => {
      queryClient.invalidateQueries({ queryKey: ['my-runs'] })
      navigate(`/runs/${resp.id}`)
    },
  })

  function handleSubmit(prompt: string, repoUrl: string, branch: string, model: string) {
    createRun.mutate({
      workflowId: 'quick-run',
      parameters: { prompt, repo_url: repoUrl, branch },
      model: model || undefined,
    })
  }

  const workflows = workflowData?.items ?? []
  const runs = runData?.items ?? []

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-bold">Fleetlift</h1>
        <p className="text-sm text-muted-foreground mt-1">Run AI workflows on your codebase</p>
      </div>

      <PromptZone onSubmit={handleSubmit} isSubmitting={createRun.isPending} error={createRun.isError ? createRun.error.message : undefined} />

      {retryRun.isError && (
        <p className="text-sm text-red-400">Retry failed: {retryRun.error.message}</p>
      )}

      <div className="grid gap-8 lg:grid-cols-[1fr_360px]">
        <TemplateGrid workflows={workflows} />
        <RecentTasks runs={runs} onRetry={(run) => retryRun.mutate(run)} isRetrying={retryRun.isPending} />
      </div>
    </div>
  )
}
