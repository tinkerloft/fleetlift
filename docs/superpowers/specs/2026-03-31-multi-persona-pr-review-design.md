# Multi-Persona PR Review Workflow

**Date:** 2026-03-31
**Status:** Approved
**Track:** New builtin workflow + platform action extension

---

## Background

The existing `pr-review` builtin workflow uses a single agent to review all aspects of a PR and post one comment. This produces generic, unfocused feedback. Multiple focused agents — each specialising in a single review dimension — produce sharper, higher-quality findings. An adversarial synthesis pass then grounds, deduplicates, and scores those findings before posting a structured summary and inline annotations to GitHub.

This design follows the GAN-inspired principle: independent specialist generators + a single adversarial critic produces better signal than one generalist agent.

---

## Goals

- Run N focused reviewer agents in parallel against a single PR diff
- Apply an adversarial synthesis pass: grounding check, deduplication, contradiction resolution, risk scoring
- Post a rich markdown summary comment + inline diff annotations to GitHub
- Work as a builtin workflow; teams fork to customise the reviewer roster
- Trigger via GitHub Actions calling the FleetLift API (no webhook infrastructure required)

## Non-Goals (v1)

- Fix proposal or automated fix application (deferred to v2)
- HITL escalation for high-severity findings (deferred to v2)
- `REQUEST_CHANGES` review event (COMMENT only for now; extension point noted)
- Shared workspace across parallel reviewer sandboxes (deferred; addressed when dedicated clone step lands)
- Webhook/GitHub App trigger (GHA → FleetLift API is sufficient for now)

---

## Architecture

```
[GitHub Action: on pull_request]
  → POST /api/runs { workflow: pr-review-multi, repo_url, pr_number }
         │
         ▼
    fetch_pr
    (clone PR branch, git diff, extract metadata)
    output: { diff, title, base_branch, changed_files[], additions, deletions }
         │
    ┌────┴──────────────────────────────────┐
    ▼           ▼              ▼            ▼
review_      review_       review_      review_
security   correctness   scalability    style
(parallel) (parallel)    (parallel)   (parallel)
    │           │              │            │
    └────┬──────┴──────────────┴────────────┘
         ▼
      synthesis
      (adversarial single pass)
      output: { executive_summary, file_risk_table[],
                findings[], focus_files[], inline_annotations[] }
         │
    ┌────┴──────────────┐
    ▼                   ▼
post_summary       post_inline
(github_pr_review) (github_pr_review_inline)
COMMENT event      per-finding positioned comments
```

---

## Step Design

### `fetch_pr`

Clones the PR branch and extracts the diff and metadata. Uses `sandbox_group: main` so the clone is reused if the platform later runs a sequential step in the same group. Designed as a **clean seam**: when the platform's dedicated clone/diff action lands, this step is a drop-in replacement with an identical output schema.

**Base branch resolution:** The agent first calls `gh pr view` to get `baseRefName`, then diffs against `origin/<baseRefName>` — never against a hardcoded `origin/main`. This correctly handles PRs targeting release branches, hotfix branches, or any non-main default branch.

**Output schema:**
```json
{
  "diff": "string",
  "title": "string",
  "base_branch": "string",
  "changed_files": ["string"],
  "additions": 0,
  "deletions": 0
}
```

### Reviewer Steps

Four named steps, all `depends_on: [fetch_pr]`, running in parallel. Each receives the full diff and changed files list. Each is instructed to review **only the changed lines** and to produce no findings if nothing is wrong — reducing hallucination pressure.

| Step ID | Persona | Focus Areas |
|---|---|---|
| `review_security` | Security Reviewer | Auth/authz flaws, injection vectors, secrets in code, unsafe crypto, missing input validation, insecure dependencies |
| `review_correctness` | Correctness Reviewer | Logic errors, off-by-one, nil dereferences, unhandled errors, data races, incorrect state assumptions |
| `review_scalability` | Scalability Reviewer | N+1 queries, unbounded allocations, blocking calls on hot paths, missing pagination, inefficient data structures |
| `review_style` | Conventions Reviewer | Missing tests, naming inconsistencies, undocumented API changes, breaking changes, dead code |

**Each reviewer output schema:**
```json
{
  "findings": [
    {
      "file": "string",
      "line": 0,
      "severity": "critical|high|medium|low",
      "dimension": "string",
      "description": "string"
    }
  ],
  "summary": "string"
}
```

Reviewer steps do not have a `repositories:` field — they work entirely from the diff text injected into the prompt. No repo clone is required. Each runs in its own lightweight sandbox. All four start as soon as `fetch_pr` completes.

### `synthesis`

The adversarial single pass. Receives all four reviewer outputs plus the original diff and changed files list. No repository clone needed — runs without `repositories:`.

**Jobs in order:**

1. **Grounding check** — discard any finding whose `file` is not in `changed_files`, or whose `line` does not appear in the diff hunk for that file. Ungrounded findings are silently dropped.
2. **Deduplication** — findings from different reviewers citing the same `file:line` are merged into one, keeping the higher severity and combining descriptions.
3. **Contradiction resolution** — if reviewers contradict each other on the same code, both perspectives are noted and severity is set to `needs-discussion`.
4. **Risk scoring** — each changed file gets a risk level (`critical/high/medium/low`) based on density and severity of grounded findings.
5. **Focus list** — changed files ranked by risk level descending.
6. **Inline annotation selection** — from grounded findings, select those specific enough for inline diff comments (clear file + line, actionable one-sentence description). Output file path, line number, and `side`: `"RIGHT"` for added or context lines (new file line number), `"LEFT"` for deleted lines (original file line number). The platform action posts these directly using GitHub's `Line`/`Side` API fields — no diff position translation required.

**Output schema:**
```json
{
  "executive_summary": "string (markdown)",
  "file_risk_table": [{ "file": "string", "risk_level": "string", "finding_count": 0 }],
  "findings": [{ "file": "string", "line": 0, "severity": "string", "dimension": "string", "description": "string" }],
  "focus_files": ["string"],
  "inline_annotations": [{ "file": "string", "line": 0, "side": "LEFT|RIGHT", "body": "string" }]
}
```

### `post_summary`

Action step. Uses the existing `github_pr_review` action to post the `executive_summary` as a top-level PR review `COMMENT` event.

### `post_inline`

Action step. Uses the new `github_pr_review_inline` action (see Platform Changes). Depends on `post_summary` to ensure the summary comment lands first. Receives the original diff for line-number → diff-position translation.

---

## Platform Changes Required

### 1. `github_pr_review_inline` action (Blocker)

New action in `internal/activity/actions.go`. Calls the GitHub `PullRequests.CreateReview` API with a `Comments` array rather than a plain body.

**Config fields:**
```
repo_url      string
pr_number     int
annotations   []{ file string, line int, side string, body string }
  — side is "RIGHT" (added/context lines, new-file line number)
     or "LEFT"  (deleted lines, original-file line number)
```

**Implementation notes:**

- Use GitHub's `Line` + `Side` fields in `DraftReviewComment` rather than the legacy `Position` field. This eliminates diff-position parsing entirely and correctly handles both added lines (`RIGHT`) and deleted lines (`LEFT`). The `go-github` v62 `DraftReviewComment` struct supports both.
- Post all annotations as a single `CreateReview` call with `Event: "COMMENT"` to avoid one notification email per comment.
- If GitHub rejects an individual annotation (e.g., the line is outside the diff window, or the side/line combination is invalid), record it in a `skipped` list — do not fail the entire review step.
- **Observability:** Return `{ "posted": N, "skipped": N, "skipped_details": [{ "file", "line", "reason" }] }` from the action. If any high-severity findings were skipped, include a visible warning in the posted summary comment: `> ⚠️ N inline annotation(s) could not be posted (see run detail for reasons).`
- Empty `annotations` list is a no-op, not an error.

### 2. `github_pr_review` action extension (Minor, future)

The existing action hardcodes `Event: "COMMENT"`. When `REQUEST_CHANGES` support is added later, add an optional `event` config field defaulting to `"COMMENT"`.

---

## Workflow YAML

```yaml
version: 1
id: pr-review-multi
title: Multi-Persona PR Review
description: >
  Parallel focused code review across security, correctness, scalability, and
  conventions dimensions. Findings are grounded against the diff, deduplicated,
  and posted as a summary comment + inline annotations.
tags:
  - review
  - automation
  - single-repo
parameters:
  - name: repo_url
    type: string
    required: true
    description: "Repository URL"
  - name: pr_number
    type: int
    required: true
    description: "Pull request number to review"
sandbox_groups:
  main:
    image: claude-code-sandbox:latest
steps:
  - id: fetch_pr
    title: Fetch PR diff
    sandbox_group: main
    repositories:
      - url: "{{ .Params.repo_url }}"
        ref: "refs/pull/{{ .Params.pr_number }}/head"
    execution:
      agent: claude-code
      credentials: [GITHUB_TOKEN]
      prompt: |
        You are in a repository with PR #{{ .Params.pr_number }} checked out at HEAD.

        First, fetch the PR metadata to get the actual base branch:
          gh pr view {{ .Params.pr_number }} --json title,baseRefName

        Then, using the baseRefName value from that output, run:
          git fetch origin <baseRefName>
          git diff origin/<baseRefName>...HEAD
          git diff --name-only origin/<baseRefName>...HEAD

        Do NOT hardcode "main" — use the baseRefName from the gh output.
        Output the full unified diff, the list of changed files, PR title,
        base branch name, and total additions/deletions.
      output:
        schema:
          diff: string
          title: string
          base_branch: string
          changed_files: array
          additions: int
          deletions: int

  - id: review_security
    title: Security review
    depends_on: [fetch_pr]
    execution:
      agent: claude-code
      prompt: |
        You are a security-focused code reviewer. Review ONLY the changed lines in the diff below.
        Do not comment on code that is not in the diff.

        PR: {{ .Steps.fetch_pr.Output.title }}
        Changed files: {{ .Steps.fetch_pr.Output.changed_files | toJSON }}

        Diff:
        {{ .Steps.fetch_pr.Output.diff }}

        Focus exclusively on: authentication and authorisation flaws, injection vectors
        (SQL, command, path traversal), secrets or credentials committed to code,
        use of deprecated or broken crypto primitives, missing input validation at
        trust boundaries, insecure dependencies or transitive vulnerabilities.

        For each finding: exact file path, line number in the file, severity
        (critical/high/medium/low), and a concise actionable description.
        If you find nothing, say so explicitly. Do not invent findings.
      output:
        schema:
          findings: array
          summary: string

  - id: review_correctness
    title: Correctness review
    depends_on: [fetch_pr]
    execution:
      agent: claude-code
      prompt: |
        You are a correctness-focused code reviewer. Review ONLY the changed lines in the diff below.

        PR: {{ .Steps.fetch_pr.Output.title }}
        Changed files: {{ .Steps.fetch_pr.Output.changed_files | toJSON }}

        Diff:
        {{ .Steps.fetch_pr.Output.diff }}

        Focus exclusively on: logic errors, off-by-one errors, nil/null pointer dereferences,
        unhandled or swallowed error paths, data races and concurrency bugs, incorrect
        assumptions about ordering or state, wrong algorithm for the stated purpose.

        For each finding: exact file path, line number, severity, and description.
        If you find nothing, say so. Do not invent findings.
      output:
        schema:
          findings: array
          summary: string

  - id: review_scalability
    title: Scalability & performance review
    depends_on: [fetch_pr]
    execution:
      agent: claude-code
      prompt: |
        You are a scalability and performance-focused code reviewer. Review ONLY the changed lines below.

        PR: {{ .Steps.fetch_pr.Output.title }}
        Changed files: {{ .Steps.fetch_pr.Output.changed_files | toJSON }}

        Diff:
        {{ .Steps.fetch_pr.Output.diff }}

        Focus exclusively on: N+1 query patterns, unbounded loops or memory allocations,
        synchronous blocking calls on hot paths, missing pagination on list operations,
        cache invalidation bugs, inefficient data structures for the shown access pattern,
        unnecessary serialisation or lock contention.

        For each finding: exact file path, line number, severity, and description.
        If you find nothing, say so. Do not invent findings.
      output:
        schema:
          findings: array
          summary: string

  - id: review_style
    title: Conventions & coverage review
    depends_on: [fetch_pr]
    execution:
      agent: claude-code
      prompt: |
        You are a code conventions and test coverage reviewer. Review ONLY the changed lines below.

        PR: {{ .Steps.fetch_pr.Output.title }}
        Changed files: {{ .Steps.fetch_pr.Output.changed_files | toJSON }}

        Diff:
        {{ .Steps.fetch_pr.Output.diff }}

        Focus exclusively on: missing or inadequate tests for changed behaviour,
        naming inconsistencies with the surrounding codebase visible in the diff,
        undocumented public API or interface changes, breaking changes without
        version bumps, dead code introduced by the change.

        For each finding: exact file path, line number, severity, and description.
        If you find nothing, say so. Do not invent findings.
      output:
        schema:
          findings: array
          summary: string

  - id: synthesis
    title: Synthesise and score findings
    depends_on: [review_security, review_correctness, review_scalability, review_style]
    execution:
      agent: claude-code
      prompt: |
        You are an adversarial code review synthesiser. Your job is quality control over
        other reviewers' output — not additional review.

        Original diff:
        {{ .Steps.fetch_pr.Output.diff }}

        Changed files (ground truth — findings outside this list are invalid):
        {{ .Steps.fetch_pr.Output.changed_files | toJSON }}

        Reviewer outputs:
        Security:     {{ .Steps.review_security.Output | toJSON }}
        Correctness:  {{ .Steps.review_correctness.Output | toJSON }}
        Scalability:  {{ .Steps.review_scalability.Output | toJSON }}
        Style:        {{ .Steps.review_style.Output | toJSON }}

        Steps:
        1. GROUNDING CHECK: Discard any finding whose file is not in the changed files list,
           or whose line number does not appear in the diff hunk for that file.
           Do not include ungrounded findings in your output under any circumstances.
        2. DEDUPLICATION: If multiple reviewers flag the same issue at the same file and line,
           merge into one finding. Keep the highest severity. Combine descriptions concisely.
        3. CONTRADICTION: If reviewers disagree about the same code, include both perspectives
           and set severity to "needs-discussion".
        4. RISK SCORING: For each changed file, assign a risk level (critical/high/medium/low)
           based on the density and severity of grounded findings against it.
        5. FOCUS LIST: Rank changed files by risk level, highest first.
        6. INLINE ANNOTATIONS: From grounded findings, select those specific enough for an
           inline diff comment — must have a clear file path, line number, and actionable
           one-sentence description. For each annotation output:
           - file: exact file path
           - line: the line number (new-file line for added/context code; original-file line for deleted code)
           - side: "RIGHT" if the finding is on an added or context line; "LEFT" if on a deleted line
           - body: one concise sentence

        Write the executive_summary in markdown for a PR comment audience. Be direct and
        specific. Do not pad with pleasantries. Do not repeat all findings verbatim —
        synthesise into a coherent assessment.

        Save the review report using mcp__fleetlift__artifact__create with
        name="pr-review-multi" and content_type="text/markdown".
      output:
        schema:
          executive_summary: string
          file_risk_table: array
          findings: array
          focus_files: array
          inline_annotations: array

  - id: post_summary
    title: Post review summary
    depends_on: [synthesis]
    action:
      type: github_pr_review
      credentials: [GITHUB_TOKEN]
      config:
        repo_url: "{{ .Params.repo_url }}"
        pr_number: "{{ .Params.pr_number }}"
        summary: "{{ .Steps.synthesis.Output.executive_summary }}"

  - id: post_inline
    title: Post inline annotations
    depends_on: [post_summary]
    action:
      type: github_pr_review_inline
      credentials: [GITHUB_TOKEN]
      config:
        repo_url: "{{ .Params.repo_url }}"
        pr_number: "{{ .Params.pr_number }}"
        annotations: "{{ .Steps.synthesis.Output.inline_annotations | toJSON }}"
```

---

## GitHub Actions Trigger

Minimal GHA workflow to dispatch a review run on PR open/update:

```yaml
name: FleetLift PR Review
on:
  pull_request:
    types: [opened, synchronize, reopened]

jobs:
  dispatch:
    runs-on: ubuntu-latest
    steps:
      - name: Dispatch multi-persona review
        run: |
          curl -X POST "${{ secrets.FLEETLIFT_API_URL }}/api/runs" \
            -H "Authorization: Bearer ${{ secrets.FLEETLIFT_TOKEN }}" \
            -H "Content-Type: application/json" \
            -d '{
              "workflow_id": "pr-review-multi",
              "params": {
                "repo_url": "${{ github.event.repository.clone_url }}",
                "pr_number": ${{ github.event.pull_request.number }}
              }
            }'
```

---

## Platform Gaps Summary

| Gap | Severity | Notes |
|---|---|---|
| `github_pr_review_inline` action | **Blocker** | New action in `internal/activity/actions.go`; uses GitHub's `Line`+`Side` fields (`DraftReviewComment`) — no diff-position parsing required |
| Skipped-annotation observability | **Part of blocker** | Action must return `{ posted, skipped, skipped_details }` and append a visible warning to the summary comment when high-severity annotations are skipped |
| Large diff scanner buffer | Low | `bufio.Scanner` in agent output must use 4MB explicit buffer (already called out in CLAUDE.md) |
| `fetch_pr` agent step is heavyweight | Low/Future | Full Claude sandbox just to run `git diff`; replaced when dedicated platform clone/diff action lands (same output schema, drop-in) |
| Shared read-only workspace for parallel agents | Future | Not needed for v1 (reviewers work from diff text, no clone). Relevant if reviewers are later upgraded to full codebase access. |
| `REQUEST_CHANGES` event support | Future | Add optional `event` field to `github_pr_review` action config, defaulting to `"COMMENT"` |

---

## v2 Extensions (Out of Scope)

- **Fix proposals + HITL**: After synthesis, a `propose_fixes` step drafts concrete code changes; `inbox.request_input` pauses workflow for user to select which fixes to apply; a `apply_fixes` step or separate Fix workflow applies selected fixes to a new branch.
- **`REQUEST_CHANGES` mode**: High-severity findings trigger a blocking review event; requires operator opt-in per workflow or per run.
- **Webhook/GitHub App trigger**: Replace GHA dispatch with a native FleetLift GitHub App that handles `pull_request` events directly.
- **Reviewer customisation via Prompt Library**: Team-specific prompt snippets injected into reviewer steps without forking (Track P).
