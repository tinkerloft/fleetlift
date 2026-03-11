import { useCallback, useMemo } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  type Node,
  type Edge,
  Position,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import type { StepDef, StepRun, StepStatus } from '@/api/types'

const STATUS_COLORS: Record<StepStatus | 'default', string> = {
  pending: '#6b7280',
  cloning: '#3b82f6',
  running: '#3b82f6',
  verifying: '#8b5cf6',
  awaiting_input: '#eab308',
  complete: '#22c55e',
  failed: '#ef4444',
  skipped: '#9ca3af',
  default: '#6b7280',
}

function statusColor(status?: StepStatus): string {
  return STATUS_COLORS[status ?? 'default'] ?? STATUS_COLORS.default
}

interface DAGGraphProps {
  steps: StepDef[]
  stepRuns: StepRun[]
  onSelectStep?: (stepId: string) => void
  selectedStepId?: string
}

export function DAGGraph({ steps, stepRuns, onSelectStep, selectedStepId }: DAGGraphProps) {
  const stepRunMap = useMemo(() => {
    const map = new Map<string, StepRun>()
    for (const sr of stepRuns) {
      map.set(sr.step_id, sr)
    }
    return map
  }, [stepRuns])

  const { nodes, edges } = useMemo(() => {
    const n: Node[] = []
    const e: Edge[] = []

    // Simple layout: assign y based on topological order, x based on column
    const levels = computeLevels(steps)

    steps.forEach((step, _i) => {
      const sr = stepRunMap.get(step.id)
      const status = sr?.status ?? 'pending'
      const level = levels.get(step.id) ?? 0
      const col = getColumnAtLevel(step.id, steps, levels)

      n.push({
        id: step.id,
        position: { x: col * 220, y: level * 120 },
        data: {
          label: step.title || step.id,
        },
        sourcePosition: Position.Bottom,
        targetPosition: Position.Top,
        style: {
          background: statusColor(status as StepStatus),
          color: '#fff',
          border: selectedStepId === step.id ? '3px solid #fff' : '1px solid rgba(255,255,255,0.3)',
          borderRadius: '8px',
          padding: '8px 16px',
          fontSize: '13px',
          fontWeight: 500,
          cursor: 'pointer',
          animation: (status === 'running' || status === 'cloning') ? 'pulse 2s infinite' : undefined,
          boxShadow: selectedStepId === step.id ? '0 0 0 2px #3b82f6' : undefined,
        },
      })

      for (const dep of step.depends_on ?? []) {
        e.push({
          id: `${dep}->${step.id}`,
          source: dep,
          target: step.id,
          animated: (stepRunMap.get(dep)?.status === 'running'),
          style: { stroke: '#6b7280' },
        })
      }
    })

    return { nodes: n, edges: e }
  }, [steps, stepRunMap, selectedStepId])

  const onNodeClick = useCallback((_: React.MouseEvent, node: Node) => {
    onSelectStep?.(node.id)
  }, [onSelectStep])

  return (
    <div style={{ width: '100%', height: 400 }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodeClick={onNodeClick}
        fitView
        proOptions={{ hideAttribution: true }}
        nodesDraggable={false}
        nodesConnectable={false}
      >
        <Background />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  )
}

function computeLevels(steps: StepDef[]): Map<string, number> {
  const levels = new Map<string, number>()
  const visited = new Set<string>()

  function visit(id: string): number {
    if (levels.has(id)) return levels.get(id)!
    if (visited.has(id)) return 0
    visited.add(id)

    const step = steps.find(s => s.id === id)
    if (!step || !step.depends_on?.length) {
      levels.set(id, 0)
      return 0
    }

    let maxDep = 0
    for (const dep of step.depends_on) {
      maxDep = Math.max(maxDep, visit(dep) + 1)
    }
    levels.set(id, maxDep)
    return maxDep
  }

  for (const step of steps) {
    visit(step.id)
  }
  return levels
}

function getColumnAtLevel(stepId: string, steps: StepDef[], levels: Map<string, number>): number {
  const level = levels.get(stepId) ?? 0
  const sameLevel = steps.filter(s => (levels.get(s.id) ?? 0) === level)
  return sameLevel.findIndex(s => s.id === stepId)
}
