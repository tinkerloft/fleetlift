import { useState, useEffect } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { FileText, ChevronDown, ChevronUp, Download } from 'lucide-react'
import type { Artifact } from '@/api/types'
import { api, authHeaders } from '@/api/client'

interface ArtifactCardProps {
  artifact: Artifact
  defaultExpanded?: boolean
}

function formatSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '—'
  if (bytes === 0) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function isMarkdown(contentType: string, name: string): boolean {
  if (contentType === 'text/markdown') return true
  const lower = name.toLowerCase()
  return lower.endsWith('.md') || lower.endsWith('.markdown')
}

function isText(contentType: string): boolean {
  return contentType.startsWith('text/')
}

async function downloadArtifact(artifact: Artifact): Promise<void> {
  const res = await fetch(
    `${api.getArtifactContentUrl(artifact.id)}?download=1`,
    { headers: authHeaders() },
  )
  if (!res.ok) throw new Error(`Download failed: ${res.statusText}`)

  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = artifact.name
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

export function ArtifactCard({ artifact, defaultExpanded = false }: ArtifactCardProps) {
  const [expanded, setExpanded] = useState(defaultExpanded)
  const [content, setContent] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [downloadError, setDownloadError] = useState<string | null>(null)

  const showMarkdown = isMarkdown(artifact.content_type, artifact.name)
  const showText = !showMarkdown && isText(artifact.content_type)

  // Reset content when artifact ID changes
  useEffect(() => {
    setContent(null)
    setError(null)
    setLoading(false)
  }, [artifact.id])

  useEffect(() => {
    if (!expanded || content !== null || loading) return

    // Binary content: skip fetch
    if (!showText && !showMarkdown) return

    let cancelled = false
    setLoading(true)
    setError(null)
    fetch(api.getArtifactContentUrl(artifact.id), { headers: authHeaders() })
      .then(res => {
        if (!res.ok) throw new Error(`Failed to load content: ${res.statusText}`)
        return res.text()
      })
      .then(text => {
        if (!cancelled) {
          setContent(text)
          setLoading(false)
        }
      })
      .catch(err => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to load content')
          setLoading(false)
        }
      })

    return () => { cancelled = true }
  }, [expanded, artifact.id, showText, showMarkdown]) // eslint-disable-line react-hooks/exhaustive-deps

  function handleExpand() {
    setExpanded((v) => !v)
  }

  async function handleDownload() {
    setDownloadError(null)
    try {
      await downloadArtifact(artifact)
    } catch (e) {
      setDownloadError(e instanceof Error ? e.message : 'Download failed')
    }
  }

  return (
    <div className="rounded-lg border bg-card text-card-foreground">
      {/* Header row */}
      <div className="flex items-center gap-2 px-3 py-2">
        <FileText className="h-4 w-4 shrink-0 text-muted-foreground" />
        <span className="flex-1 truncate text-sm font-medium">{artifact.name}</span>
        <span className="shrink-0 rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
          {artifact.content_type}
        </span>
        <span className="shrink-0 text-xs text-muted-foreground">
          {formatSize(artifact.size_bytes)}
        </span>
        <button
          onClick={handleExpand}
          aria-expanded={expanded}
          className="shrink-0 rounded px-2 py-1 text-xs hover:bg-muted transition-colors"
          aria-label={expanded ? 'Collapse' : 'Expand'}
        >
          {expanded ? (
            <span className="flex items-center gap-1"><ChevronUp className="h-3 w-3" /> Collapse</span>
          ) : (
            <span className="flex items-center gap-1"><ChevronDown className="h-3 w-3" /> Expand</span>
          )}
        </button>
        <button
          onClick={handleDownload}
          className="shrink-0 rounded px-2 py-1 text-xs hover:bg-muted transition-colors"
          aria-label="Download"
        >
          <span className="flex items-center gap-1"><Download className="h-3 w-3" /> Download</span>
        </button>
      </div>

      {downloadError && (
        <div className="px-3 pb-2 text-xs text-destructive">{downloadError}</div>
      )}

      {/* Expanded content */}
      {expanded && (
        <div className="border-t">
          <div className="max-h-[600px] overflow-y-auto p-3">
            {loading && (
              <div className="text-sm text-muted-foreground">Loading...</div>
            )}
            {error && (
              <div className="text-sm text-destructive">{error}</div>
            )}
            {!loading && !error && content !== null && (
              <>
                {showMarkdown && (
                  <div className="prose prose-sm max-w-none dark:prose-invert">
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
                  </div>
                )}
                {showText && (
                  <pre className="text-sm whitespace-pre-wrap font-mono">{content}</pre>
                )}
              </>
            )}
            {!loading && !error && content === null && !showText && !showMarkdown && (
              <div className="text-sm text-muted-foreground">
                Binary content — download to view
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
