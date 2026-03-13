# Fleetlift Platform Redesign

**Date:** 2026-03-11
**Status:** Design — approved, pending implementation plan
**Scope:** Fresh build on a new branch, reusing Temporal and OpenSandbox infrastructure

---

## 1. Problem Statement

The current Fleetlift is a capable but narrow tool: it runs Claude Code against multiple
repositories and either creates PRs or generates reports. The architecture is sound but the
task model is hardcoded to two modes (`transform` / `report`) and the system has no auth,
no multi-tenancy, and no way to compose multi-step workflows.

The opportunity is a general-purpose agentic workflow platform. A developer experience team
can offer a library of reusable workflow templates — bug fix, migration, audit, triage, PR
review — and any user can run one with a prompt and a target repo. The platform handles
execution, streaming output, human-in-the-loop review, and result delivery. Infrastructure
scales from local Docker to Kubernetes.

---

## 2. Goals

- DAG-based workflow templates, parameterisable at run time
- Multi-tenant with GitHub OAuth (Okta and others pluggable)
- Pluggable coding agents (Claude Code first; Codex, Gemini later)
- Real-time streaming output from agent sandboxes to the UI
- Human-in-the-loop at any step: approval, rejection, steering
- Inbox for both HITL gates and completed report outputs
- Web UI and CLI with consistent capabilities
- Local Docker and Kubernetes production deployment
- Nine pre-built workflow templates

---

## 3. Non-Goals

- Visual DAG builder (visualisation yes, drag-and-drop construction later)
- Git-backed template provider (BuiltinProvider and DatabaseProvider first)
- Billing, usage quotas, or SaaS multi-organisation management

---

## 4. Architecture

```
┌─────────────────────────────────────────────────────────┐
│  Surfaces                                               │
│  Web UI (React SPA)          CLI (cobra)                │
├─────────────────────────────────────────────────────────┤
│  API Server (Go / chi)                                  │
│  Auth · Multi-tenancy · REST · SSE                      │
│  Workflow CRUD · Run management · Reports               │
├─────────────────────────────────────────────────────────┤
│  Orchestration (Temporal)                               │
│  DAGWorkflow · StepWorkflow · HITL signals / queries    │
├─────────────────────────────────────────────────────────┤
│  Execution (OpenSandbox SDK — direct, no sidecar)       │
│  Sandbox lifecycle · AgentRunner interface              │
│  ClaudeCodeRunner · CodexRunner (future)                │
├─────────────────────────────────────────────────────────┤
│  Template Providers (pluggable)                         │
│  BuiltinProvider · DatabaseProvider · GitProvider(TBD)  │
└─────────────────────────────────────────────────────────┘
```

**Key departure from current Fleetlift:** the `fleetlift-agent` sidecar binary and
file-based protocol (manifest / status / result / steering JSON files) are removed. The
worker orchestrates sandboxes directly via the OpenSandbox SDK. There is no polling of
status files; commands stream stdout via SSE and files are read directly via execd.

---

## 5. Data Model

### 5.1 Core Entities

**`Team`** — tenancy boundary. Workflows, Runs, credentials, and API keys belong to a
team. Users are members of teams with a role.

**`User`** — authenticated identity. Has a role (`member` or `admin`) per team. Identified
by an auth provider ID (GitHub user ID initially).

**`WorkflowTemplate`** — a reusable DAG definition. Owned by a team (or builtin). Has
version, title, description, tags, a parameter schema, and a list of steps. Stored as YAML.

**`Run`** — one execution of a WorkflowTemplate with a specific parameter set. Belongs to a
user and team. Tracks status, start/end time, and Temporal workflow ID.

**`StepRun`** — one execution of one step within a Run. Tracks phase, sandbox ID,
streaming log reference, outputs, and error.

**`Artifact`** — a file or document produced by a step. Stored in PostgreSQL (small,
<1 MB) or object storage (large). Referenced by ID in StepRun output.

**`InboxItem`** — a notification record. Either `awaiting_input` (HITL gate) or
`output_ready` (completed report). Dismissed per-user; the underlying Run and output are
permanent.

**`Credential`** — a team-scoped secret (API key, token). Stored encrypted. Injected as
env vars into sandboxes at run time; never exposed in YAML or logs.

### 5.2 Workflow Template YAML

```yaml
version: 1
id: bug-fix
title: "Bug Fix"
description: "Analyze an issue, implement a fix, self-review, raise PR"
tags: [code, bugfix, pr]

parameters:
  - name: repo_url
    type: string
    required: true
  - name: issue_body
    type: string
    required: true
  - name: verifiers
    type: json
    required: false
    default: []

steps:
  - id: analyze
    title: "Analyze"
    sandbox_group: main
    mode: report
    repositories:
      - url: "{{ .Params.repo_url }}"
    execution:
      agent: claude-code
      prompt: |
        Analyze this issue and identify root cause:
        {{ .Params.issue_body }}
      output:
        schema:
          type: object
          properties:
            root_cause: { type: string }
            affected_files: { type: array, items: { type: string } }
            fix_strategy: { type: string }

  - id: fix
    title: "Implement Fix"
    depends_on: [analyze]
    sandbox_group: main
    mode: transform
    approval_policy: agent
    repositories:
      - url: "{{ .Params.repo_url }}"
    execution:
      agent: claude-code
      prompt: |
        Fix this issue.
        Root cause: {{ .Steps.analyze.output.root_cause }}
        Strategy: {{ .Steps.analyze.output.fix_strategy }}
      verifiers: "{{ .Params.verifiers }}"
    pull_request:
      branch_prefix: "auto/fix"
      title: "fix: {{ .Params.issue_body | truncate 60 }}"
```

---

## 6. DAG Execution Model

### 6.1 Temporal Topology

```
DAGWorkflow (one per Run)
├─ Resolve parameter templates
├─ Topological sort of steps
├─ For each ready tier (deps satisfied):
│   ├─ Launch StepWorkflow children in parallel
│   └─ Collect StepOutputs → resolve templates for next tier
└─ Aggregate RunResult

StepWorkflow (one per step per Run)
├─ Activity: ProvisionSandbox      sandbox.create(image, env)
│                                  (skipped if sandbox_group reuse)
├─ Activity: ExecuteStep           git clone + run agent (streaming)
│   ├─ Writes log lines to DB incrementally (for SSE)
│   └─ Returns StepOutput (diff / report JSON / pr_url / branch / error)
├─ [If approval_policy triggers pause]
│   └─ Workflow pauses, signals: approve / reject / steer
│      On steer: re-run ExecuteStep with amended prompt + history
├─ Activity: RunVerifiers          sandbox.commands.run per verifier
├─ Activity: CollectOutput         sandbox.files.read_file (reports)
├─ Activity: CreatePR              sandbox.commands.run("gh pr create")
└─ Activity: CleanupSandbox        sandbox.kill()
                                   (skipped if sandbox_group reuse;
                                    DAGWorkflow cleans up on group completion)
```

### 6.2 Fan-Out Steps

A step with `repositories` as a list spawns one sub-child per repo, up to `max_parallel`,
using the same group orchestration pattern from current Fleetlift. Each repo gets its own
sandbox regardless of `sandbox_group`. The StepOutput for a fan-out step is an array of
per-repo results.

### 6.3 Approval Policy

The HITL trigger is evaluated at the end of `ExecuteStep`:

| Policy | Pause condition |
|---|---|
| `always` | Always pause for human review |
| `never` | Never pause; auto-proceed |
| `agent` | Pause iff `agent.output.needs_review == true` |
| `on_changes` | Pause iff diff is non-empty |

`approval_policy: agent` requires the step's output schema to include a `needs_review`
boolean. The agent sets this based on its own assessment (ambiguity, risk, etc.).

**Mid-execution pause:** if `allow_mid_execution_pause: true`, the agent can emit a
structured event to stdout signalling it needs input before continuing. Due to execd's
`AllowStdin: false` constraint, this is implemented as pause-and-restart: the running
command is killed, the user's response is collected, and the step restarts with original
prompt + conversation history + user response appended.

### 6.4 Template Resolution

Cross-step references are resolved in the parent `DAGWorkflow` before launching each child,
not inside the sandbox:

```
{{ .Steps.analyze.output.root_cause }}   → scalar field from StepOutput
{{ .Steps.scan.outputs | toJSON }}       → full array of per-repo outputs (fan-out)
{{ .Steps.fix.output.branch_name }}      → git branch created by upstream step
{{ .Params.repos }}                      → run-time parameter
```

Unknown variables fail rendering and abort the run with a clear error.

### 6.5 Sandbox Groups

Steps in the same `sandbox_group` share one container. The sandbox is provisioned once
(when the first step in the group is launched) and cleaned up once (after the last step in
the group completes). This eliminates re-cloning and dependency reinstallation for
sequential steps on the same repository.

Steps in the same group must use the same image. Fan-out steps cannot share a sandbox
group (each repo gets its own container).

The worker calls `POST /sandboxes/{id}/renew-expiration` periodically to prevent TTL
expiry during long HITL waits.

---

## 7. Context Passing Between Steps

Three mechanisms, chosen based on data type:

**Structured output → template variables**
Small JSON data (report fields, analysis results, classifications) flows via `StepOutput`
and is templated into downstream prompts. Size limit ~64 KB.

**Code changes → git branches**
A transform step commits to a branch and pushes. Downstream steps reference the branch via
`{{ .Steps.fix.output.branch_name }}`. Git is the artifact store for code; no file
transfer needed.

**Files and documents → artifact store**
Non-code files (reports, generated configs, binaries) are extracted by the worker after a
step completes (`sandbox.files.read_file`) and stored as Artifacts (PostgreSQL for <1 MB,
object storage for larger). The next step's sandbox receives them via
`sandbox.files.write_file` before the agent starts.

Steps declare their artifact interface:
```yaml
outputs:
  artifacts:
    - path: /workspace/generated/config.yaml
      name: generated_config

inputs:
  artifacts:
    - name: generated_config           # from an upstream step
      mount_path: /workspace/input/config.yaml
```

---

## 8. Template Provider System

```go
type TemplateProvider interface {
    List(ctx context.Context, teamID string) ([]WorkflowTemplate, error)
    Get(ctx context.Context, teamID, id string) (*WorkflowTemplate, error)
    Save(ctx context.Context, teamID string, t *WorkflowTemplate) error
    Delete(ctx context.Context, teamID, id string) error
    Writable() bool
}
```

**`BuiltinProvider`** — nine workflow templates embedded in the binary via `//go:embed`.
Read-only. Available to all teams. Lower priority than DatabaseProvider.

**`DatabaseProvider`** — team-owned workflows stored in PostgreSQL. Writable via UI or
CLI. A team can fork a builtin (copies to their DatabaseProvider) and customise it without
affecting the original.

**`GitProvider`** *(future)* — polls a configured git repo for
`.fleetlift/workflows/*.yaml`. Enables version-controlled team workflows.

Discovery merges providers for a given team. A workflow with the same ID in DatabaseProvider
overrides the same ID in BuiltinProvider.

---

## 9. Pluggable Agent Runners

```go
type AgentRunner interface {
    Name() string
    Run(ctx context.Context, sandbox Sandbox, opts AgentRunOpts) (<-chan AgentEvent, error)
}

type AgentRunOpts struct {
    Prompt      string
    WorkDir     string
    Environment map[string]string
    MaxTurns    int
}

type AgentEvent struct {
    Type    string          // stdout_line | stderr_line | complete | error | needs_input
    Content string
    Output  map[string]any  // structured output on complete
}
```

**`ClaudeCodeRunner`** — invokes `claude -p "..." --output-format stream-json` via
`sandbox.commands.run`. Parses streaming JSON events and emits `AgentEvent`s.

**`CodexRunner`** *(future)* — starts `codex app-server`, handshakes via JSON-RPC over
stdio, drives turns. Follows the Symphony app-server protocol.

The workflow template specifies the agent per step: `execution.agent: claude-code`.
Credentials are injected by the platform from team credential store.

Skills, plugins, and MCPs are configured as part of the agent's environment, mounted into
the sandbox before the agent starts.

---

## 10. Auth & Multi-Tenancy

### 10.1 Auth Flow (GitHub OAuth)

```
GET /auth/github  →  github.com/login/oauth/authorize
                  →  GET /auth/github/callback?code=...
                  →  exchange code → fetch github.com/user
                  →  upsert User (provider=github, provider_id=github_id)
                  →  issue signed JWT (user_id, team_ids, roles, exp=1h)
                  →  set HttpOnly cookie (web) or return token (CLI)
```

JWT is short-lived (1 hour), refreshed via a refresh token in the database. No database
hit on the hot path — JWT is validated by signature only.

**Pluggable auth:**
```go
type AuthProvider interface {
    Name() string
    AuthURL(state string) string
    Exchange(ctx context.Context, code string) (*ExternalIdentity, error)
}
```

### 10.2 Roles

- `member` — create and interact with Runs for their team's workflows
- `admin` — full team access including members, credentials, workflow management
- `platform_admin` — cross-team access (flag on User, not per-team role)

### 10.3 Credentials

Team-scoped secrets stored encrypted in PostgreSQL. Referenced by name in workflow YAML,
resolved at run time, injected as env vars into `Sandbox.create()`. Never logged.

```bash
fleetlift credential set ANTHROPIC_API_KEY sk-ant-...
fleetlift credential set GITHUB_TOKEN ghp_...
```

**Service accounts:** long-lived API keys for CI/CD triggers, scoped to team + role.

---

## 11. Streaming

Agent log lines are written to a `step_run_logs` table (append-only, sequence number per
`step_run_id`) by the `ExecuteStep` activity as they arrive from the execd SSE stream. The
API server's `/runs/{id}/events` SSE endpoint tails this table for the relevant run and
pushes events to connected browsers. No direct worker→browser connection needed.

This means:
- Streaming survives worker restarts (logs are in the DB)
- Multiple browser tabs can watch the same run
- Logs are queryable after run completion

---

## 12. UI

**Four top-level areas:**

### Workflow Library
Browse and search all templates (builtins + team-owned). Cards show title, description,
tags, last-run stats. Actions: Run, Fork (builtins), Edit (team-owned), View DAG.

**DAG visualisation** — read-only graph. Nodes are steps (colour-coded by type: agent,
action, fan-out). Edges are dependencies. Clicking a node shows its prompt and config. Live
status overlay on Run Detail view (nodes colour by current phase; active step pulses).

### Runs
Live list of all runs for the team. Filterable by status. Each row shows workflow name,
triggered by, elapsed time, current step.

**Run Detail:**
- DAG with live status overlay
- Step panel: streaming logs, output artifacts, diff viewer
- Timeline: ordered step events with timestamps
- HITL panel (when step is `awaiting_input`): diff/report + Approve / Reject / Steer inline

### Inbox
Unified "needs your attention" surface. Two item types:

- **Awaiting input** — step paused, needs Approve / Reject / Steer
- **Output ready** — run completed with a deliverable (report, results) not yet viewed

Dismissing clears the notification. The underlying Run and output remain permanently
accessible via the Run Detail view and Reports section.

### Reports
All report outputs across all runs, newest first. Filterable by workflow type and date.
Searchable across report content. Each report downloadable as JSON or Markdown. The badge
on an item clears when first viewed (Inbox or directly).

---

## 13. CLI

```bash
# Auth
fleetlift auth login / logout / status

# Workflows
fleetlift workflow list
fleetlift workflow show <id>
fleetlift workflow create -f flow.yaml
fleetlift workflow edit <id>
fleetlift workflow fork <id> --as <name>

# Runs
fleetlift run <workflow-id> --param k=v ...
fleetlift run list [--status <status>]
fleetlift run status <run-id>
fleetlift run logs <run-id> [--step <id>]
fleetlift run diff <run-id> [--step <id>]
fleetlift run approve <run-id> [--step <id>]
fleetlift run reject  <run-id> [--step <id>]
fleetlift run steer   <run-id> --step <id> --prompt "..."
fleetlift run cancel  <run-id>
fleetlift run output  <run-id> [--step <id>] [--format json|markdown]

# Inbox
fleetlift inbox
fleetlift inbox read <run-id>

# Credentials
fleetlift credential set <name> <value>
fleetlift credential list
fleetlift credential delete <name>
```

`--step` defaults to the currently active HITL step when omitted.

---

## 14. Action Step Catalog

Non-agent steps that call external APIs. No sandbox needed — executed as Temporal
activities directly.

| Type | Purpose |
|---|---|
| `notify_slack` | Post message to Slack channel |
| `github.post_review_comment` | Post review comment on a PR |
| `github.assign_issue` | Assign an issue to a user |
| `github.add_label` | Add label to issue or PR |
| `github.post_issue_comment` | Post a comment on an issue |
| `jira.add_comment` | Add comment to Jira ticket |
| `jira.transition` | Transition Jira issue to a new state |
| `jira.assign` | Assign Jira issue |
| `webhook` | POST JSON payload to a URL |

Action steps declare credentials and config:
```yaml
- id: post_review
  action: github.post_review_comment
  depends_on: [review]
  config:
    repo: "{{ .Params.repo_url }}"
    pr_number: "{{ .Params.pr_number }}"
    body: "{{ .Steps.review.output.summary }}"
```

---

## 15. Pre-Built Workflow Templates

Nine builtin templates covering core DX use cases.

### fleet-research
Parallel codebase research across a fleet of repos. Each repo runs in its own sandbox.
Results collated and delivered to Inbox as output-ready.

```
fan-out: [research × N repos] → collate → output (Inbox)
```
Parameters: `repos`, `prompt`, `output_schema`, `max_parallel`

---

### fleet-transform
Parallel code transformation across a fleet of repos. Approval gate before PRs created.
Failure threshold pauses the fan-out.

```
fan-out: [transform × N repos] → [awaiting_input] → PRs
```
Parameters: `repos`, `prompt`, `verifiers`, `pr_config`, `max_parallel`, `failure_threshold`

---

### dependency-update
Research phase identifies affected repos; transform phase targets only those repos.

```
fan-out: [identify × N repos] → fan-out: [transform × affected repos] → PRs
```
Parameters: `repos`, `dependency_name`, `current_version`, `target_version`, `verifiers`

---

### bug-fix
Sequential analysis, fix, and self-review in a shared sandbox. Agent decides if approval
is needed.

```
analyze → fix → self-review → notify
sandbox_group: main (all agent steps)
approval_policy: agent on fix step
```
Parameters: `repo_url`, `issue_body`, `verifiers`, `slack_channel`

---

### pr-review
Automated code review: fetch PR diff, run agent review, post comments back to GitHub.
No HITL — fully automated.

```
[fetch PR] → [review code] → [post comments]
sandbox_group: main
```
Parameters: `repo_url`, `pr_number`

---

### migration
Analyze impact across fleet, transform affected repos, validate, create PRs, notify teams.

```
fan-out: [analyze × N repos] → fan-out: [transform × affected repos]
  → fan-out: [validate × transformed repos] → [awaiting_input] → PRs → notify
```
Parameters: `repos`, `migration_description`, `verifiers`, `notify_channel`

---

### triage
Analyze an issue, classify severity, assign, and comment. Fully automated.

```
analyze → classify → assign → comment
sandbox_group: main
```
Parameters: `repo_url`, `issue_number`, `issue_body`

---

### audit
Parallel security/compliance scan across fleet. Agent synthesises per-repo reports into a
single collated report for the compliance team.

```
fan-out: [scan × N repos] → [collate] → notify_compliance → output (Inbox)
```
Parameters: `repos`, `audit_prompt`, `compliance_channel`

The `collate` step is an agent step whose prompt receives `{{ .Steps.scan.outputs | toJSON }}`
— the full array of per-repo structured outputs.

---

### incident-response
Analyze logs, propose fix, apply fix with approval, verify deployment.

```
analyze_logs → propose_fix → [fix (awaiting_input)] → deploy_check → notify
sandbox_group: main (agent steps)
```
Parameters: `repo_url`, `log_excerpt`, `incident_description`, `deploy_check_command`

Log content is passed as a parameter (`log_excerpt`), or the agent accesses log tooling
via a configured MCP server.

---

## 15a. Step Infrastructure Configuration

Each step can declare a `sandbox` block that configures the execution environment
independently of what the agent does. This is separate from `agent_config` (Claude/Codex
settings) and `execution` (prompt and verifiers).

```yaml
steps:
  - id: analyze-logs
    execution:
      agent: claude-code
      prompt: "Analyze these logs for anomalies"
    sandbox:
      image: "my-registry.internal/claude-agent:v2"  # override default agent image
      resources:
        cpu: "2"
        memory: "4Gi"
        gpu: false
      egress:
        allow:
          - "api.datadoghq.com"
          - "*.internal.corp.com"      # private services
          - "registry.npmjs.org"       # package install during setup
        deny_all_by_default: true      # explicit allowlist only
      timeout: "45m"                   # override workflow-level default
      workspace_size: "10Gi"           # for large repos

  - id: run-ml-analysis
    sandbox:
      resources:
        gpu: true
        memory: "16Gi"
      egress:
        allow:
          - "huggingface.co"
```

**Resolution order** — `ProvisionSandbox` merges three sources, highest priority first:

1. **Step-level `sandbox` block** — explicit per-step overrides
2. **Workflow-level `defaults.sandbox`** — shared defaults for all steps in a workflow
3. **Platform defaults** — set by platform admins; defines the safe baseline
   (e.g. no GPU, no egress without explicit allow)

Teams can only expand within what the platform policy permits. A platform admin can enforce
that no sandbox accesses the internet without an explicit `egress.allow` entry — individual
steps cannot override this ceiling.

The `SandboxSpec` model:

```go
type SandboxSpec struct {
    Image         string            `yaml:"image,omitempty"`
    Resources     SandboxResources  `yaml:"resources,omitempty"`
    Egress        EgressPolicy      `yaml:"egress,omitempty"`
    Timeout       string            `yaml:"timeout,omitempty"`
    WorkspaceSize string            `yaml:"workspace_size,omitempty"`
}

type SandboxResources struct {
    CPU    string `yaml:"cpu,omitempty"`     // e.g. "2"
    Memory string `yaml:"memory,omitempty"`  // e.g. "4Gi"
    GPU    bool   `yaml:"gpu,omitempty"`
}

type EgressPolicy struct {
    Allow           []string `yaml:"allow,omitempty"`
    DenyAllByDefault bool    `yaml:"deny_all_by_default,omitempty"`
}
```

MCP server egress hosts are automatically merged into the step's egress allowlist by
`ProvisionSandbox` — workflow authors don't need to duplicate them.

---

## 15b. Agent Sandbox Configuration (MCP, Skills, Plugins)

Claude Code reads `~/.claude/settings.json` inside the container for MCP server configuration
and `~/.claude/` for skills (slash commands) and plugins.

The `StepDef` carries an optional `agent_config` block:

```yaml
execution:
  agent: claude-code
  agent_config:
    mcp_servers:
      - name: github
        type: stdio                                        # stdio | sse
        command: "npx -y @modelcontextprotocol/server-github"
        credentials: [GITHUB_TOKEN]                       # resolved from team credential store
      - name: datadog
        type: sse
        url: "https://mcp.datadoghq.com/sse"
        credentials: [DD_API_KEY]
        headers:
          DD-API-KEY: "$DD_API_KEY"
    skills:
      - source: repo                                       # read from target repo's .claude/
        path: .claude/commands/
      - source: builtin                                    # bundled in platform
        name: git-helpers
    context_files:
      - CLAUDE.md                                          # inject repo-level context into sandbox
```

**How it works:**

The `ProvisionSandbox` activity writes configuration files into the sandbox before the
agent starts, using `sandbox.files.write_file()`:

1. **`~/.claude/settings.json`** — generated from `mcp_servers` config. Credentials are
   resolved from the team credential store and injected as env vars in the sandbox; the
   settings file references them by env var name (e.g. `$GITHUB_TOKEN`).

2. **`~/.claude/commands/`** — skill files written if `skills[].source == builtin` or
   fetched from the target repo if `skills[].source == repo` (via initial git clone).

3. **`CLAUDE.md` context** — if `context_files` includes `CLAUDE.md`, it is read from the
   cloned repository and copied to the sandbox working directory so Claude Code picks it up
   automatically.

**Remote MCP (SSE type):** requires outbound network access from the sandbox to the MCP
endpoint. OpenSandbox's `egress` rules on sandbox creation must allowlist the target hosts.
The platform resolves required egress hosts from the `mcp_servers` config and passes them
to `Sandbox.create()`.

**Network policy example for `ProvisionSandbox`:**
```go
egressHosts := extractMCPHosts(step.Execution.AgentConfig.MCPServers)
sandboxID, err = sbClient.Create(ctx, sandbox.CreateOpts{
    Image:        agentImage(agent),
    Env:          resolvedEnv,
    EgressHosts:  egressHosts,   // ["mcp.datadoghq.com", "api.github.com"]
    TimeoutMins:  120,
})
```

---

## 16. Implementation Approach

Fresh branch from current Fleetlift `main`. Reuse:
- Temporal SDK patterns (workflow, activity, signal, query)
- OpenSandbox SDK and provider interface
- Grouped execution logic (fan-out, failure thresholds)
- Knowledge capture and enrichment activities
- Prometheus metrics, structured logging
- React SPA shell, shadcn/ui components, SSE infrastructure

Replace / remove:
- `fleetlift-agent` binary (no sidecar)
- File-based protocol (`manifest.json`, `status.json`, `result.json`, `steering.json`)
- Hardcoded `transform` / `report` task model
- Single-tenant server with no auth

New:
- `DAGWorkflow` and `StepWorkflow` Temporal workflows
- `WorkflowTemplate` model and `TemplateProvider` interface
- `AgentRunner` interface and `ClaudeCodeRunner`
- Auth layer (JWT, GitHub OAuth)
- Team / User / Credential data model (PostgreSQL)
- `step_run_logs` table for streaming
- `Artifact` storage (PostgreSQL + object store)
- Action step catalog
- Inbox model and Reports section in UI
- Nine builtin workflow template YAMLs
