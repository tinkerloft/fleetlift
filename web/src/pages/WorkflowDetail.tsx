import { useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import CodeMirror from '@uiw/react-codemirror'
import { yaml } from '@codemirror/lang-yaml'
import { api } from '@/api/client'
import { DAGGraph } from '@/components/DAGGraph'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/Skeleton'
import { workflowCategory, CATEGORY_STYLES } from '@/lib/workflow-colors'
import { cn } from '@/lib/utils'
import { Shield, Bug, GitBranch, Search, Tag, Terminal } from 'lucide-react'
import type { WorkflowDef, ParameterDef } from '@/api/types'
import { parse as parseYaml } from '@/lib/yaml'

const ICON_MAP: Record<string, React.FC<{ className?: string }>> = {
  Shield, Bug, GitBranch, Search, Tag, Terminal,
}

function coerceParams(def: WorkflowDef, raw: Record<string, string>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const p of def.parameters ?? []) {
    const v = raw[p.name]
    if (v === undefined || v === '') continue // let server apply default
    switch (p.type) {
      case 'int': {
        const n = Number(v)
        if (!Number.isNaN(n)) out[p.name] = n
        break
      }
      case 'bool':
        out[p.name] = v === 'true' || v === '1'
        break
      default:
        out[p.name] = v
    }
  }
  return out
}

export function WorkflowDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const { data: wf, isLoading } = useQuery({
    queryKey: ['workflow', id],
    queryFn: () => api.getWorkflow(id!),
    enabled: !!id,
  })

  const [params, setParams] = useState<Record<string, string>>({})
  const [showYaml, setShowYaml] = useState(false)

  const runMutation = useMutation({
    mutationFn: () => api.createRun(wf!.id, def ? coerceParams(def, params) : params),
    onSuccess: (run) => {
      queryClient.invalidateQueries({ queryKey: ['runs'] })
      navigate(`/runs/${run.id}`)
    },
  })

  if (isLoading || !wf) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-10 w-64" />
        <Skeleton className="h-4 w-96" />
        <Skeleton className="h-64 w-full rounded-lg" />
      </div>
    )
  }

  let def: WorkflowDef | null = null
  try { def = parseYaml(wf.yaml_body) as unknown as WorkflowDef } catch { /* ignore */ }

  const cat = workflowCategory(wf.tags ?? [])
  const styles = CATEGORY_STYLES[cat.color]
  const Icon = ICON_MAP[cat.icon] ?? Terminal

  return (
    <div className="space-y-6">
      {/* Hero */}
      <div className="flex items-start gap-4">
        <div className={cn('flex h-12 w-12 items-center justify-center rounded-xl', styles.iconBg)}>
          <Icon className={cn('h-6 w-6', styles.text)} />
        </div>
        <div className="flex-1">
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-bold">{wf.title}</h1>
            {wf.builtin && <Badge variant="secondary">builtin</Badge>}
          </div>
          <p className="text-sm text-muted-foreground mt-1">{wf.description}</p>
          {def?.steps && (
            <p className="text-xs text-muted-foreground mt-1">
              {def.steps.length} steps
              {def.parameters?.length ? ` · ${def.parameters.length} parameters` : ''}
            </p>
          )}
        </div>
        <div className="flex flex-wrap gap-1">
          {wf.tags?.map(tag => <Badge key={tag} variant="outline">{tag}</Badge>)}
        </div>
      </div>

      {/* DAG Preview */}
      {def?.steps && (
        <div className="rounded-lg border bg-card p-4">
          <h2 className="mb-3 text-sm font-semibold">DAG Preview</h2>
          <DAGGraph steps={def.steps} stepRuns={[]} />
        </div>
      )}

      {/* Parameters form */}
      {def?.parameters && def.parameters.length > 0 && (
        <div className="rounded-lg border bg-card p-4 space-y-4">
          <h2 className="text-sm font-semibold">Parameters</h2>
          {def.parameters.map((p: ParameterDef) => (
            <div key={p.name} className="space-y-1">
              <label className="text-sm font-medium">
                {p.name}
                {p.required && <span className="text-red-400 ml-1">*</span>}
                <span className="ml-2 text-xs text-muted-foreground">{p.type}</span>
              </label>
              {p.description && <p className="text-xs text-muted-foreground">{p.description}</p>}
              <input
                type="text"
                className="w-full rounded-md border bg-background px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                placeholder={p.default != null ? String(p.default) : ''}
                value={params[p.name] ?? ''}
                onChange={(e) => setParams({ ...params, [p.name]: e.target.value })}
              />
            </div>
          ))}
          <Button onClick={() => runMutation.mutate()} disabled={runMutation.isPending}>
            {runMutation.isPending ? 'Starting…' : 'Run Workflow'}
          </Button>
          {runMutation.isError && <p className="text-sm text-red-400">{runMutation.error.message}</p>}
        </div>
      )}

      {/* YAML viewer */}
      <div className="rounded-lg border bg-card overflow-hidden">
        <button
          onClick={() => setShowYaml(!showYaml)}
          className="w-full flex items-center justify-between px-4 py-3 text-sm font-medium hover:bg-muted/50 transition-colors"
        >
          <span>Workflow YAML</span>
          <span className="text-muted-foreground text-xs">{showYaml ? 'hide' : 'show'}</span>
        </button>
        {showYaml && (
          <div className="border-t">
            <CodeMirror
              value={wf.yaml_body}
              extensions={[yaml()]}
              editable={false}
              basicSetup={{ lineNumbers: true, foldGutter: true }}
              className="text-sm"
              maxHeight="400px"
            />
          </div>
        )}
      </div>
    </div>
  )
}
