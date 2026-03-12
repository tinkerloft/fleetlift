import { useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import { DAGGraph } from '@/components/DAGGraph'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import type { WorkflowDef, ParameterDef } from '@/api/types'
import { parse as parseYaml } from '@/lib/yaml'

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

  const runMutation = useMutation({
    mutationFn: () => api.createRun(wf!.id, params),
    onSuccess: (run) => {
      queryClient.invalidateQueries({ queryKey: ['runs'] })
      navigate(`/runs/${run.id}`)
    },
  })

  if (isLoading || !wf) {
    return <p className="text-muted-foreground text-sm">Loading...</p>
  }

  let def: WorkflowDef | null = null
  try {
    def = parseYaml(wf.yaml_body) as unknown as WorkflowDef
  } catch {
    // YAML parse failed; show raw
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">{wf.title}</h1>
          <p className="text-sm text-muted-foreground">{wf.description}</p>
        </div>
        <div className="flex items-center gap-2">
          {wf.builtin && <Badge variant="secondary">builtin</Badge>}
          <div className="flex flex-wrap gap-1">
            {wf.tags?.map((tag) => (
              <Badge key={tag} variant="outline">{tag}</Badge>
            ))}
          </div>
        </div>
      </div>

      {def?.steps && (
        <div className="rounded-lg border p-4">
          <h2 className="mb-4 font-semibold">DAG Preview</h2>
          <DAGGraph steps={def.steps} stepRuns={[]} />
        </div>
      )}

      {def?.parameters && def.parameters.length > 0 && (
        <div className="rounded-lg border p-4 space-y-4">
          <h2 className="font-semibold">Parameters</h2>
          {def.parameters.map((p: ParameterDef) => (
            <div key={p.name} className="space-y-1">
              <label className="text-sm font-medium">
                {p.name}
                {p.required && <span className="text-red-400 ml-1">*</span>}
                <span className="ml-2 text-xs text-muted-foreground">{p.type}</span>
              </label>
              {p.description && (
                <p className="text-xs text-muted-foreground">{p.description}</p>
              )}
              <input
                type="text"
                className="w-full rounded-md border bg-background px-3 py-1.5 text-sm"
                placeholder={p.default != null ? String(p.default) : ''}
                value={params[p.name] ?? ''}
                onChange={(e) => setParams({ ...params, [p.name]: e.target.value })}
              />
            </div>
          ))}
          <Button
            onClick={() => runMutation.mutate()}
            disabled={runMutation.isPending}
          >
            {runMutation.isPending ? 'Starting...' : 'Run Workflow'}
          </Button>
          {runMutation.isError && (
            <p className="text-sm text-red-400">{runMutation.error.message}</p>
          )}
        </div>
      )}

      <details>
        <summary className="cursor-pointer text-sm text-muted-foreground">View YAML</summary>
        <pre className="mt-2 max-h-96 overflow-auto rounded-md bg-muted p-4 text-xs font-mono">
          {wf.yaml_body}
        </pre>
      </details>
    </div>
  )
}
