# Agentbox Split — Phase 4: Protocol Shim Elimination & Cleanup

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Delete the `internal/agent/protocol/` shim package by migrating all 18 remaining importers to canonical source packages, then do final CLAUDE.md update and verification.

**Architecture:** The shim (`internal/agent/protocol/types.go`) re-exports types from two sources: `agentbox/protocol` (generic Phase*, SteeringAction*, AgentStatus) and `internal/agent/fleetproto` (domain types TaskManifest, AgentResult, etc.). Migration replaces every `protocol.X` reference with the canonical source. A new `fleetproto.DefaultBasePath` constant replaces the shim-defined path constant.

**Tech Stack:** Go 1.25, `github.com/tinkerloft/agentbox/protocol`, `github.com/tinkerloft/fleetlift/internal/agent/fleetproto`

**Working directory:** `/Users/andrew/dev/code/projects/fleetlift` (branch `feat/agentbox-split`)

---

## Shim Importer Map

| File | Uses from shim | Migrate to |
|------|---------------|------------|
| `internal/agent/clone.go` | `protocol.TaskManifest`, `ManifestRepo`, `GitConfig` | `fleetproto` |
| `internal/agent/collect.go` | `protocol.TaskManifest`, `AgentResult`, `RepoResult` | `fleetproto` |
| `internal/agent/pipeline.go` | `protocol.TaskManifest`, `AgentStatus`, `Phase*`, path helpers | `fleetproto` + `agentboxproto` |
| `internal/agent/pr.go` | `protocol.TaskManifest`, `PRInfo`, `RepoResult` | `fleetproto` |
| `internal/agent/transform.go` | `protocol.TaskManifest`, `RepoResult` | `fleetproto` |
| `internal/agent/validate.go` | `protocol.TaskManifest` | `fleetproto` |
| `internal/agent/verify.go` | `protocol.TaskManifest`, `VerifierResult` | `fleetproto` |
| `cmd/agent/main.go` | `protocol.BasePath` | `fleetproto.DefaultBasePath` |
| `internal/activity/sandbox.go` | `protocol.BasePath` | `fleetproto.DefaultBasePath` |
| `internal/activity/agent_test.go` | `protocol.AgentStatus`, `Phase*`, `AgentResult`, `TaskManifest` | `agentboxproto` + `fleetproto` |
| `internal/agent/clone_test.go` | `protocol.TaskManifest` | `fleetproto` |
| `internal/agent/collect_test.go` | `protocol.TaskManifest`, `AgentResult`, `RepoResult` | `fleetproto` |
| `internal/agent/pipeline_test.go` | `protocol.TaskManifest`, `AgentStatus`, `Phase*` | `fleetproto` + `agentboxproto` |
| `internal/agent/pr_test.go` | `protocol.TaskManifest`, `RepoResult`, `PRInfo` | `fleetproto` |
| `internal/agent/transform_test.go` | `protocol.TaskManifest`, `RepoResult` | `fleetproto` |
| `internal/agent/validate_test.go` | `protocol.TaskManifest` | `fleetproto` |
| `internal/agent/verify_test.go` | `protocol.TaskManifest`, `VerifierResult` | `fleetproto` |
| `internal/workflow/transform_v2_test.go` | `protocol.AgentStatus`, `Phase*` | `agentboxproto` |

**Import aliases to use:**
- `agentboxproto "github.com/tinkerloft/agentbox/protocol"` — for Phase*, SteeringAction*, AgentStatus, StatusProgress
- `"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"` — for TaskManifest, AgentResult, RepoResult, PRInfo, SteeringInstruction, etc.

---

## Task 1: Add `DefaultBasePath` constant to `fleetproto`

**Files:**
- Modify: `internal/agent/fleetproto/types.go`

**Step 1: Add constant**

In `internal/agent/fleetproto/types.go`, add near the top (after package declaration / imports):

```go
// DefaultBasePath is the base directory for fleetlift agent protocol files inside the sandbox.
// This is the fleetlift-specific override of agentbox's DefaultBasePath.
const DefaultBasePath = "/workspace/.fleetlift"
```

**Step 2: Build**

```bash
go build ./internal/agent/fleetproto/...
```
Expected: no errors.

---

## Task 2: Migrate `internal/agent/` step files (non-test)

**Files:**
- Modify: `internal/agent/clone.go`
- Modify: `internal/agent/collect.go`
- Modify: `internal/agent/pr.go`
- Modify: `internal/agent/transform.go`
- Modify: `internal/agent/validate.go`
- Modify: `internal/agent/verify.go`

**Step 1: For each file, replace the protocol import**

Pattern — replace:
```go
"github.com/tinkerloft/fleetlift/internal/agent/protocol"
```
with:
```go
"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
```

Then replace every `protocol.` reference with `fleetproto.` (all types in these files are fleetproto-backed aliases: `TaskManifest`, `ManifestRepo`, `ManifestGitConfig`, `AgentResult`, `RepoResult`, `VerifierResult`, `PRInfo`, etc.).

**Step 2: Build**

```bash
go build ./internal/agent/...
```
Expected: no errors. Fix any remaining `protocol.` references.

**Step 3: Run step tests**

```bash
go test ./internal/agent/... 2>&1 | head -30
```
Expected: may fail (test files still import shim) — that's OK, will fix in Task 4.

---

## Task 3: Migrate `internal/agent/pipeline.go`

**Files:**
- Modify: `internal/agent/pipeline.go`

This file uses both `fleetproto`-backed types AND `agentboxproto`-backed types (Phase*, AgentStatus) and possibly the path helpers.

**Step 1: Replace import**

Replace `internal/agent/protocol` with two imports:
```go
agentboxproto "github.com/tinkerloft/agentbox/protocol"
"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
```

**Step 2: Update references**

- `protocol.TaskManifest` → `fleetproto.TaskManifest`
- `protocol.AgentStatus` → `agentboxproto.AgentStatus`
- `protocol.Phase*` → `agentboxproto.Phase*`
- `protocol.StatusProgress` → `agentboxproto.StatusProgress`
- `protocol.ManifestPath(...)` etc. → `agentboxproto.ManifestPath(...)` but with the fleetlift base path (already passed as `p.basePath`)
- `protocol.SteeringInstruction` → `fleetproto.SteeringInstruction`
- `protocol.SteeringAction*` → `agentboxproto.SteeringAction*`

**Step 3: Build**

```bash
go build ./internal/agent/...
```
Expected: no errors.

---

## Task 4: Migrate `internal/agent/` test files

**Files:**
- Modify: `internal/agent/clone_test.go`
- Modify: `internal/agent/collect_test.go`
- Modify: `internal/agent/pipeline_test.go`
- Modify: `internal/agent/pr_test.go`
- Modify: `internal/agent/transform_test.go`
- Modify: `internal/agent/validate_test.go`
- Modify: `internal/agent/verify_test.go`

**Step 1: For each test file, apply the same import replacement**

Files using only fleetproto types (`TaskManifest`, `AgentResult`, `RepoResult`, `PRInfo`):
- `clone_test.go`, `collect_test.go`, `pr_test.go`, `transform_test.go`, `validate_test.go`, `verify_test.go`

Replace `internal/agent/protocol` → `internal/agent/fleetproto`, then `protocol.` → `fleetproto.`

Files using both (`pipeline_test.go`):
Replace with dual import `agentboxproto` + `fleetproto`, update references.

**Step 2: Build and test**

```bash
go build ./internal/agent/... && go test ./internal/agent/...
```
Expected: all tests pass.

---

## Task 5: Migrate `cmd/agent/main.go`

**Files:**
- Modify: `cmd/agent/main.go`

**Step 1: Replace import**

Remove `"github.com/tinkerloft/fleetlift/internal/agent/protocol"`.
Add (if not already present): `"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"`.

**Step 2: Replace `protocol.BasePath`**

```go
basePath := protocol.BasePath
```
→
```go
basePath := fleetproto.DefaultBasePath
```

**Step 3: Build**

```bash
go build ./cmd/agent/...
```
Expected: no errors.

---

## Task 6: Migrate `internal/activity/sandbox.go`

**Files:**
- Modify: `internal/activity/sandbox.go`

**Step 1: Inspect current usage**

```bash
grep -n "protocol\." internal/activity/sandbox.go
```

The file uses `protocol.BasePath` for the sandbox base path configuration.

**Step 2: Replace import and reference**

Remove `"github.com/tinkerloft/fleetlift/internal/agent/protocol"`.
Add `"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"`.
Replace `protocol.BasePath` → `fleetproto.DefaultBasePath`.

**Step 3: Build**

```bash
go build ./internal/activity/...
```
Expected: no errors.

---

## Task 7: Migrate test files outside `internal/agent/`

**Files:**
- Modify: `internal/activity/agent_test.go`
- Modify: `internal/workflow/transform_v2_test.go`

### `internal/activity/agent_test.go`

Uses both `agentboxproto`-backed (Phase*, AgentStatus) and `fleetproto`-backed (AgentResult, RepoResult, PRInfo, TaskManifest) types.

Replace `internal/agent/protocol` with dual import:
```go
agentboxproto "github.com/tinkerloft/agentbox/protocol"
"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
```

Update references:
- `protocol.AgentStatus` → `agentboxproto.AgentStatus`
- `protocol.Phase*` → `agentboxproto.Phase*`
- `protocol.Phase` → `agentboxproto.Phase`
- `protocol.AgentResult` → `fleetproto.AgentResult`
- `protocol.RepoResult` → `fleetproto.RepoResult`
- `protocol.PRInfo` → `fleetproto.PRInfo`
- `protocol.TaskManifest` → `fleetproto.TaskManifest`

### `internal/workflow/transform_v2_test.go`

Uses only agentboxproto-backed types (AgentStatus, Phase*).

Replace import → `agentboxproto "github.com/tinkerloft/agentbox/protocol"`, then `protocol.` → `agentboxproto.`

**Step 3: Build and test**

```bash
go build ./... && go test ./internal/activity/... ./internal/workflow/...
```
Expected: all pass.

---

## Task 8: Delete protocol shim

**Files:**
- Delete: `internal/agent/protocol/types.go`
- Delete: `internal/agent/protocol/` directory (if empty)

**Step 1: Verify no remaining importers**

```bash
grep -rn '"github.com/tinkerloft/fleetlift/internal/agent/protocol"' --include="*.go" .
```
Expected: zero results.

**Step 2: Delete**

```bash
rm internal/agent/protocol/types.go
rmdir internal/agent/protocol/ 2>/dev/null || true
```

**Step 3: Build and test**

```bash
go build ./... && go test ./...
```
Expected: everything passes. If any missed importer surfaces, fix it.

**Step 4: Lint**

```bash
make lint
```
Expected: no errors.

---

## Task 9: Update `CLAUDE.md` and final docs

**Files:**
- Modify: `CLAUDE.md` (project-level)

**Step 1: Update project structure section**

In `CLAUDE.md`, update the Project Structure section to remove any mention of `internal/sandbox/` (deleted in Phase 3a) and `internal/agent/protocol/` (deleted in Task 8). Add notes about `internal/agent/fleetproto/` and the agentbox import.

**Step 2: Update implementation plan**

In `docs/plans/IMPLEMENTATION_PLAN.md`:
- Mark Phase AB-3c (legacy workflow migration) as complete
- Mark Phase AB-4 (protocol shim elimination) as complete

In `docs/plans/2026-03-04-agentbox-split-phase2b-3.md`, mark Task 14 (Step 10 legacy migration) as complete.

**Step 3: Final full verification**

```bash
go build ./... && go test ./... && make lint
```

All three must pass with no errors before this phase is considered complete.

---

## Unanswered Questions

1. **`internal/agent/protocol/` package tests?** — The package has only `types.go` (no `_test.go`). Deletion should be clean.
2. **agentbox WIP commit cleanup** — The HEAD in agentbox repo is `4302bc8 WIP`. Should this be squash-committed or amended before merging? (Not blocking Phase 4.)
3. **opensandbox adapter** — The full spec calls for `sandbox/opensandbox/` in agentbox. Deferred: needs knowledge of the OpenSandbox API surface before implementation.
