---
name: DAG fan-out visualization direction
description: Agreed design direction for rendering large fan-out steps in the DAG graph
type: project
---

Agreed approach: threshold-based hybrid (option 3).

- N ≤ 6 fan-out instances: expand as individual nodes (current behavior after recent fix)
- N > 6: collapse to a single aggregated node showing `Step title (N repos)` with aggregate status

**Why:** The DAG communicates workflow *structure*, not per-repo detail. At 100 repos, individual nodes are unusable (21m of canvas). The step list panel is already the right place for per-repo drill-down.

**Aggregated node design:**
- Label: `{step title} ({N} repos)`
- Small inline progress bar: `4 / 5 complete`
- Color = worst status (grey=pending, blue=running, green=all-complete, red=any-failed)
- Click → filters step list panel to that step's fan-out instances (no DAG expansion)

**How to apply:** When implementing, update `DAGGraph.tsx` — detect fan-out via the existing `fanOutMap`, apply threshold (const `FAN_OUT_COLLAPSE_THRESHOLD = 6`), render collapsed node using a new `DAGNodeData` variant with `fanOutCount` and `fanOutStatus` breakdown fields.
