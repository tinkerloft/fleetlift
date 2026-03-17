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

- Spec: `docs/superpowers/specs/2026-03-17-agent-profile-design.md`
- Plan: `docs/superpowers/plans/2026-03-17-agent-profiles.md`

### CLI `init-local` Improvements

Make `fleetlift init-local` fully self-contained — preflight checks (Go, npm, temporal, Docker, port 8080), auto-build binaries, Docker stack auto-detect, auto-start + health verify at the end.

- Spec: `docs/superpowers/specs/2026-03-16-init-local-improvements-design.md`
