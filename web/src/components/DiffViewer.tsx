import { useQuery } from '@tanstack/react-query'
import ReactDiffViewer, { DiffMethod } from 'react-diff-viewer-continued'
import { api } from '@/api/client'
import type { DiffOutput, FileDiff } from '@/api/types'

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

function FileSection({ file }: { file: FileDiff }) {
  return (
    <details className="mb-2 border rounded overflow-hidden" open>
      <summary className="px-3 py-2 text-sm font-mono cursor-pointer bg-muted/30 hover:bg-muted/50 flex items-center gap-3">
        <span className="flex-1">{file.path}</span>
        <span className="text-xs text-green-600">+{file.additions}</span>
        <span className="text-xs text-red-600">-{file.deletions}</span>
      </summary>
      <div className="text-xs overflow-auto">
        <ReactDiffViewer
          oldValue={parseOldContent(file.diff)}
          newValue={parseNewContent(file.diff)}
          splitView={false}
          compareMethod={DiffMethod.LINES}
        />
      </div>
    </details>
  )
}

function RepoSection({ diff }: { diff: DiffOutput }) {
  return (
    <div className="mb-6">
      <div className="flex items-center gap-2 mb-2">
        <h3 className="font-mono text-sm font-medium">{diff.repository}</h3>
        <span className="text-xs text-muted-foreground">
          {diff.files.length} file{diff.files.length !== 1 ? 's' : ''} · {diff.total_lines} lines
        </span>
        {diff.truncated && <span className="text-xs text-yellow-600">(truncated)</span>}
      </div>
      {diff.files.map((file) => <FileSection key={file.path} file={file} />)}
    </div>
  )
}

export function DiffViewer({ workflowId }: { workflowId: string }) {
  const { data, isLoading, error } = useQuery({
    queryKey: ['diff', workflowId],
    queryFn: () => api.getDiff(workflowId),
    refetchInterval: 15_000,
  })

  if (isLoading) return <p className="text-sm text-muted-foreground">Loading diffs...</p>
  if (error)     return <p className="text-sm text-destructive">Failed to load diffs</p>
  if (!data?.diffs?.length) return <p className="text-sm text-muted-foreground">No changes yet.</p>

  return <div>{data.diffs.map((d) => <RepoSection key={d.repository} diff={d} />)}</div>
}
