# Documentation Audit & Update — Design

**Date**: 2026-03-08

## Goal

Audit all repository documentation against current source code and update every doc to accurately reflect the current state of the project.

## Approach: Domain-Parallel Team

A Code Auditor produces a single authoritative capability inventory; three specialist doc-writer agents consume it in parallel; a coordinator reviews cross-doc consistency.

## Team Structure

| Agent | Responsibility |
|-------|---------------|
| **Code Auditor** | Read all Go source files and existing docs; produce structured capability inventory covering CLI commands/flags, workflows, activities, models, agent protocol, and configuration |
| **User Docs Writer** | Update `README.md`, `docs/CLI_REFERENCE.md`, `docs/TASK_FILE_REFERENCE.md`, `docs/TROUBLESHOOTING.md`, `docs/GITHUB_ACTIONS_DISCOVERY.md` |
| **Architecture Docs Writer** | Update `docs/DESIGN.md`, `docs/OVERVIEW.md`, `docs/SIDECAR_AGENT.md`, `docs/GROUPED_EXECUTION.md` |
| **Plan Docs Writer** | Update `docs/plans/IMPLEMENTATION_PLAN.md`, `docs/ROADMAP.md` |
| **Coordinator (team lead)** | Sequences work, distributes inventory, reviews all changes for cross-doc consistency |

## Data Flow

1. Code Auditor reads source → produces `CAPABILITY_INVENTORY.md` (temp working doc)
2. All three doc writers receive inventory + their assigned docs
3. Writers update docs in parallel
4. Coordinator reviews for terminology consistency, removes temp inventory

## Scope

All markdown files in the repository root and `docs/` directory. No code changes.

## Success Criteria

- Every CLI command/flag documented in CLI_REFERENCE matches `cmd/cli/*.go`
- README feature table matches actual implemented features
- IMPLEMENTATION_PLAN phase statuses match current codebase
- Architecture docs reflect current package structure (no references to deleted packages)
- All cross-references between docs are consistent
