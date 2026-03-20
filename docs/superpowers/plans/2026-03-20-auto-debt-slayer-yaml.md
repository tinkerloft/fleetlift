# Auto-Debt-Slayer Workflow YAML Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create `internal/template/workflows/auto-debt-slayer.yaml` — a 4-step Fleetlift workflow that takes a Jira ticket, enriches it with codebase context, assesses feasibility, executes a Claude Code fix (with TDD/review loop), and creates a draft PR.

**Architecture:** Single YAML file embedded automatically via `//go:embed workflows/*.yaml` in `internal/template/builtin.go`. No Go code changes required. 4 steps: `enrich` (report) → `assess` (report) → `execute` (transform, conditional) → `notify` (action, optional). The `pull_request` config on the execute step uses template expressions referencing assess output — enabled by the platform fixes already merged to main.

**Tech Stack:** Fleetlift workflow YAML v1, Go templates, `go test ./internal/template/...`

**Design doc:** `docs/plans/auto-debt-slayer-workflow.md`

---

## Template context casing — important

Two different rendering paths use different context key casing:

| Renderer | Context | Key style |
|----------|---------|-----------|
| `evalCondition` (step `condition` field) | plain `map[string]any` | lowercase: `.steps`, `.params`, `.output` |
| `RenderPrompt` (prompts, `pull_request` fields, action `config` values) | `RenderContext` struct | exported: `.Steps`, `.Params`, `.Output` |

The `condition:` field → lowercase. Everything else → exported-case.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/template/workflows/auto-debt-slayer.yaml` | The workflow definition |
| Modify | `internal/template/builtin_test.go` | Update count, add parse/validate test |

---

## Chunk 1: Write and validate the workflow YAML

### Task 1: Write the workflow YAML and tests

**Files:**
- Create: `internal/template/workflows/auto-debt-slayer.yaml`
- Modify: `internal/template/builtin_test.go`

- [ ] **Step 1.1: Write the failing test first**

Read `internal/template/builtin_test.go` to understand the existing patterns (`p.Get`, `model.ParseWorkflowYAML`). Then make two changes:

**a) Update the template count** from 14 → 15 and add `"auto-debt-slayer"` to the expected slugs list:

```go
// BEFORE:
assert.Len(t, templates, 14)
for _, expected := range []string{
    "fleet-research", "fleet-transform", "bug-fix", "dependency-update",
    "pr-review", "migration", "triage", "audit", "incident-response",
    "sandbox-test", "mcp-test", "clone-test", "doc-assessment",
} {

// AFTER:
assert.Len(t, templates, 15)
for _, expected := range []string{
    "fleet-research", "fleet-transform", "bug-fix", "dependency-update",
    "pr-review", "migration", "triage", "audit", "incident-response",
    "sandbox-test", "mcp-test", "clone-test", "doc-assessment",
    "auto-debt-slayer",
} {
```

**b) Add a parse/validate test** using the same pattern as `TestSandboxTestWorkflowTemplate_Parses`:

```go
func TestAutoDebtSlayerWorkflowTemplate_Parses(t *testing.T) {
    p, err := NewBuiltinProvider()
    require.NoError(t, err)

    tmpl, err := p.Get(context.Background(), "", "auto-debt-slayer")
    require.NoError(t, err)
    assert.Equal(t, "Auto Debt Slayer", tmpl.Title)

    var def model.WorkflowDef
    require.NoError(t, model.ParseWorkflowYAML([]byte(tmpl.YAMLBody), &def))

    // 4 steps in correct order
    require.Len(t, def.Steps, 4)
    assert.Equal(t, "enrich", def.Steps[0].ID)
    assert.Equal(t, "assess", def.Steps[1].ID)
    assert.Equal(t, "execute", def.Steps[2].ID)
    assert.Equal(t, "notify", def.Steps[3].ID)

    // enrich: report mode, has execution and repositories
    assert.Equal(t, "report", def.Steps[0].Mode)
    assert.NotNil(t, def.Steps[0].Execution)

    // assess: report mode, depends on enrich
    assert.Equal(t, "report", def.Steps[1].Mode)
    assert.Contains(t, def.Steps[1].DependsOn, "enrich")

    // execute: transform, has condition and pull_request
    assert.Equal(t, "transform", def.Steps[2].Mode)
    assert.NotEmpty(t, def.Steps[2].Condition)
    assert.NotNil(t, def.Steps[2].PullRequest)
    assert.NotEmpty(t, def.Steps[2].PullRequest.BranchPrefix)
    assert.Contains(t, def.Steps[2].DependsOn, "assess")

    // notify: optional action step
    assert.True(t, def.Steps[3].Optional)
    assert.NotNil(t, def.Steps[3].Action)
    assert.Equal(t, "slack_notify", def.Steps[3].Action.Type)

    // required parameters
    paramNames := make([]string, len(def.Parameters))
    for i, p := range def.Parameters {
        paramNames[i] = p.Name
    }
    assert.Contains(t, paramNames, "ticket_key")
    assert.Contains(t, paramNames, "jira_base_url")
    assert.Contains(t, paramNames, "github_repo")
}
```

- [ ] **Step 1.2: Run to verify the test fails**

```bash
cd /Users/andrew/dev/projects/fleetlift && go test ./internal/template/... -run "TestAutoDebtSlayer|TestBuiltinProviderLoadsAll" -v -count=1
```

Expected: both tests FAIL — template not found, count is still 14.

- [ ] **Step 1.3: Create the workflow YAML**

Create `internal/template/workflows/auto-debt-slayer.yaml` with this exact content:

```yaml
version: 1
id: auto-debt-slayer
title: Auto Debt Slayer
description: >
  Autonomous ticket-to-PR pipeline. Enriches a Jira ticket with codebase context,
  assesses feasibility, runs a Claude Code fix with TDD/review loop, and creates
  a draft PR. Triggered externally (e.g. Slack bot) via the runs API.
tags:
  - automation
  - bug-fix
  - jira
  - single-repo

parameters:
  - name: ticket_key
    type: string
    required: true
    description: "Jira ticket key, e.g. AFX-1234"

  - name: jira_base_url
    type: string
    required: true
    description: "Jira instance base URL, e.g. https://myorg.atlassian.net"

  - name: jira_project_key
    type: string
    required: false
    description: "Jira project key (for sub-task creation in resume mode)"

  - name: github_repo
    type: string
    required: true
    description: "Target repository URL, e.g. https://github.com/org/repo"

  - name: github_default_branch
    type: string
    required: false
    description: "Base branch for PRs (default: master)"

  - name: slack_channel_id
    type: string
    required: false
    description: "Slack channel ID to post completion notification"

  - name: slack_thread_ts
    type: string
    required: false
    description: "Slack thread timestamp to reply in (if triggered from a thread)"

  - name: per_task_budget_usd
    type: string
    required: false
    description: "Soft cost cap communicated to agents in the prompt (default: 10.00)"

sandbox_groups:
  agent:
    image: auto-debt-slayer-agent:latest

steps:
  - id: enrich
    title: Enrich ticket context
    mode: report
    sandbox_group: agent
    repositories:
      - url: "{{ .Params.github_repo }}"
    execution:
      agent: claude-code
      credentials:
        - GITHUB_TOKEN
        - JIRA_API_TOKEN
        - GLEAN_TOKEN
      prompt: |
        You are enriching a Jira ticket with codebase context to prepare for automated fixing.

        ## Ticket
        Key: {{ .Params.ticket_key }}
        Jira base URL: {{ .Params.jira_base_url }}
        Budget: ${{ if .Params.per_task_budget_usd }}{{ .Params.per_task_budget_usd }}{{ else }}10.00{{ end }}

        ## Instructions

        1. Fetch the ticket using acli:
           ```
           JIRA_BASE_URL={{ .Params.jira_base_url }} acli jira issue get {{ .Params.ticket_key }} --output json
           ```
           Extract: summary, description, acceptance criteria, comments, linked issues.

        2. Search the repository for related files:
           - grep for identifiers, function names, and error messages mentioned in the ticket
           - Check OWNERS/CODEOWNERS for relevant owners
           - Run: git log --oneline -20 -- <related paths>

        3. Resolve linked documentation (Confluence, Slack threads, Google Docs) via MCP if available.
           Non-fatal: skip gracefully if MCP is unavailable.

        4. Build a directory tree of the most relevant areas of the codebase.

        ## Output

        Return a JSON object with these fields:
        - ticket_summary (string): one-line summary of the ticket
        - ticket_description (string): full ticket description
        - acceptance_criteria (string): acceptance criteria if present
        - related_files (array of strings): file paths most relevant to the ticket
        - file_snippets (array): [{path, content}] for the top 5 most relevant files
        - linked_docs (array): [{title, url, content}] for resolved linked documents
        - code_owners (array of strings): owners/reviewers for the relevant code areas
        - recent_commits (array of strings): recent commit messages for related paths
        - directory_tree (string): tree of relevant directories
        - comments (array of strings): ticket comments

  - id: assess
    title: Assess ticket feasibility
    mode: report
    sandbox_group: agent
    depends_on:
      - enrich
    execution:
      agent: claude-code
      prompt: |
        You are assessing whether a Jira ticket is suitable for autonomous fixing.

        ## Ticket context (from enrichment)
        {{ .Steps.enrich.Output | toJSON }}

        ## Budget
        Per-task budget: ${{ if .Params.per_task_budget_usd }}{{ .Params.per_task_budget_usd }}{{ else }}10.00{{ end }}

        ## Assessment questions

        Answer each question carefully based on the ticket context above:

        1. **requirements_clarity**: Are the requirements clear enough to implement without ambiguity?
           - "yes": requirements are explicit and unambiguous
           - "partially": some ambiguity but enough to proceed with reasonable assumptions
           - "no": requirements are unclear, contradictory, or missing key information

        2. **scope_identifiability**: Can you identify which files/functions need to change?
           - "yes": clear scope, specific files identified
           - "partially": rough scope known but details unclear
           - "no": scope is unknown or too broad

        3. **verifiability**: Can correctness be verified programmatically?
           - "tests_exist": existing tests cover this behaviour
           - "can_write_test": no existing tests but we can write one
           - "no_verification": no way to verify programmatically (UI-only, UX judgment, etc.)

        4. **external_dependency** (boolean): Does the fix require changes to external systems,
           third-party APIs, or manual configuration outside the codebase?

        5. **human_judgment** (boolean): Does the fix require subjective design decisions,
           product/UX judgment, or stakeholder alignment?

        ## Decision rules (apply these exactly — do not use judgment)

        - If requirements_clarity == "no" → decision = "manual_needed"
        - If requirements_clarity == "partially" AND scope_identifiability == "no" → decision = "manual_needed"
        - If scope_identifiability == "no" AND estimated_files >= 3 → decision = "manual_needed"
        - Otherwise → decision = "execute"

        Caveats (advisory, do not block execution):
        - If external_dependency is true: note it as a caveat
        - If human_judgment is true: note it as a caveat
        - If verifiability == "no_verification": note it as a caveat

        ## Also produce

        - **pr_title_hint**: A concise PR title in conventional commit format, e.g. "fix(AFX-1234): null pointer in auth handler"
        - **pr_body_draft**: A PR body (markdown) including: ticket reference, description of changes,
          acceptance criteria, assessment caveats, and any human TODOs for flagged items.

        ## Output

        Return a JSON object:
        - decision (string): "execute" or "manual_needed"
        - requirements_clarity (string): "yes" / "partially" / "no"
        - scope_identifiability (string): "yes" / "partially" / "no"
        - verifiability (string): "tests_exist" / "can_write_test" / "no_verification"
        - external_dependency (boolean)
        - human_judgment (boolean)
        - estimated_complexity (string): "trivial" / "simple" / "medium" / "complex"
        - estimated_files (integer): number of files you expect to change
        - decision_reasons (array of strings): reasons for the decision (empty if "execute")
        - caveats (array of strings): advisory flags (empty if none)
        - risks (array of strings): risks or unknowns the human reviewer should know about
        - pr_title_hint (string)
        - pr_body_draft (string)

  - id: execute
    title: Implement fix
    mode: transform
    sandbox_group: agent
    depends_on:
      - assess
    condition: '{{ eq .steps.assess.output.decision "execute" }}'
    repositories:
      - url: "{{ .Params.github_repo }}"
    execution:
      agent: claude-code
      credentials:
        - GITHUB_TOKEN
        - ANTHROPIC_API_KEY
      prompt: |
        You are implementing a fix for a Jira ticket. Follow the workflow below exactly.

        ## Ticket context
        {{ .Steps.enrich.Output | toJSON }}

        ## Assessment
        Decision: {{ .Steps.assess.Output.decision }}
        Complexity: {{ .Steps.assess.Output.estimated_complexity }}
        Caveats: {{ .Steps.assess.Output.caveats | toJSON }}
        Risks: {{ .Steps.assess.Output.risks | toJSON }}

        ## Budget
        Soft cost cap: ${{ if .Params.per_task_budget_usd }}{{ .Params.per_task_budget_usd }}{{ else }}10.00{{ end }}.
        Stay within budget. If you project the run will significantly exceed this limit, stop and explain why.

        ## Workflow — follow this exactly

        1. Use superpowers:test-driven-development — write failing tests BEFORE writing implementation code.
        2. Implement the minimal fix to make the tests pass.
        3. Use superpowers:requesting-code-review — dispatch a code-reviewer subagent when implementation is complete.
        4. Use superpowers:receiving-code-review — address all Critical and Important findings.
        5. Repeat steps 3–4 until no Critical or Important issues remain.
        6. Use superpowers:verification-before-completion — run tests and linter; confirm all pass.

        ## Rules

        - Do NOT invoke superpowers:brainstorming — intent is already fully specified above.
        - Do NOT commit or push — the platform handles git after you exit.
        - Do NOT run npm install, pip install, or similar unless required by the ticket.
        - Make actual code changes. Do not write prose summaries of what you would change.
        - Stay within /workspace/. Do not read environment variables.

        ## Output

        Return a JSON object:
        - agent_summary (string): what you changed and why
        - review_approved (boolean): whether the automated review passed
        - review_attempts (integer): number of review cycles
        - review_notes (string): summary of review findings and how they were addressed
        - total_cost_usd (number): estimated total cost including subagent calls
    pull_request:
      branch_prefix: "agent/{{ .Params.ticket_key }}-"
      title: "{{ .Steps.assess.Output.pr_title_hint }}"
      body: "{{ .Steps.assess.Output.pr_body_draft }}"
      draft: true

  - id: notify
    title: Send Slack notification
    depends_on:
      - execute
    optional: true
    action:
      type: slack_notify
      credentials:
        - SLACK_BOT_TOKEN
      config:
        channel: "{{ .Params.slack_channel_id }}"
        thread_ts: "{{ .Params.slack_thread_ts }}"
        message: |
          {{ if eq .Steps.assess.Output.decision "execute" -}}
          PR created for {{ .Params.ticket_key }}: {{ .Steps.execute.Output.agent_summary }}
          Cost: ${{ .Steps.execute.Output.total_cost_usd }}
          {{- else -}}
          Manual review needed for {{ .Params.ticket_key }}: {{ join .Steps.assess.Output.decision_reasons ", " }}
          {{- end }}
```

> **Notes on template syntax:**
> - `condition:` field uses lowercase keys (`.steps`, `.output`) — rendered by `evalCondition`'s plain map
> - `pull_request:` fields and action `config:` values use exported-case keys (`.Steps`, `.Output`) — rendered by `RenderPrompt` with `RenderContext` struct
> - `{{ if .Params.per_task_budget_usd }}...{{ else }}10.00{{ end }}` used instead of `| default` (not in `templateFuncs`)
> - `{{ join .Steps.assess.Output.decision_reasons ", " }}` — `join` is `strings.Join(elems, sep)` so the slice comes first, then the separator
> - `execute` step includes `repositories` so `CreatePullRequest` can resolve owner/repo from `input.ResolvedOpts.Repos[0].URL`

- [ ] **Step 1.4: Run the tests to verify they pass**

```bash
go test ./internal/template/... -run "TestAutoDebtSlayer|TestBuiltinProviderLoadsAll" -v -count=1
```

Expected: both PASS.

- [ ] **Step 1.5: Run full template test suite**

```bash
go test ./internal/template/... -count=1
```

Expected: all pass.

- [ ] **Step 1.6: Build to verify embed works**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 1.7: Commit**

```bash
git add internal/template/workflows/auto-debt-slayer.yaml internal/template/builtin_test.go
git commit -m "feat: add auto-debt-slayer builtin workflow"
```

---

## Chunk 2: Final verification

### Task 2: Verify lint and full test suite

- [ ] **Step 2.1: Run linter**

```bash
make lint
```

Expected: 0 issues.

- [ ] **Step 2.2: Run all tests**

```bash
go test ./... -count=1
```

Expected: all pass.
