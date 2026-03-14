# FleetLift OSS Positioning Plan

**Goal:** Make FleetLift a credible open-source alternative that platform teams evaluate alongside SaaS products like Cursor Automations, Codegen, Sweep, etc.

**Date:** 2026-03-14

---

## Current State

FleetLift has excellent *internal* documentation (architecture, CLI reference, workflow schema, troubleshooting) but reads like an internal tool, not a product. A platform engineer landing on the repo today would see a wall of env vars and `docker compose up` — no story about *why* this exists or *who* it's for.

### What's Strong
- Architecture docs: comprehensive, clear diagrams
- CLI and workflow reference: complete
- 15 example YAML workflows
- Troubleshooting guide
- CLAUDE.md developer guide

### What's Missing
- No compelling narrative ("why FleetLift?")
- README is transactional, not persuasive
- No use-case documentation
- No comparison with alternatives
- No getting-started tutorial
- No landing page or feature showcase
- No CONTRIBUTING.md
- No production deployment guide
- web/README.md is Vite boilerplate

---

## Plan

### Phase 1: README Rewrite (High Impact, Low Effort)

**File:** `README.md`

The README is the storefront. Rewrite it with this structure:

1. **One-liner + tagline** — what it is in plain English
   - Current: "Multi-tenant agentic workflow platform"
   - Target: "Open-source platform for running AI coding agents across your entire fleet of repositories — with DAG orchestration, human approval gates, and a knowledge loop that gets smarter over time."

2. **Hero image/GIF** — show the DAG view, log streaming, HITL approval in action
   - Use existing `docs/images/demo.gif` or create a fresh one
   - Update `docs/demo.tape` to capture current UI state

3. **"Why FleetLift?"** section — 3-4 short paragraphs answering:
   - What problem does this solve? (Running AI agents at fleet scale with oversight)
   - Why not just use Cursor Automations / GitHub Actions / a bash script? (DAG orchestration, multi-repo fan-out, HITL, knowledge loop, self-hosted)
   - Who is this for? (Platform teams, DevOps leads, security teams)

4. **Feature highlights** — visual cards or bold sections for:
   - DAG workflows (YAML → parallel execution → conditional logic)
   - Fleet-scale fan-out (one template, N repos, aggregated results)
   - Human-in-the-loop (approve/reject/steer mid-execution)
   - Knowledge loop (agents learn from past runs)
   - Secure by default (sandboxed containers, encrypted credentials, multi-tenant)
   - 10 built-in templates (link to examples/)

5. **Quick comparison table** — FleetLift vs Cursor Automations vs GitHub Actions vs raw Temporal
   - Columns: DAG support, multi-repo, HITL, knowledge loop, self-hosted, event triggers
   - Keep it factual, not snarky

6. **30-second quick start** — keep the existing docker compose flow but tighten it
   - Show the *outcome* first ("In 30 seconds you'll have an AI agent auditing a repo")
   - Then the commands

7. **Architecture diagram** — inline the ASCII diagram from ARCHITECTURE.md

8. **Documentation links** — keep current structure

9. **Contributing + License** — link to CONTRIBUTING.md, show MIT badge

### Phase 2: Use Cases Document

**File:** `docs/USE_CASES.md`

Write 5-7 concrete scenarios, each with:
- **Scenario title** (e.g., "Fleet-Wide Security Audit")
- **The problem** (2-3 sentences)
- **How FleetLift solves it** (which template, what the DAG looks like, what the output is)
- **Example command** (`fleetlift run start audit --param repos=...`)
- **Why this is hard without FleetLift** (manual alternative)

Target scenarios:
1. Fleet-wide security audit with executive report
2. Bulk dependency upgrade across 50 repos
3. Code migration (e.g., logging framework swap) with validation
4. Automated PR review with human escalation
5. Incident response: triage → root cause → fix → verify
6. Adding test coverage to under-tested repos
7. Cross-repo research ("which repos use deprecated API X?")

### Phase 3: "FleetLift vs Alternatives" Page

**File:** `docs/COMPARISON.md`

Honest comparison matrix covering:
- **Cursor Automations** — event triggers are better, but no DAG, no multi-repo, no HITL gates, SaaS-only
- **GitHub Actions + Copilot** — CI-native but no agent orchestration, no HITL, no knowledge loop
- **Raw Temporal** — powerful but you build everything yourself; FleetLift is the opinionated layer
- **Codegen / Sweep / similar** — typically single-repo, no DAG, no fleet operations
- **Custom scripts** — no orchestration, no streaming, no approval gates, no audit trail

Frame as: "Choose FleetLift when you need X. Choose Y when you need Z." Respectful, not combative.

### Phase 4: Getting Started Tutorial

**File:** `docs/GETTING_STARTED.md`

Step-by-step guide that takes a new user from zero to "I just ran an AI agent across 3 repos and got a merged PR":

1. Prerequisites (Docker, Go, Anthropic API key)
2. Clone + boot infrastructure (5 min)
3. Create your first workflow from a builtin template
4. Run it against a test repo
5. Watch the DAG execute in the web UI
6. Approve a step via CLI
7. See the PR get created
8. Modify the template for your needs
9. Next steps (custom templates, fleet operations, knowledge)

Include screenshots at each step.

### Phase 5: CONTRIBUTING.md

**File:** `CONTRIBUTING.md`

Standard OSS contribution guide:
- Development setup (references existing Quick Start)
- Running tests (`go test ./...`, `make lint`, `cd web && npm test`)
- Code style (references CLAUDE.md conventions)
- PR process (branch naming, review expectations)
- Architecture overview (link to ARCHITECTURE.md)
- Where to contribute (good first issues, enhancement backlog)
- Code of conduct (adopt Contributor Covenant or similar)

### Phase 6: Production Deployment Guide

**File:** `docs/DEPLOYMENT.md`

For the platform team evaluating FleetLift for real use:
- Kubernetes deployment (Helm chart or raw manifests for server, worker, temporal, postgres)
- Required secrets setup (JWT_SECRET, CREDENTIAL_ENCRYPTION_KEY, OAuth, OpenSandbox)
- Scaling considerations (worker replicas, Temporal namespace config)
- Observability (Prometheus metrics endpoint already exists, document it)
- Backup strategy (PostgreSQL, Temporal persistence)
- Multi-tenant setup (team provisioning, user onboarding)
- Security hardening checklist

### Phase 7: Example READMEs + Cookbook

**File:** `examples/README.md`

Add a README to the examples directory that:
- Lists every example with a one-line description
- Groups by category (single-repo, multi-repo, report, transform)
- Links to the Workflow Reference for schema details
- Includes a "write your own" mini-tutorial

### Phase 8: Web Landing Route (Optional)

Add a public `/` route in the web SPA that shows a feature overview before login. This is lower priority — the GitHub README is the real landing page for an OSS project. But if the server is deployed publicly, a non-authenticated landing page helps.

- Hero section with tagline
- Feature grid (DAG, HITL, knowledge, fleet ops)
- "Get Started" button → links to GitHub README
- Architecture diagram
- Link to demo video

---

## Priority Order

| Phase | Impact | Effort | Priority |
|-------|--------|--------|----------|
| 1. README rewrite | Very High | Medium | **Do first** |
| 2. Use cases | High | Medium | **Do second** |
| 3. Comparison page | High | Low | **Do third** |
| 4. Getting started tutorial | High | High | **Do fourth** |
| 5. CONTRIBUTING.md | Medium | Low | **Do fifth** |
| 6. Deployment guide | Medium | High | **Do sixth** |
| 7. Example READMEs | Medium | Low | **Do seventh** |
| 8. Web landing route | Low | High | **Defer** |

---

## Success Criteria

A platform engineer who has never heard of FleetLift should be able to:

1. **Understand what it does in 10 seconds** from the README headline + tagline
2. **See why it matters in 60 seconds** from the "Why FleetLift?" section and comparison table
3. **Find their use case in 2 minutes** from USE_CASES.md
4. **Have it running in 15 minutes** from GETTING_STARTED.md
5. **Evaluate it for production in 1 hour** from DEPLOYMENT.md + ARCHITECTURE.md
6. **Know how to contribute** from CONTRIBUTING.md

If all six hold, FleetLift reads as a serious OSS project, not an internal prototype.
