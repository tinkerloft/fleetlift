---
date: 2026-03-05T22:26:57Z
researcher: Claude Sonnet 4.6
git_commit: 2d984b1
branch: feat/agentbox-split
repository: fleetlift + agentbox (two repos)
topic: "Agentbox Split — Phases 3b+3c Complete, Phase 4 Planned"
tags: [agentbox, split, sandbox, protocol, temporalkit, fleetproto, cleanup]
status: complete
last_updated: 2026-03-05
last_updated_by: Claude Sonnet 4.6
type: implementation_strategy
---

# Handoff: agentbox-split Phases 3b+3c Complete, Phase 4 Ready to Execute

## Task(s)

**Goal:** Split `github.com/tinkerloft/fleetlift` into two repos:
- `github.com/tinkerloft/agentbox` — generic agent hosting platform (`/Users/andrew/dev/code/projects/agentbox`)
- `github.com/tinkerloft/fleetlift` — refactored to import agentbox (branch `feat/agentbox-split`)

### Status

| Phase | Tasks | Status |
|-------|-------|--------|
| Phase 1: agentbox `protocol` + `sandbox` | Tasks 1–5 | ✅ Complete |
| Phase 2: agentbox `agent` + `temporalkit` | Tasks 6–8 | ✅ Complete |
| Phase 3a: fleetlift structural refactor | Tasks 9–14 | ✅ Complete |
| Phase 3b: activity/manifest/workflow/agent-binary refactor | Steps 5–9 | ✅ Complete (this session) |
| Phase 3c: Legacy workflow migration | Step 10 | ✅ Complete (this session) |
| Phase 4: Protocol shim elimination + cleanup | — | 📋 Planned, ready to execute |
| Phase 2 OpenSandbox adapter | `sandbox/opensandbox/` | ❌ Deferred (needs OpenSandbox API knowledge) |

**Working from plans:**
- `/Users/andrew/dev/code/projects/fleetlift/docs/plans/delegated-pondering-teacup.md` — full spec (source of truth)
- `/Users/andrew/dev/code/projects/fleetlift/docs/plans/2026-03-04-agentbox-split-phase2b-3.md` — Phase 2b+3 plan
- `/Users/andrew/dev/code/projects/fleetlift/docs/plans/2026-03-05-agentbox-split-phase4-cleanup.md` — **Phase 4 plan (next to execute)**

## Critical References

- `docs/plans/2026-03-05-agentbox-split-phase4-cleanup.md` — **the Phase 4 plan to execute next**
- `docs/plans/delegated-pondering-teacup.md` — full original spec
- `internal/agent/protocol/types.go` — the shim to be deleted in Phase 4

## Recent Changes

**Phase 3b (activity/workflow/agent-binary refactor) — done this session:**

- `internal/activity/manifest.go` — `BuildManifest()` now returns `fleetproto.TaskManifest` directly (not via shim)
- `internal/activity/agent.go` — `AgentActivities` now delegates to `agentbox/temporalkit.AgentActivities`; `SubmitTaskManifestInput.Manifest` is `fleetproto.TaskManifest`; `WaitForAgentPhase` returns `*agentboxproto.AgentStatus`; `ReadAgentResult` returns `*fleetproto.AgentResult`; inline polling/staleness logic removed (now in agentbox)
- `internal/activity/sandbox.go` — delegates Provision/Cleanup to `agentbox/temporalkit.SandboxActivities`; `Provider` field now `agentboxsandbox.AgentProvider` (was `Provider`)
- `internal/workflow/transform_v2.go` — imports `agentboxproto` + `fleetproto` directly instead of shim; `var agentResult *fleetproto.AgentResult`, `var agentStatus *agentboxproto.AgentStatus`
- `internal/agent/pipeline.go` — `Run()` removed; `execute()` renamed `Execute()` (exported); has `proto *agentboxagent.Protocol` field
- `internal/agent/deps.go` — added `OSFileSystem = osFileSystem` exported type alias
- `cmd/agent/main.go` — now uses `agentboxagent.New(basePath, agent.OSFileSystem{})` + `proto.WaitForManifest()` etc.; `run()` function handles signal handling + manifest wait + pipeline execution

**Phase 3c (legacy workflow migration) — done this session:**

- `internal/workflow/transform.go` — replaced 1531-line exec-based impl with 82-line file; `Transform()` delegates to `TransformV2()`; kept `substitutePromptTemplate()` and `buildDiffSummary()`
- `internal/workflow/transform_group.go` — replaced 415-line exec-based impl with 75-line file; builds per-group `model.Task`, invokes `TransformV2` as Temporal child workflow
- `internal/workflow/transform_test.go` — stripped to only `TestSubstitutePromptTemplate`
- **Deleted:** `internal/activity/claudecode.go`, `internal/activity/deterministic.go`, `internal/activity/deterministic_test.go`, `internal/activity/steering.go`, `internal/activity/steering_test.go`
- `internal/activity/sandbox.go` — removed `CloneRepositories`, `cloneTransformationMode`, `cloneRepos`, `configureGitCredentials`, `CloneRepositoriesInput`
- `internal/activity/report.go` — removed `CollectReport`; kept `ValidateSchema`
- `internal/activity/report_test.go` — removed CollectReport tests
- `internal/activity/constants.go` — removed `ActivityCloneRepositories`, `ActivityRunClaudeCode`, `ActivityExecuteDeterministic`, `ActivityCollectReport`, `ActivityGetDiff`, `ActivityGetVerifierOutput`
- `cmd/worker/main.go` — removed `claudeActivities`, `deterministicActivities`, `steeringActivities` creation and registrations

**Build/Test/Lint status:** All three pass (`go build ./...`, `go test ./...`, `make lint`) — verified after each phase.

**agentbox repo status:** `make lint` clean, `go test ./...` all pass. HEAD is `4302bc8 WIP` (contains OSCommandExecutor export fix — lint passes despite messy commit message).

## Learnings

- **Protocol shim is still live:** `internal/agent/protocol/types.go` still has 18 importers. All are in `internal/agent/` step files (clone, collect, pipeline, pr, transform, validate, verify) and their test files, plus `cmd/agent/main.go`, `internal/activity/sandbox.go`, `internal/activity/agent_test.go`, `internal/workflow/transform_v2_test.go`. Phase 4 migrates these and deletes the shim.
- **`fleetproto.DefaultBasePath` doesn't exist yet:** The shim defines `DefaultBasePath = "/workspace/.fleetlift"`. Phase 4 Task 1 adds this constant to `fleetproto` before migrating callers.
- **agentbox temporalkit uses simpler bare-param APIs** (not spec's typed input structs). Fleetlift activities wrap these with type marshaling/unmarshaling. This is intentional and working.
- **`Transform()` and `TransformV2()` have identical signatures** — made the delegation trivial.
- **`buildDiffSummary()` in transform.go is called from transform_v2.go** — that's why it was kept when the rest of transform.go was stripped.
- **agentbox `agent.Protocol.WriteStatus()` returns `error`** (spec had void) — callers must handle the return value.

## Artifacts

**Plan documents:**
- `docs/plans/2026-03-05-agentbox-split-phase4-cleanup.md` — Phase 4 plan (9 tasks, ready to execute)
- `docs/plans/2026-03-04-agentbox-split-phase2b-3.md` — updated with progress markers
- `docs/plans/2026-03-04-agentbox-split-phase1-2.md` — marked ALL TASKS COMPLETE
- `docs/IMPLEMENTATION_PLAN.md` — updated with AB-1 through AB-4 sections

**Key modified files (fleetlift):**
- `internal/activity/agent.go`, `sandbox.go`, `manifest.go`
- `internal/workflow/transform_v2.go`, `transform.go`, `transform_group.go`
- `cmd/agent/main.go`, `cmd/worker/main.go`
- `internal/agent/pipeline.go`, `deps.go`

**Protocol shim (to be deleted in Phase 4):**
- `internal/agent/protocol/types.go`

## Action Items & Next Steps

### Immediate: Execute Phase 4

The plan at `docs/plans/2026-03-05-agentbox-split-phase4-cleanup.md` is ready. Execute task-by-task:

1. **Task 1:** Add `DefaultBasePath = "/workspace/.fleetlift"` to `internal/agent/fleetproto/types.go`
2. **Task 2:** Migrate 6 `internal/agent/` step files (clone, collect, pr, transform, validate, verify) → `fleetproto`
3. **Task 3:** Migrate `internal/agent/pipeline.go` → `fleetproto` + `agentboxproto`
4. **Task 4:** Migrate 7 `internal/agent/` test files
5. **Task 5:** Migrate `cmd/agent/main.go` (`protocol.BasePath` → `fleetproto.DefaultBasePath`)
6. **Task 6:** Migrate `internal/activity/sandbox.go` (`protocol.BasePath` → `fleetproto.DefaultBasePath`)
7. **Task 7:** Migrate `internal/activity/agent_test.go` + `internal/workflow/transform_v2_test.go`
8. **Task 8:** Verify zero shim importers, then `rm internal/agent/protocol/types.go`
9. **Task 9:** Update CLAUDE.md + mark Phase 3c+4 complete in plan docs + final `go build ./... && go test ./... && make lint`

### After Phase 4

- **OpenSandbox adapter** (`agentbox/sandbox/opensandbox/`): Implement `sandbox.AgentProvider` backed by OpenSandbox REST API. Deferred — needs knowledge of OpenSandbox API surface.
- **agentbox WIP commit cleanup**: `4302bc8 WIP` — messy commit message. Consider amending before merging (check if remote exists first).
- **Commit the work**: Neither repo has new commits from this session's changes. All changes are uncommitted working tree modifications on `feat/agentbox-split`.

## Other Notes

- **Two repo layout:** agentbox at `/Users/andrew/dev/code/projects/agentbox`, fleetlift at `/Users/andrew/dev/code/projects/fleetlift`. `go.mod` in fleetlift has `replace github.com/tinkerloft/agentbox => ../agentbox`.
- **fleetlift branch:** `feat/agentbox-split`. Main is untouched.
- **agentbox is on `main`** — no feature branch.
- **Protocol shim importer count:** `grep -rn '"github.com/tinkerloft/fleetlift/internal/agent/protocol"' --include="*.go" .` should return 18 results before Phase 4, and 0 after Task 8.
- **Phase 4 is purely mechanical** — all changes are import path substitutions (`protocol.X` → `fleetproto.X` or `agentboxproto.X`). No logic changes.
- **`buildDiffSummary` dependency:** `internal/workflow/transform.go:` contains `buildDiffSummary()` which is called by `transform_v2.go`. Do not delete this function.
