# Agent Profile — Skill & MCP Injection Design

**Date:** 2026-03-17
**Status:** Draft
**Scope:** How fleetlift configures Claude agents with skills, MCPs, and plugins for both normal workflow operation and skill evaluation

---

## Problem

Claude agents running inside fleetlift sandboxes currently have no skills, no MCPs beyond the fleetlift sidecar, and no plugins. This limits what agents can do — they cannot use organisation-specific slash commands, specialised tooling, or external data sources.

Two distinct use cases need to be solved:

1. **Normal operation** — workflows need a stable set of skills and MCPs to do their job (e.g. a helm-audit workflow needs the `miro-helm-doctor` plugin and the Digital Twin MCP)
2. **Evaluation** — a skill or plugin under development needs to be tested against real workloads before it is published to the marketplace; the version under test is not yet in any marketplace

---

## Concepts

### Marketplace

A GitHub repository that hosts plugins and skills. Fleetlift supports one default marketplace defined by server config, with optional additional marketplaces registered in the database.

The default marketplace is configured via env vars:

```
FLEETLIFT_MARKETPLACE_URL=https://github.com/miroapp-dev/claude-marketplace.git
FLEETLIFT_MARKETPLACE_CREDENTIAL=GITHUB_TOKEN   # CredStore name; empty = public repo
```

Additional marketplaces (team-scoped or system-wide) can be registered via API and stored in a `marketplaces` DB table:

```go
type Marketplace struct {
    ID         string
    Name       string   // e.g. "miro-official", "team-internal"
    RepoURL    string
    Credential string   // CredStore name for GitHub token; empty string = no auth required
    TeamID     *string  // nil = system-wide
    CreatedAt  time.Time
}
```

Omitting `Marketplace` on a `PluginSource` or `SkillSource` implies the default marketplace.

### Plugin

A directory in a marketplace repository containing a `plugin.yaml` manifest. A plugin bundles any combination of skills and MCP configurations. Skills-only, MCPs-only, and mixed plugins are all valid.

**Manifest format (`plugin.yaml`):**

```yaml
name: miro-helm-doctor
description: Helm values authoring and diagnosis for miro-svc-helm-chart

skills:
  - skills/helm-values
  - skills/helm-diagnosis

mcps:
  - name: miro-digital-twin
    type: remote
    transport: sse
    url: https://digital-twin.miro.internal/sse
    credentials: [DIGITAL_TWIN_TOKEN]
```

### Agent Profile

A named, reusable configuration that declares which plugins, standalone skills, and standalone MCPs should be available to the agent. Profiles are stored in the database and referenced by workflow templates.

```go
type AgentProfile struct {
    ID          string
    TeamID      *string   // nil = system-wide
    Name        string    // e.g. "helm-auditor", "baseline"
    Description string
    Plugins     []PluginSource
    Skills      []SkillSource
    MCPs        []MCPConfig
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// Exactly one of (Marketplace+Plugin) or GitHubURL must be set.
// Validated at profile creation time — API handler rejects profiles where both or neither are set.
// GitHubURL must use https:// scheme (same rule as RepoRef.URL — rejects file://, ssh://, git://).
type PluginSource struct {
    Marketplace string  // name of registered marketplace; omit to use default
    Plugin      string  // path within marketplace repo, e.g. "plugins/miro-helm-doctor"
    GitHubURL   string  // direct URL to plugin directory — for eval, bypasses marketplace
}

// Same one-of invariant and validation rules as PluginSource.
type SkillSource struct {
    Marketplace string
    Skill       string  // path within marketplace repo, e.g. "skills/git-helpers"
    GitHubURL   string  // direct URL to skill directory — for eval
}

type MCPConfig struct {
    Name        string    // logical name, key in MCP config
    Type        string    // "remote" (binary MCPs are out of scope for this spec)
    Transport   string    // "http" or "sse" — passed to `claude mcp add --transport`
    URL         string    // HTTP/SSE endpoint
    Headers     []Header  // e.g. Authorization — values support ${ENV_VAR} substitution
    Credentials []string  // CredStore names injected as env vars before agent runs
}

type Header struct {
    Name  string
    Value string
}
```

---

## Model Changes

`WorkflowDef` gains a top-level field:

```go
type WorkflowDef struct {
    // ... existing fields ...
    AgentProfile string  // optional; references AgentProfile.Name
}
```

`ExecutionDef` gains a field for eval-time plugin injection:

```go
type ExecutionDef struct {
    // ... existing fields ...
    EvalPlugins []string  // GitHub URLs; rendered from template params at dispatch time
}
```

`EvalPlugins` values are rendered as Go templates (same as `Prompt`) so they can reference run parameters:

```yaml
eval_plugins:
  - "{{ .Params.plugin_url }}"
```

---

## Workflow Integration

Workflow templates declare an `agent_profile` by name at the workflow level. All steps inherit it.

```yaml
version: 1
id: helm-audit
agent_profile: helm-auditor

steps:
  - id: diagnose
    execution:
      agent: claude-code
      prompt: /helm-diagnosis
  - id: summarise
    execution:
      agent: claude-code
      prompt: Summarise the findings from the previous step.
```

### Resolution

At run time, the effective profile is resolved by merging two layers in order:

1. **System baseline** — the `AgentProfile` named `"baseline"` (system-wide, `TeamID = nil`). Seeded by a DB migration on first deployment. If absent, treated as an empty profile.
2. **Workflow profile** — the profile named in `agent_profile`, looked up as follows:
   - First: a team-scoped profile with that name for the run's team
   - Fallback: a system-wide profile with that name
   - If neither exists: error at run dispatch time, not silently skipped

Plugins, skills, and MCPs **accumulate** across layers — the workflow profile extends the baseline rather than replacing it. Deduplication key for plugins is the `Plugin` path (e.g. `"plugins/miro-helm-doctor"`) for marketplace sources, or the full `GitHubURL` for eval sources. If the same key appears in both layers, the later layer wins.

If no `agent_profile` is declared in the workflow, only the baseline applies.

### Data flow

The effective profile is resolved once per DAG run (not per step). Per-step `eval_plugins` are rendered at step dispatch time since they may reference step-level template variables.

```
DAGWorkflow
  └─ resolve effective profile once (DB lookup: baseline + workflow profile)
  └─ for each step: render EvalPluginURLs from step's ExecutionDef.EvalPlugins
  └─ pass ResolvedStepOpts to StepWorkflow

ResolvedStepOpts (new fields):
  EffectiveProfile  *AgentProfile   // resolved merged profile (same for all steps)
  EvalPluginURLs    []string        // rendered eval_plugins for this specific step

StepWorkflow
  └─ ProvisionSandbox activity receives ResolvedStepOpts
       └─ runs pre-flight script (plugin install, mcp add) from EffectiveProfile
       └─ clones EvalPluginURLs → returns EvalPluginDirs []string
  └─ ExecuteStep activity receives EvalPluginDirs
       └─ ClaudeCodeRunner.Run appends --plugin-dir flags to claude invocation
```

---

## Provision-Time Mechanics

Profile materialisation happens in `ProvisionSandbox`, after sandbox creation and before main agent execution. It consists of two phases.

### Phase 1 — Pre-flight script

A shell script is generated from the resolved effective profile and executed inside the sandbox via `sandbox.Exec`:

```bash
# Configure git auth for private marketplace
git config --global credential.helper store
echo "https://x-access-token:${GITHUB_TOKEN}@github.com" > ~/.git-credentials

# Register marketplace
claude plugin marketplace add https://github.com/miroapp-dev/claude-marketplace

# Install plugins (remove first to ensure idempotency)
claude plugin uninstall miro-helm-doctor 2>/dev/null || true
claude plugin install miro-helm-doctor

# Register standalone MCPs (remove first to ensure idempotency)
claude mcp remove miro-digital-twin 2>/dev/null || true
claude mcp add --transport sse --scope user miro-digital-twin \
  https://digital-twin.miro.internal/sse \
  --header "Authorization: Bearer ${DIGITAL_TWIN_TOKEN}"
```

`GITHUB_TOKEN` and any MCP credential values are fetched from CredStore and injected as sandbox env vars before this script runs — the same path as existing run credentials.

Idempotency is handled explicitly: each resource is removed before re-adding, so sandbox reuse across steps is safe.

### Phase 2 — Eval plugin dirs

For `EvalPluginURLs` entries (rendered from `eval_plugins` in `ExecutionDef`), the pre-flight script additionally clones each target directory:

```bash
# Each GitHubURL is validated for https:// scheme before use
git clone --depth 1 --filter=blob:none --sparse \
  https://github.com/org/repo.git /tmp/eval-plugin-0
cd /tmp/eval-plugin-0
git sparse-checkout set plugins/miro-helm-doctor
```

`ProvisionSandbox` returns the cloned paths as `EvalPluginDirs []string`. These are passed to `ClaudeCodeRunner.Run`, which appends `--plugin-dir` flags to the `claude` invocation:

```bash
claude -p "..." \
  --plugin-dir /tmp/eval-plugin-0/plugins/miro-helm-doctor \
  --output-format stream-json --verbose --dangerously-skip-permissions --max-turns N
```

`--plugin-dir` is session-only — it does not persist to user config and does not interact with marketplace-installed plugins.

---

## Evaluation Pattern

An evaluation workflow treats the plugin under test as a run parameter:

```yaml
version: 1
id: helm-skill-eval
agent_profile: baseline   # only baseline MCPs — no pre-installed helm plugin

parameters:
  - name: plugin_url
    type: string
    required: true
    description: GitHub URL to the plugin directory under evaluation
  - name: repos
    type: json
    required: true
    description: List of repo URLs to audit

steps:
  - id: diagnose
    foreach: "{{ .Params.repos }}"
    execution:
      agent: claude-code
      prompt: /helm-diagnosis
      eval_plugins:
        - "{{ .Params.plugin_url }}"

  - id: aggregate
    depends_on: [diagnose]
    execution:
      agent: claude-code
      prompt: |
        Aggregate findings across all repos. Produce a structured report:
        per-repo findings table, A/B/C category counts, systemic issues.
```

Each sandbox runs with only the eval plugin active. To compare versions, dispatch two runs with different `plugin_url` values — run history and cost tracking provide the comparison baseline.

---

## Database Changes

New table: `marketplaces`

```sql
CREATE TABLE marketplaces (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    repo_url    TEXT NOT NULL,
    credential  TEXT NOT NULL DEFAULT '',  -- empty = no auth required
    team_id     TEXT REFERENCES teams(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Unique name per team; system-wide (team_id IS NULL) names also unique
CREATE UNIQUE INDEX marketplaces_team_name_idx ON marketplaces (team_id, name) WHERE team_id IS NOT NULL;
CREATE UNIQUE INDEX marketplaces_system_name_idx ON marketplaces (name) WHERE team_id IS NULL;
```

New table: `agent_profiles`

```sql
CREATE TABLE agent_profiles (
    id          TEXT PRIMARY KEY,
    team_id     TEXT REFERENCES teams(id),
    name        TEXT NOT NULL,
    description TEXT,
    body        JSONB NOT NULL,   -- serialised AgentProfile (plugins, skills, mcps)
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Unique name per team; system-wide (team_id IS NULL) names also unique
CREATE UNIQUE INDEX agent_profiles_team_name_idx ON agent_profiles (team_id, name) WHERE team_id IS NOT NULL;
CREATE UNIQUE INDEX agent_profiles_system_name_idx ON agent_profiles (name) WHERE team_id IS NULL;
```

New column on `workflow_templates`:

```sql
ALTER TABLE workflow_templates ADD COLUMN agent_profile TEXT;
-- References agent_profiles.name (soft reference — no FK, profile may not exist yet)
```

The `"baseline"` system profile is seeded by the migration that creates `agent_profiles`, as an empty profile (no plugins, skills, or MCPs). A team can register a profile also named `"baseline"` — this replaces the system baseline for that team's runs (team-scoped profile wins in lookup order).

---

## Open Questions

1. **Marketplace auth in sandbox** — does `claude plugin marketplace add` respect `GITHUB_TOKEN` via git credential helper, or does it need a different mechanism? Needs verification against a real private repo before implementation.
2. **Pre-flight caching** — sparse clones of the marketplace repo are repeated per sandbox. A shared cache on the fleetlift host (or a pre-warmed sandbox image) could reduce provision time for large marketplaces. Out of scope for initial implementation.
3. **Binary MCPs** — MCPs that are local binaries (e.g. the current fleetlift sidecar) are out of scope here. A follow-up spec is needed if binary MCPs from the marketplace are required.
