# Golden Set Spot-Check Findings

This document records entries removed during human spot-check of BLOCKING-classified findings, with reasoning. Used to inform the methodology doc and any future iterations of the collection pipeline.

---

## Removal Criteria

An entry is removed from BLOCKING if Claude could not plausibly identify the issue from the PR diff alone. Specifically:

- **Cross-team context**: Issue only visible if you know another team owns the code or depends on it
- **Prior PR context**: Issue only visible if you know what a previous PR fixed
- **Runtime/UI observation**: Issue only discoverable by running the app or E2E tests, not reading code
- **Process noise**: Merge conflicts, version bumps, reminder comments, not code defects

---

## Batch 1 (entries 1-30)

| # | PR | Repo | Issue summary | Reason dropped |
|---|---|---|---|---|
| 1 | #2926 | client | Rebase conflict | Process noise, not a code defect Claude can catch |
| 2 | #7347 | client | Temporary package.json change to revert | Conditional TODO, not a defect |
| 3 | #17234 | client | Removing analytics events another team owns | Cross-team context, not visible in diff |
| 4 | #17234 | client | Removing code another team owns | Cross-team context, not visible in diff |
| 5 | #17234 | client | Removing code another team wants to retain | Cross-team context, not visible in diff |
| 7 | #20548 | client | Console.log in production | Downgraded to SHOULD_FIX, not merge-blocking |
| 8 | #20829 | client | Debug line left in | Downgraded to SHOULD_FIX, not merge-blocking |
| 9 | #23181 | client | Hackathon crutches in production | Vague, subjective, requires knowing intent |
| 10 | #58349 | client | Config env file modified, needs revert | Process noise |
| 15 | #59393 | client | Package version mismatch | CI/process issue, not a code defect |
| 16 | #59792 | client | Testing conditions left in code | Vague comment, not clearly a defect Claude can identify |
| 20 | #53667 | server | Version number not updated | Process reminder, not a code defect |

---

## Batch 2 (entries 31-60)

| # | PR | Repo | Issue summary | Reason dropped |
|---|---|---|---|---|
| 32 | #517 | client | Feature flag removed without associated bug fix | Requires knowing content of another PR discussion |
| 33 | #593 | client | "Make sure this won't introduce bugs" | Vague, no specific issue visible in diff |
| 35 | #3282 | client | Breaks a fix from prior PR RTB-104954 | Requires knowing what a prior PR fixed |
| 40 | #4811 | client | Removed shared utils force teams to reimplement | Cross-team impact, not visible in diff |
| 42 | #5718 | client | E2E locator missing prerequisite step | Requires UI/runtime context |
| 43 | #6225 | client | Icon duplication in UI | Screenshot evidence, not visible in diff |
| 47 | #8213 | client | Duplicate events fired | Requires runtime observation |
| 48 | #8553 | client | feedbackEmoji locator broken | Requires E2E test run |
| 49 | #9033 | client | Removed CSS locator breaks E2E | Requires test run context |
| 50 | #9033 | client | Model version pinned ambiguously | Ambiguous question, not a confirmed defect |
| 52 | #12113 | client | UI regression from screenshot | Screenshot evidence, not visible in diff |
| 53 | #12113 | client | Placeholder/avatar color regression | Screenshot evidence, not visible in diff |
| 56 | #13545 | client | Mention breaks on edit | Requires runtime context |
| 59 | #16679 | client | Tooltip behavior broken | Verified in testing, not visible in diff |
| 60 | #16928 | client | Delayed author popup broken | Verified in testing, not visible in diff |

---

## Patterns Observed

1. **Runtime-only issues are common**: a significant portion of BLOCKING comments reference behavior observed by running the app or E2E tests, not readable from the diff. Claude cannot catch these.

2. **Cross-team ownership is invisible in diffs**: "please don't remove, we own this" comments cannot be reproduced by Claude without org-level metadata.

3. **E2E test failures**: several entries flagged broken E2E locators or test suite failures. Claude could potentially catch some of these (e.g. removed CSS class referenced in tests) but only if both files appear in the diff.

4. **Screenshot/visual regressions**: entirely uncatchable from diff alone. These should be a separate category in future iterations.

---

## What This Tells Us: LLM Review vs Human Review

Roughly **34% of what senior engineers flagged as BLOCKING** was dropped because the information required to catch the issue simply isn't present in a pull request diff. This isn't a model capability gap: it's a structural limitation of what code review as an input can encode.

### What LLM-powered review can catch

These are issues where the signal lives in the code itself:

- **Wrong types, missing fields, bad API contracts**: type mismatches, wrong enum values, missing path segments, incorrect annotations
- **Dangerous code patterns**: `dangerouslySetInnerHTML` without sanitization, raw string interpolation into URLs, wrong passport type for a flow
- **Logic errors visible in the diff**: falsy check on `0`, `.bind()` creating new function references, listeners never removed, wrong condition operator
- **Architectural boundary violations**: core packages depending on feature packages, wrong layer setting data that belongs in another layer
- **Compilation failures**: deleted constructors, removed super methods, incompatible nullability signatures

### What only a human with system context can catch

These require a mental model of the whole system that no diff can encode:

**1. Runtime and UI behavior**
Comments like "I tested this and ESC no longer exits focus mode" or "the icons are duplicated." The diff shows the code change but gives no signal that behavior is broken. Claude reviews code, not behavior.

**2. Cross-team ownership and org context**
"Please don't remove, our team owns this and it's still in use." The diff shows deletion of code that looks unused. Only a human who knows team structure, package ownership, and downstream consumers can catch this. Claude has no org graph.

**3. Deployment and operational sequencing**
"The backend must be released before this frontend PR can merge", "on first deploy this consumer will replay all messages." These are valid blocking concerns but they exist in the operational layer. No amount of reading the diff reveals deploy order or production state.

**4. Unconfirmed suspicions from experienced reviewers**
A large share of dropped comments were phrased as questions: "Aren't these fields still used by the client?", "Is that the intent?", "Are you sure this is safe?" this is where a senior engineer smells something wrong but hasn't confirmed it. These are legitimate human review signals but they aren't actionable ground truth for an eval. Claude producing the same hedge would be noise, not signal.

**5. Prior PR and historical context**
"This breaks the fix from PR #22960 for bug RTB-104954." A human reviewer remembers or looks up what a prior PR fixed. Claude only sees the current diff with no memory of prior changes.

### The practical implication

The golden set and therefore the eval measures only the first category. This is intentional: we want to know if Claude catches the issues it *should* be able to catch, not penalize it for lacking information it can never have.

The dropped entries are not a failure of the golden set process. They are documentation of where human reviewers add value that LLM review structurally cannot replace.

---

## Batch 3 (entries 61-90)

| # | PR | Repo | Issue summary | Reason dropped |
|---|---|---|---|---|
| 62 | #27845 | client | Broke draft saving functionality | Requires runtime context |
| 63 | #28256 | client | Sidekicks not available | Screenshot evidence, runtime |
| 64 | #28605 | client | Unvalidated account reference | Requires external Slack incident context |
| 65 | #29983 | client | Children array order not guaranteed | Subjective architectural concern, no concrete bug |
| 67 | #31209 | client | No validation on widget type | Design concern, not concrete bug visible in diff |
| 68 | #31209 | client | singleSelection undefined at runtime | Runtime observation via screenshot |
| 69 | #31209 | client | Nothing selectable in immersive mode | Runtime observation via screenshot |
| 70 | #31209 | client | Focus behavior changed | Requires runtime context |
| 71 | #31231 | client | Backend must release before frontend merge | Deployment ordering, not a code defect |
| 76 | #57874 | client | Input accepts letters, Enter adds line break | Runtime UI behavior |
| 79 | #57966 | client | Creating transactions on widget update discouraged | Design concern, requires domain knowledge |
| 80-88 | #58209 | client | Wrong screenshots (9 entries) | Screenshot comparison, all runtime/visual |
| 90 | #58219 | client | "Same here" reference comment | Duplicate, no standalone value |

---

## Batch 4 (entries 91-120)

| # | PR | Repo | Issue summary | Reason dropped |
|---|---|---|---|---|
| 91 | #58733 | client | Incorrect truth table for overflow logic | Screenshot evidence for expected values |
| 93 | #58745 | client | Business logic in general component | Architectural/subjective, not a concrete bug |
| 94 | #58779 | client | addStep misplaced inside forEach | Unconfirmed question ("I guess") |
| 95 | #58785 | client | Card insertion in wrong column | Runtime UI behavior |
| 98 | #58849 | client | getJiraInstanceIdByIssueUrl wrong for non-/browse/ URLs | Requires domain knowledge of URL structure |
| 100 | #58998 | client | Opening SVG triggers board mutations | Clarification question, no confirmed defect |
| 101 | #58998 | client | Race conditions in async auto-apply | Clarification question with screenshot |
| 102 | #58998 | client | Unclear if apply mutates board for all users | Clarification question |
| 103 | #58998 | client | Parallel write effect on board open | Question/concern, not confirmed bug |
| 105 | #59027 | client | Remote delete crashes context for other users | Complex runtime concurrency |
| 106 | #59087 | client | Code copied instead of reused | Design/maintenance concern, not a bug |
| 107 | #59087 | client | Confirm deletion of recently modified code is safe | Question, not confirmed defect |
| 109 | #59100 | client | && vs \|\| in access control logic | Unconfirmed question |
| 114 | #59298 | client | Apps visible but uninitialized when flags disabled | Runtime behavior |
| 116 | #59390 | client | 404 for product templates in CIS | Requires domain knowledge |
| 117 | #59490 | client | Export ordering incorrect | Requires runtime comparison |
| 118 | #59511 | client | Line causing old users to show as online | Past tense, already fixed |

---

## Batch 5 (entries 121-150)

| # | PR | Repo | Issue summary | Reason dropped |
|---|---|---|---|---|
| 121 | #60281 | client | `card` variant on wrong component | Question ("right?"), not confirmed |
| 123 | #60387 | client | Widget not detached from grid container | Question, not confirmed defect |
| 129 | #53250 | server | PictureResource vs IconResource contract impact | Question about impact, not confirmed |
| 130 | #53574 | server | Unsafe code path, no test coverage | "Are you sure this is safe?", unconfirmed |
| 131 | #53652 | server | existingJsonElement silently overridden | "no?" style question, not confirmed |
| 132 | #53666 | server | createSlideContainer behavior change | "Is that the intent?", unconfirmed |
| 135 | #53727 | server | Requires fix from another PR | Cross-PR context required |
| 138 | #53782 | server | Fallback moves all notifications on zero threads | "Either I'm missing something", unconfirmed |
| 141 | #53924 | server | Starter price variant logic multi-scenario | Multi-scenario question, not confirmed defect |
| 142 | #53966 | server | Confirm persistent fields erased on prod | Deployment/ops concern, not code defect |
| 143 | #53982 | server | Switcher defaults to allowing everything on deploy | Deployment sequencing concern |
| 144 | #53982 | server | Consumer replays all messages on first deploy | Deployment concern, not code defect |
| 147 | #54085 | server | Dead code path performs writes without EES events | "Can you double check", unconfirmed |
| 149 | #54118 | server | Removed JSON fields still used by client batch API | "Aren't those fields...?", unconfirmed question |

---

## Batch 6 (entries 151-160)

| # | PR | Repo | Issue summary | Reason dropped |
|---|---|---|---|---|
| 154 | #18652 | client | Sensitive env config present in file | Vague comment, Claude can't identify without seeing actual values |
| 157 | #59792 | client | "Need another security review pass" | Meta-comment, not a specific issue |
| 158 | #53926 | server | Customer data sent to analytics system | Policy question, not confirmed defect |

---

## Implication for Classifier Prompt

Only classify as BLOCKING if the issue is identifiable from the PR diff alone, without requiring runtime execution, prior PR knowledge, or cross-team ownership context.
