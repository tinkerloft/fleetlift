# Design: ADS Agent Image, Superpowers Profile, and Workflow Update

**Date:** 2026-03-20
**Status:** Approved
**Context:** Completes the auto-debt-slayer workflow implementation. Provides the sandbox image that enrich/execute steps run in, the superpowers agent profile for TDD and code review, and wires both into the workflow YAML.

---

## Deliverables

### 1. `docker/Dockerfile.ads-agent`

Extends `claude-code-sandbox:latest` (Ubuntu 24.04). Adds:

- **JRE**: `openjdk-21-jre-headless` — required to run the acli JAR
- **acli**: Atlassian CLI JAR downloaded to `/usr/local/bin/acli.jar` with a thin wrapper at `/usr/local/bin/acli` so it is callable as `acli <args>`
- **`/agent/fix-rules.md`**: Non-negotiable coding rules injected into every execute prompt. Copied from `docker/ads-fix-rules.md` at build time.

No custom entrypoint — Fleetlift's sandbox handles execution.

The image is tagged `auto-debt-slayer-agent:latest` and referenced in `auto-debt-slayer.yaml` via `sandbox_groups.agent.image`.

### 2. `docker/ads-fix-rules.md`

Verbatim copy of `auto-debt-slayer/knowledge/FIX_RULES.md`. Covers: code quality, investigation discipline, safety (no auth/encryption changes, no data deletion), testing, verification (type-safety, no orphaned exports), git (no commit/push — platform handles it), and review readiness.

### 3. `internal/db/migrations/010_superpowers_profile.up.sql`

Seeds a system-wide (null team_id) agent profile named `superpowers`:

```sql
INSERT INTO agent_profiles (id, name, description, body)
VALUES (
    gen_random_uuid(),
    'superpowers',
    'Installs the superpowers plugin for TDD and code review workflows',
    '{"plugins":[{"plugin":"superpowers@5.0.1"}]}'
);
```

Pattern matches migration 005 (`baseline` profile). Applied automatically at server/worker startup via `db.Migrate()`.

### 4. `internal/model/workflow.go` — add `json` tag to `AgentProfile`

The `AgentProfile` field is currently missing a `json:` tag. Since `WorkflowDef` is serialized into Temporal history as JSON, the key must be stable before any workflows run. Add the tag now:

```go
// BEFORE:
AgentProfile  string  `yaml:"agent_profile,omitempty"`

// AFTER:
AgentProfile  string  `yaml:"agent_profile,omitempty" json:"agent_profile,omitempty"`
```

### 5. `internal/template/workflows/auto-debt-slayer.yaml` — one-line addition

Add `agent_profile: superpowers` at the top level (same position as `profile-test.yaml`):

```yaml
version: 1
id: auto-debt-slayer
title: Auto Debt Slayer
agent_profile: superpowers
...
```

---

## acli wrapper script

The wrapper at `/usr/local/bin/acli`:

```sh
#!/bin/sh
exec java -jar /usr/local/bin/acli.jar "$@"
```

---

## Notes

- **Sparse profile body**: `{"plugins":[...]}` omitting `skills` and `mcps` is safe — the consumer (`preflight.go`) ranges over slices, and nil slices are range-safe in Go.
- **No `.down.sql`**: Consistent with migrations 005–009. Data-only seed; deliberate choice.

## Testing

- `go test ./internal/db/...` — verifies migration applies cleanly
- `go test ./internal/template/...` — verifies YAML still parses with the new `agent_profile` field
- `go test ./internal/model/...` — verifies WorkflowDef JSON round-trip includes `agent_profile`
- Manual: `docker build -f docker/Dockerfile.ads-agent -t auto-debt-slayer-agent:latest .` confirms the image builds; `docker run --rm auto-debt-slayer-agent:latest acli --version` exits 0 with a version string
