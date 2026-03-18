# Documentation Assessment Workflow — Design Spec

## Problem

Organizations with hundreds of git repositories accumulate stale, inaccurate, and incomplete documentation. This wastes developer time and — critically — misleads AI coding agents that rely on docs (README, CLAUDE.md, AGENTS.md) to navigate and work in repos efficiently. There is no systematic way to assess documentation quality fleet-wide or fix issues at scale.

## Goals

1. Assess documentation quality and AI-readiness across a fleet of repositories
2. Produce structured, comparable reports (per-repo scorecards + fleet-wide summary)
3. Optionally fix documentation issues and raise PRs — gated by a score threshold
4. Support delta assessment: given repos with recent changes, assess only whether those changes invalidated docs
5. Optimize for agent-readiness — docs should help agents find relevant code with minimal token spend

## Non-Goals

- Inline code comment assessment
- Executing setup instructions to verify they work
- Discovering repos automatically (caller provides the list)
- Enforcing a documentation template or standard across repos

## Workflow Definition

### Identity

```yaml
id: doc-assessment
title: Documentation Assessment
description: >
  Assess documentation quality and AI-readiness across repositories,
  optionally fixing issues via PRs.
tags: [documentation, fleet, report, transform]
```

### Parameters

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `repositories` | json | yes | — | JSON array of `{url, branch?}` objects |
| `mode` | string | no | `"report"` | `"report"` (assess only) or `"fix"` (assess + raise PRs where score below threshold) |
| `focus` | string | no | `"balanced"` | Scoring emphasis: `"balanced"`, `"accuracy"`, `"completeness"`, `"currency"`, or `"agent-readiness"` |
| `fix_threshold` | int | no | `3` | Overall weighted score (1-5) below which fix PRs are raised. Only used when `mode=fix` |
| `delta_context` | string | no | `""` | When set, narrows assessment to recent changes (e.g., "Only assess docs against changes from the last 7 days") |
| `max_parallel` | int | no | `20` | Maximum concurrent repo assessments |
| `draft_prs` | bool | no | `true` | Whether fix PRs are created as drafts |

### Steps

```
assess (fan-out) → collate
```

#### Step 1: `assess`

- **Mode:** `transform` (supports both report-only and fix+PR flows)
- **Fan-out:** Across all repositories, limited by `max_parallel`
- **Approval policy:** `never` (autonomous)
- **Execution:** Each sandbox:
  1. Clones the repository
  2. Discovers documentation files: README.md, CLAUDE.md, AGENTS.md, docs/**/*.md, API specs, architecture docs
  3. Understands repo structure (language, build system, directory layout)
  4. Cross-references documentation against reality:
     - Referenced commands exist (check package.json, Makefile, etc.)
     - Referenced directories and files exist
     - API endpoint descriptions match router/handler definitions
     - Module/package names match actual structure
  5. If `delta_context` is set: runs `git log` to identify recent changes, narrows assessment to docs affected by those changes
  6. Scores four dimensions (1-5 each):
     - **Accuracy** — do docs match the actual codebase?
     - **Completeness** — do docs cover key areas (setup, architecture, API, conventions)?
     - **Currency** — are docs up to date with recent code changes?
     - **Agent-readiness** — can an AI agent efficiently navigate and work in this repo using the docs?
  7. Computes weighted overall score based on `focus` parameter
  8. If `mode=fix` AND overall score < `fix_threshold`: fixes issues, commits to branch, creates PR
- **Pull request config** (used when fixes are applied):
  - Branch prefix: `docs/fleetlift-assessment-`
  - Title: `docs: fix documentation issues (Fleetlift assessment)`
  - Labels: `["documentation", "automated"]`
  - Draft: `{{ .Params.draft_prs }}` (default: true)
- **Output schema:** See Assessment Output Schema below
- **Artifacts:** Per-repo detailed report (markdown)

**Note:** The step uses `mode: transform` so that the `pull_request` config is available when fixes are applied. When `mode=report` or the score is above threshold, no branch is created and no PR is raised — the step simply produces the assessment report. The prompt instructs the agent to skip git operations when no fixes are needed.

#### Step 2: `collate`

- **Mode:** report
- **Depends on:** `assess`
- **Execution:** Receives all per-repo structured outputs, produces fleet-wide summary:
  - Score distribution (histogram across 1-5)
  - Ranked list of repos by overall score (worst first)
  - Most common issues across the fleet (e.g., "23 repos have no CLAUDE.md")
  - Breakdown by dimension (which dimension is weakest fleet-wide)
  - List of PRs raised (if fix mode)
- **Artifacts:** Fleet summary report (markdown)
- **Inbox:** Creates notification with summary

## Assessment Output Schema

Each repo's assess step outputs structured JSON:

```json
{
  "repo": "org/repo-name",
  "scores": {
    "accuracy": 3,
    "completeness": 2,
    "currency": 4,
    "agent_readiness": 1,
    "overall": 2.5
  },
  "focus": "balanced",
  "findings": [
    {
      "dimension": "accuracy",
      "severity": "high",
      "file": "README.md",
      "description": "Setup instructions reference 'make build' but no Makefile exists"
    },
    {
      "dimension": "agent_readiness",
      "severity": "high",
      "file": null,
      "description": "No CLAUDE.md or AGENTS.md — agent has no guidance for this repo"
    }
  ],
  "summary": "Documentation exists but is significantly outdated. README references removed modules. No agent guidance files.",
  "fix_applied": true,
  "pr_url": "https://github.com/org/repo-name/pull/42",
  "files_modified": ["README.md", "CLAUDE.md"]
}
```

- Scores are integers 1-5; overall is a weighted float
- Findings list individual issues with dimension, severity (high/medium/low), affected file, description
- `fix_applied`, `pr_url`, `files_modified` only populated when `mode=fix` and score was below threshold

## Scoring Model

### Dimension Definitions

| Score | Meaning |
|-------|---------|
| 1 | Actively harmful — misleading information that would cause wrong actions |
| 2 | Poor — major gaps or inaccuracies, docs are unreliable |
| 3 | Adequate — covers basics, some gaps or stale content |
| 4 | Good — accurate and fairly complete, minor improvements possible |
| 5 | Excellent — accurate, complete, well-structured for both humans and agents |

### Focus Weighting

| Focus | Accuracy | Completeness | Currency | Agent-readiness |
|-------|----------|-------------|----------|-----------------|
| `balanced` | 25% | 25% | 25% | 25% |
| `accuracy` | 40% | 20% | 20% | 20% |
| `completeness` | 20% | 40% | 20% | 20% |
| `currency` | 20% | 20% | 40% | 20% |
| `agent-readiness` | 20% | 20% | 20% | 40% |

### Fix Threshold

Overall weighted score below `fix_threshold` (default: 3) triggers PR creation in fix mode. Repos at or above the threshold receive a report but no PR.

## Prompt Design Principles

The assess step prompt will:

1. Be specific about what each score level means (see scoring table)
2. Instruct the agent to verify concrete claims: run `ls` for referenced paths, check build files for referenced commands, inspect router definitions for API claims
3. For delta mode: use `git log` to scope the assessment to recent changes only
4. For fixes: "fix what's wrong, don't rewrite what's adequate. Add CLAUDE.md only if none exists. Preserve the repo's existing documentation style."
5. Require structured JSON output matching the schema above

## Platform Changes

**None required.** This workflow uses existing Fleetlift capabilities:
- Fan-out across repositories
- Conditional steps
- Artifact collection
- Pull request creation
- Inbox notifications
- Structured output schemas

## Implementation Scope

1. New workflow YAML template: `internal/template/workflows/doc-assessment.yaml`
2. Register in builtin provider
3. Tests: workflow template rendering, output schema validation

## Operational Considerations

- **Cost:** At ~$0.50–2.00 per repo assessment (depending on repo size), a 200-repo baseline run costs ~$100–400. Delta runs assessing 20-30 repos weekly are significantly cheaper.
- **Parallelism:** Default `max_parallel=20` balances throughput against sandbox resource limits. Tunable per run.
- **Idempotency:** Running fix mode twice on the same repo is safe — if a fix branch already exists, the PR creation will fail for that repo but others proceed (existing Fleetlift fan-out failure isolation).

## Edge Cases

- **Repo has no documentation at all:** Agent scores all dimensions 1 or 2, findings note absence of each expected file. In fix mode, agent creates minimal docs (README, CLAUDE.md) based on code inspection.
- **All repos score above threshold in fix mode:** No PRs are raised. Collate step still produces the fleet summary report.
- **Delta context finds no recent changes:** Agent reports "no changes detected" with scores carried forward as N/A. Step completes successfully with no fixes.
- **Repo is empty or inaccessible:** Fan-out child fails for that repo; other repos continue. Error captured in collate summary.
