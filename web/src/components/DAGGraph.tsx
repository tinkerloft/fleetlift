import { useCallback, useMemo } from 'react'
import {
  ReactFlow,
  Controls,
  type Node,
  type Edge,
  type NodeTypes,
  Position,
  MarkerType,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import type { StepDef, StepRun, StepStatus } from '@/api/types'
import { DAGNode, type DAGNodeData } from './DAGNode'

const nodeTypes: NodeTypes = { dagNode: DAGNode }

interface DAGGraphProps {
  steps: StepDef[]
  stepRuns: StepRun[]
  onSelectStep?: (stepId: string) => void
  selectedStepId?: string
}

export function DAGGraph({ steps, stepRuns, onSelectStep, selectedStepId }: DAGGraphProps) {
  const stepRunMap = useMemo(() => {
    const map = new Map<string, StepRun>()
    for (const sr of stepRuns) map.set(sr.step_id, sr)
    return map
  }, [stepRuns])

  const { nodes, edges, graphHeight } = useMemo(() => {
    const levels = computeLevels(steps)
    const maxLevel = Math.max(0, ...levels.values())
    const height = Math.min(400, (maxLevel + 1) * 140 + 60)

    // Center nodes at each level
    const levelGroups = new Map<number, StepDef[]>()
    for (const step of steps) {
      const lvl = levels.get(step.id) ?? 0
      if (!levelGroups.has(lvl)) levelGroups.set(lvl, [])
      levelGroups.get(lvl)!.push(step)
    }

    const n: Node[] = []
    const e: Edge[] = []
    const NODE_W = 210
    const NODE_H = 60
    const GAP_X = 30
    const GAP_Y = 100

    for (const step of steps) {
      const sr = stepRunMap.get(step.id)
      const status: StepStatus = (sr?.status as StepStatus) ?? 'pending'
      const level = levels.get(step.id) ?? 0
      const siblings = levelGroups.get(level) ?? [step]
      const col = siblings.indexOf(step)
      const totalWidth = siblings.length * NODE_W + (siblings.length - 1) * GAP_X
      const offsetX = (800 - totalWidth) / 2

      n.push({
        id: step.id,
        type: 'dagNode',
        position: { x: offsetX + col * (NODE_W + GAP_X), y: level * (NODE_H + GAP_Y) + 20 },
        data: {
          label: step.title || step.id,
          status,
          mode: step.mode,
          startedAt: sr?.started_at,
          completedAt: sr?.completed_at,
          selected: selectedStepId === step.id,
        } satisfies DAGNodeData,
        sourcePosition: Position.Bottom,
        targetPosition: Position.Top,
      })

      for (const dep of step.depends_on ?? []) {
        const depSr = stepRunMap.get(dep)
        const depStatus = depSr?.status ?? 'pending'
        const isActive = depStatus === 'running' || depStatus === 'cloning' || depStatus === 'verifying'
        const isComplete = depStatus === 'complete'

        e.push({
          id: `${dep}->${step.id}`,
          source: dep,
          target: step.id,
          type: 'smoothstep',
          animated: isActive,
          style: {
            stroke: isComplete ? 'hsl(142 71% 45%)' : isActive ? 'hsl(221 83% 53%)' : 'hsl(220 13% 82%)',
            strokeWidth: 2,
            opacity: isComplete ? 0.5 : 1,
            ...(isActive ? { strokeDasharray: '8 4' } : {}),
          },
          markerEnd: { type: MarkerType.ArrowClosed, width: 12, height: 12 },
        })
      }
    }

    return { nodes: n, edges: e, graphHeight: height }
  }, [steps, stepRunMap, selectedStepId])

  const onNodeClick = useCallback((_: React.MouseEvent, node: Node) => {
    onSelectStep?.(node.id)
  }, [onSelectStep])

  return (
    <div style={{ width: '100%', height: graphHeight }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodeClick={onNodeClick}
        fitView
        proOptions={{ hideAttribution: true }}
        nodesDraggable={false}
        nodesConnectable={false}
        minZoom={0.5}
        maxZoom={1.5}
      >
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

  for (const step of steps) visit(step.id)
  return levels
}
