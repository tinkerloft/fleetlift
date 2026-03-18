# Agent Profiles

Agent profiles configure Claude agents with plugins, MCPs, and skills before they execute workflow steps. Profiles are resolved at run time and applied to every step in the workflow via a pre-flight script that runs inside the sandbox.

---

## Concepts

### Profile

A named configuration that declares which plugins and MCPs should be available to the agent. Profiles are stored in the database and referenced by workflow templates.

Profiles can be **team-scoped** (visible only to the owning team) or **system-wide** (visible to all teams). A team-scoped profile with the same name as a system profile takes precedence for that team's runs.

### Baseline

A special system-wide profile named `baseline` is seeded automatically on first deployment. It starts empty (no plugins, skills, or MCPs). Teams can create a team-scoped `baseline` to override the system default.

Every workflow run merges the baseline with the workflow's declared profile. Plugins, skills, and MCPs accumulate across layers — the workflow profile extends the baseline rather than replacing it. If the same plugin or MCP appears in both, the workflow profile's version wins.

### Marketplace

A GitHub repository that hosts plugins and skills. Registered via the API so the pre-flight script can install plugins from it using `claude plugin install`.

### Eval Plugins

Plugins under development that are not yet published to any marketplace. These are injected as `--plugin-dir` flags on the `claude` invocation, loaded from a GitHub URL via sparse checkout. Useful for A/B testing plugin versions against real workloads.

---

## API

### Agent Profiles

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/agent-profiles` | List all profiles visible to the team |
| `POST` | `/api/agent-profiles` | Create a new profile |
| `GET` | `/api/agent-profiles/{id}` | Get a single profile |
| `PUT` | `/api/agent-profiles/{id}` | Update a profile |
| `DELETE` | `/api/agent-profiles/{id}` | Delete a profile |

#### Create/Update body

```json
{
  "name": "helm-auditor",
  "description": "Profile for Helm values auditing workflows",
  "body": {
    "plugins": [
      {"plugin": "plugins/miro-helm-doctor"}
    ],
    "skills": [],
    "mcps": [
      {
        "name": "digital-twin",
        "type": "remote",
        "transport": "sse",
        "url": "https://digital-twin.example.com/sse",
        "headers": [
          {"name": "Authorization", "value": "Bearer ${DT_TOKEN}"}
        ],
        "credentials": ["DT_TOKEN"]
      }
    ]
  }
}
```

**Plugin sources** — exactly one of:
- `plugin`: path within a registered marketplace (e.g. `plugins/miro-helm-doctor`)
- `github_url`: direct GitHub URL for eval/development plugins (`https://github.com/org/repo/tree/main/plugins/foo`)

**Skill sources** — same one-of rule:
- `skill`: path within a marketplace
- `github_url`: direct GitHub URL

**MCP config:**
- `name`: logical name used as the key in Claude's MCP config
- `type`: must be `remote` (binary MCPs are not supported)
- `transport`: `http` or `sse`
- `url`: must use `http://` or `https://` scheme
- `headers`: optional HTTP headers (values support `${ENV_VAR}` substitution)
- `credentials`: CredStore names injected as env vars before the pre-flight runs

### Marketplaces

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/marketplaces` | List all marketplaces |
| `POST` | `/api/marketplaces` | Register a new marketplace |
| `DELETE` | `/api/marketplaces/{id}` | Remove a marketplace |

```json
{
  "name": "miro-official",
  "repo_url": "https://github.com/miroapp-dev/claude-marketplace.git",
  "credential": "MARKETPLACE_TOKEN"
}
```

- `repo_url` must use `https://` scheme
- `credential` is the CredStore name for a GitHub token (empty for public repos)

---

## Workflow Configuration

### Declaring a profile

Add `agent_profile` at the top level of your workflow YAML:

```yaml
version: 1
id: helm-audit
agent_profile: helm-auditor

steps:
  - id: diagnose
    execution:
      agent: claude-code
      prompt: /helm-diagnosis
```

All steps in the workflow inherit the profile. The effective profile is resolved once per run (not per step).

### Eval plugins

Add `eval_plugins` to a step's `execution` block to inject unreleased plugins:

```yaml
steps:
  - id: diagnose
    execution:
      agent: claude-code
      prompt: /helm-diagnosis
      eval_plugins:
        - "{{ .Params.plugin_url }}"
```

Each URL is a GitHub tree URL pointing to a plugin directory (e.g. `https://github.com/org/repo/tree/main/plugins/foo`). The directory is sparse-cloned into the sandbox and passed to `claude` via `--plugin-dir`.

### No profile

If `agent_profile` is not declared, only the baseline profile applies. If the baseline is empty (default), no pre-flight script runs and the agent starts with its default configuration.

---

## How It Works

```
DAGWorkflow
  ├─ ResolveAgentProfile activity
  │    └─ Looks up baseline + workflow profile from DB
  │    └─ Merges them (later layer wins on conflict)
  │    └─ Adds MCP credentials to credential preflight check
  │
  └─ For each step:
       ├─ ProvisionSandbox
       ├─ RunPreflight activity
       │    ├─ Generates shell script from merged profile:
       │    │    ├─ git credential setup (for private marketplaces)
       │    │    ├─ claude plugin marketplace add <url>
       │    │    ├─ claude plugin install <name> (for each marketplace plugin)
       │    │    └─ claude mcp add --transport <t> <name> <url> (for each MCP)
       │    ├─ Executes script in sandbox
       │    └─ Sparse-clones eval plugin URLs → returns local dirs
       │
       └─ ExecuteStep
            └─ claude -p "..." --plugin-dir /tmp/eval-plugin-0/plugins/foo
```

### Deduplication

Plugins deduplicate by path (marketplace) or URL (GitHub). MCPs deduplicate by name. If the same key appears in both baseline and workflow profile, the workflow profile version wins.

### Idempotency

The pre-flight script removes each resource before re-adding it (`claude plugin uninstall ... || true`, `claude mcp remove ... || true`), making sandbox reuse across steps safe.

---

## Example: Evaluation Workflow

Test a plugin under development against multiple repos:

```yaml
version: 1
id: helm-skill-eval
agent_profile: baseline

parameters:
  - name: plugin_url
    type: string
    required: true
    description: GitHub URL to the plugin directory under evaluation
  - name: repos
    type: json
    required: true

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
        Aggregate findings across all repos. Produce a structured report.
```

To compare plugin versions, dispatch two runs with different `plugin_url` values.
