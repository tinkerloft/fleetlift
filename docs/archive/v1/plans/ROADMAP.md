# Fleetlift Roadmap

Outstanding work as of 2026-03-08.

**Completed**: Phases 1–9.5, agentbox split (AB-1–AB-5), knowledge system (Phase 10.1–10.7), NL task creation (Phase 11), template library (Phase 11.6).

---

## Phase 8: Security Hardening

Production-grade security for K8s deployments.

Network policies, RBAC, secret management, scaling infrastructure, and deployment artifacts are delegated to OpenSandbox and the ops/infra layer. Remaining fleetlift-owned items:

- [ ] **Orphaned sandbox reaper**: Periodic process that queries OpenSandbox for sandboxes labelled with fleetlift workflow IDs and cleans up any whose workflow is in a terminal state
- [ ] **Backpressure config**: Configure Temporal `MaxConcurrentActivityExecutionSize` to bound concurrent sandbox provisioning per worker

---

## Phase 10b: Knowledge System (Remaining)

Core knowledge system (10.1–10.7) is complete: capture, enrichment, CLI (list/show/add/delete), curation (review/commit), workflow integration (single + grouped), and task YAML extensions.

- [x] `fleetlift knowledge review [--task-id ID]` — interactive TUI; approve/edit/delete items before promotion to Tier 3
- [x] `fleetlift knowledge commit [--repo PATH]` — copy approved items into `.fleetlift/knowledge/` in a transformation repo
- [x] Grouped execution wiring: knowledge capture runs per-group through `TransformV2`; all groups contribute to the same knowledge pool
- [ ] Efficacy tracking (10.8): add `times_used`, `success_rate` fields to `KnowledgeItem`; `fleetlift knowledge stats` command; auto-deprecate items with low confidence after N uses with no improvement

---

## Phase 11: Natural Language Task Creation

Create Task YAML via conversation instead of hand-editing.

### 11.1 Core commands
- [ ] `fleetlift create` — multi-step interactive session; asks for repos, prompt, verifiers, mode, approval
- [x] `fleetlift create --describe "..."` — one-shot; Claude infers all params, writes YAML
- [x] Generated YAML validated + `[Y/n/e]` prompt
- [x] `--dry-run`, `--output task.yaml`; `edit` choice opens `$EDITOR`
- [x] `--run` flag: save and immediately execute (requires `--output`)
- [x] `--template` flag: use a built-in or user-defined template as starting point

### 11.2 Schema bundle ✅ Complete
- [x] Embed Task YAML schema + canonical examples as `//go:embed` in CLI binary
- [x] Include field descriptions + constraints (e.g. `timeout` format: `"30m"`, `"1h"`)
- [x] Bundle is the system prompt for all `create` flow Claude API calls

### 11.3 Repo discovery
- [ ] GitHub API: support patterns like `"all repos in acme-org"`, `"repos matching service-*"`, `"repos with go.mod"`
- [ ] Prompt confirmation: "Found 23 repos matching 'service-*' in acme-org. Include all?"
- [ ] Cache org repo list in `~/.fleetlift/cache/repos/`; paginate large orgs; respect rate limits

### 11.4 Transformation repo registry
- [ ] Optional `~/.fleetlift/registries/repos.yaml` mapping tags → transformation repo URLs
- [ ] During `create`, suggest matching repos if description matches registered tags
- [ ] If selected repo has knowledge items (Phase 10), surface known gotchas

### 11.5 Template library ✅ Complete
- [x] Embed templates for common transforms: dep upgrade, API migration, security audit, framework upgrade
- [x] `fleetlift create --template api-migration`; `fleetlift templates list`
- [x] User-local templates at `~/.fleetlift/templates/`

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
