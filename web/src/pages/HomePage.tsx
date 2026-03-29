import { useState, useCallback, useRef, useEffect, useMemo } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { Run, WorkflowTemplate, Preset, SavedRepo } from '@/api/types'
import { ModelSelect, getPreferredModel } from '@/components/ModelSelect'
import { StatusBadge } from '@/components/StatusBadge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { workflowCategory, CATEGORY_STYLES, WORKFLOW_ICON_MAP } from '@/lib/workflow-colors'
import { formatTimeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'
import { PromptImproveModal } from '@/components/PromptImproveModal'
import { Sparkles, Play, RotateCcw, ArrowRight, Inbox, Terminal, Bookmark, BookmarkPlus, X, Save, ChevronDown } from 'lucide-react'

/* ─── Repo Combobox ─── */

function RepoCombobox({
  value,
  onChange,
  savedRepos,
  onSaveRepo,
}: {
  value: string
  onChange: (url: string) => void
  savedRepos: SavedRepo[]
  onSaveRepo?: (url: string) => void
}) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [])

  const isSaved = savedRepos.some((r) => r.url === value)

  return (
    <div className="relative flex-1 min-w-[200px]" ref={ref}>
      <label className="mb-1 block text-xs text-muted-foreground">Repository URL</label>
      <div className="flex gap-1">
        <div className="relative flex-1">
          <input
            type="text"
            value={value}
            onChange={(e) => onChange(e.target.value)}
            onFocus={() => savedRepos.length > 0 && setOpen(true)}
            placeholder="https://github.com/org/repo"
            className="w-full rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 pr-8 text-sm text-zinc-200 placeholder:text-zinc-500 focus:border-violet-500/40 focus:outline-none focus:ring-1 focus:ring-violet-500/20"
          />
          {savedRepos.length > 0 && (
            <button
              type="button"
              onClick={() => setOpen(!open)}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-zinc-500 hover:text-zinc-300"
            >
              <ChevronDown className="h-3.5 w-3.5" />
            </button>
          )}
        </div>
        {value.trim() && !isSaved && onSaveRepo && (
          <button
            type="button"
            onClick={() => onSaveRepo(value.trim())}
            className="rounded-md border border-zinc-700 bg-zinc-900 px-2 text-zinc-500 hover:text-violet-400 hover:border-violet-500/40 transition-colors"
            title="Save repo for quick access"
          >
            <BookmarkPlus className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
      {open && savedRepos.length > 0 && (
        <div className="absolute z-50 mt-1 w-full rounded-md border border-zinc-700 bg-zinc-900 shadow-lg max-h-48 overflow-auto">
          {savedRepos.map((repo) => (
            <button
              key={repo.id}
              type="button"
              className="w-full px-3 py-2 text-left text-sm hover:bg-zinc-800 flex items-center gap-2 truncate"
              onClick={() => {
                onChange(repo.url)
                setOpen(false)
              }}
            >
              <Bookmark className="h-3 w-3 shrink-0 text-violet-400" />
              <span className="truncate">{repo.label || repo.url}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

/* ─── Prompt Zone ─── */

function PromptZone({
  onSubmit,
  isSubmitting,
  error,
  savedRepos,
  onSaveRepo,
  presets,
  injectedPrompt,
  onPromptConsumed,
  onSaveAsPreset,
}: {
  onSubmit: (prompt: string, repoUrl: string, branch: string, model: string) => void
  isSubmitting: boolean
  error?: string
  savedRepos: SavedRepo[]
  onSaveRepo: (url: string) => void
  presets: Preset[]
  injectedPrompt?: string | null
  onPromptConsumed?: () => void
  onSaveAsPreset?: (prompt: string) => void
}) {
  const [prompt, setPrompt] = useState('')
  const [repoUrl, setRepoUrl] = useState('')
  const [branch, setBranch] = useState('main')
  const [model, setModel] = useState(getPreferredModel)
  const [showImproveModal, setShowImproveModal] = useState(false)

  // Accept externally injected prompt (from sidebar preset click)
  useEffect(() => {
    if (injectedPrompt != null) {
      setPrompt(injectedPrompt)
      onPromptConsumed?.()
    }
  }, [injectedPrompt, onPromptConsumed])

  const handleAcceptImproved = useCallback((improved: string) => {
    setPrompt(improved)
    setShowImproveModal(false)
  }, [])

  const handleDeclineImproved = useCallback(() => {
    setShowImproveModal(false)
  }, [])

  const hasPrompt = prompt.trim().length > 0
  const canSubmit = hasPrompt && repoUrl.trim().length > 0

  // Quick-preset chips (show up to 3 most recent)
  const quickPresets = presets.slice(0, 3)

  return (
    <div className="relative rounded-xl border border-violet-500/20 bg-gradient-to-b from-violet-500/[0.03] to-transparent p-[1px]">
      <div className="rounded-[11px] bg-card p-5 space-y-4">
        {quickPresets.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {quickPresets.map((p) => (
              <button
                key={p.id}
                type="button"
                onClick={() => setPrompt(p.prompt)}
                className="rounded-full border border-zinc-700 bg-zinc-800/60 px-3 py-1 text-xs text-zinc-300 hover:border-violet-500/40 hover:text-violet-300 transition-colors truncate max-w-[200px]"
                title={p.prompt}
              >
                {p.title}
              </button>
            ))}
          </div>
        )}

        <div>
          <textarea
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="Describe what you want to do..."
            rows={4}
            className="w-full resize-none rounded-lg border border-zinc-700/60 bg-zinc-900/60 px-4 py-3 text-sm text-zinc-100 placeholder:text-zinc-500 focus:border-violet-500/40 focus:outline-none focus:ring-1 focus:ring-violet-500/20"
          />
          {hasPrompt && onSaveAsPreset && (
            <button
              type="button"
              onClick={() => onSaveAsPreset(prompt)}
              className="mt-1 text-[11px] text-muted-foreground hover:text-violet-400 transition-colors"
            >
              Save as preset
            </button>
          )}
        </div>

        <div className="flex flex-wrap items-end gap-3">
          <RepoCombobox
            value={repoUrl}
            onChange={setRepoUrl}
            savedRepos={savedRepos}
            onSaveRepo={onSaveRepo}
          />

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
            <Button
              variant="outline"
              size="default"
              disabled={!hasPrompt}
              onClick={() => setShowImproveModal(true)}
              className="gap-1.5 text-muted-foreground"
            >
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

      {showImproveModal && (
        <PromptImproveModal
          original={prompt}
          onAccept={handleAcceptImproved}
          onDecline={handleDeclineImproved}
        />
      )}
    </div>
  )
}

/* ─── Save Preset Modal ─── */

function SavePresetModal({
  defaultPrompt,
  onSave,
  onClose,
  isSaving,
}: {
  defaultPrompt: string
  onSave: (title: string, scope: 'personal' | 'team') => void
  onClose: () => void
  isSaving: boolean
}) {
  const [title, setTitle] = useState('')
  const [scope, setScope] = useState<'personal' | 'team'>('personal')

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={onClose}>
      <div
        className="w-full max-w-md rounded-lg border border-zinc-700 bg-zinc-900 p-6 space-y-4 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-semibold">Save as Preset</h3>
          <button onClick={onClose} className="text-zinc-500 hover:text-zinc-300">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="space-y-3">
          <div>
            <label className="mb-1 block text-xs text-muted-foreground">Title</label>
            <input
              type="text"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="e.g. Fix TypeScript errors"
              autoFocus
              className="w-full rounded-md border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-200 placeholder:text-zinc-500 focus:border-violet-500/40 focus:outline-none focus:ring-1 focus:ring-violet-500/20"
            />
          </div>

          <div>
            <label className="mb-1 block text-xs text-muted-foreground">Prompt</label>
            <p className="rounded-md border border-zinc-700/50 bg-zinc-800/50 px-3 py-2 text-xs text-zinc-400 line-clamp-3">
              {defaultPrompt}
            </p>
          </div>

          <div>
            <label className="mb-1 block text-xs text-muted-foreground">Scope</label>
            <div className="flex gap-2">
              {([{ label: 'Personal', value: 'personal' }, { label: 'Team', value: 'team' }] as const).map((opt) => (
                <button
                  key={opt.value}
                  type="button"
                  onClick={() => setScope(opt.value)}
                  className={cn(
                    'rounded-md border px-3 py-1.5 text-xs transition-colors',
                    scope === opt.value
                      ? 'border-violet-500/40 bg-violet-500/10 text-violet-300'
                      : 'border-zinc-700 text-zinc-400 hover:border-zinc-600',
                  )}
                >
                  {opt.label}
                </button>
              ))}
            </div>
          </div>
        </div>

        <div className="flex justify-end gap-2">
          <Button variant="outline" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button
            size="sm"
            disabled={!title.trim() || isSaving}
            onClick={() => onSave(title.trim(), scope)}
            className="gap-1.5 bg-violet-600 text-white hover:bg-violet-500"
          >
            <Save className="h-3 w-3" />
            {isSaving ? 'Saving...' : 'Save'}
          </Button>
        </div>
      </div>
    </div>
  )
}

/* ─── Presets Sidebar ─── */

function PresetsSidebar({
  presets,
  currentUserId,
  onSelect,
  onDelete,
}: {
  presets: Preset[]
  currentUserId: string | undefined
  onSelect: (preset: Preset) => void
  onDelete: (id: string) => void
}) {
  const personal = useMemo(() => presets.filter((p) => p.scope === 'personal'), [presets])
  const team = useMemo(() => presets.filter((p) => p.scope === 'team'), [presets])

  if (presets.length === 0) return null

  return (
    <aside className="space-y-4">
      <h3 className="text-sm font-semibold flex items-center gap-1.5">
        <Bookmark className="h-3.5 w-3.5 text-violet-400" />
        Presets
      </h3>
      {personal.length > 0 && (
        <PresetGroup label="My Presets" presets={personal} currentUserId={currentUserId} onSelect={onSelect} onDelete={onDelete} />
      )}
      {team.length > 0 && (
        <PresetGroup label="Team Presets" presets={team} currentUserId={currentUserId} onSelect={onSelect} onDelete={onDelete} />
      )}
    </aside>
  )
}

function PresetGroup({
  label,
  presets,
  currentUserId,
  onSelect,
  onDelete,
}: {
  label: string
  presets: Preset[]
  currentUserId: string | undefined
  onSelect: (preset: Preset) => void
  onDelete: (id: string) => void
}) {
  return (
    <div className="space-y-1.5">
      <p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</p>
      {presets.map((p) => (
        <div
          key={p.id}
          className="group flex items-center gap-2 rounded-md border border-transparent px-2.5 py-1.5 text-sm hover:border-zinc-700 hover:bg-zinc-800/50 cursor-pointer transition-colors"
          onClick={() => onSelect(p)}
        >
          <span className="flex-1 truncate text-xs text-zinc-300">{p.title}</span>
          {currentUserId && p.created_by === currentUserId && (
            <button
              onClick={(e) => {
                e.stopPropagation()
                onDelete(p.id)
              }}
              className="hidden text-zinc-600 hover:text-red-400 group-hover:block"
            >
              <X className="h-3 w-3" />
            </button>
          )}
        </div>
      ))}
    </div>
  )
}

/* ─── Template Grid ─── */

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
  const display = useMemo(
    () => workflows.slice().sort((a, b) => a.title.localeCompare(b.title)).slice(0, 6),
    [workflows],
  )

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

/* ─── Recent Tasks ─── */

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

/* ─── Home Page ─── */

export function HomePage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [savePresetPrompt, setSavePresetPrompt] = useState<string | null>(null)
  const [pendingPresetPrompt, setPendingPresetPrompt] = useState<string | null>(null)

  const { data: workflowData } = useQuery({
    queryKey: ['workflows'],
    queryFn: () => api.listWorkflows(),
  })

  const { data: runData } = useQuery({
    queryKey: ['my-runs'],
    queryFn: () => api.listMyRuns(10),
  })

  const { data: presetData } = useQuery({
    queryKey: ['presets'],
    queryFn: () => api.listPresets(),
  })

  const { data: repoData } = useQuery({
    queryKey: ['saved-repos'],
    queryFn: () => api.listSavedRepos(),
  })

  const { data: meData } = useQuery({
    queryKey: ['me'],
    queryFn: () => api.getMe(),
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

  const savePreset = useMutation({
    mutationFn: ({ title, prompt, scope }: { title: string; prompt: string; scope: 'personal' | 'team' }) =>
      api.createPreset(title, prompt, scope),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['presets'] })
      setSavePresetPrompt(null)
    },
  })

  const deletePreset = useMutation({
    mutationFn: (id: string) => api.deletePreset(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['presets'] }),
  })

  const saveRepo = useMutation({
    mutationFn: (url: string) => api.createSavedRepo(url),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['saved-repos'] }),
  })

  const handlePromptConsumed = useCallback(() => setPendingPresetPrompt(null), [])
  const handleSaveAsPreset = useCallback((prompt: string) => setSavePresetPrompt(prompt), [])

  function handleSubmit(prompt: string, repoUrl: string, branch: string, model: string) {
    createRun.mutate({
      workflowId: 'quick-run',
      parameters: { prompt, repo_url: repoUrl, branch },
      model: model || undefined,
    })
  }

  const workflows = workflowData?.items ?? []
  const runs = runData?.items ?? []
  const presets = presetData?.items ?? []
  const savedRepos = repoData?.items ?? []
  const currentUserId = meData?.user_id

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-bold">Fleetlift</h1>
        <p className="text-sm text-muted-foreground mt-1">Run AI workflows on your codebase</p>
      </div>

      {(savePreset.isError || deletePreset.isError || saveRepo.isError) && (
        <p className="text-sm text-red-400">
          {savePreset.error?.message ?? deletePreset.error?.message ?? saveRepo.error?.message}{' '}
          <button className="underline" onClick={() => { savePreset.reset(); deletePreset.reset(); saveRepo.reset() }}>Dismiss</button>
        </p>
      )}

      <div className="flex gap-6">
        <div className="flex-1 min-w-0 space-y-4">
          <PromptZone
            onSubmit={handleSubmit}
            isSubmitting={createRun.isPending}
            error={createRun.isError ? createRun.error.message : undefined}
            savedRepos={savedRepos}
            onSaveRepo={(url) => saveRepo.mutate(url)}
            presets={presets}
            injectedPrompt={pendingPresetPrompt}
            onPromptConsumed={handlePromptConsumed}
            onSaveAsPreset={handleSaveAsPreset}
          />
        </div>

        {presets.length > 0 && (
          <div className="hidden lg:block w-56 shrink-0">
            <PresetsSidebar
              presets={presets}
              currentUserId={currentUserId}
              onSelect={(p) => setPendingPresetPrompt(p.prompt)}
              onDelete={(id) => deletePreset.mutate(id)}
            />
          </div>
        )}
      </div>

      {retryRun.isError && (
        <p className="text-sm text-red-400">Retry failed: {retryRun.error.message}</p>
      )}

      <div className="grid gap-8 lg:grid-cols-[1fr_360px]">
        <TemplateGrid workflows={workflows} />
        <RecentTasks runs={runs} onRetry={(run) => retryRun.mutate(run)} isRetrying={retryRun.isPending} />
      </div>

      {savePresetPrompt !== null && (
        <SavePresetModal
          defaultPrompt={savePresetPrompt}
          onSave={(title, scope) => savePreset.mutate({ title, prompt: savePresetPrompt, scope })}
          onClose={() => setSavePresetPrompt(null)}
          isSaving={savePreset.isPending}
        />
      )}
    </div>
  )
}
