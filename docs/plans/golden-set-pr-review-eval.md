# RFC: Golden Input Dataset for Claude PR Code Review Evaluation

**Status:** Draft  
**Author:** Ankit Borawake  
**Date:** 2026-04-20  
**Related:** [Claude Code Review Enablement](https://miro.atlassian.net/wiki/spaces/AI/pages/5011144812/Claude+Code+Review+Enablement), [RFC: Evals Framework](https://miro.atlassian.net/wiki/spaces/AI/pages/5502074902/RFC+Evals+Framework+for+AI+Workflows+and+Agentic+Systems)

---

## What Is a Golden Set and Why Do We Need One

A golden set is a fixed, labeled collection of inputs with known correct outputs. For Claude PR code review, it is a curated set of pull requests where we know in advance what issues exist, so we can check whether Claude finds them.

Without it, there is no repeatable way to answer: *did this prompt change make the review better or worse?* The golden set gives us a targeted, on-demand signal that complements broader adoption metrics with precise, per-category recall and noise measurements.

---

## What Informed This Design

**Academic research on code review** establishes that defect detection is the primary measurable output, and that a useful review comment is specific, actionable, and addresses a real technical issue. Research also consistently finds that around 75% of defects caught in code review do not affect visible functionality. They are evolvability defects (maintainability, error handling, edge cases) rather than outright bugs. This shaped our category balance.

**Miro's own [Code Review Practices](https://miro.atlassian.net/wiki/spaces/PT/pages/2899935233/Code+review+practices)** explicitly names *"number of defects uncovered during code review"* as the metric that *"100% reflects quality of code review."* It also establishes Miro's native severity system (must-fix task / optional / nit) which maps directly to Claude's output taxonomy (BLOCKING / SHOULD_FIX / CONSIDER).

**Existing open source benchmarks** (CR-Bench, SWR-Bench, CodeReviewBench) were reviewed but assessed as unsuitable as a primary source. The core problem is contamination: Claude has been trained on public GitHub data, so evaluating it on popular OSS repos likely tests memory rather than reasoning. Public benchmarks are also Python-only or OSS-only and do not capture Miro-specific patterns, architecture, or review standards.

---

## Ground Truth Source: Why Human Review Comments

The most legitimate signal for what a correct review looks like is **what senior engineers actually commented on in production PRs**. This is revealed preference, not synthetic bugs or academic datasets from different codebases.

Alternatives considered and rejected:

| Option | Why rejected |
|---|---|
| Synthetic bug injection | Injected bugs don't reflect how issues appear in our codebase. |
| Retrospective production bugs | Tracing post-merge incidents back to specific PR lines is high effort, low yield. |
| External benchmark datasets | OSS-only, wrong language stack, and contamination risk makes scores misleading. |

Human review comments are noisy but filterable. Where possible, we check for commits touching the same file after the comment timestamp as a signal that it was addressed, but this requires manual confirmation.

---

## Dataset Design

**Size:** 80-100 PRs  
**Repo split:** 50% `miroapp-dev/client` (TypeScript/React), 50% `miroapp-dev/server` (Java/Kotlin)  
**Lookback window:** 24-30 months, to ensure enough BLOCKING-severity entries given that security and logic bugs are rare in any given month

### Category Breakdown

| Category | Count | What it covers |
|---|---|---|
| Security — BLOCKING | 18-20 | XSS, injection, auth bypass, exposed secrets |
| Logic/correctness — BLOCKING | 18-20 | Null deref, race conditions, off-by-one, broken business logic |
| Performance — SHOULD_FIX | 14-16 | N+1 queries, memory leaks, O(n²) loops |
| Code quality — SHOULD_FIX | 10-12 | Missing error handling, dangerous patterns, edge cases |
| Clean PRs (no real issues) | 18-22 | Approved PRs with no substantive feedback, to measure false positive rate |

### What Gets Excluded

- **Experiment PRs:** Miro applies a more pragmatic review standard to experiments; including them would produce false positives.
- **Hotfix PRs:** Reviewed under relaxed emergency standards.
- **Draft and validation PRs:** Not production-quality submissions.
- **PRs over 600 LOC or 40 files:** We will intentionally include a small number of larger PRs (400-600 LOC) to test Claude's behavior on harder inputs where human reviewers are more likely to miss things.
- **Auto-generated files, migrations, lock files:** Not meaningful review targets.

---

## Collection and Labeling Approach

1. **Collect** merged PRs from both repos via GitHub API. Apply programmatic pre-filters to cut noise before any LLM calls.
2. **Fetch review comments** per candidate PR. Drop bot comments and short acknowledgements ("done", "LGTM", "+1").
3. **LLM classify** remaining comments: is this substantive? What category, severity, file, and line?
4. **Human spot-check** BLOCKING-classified entries only. Roughly 2 hours across both repos. Discard anything that doesn't clearly warrant blocking merge.
5. **Generate YAML golden set entries** with ground truth findings and LLM judge rubrics per issue.

---

## What This Enables

Once built, the golden set feeds into the fleetlift eval framework. Each time the review prompt, model version, or workflow config changes, we get:

- **BLOCKING recall:** did Claude catch the issues that must be caught?
- **Usefulness rate:** what fraction of Claude's comments were real issues, not noise?
- **Signal-to-noise ratio:** how much actionable signal does Claude produce relative to noise?
- **Clean PR pass rate:** does Claude incorrectly block PRs with no real issues?
