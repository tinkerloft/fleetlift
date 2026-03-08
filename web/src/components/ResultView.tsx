import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { TaskResult, RepositoryResult, GroupResult } from '@/api/types'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  ExternalLink, GitPullRequest, CheckCircle2, XCircle,
  FileText, Clock, AlertCircle,
} from 'lucide-react'
import { cn } from '@/lib/utils'

function formatDuration(seconds?: number): string {
  if (!seconds) return '-'
  if (seconds < 60) return `${Math.round(seconds)}s`
  const mins = Math.floor(seconds / 60)
  const secs = Math.round(seconds % 60)
  if (mins < 60) return `${mins}m ${secs}s`
  const hrs = Math.floor(mins / 60)
  return `${hrs}h ${mins % 60}m`
}

function RepoStatusIcon({ status }: { status: string }) {
  if (status === 'success') return <CheckCircle2 className="h-4 w-4 text-emerald-500" />
  if (status === 'failed') return <XCircle className="h-4 w-4 text-red-500" />
  return <Clock className="h-4 w-4 text-muted-foreground" />
}

function PRLink({ result }: { result: RepositoryResult }) {
  if (!result.pull_request) return null
  return (
    <a
      href={result.pull_request.pr_url}
      target="_blank"
      rel="noopener noreferrer"
      className="inline-flex items-center gap-1.5 text-xs text-blue-600 hover:underline"
    >
      <GitPullRequest className="h-3 w-3" />
      #{result.pull_request.pr_number}
      <ExternalLink className="h-2.5 w-2.5" />
    </a>
  )
}

function TransformResultView({ result }: { result: TaskResult }) {
  const repos = result.repositories ?? []
  const prs = repos.filter(r => r.pull_request)
  const failed = repos.filter(r => r.status === 'failed')

  return (
    <div className="space-y-6">
      {/* Summary stats */}
      <div className="grid gap-3 sm:grid-cols-3">
        <div className="rounded-lg border px-4 py-3">
          <p className="text-xs text-muted-foreground">Duration</p>
          <p className="text-lg font-semibold tabular-nums mt-0.5">
            {formatDuration(result.duration_seconds)}
          </p>
        </div>
        <div className="rounded-lg border px-4 py-3">
          <p className="text-xs text-muted-foreground">Pull Requests</p>
          <p className="text-lg font-semibold tabular-nums mt-0.5">{prs.length}</p>
        </div>
        <div className="rounded-lg border px-4 py-3">
          <p className="text-xs text-muted-foreground">Repositories</p>
          <p className="text-lg font-semibold tabular-nums mt-0.5">
            {repos.length}
            {failed.length > 0 && (
              <span className="text-sm text-red-500 font-normal ml-1">
                ({failed.length} failed)
              </span>
            )}
          </p>
        </div>
      </div>

      {/* Error */}
      {result.error && (
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3">
          <div className="flex items-start gap-2">
            <AlertCircle className="h-4 w-4 text-red-500 mt-0.5 shrink-0" />
            <div>
              <p className="text-sm font-medium text-red-800">Workflow Error</p>
              <p className="text-sm text-red-700 mt-1 font-mono">{result.error}</p>
            </div>
          </div>
        </div>
      )}

      {/* Per-repo results */}
      {repos.length > 0 && (
        <div className="space-y-2">
          <h3 className="text-sm font-medium">Repository Results</h3>
          {repos.map((repo) => (
            <div
              key={repo.repository}
              className={cn(
                'flex items-center gap-3 rounded-lg border px-4 py-3',
                repo.status === 'failed' && 'border-red-200 bg-red-50/30',
              )}
            >
              <RepoStatusIcon status={repo.status} />
              <div className="flex-1 min-w-0">
                <p className="text-sm font-mono font-medium truncate">{repo.repository}</p>
                {repo.files_modified && repo.files_modified.length > 0 && (
                  <p className="text-xs text-muted-foreground mt-0.5">
                    {repo.files_modified.length} file{repo.files_modified.length !== 1 ? 's' : ''} modified
                  </p>
                )}
                {repo.error && (
                  <p className="text-xs text-red-600 mt-0.5 font-mono">{repo.error}</p>
                )}
              </div>
              <PRLink result={repo} />
            </div>
          ))}
        </div>
      )}

      {/* Group results */}
      {result.groups && result.groups.length > 0 && (
        <GroupResultsView groups={result.groups} />
      )}
    </div>
  )
}

function ReportResultView({ result }: { result: TaskResult }) {
  const repos = result.repositories ?? []

  return (
    <div className="space-y-6">
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="rounded-lg border px-4 py-3">
          <p className="text-xs text-muted-foreground">Duration</p>
          <p className="text-lg font-semibold tabular-nums mt-0.5">
            {formatDuration(result.duration_seconds)}
          </p>
        </div>
        <div className="rounded-lg border px-4 py-3">
          <p className="text-xs text-muted-foreground">Reports</p>
          <p className="text-lg font-semibold tabular-nums mt-0.5">
            {repos.filter(r => r.report).length}
          </p>
        </div>
      </div>

      {result.error && (
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3">
          <div className="flex items-start gap-2">
            <AlertCircle className="h-4 w-4 text-red-500 mt-0.5 shrink-0" />
            <p className="text-sm text-red-700 font-mono">{result.error}</p>
          </div>
        </div>
      )}

      {repos.map((repo) => (
        <Card key={repo.repository}>
          <CardHeader className="pb-3">
            <div className="flex items-center gap-2">
              <RepoStatusIcon status={repo.status} />
              <CardTitle className="text-sm font-mono">{repo.repository}</CardTitle>
            </div>
          </CardHeader>
          <CardContent>
            {repo.report ? (
              <div className="space-y-3">
                {repo.report.frontmatter && Object.keys(repo.report.frontmatter).length > 0 && (
                  <div className="rounded-md border bg-muted/30 p-3">
                    <p className="text-xs font-medium text-muted-foreground mb-2">Frontmatter</p>
                    <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
                      {Object.entries(repo.report.frontmatter).map(([k, v]) => (
                        <div key={k} className="contents">
                          <dt className="text-muted-foreground">{k}</dt>
                          <dd className="font-mono">{String(v)}</dd>
                        </div>
                      ))}
                    </dl>
                  </div>
                )}
                {repo.report.body && (
                  <div className="prose prose-sm max-w-none">
                    <pre className="text-xs whitespace-pre-wrap">{repo.report.body}</pre>
                  </div>
                )}
                {repo.report.error && (
                  <p className="text-xs text-red-600 font-mono">{repo.report.error}</p>
                )}
              </div>
            ) : repo.error ? (
              <p className="text-sm text-red-600 font-mono">{repo.error}</p>
            ) : (
              <p className="text-sm text-muted-foreground">No report generated</p>
            )}
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

function GroupResultsView({ groups }: { groups: GroupResult[] }) {
  return (
    <div className="space-y-2">
      <h3 className="text-sm font-medium">Group Results</h3>
      {groups.map((group) => (
        <details key={group.group_name} className="rounded-lg border overflow-hidden">
          <summary className={cn(
            'flex items-center gap-3 px-4 py-3 cursor-pointer hover:bg-muted/30',
            group.status === 'failed' && 'bg-red-50/30',
          )}>
            <RepoStatusIcon status={group.status} />
            <span className="text-sm font-medium">{group.group_name}</span>
            <span className="text-xs text-muted-foreground ml-auto">
              {group.repositories?.length ?? 0} repo{(group.repositories?.length ?? 0) !== 1 ? 's' : ''}
            </span>
          </summary>
          {group.error && (
            <p className="px-4 py-2 text-xs text-red-600 font-mono border-t bg-red-50/30">
              {group.error}
            </p>
          )}
          {group.repositories && group.repositories.length > 0 && (
            <div className="border-t divide-y">
              {group.repositories.map(repo => (
                <div key={repo.repository} className="flex items-center gap-3 px-4 py-2.5">
                  <RepoStatusIcon status={repo.status} />
                  <span className="text-sm font-mono flex-1 truncate">{repo.repository}</span>
                  {repo.error && (
                    <span className="text-xs text-red-500 truncate max-w-[200px]">{repo.error}</span>
                  )}
                  <PRLink result={repo} />
                </div>
              ))}
            </div>
          )}
        </details>
      ))}
    </div>
  )
}

export function ResultView({ workflowId }: { workflowId: string }) {
  const { data: result, isLoading, error } = useQuery({
    queryKey: ['result', workflowId],
    queryFn: () => api.getResult(workflowId),
    retry: false,
  })

  if (isLoading) {
    return (
      <div className="space-y-3">
        <div className="h-20 rounded-lg bg-muted/30 animate-pulse" />
        <div className="h-32 rounded-lg bg-muted/30 animate-pulse" />
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex flex-col items-center py-8 text-center">
        <FileText className="h-8 w-8 text-muted-foreground/40 mb-2" />
        <p className="text-sm text-muted-foreground">
          Results not available yet. The workflow may still be running.
        </p>
      </div>
    )
  }

  if (!result) return null

  if (result.mode === 'report') {
    return <ReportResultView result={result} />
  }

  return <TransformResultView result={result} />
}
