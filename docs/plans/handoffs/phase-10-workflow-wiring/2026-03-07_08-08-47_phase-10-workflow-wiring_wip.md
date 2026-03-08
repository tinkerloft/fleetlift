---
date: 2026-03-07T08:08:47+0000
researcher: Claude Sonnet 4.6
git_commit: d0bd825
branch: feat/agentbox-split
repository: fleetlift
topic: "Phase 10: Knowledge Workflow Wiring"
tags: [implementation, knowledge, continual-learning, temporal, workflow, transform_v2]
status: in_progress
last_updated: 2026-03-07
last_updated_by: Claude Sonnet 4.6
type: implementation_strategy
---

# Handoff: Phase 10 Knowledge Workflow Wiring — In Progress

## Task(s)

Wiring the already-implemented `EnrichPrompt` and `CaptureKnowledge` knowledge activities into the `TransformV2` workflow — the last remaining piece of Phase 10.

**Plan document:** `docs/plans/2026-03-06-phase-10-workflow-wiring.md` — read this for full task specs with exact code.

| # | Task | Status |
|---|------|--------|
| 1 | Add knowledge activity mocks and update existing tests | ✅ Complete (`d0bd825`) |
| 2 | Wire EnrichPrompt before manifest submission (TDD) | 🔴 Not started — next |
| 3 | Wire CaptureKnowledge after approval (TDD) | 🔴 Not started |
| 4 | Final verification + docs update | 🔴 Not started |

## Critical References

- **Plan:** `docs/plans/2026-03-06-phase-10-workflow-wiring.md` — full task specs with exact code for Tasks 2–4
- **Workflow to modify:** `internal/workflow/transform_v2.go`
- **Test file to modify:** `internal/workflow/transform_v2_test.go`

## Recent Changes

- `internal/workflow/transform_v2_test.go` — added `EnrichPrompt` and `CaptureKnowledge` mock methods to `AgentMockActivities`; registered both in all 6 `TestTransformV2_*` test functions with `.Maybe()` passthrough expectations (`d0bd825`)

## Learnings

- **What's already done (Phase 10a):** `internal/activity/knowledge.go` has `CaptureKnowledge` + `EnrichPrompt` fully implemented; `internal/knowledge/store.go` has YAML persistence; `cmd/cli/knowledge.go` has `list/show/add/delete` CLI; both activities registered in `cmd/worker/main.go`. The ONLY gap is calling them from the workflow.

- **EnrichPrompt injection point:** Insert between `status = model.TaskStatusRunning` (line ~154) and `manifest := activity.BuildManifest(task)` (line ~155) in `transform_v2.go`. Modifying `task.Execution.Agentic.Prompt` before `BuildManifest` works because `task` is a value but `Agentic` is a `*AgenticExecution` pointer — the modification is safe and flows through to the manifest. Store `originalPrompt` before enrichment for use in `CaptureKnowledge` input.

- **CaptureKnowledge injection point:** After the closing `}` of `if task.RequireApproval {` block (around line 325), before `return buildTaskResultFromAgent(...)`. Guard: `task.KnowledgeCaptureEnabled() && len(steeringState.History) > 0`.

- **Non-blocking pattern:** Both activities use `StartToCloseTimeout: 30 * time.Second, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1}`. On error, log warning and continue — never fail the workflow.

- **`Return("", nil)` for EnrichPrompt passthrough:** The implementation guards `if enrichedPrompt != ""` before updating the prompt. So returning `""` from a mock means "use original prompt" — this is the correct passthrough pattern for tests that don't test knowledge enrichment.

- **`originalPrompt` variable:** Declared in the EnrichPrompt block. It must be in scope when CaptureKnowledge is called (after the RequireApproval block). Declaring it at the top of the function (before the manifest block) keeps it in scope.

- **Task helpers on `model.Task`:** `KnowledgeCaptureEnabled()`, `KnowledgeEnrichEnabled()`, `KnowledgeMaxItems()`, `KnowledgeTags()`, `UsesTransformationRepo()`, `GetEffectiveRepositories()` — all exist at `internal/model/task.go:634+`.

- **Temporal test environment activity naming:** Registering `env.RegisterActivity(mockActivities.EnrichPrompt)` works because the short function name `"EnrichPrompt"` matches `activity.ActivityEnrichPrompt = "EnrichPrompt"` which is how the workflow calls it.

## Artifacts

- `docs/plans/2026-03-06-phase-10-workflow-wiring.md` — implementation plan (source of truth for Tasks 2–4)
- `docs/plans/ROADMAP.md` — Phase 10b section (update when wiring complete)
- `internal/workflow/transform_v2_test.go` — modified (mock struct + all test registrations)
- `internal/workflow/transform_v2.go` — to be modified in Tasks 2 & 3
- `internal/activity/knowledge.go` — activity implementations (reference only)
- `internal/activity/constants.go:48-50` — `ActivityCaptureKnowledge`, `ActivityEnrichPrompt` constants

## Action Items & Next Steps

The next agent should pick up from **Task 2** in `docs/plans/2026-03-06-phase-10-workflow-wiring.md`.

### Task 2: Wire EnrichPrompt (TDD)

Write `TestTransformV2_EnrichPrompt_EnrichesManifest` (full code in plan), run to confirm it fails, then implement in `transform_v2.go`:
- After `status = model.TaskStatusRunning`, store `originalPrompt` from `task.Execution.Agentic.Prompt`
- If `task.KnowledgeEnrichEnabled() && task.Execution.Agentic != nil`, call `ActivityEnrichPrompt` with `MaximumAttempts: 1`
- If enriched prompt non-empty, update `task.Execution.Agentic.Prompt` before `BuildManifest`
- Run all workflow tests. Commit.

### Task 3: Wire CaptureKnowledge (TDD)

Write `TestTransformV2_CaptureKnowledge_CalledAfterSteering` and `TestTransformV2_CaptureKnowledge_SkippedWithoutSteering` (full code in plan), run to confirm they fail, then implement:
- After closing `}` of `if task.RequireApproval {`, before `return buildTaskResultFromAgent(...)`
- Guard: `task.KnowledgeCaptureEnabled() && len(steeringState.History) > 0`
- Use `originalPrompt` (declared in Task 2's block) for `CaptureKnowledgeInput.OriginalPrompt`
- Build `repoNames` from `task.GetEffectiveRepositories()`
- Run all workflow tests. Commit.

### Task 4: Final verification

```bash
go test ./... && make lint && go build ./...
```

Update `docs/plans/IMPLEMENTATION_PLAN.md` Phase 10 section to mark wiring complete. Update `docs/plans/ROADMAP.md` Phase 10b section note. Commit.

## Other Notes

- **Branch state:** `feat/agentbox-split` — this branch completed the full agentbox split (Phases AB-1 through AB-5) and now also has the Phase 10 knowledge wiring. The branch includes major changes to `internal/workflow/transform_v2.go` from the agentbox split; the base workflow structure is correct.
- **`transform.go` (77 lines):** `Transform` function just delegates to `TransformV2`. No changes needed there.
- **`transform_group.go`:** Invokes child `TransformV2` workflows per group — knowledge wiring happens inside each child, so no changes needed.
- **Existing tests that test steering (steer+approve) will now automatically exercise CaptureKnowledge** once Task 3 is implemented — the `.Maybe()` expectations in Task 1 handle this gracefully. The specific new tests in Task 3 verify the exact inputs.
- **`import "go.temporal.io/sdk/temporal"`** is already present in `transform_v2.go:7` — no new import needed for `temporal.RetryPolicy`.
