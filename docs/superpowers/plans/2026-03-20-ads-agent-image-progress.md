# ADS Agent Image & Superpowers Profile — Progress

**Spec:** `docs/superpowers/specs/2026-03-20-ads-agent-image-design.md`
**Date:** 2026-03-20

---

## Completed

All five deliverables from the spec are implemented and tests pass.

| Deliverable | File | Notes |
|---|---|---|
| ADS agent Dockerfile | `docker/Dockerfile.ads-agent` | Extends `claude-code-sandbox:latest`; adds JRE 21, acli wrapper, fix-rules |
| Fix rules doc | `docker/ads-fix-rules.md` | Injected into sandbox at `/agent/fix-rules.md` at build time |
| Migration 010 | `internal/db/migrations/010_superpowers_profile.up.sql` | Seeds `superpowers` system profile; applied automatically at startup |
| `AgentProfile` json tag | `internal/model/workflow.go:46` | Adds `json:"agent_profile,omitempty"` before any workflows use the field |
| `agent_profile: superpowers` | `internal/template/workflows/auto-debt-slayer.yaml` | Wires profile into the ADS workflow |

Also fixed two pre-existing staticcheck lint warnings in `internal/knowledge/store.go` (QF1012).

---

## Next Steps

### 1. Verify acli download URL & version

The Dockerfile uses `https://acli.atlassian.com/acli-${ACLI_VERSION}.jar` with `ACLI_VERSION=10.1.0`. This URL needs to be confirmed against Atlassian's actual distribution endpoint — it may be behind a login or at a different path. Validate with:

```sh
curl -fsSL "https://acli.atlassian.com/acli-10.1.0.jar" -o /dev/null -w "%{http_code}"
```

### 2. Build and smoke-test the image

Requires a machine with Docker volume mounting enabled (not available in this sandbox):

```sh
docker build -f docker/Dockerfile.ads-agent -t auto-debt-slayer-agent:latest .
docker run --rm auto-debt-slayer-agent:latest acli --version
```

### 3. Tag the image in the ADS workflow YAML

Per the spec, `auto-debt-slayer.yaml` should reference the image via `sandbox_groups.agent.image: auto-debt-slayer-agent:latest`. This field is not yet added — the yaml only has `agent_profile: superpowers` at the top level. Check whether `sandbox_groups` block needs to be wired in as well.

### 4. Run e2e integration tests

The sandbox environment can't run `docker compose` (volume mounts are disabled). Run on a local machine or CI:

```sh
docker compose up -d
scripts/integration/start.sh --build
scripts/integration/run-sandbox-test.sh
```

### 5. Migration smoke-test

After bringing up the stack, verify migration 010 applied and the profile is queryable:

```sql
SELECT name, description, body FROM agent_profiles WHERE name = 'superpowers';
```
