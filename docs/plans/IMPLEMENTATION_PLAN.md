# Implementation Plan

Incremental implementation phases for the code transformation and discovery platform.

> **Last Updated**: 2026-03-08 (Phase 10 complete)
>
> **Note**: Implementation uses Task/Campaign terminology aligned with the design documents.
>
> **Vision**: Managed Turbolift with two execution backends (container images for deterministic
> transforms, Claude Code for agentic transforms) and two modes (transform for PRs, report for
> discovery). Sandbox provider: OpenSandbox (`internal/sandbox/opensandbox/`). See DESIGN.md for full architecture and OVERVIEW.md for use cases.

---

## Phase 1: Local MVP ✅ Complete

**Goal**: Single-repo agentic transformation running locally.

The project is a Go module with Makefile targets. Core data models (`TransformTask`, `Repository`, `Verifier`, `TransformResult`) are defined in `internal/model/`. The sandbox `AgentProvider` interface lives in `internal/sandbox/`; the only registered implementation is OpenSandbox (`internal/sandbox/opensandbox/`). The `Transform` Temporal workflow orchestrates provisioning, cloning, running Claude Code, running verifiers, and cleanup. The CLI supports `fleetlift run` with `--repo`, `--prompt`, `--file`, and `--verifier` flags, plus `fleetlift status`.

### Deliverable

```bash
fleetlift run \
  --repo https://github.com/example/test-repo.git \
  --prompt "Add input validation" \
  --verifier "build:go build ./..." \
  --verifier "test:go test ./..."
```

---

## Phase 2: PR Creation & Multi-Repo ✅ Complete

**Goal**: Create pull requests, support multiple repositories per task.

GitHub PR creation is implemented via the `gh` CLI (token from env), with a `CreatePullRequest` activity that creates a branch, pushes changes, and opens the PR. The workflow iterates over multiple repositories using a groups-based model with a `max_parallel` field. Task results include PR URLs and file modification counts. The CLI accepts multiple `--repos` values (comma-separated), supports `--output json/table`, and includes a `fleetlift list` command.

### Deliverable

```bash
fleetlift run \
  --repo https://github.com/org/service-a.git \
  --repo https://github.com/org/service-b.git \
  --prompt "Update API v1 to v2" \
  --verifier "test:npm test"
# Creates PRs in both repos
```

---

## Phase 3: Deterministic Transformations ✅ Complete

**Goal**: Support image-based deterministic transformations.

An `ExecuteDeterministic` activity runs a transformation container image at `/workspace`, detects modifications via `git status --porcelain`, and runs verifiers. If no changes are detected, the task succeeds with an empty PR list. The `TransformTask` model gains `TransformMode` (`agentic`/`deterministic`), `TransformImage`, `TransformArgs`, and `TransformEnv` fields. The CLI adds `--image`, `--args`, and `--env` flags; YAML task files support `mode`, `image`, `args`, and `env`.

### Deliverable

```bash
fleetlift run \
  --repo https://github.com/org/service.git \
  --image openrewrite/rewrite:latest \
  --args "rewrite:run" \
  --args "-Drewrite.activeRecipes=org.openrewrite.java.logging.log4j.Log4j1ToLog4j2" \
  --verifier "build:mvn compile"
```

---

## Phase 4: Report Mode (Discovery) ✅ Complete

**Goal**: Support distributed code analysis and discovery across repositories without creating PRs.

Report mode (`mode: report`) skips PR creation and instead collects the agent's output from `/workspace/REPORT.md`. A `CollectReport` activity parses YAML frontmatter (between `---` delimiters) from the markdown and stores both structured data and prose body in the result. The `output.schema` field in the task spec enables JSON Schema validation of the frontmatter. The workflow branches on mode; report-mode prompts are automatically appended with output instructions.

### Deliverable

```yaml
# discovery-task.yaml
version: 1
id: auth-audit
title: "Authentication security audit"
mode: report

repositories:
  - url: https://github.com/org/service-a.git
  - url: https://github.com/org/service-b.git

execution:
  agentic:
    prompt: |
      Analyze this repository's authentication implementation.
      Write your report to /workspace/REPORT.md with YAML frontmatter.

    output:
      schema:
        type: object
        properties:
          auth_library: { type: string }
          score: { type: integer }
          issues: { type: array }
```

```bash
# Run discovery
fleetlift run --file auth-audit.yaml

# View full reports (markdown)
fleetlift reports auth-audit-xyz789

# Export structured frontmatter as JSON
fleetlift reports auth-audit-xyz789 --format json > results.json
```

---

## Phase 4b: forEach Multi-Target Discovery ✅ Complete

**Goal**: Enable iterating over multiple targets within a single repository for fine-grained discovery.

The `for_each` field on a Task defines named iteration targets with optional context. The workflow loops over targets within each repository, executes the agent once per target with `{{.Name}}` and `{{.Context}}` substituted in the prompt, and collects each report as `REPORT-{target.name}.md`. Results are aggregated as `ForEachExecution` entries in `RepositoryResult`. Target names are validated as filesystem-safe; `for_each` is an error with `mode: transform`. The CLI `reports` command supports `--target <name>` filtering and JSON export with target-level grouping.

### Deliverable

```bash
# Run forEach discovery
fleetlift run --file api-audit.yaml

# View all target reports
fleetlift reports <workflow-id>

# Export as JSON for aggregation
fleetlift reports <workflow-id> --output json
```

---

## Phase 4c: Transformation Repository Support ✅ Complete

**Goal**: Enable separation of "recipe" (transformation repo) from "targets" for reusable skills and tools.

The `transformation` field on a Task specifies a "recipe" repository (with skills, tools, CLAUDE.md) and a `targets` field lists repos to analyze or transform. The `CloneRepositories` activity clones the transformation repo to `/workspace/` and targets to `/workspace/targets/{name}/`; transformation repo setup runs first. All downstream activities (`RunVerifiers`, `CreatePullRequest`, `CollectReport`) use the correct base path based on the layout. The `repositories` field alone still works unchanged.

### Deliverable

```bash
# Workspace layout with transformation repo:
# /workspace/
# ├── .claude/skills/     # From transformation repo
# ├── CLAUDE.md           # From transformation repo
# └── targets/
#     ├── server/         # Target repos
#     └── client/
```

---

## Phase 5: Grouped Execution with Failure Thresholds ✅ Complete

**Goal**: Enable fleet-wide operations with failure handling and retry capabilities.

Groups of repositories execute in parallel up to `max_parallel`. After each group completes, the failure percentage is calculated and compared to `failure.threshold_percent`. On breach, the workflow either pauses (waits up to 24 hours for a `continue` signal) or aborts; in-flight groups always complete. A query handler exposes real-time `ExecutionProgress`. The CLI adds `fleetlift continue --workflow-id` (with `--skip-remaining`) and `fleetlift retry --workflow-id --failed-only`.

### Deliverable

```yaml
groups:
  - name: platform-team
    repositories:
      - url: https://github.com/org/auth-service.git

max_parallel: 3

failure:
  threshold_percent: 20  # Pause if >20% of groups fail
  action: pause          # "pause" or "abort"
```

```bash
fleetlift status --workflow-id transform-fleet-slog-migration
# Progress: 8/12 groups complete, Failed: 2 (16%)

fleetlift continue --workflow-id transform-fleet-slog-migration

fleetlift retry \
  --file fleet-slog-migration.yaml \
  --workflow-id transform-fleet-slog-migration \
  --failed-only
```

---

## Phase 6: Sandbox Sidecar Agent & Kubernetes Provider ✅ Complete

**Goal**: Replace exec-per-step sandbox interaction with a sidecar agent pattern. Worker becomes non-blocking. Enables Kubernetes sandbox support with direct Job management (no CRD/controller).

### 6a. Sidecar Agent & Protocol ✅ Complete

A `fleetlift-agent` binary runs inside the sandbox as the container entrypoint. The worker submits a file-based manifest and polls for completion rather than blocking a goroutine for the full run duration. The `AgentProvider` interface in `internal/sandbox/` defines `SubmitManifest`, `PollStatus`, `ReadResult`, and `SubmitSteering`; the OpenSandbox provider implements these via its execd API. A `TransformV2` workflow orchestrates the submit→poll→HITL steering loop→read results lifecycle. New activities (`SubmitTaskManifest`, `WaitForAgentPhase`, `ReadAgentResult`, `SubmitSteeringAction`) are registered in the worker.

### 6b. Sandbox Provider ✅ Complete

The sole registered provider is **OpenSandbox** (`internal/sandbox/opensandbox/`), which implements the full `AgentProvider` interface against the OpenSandbox REST API (lifecycle + execd). Docker and Kubernetes provider implementations are available in `github.com/tinkerloft/agentbox` if needed. Provider configuration is via env vars (`OPENSANDBOX_DOMAIN`, `OPENSANDBOX_API_KEY`, etc.).

### Deliverable

```bash
# Agent binary built alongside worker and CLI
make build
# bin/fleetlift-agent  (statically compiled, ~10MB)

# Worker starts TransformV2 workflow for agent-mode tasks
fleetlift run --file task.yaml --agent-mode
```

---

## Phase 7: Observability ✅ Complete

**Goal**: Metrics, logging, and dashboards for operational visibility.

Prometheus metrics are instrumented in `internal/metrics/` and exposed on `:9090` (env: `METRICS_ADDR`) by the worker and on the API server's main port. Key metrics include per-activity duration histograms and counters, sandbox provision time, and PR creation totals. Structured JSON logging uses slog with a `SlogAdapter` bridging to Temporal's logger interface; verbosity is controlled by `LOG_LEVEL`. A Grafana dashboard (`deploy/grafana/fleetlift-dashboard.json`) covers activity rates, success rates, p95 durations, and sandbox provision time. Alerting rules (`deploy/prometheus/alerts.yaml`) fire on high failure rates, slow provisioning, and worker inactivity.

---

## Phase 8: Security Hardening 🔄 Partial

**Goal**: Production-grade security and operational resilience.

Network policies, RBAC, secret management, audit logging, scaling infrastructure, and deployment artifacts (Helm/Terraform/Kustomize) are delegated to OpenSandbox and the ops/infra layer. The remaining fleetlift-owned items:

- [ ] **Orphaned sandbox reaper**: Periodic process that queries OpenSandbox for sandboxes labelled with fleetlift workflow IDs and cleans up any whose workflow is in a terminal state. Covers worker crashes between provision and cleanup.
- [ ] **Backpressure / resource awareness**: Configure Temporal `MaxConcurrentActivityExecutionSize` to bound concurrent sandbox provisioning per worker. Combine with K8s `ResourceQuota` per sandbox namespace to prevent cluster overcommit.

---

## Phase 9: Advanced Features

**Goal**: Enhanced capabilities for production usage.

### 9.1 Human-in-the-Loop (Basic) ✅ Complete

Approve/reject/cancel Temporal signals are implemented with a `NotifySlack` activity for notifications and a 24-hour `AwaitWithTimeout` for the approval gate.

### 9.2 Human-in-the-Loop (Iterative Steering) ✅ Complete

A `steer` signal triggers a re-run of Claude Code with the steering prompt appended, preserving conversation context across iterations. `GetDiff` and `GetVerifierOutput` activities return the current diff and test output. Query handlers expose diff, verifier logs, and steering state for the CLI and web UI. Max iterations is configurable via `max_steering_iterations` (default 5). CLI commands: `fleetlift diff --workflow-id <id>`, `fleetlift logs --workflow-id <id>`, `fleetlift steer --workflow-id <id> --prompt "..."`.

### 9.3 Scheduled Tasks

- [ ] Temporal schedules for recurring tasks
- [ ] Cron-like syntax support
- [ ] Example: weekly dependency update transforms

### 9.4 Cost Tracking

- [ ] Track API token usage per task (Claude API)
- [ ] Compute cost attribution (sandbox CPU/memory hours)
- [ ] Per-team/namespace cost rollup
- [ ] Budget alerts and quotas

### 9.5 Web UI ✅ Complete

A Go API server (`cmd/server`) using chi exposes REST + SSE endpoints wrapping the Temporal client. Endpoints cover task list, inbox, task detail, diff, logs, steering state, progress, and signals (approve/reject/cancel/steer/continue). SSE live updates stream on `GET /tasks/{id}/events`. A React + TypeScript SPA (`web/`) built with Vite, shadcn/ui, and Tailwind provides an Inbox page, Task List page, and Task Detail page with diff viewer (collapsible per file), steering panel (approve/reject/steer with iteration history), verifier logs, and group progress. The frontend is embedded into the Go binary via `web/embed.go`.

### 9.6 Report Storage Options

- [ ] Inline in result (default, for small reports)
- [ ] S3/GCS backend for large-scale discovery (100+ repos)
- [ ] Config: `report_storage.backend`

---

## Phase 10: Continual Learning / Knowledge Items 🔄 Mostly Complete

**Goal**: Capture knowledge from successful transformations and reuse it to improve future runs.

### 10.1–10.5 Core Knowledge System ✅ Complete

`KnowledgeItem` structs (types: `pattern`, `correction`, `gotcha`, `context`) are stored as YAML files in a two-tier system: Tier 2 local store at `~/.fleetlift/knowledge/{task-id}/` (auto-captured) and Tier 3 transformation repo at `.fleetlift/knowledge/items/` (curated, shared). After each approved transformation, `ActivityCaptureKnowledge` uses Claude to analyze the execution history (original prompt, steering corrections, diff, verifier output) and extract knowledge items written to Tier 2. Before agent execution, `ActivityEnrichPrompt` loads relevant items (tag-matched, capped at 10 items / ~2000 tokens) and appends a "Lessons from previous runs" section to the prompt. Both steps are non-blocking and skippable via `knowledge.capture_disabled`/`knowledge.enrich_disabled` task config.

### 10.6 Workflow Integration ✅ Complete

- Knowledge capture and prompt enrichment are wired into the single-repo `Transform` workflow.
- Grouped execution: `Transform` orchestrates N groups via `TransformGroup` children in parallel batches; knowledge capture runs per-group through `TransformV2`; all groups contribute to the same knowledge pool.

### 10.7 Task YAML Extensions ✅ Complete

```yaml
knowledge:
  capture_disabled: false
  enrich_disabled: false
  max_items: 10
  tags: [go, logging]
```

### 10.8 Knowledge Efficacy Tracking

- [ ] Track per-item usage: times injected, success rate, avg steering rounds
- [ ] `fleetlift knowledge stats` — show efficacy metrics
- [ ] Auto-deprecate items with low confidence after N uses with no improvement

### Deliverable

```bash
$ fleetlift knowledge list --task-id transform-slog-migration-abc
ID                        TYPE        CONFIDENCE  SUMMARY
slog-test-helpers-01      correction  0.95        Update test helpers that wrap the logger
slog-go-kit-compat-01     gotcha      0.80        go-kit repos need different logger interface
slog-mod-tidy-01          pattern     0.90        Run go mod tidy after import changes

$ fleetlift knowledge review --task-id transform-slog-migration-abc

$ fleetlift knowledge commit --repo ./slog-migration-toolkit
# Wrote 3 items to ./slog-migration-toolkit/.fleetlift/knowledge/items/

$ fleetlift run -f slog-migration-batch2.yaml
# Prompt enriched with 3 knowledge items — 0 steering rounds needed (vs 2 previously)
```

---

## Phase 11: Natural Language Task Creation 🔄 Core Complete

**Goal**: Let users create task YAML files through natural language conversation, lowering the barrier to entry.

### 11.1 Interactive Create Command

- [ ] `fleetlift create` — starts interactive multi-turn task creation session (intent → targets → details → verification → review → output)

### 11.2 One-Shot Create ✅ Complete

`fleetlift create --describe "..."` sends the description to Claude with the full task YAML schema and canonical examples as context, generates valid YAML, validates it, shows it for review, and writes the file. `--output task.yaml` controls the filename. `$EDITOR` is opened if the user selects "edit" at the review prompt.

- [ ] `--run` flag to immediately execute after generation
- [ ] `--dry-run` flag: show generated YAML without saving

### 11.3 GitHub Repository Discovery

- [ ] Integrate with `gh` CLI or GitHub API for repo discovery during `create`
- [ ] Support patterns: "all repos in acme-org", "repos matching service-*", "repos with go.mod"
- [ ] Cache org repo list locally at `~/.fleetlift/cache/repos/`

### 11.4 Schema Context Bundle ✅ Complete

The full task YAML schema and 5 canonical examples (single-repo agentic, multi-repo grouped, report mode with forEach, deterministic, transformation repo) are embedded in the CLI binary and used as the system prompt for Claude calls in the create flow.

### 11.5 Transformation Repo Suggestions

- [ ] Optional registry at `~/.fleetlift/registries/repos.yaml` with tag-matched repo suggestions during `create`
- [ ] If selected repo has knowledge items, surface known gotchas and offer to include them in the prompt

### 11.6 Template Library

- [ ] Ship built-in templates embedded in binary: `dependency-upgrade`, `api-migration`, `security-audit`, `framework-upgrade`
- [ ] `fleetlift create --template api-migration` and `fleetlift templates list`
- [ ] User-defined templates at `~/.fleetlift/templates/`

### Deliverable

```bash
# One-shot mode
$ fleetlift create \
  --describe "Audit authentication in all Go services in acme-corp, report mode" \
  --output auth-audit.yaml
# Generates, validates, and writes auth-audit.yaml
```

---

## Summary

| Phase | Focus | Key Deliverable | Status |
|-------|-------|-----------------|--------|
| 1 | Local MVP | Single-repo agentic task with Docker | ✅ Complete |
| 2 | PR Creation | Multi-repo with GitHub PRs | ✅ Complete |
| 3 | Deterministic | Docker-based transformations | ✅ Complete |
| 4 | Report Mode | Discovery and analysis (no PRs) | ✅ Complete |
| 4b | forEach Discovery | Multi-target iteration within repos | ✅ Complete |
| 4c | Transformation Repo | Reusable skills, recipe/targets separation | ✅ Complete |
| 5 | **Grouped Execution** | Failure thresholds, pause/continue, retry | ✅ Complete |
| 6a | **Sidecar Agent** | Agent binary + file-based protocol + TransformV2 workflow | ✅ Complete |
| 6b | **Kubernetes Provider** | K8s Jobs, RBAC, NetworkPolicy | ✅ Complete |
| 7 | **Observability** | Metrics, logging, dashboards | ✅ Complete |
| 8 | **Security** | Orphaned sandbox reaper + backpressure config (infra delegated to agentbox) | 🔄 Partial |
| 9.1 | **HITL (Basic)** | Approve/reject signals, Slack notifications | ✅ Complete |
| 9.2 | **HITL (Steering)** | Iterative steering with diff/logs/steer commands | ✅ Complete |
| 9.3 | **Scheduled tasks** | Temporal cron-style recurring tasks | ⬜ Deferred |
| 9.4 | **Cost tracking** | API token + compute attribution | ⬜ Deferred |
| 9.5 | **Web UI** | Inbox, diff review, approval/steering dashboard | ✅ Complete |
| 9.6 | **Report storage** | S3/GCS backend for large-scale discovery | ⬜ Deferred |
| 10 | **Continual Learning** | Knowledge capture, enrichment, curation | ✅ Complete (10.8 deferred) |
| 11 | **NL Task Creation** | One-shot create, schema bundle, validation | 🔄 Core complete (11.1, 11.3, 11.5, 11.6 deferred) |

Each phase builds on the previous and delivers working functionality.

### Recommended Next Steps

**Reliability Track:**
1. **Phase 8.5** - Orphaned sandbox reaper (worker crashes between provision/cleanup)
2. **Phase 8.5** - Backpressure config (Temporal `MaxConcurrentActivityExecutionSize`)

**Polish Track:**
1. **Phase 11.2** - Add `--run` flag to `fleetlift create` (trivial; finishes phase)

**Deferred (schedule based on usage/feedback):**
- Phase 9.3: Scheduled/recurring tasks
- Phase 9.4: Cost tracking
- Phase 10.8: Knowledge efficacy tracking
- Phase 11.1/11.3/11.5/11.6: Full interactive create, repo discovery, templates

> **Status Note**: Core platform capabilities are complete. All product features (agentic/deterministic
> transforms, report mode, grouped execution, failure handling, retry, HITL iterative steering, sidecar
> agent, knowledge capture/enrichment/curation, natural language task creation) are implemented.
> Infrastructure concerns (network policies, RBAC, secrets, scaling, deployment) are delegated to
> `github.com/tinkerloft/agentbox`.

---

### Key Changes from Previous Plan

| Previous | Current | Rationale |
|----------|---------|-----------|
| Phase 4: CRD & Controller | Phase 4: Report Mode | Report mode is core functionality; CRD is optional convenience layer |
| Phase 8: Agent Sandbox | Removed | Plain K8s Jobs are sufficient; Agent Sandbox adds complexity without clear benefit |
| Phase 9.6: Report Mode | Phase 4: Report Mode | Promoted to core phase—discovery is first-class, not an afterthought |
| Campaign as separate concept | Phase 5: Grouped Execution | Simpler model: failure handling at Task level with groups instead of separate Campaign type |
| CRD/Controller pattern | Phase 6: Direct Job management | Simpler architecture: worker creates Jobs directly, no CRD or controller needed |
| Deterministic via `docker run` in sandbox | Direct command execution + init container injection | No Docker-in-Docker; agent binary injected at deploy time; one mode of operation for agent |
