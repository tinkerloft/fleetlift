# Implementation Plan

Incremental implementation phases for the code transformation and discovery platform.

> **Last Updated**: 2026-02-19 (Phase 9.5 Web UI complete)
>
> **Note**: Implementation uses Task/Campaign terminology aligned with the design documents.
>
> **Vision**: Managed Turbolift with two execution backends (Docker images for deterministic
> transforms, Claude Code for agentic transforms) and two modes (transform for PRs, report for
> discovery). See DESIGN.md for full architecture and OVERVIEW.md for use cases.

---

## Phase 1: Local MVP

**Goal**: Single-repo agentic transformation running locally with Docker.

### 1.1 Project Setup

- [x] Initialize Go module
- [x] Set up directory structure
- [x] Add Makefile with common targets
- [x] Create Dockerfile.sandbox (base image with git, Claude Code CLI)

### 1.2 Data Model

- [x] Define `Task` struct *(implemented as `TransformTask`)*
- [x] Define `RepositoryTarget` struct *(implemented as `Repository` with Setup field)*
- [x] Define `Transformation` (Agentic only for now) *(prompt embedded in TransformTask)*
- [x] Define `Verifier` struct *(with VerifierResult, VerifiersResult)*
- [x] Define `TaskResult` and `RepositoryResult` *(implemented as `TransformResult`, `ClaudeCodeResult`)*

### 1.3 Docker Sandbox Provider

- [x] Implement `Provider` interface *(implemented as `docker.Client`)*
- [x] `Provision()` - create container with mounted workspace
- [x] `Exec()` - run commands in container
- [x] `Cleanup()` - stop and remove container
- [x] Unit tests with mock Docker client *(integration tests in client_test.go)*

### 1.4 Temporal Workflow (Basic)

- [x] Set up local Temporal server (docker-compose) *(docker-compose.yaml with Postgres)*
- [x] Implement `ExecuteTask` workflow *(implemented as `Transform` workflow)*
- [x] Implement activities:
  - [x] `ProvisionSandbox`
  - [x] `CloneRepository` *(implemented as `CloneRepositories` with setup)*
  - [x] `RunSetup` *(integrated into CloneRepositories)*
  - [x] `ExecuteAgentic` (run Claude Code) *(implemented as `RunClaudeCode`)*
  - [x] `CleanupSandbox`
  - [x] `RunVerifiers` *(new activity)*
- [x] Workflow tests with Temporal test framework *(basic tests in bugfix_test.go)*

### 1.5 Claude Code Integration

- [x] Build prompt from `AgenticTransform` *(buildPrompt function)*
- [x] Append verifier instructions to prompt
- [x] Execute Claude Code CLI in sandbox
- [x] Capture output
- [x] Run verifiers as final gate *(RunVerifiers activity called before PR creation)*

### 1.6 CLI (Minimal)

- [x] `fleetlift run --repo <url> --prompt <prompt>` *(both `start` and `run` commands)*
- [x] `fleetlift run --file task.yaml` *(--file flag with YAML parsing)*
- [x] `fleetlift status <task-id>` *(implemented as `status --workflow-id`)*
- [x] YAML task file parsing *(TaskFile struct with loadTaskFile function)*

### Deliverable

Run an agentic task locally:
```bash
fleetlift run \
  --repo https://github.com/example/test-repo.git \
  --prompt "Add input validation" \
  --verifier "build:go build ./..." \
  --verifier "test:go test ./..."
```

---

## Phase 2: PR Creation & Multi-Repo

**Goal**: Create pull requests, support multiple repositories per task.

### 2.1 GitHub Integration

- [x] GitHub client setup (go-github or gh CLI) *(uses gh CLI via shell)*
- [x] `CreatePullRequest` activity
  - [x] Create branch
  - [x] Push changes
  - [x] Open PR with title/body
- [x] Handle authentication (token from env)

### 2.2 Multi-Repository Support

- [x] Update workflow to iterate over repositories
- [x] Flexible execution patterns via groups *(unified groups-based model, max_parallel field)*
- [x] Aggregate results across repos

### 2.3 Task Result Improvements

- [x] Track files modified *(in ClaudeCodeResult.FilesModified)*
- [x] Include PR URLs in result *(in TransformResult.PullRequests)*
- [x] Better error reporting

### 2.4 CLI Enhancements

- [x] `--repo` flag accepts multiple values *(comma-separated in `--repos`)*
- [x] Output formatting (JSON, table) *(`--output` flag with json/table options)*
- [x] `fleetlift list` command

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

## Phase 3: Deterministic Transformations

**Goal**: Support Docker-based deterministic transformations.

### 3.1 Deterministic Transform Execution

- [x] `ExecuteDeterministic` activity *(new `internal/activity/deterministic.go`)*
- [x] Pull transformation image *(via docker run in sandbox)*
- [x] Mount workspace into transformation container *(bind mount at /workspace)*
- [x] Run transformation
- [x] Capture output/errors
- [x] Detect modified files via `git status --porcelain`

### 3.2 CLI Support

- [x] `--image` flag for deterministic mode
- [x] `--args` flag for transformation arguments
- [x] `--env` flag for environment variables
- [x] YAML task file support (`mode`, `image`, `args`, `env` fields)

### 3.3 Validation

- [x] Run verifiers after deterministic transform
- [x] Skip PR if no changes detected *(returns success with empty PR list)*
- [x] Skip human approval for deterministic mode *(pre-vetted transforms)*

### 3.4 Data Model

- [x] `TransformMode` type (`agentic`, `deterministic`)
- [x] `DeterministicResult` struct
- [x] New `TransformTask` fields: `TransformMode`, `TransformImage`, `TransformArgs`, `TransformEnv`

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

## Phase 4: Report Mode (Discovery)

**Goal**: Support distributed code analysis and discovery across repositories without creating PRs.

> **Design Rationale**: Report mode enables discovery—analyzing repositories to collect
> structured data. This is essential for security audits, dependency inventories, and
> pre-migration analysis.

### 4.1 Core Report Mode

- [x] Add `mode` field to Task: `transform` (default) or `report`
- [x] Skip PR creation workflow when `mode: report`
- [x] Capture agent stdout as structured output
- [x] Store report in Task result
- [x] Update workflow to branch based on mode

### 4.2 Report Output Collection

- [x] `CollectReport` activity - read `/workspace/REPORT.md` from sandbox
- [x] Parse frontmatter (YAML between `---` delimiters) from markdown
- [x] Store both structured frontmatter and prose body in result
- [x] Append output instructions to prompt for report mode tasks

### 4.3 Frontmatter Schema Validation

- [x] Parse `output.schema` (JSON Schema) from Task spec
- [x] Validate frontmatter against schema
- [x] Report validation errors in result
- [x] Support common types: object, array, string, number, boolean, enum

### 4.4 forEach: Multi-Target Discovery

> **Moved to Phase 4b** - See dedicated phase below for forEach implementation.

### 4.5 CLI Support

- [x] `fleetlift run --mode report --prompt "..."` - run discovery
- [x] `fleetlift reports <workflow-id>` - view collected reports
- [x] `fleetlift reports <workflow-id> --output json` - export reports
- [x] Update `status` command to show reports for report-mode tasks

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

      Write your report to /workspace/REPORT.md with:
      1. YAML frontmatter containing structured data (auth_library, score, issues)
      2. Detailed markdown analysis with findings and recommendations

    output:
      schema:  # Validates the frontmatter
        type: object
        properties:
          auth_library: { type: string }
          score: { type: integer }
          issues: { type: array }
```

Agent writes `/workspace/REPORT.md`:
```markdown
---
auth_library: custom
score: 4
issues:
  - severity: high
    location: config/secrets.yaml:12
---

# Authentication Audit: service-b

## Summary
This service uses a custom auth implementation...

## Findings
### 1. Hardcoded Credentials (High)
Found API key in config/secrets.yaml...
```

```bash
# Run discovery
fleetlift run --file auth-audit.yaml

# View full reports (markdown)
fleetlift reports auth-audit-xyz789
# Displays each repository's full report

# Export structured data (frontmatter) for aggregation
fleetlift reports auth-audit-xyz789 --format json > results.json
# [{"repository": "service-a", "frontmatter": {"auth_library": "oauth2", ...}}, ...]
```

---

## Phase 4b: forEach Multi-Target Discovery

**Goal**: Enable iterating over multiple targets within a single repository for fine-grained discovery.

> **Design Rationale**: forEach enables analyzing multiple components within a repo (e.g., each
> microservice in a monorepo, each API endpoint, each config file) with separate reports per target.
> This is useful for detailed audits where a single repo contains many analyzable units.

### 4b.1 Data Model

- [x] Add `for_each[]` field to Task for defining iteration targets *(already in model.Task)*
- [x] Add `ForEachExecution` struct to track per-target results *(model.ForEachExecution)*
- [x] Add `ForEachResults` to `RepositoryResult` *(RepositoryResult.ForEachResults)*

### 4b.2 Template Substitution

- [x] Parse `{{.Name}}` and `{{.Context}}` variables in prompt *(substitutePromptTemplate function)*
- [x] Inject target context into each execution *(buildPromptForTarget function)*
- [x] Error handling for invalid template syntax

### 4b.3 Workflow Changes

- [x] Loop over `for_each` targets within each repository *(in Transform workflow report mode section)*
- [x] Execute agent once per target with substituted prompt
- [x] Collect report per target (namespaced as `REPORT-{target.name}.md`)
- [x] Aggregate `ForEachExecution` results in `RepositoryResult`
- [x] Continue on partial failures (record error, process next target)

### 4b.4 CLI Support

- [x] Display per-target reports in `fleetlift reports` command
- [x] Support `--target <name>` filter for viewing specific target reports
- [x] JSON export includes target-level grouping

### 4b.5 Config Validation

- [x] Validate target names contain only `[a-zA-Z0-9_-]` (safe for filenames)
- [x] Error if `for_each` used with `mode: transform`

### Deliverable

```yaml
# forEach discovery example
version: 1
id: api-endpoint-audit
title: "Audit all API endpoints"
mode: report

repositories:
  - url: https://github.com/org/api-gateway.git

for_each:
  - name: users-api
    context: "The /users endpoint in src/routes/users.go"
  - name: orders-api
    context: "The /orders endpoint in src/routes/orders.go"
  - name: payments-api
    context: "The /payments endpoint in src/routes/payments.go"

execution:
  agentic:
    prompt: |
      Analyze {{.name}}: {{.context}}

      Check for: authentication, rate limiting, input validation.
      Write your findings to /workspace/REPORT.md
```

```bash
# Run forEach discovery
fleetlift run --file api-audit.yaml

# View all target reports
fleetlift reports <workflow-id>
# Repository: api-gateway
#   Target: users-api
#     Frontmatter: {auth: "jwt", rate_limit: true, ...}
#   Target: orders-api
#     Frontmatter: {auth: "jwt", rate_limit: false, ...}
#   Target: payments-api
#     Frontmatter: {auth: "jwt", rate_limit: true, ...}

# Export as JSON for aggregation
fleetlift reports <workflow-id> --output json
```

---

## Phase 4c: Transformation Repository Support

**Goal**: Enable separation of "recipe" (transformation repo) from "targets" for reusable skills and tools.

> **Design Rationale**: Transformation repositories allow centralized skills, tools, and configuration
> to be applied across multiple target repositories. This is essential for endpoint classification,
> security audits, and multi-repo analysis where the "how" (skills) should be separate from the
> "what" (target repos).

### 4c.1 Data Model

- [x] Add `Transformation *Repository` field to Task (the "recipe" repo)
- [x] Add `Targets []Repository` field (repos to analyze/transform)
- [x] Add `UsesTransformationRepo()` helper method
- [x] Add `GetEffectiveRepositories()` to return targets or repositories based on mode

### 4c.2 Config Loader

- [x] Parse `transformation` and `targets` fields from YAML
- [x] Validation: error if both `transformation` and `repositories` are used
- [x] Validation: error if `targets` without `transformation`
- [x] Backward compatible: `repositories` alone still works

### 4c.3 Workspace Layout

- [x] Transformation mode: clone transformation repo to `/workspace/`, targets to `/workspace/targets/`
- [x] Legacy mode: clone repos directly to `/workspace/{name}`
- [x] Run transformation repo setup commands first
- [x] Run target repo setup commands in `/workspace/targets/{name}`

### 4c.4 Activity Updates

- [x] `CloneRepositories` - support transformation layout with new input struct
- [x] `RunVerifiers` - use correct base path based on layout
- [x] `CreatePullRequest` - use correct repo path based on layout
- [x] `CollectReport` - use correct report path based on layout

### 4c.5 Workflow Updates

- [x] Pass `UseTransformationLayout` flag to all relevant activities
- [x] Use `GetEffectiveRepositories()` for iteration
- [x] Update `generateAgentsMD()` to show transformation info
- [x] Update `buildPrompt()` to show correct paths

### Deliverable

```yaml
# transformation-task.yaml
version: 1
id: endpoint-classification
title: "Classify endpoints for removal"
mode: report

# Transformation repo with skills and tools
transformation:
  url: https://github.com/org/classification-tools.git
  branch: main
  setup:
    - npm install

# Target repos to analyze
targets:
  - url: https://github.com/org/api-server.git
    name: server
  - url: https://github.com/org/web-client.git
    name: client

for_each:
  - name: users-endpoint
    context: |
      Endpoint: GET /api/v1/users
      Location: targets/server/src/handlers/users.go:45

execution:
  agentic:
    prompt: |
      Use the endpoint-classification skill to analyze {{.Name}}.
      {{.Context}}

      Search for callers in /workspace/targets/
```

```bash
# Run with transformation repo
fleetlift run --file transformation-task.yaml

# Workspace layout:
# /workspace/
# ├── .claude/skills/     # From transformation repo
# ├── CLAUDE.md           # From transformation repo
# └── targets/
#     ├── server/         # Target repos
#     └── client/
```

---

## Phase 5: Grouped Execution with Failure Thresholds - COMPLETE

**Goal**: Enable fleet-wide operations with failure handling and retry capabilities.

> **Implementation Note**: Instead of implementing a separate Campaign concept, we enhanced
> Task execution with grouped execution, failure thresholds, and retry capabilities. This
> provides the same benefits with a simpler, more flexible model.

### 5.1 Data Model - COMPLETE

- [x] Add `FailureConfig` to Task for threshold and action
- [x] Add `GroupResult` to track per-group execution status
- [x] Add `ExecutionProgress` for real-time progress queries
- [x] Add `ContinueSignalPayload` for resume signals
- [x] Add `Groups` and `OriginalWorkflowID` to TaskResult

### 5.2 Grouped Execution with Failure Detection - COMPLETE

- [x] Track results incrementally as groups complete
- [x] Calculate failure percentage after each group
- [x] Pause execution when threshold exceeded (action: pause)
- [x] Abort execution when threshold exceeded (action: abort)
- [x] Allow in-flight groups to complete during pause/abort
- [x] Register query handler for execution progress

### 5.3 Pause/Continue/Retry - COMPLETE

- [x] Signal handler for continue with skip_remaining option
- [x] Wait for human decision during pause (24hr timeout)
- [x] Support cancellation during pause
- [x] CLI: `continue` command with --skip-remaining flag
- [x] CLI: `retry` command with --failed-only flag
- [x] Enhanced `status` command to show group progress

### 5.4 Config Loading - COMPLETE

- [x] Parse `failure.threshold_percent` and `failure.action` from YAML
- [x] Validate threshold is 0-100
- [x] Validate action is "pause" or "abort"
- [x] Helper methods on Task: `GetFailureThresholdPercent()`, `ShouldPauseOnFailure()`

### Deliverable

```yaml
version: 1
id: fleet-slog-migration
title: "Migrate to slog across all services"

groups:
  - name: platform-team
    repositories:
      - url: https://github.com/org/auth-service.git
      - url: https://github.com/org/user-service.git
  - name: payments-team
    repositories:
      - url: https://github.com/org/payment-gateway.git
  - name: notifications-team
    repositories:
      - url: https://github.com/org/email-service.git

execution:
  agentic:
    prompt: "Migrate from log.Printf to slog..."
    verifiers:
      - name: build
        command: ["go", "build", "./..."]

max_parallel: 3

failure:
  threshold_percent: 20  # Pause if >20% of groups fail
  action: pause          # "pause" or "abort"

timeout: 30m
```

```bash
# Run task with grouped execution
fleetlift run -f fleet-slog-migration.yaml

# Check progress (shows group-level detail)
fleetlift status --workflow-id transform-fleet-slog-migration
# Progress: 8/12 groups complete
# Failed: 2 (16%)

# If paused on threshold
fleetlift continue --workflow-id transform-fleet-slog-migration

# Retry failed groups after completion
fleetlift retry \
  --file fleet-slog-migration.yaml \
  --workflow-id transform-fleet-slog-migration \
  --failed-only
```

---

## Phase 6: Sandbox Sidecar Agent & Kubernetes Provider

**Goal**: Replace exec-per-step sandbox interaction with a sidecar agent pattern. Worker becomes non-blocking. Enables Kubernetes sandbox support with direct Job management (no CRD/controller).

> **Design Rationale**: The original exec-per-step approach blocks a worker goroutine for the full
> duration of each sandbox interaction (15+ minutes for Claude Code runs). The sidecar agent runs
> autonomously inside the sandbox, and the worker only submits a manifest and polls for results.
> For Kubernetes, the worker creates Jobs directly — no CRD or separate controller needed. The
> file-based protocol avoids etcd size limits (1.5MB) that would constrain CR-based approaches.
>
> **See [SIDECAR_AGENT.md](./SIDECAR_AGENT.md) for full architecture details.**

### 6a. Sidecar Agent & Protocol — COMPLETE

- [x] Define file-based protocol types (manifest, status, result, steering)
- [x] Build `fleetlift-agent` binary (pipeline, clone, transform, verify, collect, PR)
- [x] Agent entrypoint: `fleetlift-agent serve`
- [x] Extend `Provider` interface with task ops (SubmitManifest, PollStatus, ReadResult, SubmitSteering)
- [x] Implement task ops in Docker provider (via CopyTo/CopyFrom)
- [x] New activities: SubmitTaskManifest, WaitForAgentPhase, ReadAgentResult, SubmitSteeringAction
- [x] `TransformV2` workflow (submit manifest → poll → HITL steering loop → read results)
- [x] Manifest builder: convert `model.Task` → `protocol.TaskManifest`
- [x] Register TransformV2 + new activities in worker
- [x] Update Makefile with `fleetlift-agent` build target
- [x] Update Dockerfile.sandbox with agent binary and `fleetlift-agent serve` entrypoint
- [x] Unit tests for protocol types, activities, manifest builder, workflow

### 6b. Kubernetes Provider — COMPLETE

- [x] Implement K8s provider: create Jobs directly (labels: `fleetlift.io/task-id`)
- [x] Task ops via `exec cat`/stdin pipe into pod (SPDY executor)
- [x] Worker RBAC: Jobs (CRUD), Pods (get/list/watch), Pods/exec (create), Secrets (get)
- [x] Sandbox pod security: `automountServiceAccountToken: false`, restricted seccomp, non-root
- [x] Init container agent injection: copy `fleetlift-agent` binary into shared volume for deterministic images
- [x] Image selection: agentic → agent serve command, deterministic → idle container
- [x] Provider selection factory (`SANDBOX_PROVIDER` env var)
- [x] Configurable sandbox namespace (default: `sandbox-isolated`)
- [x] ResourceQuota enforcement per namespace
- [x] NetworkPolicy: sandbox egress allow HTTPS (443), deny all ingress
- [x] kind cluster setup script + integration tests

### Deliverable

```bash
# Agent binary built alongside worker and CLI
make build
# bin/fleetlift-agent  (statically compiled, ~10MB)

# Worker starts TransformV2 workflow for agent-mode tasks
fleetlift run --file task.yaml --agent-mode

# Agentic: sandbox runs agent as entrypoint (Claude Code + git in image)
docker inspect claude-sandbox-task-123 | jq '.[0].Config.Cmd'
# ["fleetlift-agent", "serve"]

# Deterministic: agent binary injected via init container, tool image as main
# Pod spec: initContainers[inject-agent] → containers[openrewrite/rewrite:latest]
# Agent runs the tool's command directly in the sandbox (no Docker-in-Docker)

# K8s mode (future): worker creates Jobs directly
kubectl get jobs -n sandbox-isolated -l fleetlift.io/task-id=slog-migration
```

---

## Phase 7: Observability

**Goal**: Metrics, logging, and dashboards for operational visibility.

### 7.1 Prometheus Metrics

- [ ] Instrument workflow and activities with prometheus client
- [ ] `codetransform_tasks_total` (counter) - by status, mode, transform_type
- [ ] `codetransform_task_duration_seconds` (histogram) - end-to-end duration
- [ ] `codetransform_sandbox_provision_seconds` (histogram) - sandbox startup time
- [ ] `codetransform_verifier_duration_seconds` (histogram) - by verifier name
- [ ] `codetransform_pr_created_total` (counter) - successful PRs
- [ ] `codetransform_reports_collected_total` (counter) - report mode
- [ ] `codetransform_api_tokens_used` (counter) - Claude API token consumption

### 7.2 Structured Logging

- [ ] Use slog for structured JSON logs
- [ ] Include task_id, workflow_id, repository in all log entries
- [ ] Log lifecycle events: task started, sandbox provisioned, transform complete
- [ ] Separate log streams for worker vs sandbox

### 7.3 Grafana Dashboard

- [ ] Task throughput and success rate
- [ ] Duration percentiles (p50, p95, p99)
- [ ] Active tasks and queue depth
- [ ] Sandbox provisioning latency
- [ ] Error rate by error type
- [ ] Campaign progress visualization

### 7.4 Alerting Rules

- [ ] High task failure rate (>10% over 1h)
- [ ] Task stuck in Running >2x timeout
- [ ] Sandbox provisioning failures
- [ ] Worker pod restarts
- [ ] Campaign paused on threshold

### Deliverable

```yaml
# ServiceMonitor for Prometheus Operator
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: codetransform-worker
spec:
  selector:
    matchLabels:
      app: codetransform-worker
  endpoints:
  - port: metrics
    interval: 30s
```

---

## Phase 8: Security Hardening

**Goal**: Production-grade security and operational resilience.

> **Note**: Core worker RBAC (Jobs, Pods, Pods/exec, Secrets) is defined in Phase 6b.
> This phase focuses on additional security hardening.

### 8.1 Network Policies

- [ ] Sandbox egress policy: allow HTTPS to GitHub, package registries, AI APIs
- [ ] Deny all ingress to sandbox pods
- [ ] Worker-to-sandbox communication restricted to exec-based file protocol only
- [ ] Document required egress destinations for common ecosystems

### 8.2 Advanced RBAC

- [ ] Namespace-scoped roles for multi-tenant deployments
- [ ] Admission webhooks for additional policy enforcement
- [ ] Pod Security Standards (restricted profile for sandboxes)

### 8.3 Secret Management

- [ ] IRSA for AWS credentials (ECR pull, Secrets Manager)
- [ ] External Secrets Operator integration
- [ ] Secret rotation without pod restart
- [ ] Audit which tasks accessed which secrets

### 8.4 Audit Logging

- [ ] Enable K8s audit logging for sandbox namespace
- [ ] Log all exec operations with task context
- [ ] Integration with SIEM (CloudWatch, Splunk, etc.)

### 8.5 Scaling & Reliability

- [ ] HPA for workers based on Temporal task queue depth
- [ ] Cluster Autoscaler configuration for sandbox node pool
- [ ] Spot instance node group with fallback to on-demand
- [ ] Pod Disruption Budgets for workers
- [ ] Graceful shutdown: drain active tasks before termination
- [ ] **Orphaned sandbox reaper**: Periodic process that labels sandboxes with workflow IDs and cleans up any whose workflow is terminal. Covers worker crashes between provision and cleanup. For Docker: goroutine scanning containers by label. For K8s: CronJob scanning Jobs by `fleetlift.io/task-id` label.
- [ ] **Backpressure / resource awareness**: Configure Temporal `MaxConcurrentActivityExecutionSize` to bound concurrent sandbox provisioning per worker. Combine with K8s `ResourceQuota` per sandbox namespace to prevent cluster overcommit.

### 8.6 Deployment Artifacts

- [ ] Helm chart with configurable values
- [ ] Terraform module for EKS cluster setup
- [ ] Kustomize overlays for dev/staging/prod
- [ ] Runbook: common failure modes and remediation

### Deliverable

```bash
# Deploy with Helm
helm install codetransform ./charts/codetransform \
  --namespace codetransform-system \
  --set temporal.address=temporal.internal:7233 \
  --set sandbox.namespace=sandbox-isolated \
  --set sandbox.runtimeClassName=gvisor
```

---

## Phase 9: Advanced Features

**Goal**: Enhanced capabilities for production usage.

### 9.1 Human-in-the-Loop (Basic) - COMPLETE

- [x] Temporal signals for approval *(approve/reject/cancel signals implemented)*
- [x] Slack integration for notifications *(NotifySlack activity)*
- [x] Approval timeout handling *(24-hour timeout with AwaitWithTimeout)*

### 9.2 Human-in-the-Loop (Iterative Steering) - COMPLETE

**Goal**: Enable iterative human-agent collaboration instead of binary approve/reject.

> **This is a key differentiator.** Basic HITL (approve/reject) is table stakes. Iterative
> steering—where humans can guide the agent through multiple rounds—makes the platform valuable.

#### 9.2.1 View Changes

- [x] `GetDiff` activity - return full git diffs for all modified files
- [x] `GetVerifierOutput` activity - return detailed test/build output
- [x] CLI: `fleetlift diff --workflow-id <id>` - view changes in terminal
- [x] CLI: `fleetlift logs --workflow-id <id>` - view verifier output
- [x] Slack: Include diff summary in approval notifications

#### 9.2.2 Steering Prompts

- [x] Add `steer` signal with prompt payload to workflow
- [x] Workflow loops back to Claude Code with steering prompt
- [x] Preserve conversation context across steering iterations
- [x] CLI: `fleetlift steer --workflow-id <id> --prompt "try X instead"`
- [x] Track iteration count and history
- [x] Configurable max iterations (`max_steering_iterations` in task, default 5)
- [x] Query handlers for diff, verifier logs, and steering state (web-ready)

#### 9.2.3 Partial Approval

> **Explicitly out of scope** - Partial/granular file-level approval was excluded per requirements.
> Users can steer to request specific file changes, then approve all changes together.

### 9.3 Scheduled Tasks

- [ ] Temporal schedules for recurring tasks
- [ ] Cron-like syntax support
- [ ] Example: weekly dependency update transforms

### 9.4 Cost Tracking

- [ ] Track API token usage per task (Claude API)
- [ ] Compute cost attribution (sandbox CPU/memory hours)
- [ ] Per-team/namespace cost rollup
- [ ] Budget alerts and quotas

### 9.5 Web UI — COMPLETE

- [x] Go API server (`cmd/server`) — chi router, REST+SSE endpoints wrapping Temporal client
- [x] `TemporalClient` interface + mock for unit testing
- [x] Task list + inbox handlers (GET /tasks, /tasks/inbox, /tasks/{id}, diff, logs, steering, progress)
- [x] Signal handlers (approve, reject, cancel, steer, continue)
- [x] SSE live status updates (`GET /tasks/{id}/events`)
- [x] React + TypeScript SPA (`web/`) — Vite, shadcn/ui, Tailwind, react-query
- [x] TypeScript API types mirroring Go model JSON tags
- [x] App shell with routing (Inbox / Task List / Task Detail)
- [x] Inbox page with 5s polling and badge per inbox type
- [x] Task List page with status filter and 10s polling
- [x] Task Detail page with tabs and SSE live status
- [x] DiffViewer component (react-diff-viewer-continued, collapsible per file)
- [x] SteeringPanel component (approve/reject/steer mutations, iteration history)
- [x] VerifierLogs + GroupProgress components
- [x] Go embed wiring (`web/embed.go`, Makefile `build-web` target)

### 9.6 Report Storage Options

- [ ] Inline in result (default, for small reports)
- [ ] S3/GCS backend for large-scale discovery (100+ repos)
- [ ] Config: `report_storage.backend`

---

## Phase 10: Continual Learning / Knowledge Items

**Goal**: Capture knowledge from successful transformations and reuse it to improve future runs.

> **Design Rationale**: AWS Transform's most compelling feature is "continual learning" — the agent
> gets better with each execution. Fleetlift's advantage is doing this *transparently*: knowledge
> items are version-controlled YAML files in the transformation repo that users can read, edit,
> share, and audit. The transformation repository (Phase 4c) is the natural home for this.
>
> **Key insight**: The richest signal comes from *steering corrections* (Phase 9.2). When a human
> says "no, do it this way," that's exactly the knowledge that should persist. Every approved
> transformation produces: the original prompt, steering corrections, the final diff, verifier
> results, and the approval decision. This is the training data.

### 10.1 Knowledge Item Data Model

- [ ] Define `KnowledgeItem` struct:
  ```go
  type KnowledgeItem struct {
      ID          string          `json:"id" yaml:"id"`
      Type        KnowledgeType   `json:"type" yaml:"type"`
      Summary     string          `json:"summary" yaml:"summary"`
      Details     string          `json:"details" yaml:"details"`
      Source      KnowledgeSource `json:"source" yaml:"source"`
      Tags        []string        `json:"tags,omitempty" yaml:"tags,omitempty"`
      Confidence  float64         `json:"confidence" yaml:"confidence"`
      CreatedFrom *KnowledgeOrigin `json:"created_from,omitempty" yaml:"created_from,omitempty"`
      CreatedAt   time.Time       `json:"created_at" yaml:"created_at"`
  }
  ```
- [ ] `KnowledgeType` enum: `pattern`, `correction`, `gotcha`, `context`
  - `pattern` — a reusable approach that worked (e.g., "when migrating logger X→Y, also update config files")
  - `correction` — extracted from steering, where the agent went wrong and was corrected
  - `gotcha` — a non-obvious failure mode (e.g., "Python 3.9→3.11 breaks walrus operator in certain comprehensions")
  - `context` — repo-specific knowledge (e.g., "this repo uses a custom build system, run `make` not `go build`")
- [ ] `KnowledgeSource` enum: `auto_captured`, `steering_extracted`, `manual`
- [ ] `KnowledgeOrigin` struct: `TaskID`, `SteeringPrompt`, `Iteration`, `Repository`

### 10.2 Three-Tier Knowledge Storage

Knowledge lives at three levels with increasing curation:

**Tier 1: Execution Log (automatic, ephemeral)**
- Already exists in Temporal workflow history
- Full record of every run including failures
- No new storage needed; queryable via existing Temporal APIs

**Tier 2: Local Knowledge Store (automatic, persistent)**
- [ ] Store auto-captured items at `~/.fleetlift/knowledge/{task-id}/`
- [ ] YAML files, one per knowledge item
- [ ] Populated automatically after each approved transformation
- [ ] Used to enrich future prompts when no transformation repo is set
- [ ] Indexed by tags for fast lookup

**Tier 3: Transformation Repository (curated, shared)**
- [ ] Convention: `.fleetlift/knowledge/` directory in transformation repos
- [ ] Human-curated subset of Tier 2 items, committed to version control
- [ ] Team-shareable, auditable, version-controlled
- [ ] Takes precedence over Tier 2 when a transformation repo is used

Directory layout in transformation repo:
```
transformation-repo/
├── .claude/skills/          # Existing: Claude Code skills
├── .fleetlift/
│   └── knowledge/
│       ├── items/
│       │   ├── slog-test-helpers.yaml
│       │   ├── slog-go-kit-pattern.yaml
│       │   └── slog-mod-tidy.yaml
│       └── config.yaml      # Optional: tag filters, max items per prompt
├── CLAUDE.md
└── ...
```

### 10.3 Knowledge Capture Activity

- [ ] New activity: `ActivityCaptureKnowledge`
- [ ] Triggered after approval (new workflow step between approval and PR creation)
- [ ] Input: original prompt, all steering prompts + iterations, final diff, verifier results
- [ ] Uses Claude to analyze the execution and extract reusable knowledge items:
  ```
  Prompt: "Analyze this transformation execution. Extract reusable knowledge items
  that would help future runs of similar transformations.

  Original prompt: {prompt}
  Steering corrections: {steering_history}
  Final diff summary: {diff_stats}
  Verifier results: {verifier_output}

  For each knowledge item, provide: type, summary (1 line), details, tags, confidence (0-1).
  Focus especially on steering corrections — these indicate where the agent went wrong."
  ```
- [ ] Parse Claude's response into `KnowledgeItem` structs
- [ ] Write to Tier 2 (local store) automatically
- [ ] Log to CLI: "2 knowledge items captured. Run `fleetlift knowledge review` to curate."
- [ ] This activity is non-blocking — failure should not prevent PR creation

### 10.4 Prompt Enrichment Activity

- [ ] New activity: `ActivityEnrichPrompt`
- [ ] Runs before `ActivityRunClaudeCode` in the workflow
- [ ] Input: original prompt, task tags/metadata, transformation repo path (if any)
- [ ] Load knowledge items from Tier 3 (transformation repo) first, then Tier 2 (local)
- [ ] Filter for relevance: match on tags, transformation type, repo characteristics
- [ ] Cap injected knowledge (configurable, default: 10 items, ~2000 tokens max)
- [ ] Append to prompt as a structured section:
  ```
  {original prompt}

  ---
  ## Lessons from previous runs

  Keep these in mind based on previous transformations:
  - [correction] slog migration requires updating test helpers that wrap the logger
  - [gotcha] Repos using go-kit have a different logger interface; check for go-kit/log imports first
  - [pattern] Always run `go mod tidy` after updating logger imports
  ```
- [ ] Return enriched prompt for use by `ActivityRunClaudeCode`

### 10.5 Knowledge CLI Commands

- [ ] `fleetlift knowledge list [--task-id ID] [--type TYPE] [--tag TAG]` — list knowledge items
- [ ] `fleetlift knowledge show <item-id>` — show full item details
- [ ] `fleetlift knowledge review [--task-id ID]` — interactive review of auto-captured items (mark as keep/discard/edit)
- [ ] `fleetlift knowledge commit [--repo PATH]` — copy reviewed items into transformation repo's `.fleetlift/knowledge/`
- [ ] `fleetlift knowledge add --summary "..." --type correction --tags "go,logging"` — manually add a knowledge item
- [ ] `fleetlift knowledge delete <item-id>` — remove an item

### 10.6 Workflow Integration

- [ ] Update `Transform` workflow to insert knowledge capture after approval
- [ ] Update `Transform` workflow to insert prompt enrichment before agent execution
- [ ] Both steps are skippable via task config: `knowledge.capture: false`, `knowledge.enrich: false`
- [ ] Steering iterations also benefit: knowledge is injected on first run, steering corrections from this run are captured at the end
- [ ] Grouped execution: knowledge capture runs per-group; all groups contribute to the same knowledge pool

### 10.7 Task YAML Extensions

- [ ] Add optional `knowledge` section to Task:
  ```yaml
  knowledge:
    capture: true          # Auto-capture after approval (default: true)
    enrich: true           # Enrich prompt with past knowledge (default: true)
    max_items: 10          # Max knowledge items injected into prompt
    tags: [go, logging]    # Additional tags for filtering/matching
  ```

### 10.8 Knowledge Efficacy Tracking

- [ ] Track per-item usage: how many times injected, how many runs succeeded
- [ ] Track steering frequency: do runs with knowledge enrichment need fewer steering rounds?
- [ ] Store metrics in item metadata: `times_used`, `success_rate`, `avg_steering_rounds`
- [ ] `fleetlift knowledge stats` — show efficacy metrics
- [ ] Auto-deprecate items with low confidence after N uses with no improvement

### Deliverable

```yaml
# After running a slog migration with 2 steering rounds:
$ fleetlift knowledge list --task-id transform-slog-migration-abc
ID                        TYPE        CONFIDENCE  SUMMARY
slog-test-helpers-01      correction  0.95        Update test helpers that wrap the logger
slog-go-kit-compat-01     gotcha      0.80        go-kit repos need different logger interface
slog-mod-tidy-01          pattern     0.90        Run go mod tidy after import changes

# Review and curate
$ fleetlift knowledge review --task-id transform-slog-migration-abc
# Interactive: keep/discard/edit each item

# Commit curated items to transformation repo
$ fleetlift knowledge commit --repo ./slog-migration-toolkit
# Wrote 3 items to ./slog-migration-toolkit/.fleetlift/knowledge/items/

# Next run automatically uses knowledge:
$ fleetlift run -f slog-migration-batch2.yaml
# Prompt enriched with 3 knowledge items from transformation repo
# Result: 0 steering rounds needed (vs 2 previously)
```

### Design Decisions & Tradeoffs

| Decision | Choice | Alternative | Rationale |
|----------|--------|-------------|-----------|
| Storage format | YAML files in repo | Database/API | Version control, auditable, no extra infra |
| Capture trigger | After approval | After PR merge | Faster feedback loop; merge status tracked separately in efficacy metrics |
| Relevance matching | Tag-based filtering | Embedding similarity | Simple, predictable, no vector DB dependency; can add embeddings later |
| Knowledge extraction | Claude analysis | Diff heuristics | Steering corrections need semantic understanding, not just pattern matching |
| Prompt injection style | Append section | System prompt / separate context | Keeps it visible to the agent; easy to debug |

---

## Phase 11: Natural Language Task Creation

**Goal**: Let users create task YAML files through natural language conversation, lowering the barrier to entry.

> **Design Rationale**: The YAML task file is powerful but requires knowing the schema. AWS Transform
> lets users define transformations via natural language chat. Fleetlift should offer this as an
> optional on-ramp — the generated YAML becomes the artifact, keeping the system transparent and
> debuggable. This is a CLI-only feature (no Temporal workflow needed).
>
> **Key insight**: This feature composes with the transformation repo and knowledge system. When a
> user describes a task, the create flow can suggest relevant transformation repos from a registry
> and inject knowledge items into the generated prompt.

### 11.1 Interactive Create Command

- [ ] `fleetlift create` — starts interactive task creation session
- [ ] Uses Claude API directly (not through Temporal) with the task YAML schema as context
- [ ] Conversational flow:
  1. **Intent**: "What do you want to do?" → determines mode (transform/report) and execution type (agentic/deterministic)
  2. **Targets**: "Which repositories?" → accepts URLs, org patterns, or org-wide discovery
  3. **Details**: "Describe the transformation/analysis" → generates the prompt
  4. **Verification**: "How should we verify?" → generates verifiers
  5. **Review**: "Should changes be reviewed before PRs?" → sets approval/grouping config
  6. **Output**: Writes YAML file, shows summary, offers to run immediately
- [ ] Each step allows free-form natural language input
- [ ] Claude generates valid YAML by using the full schema + examples as context

### 11.2 One-Shot Create

- [ ] `fleetlift create --describe "Migrate all Go services in acme-org from logrus to slog, verify with go build and go test, require approval"` — single command, no interaction
- [ ] Claude infers all parameters from the description
- [ ] Writes YAML file and shows it for review
- [ ] `--run` flag to immediately execute after generation
- [ ] `--output task.yaml` to specify output file (default: `{id}.yaml`)

### 11.3 GitHub Repository Discovery

- [ ] Integrate with `gh` CLI or GitHub API for repo discovery during `create`
- [ ] Support patterns: "all repos in acme-org", "repos matching service-*", "repos with go.mod"
- [ ] `fleetlift create` prompts: "I found 23 repos matching 'service-*' in acme-org. Include all?"
- [ ] Caches org repo list locally for subsequent runs (`~/.fleetlift/cache/repos/`)
- [ ] Respects GitHub API rate limits; paginates large orgs

### 11.4 Schema Context Bundle

- [ ] Bundle full Task YAML schema as Go embed in CLI binary
- [ ] Include 4-5 canonical examples covering:
  - Simple single-repo agentic transform
  - Multi-repo grouped execution with failure thresholds
  - Report mode with forEach and schema validation
  - Deterministic transformation with Docker image
  - Transformation repo with targets
- [ ] Include field descriptions and constraints (e.g., "timeout format: '30m', '1h'")
- [ ] This bundle is the system prompt for the create flow's Claude calls

### 11.5 Transformation Repo Suggestions

- [ ] Optional registry file: `~/.fleetlift/registries/repos.yaml`
  ```yaml
  transformation_repos:
    - name: slog-migration-toolkit
      url: https://github.com/org/slog-migration-toolkit.git
      description: "Migrate Go services from various loggers to slog"
      tags: [go, logging, slog]
    - name: security-audit-tools
      url: https://github.com/org/security-audit-tools.git
      description: "Security audit skills for authentication, authorization, secrets"
      tags: [security, audit]
  ```
- [ ] During `fleetlift create`, if the user's description matches a registered repo's tags, suggest it:
  "This looks like a logging migration. Use the 'slog-migration-toolkit' transformation repo? [Y/n]"
- [ ] If a transformation repo is selected and has knowledge items (Phase 10), mention known gotchas:
  "Previous runs found that test helpers wrapping the logger need manual updates. Include this in the prompt? [Y/n]"

### 11.6 Template Library

- [ ] Ship built-in templates for common transformations (embedded in CLI binary):
  - `dependency-upgrade` — generic dependency version bump
  - `api-migration` — migrate from one API to another
  - `security-audit` — report mode security analysis
  - `framework-upgrade` — language/framework version upgrade
- [ ] `fleetlift create --template api-migration` — start from template
- [ ] `fleetlift templates list` — show available templates
- [ ] Templates are Go embed files, not fetched from network
- [ ] Users can add custom templates: `~/.fleetlift/templates/`

### 11.7 Validation and Review

- [ ] Generated YAML is validated against the schema before writing
- [ ] Show generated YAML with syntax highlighting in terminal
- [ ] Prompt: "Save to {filename}? [Y/n/edit]"
- [ ] `edit` opens `$EDITOR` with the generated YAML for manual tweaks
- [ ] After editing, re-validate before saving
- [ ] `--dry-run` flag: show generated YAML without saving

### 11.8 Claude API Integration (CLI-side)

- [ ] New package: `internal/create/` — handles the conversational flow
- [ ] Uses Claude API directly with the Anthropic Go SDK (not through Temporal/sandbox)
- [ ] Uses the same `ANTHROPIC_API_KEY` env var as the rest of the system
- [ ] Model selection: use a fast model (Haiku) for the create flow to minimize cost/latency
- [ ] Conversation is multi-turn: each clarifying question is a new API call with prior context
- [ ] Token budget cap for the create flow (~10K tokens max)

### Deliverable

```bash
# Interactive mode
$ fleetlift create
What do you want to do?
> Migrate our Python services from boto2 to boto3

Mode: transform (creating PRs with code changes)
Execution: agentic (AI-assisted, context-dependent migration)

Which repositories?
> All repos matching "service-*" in acme-corp

Found 18 repos matching "service-*" in acme-corp. Include all? [Y/n]
> Y

How should changes be verified?
> Run pytest and mypy

Should changes be reviewed before PRs are created? [Y/n]
> Y

With 18 repos, group them for parallel execution? [Y/n]
> Yes, groups of 5

Suggested transformation repo: 'boto3-migration-tools' (matches tags: python, aws, boto)
Use it? [Y/n]
> Y

Previous knowledge from boto3-migration-tools:
  - [gotcha] DynamoDB Table.scan() pagination changed in boto3; check for manual pagination loops
  - [pattern] Always update requirements.txt AND setup.py when present
Include in prompt? [Y/n]
> Y

Generated: migrate-boto3.yaml

---
version: 1
id: migrate-boto3
title: "Migrate Python services from boto2 to boto3"

transformation:
  url: https://github.com/acme-corp/boto3-migration-tools.git
  branch: main

targets:
  - url: https://github.com/acme-corp/service-auth.git
  - url: https://github.com/acme-corp/service-billing.git
  # ... (16 more)

execution:
  agentic:
    prompt: |
      Migrate all boto2 usage to boto3 in this repository.
      - Replace `import boto` with `import boto3`
      - Update client initialization (boto.connect_* → boto3.client/resource)
      - Migrate all API calls to boto3 equivalents
      - Update requirements.txt AND setup.py when present
      - Check for manual DynamoDB pagination loops (pagination API changed in boto3)

    verifiers:
      - name: test
        command: [pytest]
      - name: typecheck
        command: [mypy, .]

groups:
  - name: batch-1
    repositories: [service-auth, service-billing, service-cart, service-catalog, service-checkout]
  - name: batch-2
    repositories: [service-email, service-events, service-gateway, service-inventory, service-jobs]
  - name: batch-3
    repositories: [service-kafka, service-logs, service-metrics, service-notifications, service-orders]
  - name: batch-4
    repositories: [service-payments, service-queue, service-reports]

max_parallel: 4
require_approval: true
timeout: 30m

knowledge:
  capture: true
  enrich: true
  tags: [python, aws, boto]

pull_request:
  branch_prefix: "migrate/boto3-"
  title: "Migrate from boto2 to boto3"
  labels: [migration, automated]
---

Save to migrate-boto3.yaml? [Y/n/edit]
> Y

Run now? [Y/n]
> Y
Workflow started: transform-migrate-boto3-1738512345

# One-shot mode
$ fleetlift create \
  --describe "Audit authentication in all Go services in acme-corp, report mode" \
  --run
# Discovers repos, generates YAML, runs immediately
```

### Design Decisions & Tradeoffs

| Decision | Choice | Alternative | Rationale |
|----------|--------|-------------|-----------|
| Execution | CLI-only, no Temporal | Temporal workflow | No durability needed; fast iteration; lower complexity |
| AI model | Haiku (fast/cheap) | Opus (smart) | Schema is well-defined; generation is constrained; cost matters for a CLI UX flow |
| Output | YAML file (artifact) | Run directly from memory | YAML is inspectable, editable, version-controllable, reproducible |
| Repo discovery | `gh` CLI / GitHub API | Manual listing only | Huge UX win for fleet operations; the whole point is operating at scale |
| Template system | Embedded in binary | Fetched from registry | No network dependency for basic usage; registry is optional |
| Interactive vs one-shot | Both | Interactive only | Power users want `--describe`; new users want guided flow |

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
| 7 | **Observability** | Metrics, logging, dashboards | ⬜ Not started |
| 8 | **Security** | NetworkPolicy, secrets, scaling | ⬜ Not started |
| 9.1 | **HITL (Basic)** | Approve/reject signals, Slack notifications | ✅ Complete |
| 9.2 | **HITL (Steering)** | Iterative steering with diff/logs/steer commands | ✅ Complete |
| 9.3 | **Scheduled tasks** | Temporal cron-style recurring tasks | ⬜ Not started |
| 9.4 | **Cost tracking** | API token + compute attribution | ⬜ Not started |
| 9.5 | **Web UI** | Inbox, diff review, approval/steering dashboard | ✅ Complete |
| 9.6 | **Report storage** | S3/GCS backend for large-scale discovery | ⬜ Not started |
| 10 | **Continual Learning** | Knowledge capture, enrichment, curation | ⬜ Not started |
| 11 | **NL Task Creation** | Conversational task creation, repo discovery, templates | ⬜ Not started |

Each phase builds on the previous and delivers working functionality.

### Recommended Next Steps

**Production Infrastructure Track:**
1. **Phase 6b** - Kubernetes sandbox provider (direct Job creation, RBAC, NetworkPolicy)
2. **Phase 7** - Observability (metrics, dashboards, alerting)
3. **Phase 8** - Security hardening (secrets management, scaling)

**Enhanced Features Track:**
1. **Phase 9.3** - Scheduled tasks (recurring transformations)
2. **Phase 9.4** - Cost tracking (API usage, compute attribution)
3. **Phase 9.5** - Web UI (optional dashboard)

**Intelligence & UX Track:**
1. **Phase 10** - Continual learning (knowledge capture from steering, prompt enrichment, curation CLI)
2. **Phase 11** - Natural language task creation (conversational YAML generation, repo discovery, templates)

> **Status Note**: Core platform capabilities are complete. All product features (agentic/deterministic
> transforms, report mode, grouped execution, failure handling, retry, HITL iterative steering, sidecar
> agent with file-based protocol) are implemented. Next phase focuses on production infrastructure
> (Kubernetes provider) and operational features (observability, security).

### Key Changes from Previous Plan

| Previous | Current | Rationale |
|----------|---------|-----------|
| Phase 4: CRD & Controller | Phase 4: Report Mode | Report mode is core functionality; CRD is optional convenience layer |
| Phase 8: Agent Sandbox | Removed | Plain K8s Jobs are sufficient; Agent Sandbox adds complexity without clear benefit |
| Phase 9.6: Report Mode | Phase 4: Report Mode | Promoted to core phase—discovery is first-class, not an afterthought |
| Campaign as separate concept | Phase 5: Grouped Execution | Simpler model: failure handling at Task level with groups instead of separate Campaign type |
| CRD/Controller pattern | Phase 6: Direct Job management | Simpler architecture: worker creates Jobs directly, no CRD or controller needed |
| Deterministic via `docker run` in sandbox | Direct command execution + init container injection | No Docker-in-Docker; agent binary injected at deploy time; one mode of operation for agent |
