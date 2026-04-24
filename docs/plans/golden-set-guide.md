# Golden Set Guide

## What it is

A fixed, labeled collection of 80 pull requests from `miroapp-dev/client` and `miroapp-dev/server` where the correct review output is known in advance. Used to measure whether Claude catches real issues and avoids false alarms as the prompt, model, or workflow config changes.

## Composition

| Category | PRs | What it tests |
|---|---|---|
| `logic` | 20 | Logic and correctness bugs visible in the diff |
| `clean` | 22 | PRs with no real issues, false positive baseline |
| `performance` | 16 | N+1 queries, memory leaks, inefficient patterns |
| `code-quality` | 12 | Missing error handling, dangerous patterns |
| `security` | 10 | XSS, injection, auth misuse, data leaks |

**Repo split:** 44 client (TypeScript/React), 36 server (Java/Kotlin)

**Dev/holdout split:** 58 `dev` (iterate freely against these), 22 `holdout` (only use for final verification after iteration converges)

## Entry structure

Each PR entry contains:

```yaml
prNumber: 54149
repo: server
repoSlug: miroapp-dev/server
title: "..."
mergedAt: "2026-04-13T12:40:09Z"
split: holdout
category: security
totalAdditions: 351
totalDeletions: 16
findings:
  - file: path/to/file.java
    line: 149
    issue: Raw search string interpolated directly into URL without encoding
    severity: BLOCKING
    commentId: 3062999543
```

`commentId` links back to the original human review comment on GitHub. `clean` PRs have an empty `findings` list.

## Findings breakdown

105 total findings across the 58 non-clean PRs: 7 `BLOCKING`, 98 `SHOULD_FIX`. The low BLOCKING count reflects a strict human spot-check that dropped ~34% of originally BLOCKING-classified entries, only issues identifiable from the diff alone were kept. See `spot-check-findings.md` for the full reasoning.

## How an eval run uses this

1. For each PR entry, fetch the actual diff from GitHub using `prNumber` and `repoSlug`
2. Run Claude's review workflow against that diff
3. For non-clean PRs: check whether Claude produced a finding that matches the known issue at the right file/line, at the right severity
4. For clean PRs: check whether Claude incorrectly raises alarms on a PR humans approved without issue

Results are broken down by category and split to isolate where the model is strong or weak.

## What the eval measures

| Signal | Question |
|---|---|
| BLOCKING recall | Did Claude catch the issues that must be caught? |
| Usefulness rate | What fraction of Claude's comments were real issues, not noise? |
| Clean PR pass rate | Does Claude incorrectly block PRs with no real issues? |
| Severity calibration | When Claude finds a real issue, does it assign the right severity? |

## Related docs

- `spot-check-findings.md`: full record of entries dropped during human spot-check, with reasoning
- `golden-set-rfc.md`: methodology: why this design, what was considered, how it was built
