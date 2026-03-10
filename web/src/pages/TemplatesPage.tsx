import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { Template } from '@/api/types'
import { FileCode, Search, Loader2, AlertCircle, ArrowRight } from 'lucide-react'
import { cn, formatTemplateName, TRANSFORM_KEYWORDS } from '@/lib/utils'

type ModeFilter = 'all' | 'transform' | 'report'

export function TemplatesPage() {
  const navigate = useNavigate()
  const [search, setSearch] = useState('')
  const [modeFilter, setModeFilter] = useState<ModeFilter>('all')
  const [selected, setSelected] = useState<Template | null>(null)
  const [loadingPreview, setLoadingPreview] = useState(false)
  const [previewError, setPreviewError] = useState<string | null>(null)

  const { data, isLoading, error } = useQuery({
    queryKey: ['templates'],
    queryFn: () => api.listTemplates(),
  })

  const templates = data?.templates ?? []

  const filtered = templates.filter((t) => {
    const matchesSearch =
      search === '' ||
      t.name.toLowerCase().includes(search.toLowerCase()) ||
      t.description.toLowerCase().includes(search.toLowerCase())
    const matchesMode =
      modeFilter === 'all' ||
      (modeFilter === 'transform'
        ? TRANSFORM_KEYWORDS.some((k) => t.description.toLowerCase().includes(k))
        : t.description.toLowerCase().includes(modeFilter))
    return matchesSearch && matchesMode
  })

  const handleSelect = async (t: Template) => {
    if (selected?.name === t.name) return
    setSelected(t)
    setPreviewError(null)
    setLoadingPreview(false)
    if (!t.content) {
      setLoadingPreview(true)
      const targetName = t.name
      try {
        const full = await api.getTemplate(t.name)
        setSelected((current) => current?.name === targetName ? full : current)
      } catch {
        setPreviewError('Failed to load template content.')
      } finally {
        setLoadingPreview(false)
      }
    }
  }

  const handleUse = () => {
    if (!selected) return
    navigate(`/create?template=${encodeURIComponent(selected.name)}`)
  }

  return (
    <div className="flex h-[calc(100vh-3rem)] gap-0 -mx-8 -my-6">
      {/* Left panel */}
      <div className="flex w-96 shrink-0 flex-col border-r">
        {/* Header */}
        <div className="border-b px-4 py-4">
          <h1 className="text-base font-semibold">Templates</h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            Start from a pre-built configuration
          </p>
        </div>

        {/* Search */}
        <div className="px-4 pt-3 pb-2">
          <div className="relative">
            <Search className="absolute left-2.5 top-2.5 h-3.5 w-3.5 text-muted-foreground" />
            <input
              type="text"
              placeholder="Search templates..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full rounded-md border bg-background pl-8 pr-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
            />
          </div>
        </div>

        {/* Mode filter */}
        <div className="flex gap-1 px-4 pb-3">
          {(['all', 'transform', 'report'] as ModeFilter[]).map((m) => (
            <button
              key={m}
              onClick={() => setModeFilter(m)}
              className={cn(
                'rounded-md px-2.5 py-1 text-xs font-medium capitalize transition-colors',
                modeFilter === m
                  ? 'bg-primary text-primary-foreground'
                  : 'text-muted-foreground hover:bg-muted',
              )}
            >
              {m}
            </button>
          ))}
        </div>

        {/* Template list */}
        <div className="flex-1 overflow-y-auto px-3 pb-3">
          {isLoading && (
            <div className="flex items-center gap-2 py-8 justify-center text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              <span className="text-sm">Loading templates...</span>
            </div>
          )}
          {error && (
            <div className="flex items-center gap-2 py-8 justify-center text-destructive text-sm">
              <AlertCircle className="h-4 w-4" />
              Failed to load templates
            </div>
          )}
          {!isLoading && filtered.length === 0 && (
            <div className="flex flex-col items-center py-12 text-muted-foreground">
              <FileCode className="h-8 w-8 mb-2 opacity-40" />
              <p className="text-sm">No templates match your search</p>
            </div>
          )}
          <div className="space-y-1">
            {filtered.map((t) => (
              <button
                key={t.name}
                onClick={() => handleSelect(t)}
                className={cn(
                  'w-full flex items-start gap-3 rounded-lg px-3 py-3 text-left transition-colors',
                  selected?.name === t.name
                    ? 'bg-accent'
                    : 'hover:bg-muted/50',
                )}
              >
                <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-muted mt-0.5">
                  <FileCode className="h-4 w-4 text-muted-foreground" />
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium truncate">
                      {formatTemplateName(t.name)}
                    </span>
                    <ModeBadge description={t.description} />
                  </div>
                  <p className="text-xs text-muted-foreground mt-0.5 line-clamp-2">
                    {t.description}
                  </p>
                </div>
              </button>
            ))}
          </div>
        </div>
      </div>

      {/* Right panel — preview */}
      <div className="flex flex-1 flex-col">
        {selected ? (
          <>
            <div className="flex items-center justify-between border-b px-6 py-4">
              <div>
                <h2 className="text-sm font-semibold">{formatTemplateName(selected.name)}</h2>
                <p className="text-xs text-muted-foreground mt-0.5">{selected.description}</p>
              </div>
              <button
                onClick={handleUse}
                className="flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-xs font-medium text-primary-foreground hover:bg-primary/90 transition-colors"
              >
                Use template
                <ArrowRight className="h-3.5 w-3.5" />
              </button>
            </div>
            <div className="flex-1 overflow-y-auto bg-muted/30 px-6 py-4">
              {loadingPreview && (
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  <span className="text-sm">Loading...</span>
                </div>
              )}
              {previewError && (
                <div className="flex items-center gap-2 text-destructive text-sm">
                  <AlertCircle className="h-4 w-4" />
                  {previewError}
                </div>
              )}
              {!loadingPreview && !previewError && selected.content && (
                <pre className="text-xs font-mono leading-relaxed whitespace-pre-wrap text-foreground">
                  {selected.content}
                </pre>
              )}
            </div>
          </>
        ) : (
          <div className="flex flex-1 flex-col items-center justify-center text-muted-foreground">
            <FileCode className="h-12 w-12 mb-3 opacity-30" />
            <p className="text-sm">Select a template to preview</p>
          </div>
        )}
      </div>
    </div>
  )
}

function ModeBadge({ description }: { description: string }) {
  const lower = description.toLowerCase()
  if (lower.includes('report')) {
    return (
      <span className="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
        report
      </span>
    )
  }
  if (TRANSFORM_KEYWORDS.some((k) => lower.includes(k))) {
    return (
      <span className="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400">
        transform
      </span>
    )
  }
  return null
}
