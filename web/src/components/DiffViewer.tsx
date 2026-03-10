import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import ReactDiffViewer, { DiffMethod } from 'react-diff-viewer-continued'
import { api } from '@/api/client'
import type { DiffOutput, FileDiff } from '@/api/types'
import { Columns2, AlignLeft, ChevronsDownUp, ChevronsUpDown, Search } from 'lucide-react'
import { cn } from '@/lib/utils'

function parseOldContent(diff: string): string {
  return diff.split('\n')
    .filter((l) => !l.startsWith('+') || l.startsWith('+++'))
    .map((l) => l.startsWith('-') ? l.slice(1) : l.startsWith('@@') ? '' : l)
    .join('\n')
}

function parseNewContent(diff: string): string {
  return diff.split('\n')
    .filter((l) => !l.startsWith('-') || l.startsWith('---'))
    .map((l) => l.startsWith('+') ? l.slice(1) : l.startsWith('@@') ? '' : l)
    .join('\n')
}

function FileSection({
  file,
  splitView,
  open,
}: {
  file: FileDiff
  splitView: boolean
  open: boolean
}) {
  return (
    <details className="mb-2 border rounded overflow-hidden" open={open}>
      <summary className="px-3 py-2 text-sm font-mono cursor-pointer bg-muted/30 hover:bg-muted/50 flex items-center gap-3">
        <span className="flex-1">{file.path}</span>
        <span className="text-xs text-green-600">+{file.additions}</span>
        <span className="text-xs text-red-600">-{file.deletions}</span>
      </summary>
      <div className="text-xs overflow-auto">
        <ReactDiffViewer
          oldValue={parseOldContent(file.diff)}
          newValue={parseNewContent(file.diff)}
          splitView={splitView}
          compareMethod={DiffMethod.LINES}
        />
      </div>
    </details>
  )
}

function RepoSection({
  diff,
  splitView,
  allOpen,
  openKey,
  search,
}: {
  diff: DiffOutput
  splitView: boolean
  allOpen: boolean
  openKey: number
  search: string
}) {
  const files = search
    ? diff.files.filter(f => f.path.toLowerCase().includes(search.toLowerCase()))
    : diff.files

  if (files.length === 0) return null

  return (
    <div className="mb-6">
      <div className="flex items-center gap-2 mb-2">
        <h3 className="font-mono text-sm font-medium">{diff.repository}</h3>
        <span className="text-xs text-muted-foreground">
          {files.length} file{files.length !== 1 ? 's' : ''} · {diff.total_lines} lines
        </span>
        {diff.truncated && <span className="text-xs text-yellow-600">(truncated)</span>}
      </div>
      {files.map((file) => (
        <FileSection
          key={`${file.path}-${openKey}`}
          file={file}
          splitView={splitView}
          open={allOpen}
        />
      ))}
    </div>
  )
}

export function DiffViewer({ workflowId }: { workflowId: string }) {
  const [splitView, setSplitView] = useState(false)
  const [allOpen, setAllOpen] = useState(true)
  const [openKey, setOpenKey] = useState(0)
  const [search, setSearch] = useState('')

  const { data, isLoading, error } = useQuery({
    queryKey: ['diff', workflowId],
    queryFn: () => api.getDiff(workflowId),
    refetchInterval: 15_000,
  })

  const totalFiles = useMemo(
    () => (data?.diffs ?? []).reduce((sum, d) => sum + d.files.length, 0),
    [data],
  )

  if (isLoading) return <p className="text-sm text-muted-foreground">Loading diffs...</p>
  if (error)     return <p className="text-sm text-destructive">Failed to load diffs</p>
  if (!data?.diffs?.length) return <p className="text-sm text-muted-foreground">No changes yet.</p>

  const toggleAllOpen = () => {
    setAllOpen(v => !v)
    setOpenKey(k => k + 1) // force <details> remount to apply new open value
  }

  return (
    <div>
      {/* Toolbar */}
      <div className="flex items-center gap-2 mb-3 flex-wrap">
        <button
          onClick={() => setSplitView(v => !v)}
          className={cn(
            'flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium transition-colors border',
            splitView ? 'bg-primary text-primary-foreground border-transparent' : 'bg-background text-muted-foreground hover:bg-muted',
          )}
          title={splitView ? 'Switch to unified view' : 'Switch to split view'}
        >
          {splitView ? <AlignLeft className="h-3.5 w-3.5" /> : <Columns2 className="h-3.5 w-3.5" />}
          {splitView ? 'Unified' : 'Split'}
        </button>

        <button
          onClick={toggleAllOpen}
          className="flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium border bg-background text-muted-foreground hover:bg-muted transition-colors"
          title={allOpen ? 'Collapse all files' : 'Expand all files'}
        >
          {allOpen
            ? <ChevronsDownUp className="h-3.5 w-3.5" />
            : <ChevronsUpDown className="h-3.5 w-3.5" />}
          {allOpen ? 'Collapse all' : 'Expand all'}
        </button>

        {totalFiles >= 3 && (
          <div className="relative ml-auto">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
            <input
              type="text"
              placeholder="Filter files..."
              value={search}
              onChange={e => setSearch(e.target.value)}
              className="rounded-md border bg-background pl-8 pr-3 py-1.5 text-xs focus:outline-none focus:ring-2 focus:ring-ring"
            />
          </div>
        )}
      </div>

      {data.diffs.map((d) => (
        <RepoSection
          key={d.repository}
          diff={d}
          splitView={splitView}
          allOpen={allOpen}
          openKey={openKey}
          search={search}
        />
      ))}
    </div>
  )
}
