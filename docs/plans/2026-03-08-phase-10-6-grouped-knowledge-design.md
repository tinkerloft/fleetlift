# Phase 10.6: Grouped Execution Orchestration Design

**Date**: 2026-03-08
**Status**: Approved

## Problem

`Transform` currently delegates directly to `TransformV2` for all tasks. When a task defines multiple groups, no parent workflow exists to run them in parallel, check failure thresholds, or aggregate group-level results. Knowledge capture already works per-group (via `TransformV2` inside `TransformGroup`), but it never runs for multi-group tasks because the orchestration is missing.

## Approach

Modify `Transform` to branch on group count:
- **1 group**: current path — direct `TransformV2` call, unchanged (preserves HITL signal handling)
- **N groups**: new orchestration loop inside `Transform`

## Orchestration Loop (N-group path)

1. Register `QueryExecutionProgress` query handler (returns `model.ExecutionProgress`)
2. Set up `continue` signal channel (for pause/resume)
3. Process groups sequentially in batches of `task.MaxParallel` (default 1 if unset)
   - Each batch runs `TransformGroup` children concurrently via `workflow.Go` + selector
   - Collect `GroupTransformResult` as each child completes
4. After each batch: calculate failure % = `failedGroups / completedGroups * 100`
5. If failure % exceeds `task.GetFailureThresholdPercent()`:
   - `action: abort` (or empty) → mark remaining groups as `skipped`, return result
   - `action: pause` → wait up to 24h for `continue` signal
     - `SkipRemaining: true` → mark remaining as skipped
     - `SkipRemaining: false` → continue processing remaining batches
     - Timeout → abort
6. Aggregate into `TaskResult`: populate `Groups []GroupResult`, flatten `Repositories` from all groups

## Signal Handling

- `continue` signal: handled at parent `Transform` level
- `approve`/`steer`/`cancel` signals: sent to child workflow ID `{task.ID}-{group.Name}` — no change needed

## Knowledge Capture

No changes needed. `TransformV2` inside each `TransformGroup` child already captures knowledge. All groups share the same knowledge pool (keyed by original task ID prefix).

## Model Types (all pre-existing)

- `model.GroupResult` — `GroupName`, `Status`, `Repositories`, `Error`
- `model.ExecutionProgress` — `TotalGroups`, `CompletedGroups`, `FailedGroups`, `FailurePercent`, `IsPaused`
- `model.ContinueSignalPayload` — `SkipRemaining bool`
- `model.FailureConfig` — `ThresholdPercent`, `Action`
- `task.GetExecutionGroups()`, `task.GetFailureThresholdPercent()`, `task.MaxParallel`

## Files Changed

- `internal/workflow/transform.go` — add orchestration logic
- `internal/workflow/transform_test.go` — add multi-group tests

## Out of Scope

- Phase 10.8 (knowledge efficacy tracking) — deferred
- Changes to `TransformGroup` or `TransformV2`
- CLI changes (signals already work; `fleetlift continue` already sends the signal)
