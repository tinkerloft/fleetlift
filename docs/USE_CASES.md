# FleetLift Use Cases

Real-world scenarios where FleetLift eliminates the toil of running AI coding agents across large repository fleets.

---

## 1. Fleet-Wide Security Audit with Executive Report

**The problem.**
Your security team needs a comprehensive audit across 120 microservices before a SOC 2 renewal. Manually running scanners, reading output, cross-referencing CVEs, and writing a summary for leadership takes a full sprint. By the time the report is done, new vulnerabilities have already landed on `main`.

**How FleetLift solves it.**
The `audit` template fans out across every repo in your fleet in parallel, with `max_parallel: 10` to avoid hammering your SCM. Each sandbox clones the repo, runs the agent with your audit prompt (dependency CVEs, secret scanning, OWASP Top 10 patterns), and produces structured findings. A final aggregation step collates results into a single report ranked by severity, grouped by team ownership.

The DAG looks like:

```
fan-out: audit each repo (parallel, max 10)
    |
    v
aggregate findings into fleet-wide report
```

Output: a structured JSON report and a human-readable markdown summary you can hand directly to leadership or paste into a compliance tool.

**Example command.**

```bash
fleetlift run start audit \
  --param repos=@fleet-repos.txt \
  --param prompt="Scan for known CVEs in dependencies, hardcoded secrets, SQL injection, and insecure deserialization. Rate each finding Critical/High/Medium/Low." \
  --param report_format=markdown
```

**Without FleetLift.**
You write a shell script that loops over repos, clones each one, runs `trivy` or `semgrep`, pipes output to files, then spends two days manually reading 120 output files and copy-pasting findings into a spreadsheet. Half the repos fail silently because of auth issues. The spreadsheet is outdated before you send it.

---

## 2. Bulk Dependency Upgrade Across 50+ Repos

**The problem.**
A critical OpenSSL patch drops. You have 53 Go services that pin the vulnerable version. Each repo needs the dependency bumped, tests run, and a PR opened. Your team estimates two days of copy-paste work, and someone will inevitably forget a repo or skip the test step.

**How FleetLift solves it.**
The `dependency-update` template fans out across your target repos. For each repo, the agent identifies outdated or vulnerable dependencies, updates them, runs the existing test suite inside the sandbox to verify nothing breaks, and opens a PR with a clear changelog. The HITL gate is set to `on_changes`, so you only review repos where something actually changed.

The DAG per repo:

```
identify outdated deps
    |
    v
update dependencies
    |
    v
run tests & verify
    |
    v
open PR (approval gate: on_changes)
```

A failure threshold (e.g., `fail_after: 5`) stops the run early if too many repos have broken builds, so you can investigate before burning through the whole fleet.

**Example command.**

```bash
fleetlift run start dependency-update \
  --param repos=@go-services.txt \
  --param target_dependency="golang.org/x/crypto" \
  --param target_version="v0.27.0" \
  --param run_tests=true
```

**Without FleetLift.**
You open 53 browser tabs (or write a Bash loop with `gh`), create a branch in each, run `go get`, hope tests pass, push, open a PR, then track which ones merged and which ones broke. Three weeks later, someone discovers repo #47 was never updated.

---

## 3. Large-Scale Code Migration (Logging Framework Swap)

**The problem.**
Your organization is migrating from `logrus` to `slog` across 80 Go services. It is not a find-and-replace job: call sites differ, structured fields need reshaping, and custom formatters need rewriting. Nobody wants to own this migration, and it keeps slipping quarter after quarter.

**How FleetLift solves it.**
The `migration` template runs a three-phase DAG. First, an impact analysis step scans every repo to classify the scope of changes needed (trivial, moderate, complex). Then a transform step fans out, giving the agent a detailed migration prompt and examples. Finally, a validation step runs tests and linters in the sandbox. PRs are gated behind `always` approval so a human reviews every change before merge.

The DAG:

```
impact analysis (fan-out, read-only)
    |
    v
transform: apply migration (fan-out, parallel)
    |
    v
validate: run tests + lint (fan-out, parallel)
    |
    v
open PRs (approval gate: always)
```

The knowledge loop captures patterns the agent discovers in early repos (e.g., a common custom middleware wrapper around logrus) and injects that context into later repos, improving consistency across the fleet.

**Example command.**

```bash
fleetlift run start migration \
  --param repos=@all-go-services.txt \
  --param prompt="Migrate all logrus usage to log/slog. Replace logrus.Fields with slog.Attr. Replace logrus.WithField/WithFields with slog.With. Replace custom TextFormatter with slog.NewTextHandler. Preserve all existing log levels and structured fields." \
  --param approval=always
```

**Without FleetLift.**
An engineer spends three months opening PRs across 80 repos. Each PR needs manual review because the changes are non-trivial. By the time repo #60 is migrated, repos #1 through #20 have drifted and introduced new `logrus` call sites. The migration Jira epic haunts your backlog for two quarters.

---

## 4. Automated PR Review with Human Escalation

**The problem.**
Your team merges 40 PRs a day. Senior engineers spend 30% of their time on code review, most of which is catching the same categories of issues: missing error handling, test gaps, style violations, and security anti-patterns. Reviewers are the bottleneck and they are burning out.

**How FleetLift solves it.**
The `pr-review` template takes a PR reference, clones the repo at the PR branch, and runs the agent with a review prompt that covers your team's standards. The agent posts structured feedback (blocking issues vs. suggestions) as a PR comment. When the agent flags a blocking issue, the HITL gate escalates to a human reviewer via the inbox. Low-severity PRs with no blocking findings get an approval comment automatically.

The DAG:

```
clone repo at PR branch
    |
    v
run AI review against team standards
    |
    v
post findings as PR comment
    |
    v
if blocking issues: escalate to human (HITL)
```

You can trigger this from CI by calling `fleetlift run start pr-review` in your GitHub Actions workflow on every `pull_request` event.

**Example command.**

```bash
fleetlift run start pr-review \
  --param repo=https://github.com/acme/payments-service \
  --param pr_number=482 \
  --param prompt="Review for error handling, test coverage, SQL injection, and adherence to our Go style guide. Flag blocking issues separately from suggestions."
```

**Without FleetLift.**
Senior engineers context-switch between feature work and review queues all day. Simple PRs sit for hours waiting for a human to confirm that yes, the three-line config change is fine. Complex PRs get rubber-stamped at 5pm on Friday because the reviewer is fatigued. Bugs that reviewers would have caught in a fresh state slip through.

---

## 5. Incident Response: Triage, Root Cause, Fix, Verify

**The problem.**
It is 2am. PagerDuty fires. The on-call engineer has to figure out which service is broken, why, write a fix, verify it does not break anything else, and get it deployed. Under pressure, this process is slow, error-prone, and leads to "fix the symptom, not the cause" patches that create repeat incidents.

**How FleetLift solves it.**
The `incident-response` template runs a four-step DAG. First, a triage step analyzes the alert payload, recent commits, and logs to identify the likely affected service and failure mode. Then a root cause analysis step digs into the code for the underlying bug. A fix step generates and applies the patch inside a sandbox, with a mandatory human approval gate. Finally, a verification step runs the full test suite and any integration checks to confirm the fix is safe.

The DAG:

```
triage: identify affected service + failure mode
    |
    v
root cause: analyze code for underlying bug
    |
    v
fix: generate + apply patch (approval gate: always)
    |
    v
verify: run tests + integration checks
```

Mid-execution steering lets the on-call engineer inject additional context at any step ("check the Redis connection pool, we changed the pool size yesterday") without restarting the workflow.

**Example command.**

```bash
fleetlift run start incident-response \
  --param repo=https://github.com/acme/checkout-service \
  --param description="HTTP 500 spike on /checkout endpoint starting 01:47 UTC. Error logs show 'connection refused' from downstream inventory-service." \
  --param recent_changes="PR #731 merged 6 hours ago, changed connection pool settings" \
  --param approval=always
```

**Without FleetLift.**
The on-call engineer SSHs into a jump box, tails logs, reads a git log, tries a hypothesis, reverts a commit, waits for deploy, discovers that was not the cause, tries another hypothesis. The incident takes 90 minutes instead of 20. The postmortem action item is "improve runbooks" which never happens.

---

## 6. Adding Test Coverage to Under-Tested Repos

**The problem.**
Your engineering org mandated 70% test coverage six months ago. Twelve services are still under 40%. Nobody volunteers to write tests for legacy code they did not author. The coverage gap represents real production risk, but writing tests for someone else's code is thankless work that always gets deprioritized.

**How FleetLift solves it.**
The `add-tests` template fans out across your under-tested repos. The agent analyzes each codebase, identifies untested functions and critical code paths, generates unit tests, and runs them inside the sandbox to make sure they pass. PRs are opened with a coverage diff showing the before and after. The HITL gate is set to `on_changes` so a human reviews the generated tests for correctness before merge.

The DAG per repo:

```
analyze codebase for untested code paths
    |
    v
generate unit tests
    |
    v
run tests in sandbox to verify they pass
    |
    v
open PR with coverage report (approval gate: on_changes)
```

**Example command.**

```bash
fleetlift run start add-tests \
  --param repos=@low-coverage-services.txt \
  --param prompt="Focus on untested business logic and error handling paths. Use table-driven tests. Do not generate tests for trivial getters or generated code." \
  --param min_coverage_target=70
```

**Without FleetLift.**
You create a Jira ticket titled "Improve test coverage" and assign it to each team. It sits at the bottom of every sprint for three months. Eventually a contractor writes shallow tests that assert `!= nil` on everything to game the coverage number. Actual production bugs in untested paths continue to slip through.

---

## 7. Cross-Repo Research ("Which Repos Use Deprecated API X?")

**The problem.**
You are deprecating an internal library's v2 API in favor of v3. Before you can remove it, you need to know exactly which repos import it, how they use it, and how hard the migration will be for each one. Running `grep` across 100 repos gives you import statements but no context about usage complexity or migration effort.

**How FleetLift solves it.**
The `fleet-research` template fans out across your entire fleet in read-only mode. Each agent clones a repo, searches for usage of the deprecated API, classifies the usage patterns (trivial wrapper, deep integration, custom extension), and produces a structured finding. A final aggregation step collates everything into a single report sorted by migration difficulty.

The DAG:

```
fan-out: research each repo (parallel, read-only)
    |
    v
aggregate: collate findings into summary report
```

Output: a structured report with columns for repo name, usage count, usage patterns, estimated migration difficulty (trivial/moderate/complex), and specific code locations. You can hand this directly to a tech lead to plan the deprecation timeline.

**Example command.**

```bash
fleetlift run start fleet-research \
  --param repos=@all-repos.txt \
  --param prompt="Find all usage of the internal/authz/v2 package. For each repo, list: import locations, which functions are called, whether it uses the custom middleware extension, and rate migration difficulty as trivial/moderate/complex." \
  --param report_format=markdown
```

**Without FleetLift.**
You write a script that clones 100 repos and runs `grep -r "internal/authz/v2"`. You get a wall of text showing import lines with no context. You then manually open each repo to understand the usage pattern. Two weeks later, you have a spreadsheet that is already outdated because three teams added new v2 usage in the meantime. The deprecation deadline slips by a quarter.
