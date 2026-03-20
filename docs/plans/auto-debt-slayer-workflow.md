# Plan: Auto-Debt-Slayer as a Fleetlift Workflow

**Date:** 2026-03-20
**Status:** Draft
**Reference:** [auto-debt-slayer](../../../auto-debt-slayer), [opensandbox-migration proposal](../../../auto-debt-slayer/.worktrees/opensandbox-migration/docs/plans/proposal-open-sandbox-migration.md), [workflow-expressiveness PRD](./2026-03-18-workflow-expressiveness-prd.md)

---

## Summary

Reimplement the auto-debt-slayer (ADS) pipeline as a native Fleetlift workflow. ADS takes a Jira ticket, enriches it with codebase context, runs a feasibility assessment, executes Claude Code in a sandbox (with an internal TDD/review loop), and creates a draft PR.

The migration uses Fleetlift's standard transform mode: the agent makes file changes in the sandbox working tree; the platform handles `git commit`, `git push`, and PR creation via the `pull_request` config on the execute step. The TDD/review loop (superpowers skills) runs on the uncommitted working tree inside the sandbox.

---

## Workflow DAG

```
enrich (report)
    └─ assess (report)
           ├─ execute (transform)   ← condition: decision == "execute"
           │       └─ notify (action, optional)
           └─ notify (action, optional)   ← manual_needed path, condition: decision == "manual_needed"
```

4 steps, all sequential (no fan-out). Each ticket is one run. The `notify` step appears in both branches; in practice a single optional notify step with a template-rendered message covers both cases (the message template reads from whichever of assess/execute has data).

---

## Workflow Parameters

| Name | Type | Required | Default | Notes |
|------|------|----------|---------|-------|
| `ticket_key` | string | yes | — | Jira key, e.g. `AFX-1234` |
| `jira_base_url` | string | yes | — | Jira instance URL, e.g. `https://myorg.atlassian.net` |
| `jira_project_key` | string | no | — | Jira project key (for sub-task creation in resume mode) |
| `github_repo` | string | yes | — | `https://github.com/org/repo` |
| `github_default_branch` | string | no | `master` | Target branch for PR base |
| `slack_channel_id` | string | no | — | Channel to notify on completion |
| `slack_thread_ts` | string | no | — | Thread to reply in (if triggered from Slack) |
| `per_task_budget_usd` | string | no | `10.00` | Soft cap; communicated to agents in prompt |

---

## Steps

### Step 1: `enrich` (mode: report)

**Agent:** `claude-code`
**Sandbox image:** `auto-debt-slayer-agent:latest` (see Agent Image section)

**What it does:**
- Fetch the Jira ticket via `acli` (`JIRA_BASE_URL` set from `jira_base_url` param)
- Clone the target repo (blobless) and search for related files (grep, CODEOWNERS, recent commits)
- Resolve linked docs via MCP (Glean, Confluence) — failures are non-fatal
- Emit structured JSON

**Output schema:**
```yaml
output:
  type: object
  properties:
    ticket_summary: { type: string }
    ticket_description: { type: string }
    acceptance_criteria: { type: string }
    related_files: { type: array, items: { type: string } }
    file_snippets: { type: array }
    linked_docs: { type: array }
    code_owners: { type: array, items: { type: string } }
    recent_commits: { type: array, items: { type: string } }
    directory_tree: { type: string }
    comments: { type: array, items: { type: string } }
```

**Credentials:** `JIRA_API_TOKEN`, `GITHUB_TOKEN`, `GLEAN_TOKEN` (optional)

---

### Step 2: `assess` (mode: report)

**Depends on:** `enrich`
**Agent:** `claude-code`

**What it does:** 5-question structured feasibility assessment. The decision is computed from the answers by code, not LLM gut-feel:

| Question | Answers |
|----------|---------|
| Requirements clarity? | `yes` / `partially` / `no` |
| Scope identifiable? | `yes` / `partially` / `no` |
| Verifiable? | `tests_exist` / `can_write_test` / `no_verification` |
| External dependency? | bool |
| Human judgment required? | bool |

**Decision rules:**
- `clarity == "no"` → `manual_needed`
- `clarity == "partially" && scope == "no"` → `manual_needed`
- `scope == "no" && estimated_files >= 3` → `manual_needed`
- Otherwise → `execute` (caveats injected into execute prompt)

The assess step also produces `pr_title_hint` and `pr_body_draft` — used by the execute step's `pull_request` config, since PR config is resolved before the execute step runs and cannot reference the execute step's own output.

**Output schema:**
```yaml
output:
  type: object
  properties:
    decision: { type: string, enum: [execute, manual_needed] }
    decision_reasons: { type: array, items: { type: string } }
    caveats: { type: array, items: { type: string } }
    estimated_complexity: { type: string }
    estimated_files: { type: integer }
    risks: { type: array, items: { type: string } }
    pr_title_hint: { type: string }   # e.g. "fix(AFX-1234): short description"
    pr_body_draft: { type: string }   # ticket context + assessment + caveats
```

---

### Step 3: `execute` (mode: transform)

**Depends on:** `assess`
**Condition:** `{{ eq .steps.assess.output.decision "execute" }}`
**Agent:** `claude-code`
**Sandbox image:** `auto-debt-slayer-agent:latest`

**What it does:**
1. Run Claude with superpowers skills: TDD → implement → code-review → fix → verify
2. All work happens on the sandbox working tree (agent does **not** commit or push)
3. After agent exits, the platform runs `git add -A && git commit && git push` and creates the PR using the `pull_request` config below

**Important:** The `superpowers:brainstorming` skill must be explicitly excluded from the prompt — it blocks execution until a human approves a design, which is incompatible with autonomous operation.

**Prompt:** Assembled from:
- Enrich output (ticket, related files, linked docs, codeowners, recent commits)
- Assess output (caveats, risks, relevance guidance)
- FIX_RULES embedded in image (`/agent/fix-rules.md`)
- Per-task budget limit (soft cap, communicated to agent)

**Output schema:**
```yaml
output:
  type: object
  properties:
    agent_summary: { type: string }
    review_approved: { type: boolean }
    review_attempts: { type: integer }
    review_notes: { type: string }
    total_cost_usd: { type: number }
```

**Pull request config** (requires PRD Improvement 2 — `pull_request` field template rendering):
```yaml
pull_request:
  branch_prefix: "agent/{{ .params.ticket_key | lower }}-"
  title: "{{ .steps.assess.output.pr_title_hint }}"
  body: "{{ .steps.assess.output.pr_body_draft }}"
  draft: "true"
```

Note: `pull_request` config is resolved from prior step outputs (`assess`, `enrich`) and params before the step runs. It cannot reference `execute`'s own output.

**Credentials:** `GITHUB_TOKEN`, `ANTHROPIC_API_KEY`

---

### Step 4: `notify` (action, optional)

**Depends on:** `execute` (optional — runs even if execute is skipped)
**Action type:** `slack_notify`
**Optional:** true

**Config:**
```yaml
action:
  type: slack_notify
  config:
    channel: "{{ .params.slack_channel_id }}"
    thread_ts: "{{ .params.slack_thread_ts }}"
    message: |
      {{ if eq .steps.assess.output.decision "execute" -}}
      ✅ PR created: {{ .steps.execute.output.pr_url }}
      Cost: ${{ .steps.execute.output.total_cost_usd }} | Review: {{ .steps.execute.output.review_notes }}
      {{- else -}}
      🔍 Manual review needed for {{ .params.ticket_key }}: {{ .steps.assess.output.decision_reasons | join ", " }}
      {{- end }}
```

Action config string values are rendered through `RenderPrompt` before dispatch (`dag.go:350–374`), so Go template syntax works in `channel`, `message`, etc. — no platform change needed.

**Credentials:** `SLACK_BOT_TOKEN`

---

## Agent Image

New image: `auto-debt-slayer-agent:latest` (extends `claude-code:latest`)

**Additions:**
- `acli` (Atlassian CLI) — Jira ticket fetch
- `jq`, `curl` (standard tooling)
- Superpowers plugin installed as agent user: `claude plugin install superpowers@4.3.1`
  - Active skills: `test-driven-development`, `requesting-code-review`, `receiving-code-review`, `verification-before-completion`
  - Excluded from prompts: `superpowers:brainstorming`
- `/agent/fix-rules.md` — non-negotiable coding rules injected into every execute prompt

---

## Credential Requirements

| Name | Type | Steps |
|------|------|-------|
| `GITHUB_TOKEN` | env | enrich (clone), execute (push + PR) |
| `JIRA_API_TOKEN` | env | enrich (acli) |
| `ANTHROPIC_API_KEY` | env | execute (Claude subagent for TDD/review) |
| `SLACK_BOT_TOKEN` | env | notify |
| `GLEAN_TOKEN` | env | enrich (optional) |

---

## Slack Trigger Layer (External to Fleetlift)

The Slack bot is out of scope for Fleetlift. It is a separate service that translates Slack events into Fleetlift run requests:

```
Slack @mention
  └─ Slack Bot (external service)
        ├─ Parse ticket key from message
        ├─ Check GitHub for existing open PR → set resume_branch if found
        └─ POST /api/workflows/auto-debt-slayer/runs
             {
               team_id: "...",
               parameters: {
                 ticket_key, jira_base_url, github_repo,
                 slack_channel_id, slack_thread_ts
               }
             }
```

---

## Resume Mode

When there is an existing open PR for a ticket, the flow changes: skip assessment, fetch PR review comments, prompt the agent to address them in a targeted pass.

Implemented as a **separate workflow template** (`auto-debt-slayer-resume`) with parameters:
- `ticket_key`, `jira_base_url`, `github_repo`, `slack_channel_id`, `slack_thread_ts`
- `resume_branch` (the existing agent branch)

The resume template has a single `execute` step: checks out `resume_branch`, fetches PR comments via GitHub API, runs Claude with a targeted prompt. The platform commits and pushes the new changes; the existing PR is updated automatically (GitHub detects new commits).

---

## Required Platform Changes

Three changes are needed before the ADS workflow can be implemented. All are pre-existing work items:

### 1. `evalCondition` must expose step output — bug fix

**Location:** `internal/workflow/dag.go:763–771`

`evalCondition` builds its context with only `status` and `error` per step:
```go
steps[id] = map[string]any{"status": ..., "error": ...}
```
`output` is absent, so `{{ eq .steps.assess.output.decision "execute" }}` silently evaluates to false — the execute step is always skipped.

**Fix:** Add `"output": out.Output` to the per-step context map. One line.

Note: `evalCondition` uses lowercase keys (`.steps`, `.params`), while `RenderPrompt` uses the `RenderContext` struct fields (`.Steps`, `.Params`). Condition YAML uses lowercase:
```yaml
condition: '{{ eq .steps.assess.output.decision "execute" }}'
```

---

### 2. `pull_request` config must be template-rendered

**Covered by:** PRD Improvement 2 (P1)

`resolveStep` (`dag.go:650`) assigns `opts.PRConfig = step.PullRequest` with no template rendering. The ADS workflow needs `branch_prefix`, `title`, and `body` to reference params and prior step outputs. PRD Improvement 2 applies `RenderPrompt` to all string and bool fields of `PRDef`.

---

### 3. Skip PR creation when working tree is clean

**Covered by:** PRD Improvement 1 (P1)

If the execute step's agent determines there is nothing to fix and makes no file changes, the platform must not attempt `git commit` on a clean working tree (currently this fails as an error). PRD Improvement 1 adds a `git status --porcelain` check before committing — clean tree = silent skip.

---

## What's Out of Scope (v1)

- **Hard budget enforcement** — the per-task budget is communicated to agents in the prompt as a soft cap only; no platform-level cancellation. Post-v1 platform change: add `max_cost_usd` to `WorkflowDef`, cancel run if `TotalCostUSD` exceeds cap after any step.
- **Daily budget cap** — soft enforcement only
- **Knowledge base learning** — fix-patterns and failure records are baked into the agent image as static files
- **Deduplication** — the Slack bot layer prevents two concurrent runs for the same ticket; Fleetlift has no built-in deduplication
- **Intake channel / auto-create Jira sub-tasks** — Slack bot responsibility

---

## Open Questions

1. Should resume mode be a separate workflow template or a `mode` parameter on this template? **Recommend separate template** (cleaner prompt logic, avoids conditional branching across all steps).
