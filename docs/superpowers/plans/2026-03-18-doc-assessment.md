# Documentation Assessment Workflow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `doc-assessment` builtin workflow YAML that assesses documentation quality and AI-readiness across a fleet of repos, with an optional fix+PR mode.

**Architecture:** Single YAML file in `internal/template/workflows/` auto-discovered by the builtin provider. Two steps: `assess` (fan-out per-repo) + `collate` (fleet summary). Agent-driven `gh pr create` for conditional PRs avoids unconditional platform-level PR creation on all fan-out children.

**Tech Stack:** YAML workflow template, Go template expressions, `gopkg.in/yaml.v3`, Temporal (via existing DAGWorkflow), Claude Code agent in sandbox, `gh` CLI for PR creation.

**Spec:** `docs/superpowers/specs/2026-03-18-doc-assessment-design.md`

---

## File Structure

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/template/workflows/doc-assessment.yaml` | The workflow definition |
| Modify | `internal/template/builtin_test.go` | Update expected workflow count (13→14), add `doc-assessment` to list |

No Go source changes required — the builtin provider auto-discovers all `.yaml` files in `workflows/`.

---

### Task 1: Verify test baseline

**Files:**
- Read: `internal/template/builtin_test.go`
- Read: `internal/template/builtin.go`

- [ ] **Step 1: Confirm current workflow count in test**

```bash
grep -n "Len\|doc-assessment\|fleet-research" internal/template/builtin_test.go
```

Expected output: line with `assert.Len(t, templates, 13)` and list of 13 workflow IDs.

- [ ] **Step 2: Run existing tests to confirm they pass**

```bash
go test ./internal/template/... -v -run TestBuiltin
```

Expected: PASS with 13 workflows found.

---

### Task 2: Write failing test for doc-assessment workflow

**Files:**
- Modify: `internal/template/builtin_test.go`

- [ ] **Step 1: Add doc-assessment to the expected list and bump count**

In `builtin_test.go`, find the `assert.Len(t, templates, 13)` line and the slice of expected IDs. Change `13` to `14` and add `"doc-assessment"` to the expected IDs slice.

The test structure looks like:
```go
assert.Len(t, templates, 14)
ids := make([]string, len(templates))
for i, t := range templates {
    ids[i] = t.Slug
}
assert.Contains(t, ids, "doc-assessment")
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./internal/template/... -v -run TestBuiltin
```

Expected: FAIL — "expected 14 but got 13" or similar (doc-assessment.yaml does not exist yet).

---

### Task 3: Create the workflow YAML — parameters and structure

**Files:**
- Create: `internal/template/workflows/doc-assessment.yaml`

- [ ] **Step 1: Create the file with metadata and parameters**

```yaml
version: 1
id: doc-assessment
title: Documentation Assessment
description: >
  Assess documentation quality and AI-readiness across repositories,
  optionally fixing issues and raising PRs where quality falls below threshold.
tags:
  - documentation
  - fleet
  - report
  - transform

parameters:
  - name: repos
    type: json
    required: true
    description: "JSON array of {url, branch?} objects to assess"

  - name: mode
    type: string
    required: false
    default: report
    description: "'report' to assess only, or 'fix' to also raise PRs for repos below the score threshold"

  - name: focus
    type: string
    required: false
    default: balanced
    description: "Scoring emphasis: 'balanced', 'accuracy', 'completeness', 'currency', or 'agent-readiness'"

  - name: fix_threshold
    type: int
    required: false
    default: 3
    description: "Overall weighted score (1-5) below which fix PRs are raised. Only used when mode=fix"

  - name: delta_context
    type: string
    required: false
    default: ""
    description: "When set, narrows assessment to recent changes (e.g. 'Only assess docs against changes from the last 7 days')"

  - name: max_parallel
    type: int
    required: false
    default: 20
    description: "Maximum concurrent repo assessments"

  - name: draft_prs
    type: boolean
    required: false
    default: true
    description: "When true, fix PRs are created as drafts"
```

- [ ] **Step 2: Verify YAML parses cleanly**

```bash
go run -e 'package main; import ("fmt"; "os"; "gopkg.in/yaml.v3"); func main() { b,_:=os.ReadFile("internal/template/workflows/doc-assessment.yaml"); var v any; fmt.Println(yaml.Unmarshal(b,&v)) }' 2>/dev/null || go test ./internal/template/... -v -run TestBuiltin 2>&1 | head -30
```

A simpler check — just run the template test (it will fail on count but not on YAML parse error):

```bash
go test ./internal/template/... -v -run TestBuiltin 2>&1 | grep -E "FAIL|PASS|parse|yaml|error"
```

Expected: fail on count (14 vs 14 if YAML parsed) or YAML parse error if malformed.

---

### Task 4: Write the `assess` step

**Files:**
- Modify: `internal/template/workflows/doc-assessment.yaml`

- [ ] **Step 1: Add the assess step after the parameters section**

Append to `doc-assessment.yaml`:

```yaml
steps:
  - id: assess
    title: Assess repository documentation
    mode: report
    repositories: "{{ .Params.repos }}"
    max_parallel: 20
    execution:
      agent: claude-code
      credentials:
        - GITHUB_TOKEN
      output:
        schema:
          repo: "string"
          scores: "object"
          focus: "string"
          findings: "array"
          summary: "string"
          fix_applied: "boolean"
          pr_url: "string"
          files_modified: "array"
      prompt: |
        You are a documentation quality assessor. Your job is to evaluate the documentation
        in this repository for accuracy, completeness, currency, and agent-readiness.

        ## Focus

        Scoring focus: {{ .Params.focus }}

        Focus weighting (used to compute the overall score):
        - balanced:        accuracy 25%, completeness 25%, currency 25%, agent-readiness 25%
        - accuracy:        accuracy 40%, completeness 20%, currency 20%, agent-readiness 20%
        - completeness:    accuracy 20%, completeness 40%, currency 20%, agent-readiness 20%
        - currency:        accuracy 20%, completeness 20%, currency 40%, agent-readiness 20%
        - agent-readiness: accuracy 20%, completeness 20%, currency 20%, agent-readiness 40%

        ## Delta context

        {{ if .Params.delta_context }}
        IMPORTANT: This is a delta assessment. {{ .Params.delta_context }}

        Use `git log --oneline -50` to identify recent commits. Focus your assessment
        only on documentation that may have been affected by those changes. Set scores
        to null for dimensions not relevant to the changes. If no relevant changes are
        found, output: { "repo": "<name>", "summary": "No relevant changes found", "scores": {} }
        {{ else }}
        Assess all documentation in the repository.
        {{ end }}

        ## Instructions

        ### Step 1 — Understand the repository

        Run `ls -la`, `find . -name "*.md" -not -path "*/node_modules/*" -not -path "*/.git/*"`,
        and examine the directory structure. Understand:
        - What does this project do?
        - What language/build system does it use?
        - What documentation files exist?

        ### Step 2 — Assess documentation quality

        Examine these documentation files (if they exist):
        - README.md
        - CLAUDE.md / AGENTS.md (agent guidance files)
        - docs/**/*.md
        - Any API spec files (openapi.yaml, swagger.json, etc.)
        - Any architecture docs

        For each claim in the documentation, **verify it against the code**:
        - Referenced commands: check package.json scripts, Makefile targets, shell scripts
        - Referenced directories/files: run `ls` to confirm they exist
        - Referenced modules/packages: verify they exist in the actual codebase
        - API endpoint descriptions: cross-check against router/handler definitions
        - Configuration keys: verify against actual config loading code

        ### Step 3 — Score each dimension (1-5)

        Score definitions:
        - 1 = Actively harmful — misleading information that would cause wrong actions
        - 2 = Poor — major gaps or inaccuracies, docs are unreliable
        - 3 = Adequate — covers basics but has some gaps or stale content
        - 4 = Good — accurate and fairly complete, minor improvements possible
        - 5 = Excellent — accurate, complete, well-structured for both humans and agents

        Score these four dimensions:
        - **accuracy**: Do the docs match the actual codebase? Are referenced commands, paths, and APIs real?
        - **completeness**: Do the docs cover setup, architecture, API surface, and key conventions?
        - **currency**: Are docs up to date with the current codebase? No references to removed things?
        - **agent_readiness**: Can an AI agent navigate and work in this repo using only the docs? Is there a CLAUDE.md or AGENTS.md? Are file layout, conventions, and entry points described?

        Compute the overall weighted score based on the focus parameter above.

        ### Step 4 — If mode is 'fix' and overall score < {{ .Params.fix_threshold }}

        Mode: {{ .Params.mode }}
        Fix threshold: {{ .Params.fix_threshold }}

        If mode == "fix" AND overall_score < fix_threshold:

        1. Fix the documentation issues you found. Rules:
           - Fix what's wrong; don't rewrite what's adequate
           - Preserve the repo's existing documentation style and voice
           - Add CLAUDE.md only if none exists AND agent-readiness score is 2 or below
           - Do not add verbose boilerplate; be concise and accurate
           - Only modify .md files and documentation files — never source code

        2. Create a branch and commit:
           ```
           git checkout -b docs/fleetlift-assessment-$(git rev-parse --short HEAD)
           git add -A
           git commit -m "docs: fix documentation issues (Fleetlift assessment)"
           ```

        3. Create a PR using gh CLI:
           ```
           gh pr create \
             --title "docs: fix documentation issues (Fleetlift assessment)" \
             --body "Automated documentation fixes by Fleetlift doc-assessment workflow.

           **Overall score: <score>/5**

           Issues addressed:
           <bullet list of key findings>

           Files modified: <list>" \
             --label "documentation" \
             --label "automated" \
           {{ if .Params.draft_prs }}  --draft {{ end }}
           ```

        4. Record the PR URL from the output of `gh pr create`.

        ### Step 5 — Save per-repo report

        Write a detailed markdown report to /workspace/repo-report.md covering:
        - Repo name and URL
        - Scores table (all four dimensions + overall)
        - Findings list (each with dimension, severity, file, description)
        - Summary paragraph
        - PR URL (if created)

        Then save it using:
        mcp__fleetlift__artifact__create with name="repo-report" and content_type="text/markdown"

        ### Step 6 — Output structured JSON

        Output a JSON object with this exact schema:
        ```json
        {
          "repo": "<owner/repo-name>",
          "scores": {
            "accuracy": <1-5 integer>,
            "completeness": <1-5 integer>,
            "currency": <1-5 integer>,
            "agent_readiness": <1-5 integer>,
            "overall": <weighted float>
          },
          "focus": "{{ .Params.focus }}",
          "findings": [
            {
              "dimension": "<accuracy|completeness|currency|agent_readiness>",
              "severity": "<high|medium|low>",
              "file": "<filename or null>",
              "description": "<what is wrong>"
            }
          ],
          "summary": "<1-2 sentence prose summary>",
          "fix_applied": <true|false>,
          "pr_url": "<url or empty string>",
          "files_modified": ["<list of files changed, or empty array>"]
        }
        ```
```

- [ ] **Step 2: Verify YAML is still valid**

```bash
go test ./internal/template/... -v -run TestBuiltin 2>&1 | grep -E "FAIL|PASS|parse|yaml|error|14"
```

---

### Task 5: Write the `collate` step

**Files:**
- Modify: `internal/template/workflows/doc-assessment.yaml`

- [ ] **Step 1: Append collate step**

```yaml
  - id: collate
    title: Collate fleet documentation summary
    depends_on:
      - assess
    mode: report
    execution:
      agent: claude-code
      prompt: |
        You are collating per-repository documentation assessment results into a
        fleet-wide summary report.

        Per-repository results:
        {{ .Steps.assess.Outputs | toJSON }}

        ## Instructions

        Produce a fleet-wide summary report in markdown covering:

        1. **Score Distribution** — How many repos scored at each level (1-5) overall.
           Include a simple text histogram.

        2. **Repos Needing Attention** — Ranked list of the 10 worst-scoring repos
           (by overall score), with their scores and one-line summary of key issues.

        3. **Common Issues** — The most frequently occurring issues across the fleet.
           For example: "23 repos have no CLAUDE.md", "17 repos reference commands that don't exist".
           Group by dimension (accuracy, completeness, currency, agent-readiness).

        4. **Dimension Breakdown** — Fleet-wide average score per dimension. Which
           dimension is weakest across the fleet?

        5. **PRs Raised** (if any repos have pr_url set) — List of all PRs created,
           with repo name and PR URL.

        6. **Fleet Health** — One paragraph executive summary of documentation health
           across the fleet and the highest-priority action to take.

        After writing the report, save it using:
        mcp__fleetlift__artifact__create with name="fleet-summary" and content_type="text/markdown"

        Then notify the team using:
        mcp__fleetlift__inbox__notify with urgency="low" and a brief message summarising
        the fleet health score and how many repos need attention.
```

---

### Task 6: Run the test and verify it passes

- [ ] **Step 1: Run the template tests**

```bash
go test ./internal/template/... -v -run TestBuiltin
```

Expected: PASS. The test finds 14 builtin workflows including `doc-assessment`.

- [ ] **Step 2: Run full test suite to check for regressions**

```bash
go test ./... 2>&1 | tail -30
```

Expected: all tests pass (or pre-existing failures only — none introduced by this change).

- [ ] **Step 3: Build verification**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Lint**

```bash
make lint
```

Expected: no new lint errors.

---

### Task 7: Commit

- [ ] **Step 1: Commit the workflow and test update**

```bash
git add internal/template/workflows/doc-assessment.yaml internal/template/builtin_test.go
git commit -m "feat: add doc-assessment builtin workflow

Assesses documentation quality and AI-readiness across a fleet of
repositories. Two steps: assess (fan-out per-repo) + collate (fleet
summary). Supports report mode and fix mode (raises draft PRs via gh
CLI for repos below a configurable score threshold).

Parameters: repos, mode, focus, fix_threshold, delta_context,
max_parallel, draft_prs."
```

---

## Verification Checklist

After implementation, verify:

- [ ] `go test ./internal/template/... -v -run TestBuiltin` passes with 14 workflows
- [ ] `go build ./...` succeeds
- [ ] `make lint` passes
- [ ] `doc-assessment` appears in the workflow list when the server is running
- [ ] YAML parses without error (template test covers this)
