---
date: 2026-03-05T21:57:51Z
researcher: Claude Sonnet 4.6
git_commit: 2d984b1
branch: feat/agentbox-split
repository: fleetlift + agentbox (two repos)
topic: "Agentbox Split — Phases 1-3 Implementation"
tags: [agentbox, split, sandbox, protocol, temporalkit, fleetproto]
status: complete
last_updated: 2026-03-05
last_updated_by: Claude Sonnet 4.6
type: implementation_strategy
---

# Handoff: agentbox-split Phases 1–3 Complete

## Task(s)

**Goal:** Split `github.com/tinkerloft/fleetlift` into two repos:
- `github.com/tinkerloft/agentbox` — generic agent hosting platform (new repo at `/Users/andrew/dev/code/projects/agentbox`)
- `github.com/tinkerloft/fleetlift` — refactored to import agentbox (branch `feat/agentbox-split`)

### Status

| Phase | Tasks | Status |
|-------|-------|--------|
| Phase 1: agentbox `protocol` + `sandbox` | Tasks 1–5 | ✅ Complete |
| Phase 2: agentbox `agent` + `temporalkit` | Tasks 6–8 | ✅ Complete |
| Phase 3a: fleetlift structural refactor | Tasks 9–14 | ✅ Complete |
| Phase 3b: activity/manifest/workflow/agent-binary refactor | Steps 5–9 from spec | ❌ Not started |
| Phase 3c: Legacy workflow migration | Step 10 from spec | ❌ Not started |
| Phase 2 OpenSandbox adapter | `sandbox/opensandbox/` | ❌ Not started |
| Phase 4: Cleanup + docs | — | ❌ Not started |

**Working from plans:**
- `/Users/andrew/dev/code/projects/fleetlift/docs/plans/2026-03-04-agentbox-split-phase1-2.md`
- `/Users/andrew/dev/code/projects/fleetlift/docs/plans/2026-03-04-agentbox-split-phase2b-3.md`
- Full original spec: `/Users/andrew/dev/code/projects/fleetlift/docs/plans/delegated-pondering-teacup.md`

## Critical References

- `/Users/andrew/dev/code/projects/fleetlift/docs/plans/delegated-pondering-teacup.md` — **full original spec** (source of truth; the phase plan docs are a subset of this)
- `/Users/andrew/dev/code/projects/fleetlift/docs/plans/2026-03-04-agentbox-split-phase2b-3.md` — Phase 2b+3 plan (Tasks 6–14)
- `/Users/andrew/dev/code/projects/agentbox/` — new agentbox module (fully implemented for phases 1–2)

## Recent Changes

**agentbox repo** (`/Users/andrew/dev/code/projects/agentbox`):
- `protocol/protocol.go` — generic file-IPC types; `DefaultBasePath="/workspace/.agentbox"`
- `sandbox/provider.go` — `AgentProvider.PollStatus` returns `([]byte, error)`; `ProvisionOptions` has `Cmd []string`, K8s security fields
- `sandbox/docker/provider.go` + `register.go` — container name `agentbox-{taskID}`, `basePathCache sync.Map`
- `sandbox/k8s/` (5 files) — job name `agentbox-sandbox-{taskID}`, labels `agentbox.io/task-id`, RuntimeClass/UserNamespace/OwnerRef/EphemeralStorage
- `agent/deps.go` — `FileSystem` + `CommandExecutor` interfaces; **`OSCommandExecutor` is exported** (lint fix applied but NOT YET COMMITTED — agentbox latest commit is `4302bc8 WIP`)
- `agent/constants.go` — `ManifestPollInterval=500ms`, `SteeringPollInterval=2s`
- `agent/protocol.go` — `Protocol` struct with `New(basePath, fs)` constructor (simpler than spec's `NewProtocol(ProtocolConfig, fs, logger)`)
- `temporalkit/agent_activities.go` + `sandbox_activities.go` — Temporal wrappers with bare `(ctx, id, []byte)` params (simpler than spec's typed input structs)

**fleetlift repo** (`feat/agentbox-split`, commit `2d984b1`):
- `go.mod` — `replace github.com/tinkerloft/agentbox => ../agentbox`
- `internal/agent/fleetproto/types.go` — fleetlift-specific types (`TaskManifest`, `AgentResult`, `RepoResult`, `PhaseCreatingPRs`, `SteeringInstruction`+`Timestamp`)
- `internal/agent/protocol/types.go` — shim re-exporting agentbox types as aliases + fleetproto types; uses `DefaultBasePath="/workspace/.fleetlift"`
- `internal/sandbox/` — **deleted**
- `internal/activity/agent.go` — updated: imports `agentbox/sandbox`, `PollStatus` now returns `[]byte` and is unmarshalled inline
- `internal/activity/sandbox.go` — updated: uses `agentbox/sandbox.ProvisionOptions` with `Cmd:[]string{"/agent-bin/agent","serve"}`
- `cmd/worker/main.go` — blank imports `agentbox/sandbox/docker` + `agentbox/sandbox/k8s` for init() registration

## Learnings

- **API deviations from spec (intentional simplifications):**
  - `agent.Protocol`: uses `New(basePath, fs)` not `NewProtocol(ProtocolConfig, fs, logger)`. No `ProtocolConfig` struct, no embedded slog.Logger. `WriteStatus` returns `error` (spec had void/best-effort).
  - `temporalkit`: bare function params instead of typed input structs (`SubmitManifestInput`, etc.). `WaitForPhase` returns `([]byte, error)` not `(*protocol.AgentStatus, error)`. `Provider` field is exported. `SandboxActivities` uses `AgentProvider` not `Provider`. No `ProvisionInput`/`SandboxInfo` types.
  - These deviations mean fleetlift activities do NOT delegate to `agentbox/temporalkit` — they still contain the activity logic inline.

- **Protocol shim `DefaultBasePath`**: correctly set to `/workspace/.fleetlift` (not agentbox's `/workspace/.agentbox`) — fleetlift agent still uses the old path.

- **`agentbox/agent/deps.go`**: `OSCommandExecutor` (capital O) must be exported to avoid golangci-lint `unused` error. The fix is in the WIP commit `4302bc8` but **not yet properly committed**.

- **`StatusProgress` field rename**: `CompletedRepos`/`TotalRepos` → `Current`/`Total` (agentbox uses generic names). The protocol shim aliases this, but `types_test.go` in fleetlift was updated accordingly.

- **fleetlift activities still contain full logic**: `internal/activity/agent.go` and `internal/activity/sandbox.go` were updated to use agentbox sandbox types but were NOT refactored to delegate to `agentbox/temporalkit`. This is one of the remaining work items.

## Artifacts

**agentbox** (`/Users/andrew/dev/code/projects/agentbox`):
- `go.mod`, `Makefile`
- `protocol/protocol.go`, `protocol/protocol_test.go`
- `sandbox/provider.go`, `sandbox/factory.go`, `sandbox/factory_test.go`
- `sandbox/docker/provider.go`, `sandbox/docker/provider_test.go`, `sandbox/docker/register.go`
- `sandbox/k8s/exec.go`, `sandbox/k8s/wait.go`, `sandbox/k8s/job.go`, `sandbox/k8s/provider.go`, `sandbox/k8s/provider_test.go`, `sandbox/k8s/register.go`
- `agent/deps.go`, `agent/constants.go`, `agent/protocol.go`, `agent/protocol_test.go`
- `temporalkit/agent_activities.go`, `temporalkit/agent_activities_test.go`, `temporalkit/sandbox_activities.go`, `temporalkit/sandbox_activities_test.go`

**fleetlift** (`/Users/andrew/dev/code/projects/fleetlift`, branch `feat/agentbox-split`):
- `internal/agent/fleetproto/types.go`
- `internal/agent/protocol/types.go` (shim)
- `internal/activity/agent.go` (updated)
- `internal/activity/sandbox.go` (updated)
- `cmd/worker/main.go` (updated)
- `go.mod`, `go.sum` (updated with agentbox replace directive)

**Plan documents:**
- `docs/plans/2026-03-04-agentbox-split-phase1-2.md`
- `docs/plans/2026-03-04-agentbox-split-phase2b-3.md`
- `docs/plans/delegated-pondering-teacup.md` (full spec)

## Action Items & Next Steps

### Immediate (before proceeding)
1. **Commit the agentbox lint fix**: `git -C /Users/andrew/dev/code/projects/agentbox add agent/deps.go && git -C /Users/andrew/dev/code/projects/agentbox commit -m "fix: export OSCommandExecutor to resolve unused type lint error"` — the current HEAD `4302bc8 WIP` is uncommitted/messy.
2. **Run `make lint` in agentbox** to confirm clean.

### Remaining spec work (from `delegated-pondering-teacup.md`)

**Phase 3 (fleetlift) — structural refactoring not yet done:**
- **Step 5**: Refactor `internal/activity/agent.go` to import and wrap `agentbox/temporalkit.AgentActivities` — currently has inline logic; should delegate to agentbox.
- **Step 6**: Refactor `internal/activity/sandbox.go` to wrap `agentbox/temporalkit.SandboxActivities` for Provision/Cleanup.
- **Step 7**: Refactor `internal/activity/manifest.go` — change output type from `protocol.TaskManifest` to `fleetproto.TaskManifest`.
- **Step 8**: Refactor `internal/workflow/transform_v2.go` — import `agentbox/protocol` for Phase* constants directly.
- **Step 9**: Refactor agent binary `cmd/agent/main.go` to use `agentbox/agent.Protocol` primitives (WaitForManifest → parse fleetproto.TaskManifest → steps → WriteResult).

**Phase 3 Step 10 (major)**: Migrate legacy workflows:
- `Transform` (v1) — currently execs Claude Code directly via sandbox ExecShell; refactor to sidecar agent pattern (submit manifest → poll status → read result)
- `TransformGroup` — refactor to use TransformV2 path
- Remove exec-based activities: `RunClaudeCode`, `ExecuteDeterministic`, `CollectReport`, `GetDiff`, `GetVerifierOutput`
- Keep: `CreatePullRequest`, `NotifySlack`, `CaptureKnowledge`, `EnrichPrompt`, `ValidateSchema`

**Phase 2 (agentbox)**: `sandbox/opensandbox/` adapter — implement `sandbox.AgentProvider` backed by OpenSandbox unified API.

**Phase 4**: Delete dead code, update `CLAUDE.md`, update `docs/IMPLEMENTATION_PLAN.md`.

### temporalkit API alignment (optional)
The implemented `agentbox/temporalkit` uses simpler bare-param APIs vs the spec's typed input structs. Decide whether to align to spec or accept the simpler form. If aligning:
- Add `SubmitManifestInput`, `WaitForPhaseInput`, `ReadResultInput`, `SubmitSteeringInput` structs
- Change `WaitForPhase` return to `(*protocol.AgentStatus, error)`
- Add `ProvisionInput` / `SandboxInfo` to `SandboxActivities`
- Add `NewAgentActivities()` / `NewSandboxActivities()` constructors; unexport `Provider` field

## Other Notes

- **Two repo layout**: agentbox at `/Users/andrew/dev/code/projects/agentbox`, fleetlift at `/Users/andrew/dev/code/projects/fleetlift`. The `replace` directive in fleetlift's `go.mod` points to `../agentbox` (relative path).
- **fleetlift branch**: all fleetlift changes are on `feat/agentbox-split`. Main branch is untouched.
- **agentbox is on `main`** — no feature branch; all commits go directly to main.
- **Tests pass** per fleetlift-agent's report: `make lint`, `go test ./...`, `go build ./...` all passed on commit `2d984b1`. However the subsequent `OSCommandExecutor` fix in agentbox (`4302bc8`) means `make lint` in agentbox currently fails until that WIP is properly committed.
- **Protocol path**: fleetlift's shim uses `DefaultBasePath="/workspace/.fleetlift"` — existing deployed agents use this path. If/when fleetlift adopts agentbox's `DefaultBasePath="/workspace/.agentbox"`, existing container images must be updated.
- **Temporal serialization note**: because `temporalkit.WaitForPhase` returns `[]byte` (not `*protocol.AgentStatus`), Temporal workflow history stores raw JSON bytes for phase results. If the spec's typed return is later adopted, this is a breaking change to workflow history compatibility.
