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
  // Separate direct step_runs from fan-out children (e.g. "assess-0", "assess-1").
  // Fan-out children have a step_id of the form "<base>-<N>" where <base> matches a StepDef id.
  const stepDefIds = useMemo(() => new Set(steps.map(s => s.id)), [steps])

  const { stepRunMap, fanOutMap } = useMemo(() => {
    const direct = new Map<string, StepRun>()
    const fanOut = new Map<string, StepRun[]>() // baseId -> sorted children
    for (const sr of stepRuns) {
      const match = sr.step_id.match(/^(.+)-(\d+)$/)
      if (match && stepDefIds.has(match[1])) {
        const base = match[1]
        if (!fanOut.has(base)) fanOut.set(base, [])
        fanOut.get(base)!.push(sr)
      } else {
        direct.set(sr.step_id, sr)
      }
    }
    for (const runs of fanOut.values()) {
      runs.sort((a, b) => {
        const ai = parseInt(a.step_id.split('-').pop() ?? '0', 10)
        const bi = parseInt(b.step_id.split('-').pop() ?? '0', 10)
        return ai - bi
      })
    }
    return { stepRunMap: direct, fanOutMap: fanOut }
  }, [stepRuns, stepDefIds])

  const { nodes, edges, graphHeight } = useMemo(() => {
    const levels = computeLevels(steps)
    const maxLevel = Math.max(0, ...levels.values())

    // Build the expanded node list: fan-out steps produce N nodes, others produce 1.
    // nodeIds(stepId) returns the list of graph node IDs for a given StepDef id.
    const nodeIdsFor = (stepId: string): string[] => {
      const runs = fanOutMap.get(stepId)
      return runs ? runs.map(r => r.step_id) : [stepId]
    }

    // Collect all graph nodes grouped by level for layout.
    const levelNodes = new Map<number, string[]>() // level -> nodeIds
    for (const step of steps) {
      const lvl = levels.get(step.id) ?? 0
      if (!levelNodes.has(lvl)) levelNodes.set(lvl, [])
      levelNodes.get(lvl)!.push(...nodeIdsFor(step.id))
    }

    const totalLevelNodes = Math.max(...Array.from(levelNodes.values()).map(ids => ids.length))
    const height = Math.min(600, (maxLevel + 1) * 140 + 60 + Math.max(0, totalLevelNodes - 3) * 10)

    const n: Node[] = []
    const e: Edge[] = []
    const NODE_W = 150
    const NODE_H = 44
    const GAP_X = 12
    const GAP_Y = 80

    for (const step of steps) {
      const level = levels.get(step.id) ?? 0
      const fanRuns = fanOutMap.get(step.id)
      const siblingsAtLevel = levelNodes.get(level) ?? []
      const totalWidth = siblingsAtLevel.length * NODE_W + (siblingsAtLevel.length - 1) * GAP_X
      const offsetX = (800 - totalWidth) / 2

      if (fanRuns) {
        // Expand fan-out: one node per repo run
        fanRuns.forEach((sr) => {
          const col = siblingsAtLevel.indexOf(sr.step_id)
          const status: StepStatus = (sr.status as StepStatus) ?? 'pending'
          const idx = fanRuns.indexOf(sr)
          n.push({
            id: sr.step_id,
            type: 'dagNode',
            position: { x: offsetX + col * (NODE_W + GAP_X), y: level * (NODE_H + GAP_Y) + 20 },
            data: {
              label: `${step.title || step.id} (${idx + 1}/${fanRuns.length})`,
              status,
              mode: step.mode,
              startedAt: sr.started_at,
              completedAt: sr.completed_at,
              selected: selectedStepId === step.id || selectedStepId === sr.step_id,
            } satisfies DAGNodeData,
            sourcePosition: Position.Bottom,
            targetPosition: Position.Top,
          })
        })
      } else {
        const sr = stepRunMap.get(step.id)
        const status: StepStatus = (sr?.status as StepStatus) ?? 'pending'
        const col = siblingsAtLevel.indexOf(step.id)
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
      }

      // Edges: for each depends_on, connect from all source nodes to all target nodes.
      for (const dep of step.depends_on ?? []) {
        const sourceIds = nodeIdsFor(dep)
        const targetIds = nodeIdsFor(step.id)
        for (const src of sourceIds) {
          for (const tgt of targetIds) {
            const srcSr = fanOutMap.get(dep)?.find(r => r.step_id === src) ?? stepRunMap.get(dep)
            const depStatus = srcSr?.status ?? 'pending'
            const isActive = depStatus === 'running' || depStatus === 'cloning' || depStatus === 'verifying'
            const isComplete = depStatus === 'complete'
            e.push({
              id: `${src}->${tgt}`,
              source: src,
              target: tgt,
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
      }
    }

    return { nodes: n, edges: e, graphHeight: height }
  }, [steps, stepRunMap, fanOutMap, selectedStepId])

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
        fitViewOptions={{ padding: 0.1, maxZoom: 1.0 }}
        proOptions={{ hideAttribution: true }}
        nodesDraggable={false}
        nodesConnectable={false}
        minZoom={0.3}
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
