import { useEffect } from 'react'
import { useMutation } from '@tanstack/react-query'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { api } from '@/api/client'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { X, Loader2, RefreshCw, ArrowRight } from 'lucide-react'

type ImproveResult = {
  improved: string
  scores: Record<string, { rating: string; reason: string }>
  summary: string
}

const SCORE_COLORS: Record<string, string> = {
  excellent: 'border-emerald-500/40 bg-emerald-500/10 text-emerald-400',
  good: 'border-yellow-500/40 bg-yellow-500/10 text-yellow-400',
  poor: 'border-red-500/40 bg-red-500/10 text-red-400',
}

function ScoreBadges({ scores }: { scores: Record<string, { rating: string; reason: string }> }) {
  return (
    <div className="flex flex-wrap gap-2">
      {Object.entries(scores).map(([key, { rating, reason }]) => (
        <Badge
          key={key}
          variant="outline"
          className={SCORE_COLORS[rating] ?? 'border-zinc-600 bg-zinc-800 text-zinc-400'}
          title={reason}
        >
          {key}: {rating}
        </Badge>
      ))}
    </div>
  )
}

export function PromptImproveModal({
  original,
  onAccept,
  onDecline,
}: {
  original: string
  onAccept: (improved: string) => void
  onDecline: () => void
}) {
  const mutation = useMutation({
    mutationFn: (prompt: string) => api.improvePrompt(prompt),
  })

  useEffect(() => {
    mutation.mutate(original)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    document.body.classList.add('overflow-hidden')
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onDecline()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => {
      document.body.classList.remove('overflow-hidden')
      window.removeEventListener('keydown', handleKeyDown)
    }
  }, [onDecline])

  const data = mutation.data as ImproveResult | undefined

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm">
      <div className="relative mx-4 flex max-h-[90vh] w-full max-w-5xl flex-col rounded-xl border border-zinc-700/60 bg-zinc-900 shadow-2xl">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-zinc-700/60 px-6 py-4">
          <h2 className="text-lg font-semibold text-zinc-100">Improve Prompt</h2>
          <button
            onClick={onDecline}
            className="rounded-md p-1 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200 transition-colors"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-auto p-6">
          {mutation.isPending && (
            <div className="flex flex-col items-center justify-center py-16 gap-3">
              <Loader2 className="h-8 w-8 animate-spin text-violet-400" />
              <p className="text-sm text-muted-foreground">Analyzing and improving your prompt...</p>
            </div>
          )}

          {mutation.isError && (
            <div className="flex flex-col items-center justify-center py-16 gap-4">
              <p className="text-sm text-red-400">Failed to improve prompt: {mutation.error.message}</p>
              <Button
                variant="outline"
                size="default"
                onClick={() => mutation.mutate(original)}
                className="gap-1.5"
              >
                <RefreshCw className="h-3.5 w-3.5" />
                Retry
              </Button>
            </div>
          )}

          {mutation.isSuccess && data && (
            <div className="space-y-4">
              {/* Score bar */}
              {data.scores && (
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-xs font-medium text-muted-foreground">Your prompt:</span>
                  <ScoreBadges scores={data.scores} />
                </div>
              )}

              {/* Improved prompt */}
              <div className="rounded-lg border border-emerald-500/20 bg-emerald-500/[0.03] p-5 prose prose-sm prose-invert max-w-none">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{data.improved}</ReactMarkdown>
              </div>
            </div>
          )}
        </div>

        {/* Footer */}
        {mutation.isSuccess && data && (
          <div className="flex items-center justify-between border-t border-zinc-700/60 px-6 py-4 gap-4">
            <p className="flex-1 text-sm text-muted-foreground truncate" title={data.summary}>{data.summary}</p>
            <div className="flex items-center gap-2 shrink-0">
              <Button variant="outline" size="default" onClick={onDecline}>
                Decline
              </Button>
              <Button
                size="default"
                onClick={() => onAccept(data.improved)}
                className="gap-1.5 bg-violet-600 text-white hover:bg-violet-500"
              >
                Use improved
                <ArrowRight className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
