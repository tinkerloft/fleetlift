# Fleetlift Enhancement Backlog

All tracked items have been moved to `docs/plans/ROADMAP.md`.

---

## Pending Triage

Items below have not yet been placed on the roadmap.

### Background Document Assessor

Run overnight across every repository with recent changes, check the documents against the code, report back findings and/or raise PRs to fix.

### End-to-End Code Change Manager

Manage a code change from start to finish: creation → CI → fixes → handle review comments → CI → notify user to take over.

### Agent MCP / Skill Profiles

Workflows declare an `agent_profile` that installs plugins, skills, and MCPs into the Claude agent sandbox before execution, with eval-time plugin injection support.

### User prompt injecting into exisitng workflows
- Tailoring PR review prompts

### User authoring new workflows

## Remove 'mode: [report|transform]' from workflow schema

## PRD for platform improvements
- docs/plans/2026-03-18-workflow-expressiveness-prd.md


### Snagging issues
 * Costs are collected or displaued
 * Run durations aren't shown against individual steps
 * Inputs **for a step** aren't shown, only the overall DAG
 * Outputs from each step don't appear as individual reports / artifacts
 * Outputs are not human readable, they're structured JSON.  How do I see at a glance, and download, a report that's easy to consume ?
