# Fleetlift Roadmap

Outstanding work as of 2026-03-06.

**Completed**: Phases 1–9.5, agentbox split (AB-1–AB-5), knowledge capture/enrich (Phase 10a).
**Active branch**: `feat/agentbox-split` (merge pending).

---

## Phase 8: Security Hardening

Production-grade security for K8s deployments.

### 8.1 Network Policies
- [ ] Sandbox egress: allow HTTPS to GitHub, package registries, AI APIs
- [ ] Deny all ingress to sandbox pods
- [ ] Document required egress destinations per ecosystem (Go, Node, Python, Java)

### 8.2 RBAC & Pod Security
- [ ] Namespace-scoped roles for multi-tenant deployments
- [ ] Pod Security Standards (restricted profile for sandboxes)

### 8.3 Secret Management
- [ ] IRSA for AWS credentials (ECR pull, Secrets Manager)
- [ ] External Secrets Operator integration docs + example config

### 8.4 Scaling & Reliability
- [ ] HPA for workers based on Temporal task queue depth
- [ ] Graceful shutdown: drain active tasks before termination
- [ ] **Orphaned sandbox reaper**: on worker start, scan containers/jobs by `agentbox.io/task-id` label older than configurable TTL → terminate. Docker: goroutine; K8s: CronJob.
- [ ] **Backpressure**: set `MaxConcurrentActivityExecutionSize` + K8s `ResourceQuota` per sandbox namespace to prevent overcommit

### 8.5 Deployment Artifacts
- [ ] Helm chart with configurable values (image, replicas, temporal addr, resource presets)
- [ ] Kustomize overlays (dev/staging/prod)
- [ ] Runbook: common failure modes and remediation

---

## Phase 10b: Knowledge Curation

Enable humans to curate auto-captured knowledge before sharing team-wide.

**Prerequisite status:** Phase 10a workflow wiring is now complete (EnrichPrompt + CaptureKnowledge called from TransformV2).

**Prerequisite**: Phase 10a complete — `CaptureKnowledge`, `EnrichPrompt` activities, `knowledge list/show` CLI, workflow wiring (EnrichPrompt + CaptureKnowledge called from TransformV2).

- [ ] `fleetlift knowledge review [--task-id ID]` — interactive TUI; approve/edit/delete items before promotion to Tier 3
- [ ] `fleetlift knowledge commit [--repo PATH]` — copy approved items into `.fleetlift/knowledge/` in a transformation repo
- [ ] Post-approval CLI log: "N knowledge items captured. Run `fleetlift knowledge review` to curate."
- [ ] Grouped execution wiring: single-group path done; multi-group path needs knowledge capture per-group contributing to shared pool
- [ ] Efficacy tracking: add `times_used`, `success_rate` fields to `KnowledgeItem`; `fleetlift knowledge stats` command; auto-deprecate items with low confidence after N uses with no improvement

---

## Phase 11: Natural Language Task Creation

Create Task YAML via conversation instead of hand-editing.

### 11.1 Core commands
- [ ] `fleetlift create` — multi-step interactive session; asks for repos, prompt, verifiers, mode, approval
- [ ] `fleetlift create --describe "..."` — one-shot; Claude infers all params, writes YAML
- [ ] Generated YAML validated against schema; show with syntax highlighting; prompt `[Y/n/edit]`
- [ ] `--dry-run`, `--output task.yaml`; `edit` choice opens `$EDITOR`

### 11.2 Schema bundle
- [ ] Embed Task YAML schema + 4–5 canonical examples as `//go:embed` in CLI binary
- [ ] Include field descriptions + constraints (e.g. `timeout` format: `"30m"`, `"1h"`)
- [ ] Bundle is the system prompt for all `create` flow Claude API calls

### 11.3 Repo discovery
- [ ] GitHub API: support patterns like `"all repos in acme-org"`, `"repos matching service-*"`, `"repos with go.mod"`
- [ ] Prompt confirmation: "Found 23 repos matching 'service-*' in acme-org. Include all?"
- [ ] Cache org repo list in `~/.fleetlift/cache/repos/`; paginate large orgs; respect rate limits

### 11.4 Transformation repo registry
- [ ] Optional `~/.fleetlift/registries/repos.yaml` mapping tags → transformation repo URLs
- [ ] During `create`, suggest matching repos if description matches registered tags
- [ ] If selected repo has knowledge items (Phase 10), surface known gotchas

### 11.5 Built-in templates
- [ ] Embed templates for common transforms: dep upgrade, API migration, security audit, logging migration
- [ ] `fleetlift create --template api-migration`; `fleetlift templates list`
- [ ] User-local templates at `~/.fleetlift/templates/`

---

## Phase 12: Operational Improvements

### 12.1 Temporal schedules
- [ ] `fleetlift schedule create --cron "0 9 * * 1" --file task.yaml` — recurring task
- [ ] `fleetlift schedule list / delete <id>`
- [ ] Example: weekly dependency scan, nightly security audit

### 12.2 Cost & token tracking
- [ ] `AgentStatus.Metadata` already transports `input_tokens`, `output_tokens`, `model`, `cost_usd` — wire through to `fleetlift status` output and web UI task detail
- [ ] Aggregate cost per task in `TaskResult`; surface in `fleetlift list` table
- [ ] Per-team/namespace cost rollup across workflow history
- [ ] Budget alerts: configurable per-task and per-period limits; Slack notification on breach

### 12.3 Report storage backend
- [ ] Inline in result (default, for small reports) — already works
- [ ] S3/GCS backend for large-scale discovery (100+ repos)
- [ ] Config: `report_storage.backend: s3`, `bucket`, `prefix`
