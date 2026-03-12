# Workflow Reference

Workflow templates define DAG-based agentic jobs. They are stored as YAML and can be built-in (embedded) or team-owned (database-stored). Templates accept parameters and produce typed step outputs that downstream steps can consume.

---

## Top-level fields (`WorkflowDef`)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `version` | int | yes | Schema version. Must be `1`. |
| `id` | string | yes | Unique slug for the workflow (e.g. `add-tests`). |
| `title` | string | yes | Human-readable name shown in the UI and CLI. |
| `description` | string | no | Short description of what the workflow does. |
| `tags` | []string | no | Free-form tags for filtering/searching. |
| `parameters` | []ParameterDef | no | Input parameters; values supplied at run start. |
| `steps` | []StepDef | yes | Ordered list of DAG steps (order does not imply execution order — use `depends_on`). |

---

## ParameterDef

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Parameter key used in template expressions (`{{ .Params.name }}`). |
| `type` | string | yes | One of: `string`, `int`, `bool`, `json`. |
| `required` | bool | no | If true, the run will fail if the param is not provided. |
| `default` | any | no | Default value used when param is omitted. |
| `description` | string | no | Human-readable description shown in the UI. |

---

## StepDef

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | yes | Unique step identifier within the workflow. Used in `depends_on` and template refs. |
| `title` | string | no | Human-readable step name. |
| `depends_on` | []string | no | Step IDs that must complete successfully before this step runs. |
| `sandbox_group` | string | no | Logical name for shared sandbox. Steps with the same group share one container. |
| `mode` | string | no | `transform` (default) or `report`. Controls whether output is a diff or structured data. |
| `repositories` | any | no | Repo list or Go-template expression resolving to a JSON repo array. |
| `max_parallel` | int | no | Max parallel repo executions within this step. |
| `failure_threshold` | int | no | Pause fan-out after this many repo-level failures. |
| `execution` | ExecutionDef | no | Agent execution config (prompt, verifiers, credentials). |
| `approval_policy` | string | no | When to pause for human approval: `always`, `never`, `agent`, `on_changes`. |
| `allow_mid_execution_pause` | bool | no | Allow HITL steering signals while the step is running. |
| `pull_request` | PRDef | no | PR creation config. Populated by create-PR steps. |
| `condition` | string | no | Go template expression; step is skipped if it evaluates to `false`. |
| `optional` | bool | no | If true, step failure does not block downstream steps. |
| `outputs` | StepOutputsDef | no | Artifacts produced by this step, made available to downstream steps. |
| `inputs` | StepInputsDef | no | Artifacts from upstream steps to mount into this step's sandbox. |
| `action` | ActionDef | no | Non-agent action (e.g. `create_pr`, `slack_notify`). Mutually exclusive with `execution`. |
| `sandbox` | SandboxSpec | no | Override sandbox resources/image/egress for this step. |
| `knowledge` | KnowledgeDef | no | Knowledge capture/injection config. |
| `timeout` | string | no | Go duration string (e.g. `30m`, `2h`). Overrides global timeout for this step. |

---

## ExecutionDef

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent` | string | yes | Agent type: `claude-code` or `codex`. |
| `prompt` | string | yes | Instruction sent to the agent. Supports Go template expressions. |
| `verifiers` | any | no | Commands to run after the agent finishes (e.g. `go build ./...`). |
| `credentials` | []string | no | Named credentials (from the credentials store) to inject as environment variables. |
| `output` | OutputSchemaDef | no | JSON schema for structured output (used in `report` mode). |

### OutputSchemaDef

| Field | Type | Description |
|-------|------|-------------|
| `schema` | map[string]any | JSON Schema object describing the expected output structure. |

---

## SandboxSpec

| Field | Type | Description |
|-------|------|-------------|
| `image` | string | Container image to use. Overrides `AGENT_IMAGE` env var. |
| `resources.cpu` | string | CPU allocation (e.g. `"2"`). |
| `resources.memory` | string | Memory allocation (e.g. `"4Gi"`). |
| `resources.gpu` | bool | Request GPU access. |
| `egress.allow` | []string | Allowlisted hostnames/CIDRs for outbound network. |
| `egress.deny_all_by_default` | bool | Block all outbound unless listed in `allow`. |
| `timeout` | string | Maximum wall-clock time for the sandbox. |
| `workspace_size` | string | Persistent workspace disk size (e.g. `"20Gi"`). |

---

## KnowledgeDef

Controls automatic knowledge capture (what the agent learned) and injection (prior knowledge into the prompt).

| Field | Type | Description |
|-------|------|-------------|
| `capture` | bool | Extract lessons from this step's agent session. |
| `inject` | bool | Inject relevant approved knowledge items into the prompt. |
| `tags` | []string | Tags used for knowledge retrieval matching. |

---

## PRDef

| Field | Type | Description |
|-------|------|-------------|
| `branch_prefix` | string | Git branch name prefix (e.g. `auto/add-tests`). |
| `title` | string | PR title. Supports Go template expressions. |
| `body` | string | PR body markdown. |
| `labels` | []string | GitHub labels to apply. |
| `draft` | bool | Create as a draft PR. |

---

## ActionDef

Steps without `execution` can run built-in actions:

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Action type: `create_pr` or `slack_notify`. |
| `config` | map[string]any | Action-specific config (see below). |

**`create_pr` config keys:** `branch_prefix`, `title`, `body`, `labels`, `draft`

**`slack_notify` config keys:** `channel`, `message`

---

## Artifacts (StepOutputsDef / StepInputsDef)

Steps can pass files between each other via named artifacts.

```yaml
# Producing step
outputs:
  artifacts:
    - path: /workspace/report.json   # path inside the sandbox
      name: audit-report             # artifact name referenced by consumers

# Consuming step
inputs:
  artifacts:
    - name: audit-report
      mount_path: /inputs/report.json
```

---

## Condition syntax

The `condition` field is a Go `text/template` expression. The step is skipped if the expression renders to anything other than `true`.

**Available variables:**

- `.Params` — map of parameter values supplied at run start
- `.Steps` — map of completed step outputs keyed by step ID

**Examples:**

```yaml
# Run only if the 'environment' param equals 'production'
condition: "{{ eq .Params.environment \"production\" }}"

# Run only if the scan step found findings
condition: "{{ gt (len .Steps.scan.findings) 0 }}"

# Always run (default when omitted)
condition: ""
```

---

## Complete annotated example

```yaml
version: 1
id: security-audit-and-fix
title: Security Audit and Fix
description: Scans repos for vulnerabilities, then patches critical ones.
tags:
  - security
  - audit
  - fleet

parameters:
  - name: repos
    type: json
    required: true
    description: "JSON array of {url, branch} objects"
  - name: severity_threshold
    type: string
    required: false
    default: "high"
    description: "Minimum severity to auto-fix: low | medium | high | critical"

steps:
  # Step 1: parallel scan across all repos (report mode — no code changes)
  - id: scan
    title: Scan for vulnerabilities
    mode: report
    repositories: "{{ .Params.repos }}"
    max_parallel: 10
    execution:
      agent: claude-code
      prompt: |
        Scan this repository for security vulnerabilities.
        Focus on dependency vulnerabilities, hardcoded secrets, and injection risks.
        Produce a JSON report at /workspace/findings.json.
      output:
        schema:
          type: object
          properties:
            findings:
              type: array
            critical_count:
              type: integer
    outputs:
      artifacts:
        - path: /workspace/findings.json
          name: scan-findings

  # Step 2: patch only if critical findings exist (transform mode)
  - id: patch
    title: Patch critical vulnerabilities
    depends_on:
      - scan
    condition: "{{ gt .Steps.scan.critical_count 0 }}"
    mode: transform
    repositories: "{{ .Params.repos }}"
    max_parallel: 5
    failure_threshold: 2
    execution:
      agent: claude-code
      prompt: |
        Apply patches for the critical vulnerabilities identified in the scan.
        Findings: {{ .Steps.scan.findings | toJSON }}
      verifiers:
        - name: build
          command: ["go", "build", "./..."]
        - name: test
          command: ["go", "test", "./..."]
    sandbox:
      resources:
        cpu: "2"
        memory: "4Gi"
    knowledge:
      capture: true
      inject: true
      tags: ["security", "vulnerability"]
    timeout: 45m

  # Step 3: human review before PRs are created
  - id: review
    title: Review patches
    depends_on:
      - patch
    approval_policy: always

  # Step 4: create PRs for approved changes
  - id: create-prs
    title: Create pull requests
    depends_on:
      - review
    action:
      type: create_pr
      config:
        branch_prefix: "security/auto-patch"
        title: "Security: patch critical vulnerabilities"
        labels:
          - security
          - automated
        draft: false

  # Step 5: optional Slack notification
  - id: notify
    title: Notify security team
    depends_on:
      - create-prs
    optional: true
    action:
      type: slack_notify
      config:
        channel: "#security-alerts"
        message: "Security patch PRs created. Review required."
```
