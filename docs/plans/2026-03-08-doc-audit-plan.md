# Documentation Audit & Update — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Audit all repository documentation against current source code and update every doc to accurately reflect the current state of the project.

**Architecture:** A Code Auditor agent reads all 85 Go source files and all existing markdown docs, produces a structured capability inventory, then three specialist doc-writer agents update their assigned docs in parallel using that inventory. The coordinator reviews all changes for cross-doc consistency.

**Tech Stack:** Go (85 source files), Temporal SDK, Markdown documentation

---

### Task 1: Create the team

**Step 1: Create team**

Use TeamCreate with team_name `doc-audit`.

---

### Task 2: Launch Code Auditor

**Step 1: Spawn Code Auditor agent**

Spawn a `general-purpose` agent named `code-auditor` in team `doc-audit` with the following prompt:

```
You are the Code Auditor for the fleetlift documentation audit.

Your job: read ALL Go source files and all existing markdown docs, then produce a comprehensive capability inventory saved to `docs/plans/CAPABILITY_INVENTORY.md`.

## Source files to read (read ALL of these):

### CLI commands (cmd/cli/)
- cmd/cli/main.go
- cmd/cli/create.go
- cmd/cli/create_assets.go
- cmd/cli/knowledge.go
- cmd/cli/templates.go
- cmd/cli/templates_assets.go

### Worker & Server
- cmd/worker/main.go
- cmd/server/main.go
- cmd/agent/main.go

### Activities (internal/activity/)
- internal/activity/constants.go
- internal/activity/agent.go
- internal/activity/github.go
- internal/activity/knowledge.go
- internal/activity/manifest.go
- internal/activity/report.go
- internal/activity/sandbox.go
- internal/activity/slack.go
- internal/activity/util.go

### Workflows (internal/workflow/)
Read all .go files in internal/workflow/

### Models (internal/model/)
Read all .go files in internal/model/

### Agent (internal/agent/)
- internal/agent/pipeline.go
- internal/agent/clone.go
- internal/agent/collect.go
- internal/agent/pr.go
- internal/agent/protocol.go
- internal/agent/constants.go
- internal/agent/deps.go
- internal/agent/fleetproto/types.go

### Config & other
Read all .go files in internal/ not yet covered.

### Existing docs to read:
- README.md
- docs/CLI_REFERENCE.md
- docs/TASK_FILE_REFERENCE.md
- docs/TROUBLESHOOTING.md
- docs/GITHUB_ACTIONS_DISCOVERY.md
- docs/GROUPED_EXECUTION.md
- docs/plans/DESIGN.md
- docs/plans/OVERVIEW.md
- docs/plans/SIDECAR_AGENT.md
- docs/plans/ROADMAP.md
- docs/plans/IMPLEMENTATION_PLAN.md
- docs/examples/README.md
- docs/examples/01-security-audit.md
- docs/examples/02-multi-target-discovery.md
- docs/examples/03-code-transformation.md
- docs/examples/04-deterministic-transform.md
- docs/examples/05-transformation-repository.md
- cmd/cli/schema/task-schema.md
- web/README.md

## What to produce

Write `docs/plans/CAPABILITY_INVENTORY.md` with these sections:

### 1. CLI Commands
For each subcommand: exact name, all flags (name, type, default, description), what it does.
Include: fleetlift run, create, status, list, logs, diff, steer, approve, reject, templates list, knowledge (all subcommands).

### 2. Task File Schema
Complete YAML task file schema as currently implemented: all top-level fields, nested fields, types, defaults, required vs optional. Include execution.agentic, execution.deterministic, grouping, forEach, output, knowledge fields.

### 3. Workflow Architecture
Current Temporal workflows: names, inputs, what they orchestrate, how they relate to each other (TransformV2 vs Transform, etc.).

### 4. Activity Inventory
All registered Temporal activities: name constant, function signature, what it does.

### 5. Agent Protocol
How the sidecar agent binary works: phases, file-based protocol, manifest/status/result/steering files, what protocol files live where.

### 6. Package Structure
Current package layout: what each package does, what was deleted (e.g. internal/sandbox/, internal/agent/protocol/ shim).

### 7. Configuration
All environment variables, config file fields, required vs optional.

### 8. Feature Status
For each feature listed in README "What Works Now" table: is it actually implemented in source? Note any features in docs but not in code, or in code but not in docs.

### 9. Known Drift
List every specific inaccuracy you found between existing docs and current code. Be specific: "CLI_REFERENCE.md line X says flag --foo but the flag is actually --bar in cmd/cli/create.go:45".

When done, send a message to the team lead (named "coordinator") saying: "Inventory complete at docs/plans/CAPABILITY_INVENTORY.md"
```

---

### Task 3: Wait for inventory, then launch doc writers in parallel

**Step 1: Wait for Code Auditor to report completion**

When the code-auditor sends "Inventory complete", proceed.

**Step 2: Spawn User Docs Writer**

Spawn a `general-purpose` agent named `user-docs-writer` in team `doc-audit` with this prompt:

```
You are the User Docs Writer for the fleetlift documentation audit.

Read docs/plans/CAPABILITY_INVENTORY.md thoroughly first.

Then update these files to accurately reflect the current state of the project:

1. README.md — Update the "What Works Now" feature table, "What's Coming" table, CLI usage examples, installation steps, and any other sections that have drifted. Keep the existing structure and tone.

2. docs/CLI_REFERENCE.md — Update every command, subcommand, and flag to match the actual implementation. Add any missing commands/flags. Remove any that no longer exist.

3. docs/TASK_FILE_REFERENCE.md — Update the complete task file schema. Every field, type, default, and description must match what the code actually supports.

4. docs/TROUBLESHOOTING.md — Review for accuracy. Update any instructions that reference deleted packages, old commands, or outdated behavior.

5. docs/GITHUB_ACTIONS_DISCOVERY.md — Update for current CLI flags and task file schema.

6. docs/examples/README.md and docs/examples/01 through 05 — Update all example YAML and CLI invocations to use current syntax.

7. cmd/cli/schema/task-schema.md — Update the JSON schema documentation to match current task file fields.

Rules:
- Do NOT change the overall structure/tone unless something is genuinely wrong
- DO fix every inaccuracy found in the inventory's "Known Drift" section
- DO add documentation for features that exist in code but are missing from docs
- DO remove documentation for features that no longer exist

When done, send a message to "coordinator" saying: "User docs complete. Changed: [list files you modified]"
```

**Step 3: Spawn Architecture Docs Writer**

Spawn a `general-purpose` agent named `arch-docs-writer` in team `doc-audit` with this prompt:

```
You are the Architecture Docs Writer for the fleetlift documentation audit.

Read docs/plans/CAPABILITY_INVENTORY.md thoroughly first.

Then update these files:

1. docs/plans/DESIGN.md — Update architecture diagrams (text/mermaid), component descriptions, data flow, package references. Remove references to deleted packages (internal/sandbox/, internal/agent/protocol/ shim). Add any new components not yet documented.

2. docs/plans/OVERVIEW.md — Update the high-level use case descriptions and architecture overview to match current capabilities.

3. docs/plans/SIDECAR_AGENT.md — Update the sidecar agent documentation: phases, file-based protocol, manifest/status/result/steering file formats, the TransformV2 workflow integration. Ensure it reflects the current fleetproto types and agentbox protocol.

4. docs/GROUPED_EXECUTION.md — Update grouped execution documentation: how grouping works, multi-group orchestration (Phase 10.6), failure thresholds, pause/continue, retry logic.

Rules:
- Fix every inaccuracy in the inventory's "Known Drift" section that pertains to your docs
- Update package paths to reflect current structure
- Keep diagrams/examples but update them to be accurate

When done, send a message to "coordinator" saying: "Architecture docs complete. Changed: [list files you modified]"
```

**Step 4: Spawn Plan Docs Writer**

Spawn a `general-purpose` agent named `plan-docs-writer` in team `doc-audit` with this prompt:

```
You are the Plan Docs Writer for the fleetlift documentation audit.

Read docs/plans/CAPABILITY_INVENTORY.md thoroughly first.

Then update these files:

1. docs/plans/IMPLEMENTATION_PLAN.md — Update phase statuses (✅/🔜) to match what is actually implemented. Update deliverable descriptions to match current code. The header says "Last Updated: 2026-03-08 (Phase 11 complete)" — verify this is accurate and update if needed. Add any phases that have been completed but aren't documented.

2. docs/plans/ROADMAP.md — Update the roadmap to reflect current state. Move completed items to done, update in-progress items, ensure future items are still accurate.

3. web/README.md — Update if it references outdated features or architecture.

Rules:
- Be precise about what is ✅ Complete vs 🔜 Coming
- Cross-check phase completion against actual source files, not just the plan's own status markers
- If a phase is marked complete but the code doesn't support it, mark it accurately

When done, send a message to "coordinator" saying: "Plan docs complete. Changed: [list files you modified]"
```

---

### Task 4: Review and consistency pass

**Step 1: Collect completion messages from all three writers**

Wait for all three writers to report completion.

**Step 2: Consistency review**

Read all updated files. Check:
- Terminology is consistent across docs (e.g. "task" vs "campaign", workflow names)
- Version numbers / phase numbers are consistent
- Cross-references between docs point to correct locations
- No doc references a feature described differently in another doc

**Step 3: Fix any inconsistencies found**

Edit files directly to resolve any cross-doc inconsistencies.

**Step 4: Clean up temp file**

Delete `docs/plans/CAPABILITY_INVENTORY.md` (it was a working artifact, not meant to persist).

**Step 5: Send shutdown to all agents**

Send shutdown_request to: code-auditor, user-docs-writer, arch-docs-writer, plan-docs-writer.

---

### Task 5: Final verification

**Step 1: Verify no broken references**

Search for any remaining references to deleted packages:
- `internal/sandbox/` (deleted, moved to agentbox)
- `internal/agent/protocol/` (shim deleted in Phase AB-4)

Run: `rg "internal/sandbox|internal/agent/protocol" docs/ README.md`
Expected: no matches (or only valid historical references in IMPLEMENTATION_PLAN phase descriptions)

**Step 2: Build check**

Run: `go build ./...`
Expected: no errors (docs changes shouldn't affect build, but verify)

**Step 3: Done**

Report summary of all changes made to the user.
