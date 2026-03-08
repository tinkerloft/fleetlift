# Fleetlift Capability Inventory

> Generated 2026-03-08 from source code audit of all Go packages, CLI entry points, and existing documentation.

---

## 1. CLI Commands

All commands defined in `cmd/cli/main.go` via cobra.

| Command | Subcommand | Key Flags | Source File | Description |
|---------|-----------|-----------|-------------|-------------|
| `run` | — | `--file`, `--repo/--repos`, `--prompt`, `--image`, `--args`, `--env`, `--verifier`, `--output`, `--parallel`, `--no-approval`, `--branch`, `--title`, `--label` | `main.go` | Submit a task (from file or inline flags) |
| `status` | — | `[workflow-id]` (optional, uses last) | `main.go` | Check task status |
| `result` | — | `[workflow-id]`, `--output` | `main.go` | Get task result |
| `approve` | — | `[workflow-id]` | `main.go` | Approve changes, create PRs |
| `reject` | — | `[workflow-id]` | `main.go` | Reject changes |
| `cancel` | — | `[workflow-id]` | `main.go` | Cancel task |
| `list` | — | `--output` | `main.go` | List all tasks |
| `reports` | — | `[workflow-id]`, `--format`, `--target` | `main.go` | View report output |
| `diff` | — | `[workflow-id]` | `main.go` | View current diffs |
| `logs` | — | `[workflow-id]` | `main.go` | View verifier logs |
| `steer` | — | `[workflow-id]`, `--prompt` | `main.go` | Send steering feedback |
| `continue` | — | `[workflow-id]`, `--skip-remaining` | `main.go` | Continue after pause |
| `retry` | — | `[workflow-id]`, `--file`, `--failed-only` | `main.go` | Retry failed groups |
| `create` | — | `--describe`, `--repo`, `--output`, `--dry-run`, `--run`, `--interactive/-i`, `--template` | `create.go` | AI-powered task YAML generation |
| `templates` | `list` | — | `templates.go` | List available templates |
| `knowledge` | `list` | `--task-id`, `--type`, `--tag` | `knowledge.go` | List knowledge items |
| `knowledge` | `show` | `<item-id>` | `knowledge.go` | Show knowledge item details |
| `knowledge` | `add` | `--summary`, `--type`, `--details`, `--tags` | `knowledge.go` | Add manual knowledge item |
| `knowledge` | `delete` | `<item-id>` | `knowledge.go` | Delete a knowledge item |
| `knowledge` | `review` | `--task-id` | `knowledge.go` | Interactive review/curation |
| `knowledge` | `commit` | `--repo` | `knowledge.go` | Copy approved items to repo |

**Notes:**
- All workflow-id arguments are optional; defaults to last workflow ID from `~/.fleetlift/last-workflow`
- `run --parallel` auto-generates one-repo-per-group parallel execution
- `create --run` requires `--output`
- `create` uses Anthropic SDK with `claude-sonnet-4-6` model

---

## 2. Task File Schema

Source of truth: `internal/config/loader.go` (YAML parsing) + `internal/model/task.go` (Task struct).

```yaml
version: 1                      # Required. Must be 1.
id: my-task                     # Optional. Auto-generated if omitted.
title: "Task title"             # Required.
description: "..."              # Optional.
mode: transform                 # "transform" (default) | "report"

# Standard repositories (direct mode)
repositories:
  - url: https://github.com/org/repo.git
    branch: main                # Optional, default: main
    name: repo                  # Optional display name
    setup: ["go mod download"]  # Optional post-clone commands

# OR: Transformation repo + targets (recipe mode)
transformation:
  url: https://github.com/org/recipe.git
  branch: main
  setup: ["npm install"]
targets:
  - url: https://github.com/org/target.git
    name: target-name

# forEach iteration (report mode only)
for_each:
  - name: item-name             # Used in {{.Name}} template
    context: "..."              # Used in {{.Context}} template

# Execution — exactly one of:
execution:
  agentic:
    prompt: "..."
    verifiers:
      - name: build
        command: ["go", "build", "./..."]
    output:                     # Report mode only
      schema: { ... }           # JSON Schema for frontmatter validation
  deterministic:
    image: openrewrite/rewrite:latest
    command: "..."              # Optional override
    args: ["arg1", "arg2"]
    env: { KEY: value }
    verifiers: [...]

# Orchestration
timeout: 30m                    # Duration string. Default: 1h
require_approval: false         # Default: false
max_parallel: 5                 # Default: 5
max_steering_iterations: 5      # Default: 5
requester: "user@example.com"   # Optional
ticket_url: "..."               # Optional
slack_channel: "#channel"       # Optional

# Grouped execution
groups:
  - name: group-name
    repositories:
      - url: https://github.com/org/repo.git

failure:
  threshold_percent: 20         # Pause/abort if failure rate exceeds this
  action: pause                 # "pause" | "abort"

# PR configuration (transform mode)
pull_request:
  branch_prefix: "auto/feature"
  title: "PR title"
  labels: ["automated"]
  reviewers: ["user1"]          # GitHub usernames

# Sandbox overrides
sandbox:
  image: custom-image:latest

# Credentials
credentials:
  github_token: "..."           # Usually from env
  anthropic_api_key: "..."

# Knowledge system
knowledge:
  capture_disabled: false
  enrich_disabled: false
  max_items: 10
  tags: ["go", "logging"]
```

**Mutual exclusivity rules** (enforced in `config/loader.go`):
- `repositories` vs `transformation`+`targets`: pick one pattern
- `execution.agentic` vs `execution.deterministic`: pick one
- `for_each` requires `mode: report`

---

## 3. Workflow Architecture

### Registered Workflows (cmd/worker/main.go)

| Workflow | Function | Purpose |
|----------|----------|---------|
| `Transform` | `workflow.Transform` | Entry point. Routes single-group → TransformV2, multi-group → grouped parallel execution |
| `TransformGroup` | `workflow.TransformGroup` | Child workflow per group, delegates to TransformV2 |
| `TransformV2` | `workflow.TransformV2` | Agent-mode workflow: provision → manifest → poll → HITL steering → knowledge capture |

### Flow

```
Transform(task)
├── Single group / no groups → TransformV2(task)
└── Multiple groups → transformGrouped(task)
    ├── Batch groups up to max_parallel
    ├── For each batch: spawn TransformGroup children in parallel
    ├── After each batch: check failure threshold
    ├── If threshold breached:
    │   ├── action=abort → stop
    │   └── action=pause → wait for signal (continue/skip/cancel)
    └── Aggregate results from all groups
```

### TransformV2 Lifecycle (transform_v2.go)

```
1. EnrichPrompt (knowledge injection, non-blocking)
2. ProvisionAgentSandbox
3. SubmitTaskManifest
4. WaitForAgentPhase(awaiting_input | complete | failed)
5. ReadAgentResult
6. If complete/failed → skip to step 8
7. HITL Steering Loop:
   a. Cache diffs for query handlers
   b. Set status = AwaitingApproval
   c. Wait for signal: approve/reject/steer/cancel
   d. On steer → SubmitSteeringAction, WaitForAgentPhase, ReadAgentResult, loop
   e. On approve → SubmitSteeringAction(approve), WaitForAgentPhase(complete), ReadAgentResult
   f. On reject/cancel → SubmitSteeringAction(cancel), return
8. CaptureKnowledge (non-blocking)
9. CleanupSandbox (deferred)
10. Return TaskResult
```

### Signals & Queries (transform.go, transform_v2.go)

| Type | Name | Purpose |
|------|------|---------|
| Signal | `approve` | Approve changes, create PRs |
| Signal | `reject` | Reject and cancel |
| Signal | `cancel` | Cancel task |
| Signal | `steer` | Send steering prompt (payload: string) |
| Signal | `continue` | Continue after pause (payload: `{SkipRemaining bool}`) |
| Query | `status` | Current workflow status |
| Query | `diff` | Cached diffs |
| Query | `verifier_output` | Verifier logs (alias: `logs`) |
| Query | `steering_state` | Steering iteration state |
| Query | `progress` | Grouped execution progress |

---

## 4. Activity Inventory

Source: `internal/activity/constants.go` — 13 registered activities.

| Activity Name | Struct | Method | Source File | Description |
|--------------|--------|--------|-------------|-------------|
| `ProvisionSandbox` | `SandboxActivities` | `ProvisionSandbox` | `sandbox.go` | Create sandbox container (basic mode) |
| `ProvisionAgentSandbox` | `SandboxActivities` | `ProvisionAgentSandbox` | `sandbox.go` | Create sandbox for agent mode (with image override) |
| `CleanupSandbox` | `SandboxActivities` | `CleanupSandbox` | `sandbox.go` | Destroy sandbox (respects DEBUG_NO_CLEANUP) |
| `RunVerifiers` | `SandboxActivities` | `RunVerifiers` | `sandbox.go` | Execute verifier commands in sandbox |
| `SubmitTaskManifest` | `AgentActivities` | `SubmitTaskManifest` | `agent.go` | Write manifest.json to sandbox |
| `WaitForAgentPhase` | `AgentActivities` | `WaitForAgentPhase` | `agent.go` | Poll status.json until target phases reached |
| `ReadAgentResult` | `AgentActivities` | `ReadAgentResult` | `agent.go` | Read result.json from sandbox |
| `SubmitSteeringAction` | `AgentActivities` | `SubmitSteeringAction` | `agent.go` | Write steering.json to sandbox |
| `CreatePullRequest` | `GitHubActivities` | `CreatePullRequest` | `github.go` | Create GitHub PR via go-github/v62 |
| `NotifySlack` | — | `NotifySlack` | `slack.go` | Send Slack notification (non-blocking on failure) |
| `ValidateSchema` | — | `ValidateSchema` | `report.go` | Validate report frontmatter against JSON Schema |
| `CaptureKnowledge` | — | `CaptureKnowledge` | `knowledge.go` | Auto-extract knowledge items via Claude haiku-4-5 |
| `EnrichPrompt` | — | `EnrichPrompt` | `knowledge.go` | Inject relevant knowledge items into prompt |

**Config validation** (`constants.go`): Worker checks env vars at startup when `REQUIRE_CONFIG=true`:
- `GIT_USER_EMAIL`, `GIT_USER_NAME` (git commit identity)
- `GITHUB_TOKEN` (PR creation + repo cloning)
- `ANTHROPIC_API_KEY` (agentic execution + knowledge capture)

---

## 5. Agent Protocol

Source: `internal/agent/fleetproto/types.go` + `internal/agent/` package.

### File-Based Protocol

All communication via JSON files at `/workspace/.fleetlift/` (configurable via `FLEETLIFT_BASE_PATH`).

| File | Direction | Purpose | Write Pattern |
|------|-----------|---------|---------------|
| `manifest.json` | Worker → Agent | Task definition (written once) | Direct write |
| `status.json` | Agent → Worker | Current phase (polled at 500ms) | Atomic rename |
| `result.json` | Agent → Worker | Full structured results | Atomic rename |
| `steering.json` | Worker → Agent | HITL instruction (consumed after read) | Atomic rename + delete |

### Phase Enum

`initializing` → `executing` → `verifying` → `awaiting_input` → `creating_prs` → `complete` | `failed` | `cancelled`

### Steering Actions

`steer`, `approve`, `reject`, `cancel`

### Agent Pipeline (pipeline.go)

```
1. WaitForManifest (poll manifest.json at 500ms)
2. ValidateManifest (check task_id, mode, sanitize names)
3. CloneRepos (git clone, configure identity, run setup commands, generate AGENTS.md)
4. RunTransformation:
   - Agentic: run `claude` CLI with `-p` flag + prompt
   - Deterministic: run manifest command/args directly
   - Security: blockedEnvVars list prevents leaking sensitive env vars
5. RunVerifiers (per-repo verifier commands)
6. CollectResults (git status, getDiffs, readReport for report mode)
7. WriteResult + WriteStatus(awaiting_input)
8. SteeringLoop (if require_approval):
   a. Poll steering.json at 2s interval
   b. On steer: re-run transformation with appended prompt, re-verify, re-collect
   c. On approve: create PRs, complete
   d. On reject/cancel: set cancelled, exit
   e. Max iterations: configurable (default 5)
9. CreatePullRequests (if transform mode + approved):
   - Uses `gh` CLI for PR creation
   - Injects .gitignore patterns for sensitive files
10. WriteStatus(complete) + WriteResult(final)
```

### Key Constants (constants.go)

| Constant | Value |
|----------|-------|
| ManifestPollInterval | 500ms |
| SteeringPollInterval | 2s |
| DefaultCloneDepth | 50 |
| DefaultMaxSteering | 5 |
| DefaultBasePath | `/workspace/.fleetlift` |

---

## 6. Package Structure

```
cmd/
├── cli/                        # CLI binary (fleetlift)
│   ├── main.go                 # Root command + all subcommands
│   ├── create.go               # `fleetlift create` (Anthropic SDK)
│   ├── create_assets.go        # Embedded schema + examples for create
│   ├── knowledge.go            # `fleetlift knowledge` subcommands
│   ├── templates.go            # `fleetlift templates list`
│   ├── templates_assets.go     # Embedded templates + Template type + registry
│   ├── schema/                 # Embedded schema files
│   │   ├── task-schema.md
│   │   ├── example-transform.yaml
│   │   └── example-report.yaml
│   └── templates/              # Embedded template YAML files
│       ├── dependency-upgrade.yaml
│       ├── api-migration.yaml
│       ├── security-audit.yaml
│       └── framework-upgrade.yaml
├── worker/
│   └── main.go                 # Temporal worker (registers workflows + activities)
├── server/
│   └── main.go                 # HTTP API server
└── agent/
    └── main.go                 # Sidecar agent binary (fleetlift-agent serve)

internal/
├── activity/                   # Temporal activity implementations
│   ├── constants.go            # 13 activity name constants + config validation
│   ├── agent.go                # AgentActivities (manifest, phase, result, steering)
│   ├── github.go               # GitHubActivities (PR creation via go-github/v62)
│   ├── knowledge.go            # CaptureKnowledge + EnrichPrompt
│   ├── manifest.go             # BuildManifest (model.Task → fleetproto.TaskManifest)
│   ├── report.go               # ValidateSchema (JSON Schema validation)
│   ├── sandbox.go              # SandboxActivities (provision, cleanup, verifiers)
│   ├── slack.go                # NotifySlack (via slack-go)
│   └── util.go                 # shellQuote helper
├── workflow/                   # Temporal workflow definitions
│   ├── transform.go            # Transform (entry), signal/query constants, grouped execution
│   ├── transform_v2.go         # TransformV2 (agent-mode workflow)
│   └── transform_group.go      # TransformGroup (child workflow per group)
├── model/                      # Data models
│   ├── task.go                 # Task, Repository, Execution, PullRequestConfig, etc.
│   └── knowledge.go            # KnowledgeItem, KnowledgeConfig, types/sources/status
├── agent/                      # Sidecar agent implementation
│   ├── fleetproto/
│   │   └── types.go            # Phase, SteeringAction, TaskManifest, AgentResult, paths
│   ├── pipeline.go             # Pipeline.Execute (main agent loop)
│   ├── protocol.go             # File I/O: WaitForManifest, WriteStatus, WriteResult, WaitForSteering
│   ├── clone.go                # cloneRepos, git config, AGENTS.md generation
│   ├── transform.go            # runAgenticTransformation, runDeterministicTransformation
│   ├── verify.go               # runVerifiers
│   ├── collect.go              # collectResults, getDiffs, readReport
│   ├── pr.go                   # createPullRequests (via gh CLI)
│   ├── validate.go             # ValidateManifest
│   ├── constants.go            # Poll intervals, defaults
│   └── deps.go                 # FileSystem + CommandExecutor interfaces
├── client/
│   └── starter.go              # Temporal client wrapper, TaskQueue="claude-code-tasks"
├── config/
│   └── loader.go               # Versioned YAML loader (v1), validation, → model.Task
├── state/
│   └── workflow.go             # Save/read last workflow ID (~/.fleetlift/last-workflow)
├── sandbox/                    # Sandbox provider interfaces
│   ├── provider.go             # Provider + AgentProvider interfaces
│   ├── factory.go              # ProviderFactory registration, NewProvider
│   └── opensandbox/            # OpenSandbox provider implementation
│       ├── register.go         # Auto-registers via init()
│       ├── provider.go         # Provider struct, Provision, Cleanup
│       ├── lifecycle.go        # Sandbox lifecycle management
│       └── execd.go            # File I/O via execd API
├── server/                     # HTTP API server
│   ├── server.go               # Chi router, API routes, SPA serving, metrics
│   ├── handlers_tasks.go       # GET endpoints (list, inbox, task, diff, logs, steering, progress)
│   ├── handlers_signals.go     # POST endpoints (approve, reject, cancel, steer, continue)
│   └── sse.go                  # SSE event stream for live task status
├── knowledge/
│   └── store.go                # Local filesystem store (~/.fleetlift/knowledge/), FilterByTags, LoadFromRepo
├── metrics/
│   ├── metrics.go              # Prometheus: activity_duration, activity_total, prs_created, sandbox_provision
│   └── interceptor.go          # Temporal WorkerInterceptor for auto-recording
└── logging/
    └── slog_adapter.go         # slog.Logger → Temporal log.Logger adapter
```

---

## 7. Configuration

### Environment Variables

| Variable | Used By | Default | Description |
|----------|---------|---------|-------------|
| `TEMPORAL_ADDRESS` | worker, CLI | `localhost:7233` | Temporal server address |
| `TEMPORAL_NAMESPACE` | worker, CLI | `default` | Temporal namespace |
| `OPENSANDBOX_DOMAIN` | worker | (required) | OpenSandbox API domain |
| `OPENSANDBOX_API_KEY` | worker | — | OpenSandbox API key |
| `SANDBOX_IMAGE` | worker | `claude-code-sandbox:latest` | Default sandbox container image |
| `GITHUB_TOKEN` | worker, agent | (required) | GitHub API token |
| `ANTHROPIC_API_KEY` | worker, agent | (required for agentic) | Anthropic API key |
| `GIT_USER_EMAIL` | worker | — | Git commit email |
| `GIT_USER_NAME` | worker | — | Git commit name |
| `METRICS_ADDR` | worker | `:9090` | Prometheus metrics endpoint |
| `FLEETLIFT_SERVER_ADDR` | server | `:8080` | HTTP API server listen address |
| `FLEETLIFT_BASE_PATH` | agent | `/workspace/.fleetlift` | Agent protocol file base path |
| `LOG_LEVEL` | worker | `info` | Logging verbosity (debug/info/warn/error) |
| `REQUIRE_CONFIG` | worker | `false` | If true, validate required env vars at startup |
| `DEBUG_NO_CLEANUP` | worker | `false` | Skip sandbox cleanup (debugging) |

### File System Paths

| Path | Purpose |
|------|---------|
| `~/.fleetlift/last-workflow` | Last workflow ID (for CLI default) |
| `~/.fleetlift/knowledge/{task-id}/item-{id}.yaml` | Local knowledge store (Tier 2) |
| `~/.fleetlift/templates/<name>.yaml` | User-defined task templates |
| `/workspace/.fleetlift/` | Agent protocol directory (inside sandbox) |
| `.fleetlift/knowledge/items/` | Repo-level knowledge (Tier 3, in transformation repos) |

### Temporal Configuration

| Setting | Value |
|---------|-------|
| Task Queue | `claude-code-tasks` |
| Default Workflow Timeout | Based on task `timeout` field (default 1h) |
| Heartbeat Interval | Used in WaitForAgentPhase for staleness detection |

---

## 8. Feature Status Matrix

| Feature | Status | Source Evidence |
|---------|--------|----------------|
| Agentic execution (Claude Code) | Implemented | `agent/transform.go`, `workflow/transform_v2.go` |
| Deterministic execution (Docker image) | Implemented | `agent/transform.go`, `model/task.go` |
| Transform mode (PR creation) | Implemented | `agent/pr.go`, `activity/github.go` |
| Report mode (discovery) | Implemented | `agent/collect.go`, `activity/report.go` |
| forEach multi-target iteration | Implemented | `model/task.go`, config loader |
| Transformation repo pattern | Implemented | `agent/clone.go`, config loader |
| Multi-repo support | Implemented | `workflow/transform.go` |
| Grouped execution | Implemented | `workflow/transform.go` (transformGrouped) |
| Failure thresholds | Implemented | `workflow/transform.go` |
| Pause/continue/abort | Implemented | `workflow/transform.go` signals |
| Retry failed groups | Implemented | CLI `retry` command |
| HITL approval (basic) | Implemented | `workflow/transform_v2.go` |
| HITL iterative steering | Implemented | `workflow/transform_v2.go`, `agent/pipeline.go` |
| Slack notifications | Implemented | `activity/slack.go` |
| Sidecar agent + file protocol | Implemented | `cmd/agent/`, `internal/agent/` |
| OpenSandbox provider | Implemented | `internal/sandbox/opensandbox/` |
| Knowledge capture | Implemented | `activity/knowledge.go` (CaptureKnowledge) |
| Knowledge enrichment | Implemented | `activity/knowledge.go` (EnrichPrompt) |
| Knowledge CLI (list/show/add/delete) | Implemented | `cmd/cli/knowledge.go` |
| Knowledge curation (review/commit) | Implemented | `cmd/cli/knowledge.go` |
| NL task creation (one-shot) | Implemented | `cmd/cli/create.go` |
| NL task creation (interactive) | Partial | `create.go` has `-i` flag, multi-turn loop |
| Template library (built-in) | Implemented | `cmd/cli/templates_assets.go` (4 templates) |
| User-defined templates | Implemented | `~/.fleetlift/templates/` support |
| Web UI (SPA) | Implemented | `web/`, `cmd/server/`, `internal/server/` |
| SSE live updates | Implemented | `internal/server/sse.go` |
| Prometheus metrics | Implemented | `internal/metrics/` |
| Structured logging (slog) | Implemented | `internal/logging/slog_adapter.go` |
| JSON Schema validation (reports) | Implemented | `activity/report.go` |
| GitHub repo discovery | Not implemented | Roadmap Phase 11.3 |
| Transformation repo registry | Not implemented | Roadmap Phase 11.4 |
| Scheduled/recurring tasks | Not implemented | Roadmap Phase 9.3 |
| Cost/token tracking | Not implemented | Roadmap Phase 9.4 |
| Knowledge efficacy tracking | Not implemented | Roadmap Phase 10.8 |
| S3/GCS report storage | Not implemented | Roadmap Phase 9.6 |
| Orphaned sandbox reaper | Not implemented | Roadmap Phase 8 |
| Backpressure config | Not implemented | Roadmap Phase 8 |
| Docker provider (local) | In agentbox | `github.com/tinkerloft/agentbox` (not in-tree) |
| Kubernetes provider | In agentbox | `github.com/tinkerloft/agentbox` (not in-tree) |

---

## 9. Known Drift

Documentation inaccuracies identified by comparing source code against existing docs.

### README.md

| Issue | Doc Says | Source Says |
|-------|----------|-------------|
| Project structure | Lists `internal/agent/protocol/` | Deleted; types moved to `internal/agent/fleetproto/` |
| Project structure | Lists `internal/sandbox/docker/` | Should be `internal/sandbox/opensandbox/` |
| "What's Coming" section | "Continual Learning Phase 10" listed as not implemented | Knowledge capture/enrichment IS implemented (Phase 10a–10.7 complete) |
| Default sandbox image | References `claude-sandbox:latest` | Source defaults to `claude-code-sandbox:latest` (`SANDBOX_IMAGE` env var) |

### docs/CLI_REFERENCE.md

| Issue | Details |
|-------|---------|
| Missing commands | `create`, `templates list`, `knowledge` (list/show/add/delete/review/commit) not documented |
| Non-existent global flags | Lists `--temporal-address`, `--namespace`, `--version` — none exist in source |
| Non-existent exit codes | Lists exit codes 2–5 with meanings — source does not define these |
| Missing `steer` command | Not documented |
| Missing `continue` command | Not documented |
| Missing `retry` command | Not documented |

### docs/TASK_FILE_REFERENCE.md

| Issue | Doc Says | Source Says |
|-------|----------|-------------|
| PR config field | `team_reviewers` | Not in source (`reviewers` only) |
| Task field | `owner` | Source uses `requester` |
| forEach template vars | `custom_field` / `{{.CustomField}}` | Source only supports `Name` and `Context` |
| timeout format | Shows integer minutes | Source uses Go duration strings (`"30m"`, `"1h"`) |

### docs/TROUBLESHOOTING.md

| Issue | Doc Says | Source Says |
|-------|----------|-------------|
| Parallel execution | References `parallel: true` YAML field | Actual mechanism is `--parallel` CLI flag (auto-generates groups) |

### docs/plans/OVERVIEW.md

| Issue | Details |
|-------|---------|
| Knowledge section | States "Not yet implemented" | Phase 10a–10.7 IS implemented (capture, enrich, CLI, curation) |
| NL task creation | States "Not yet implemented" | Phase 11 one-shot create IS implemented |
| Production deployment | States K8s provider "Not yet implemented" | K8s provider exists in agentbox; OpenSandbox is the active in-tree provider |

### docs/plans/SIDECAR_AGENT.md

| Issue | Doc Says | Source Says |
|-------|----------|-------------|
| Source files table | Lists `internal/agent/protocol/types.go` | Should be `internal/agent/fleetproto/types.go` |
| Source files table | Lists `internal/sandbox/docker/provider.go` | Should be `internal/sandbox/opensandbox/` |
| Sandbox provider env var | `SANDBOX_PROVIDER` selects docker/k8s | Source requires `OPENSANDBOX_DOMAIN`; factory uses OpenSandbox |

### docs/plans/ROADMAP.md

| Issue | Details |
|-------|---------|
| Phase 11.5 templates | Shows as unchecked `[ ]` | Templates ARE implemented (Phase 11.6 in IMPLEMENTATION_PLAN.md shows complete) |
| Active branch note | "Active branch: feat/agentbox-split (merge pending)" | May be stale if already merged to main |

### docs/plans/IMPLEMENTATION_PLAN.md

| Issue | Details |
|-------|---------|
| Recommended Next Steps | Lists "Phase 11.1 interactive multi-turn" as struck through but also "Phase 11.3/11.5/11.6: Repo discovery, templates" as deferred — 11.6 IS implemented |
| Phase 6b description | References "Docker + K8s providers" | Active provider is OpenSandbox; Docker/K8s are in agentbox |

### General Patterns

- Multiple docs reference `internal/agent/protocol/` (deleted package) — should be `internal/agent/fleetproto/`
- Multiple docs reference Docker provider as in-tree — only OpenSandbox is registered in-tree
- Feature status tables in README and OVERVIEW.md are stale relative to Phase 10 and 11 completion
- No doc covers the HTTP API server endpoints (`internal/server/`) comprehensively
