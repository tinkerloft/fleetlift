# Code Transformation Platform - Technical Design

## Overview

A platform for automated code transformations and discovery across repositories, supporting:

- **Deployment**: Local (Docker) or production (Kubernetes)
- **Scope**: Single repository or fleet-wide via grouped execution
- **Execution**: Deterministic (commands in sandbox) or agentic (AI prompts)
- **Mode**: Transform (create PRs) or Report (collect structured data)

### Vision: Managed Turbolift

Think of this as **managed [Turbolift](https://github.com/Skyscanner/turbolift)** with two execution backends:

| Turbolift | This Platform |
|-----------|---------------|
| CLI on your laptop | Managed service (Temporal + K8s) |
| Script/command | Docker image OR agent prompt |
| Dies if laptop closes | Durable execution, survives failures |
| No approval flow | Human-in-the-loop before PR |
| Stateless | Status tracking, audit trail |

### What We Build vs. What We Adopt

| Build (Custom) | Adopt (Standard) |
|----------------|------------------|
| Task and Campaign schemas | Temporal (workflow orchestration) |
| Task data model | Kubernetes Jobs (sandbox execution) |
| Workflow logic | Claude Code (agentic execution) |
| CLI interface | OpenRewrite/Scalafix (deterministic transforms) |
| GitHub PR integration | Docker/containerd (container runtime) |
| Sidecar agent (`fleetlift-agent`) | gVisor/Kata (optional isolation) |
| File-based protocol (manifest/status/result) | |
| Configuration loading | |

**Principle**: Build the orchestration glue, adopt standards for infrastructure.

---

## Core Concepts

### Task

The atomic unit of work. A Task operates on one or more repositories and either:
- **Transforms** code and creates PRs (`mode: transform`)
- **Reports** structured data without PRs (`mode: report`)

Tasks support grouped execution for fleet-wide operations:
- Organize repositories into groups that share a sandbox
- Execute groups in parallel with configurable concurrency (`max_parallel`)
- Pause on failure thresholds for human intervention
- Retry failed groups independently

### Modes

| Mode | Output | Approval | Use Case |
|------|--------|----------|----------|
| `transform` | Pull requests | Yes | Migrations, upgrades, fixes |
| `report` | Structured JSON | No | Audits, inventories, discovery |

### Execution Types

| Type | Implementation | Verification |
|------|----------------|--------------|
| **Deterministic** | Command executed directly in sandbox (e.g. `mvn rewrite:run`). Sandbox image contains the required tools. | Exit code + verifiers |
| **Agentic** | Claude Code CLI with prompt | Agent iterates until verifiers pass |

Both execution types use the same sidecar agent (`fleetlift-agent serve`). For deterministic, the `execution.image` field specifies the sandbox base image (with the tools pre-installed), and the agent binary is injected via init container at deploy time. See [SIDECAR_AGENT.md](./SIDECAR_AGENT.md) for details.

### Transformation Repository

A **transformation repository** separates the "recipe" (how to transform/analyze) from the "targets" (what to operate on). This enables reusable skills, tools, and configuration.

| Concept | Description |
|---------|-------------|
| **Transformation repo** | Contains `.claude/skills/`, `CLAUDE.md`, tools, and configuration |
| **Target repos** | Repositories being analyzed or transformed |

**Workspace Layout (Transformation Mode):**
```
/workspace/
├── .claude/         # From transformation repo
│   └── skills/      # Skills discovered by Claude Code
├── CLAUDE.md        # From transformation repo
├── bin/             # Tools from transformation repo
└── targets/
    ├── server/      # Target repos cloned here
    └── client/
```

**Workspace Layout (Legacy Mode):**
```
/workspace/
├── server/          # Repos cloned directly
└── client/
```

Claude Code runs from `/workspace`, so transformation repo skills are automatically discovered.

### Knowledge Items (Continual Learning)

The platform captures knowledge from successful transformations and reuses it to improve future runs. This turns Fleetlift from a "run once" tool into a system that compounds organizational knowledge.

**Knowledge Types:**

| Type | Description | Example |
|------|-------------|---------|
| `pattern` | Reusable approach that worked | "When migrating logger X→Y, also update config files" |
| `correction` | Extracted from steering, where agent went wrong | "Agent missed test helpers wrapping the logger" |
| `gotcha` | Non-obvious failure mode | "Python 3.9→3.11 breaks walrus operator in certain comprehensions" |
| `context` | Repo-specific knowledge | "This repo uses a custom build system, run `make` not `go build`" |

**Three-Tier Storage:**

| Tier | Location | Lifecycle | Scope |
|------|----------|-----------|-------|
| 1. Execution log | Temporal workflow history | Automatic, ephemeral | Debugging, audit |
| 2. Local store | `~/.fleetlift/knowledge/` | Auto-captured after approval | Personal, per-machine |
| 3. Transformation repo | `.fleetlift/knowledge/` in repo | Human-curated, committed | Team-shared, version-controlled |

**Capture trigger**: After a transformation is approved, Claude analyzes the execution transcript (original prompt, steering corrections, final diff, verifier results) and extracts reusable knowledge items. The most valuable signal is **steering corrections** — when a human says "do it this way instead," that correction persists for future runs.

**Prompt enrichment**: Before running Claude Code, relevant knowledge items are loaded from Tier 3 (transformation repo) then Tier 2 (local), filtered by tags, and appended to the prompt as a "lessons from previous runs" section.

**Transparency**: Unlike AWS Transform's opaque "continual learning," knowledge items are YAML files that can be read, edited, shared, audited, and version-controlled.

### Natural Language Task Creation

Users can create Task YAML files through conversation instead of writing YAML by hand:

- **Interactive**: `fleetlift create` — guided multi-step flow with clarifying questions
- **One-shot**: `fleetlift create --describe "..."` — single command, Claude infers all parameters

The create flow:
1. Determines mode (transform/report) and execution type (agentic/deterministic) from intent
2. Discovers repositories via GitHub API (`gh` CLI) — supports org-wide patterns
3. Generates the transformation prompt from natural language description
4. Suggests matching transformation repos from a local registry
5. Injects relevant knowledge items into the generated prompt
6. Outputs a YAML file for review — inspectable, editable, version-controllable

**Key principle**: The output is always a YAML file, not a direct execution. This keeps the system transparent and reproducible. The YAML schema + canonical examples are bundled in the CLI binary as context for Claude.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Architecture                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   Interface Layer                                                           │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │   CLI                    │   (Optional) K8s Operator                │   │
│   │   fleetlift run          │   Watches YAML, submits to Temporal      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│       │                                        │                             │
│       └────────────────────┬───────────────────┘                             │
│                            ▼                                                 │
│   Orchestration Layer                                                       │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                         Temporal                                     │   │
│   │                                                                      │   │
│   │   - Durable task execution                                          │   │
│   │   - Retry policies                                                   │   │
│   │   - Human-in-the-loop signals (approve/reject/steer)                │   │
│   │   - Campaign batch orchestration                                    │   │
│   │                                                                      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│       │                                                                      │
│       ▼                                                                      │
│   Execution Layer                                                           │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                      Sandbox Provider                                │   │
│   │                                                                      │   │
│   │   Local:      Docker containers (direct)                            │   │
│   │   Production: Kubernetes (via Sandbox Controller)                   │   │
│   │                                                                      │   │
│   │   ┌─────────────────────────────────────────────────────────────┐   │   │
│   │   │  Production Flow:                                            │   │   │
│   │   │                                                              │   │   │
│   │   │  Worker ──creates──▶ SandboxRequest CR                      │   │   │
│   │   │                            │                                 │   │   │
│   │   │                            ▼                                 │   │   │
│   │   │                    Sandbox Controller                        │   │   │
│   │   │                            │                                 │   │   │
│   │   │              ┌─────────────┼─────────────┐                   │   │   │
│   │   │              ▼             ▼             ▼                   │   │   │
│   │   │         Create Job    Exec into Pod   Cleanup               │   │   │
│   │   └─────────────────────────────────────────────────────────────┘   │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│       │                                                                      │
│       ▼                                                                      │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                   Transformation Execution                           │   │
│   │                                                                      │   │
│   │   Deterministic: Command in sandbox (OpenRewrite, Scalafix, custom)  │   │
│   │   Agentic:       Claude Code CLI + verifiers via Bash               │   │
│   │                                                                      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│       │                                                                      │
│       ▼                                                                      │
│   Integration Layer                                                         │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │   GitHub (PRs)  │  Slack  │  Storage (reports)  │  Knowledge Store    │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Interface Hierarchy

1. **CLI** (`fleetlift run --file task.yaml`) - Direct submission to Temporal
2. **Temporal API** - Programmatic workflow submission
3. **(Optional) K8s Operator** - Watches YAML resources, submits to Temporal

The K8s operator is an optional convenience layer. The CLI and Temporal API are the primary interfaces.

### Standards Used

| Concern | Solution | Why |
|---------|----------|-----|
| Workflow Orchestration | [Temporal](https://temporal.io/) | Durable execution, signals/queries, battle-tested |
| Sandbox (Local) | Docker | Simple, universal |
| Sandbox (Production) | Kubernetes Jobs | Built-in lifecycle, scaling, isolation |
| Deterministic Transforms | [OpenRewrite](https://docs.openrewrite.org/), [Scalafix](https://scalacenter.github.io/scalafix/) | Mature AST-based tools |
| AI Agent | Claude Code CLI | Agentic loop, context management, tool use |

---

## Data Model

### Schema Versioning

All Task and Campaign YAML files require a `version` field. This enables:

- **Forward compatibility**: Older CLI versions reject unknown schema versions
- **Migration tooling**: `fleetlift migrate --file task.yaml` upgrades schemas
- **Clear deprecation**: Announce version sunset with migration period

**Version Format**: Integer (e.g., `1`, `2`)

| Version | Status | Notes |
|---------|--------|-------|
| `1` | Current | Initial stable schema |

**Breaking Changes** (require version bump):
- Removing or renaming required fields
- Changing field types
- Changing default behavior

**Non-Breaking Changes** (same version):
- Adding optional fields with defaults
- Adding new enum values
- Relaxing validation constraints

**Loader Behavior**:

```go
func LoadTask(data []byte) (*Task, error) {
    // 1. Parse version field first
    var header struct {
        Version *int `yaml:"version"`
    }
    if err := yaml.Unmarshal(data, &header); err != nil {
        return nil, err
    }

    // 2. Validate version
    if header.Version == nil {
        return nil, errors.New("version field is required")
    }
    switch *header.Version {
    case 1:
        return loadTaskV1(data)
    default:
        return nil, fmt.Errorf("unsupported schema version: %d (supported: 1)", *header.Version)
    }
}
```

### Task Schema (task.yaml)

The primary interface for defining transformations:

```yaml
# task.yaml - Complete schema
version: 1                    # Schema version (required, integer)
id: string                    # Unique identifier
title: string                 # Human-readable title
description: string           # Optional longer description

mode: transform | report      # Default: transform

# Option 1: Simple mode - repositories cloned to /workspace/{name}
repositories:
  - url: string               # Git URL (required)
    branch: string            # Default: main
    name: string              # Directory name, derived from URL if not set
    setup:                    # Commands to run after clone
      - string

# Option 2: Transformation mode - recipe repo + targets
# Use transformation + targets OR repositories, not both
transformation:               # Transformation repo (the "recipe")
  url: string                 # Git URL
  branch: string              # Default: main
  name: string                # Auto-derived from URL
  setup:                      # Commands to run after clone
    - string

targets:                      # Target repos (when using transformation)
  - url: string               # Cloned to /workspace/targets/{name}
    branch: string
    name: string
    setup:
      - string

# For report mode: iterate over targets within a repo
for_each:                     # Optional
  - name: string              # Target identifier
    context: string           # Passed to prompt as {{.context}}

execution:
  # Option 1: Agentic (Claude Code)
  agentic:
    prompt: string            # The prompt for the agent
    verifiers:
      - name: string          # e.g., "build", "test"
        command: [string]     # e.g., ["go", "build", "./..."]
    limits:
      max_iterations: int     # Max agent invocations (default: 10)
      max_tokens: int         # Total token budget (default: 100000)
      max_verifier_retries: int # Retries after verifier failure (default: 3)
    output:                   # For report mode
      schema:                 # JSON schema to validate frontmatter
        type: object
        properties: {}

  # Option 2: Deterministic (command in sandbox)
  deterministic:
    image: string             # Sandbox base image (must include the tool + git)
    command: [string]         # Command to run (e.g. ["mvn"])
    args: [string]            # Arguments (e.g. ["rewrite:run"])
    env:                      # Environment variables
      KEY: value
    verifiers:                # Optional post-execution verification
      - name: string
        command: [string]

# Execution settings
timeout: duration             # e.g., "30m" - Total wall-clock time for entire task
                              # Includes: sandbox provisioning, clone, execution, verification
                              # Does NOT include: approval wait time (separate timeout)
                              # For multi-repo tasks: applies to entire task, not per-repo
require_approval: boolean     # Default: true for agentic, false for deterministic
max_parallel: int             # Max concurrent groups (default: 5, grouped execution only)

# Failure handling (grouped execution only)
failure:
  threshold_percent: int      # Pause if >N% of groups fail (0-100)
  action: pause | abort       # What to do when threshold is exceeded (default: pause)
                              # pause: wait for human to continue or abort
                              # abort: immediately skip remaining groups

# PR settings (transform mode only)
pull_request:
  branch_prefix: string       # e.g., "auto/slog-migration"
  title: string               # PR title
  body: string                # PR body template
  labels: [string]            # Labels to apply
  reviewers: [string]         # Reviewers to request

# Knowledge settings (continual learning)
knowledge:
  capture: boolean          # Auto-capture knowledge after approval (default: true)
  enrich: boolean           # Enrich prompt with past knowledge (default: true)
  max_items: int            # Max knowledge items injected into prompt (default: 10)
  tags: [string]            # Additional tags for filtering/matching

# Sandbox settings (production)
sandbox:
  namespace: string           # K8s namespace for sandbox Jobs
  runtime_class: string       # Optional: gvisor, kata
  node_selector:
    key: value
  resources:
    limits:
      memory: string          # e.g., "4Gi"
      cpu: string             # e.g., "2"

# Credentials (production)
credentials:
  github:
    secret_ref:
      name: string
      key: string
  anthropic:
    secret_ref:
      name: string
      key: string
```

#### Task Examples

**Agentic Transform:**
```yaml
version: 1
id: slog-migration
title: "Migrate to structured logging"
mode: transform

repositories:
  - url: https://github.com/org/service-a.git
    setup: ["go mod download"]
  - url: https://github.com/org/service-b.git
    setup: ["go mod download"]

execution:
  agentic:
    prompt: |
      Migrate from log.Printf to slog package.
      - Replace all log.Printf calls with slog equivalents
      - Add context fields where appropriate
      - Ensure log levels are correct

    verifiers:
      - name: build
        command: ["go", "build", "./..."]
      - name: test
        command: ["go", "test", "./..."]

timeout: 30m
require_approval: true

pull_request:
  branch_prefix: "auto/slog-migration"
  title: "Migrate to structured logging (slog)"
  labels: ["automated", "logging"]
```

**Deterministic Transform:**
```yaml
version: 1
id: log4j-upgrade
title: "Upgrade Log4j 1.x to 2.x"
mode: transform

repositories:
  - url: https://github.com/org/java-service.git

execution:
  deterministic:
    image: openrewrite/rewrite:latest
    args:
      - "rewrite:run"
      - "-Drewrite.activeRecipes=org.openrewrite.java.logging.log4j.Log4j1ToLog4j2"

    verifiers:
      - name: build
        command: ["mvn", "compile"]

timeout: 20m
require_approval: false  # Pre-vetted deterministic transform

pull_request:
  branch_prefix: "security/log4j-upgrade"
  title: "Upgrade Log4j 1.x to 2.x"
```

**Report Mode:**
```yaml
version: 1
id: auth-audit
title: "Authentication security audit"
mode: report

repositories:
  - url: https://github.com/org/service-a.git
  - url: https://github.com/org/service-b.git

execution:
  agentic:
    prompt: |
      Analyze this repository's authentication implementation.

      Output your findings as a markdown document with:
      1. YAML frontmatter containing structured data (auth_library, score, issues)
      2. Detailed prose analysis with findings, rationale, and recommendations

    output:
      schema:  # Validates the frontmatter
        type: object
        required: ["auth_library", "score"]
        properties:
          auth_library:
            type: string
          score:
            type: integer
            minimum: 1
            maximum: 10
          issues:
            type: array
            items:
              type: object
              properties:
                severity:
                  type: string
                  enum: ["low", "medium", "high", "critical"]
                location:
                  type: string

timeout: 15m
```

**Example agent output (frontmatter + markdown):**
```markdown
---
auth_library: custom
score: 3
issues:
  - severity: high
    location: config/config.yaml:42
  - severity: medium
    location: pkg/auth/jwt.go:89
---

# Authentication Audit: service-b

## Summary

This service uses a **custom authentication implementation** with significant security concerns.
Overall security score: **3/10**

## Critical Findings

### 1. Hardcoded API Key (High Severity)

Found hardcoded API key in `config/config.yaml` at line 42:

\`\`\`yaml
api_key: "sk-1234567890abcdef"
\`\`\`

**Why this matters**: Hardcoded credentials in source control can be extracted by anyone
with repository access and are difficult to rotate after a breach.

**Recommendation**: Use environment variables or a secrets manager like HashiCorp Vault.

### 2. No Token Expiration (Medium Severity)

JWT tokens in `pkg/auth/jwt.go` are issued without an expiration claim...

## Recommendations

1. **Immediate**: Rotate the exposed API key
2. **Short-term**: Implement token expiration with 24-hour TTL
3. **Medium-term**: Add rate limiting to authentication endpoints
```

**Report Mode with forEach:**
```yaml
version: 1
id: api-endpoint-audit
title: "API endpoint assessment"
mode: report

repositories:
  - url: https://github.com/org/monolith.git

for_each:
  - name: users-api
    context: "Focus on src/api/users/"
  - name: orders-api
    context: "Focus on src/api/orders/"
  - name: payments-api
    context: "Focus on src/api/payments/"

execution:
  agentic:
    prompt: |
      {{.context}}

      Assess this API endpoint for:
      - Input validation completeness
      - Error handling patterns
      - Rate limiting implementation

      Output as markdown with YAML frontmatter for structured data.

    output:
      schema:
        type: object
        properties:
          endpoint:
            type: string
          input_validation:
            type: string
            enum: ["none", "partial", "complete"]
          has_rate_limiting:
            type: boolean
          issue_count:
            type: integer

timeout: 10m
```

**Transformation Repository with Multi-Target Analysis:**
```yaml
version: 1
id: endpoint-classification
title: "Classify endpoints for removal"
mode: report

# Transformation repo contains skills and tools
transformation:
  url: https://github.com/org/classification-tools.git
  branch: main
  setup:
    - npm install

# Target repos to analyze
targets:
  - url: https://github.com/org/api-server.git
    name: server
  - url: https://github.com/org/web-client.git
    name: client

for_each:
  - name: users-endpoint
    context: |
      Endpoint: GET /api/v1/users
      Location: targets/server/src/handlers/users.go:45

execution:
  agentic:
    prompt: |
      Use the endpoint-classification skill to analyze {{.Name}}.

      {{.Context}}

      Search for callers in:
      - /workspace/targets/server
      - /workspace/targets/client

    output:
      schema:
        type: object
        properties:
          classification:
            type: string
            enum: ["remove", "keep", "deprecate"]
          callers_found:
            type: integer

timeout: 30m
```

#### Grouped Execution with Failure Handling

For fleet-wide operations, use groups with failure thresholds:

```yaml
version: 1
id: fleet-migration
title: "Migrate logging across services"

groups:
  - name: team-a
    repositories:
      - url: https://github.com/org/service-1.git
      - url: https://github.com/org/service-2.git
  - name: team-b
    repositories:
      - url: https://github.com/org/service-3.git
  - name: team-c
    repositories:
      - url: https://github.com/org/service-4.git

execution:
  agentic:
    prompt: "Migrate to slog..."
    verifiers:
      - name: build
        command: ["go", "build", "./..."]

max_parallel: 3

failure:
  threshold_percent: 20  # Pause if >20% of groups fail
  action: pause          # "pause" (default) or "abort"
```

**Behavior:**
- Groups execute in parallel (up to `max_parallel`)
- After each group completes, check failure rate
- If threshold exceeded and `action: pause`, workflow pauses
- Human can: `continue` (resume), `continue --skip-remaining` (finish early), or `cancel`
- If `action: abort`, workflow immediately skips remaining groups

**Retry Failed Groups:**
```bash
# After a task completes with failures, retry only failed groups
fleetlift retry --file fleet-migration.yaml --workflow-id <original-id> --failed-only
```

### Go Types

The schemas map to internal Go types used by Temporal workflows:

```go
// Task is the input for the Task workflow
type Task struct {
    Version      int                `json:"version"`        // Schema version, e.g., 1
    ID           string             `json:"id"`
    Title        string             `json:"title"`
    Description  string             `json:"description,omitempty"`
    Mode         TaskMode           `json:"mode"`           // "transform" or "report"
    Repositories []RepositoryTarget `json:"repositories"`
    ForEach      []ForEachTarget    `json:"for_each,omitempty"`
    Execution    Execution          `json:"execution"`
    Timeout      time.Duration      `json:"timeout,omitempty"`
    RequireApproval bool            `json:"require_approval"`
    PullRequest  *PullRequestConfig `json:"pull_request,omitempty"`
    Sandbox      *SandboxConfig     `json:"sandbox,omitempty"`
}

type TaskMode string
const (
    TaskModeTransform TaskMode = "transform"
    TaskModeReport    TaskMode = "report"
)

type RepositoryTarget struct {
    URL    string   `json:"url"`
    Branch string   `json:"branch,omitempty"` // Default: "main"
    Name   string   `json:"name,omitempty"`   // Derived from URL if not set
    Setup  []string `json:"setup,omitempty"`
}

type ForEachTarget struct {
    Name    string `json:"name"`
    Context string `json:"context"`
}

type Execution struct {
    Agentic       *AgenticExecution       `json:"agentic,omitempty"`
    Deterministic *DeterministicExecution `json:"deterministic,omitempty"`
}

type AgenticExecution struct {
    Prompt    string        `json:"prompt"`
    Verifiers []Verifier    `json:"verifiers,omitempty"`
    Limits    *AgentLimits  `json:"limits,omitempty"`
    Output    *OutputConfig `json:"output,omitempty"` // For report mode
}

// OutputConfig defines how report mode output is captured and validated
type OutputConfig struct {
    Schema json.RawMessage `json:"schema,omitempty"` // JSON Schema for frontmatter validation
}

// ReportOutput represents the parsed output from a report-mode task
type ReportOutput struct {
    Frontmatter map[string]any `json:"frontmatter"` // Structured data (validated against schema)
    Body        string         `json:"body"`        // Markdown prose
    Raw         string         `json:"raw"`         // Original unparsed output
}

type DeterministicExecution struct {
    Image     string            `json:"image"`
    Command   []string          `json:"command,omitempty"`
    Args      []string          `json:"args,omitempty"`
    Env       map[string]string `json:"env,omitempty"`
    Verifiers []Verifier        `json:"verifiers,omitempty"`
}

type Verifier struct {
    Name    string   `json:"name"`
    Command []string `json:"command"`
}

type AgentLimits struct {
    MaxIterations      int `json:"max_iterations"`
    MaxTokens          int `json:"max_tokens"`
    MaxVerifierRetries int `json:"max_verifier_retries"`
}
```

### Knowledge Types

```go
// KnowledgeItem represents a reusable insight captured from a transformation execution.
type KnowledgeItem struct {
    ID          string          `json:"id" yaml:"id"`
    Type        KnowledgeType   `json:"type" yaml:"type"`
    Summary     string          `json:"summary" yaml:"summary"`
    Details     string          `json:"details" yaml:"details"`
    Source      KnowledgeSource `json:"source" yaml:"source"`
    Tags        []string        `json:"tags,omitempty" yaml:"tags,omitempty"`
    Confidence  float64         `json:"confidence" yaml:"confidence"`
    CreatedFrom *KnowledgeOrigin `json:"created_from,omitempty" yaml:"created_from,omitempty"`
    CreatedAt   time.Time       `json:"created_at" yaml:"created_at"`

    // Efficacy tracking (updated over time)
    TimesUsed          int     `json:"times_used,omitempty" yaml:"times_used,omitempty"`
    SuccessRate        float64 `json:"success_rate,omitempty" yaml:"success_rate,omitempty"`
    AvgSteeringRounds  float64 `json:"avg_steering_rounds,omitempty" yaml:"avg_steering_rounds,omitempty"`
}

type KnowledgeType string
const (
    KnowledgeTypePattern    KnowledgeType = "pattern"     // Reusable approach that worked
    KnowledgeTypeCorrection KnowledgeType = "correction"  // Extracted from steering corrections
    KnowledgeTypeGotcha     KnowledgeType = "gotcha"      // Non-obvious failure mode
    KnowledgeTypeContext    KnowledgeType = "context"      // Repo-specific knowledge
)

type KnowledgeSource string
const (
    KnowledgeSourceAutoCaptured      KnowledgeSource = "auto_captured"      // Extracted by Claude post-approval
    KnowledgeSourceSteeringExtracted KnowledgeSource = "steering_extracted" // Derived from steering corrections
    KnowledgeSourceManual            KnowledgeSource = "manual"             // Added by user via CLI
)

type KnowledgeOrigin struct {
    TaskID          string `json:"task_id" yaml:"task_id"`
    SteeringPrompt  string `json:"steering_prompt,omitempty" yaml:"steering_prompt,omitempty"`
    Iteration       int    `json:"iteration,omitempty" yaml:"iteration,omitempty"`
    Repository      string `json:"repository,omitempty" yaml:"repository,omitempty"`
}

// KnowledgeConfig is the optional knowledge section in a Task
type KnowledgeConfig struct {
    Capture  bool     `json:"capture" yaml:"capture"`     // Auto-capture after approval (default: true)
    Enrich   bool     `json:"enrich" yaml:"enrich"`       // Enrich prompt with past knowledge (default: true)
    MaxItems int      `json:"max_items,omitempty" yaml:"max_items,omitempty"` // Max items injected (default: 10)
    Tags     []string `json:"tags,omitempty" yaml:"tags,omitempty"`           // Additional tags for filtering
}
```

### Result Types

```go
type TaskResult struct {
    TaskID       string             `json:"task_id"`
    Status       TaskStatus         `json:"status"`
    Mode         TaskMode           `json:"mode"`
    Repositories []RepositoryResult `json:"repositories"`
    StartedAt    time.Time          `json:"started_at"`
    CompletedAt  *time.Time         `json:"completed_at,omitempty"`
    Error        *string            `json:"error,omitempty"`
}

type TaskStatus string
const (
    TaskStatusPending          TaskStatus = "pending"
    TaskStatusRunning          TaskStatus = "running"
    TaskStatusAwaitingApproval TaskStatus = "awaiting_approval"
    TaskStatusCompleted        TaskStatus = "completed"
    TaskStatusFailed           TaskStatus = "failed"
    TaskStatusCancelled        TaskStatus = "cancelled"
)

type RepositoryResult struct {
    Repository    string           `json:"repository"`
    Status        string           `json:"status"` // "success" | "failed" | "skipped"
    FilesModified []string         `json:"files_modified,omitempty"`
    PullRequest   *PullRequestInfo `json:"pull_request,omitempty"` // Transform mode
    Report        *ReportOutput    `json:"report,omitempty"`        // Report mode
    Error         *string          `json:"error,omitempty"`
}

type PullRequestInfo struct {
    URL    string `json:"url"`
    Number int    `json:"number"`
    Branch string `json:"branch"`
}

// Campaign result aggregates Task results
type CampaignResult struct {
    CampaignID   string       `json:"campaign_id"`
    Status       string       `json:"status"`
    TaskResults  []TaskResult `json:"task_results"`
    Summary      CampaignSummary `json:"summary"`
    StartedAt    time.Time    `json:"started_at"`
    CompletedAt  *time.Time   `json:"completed_at,omitempty"`
}

type CampaignSummary struct {
    TotalTasks     int `json:"total_tasks"`
    CompletedTasks int `json:"completed_tasks"`
    FailedTasks    int `json:"failed_tasks"`
    SkippedTasks   int `json:"skipped_tasks"`
    PRsCreated     int `json:"prs_created,omitempty"`     // Transform mode
    ReportsCollected int `json:"reports_collected,omitempty"` // Report mode
}
```

---

## Sandbox Provider Interface

Abstracts container runtime for local and production:

```go
type Provider interface {
    // Provision creates a new sandbox
    Provision(ctx context.Context, opts ProvisionOptions) (*Sandbox, error)

    // Exec runs a command in the sandbox
    Exec(ctx context.Context, id string, cmd ExecCommand) (*ExecResult, error)

    // CopyTo copies data into the sandbox
    CopyTo(ctx context.Context, id string, src io.Reader, destPath string) error

    // CopyFrom copies data from the sandbox
    CopyFrom(ctx context.Context, id string, srcPath string) (io.ReadCloser, error)

    // Status returns the current sandbox status
    Status(ctx context.Context, id string) (*SandboxStatus, error)

    // Cleanup destroys the sandbox
    Cleanup(ctx context.Context, id string) error

    // Name returns the provider name
    Name() string
}

type ProvisionOptions struct {
    TaskID     string
    Image      string
    WorkingDir string
    Env        map[string]string
    Resources  ResourceLimits
    Timeout    time.Duration
    Labels     map[string]string

    // Kubernetes-specific (ignored by Docker)
    Namespace      string
    ServiceAccount string
    NodeSelector   map[string]string
    RuntimeClass   string // gvisor, kata
}

type Sandbox struct {
    ID         string
    Provider   string // "docker" | "kubernetes"
    WorkingDir string
    Status     SandboxPhase
}

type ExecCommand struct {
    Command    []string
    WorkingDir string
    Env        map[string]string
    Stdin      io.Reader
    Timeout    time.Duration
}

type ExecResult struct {
    ExitCode int
    Stdout   string
    Stderr   string
}
```

### Docker Provider (Local Development)

For local development and testing:

```go
func NewDockerProvider() (Provider, error) {
    client, err := docker.NewClientWithOpts(docker.FromEnv)
    if err != nil {
        return nil, err
    }
    return &dockerProvider{client: client}, nil
}
```

Provisions containers with:
- Bind-mounted workspace directory
- Environment variables for credentials
- Configurable resource limits
- Automatic cleanup on completion

### Kubernetes Sandbox Provider (Production)

For production workloads, the Kubernetes provider uses a **controller pattern** for security and operational best practices. The worker creates a `SandboxRequest` custom resource, and a dedicated controller reconciles it into a Job.

#### Why a Controller?

| Direct API Access | Controller Pattern |
|-------------------|-------------------|
| Worker has broad K8s permissions | Worker only creates CRs |
| Harder to audit at K8s level | Kubernetes-native visibility |
| No policy enforcement point | Controller enforces invariants |
| Simpler architecture | Better security posture |

**Principle of Least Privilege**: The worker (which executes user-defined prompts) should have minimal Kubernetes API access. The controller, which runs trusted code, holds the elevated permissions.

#### SandboxRequest CRD

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: sandboxrequests.fleetlift.io
spec:
  group: fleetlift.io
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          required: ["spec"]
          properties:
            spec:
              type: object
              required: ["taskId", "image"]
              properties:
                taskId:
                  type: string
                image:
                  type: string
                workingDir:
                  type: string
                  default: "/workspace"
                env:
                  type: object
                  additionalProperties:
                    type: string
                resources:
                  type: object
                  properties:
                    limits:
                      type: object
                      properties:
                        memory:
                          type: string
                        cpu:
                          type: string
                runtimeClassName:
                  type: string
                nodeSelector:
                  type: object
                  additionalProperties:
                    type: string
                credentials:
                  type: array
                  items:
                    type: object
                    properties:
                      secretRef:
                        type: string
                      key:
                        type: string
                ttlAfterComplete:
                  type: integer
                  default: 3600
                execRequests:
                  type: array
                  description: "Queue of exec requests from worker"
                  items:
                    type: object
                    properties:
                      id:
                        type: string
                      command:
                        type: array
                        items:
                          type: string
                      workingDir:
                        type: string
            status:
              type: object
              properties:
                phase:
                  type: string
                  enum: ["Pending", "Provisioning", "Running", "Succeeded", "Failed"]
                podName:
                  type: string
                jobName:
                  type: string
                startTime:
                  type: string
                  format: date-time
                completionTime:
                  type: string
                  format: date-time
                execResults:
                  type: array
                  description: "Results of exec requests"
                  items:
                    type: object
                    properties:
                      id:
                        type: string
                      exitCode:
                        type: integer
                      stdout:
                        type: string
                      stderr:
                        type: string
                      error:
                        type: string
                message:
                  type: string
      subresources:
        status: {}
      additionalPrinterColumns:
        - name: Phase
          type: string
          jsonPath: .status.phase
        - name: Pod
          type: string
          jsonPath: .status.podName
        - name: Age
          type: date
          jsonPath: .metadata.creationTimestamp
  scope: Namespaced
  names:
    plural: sandboxrequests
    singular: sandboxrequest
    kind: SandboxRequest
    shortNames:
      - sbr
```

#### Example SandboxRequest

```yaml
apiVersion: fleetlift.io/v1alpha1
kind: SandboxRequest
metadata:
  name: task-abc123
  namespace: sandbox-isolated
spec:
  taskId: "abc123"
  image: "ghcr.io/org/claude-sandbox:latest"
  workingDir: "/workspace"
  resources:
    limits:
      memory: "4Gi"
      cpu: "2"
  credentials:
    - secretRef: github-credentials
      key: token
    - secretRef: claude-credentials
      key: api-key
  runtimeClassName: gvisor  # Optional
  nodeSelector:
    workload-type: sandbox
  ttlAfterComplete: 3600
status:
  phase: Running
  podName: task-abc123-xyz
  jobName: task-abc123
  startTime: "2024-01-15T10:00:00Z"
```

#### Sandbox Controller

The controller watches `SandboxRequest` resources and reconciles them:

```go
// Controller reconciliation loop
func (r *SandboxRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var sbr fleetliftv1alpha1.SandboxRequest
    if err := r.Get(ctx, req.NamespacedName, &sbr); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    switch sbr.Status.Phase {
    case "", "Pending":
        return r.handlePending(ctx, &sbr)
    case "Provisioning":
        return r.handleProvisioning(ctx, &sbr)
    case "Running":
        return r.handleRunning(ctx, &sbr)
    case "Succeeded", "Failed":
        return r.handleCompleted(ctx, &sbr)
    }
    return ctrl.Result{}, nil
}

func (r *SandboxRequestReconciler) handlePending(ctx context.Context, sbr *fleetliftv1alpha1.SandboxRequest) (ctrl.Result, error) {
    // 1. Validate request against policies
    if err := r.validateRequest(sbr); err != nil {
        return r.failRequest(ctx, sbr, err)
    }

    // 2. Create Job
    job := r.buildJob(sbr)
    if err := r.Create(ctx, job); err != nil {
        return ctrl.Result{}, err
    }

    // 3. Update status
    sbr.Status.Phase = "Provisioning"
    sbr.Status.JobName = job.Name
    return ctrl.Result{RequeueAfter: 5 * time.Second}, r.Status().Update(ctx, sbr)
}

func (r *SandboxRequestReconciler) handleRunning(ctx context.Context, sbr *fleetliftv1alpha1.SandboxRequest) (ctrl.Result, error) {
    // Process any pending exec requests
    for _, execReq := range sbr.Spec.ExecRequests {
        if r.hasResult(sbr, execReq.ID) {
            continue // Already processed
        }

        result, err := r.execInPod(ctx, sbr.Status.PodName, execReq)
        if err != nil {
            result = &ExecResult{ID: execReq.ID, Error: err.Error()}
        }

        sbr.Status.ExecResults = append(sbr.Status.ExecResults, *result)
    }

    return ctrl.Result{RequeueAfter: 2 * time.Second}, r.Status().Update(ctx, sbr)
}
```

#### Worker Provider Implementation

The worker's Kubernetes provider creates CRs instead of Jobs directly:

```go
type k8sProvider struct {
    client    client.Client
    namespace string
}

func (p *k8sProvider) Provision(ctx context.Context, opts ProvisionOptions) (*Sandbox, error) {
    sbr := &fleetliftv1alpha1.SandboxRequest{
        ObjectMeta: metav1.ObjectMeta{
            Name:      fmt.Sprintf("task-%s", opts.TaskID),
            Namespace: p.namespace,
        },
        Spec: fleetliftv1alpha1.SandboxRequestSpec{
            TaskID:           opts.TaskID,
            Image:            opts.Image,
            WorkingDir:       opts.WorkingDir,
            Env:              opts.Env,
            Resources:        convertResources(opts.Resources),
            RuntimeClassName: opts.RuntimeClass,
            NodeSelector:     opts.NodeSelector,
            Credentials:      opts.Credentials,
        },
    }

    if err := p.client.Create(ctx, sbr); err != nil {
        return nil, fmt.Errorf("failed to create SandboxRequest: %w", err)
    }

    // Wait for Running phase
    if err := p.waitForPhase(ctx, sbr.Name, "Running", opts.Timeout); err != nil {
        return nil, err
    }

    return &Sandbox{
        ID:         sbr.Name,
        Provider:   "kubernetes",
        WorkingDir: opts.WorkingDir,
        Status:     SandboxPhaseRunning,
    }, nil
}

func (p *k8sProvider) Exec(ctx context.Context, id string, cmd ExecCommand) (*ExecResult, error) {
    // 1. Add exec request to CR
    execID := uuid.New().String()
    patch := client.MergeFrom(&fleetliftv1alpha1.SandboxRequest{})

    sbr := &fleetliftv1alpha1.SandboxRequest{}
    if err := p.client.Get(ctx, client.ObjectKey{Name: id, Namespace: p.namespace}, sbr); err != nil {
        return nil, err
    }

    sbr.Spec.ExecRequests = append(sbr.Spec.ExecRequests, fleetliftv1alpha1.ExecRequest{
        ID:         execID,
        Command:    cmd.Command,
        WorkingDir: cmd.WorkingDir,
    })

    if err := p.client.Patch(ctx, sbr, patch); err != nil {
        return nil, err
    }

    // 2. Poll for result
    return p.waitForExecResult(ctx, id, execID, cmd.Timeout)
}

func (p *k8sProvider) Cleanup(ctx context.Context, id string) error {
    sbr := &fleetliftv1alpha1.SandboxRequest{
        ObjectMeta: metav1.ObjectMeta{
            Name:      id,
            Namespace: p.namespace,
        },
    }
    return client.IgnoreNotFound(p.client.Delete(ctx, sbr))
}
```

#### Job Specification (Created by Controller)

The controller creates Jobs based on the SandboxRequest:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: task-{{.TaskID}}
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/name: fleetlift-sandbox
    app.kubernetes.io/component: sandbox
    fleetlift.io/task-id: {{.TaskID}}
  ownerReferences:
    - apiVersion: fleetlift.io/v1alpha1
      kind: SandboxRequest
      name: {{.SandboxRequestName}}
      uid: {{.SandboxRequestUID}}
      controller: true
spec:
  ttlSecondsAfterFinished: 3600
  backoffLimit: 0  # No retries - Temporal handles retry logic
  template:
    spec:
      runtimeClassName: {{.RuntimeClass}}  # Optional: gvisor
      serviceAccountName: sandbox-runner
      nodeSelector:
        {{range $k, $v := .NodeSelector}}
        {{$k}}: {{$v}}
        {{end}}
      containers:
      - name: sandbox
        image: {{.Image}}
        command: ["sleep", "infinity"]  # Keep alive for exec
        workingDir: {{.WorkingDir}}
        resources:
          limits:
            memory: {{.Resources.Memory}}
            cpu: {{.Resources.CPU}}
        env:
        - name: ANTHROPIC_API_KEY
          valueFrom:
            secretKeyRef:
              name: claude-credentials
              key: api-key
        - name: GITHUB_TOKEN
          valueFrom:
            secretKeyRef:
              name: github-credentials
              key: token
      restartPolicy: Never
```

#### Provider Selection

```go
func NewProvider() (Provider, error) {
    switch os.Getenv("SANDBOX_PROVIDER") {
    case "kubernetes":
        // Uses controller pattern - worker only creates CRs
        return kubernetes.NewProvider()
    default:
        // Direct Docker API access for local development
        return docker.NewProvider()
    }
}
```

### Security (Production)

#### RBAC - Split Permissions Model

The controller pattern enables least-privilege access:

| Component | Permissions | Rationale |
|-----------|-------------|-----------|
| **Worker** | Create/get/delete SandboxRequest CRs | Minimal access; cannot directly create Jobs or exec |
| **Controller** | Create/delete Jobs, exec into pods, read secrets | Elevated access in trusted, auditable code |
| **Sandbox Pod** | None | No K8s API access (empty RBAC) |

**Worker ServiceAccount RBAC:**

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: fleetlift-worker
  namespace: sandbox-isolated
rules:
  # Create and manage SandboxRequest CRs only
  - apiGroups: ["fleetlift.io"]
    resources: ["sandboxrequests"]
    verbs: ["create", "get", "list", "watch", "patch", "delete"]
  - apiGroups: ["fleetlift.io"]
    resources: ["sandboxrequests/status"]
    verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: fleetlift-worker
  namespace: sandbox-isolated
subjects:
  - kind: ServiceAccount
    name: fleetlift-worker
    namespace: fleetlift-system
roleRef:
  kind: Role
  name: fleetlift-worker
  apiGroup: rbac.authorization.k8s.io
```

**Controller ServiceAccount RBAC:**

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: fleetlift-controller
  namespace: sandbox-isolated
rules:
  # Manage SandboxRequest CRs
  - apiGroups: ["fleetlift.io"]
    resources: ["sandboxrequests"]
    verbs: ["get", "list", "watch", "update", "patch"]
  - apiGroups: ["fleetlift.io"]
    resources: ["sandboxrequests/status"]
    verbs: ["get", "update", "patch"]

  # Create and manage Jobs
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["create", "get", "list", "watch", "delete"]

  # Find and monitor pods
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch"]

  # Execute commands in sandbox pods
  - apiGroups: [""]
    resources: ["pods/exec"]
    verbs: ["create"]

  # Read pod logs for diagnostics
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]

  # Read credentials secrets
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
    resourceNames: ["github-credentials", "claude-credentials"]

  # Create events for observability
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: fleetlift-controller
  namespace: sandbox-isolated
subjects:
  - kind: ServiceAccount
    name: fleetlift-controller
    namespace: fleetlift-system
roleRef:
  kind: Role
  name: fleetlift-controller
  apiGroup: rbac.authorization.k8s.io
```

**Sandbox ServiceAccount (Empty RBAC):**

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: sandbox-runner
  namespace: sandbox-isolated
# No RoleBinding - sandbox pods have zero K8s API access
```

#### Policy Enforcement

The controller can enforce policies that the worker cannot bypass:

```go
func (r *SandboxRequestReconciler) validateRequest(sbr *fleetliftv1alpha1.SandboxRequest) error {
    // Enforce gVisor for all sandboxes
    if r.config.RequireGVisor && sbr.Spec.RuntimeClassName != "gvisor" {
        return fmt.Errorf("gVisor runtime is required")
    }

    // Enforce resource limits
    if memory := sbr.Spec.Resources.Limits.Memory; memory != "" {
        if qty, _ := resource.ParseQuantity(memory); qty.Cmp(r.config.MaxMemory) > 0 {
            return fmt.Errorf("memory limit %s exceeds maximum %s", memory, r.config.MaxMemory)
        }
    }

    // Enforce allowed images
    if !r.isAllowedImage(sbr.Spec.Image) {
        return fmt.Errorf("image %s is not in allowed list", sbr.Spec.Image)
    }

    return nil
}
```

#### Network Policy

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: sandbox-egress
  namespace: sandbox-isolated
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/component: sandbox
  policyTypes:
  - Egress
  - Ingress
  ingress: []  # Deny all ingress
  egress:
  - to:
    - ipBlock:
        cidr: 0.0.0.0/0
    ports:
    - protocol: TCP
      port: 443  # HTTPS only
```

#### Resource Limits

```yaml
resources:
  limits:
    memory: "4Gi"
    cpu: "2"
  requests:
    memory: "2Gi"
    cpu: "1"
```

---

## Temporal Workflows

### Task Workflow

The Task workflow executes a single Task across one or more repositories:

```
┌─────────────────────────────────────────────────────────────────┐
│                       Task Workflow                              │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   1. Provision Sandbox                                          │
│         │                                                        │
│         ▼                                                        │
│   2. For each repository:                                        │
│         │                                                        │
│         ├──▶ Clone Repository                                   │
│         │         │                                              │
│         │         ▼                                              │
│         │    Run Setup Commands                                  │
│         │         │                                              │
│         │         ▼                                              │
│         │    Execute Transformation                              │
│         │    (deterministic or agentic)                          │
│         │         │                                              │
│         │         ▼                                              │
│         │    Run Verifiers (final gate)                          │
│         │         │                                              │
│         └──▶ Collect Result                                     │
│         │                                                        │
│         ▼                                                        │
│   3. Mode == transform?                                          │
│         │                                                        │
│         ├── Yes: Await Approval ──▶ Create PRs                  │
│         │                                                        │
│         └── No (report): Collect Reports                        │
│         │                                                        │
│         ▼                                                        │
│   4. Cleanup Sandbox                                             │
│         │                                                        │
│         ▼                                                        │
│   5. Return TaskResult                                           │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**Workflow Pseudocode:**

```go
func TaskWorkflow(ctx workflow.Context, task Task) (*TaskResult, error) {
    result := &TaskResult{
        TaskID:    task.ID,
        Mode:      task.Mode,
        Status:    TaskStatusRunning,
        StartedAt: workflow.Now(ctx),
    }

    // 1. Provision sandbox
    sandbox, err := workflow.ExecuteActivity(ctx, ProvisionSandbox, task).Get(ctx, nil)
    if err != nil {
        return failedResult(task.ID, err), nil
    }
    defer workflow.ExecuteActivity(ctx, CleanupSandbox, sandbox)

    // 2. Process each repository
    for _, repo := range task.Repositories {
        repoResult := processRepository(ctx, task, sandbox, repo)
        result.Repositories = append(result.Repositories, repoResult)
    }

    // 3. Mode-specific handling
    if task.Mode == TaskModeTransform && task.RequireApproval {
        // Wait for human approval
        result.Status = TaskStatusAwaitingApproval
        approved := awaitApproval(ctx, task.ID)
        if !approved {
            result.Status = TaskStatusCancelled
            return result, nil
        }

        // Create PRs
        for i, repoResult := range result.Repositories {
            if repoResult.Status == "success" {
                pr := workflow.ExecuteActivity(ctx, CreatePullRequest, sandbox, task, repoResult).Get(ctx, nil)
                result.Repositories[i].PullRequest = pr
            }
        }
    }

    result.Status = determineOverallStatus(result.Repositories)
    now := workflow.Now(ctx)
    result.CompletedAt = &now

    return result, nil
}
```

**Activities:**

| Activity | Description |
|----------|-------------|
| `ProvisionSandbox` | Create Docker container or K8s Job |
| `CloneRepository` | Git clone with branch and depth options |
| `RunSetup` | Execute setup commands (go mod download, npm install) |
| `ExecuteAgentic` | Run Claude Code CLI with prompt |
| `ExecuteDeterministic` | Run deterministic command in sandbox |
| `RunVerifiers` | Execute verifier commands as final gate |
| `CreatePullRequest` | Create branch, push, open PR via GitHub API |
| `CleanupSandbox` | Destroy container/Job |

**Signal Handling:**

```go
// Approval signals
workflow.GetSignalChannel(ctx, "approve").Receive(ctx, nil)
workflow.GetSignalChannel(ctx, "reject").Receive(ctx, nil)
workflow.GetSignalChannel(ctx, "cancel").Receive(ctx, nil)

// Steering signal (iterative HITL)
var steerPrompt string
workflow.GetSignalChannel(ctx, "steer").Receive(ctx, &steerPrompt)
```

### Campaign Workflow

The Campaign workflow orchestrates multiple Tasks:

```
┌─────────────────────────────────────────────────────────────────┐
│                     Campaign Workflow                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   1. Initialize                                                  │
│         │                                                        │
│         ▼                                                        │
│   2. For each phase (or single phase):                          │
│         │                                                        │
│         ├──▶ For each batch:                                    │
│         │         │                                              │
│         │         ├── Submit Tasks (parallel within batch)      │
│         │         │         │                                    │
│         │         │         ▼                                    │
│         │         │    Monitor Progress                          │
│         │         │         │                                    │
│         │         │         ▼                                    │
│         │         │    Check Failure Threshold                   │
│         │         │         │                                    │
│         │         │         ├── Under threshold: Continue       │
│         │         │         │                                    │
│         │         │         └── Over threshold: Pause            │
│         │         │                   │                          │
│         │         │                   ▼                          │
│         │         │              Await Human Decision            │
│         │         │              (abort/continue/retry)          │
│         │         │                                              │
│         │         └── Collect batch results                     │
│         │                                                        │
│         └──▶ Aggregate batch results                            │
│         │                                                        │
│         ▼                                                        │
│   3. Return CampaignResult                                       │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**Workflow Pseudocode:**

```go
func CampaignWorkflow(ctx workflow.Context, campaign Campaign) (*CampaignResult, error) {
    result := &CampaignResult{
        CampaignID: campaign.ID,
        Status:     "running",
        StartedAt:  workflow.Now(ctx),
    }

    repos := campaign.Repositories
    batchSize := campaign.Batch.Size

    for i := 0; i < len(repos); i += batchSize {
        batch := repos[i:min(i+batchSize, len(repos))]

        // Submit Tasks for batch (as child workflows)
        var futures []workflow.Future
        for _, repo := range batch {
            task := createTaskFromTemplate(campaign.TaskTemplate, repo)
            future := workflow.ExecuteChildWorkflow(ctx, TaskWorkflow, task)
            futures = append(futures, future)
        }

        // Wait for batch completion
        for _, future := range futures {
            var taskResult TaskResult
            future.Get(ctx, &taskResult)
            result.TaskResults = append(result.TaskResults, taskResult)
        }

        // Check failure threshold
        failureRate := calculateFailureRate(result.TaskResults)
        if failureRate > campaign.Failure.ThresholdPercent {
            result.Status = "paused"

            // Wait for human decision
            decision := awaitDecision(ctx)
            switch decision {
            case "abort":
                result.Status = "aborted"
                return result, nil
            case "retry":
                // Re-queue failed Tasks
                i -= batchSize
            case "continue":
                // Continue to next batch
            }
        }
    }

    result.Status = "completed"
    result.Summary = calculateSummary(result.TaskResults)
    now := workflow.Now(ctx)
    result.CompletedAt = &now

    return result, nil
}
```

**Batch Failure Semantics:**

| Scenario | Behavior |
|----------|----------|
| All succeed | Campaign completes, PRs created |
| Some fail (<threshold) | Continue, report failures at end |
| Many fail (≥threshold) | Pause and ask human: abort / continue / retry |
| Critical failure | Halt immediately, notify human |

### Knowledge Capture & Enrichment

Knowledge capture and prompt enrichment are integrated into the Task workflow as optional steps:

```
┌─────────────────────────────────────────────────────────────────┐
│             Knowledge-Enhanced Task Workflow (additions)          │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Before execution:                                             │
│   ┌──────────────────────────────────────────────────────┐      │
│   │  EnrichPrompt Activity                                │      │
│   │  1. Load items from .fleetlift/knowledge/ (Tier 3)   │      │
│   │  2. Load items from ~/.fleetlift/knowledge/ (Tier 2)  │      │
│   │  3. Filter by tags, type, confidence                  │      │
│   │  4. Append "Lessons from previous runs" to prompt     │      │
│   └──────────────────────────────────────────────────────┘      │
│         │                                                        │
│         ▼                                                        │
│   [ Normal execution: RunClaudeCode → Verifiers → Approval ]    │
│         │                                                        │
│         ▼                                                        │
│   After approval (non-blocking):                                │
│   ┌──────────────────────────────────────────────────────┐      │
│   │  CaptureKnowledge Activity                            │      │
│   │  1. Collect: prompt, steering history, diff, results  │      │
│   │  2. Ask Claude to extract knowledge items             │      │
│   │  3. Write items to ~/.fleetlift/knowledge/ (Tier 2)   │      │
│   │  4. Log: "N items captured. Run knowledge review."    │      │
│   └──────────────────────────────────────────────────────┘      │
│         │                                                        │
│         ▼                                                        │
│   [ Normal continuation: Create PRs → Cleanup ]                 │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**Activities:**

| Activity | Input | Output | Failure behavior |
|----------|-------|--------|------------------|
| `EnrichPrompt` | Original prompt, task tags, transformation repo path | Enriched prompt with knowledge section | Non-fatal: returns original prompt on failure |
| `CaptureKnowledge` | Prompt, steering history, diff stats, verifier results | List of KnowledgeItems written to Tier 2 | Non-fatal: logged as warning, does not block PR creation |

**Knowledge Curation Flow (CLI-only, no Temporal):**

```
fleetlift knowledge review
    │
    ├── Show auto-captured items from Tier 2
    │   User: keep / discard / edit each item
    │
    ▼
fleetlift knowledge commit --repo ./my-transform-repo
    │
    ├── Copy reviewed items to .fleetlift/knowledge/items/
    │   (in the transformation repo)
    │
    ▼
git add / commit / push
    (standard git workflow — knowledge is version-controlled)
```

---

## Report Output Collection

For report mode, the orchestrator must collect the agent's output artifact from the sandbox.

### Collection Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                    Report Output Collection                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   1. Agent executes in sandbox                                  │
│         │                                                        │
│         ▼                                                        │
│   2. Agent writes report to well-known path                     │
│      /workspace/REPORT.md                                       │
│         │                                                        │
│         ▼                                                        │
│   3. Orchestrator reads file from sandbox                       │
│      sandbox.CopyFrom("/workspace/REPORT.md")                   │
│         │                                                        │
│         ▼                                                        │
│   4. Parse frontmatter from markdown                            │
│      Split on "---" delimiters, parse YAML                      │
│         │                                                        │
│         ▼                                                        │
│   5. Validate frontmatter against schema (if provided)          │
│         │                                                        │
│         ▼                                                        │
│   6. Store in RepositoryResult.Report                           │
│      {frontmatter: {...}, body: "...", raw: "..."}              │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Prompt Convention

For report mode, the orchestrator appends instructions to the prompt:

```
Write your report to /workspace/REPORT.md with:
- YAML frontmatter between --- delimiters containing structured data
- Markdown body with detailed analysis

Example format:
---
key: value
items:
  - item1
  - item2
---

# Report Title

Your detailed analysis here...
```

### Collection Activity

```go
func CollectReport(ctx context.Context, sandbox Sandbox, schema json.RawMessage) (*ReportOutput, error) {
    // 1. Read report file from sandbox
    content, err := sandbox.CopyFrom(ctx, "/workspace/REPORT.md")
    if err != nil {
        return nil, fmt.Errorf("failed to read report: %w", err)
    }

    // 2. Parse frontmatter
    report, err := ParseFrontmatter(content)
    if err != nil {
        return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
    }

    // 3. Validate against schema (if provided)
    if schema != nil {
        if err := ValidateJSON(report.Frontmatter, schema); err != nil {
            return nil, fmt.Errorf("frontmatter validation failed: %w", err)
        }
    }

    return report, nil
}

func ParseFrontmatter(content string) (*ReportOutput, error) {
    // Split content on "---" delimiters
    // First block after opening "---" is YAML frontmatter
    // Rest is markdown body

    parts := strings.SplitN(content, "---", 3)
    if len(parts) < 3 {
        // No frontmatter, treat entire content as body
        return &ReportOutput{
            Frontmatter: nil,
            Body:        content,
            Raw:         content,
        }, nil
    }

    var frontmatter map[string]any
    if err := yaml.Unmarshal([]byte(parts[1]), &frontmatter); err != nil {
        return nil, err
    }

    return &ReportOutput{
        Frontmatter: frontmatter,
        Body:        strings.TrimSpace(parts[2]),
        Raw:         content,
    }, nil
}
```

### Kubernetes Job Collection

For K8s Jobs, the file is read via the exec API before the pod terminates:

```go
func (p *k8sProvider) CopyFrom(ctx context.Context, sandboxID, path string) (string, error) {
    // Get pod name from Job
    pod, err := p.getPodForJob(ctx, sandboxID)
    if err != nil {
        return "", err
    }

    // Exec "cat" to read file content
    result, err := p.Exec(ctx, sandboxID, ExecCommand{
        Command: []string{"cat", path},
    })
    if err != nil {
        return "", err
    }

    return result.Stdout, nil
}
```

### Alternative: Structured Output via Stdout

For simpler cases, the agent can output directly to stdout and the orchestrator captures it:

```yaml
execution:
  agentic:
    prompt: "Analyze and output your report..."
    output:
      capture: stdout  # Alternative to file-based collection
      schema: {...}
```

The default (`capture: file`) writes to `/workspace/REPORT.md`. The `capture: stdout` option captures the agent's final output instead.

---

## Verifiers

Verifiers validate transformations before PR creation. For agentic transforms, they serve two purposes:

1. **Agent guidance**: Appended to the prompt so the agent runs them and iterates
2. **Final gate**: Run by the orchestrator as a hard check before PR

### Prompt Injection Pattern

The orchestrator appends verifier instructions to the prompt:

```
After making changes, verify your work by running these commands:
- build: go build ./...
- test: go test ./...
- lint: golangci-lint run

Fix any errors before completing the task.
```

The agent runs these using its Bash tool, sees output, and iterates until all pass.

### Final Gate Validation

Even after the agent reports success, the fleetlift runs verifiers as a final check:

```go
func RunVerifiers(ctx context.Context, sandbox Sandbox, verifiers []Verifier) (*VerifiersResult, error) {
    result := &VerifiersResult{AllPassed: true}

    for _, v := range verifiers {
        execResult, err := sandbox.Exec(ctx, ExecCommand{Command: v.Command})
        if err != nil || execResult.ExitCode != 0 {
            result.AllPassed = false
        }
        result.Results = append(result.Results, VerifierResult{
            Name:     v.Name,
            Success:  execResult.ExitCode == 0,
            ExitCode: execResult.ExitCode,
            Output:   execResult.Stdout + execResult.Stderr,
        })
    }

    return result, nil
}
```

---

## Failure Handling

### Agentic Failure Modes

| Failure Mode | Detection | Response |
|--------------|-----------|----------|
| **Agent stuck in loop** | Iteration count > `max_iterations` | Terminate, preserve partial work |
| **Token budget exceeded** | Token counter > `max_tokens` | Terminate, report what was accomplished |
| **Verifiers keep failing** | Retry count > `max_verifier_retries` | Stop iteration, ask human for guidance |
| **Agent refuses (safety)** | Specific error patterns | Report refusal reason |
| **Timeout** | Wall clock > `timeout` | Terminate sandbox, report timeout |

### Graceful Degradation

When a transform fails:

1. **Preserve partial work**: Commit changes to a WIP branch
2. **Capture diagnostics**: Save conversation history, verifier output
3. **Enable recovery**: Allow human to:
   - Steer ("try a different approach")
   - Approve partial changes ("good enough")
   - Abort and discard

### Stuck Detection

```go
// If no new file changes in N iterations, consider stuck
if iterationsSinceLastChange > 3 {
    return StuckError("No progress after 3 iterations")
}
```

### Report Mode Failure Handling

Report mode has unique failure scenarios since there's no PR creation or verifier loop:

| Failure Mode | Detection | Response |
|--------------|-----------|----------|
| **Report file missing** | `/workspace/REPORT.md` doesn't exist | Fail task, include agent stdout as diagnostic |
| **Frontmatter parse error** | YAML between `---` delimiters is invalid | Fail task, return raw content for debugging |
| **Schema validation failed** | Frontmatter doesn't match `output.schema` | Fail task, return validation errors and raw content |
| **Empty report** | File exists but is empty or trivial | Warning (not failure), flag for human review |
| **Timeout** | Wall clock > `timeout` | Terminate sandbox, return partial output if available |

**Recovery Flow:**

```go
func CollectReport(ctx context.Context, sandbox Sandbox, schema json.RawMessage) (*ReportOutput, error) {
    // 1. Attempt to read report file
    content, err := sandbox.CopyFrom(ctx, "/workspace/REPORT.md")
    if err != nil {
        // File missing - capture agent stdout as fallback diagnostic
        return &ReportOutput{
            Raw:   "",
            Error: fmt.Sprintf("Report file not found: %v", err),
        }, ErrReportMissing
    }

    // 2. Parse frontmatter
    report, err := ParseFrontmatter(content)
    if err != nil {
        // Return raw content so human can see what agent produced
        return &ReportOutput{
            Raw:   content,
            Error: fmt.Sprintf("Frontmatter parse error: %v", err),
        }, ErrFrontmatterInvalid
    }

    // 3. Validate against schema (if provided)
    if schema != nil {
        if validationErrs := ValidateJSON(report.Frontmatter, schema); len(validationErrs) > 0 {
            report.ValidationErrors = validationErrs
            return report, ErrSchemaValidation
        }
    }

    return report, nil
}
```

**Error Types:**

```go
var (
    ErrReportMissing      = errors.New("report file not found")
    ErrFrontmatterInvalid = errors.New("frontmatter parse failed")
    ErrSchemaValidation   = errors.New("frontmatter schema validation failed")
)

// ReportOutput extended for error cases
type ReportOutput struct {
    Frontmatter      map[string]any `json:"frontmatter,omitempty"`
    Body             string         `json:"body,omitempty"`
    Raw              string         `json:"raw"`
    Error            string         `json:"error,omitempty"`
    ValidationErrors []string       `json:"validation_errors,omitempty"`
}
```

**Campaign Behavior:**

When a report-mode task fails within a Campaign:
- The failure counts toward the Campaign's failure threshold
- The raw output (if any) is preserved in the result for debugging
- Human can decide to: skip the repository, retry with steering prompt, or abort

---

## Rate Limiting & Cost Control

### GitHub API Limits

| Limit | Value | Mitigation |
|-------|-------|------------|
| REST API | 5,000 req/hr per token | Queue requests, use conditional requests |
| Git operations | Varies by plan | Batch clones, use shallow clones |
| PR creation | No hard limit | Self-imposed limit per Campaign |

### Claude API Limits

| Concern | Mitigation |
|---------|------------|
| Tokens per minute | Configurable delay between transforms |
| Cost per transform | Token budget per Task (`max_tokens`) |
| Runaway costs | Campaign-level budget with hard stop |

### Configuration

```yaml
rate_limits:
  github:
    requests_per_hour: 4000      # Leave headroom
    parallel_clones: 5           # Max concurrent git operations
  claude:
    tokens_per_minute: 100000    # Anthropic tier limit
    max_cost_per_task: 5.00      # USD, terminate if exceeded
    max_cost_per_campaign: 500.00 # USD, pause Campaign if exceeded
```

### Cost Attribution

- Track token usage per Task
- Attribute to team/namespace for chargeback
- Surface in observability metrics (`codetransform_cost_usd` gauge)

---

## Credential Handling

### Local (Docker)

Credentials are passed via environment variables:

```bash
export GITHUB_TOKEN=ghp_xxxx
export ANTHROPIC_API_KEY=sk-ant-xxxx

fleetlift run --file task.yaml
```

### Production (Kubernetes)

Credentials are stored as Kubernetes Secrets and referenced in the Task:

```yaml
credentials:
  github:
    secret_ref:
      name: github-credentials
      key: token
  anthropic:
    secret_ref:
      name: claude-credentials
      key: api-key
```

**Credential Flow:**
1. Worker reads secret references from Task
2. Worker mounts secrets into sandbox pod as environment variables
3. Sandbox uses credentials for git operations and API calls
4. Cleanup ensures credentials are not persisted

**Security Considerations:**
- Secrets are namespace-scoped
- Sandbox pods use dedicated ServiceAccount with minimal RBAC
- Audit logging tracks credential access
- Consider [External Secrets Operator](https://external-secrets.io/) for centralized management

---

## Directory Structure

```
fleetlift/
├── cmd/
│   ├── worker/              # Temporal worker
│   │   └── main.go
│   ├── controller/          # Kubernetes sandbox controller
│   │   └── main.go
│   └── cli/                 # CLI tool
│       └── main.go
├── api/
│   └── v1alpha1/            # CRD API types
│       ├── sandboxrequest_types.go
│       ├── groupversion_info.go
│       └── zz_generated.deepcopy.go
├── internal/
│   ├── model/               # Data models
│   │   ├── task.go          # Task, Campaign types
│   │   └── result.go        # Result types
│   ├── workflow/            # Temporal workflows
│   │   ├── task.go          # Task workflow
│   │   ├── campaign.go      # Campaign workflow
│   │   └── signals.go       # Signal handlers
│   ├── activity/            # Temporal activities
│   │   ├── sandbox.go       # Provision, Cleanup
│   │   ├── transform.go     # Execute transformations
│   │   ├── git.go           # Clone, branch, push
│   │   └── github.go        # PR creation
│   ├── sandbox/             # Sandbox abstraction
│   │   ├── provider.go      # Interface
│   │   ├── docker/          # Docker implementation (direct)
│   │   └── kubernetes/      # K8s implementation (via CRs)
│   ├── controller/          # Sandbox controller logic
│   │   ├── sandboxrequest_controller.go
│   │   ├── job_builder.go
│   │   └── exec_handler.go
│   ├── knowledge/           # Continual learning system
│   │   ├── store.go         # Three-tier storage (local + repo)
│   │   ├── capture.go       # Post-approval knowledge extraction
│   │   ├── enrich.go        # Prompt enrichment with relevant items
│   │   ├── item.go          # KnowledgeItem model and parsing
│   │   └── efficacy.go      # Usage tracking and auto-deprecation
│   ├── create/              # Natural language task creation
│   │   ├── create.go        # Interactive + one-shot creation flow
│   │   ├── discover.go      # GitHub repo discovery via gh CLI
│   │   ├── schema.go        # Embedded schema + examples bundle
│   │   ├── templates.go     # Built-in and custom task templates
│   │   └── registry.go      # Transformation repo registry
│   └── config/              # Configuration loading
├── config/
│   ├── local.yaml
│   ├── production.yaml
│   ├── crd/                 # CRD manifests
│   │   └── fleetlift.io_sandboxrequests.yaml
│   ├── rbac/                # RBAC manifests
│   │   ├── worker_role.yaml
│   │   ├── controller_role.yaml
│   │   └── sandbox_serviceaccount.yaml
│   └── controller/          # Controller deployment
│       └── manager.yaml
├── docker/
│   ├── Dockerfile.sandbox
│   ├── Dockerfile.worker
│   └── Dockerfile.controller
└── docs/
    ├── OVERVIEW.md          # User-facing documentation
    ├── DESIGN.md            # This document
    └── IMPLEMENTATION_PLAN.md
```

---

## Key Architectural Decisions

1. **CLI as primary interface**: Users submit Tasks via CLI or YAML files. The optional K8s operator is a convenience layer, not the primary interface.

2. **Temporal for durability**: Temporal handles retries, timeouts, and human-in-the-loop signals. All state is durable and recoverable.

3. **Controller for Kubernetes sandboxes**: Instead of giving the worker direct K8s API access, we use a controller pattern:
   - Worker creates `SandboxRequest` custom resources (minimal permissions)
   - Dedicated controller reconciles CRs into Jobs (elevated permissions in trusted code)
   - Benefits: least privilege, policy enforcement, Kubernetes-native observability
   - The controller is the only component with permissions to create Jobs, exec into pods, and read secrets

4. **Two transform modes**: Deterministic (commands in sandbox) and Agentic (Claude Code) share the same orchestration but have different execution paths. Both use the same sidecar agent (`fleetlift-agent serve`). For deterministic, the sandbox image contains the required tools and the agent binary is injected via init container.

5. **Agent-agnostic design**: The platform's value is in the orchestration layer. Claude Code is the current agent, but the interface (`exec(prompt) → code changes`) allows for other agents:

   ```
   Platform (durable)          Agent (swappable)
   ─────────────────           ─────────────────
   • Temporal workflows        • Claude Code CLI (today)
   • Human-in-the-loop         • Other agents (future)
   • Multi-repo coordination   • Interface: exec(prompt) → changes
   • PR creation               • Verifiers validate regardless of agent
   • Cost/rate limiting
   • Audit trail
   ```

6. **Campaign as first-class**: Campaigns orchestrate Tasks at scale with batch execution and failure thresholds. This is core functionality, not a future capability.

7. **Report mode integrated**: Report mode (discovery, audits) is a core mode alongside transform mode, not an afterthought.

8. **Provider abstraction**: The sandbox provider interface abstracts the difference between local (Docker) and production (Kubernetes) environments. Local development uses Docker directly for simplicity; production uses the controller pattern for security.

9. **Transparent continual learning**: Knowledge items are YAML files stored in transformation repos, not an opaque model. This means knowledge is version-controlled, auditable, editable, and team-shareable. The three-tier storage (execution log → local store → transformation repo) balances automatic capture with human curation. Knowledge is injected as a prompt section, not hidden context, so it's visible and debuggable.

10. **Productionisation: Orphaned sandbox reaper**: Worker crash between `ProvisionSandbox` and `CleanupSandbox` leaks containers/pods. A reaper process (periodic goroutine or sidecar) should label sandboxes with workflow IDs and clean up any whose workflow has reached a terminal state. Low priority for local Docker dev; required for K8s production.

11. **Productionisation: Backpressure and resource awareness**: No mechanism currently limits concurrent tasks across the system (`max_parallel` only applies within a single task's groups). Use Temporal's `MaxConcurrentActivityExecutionSize` worker option to bound concurrent sandbox provisioning. For K8s, combine with `ResourceQuota` per sandbox namespace and cluster autoscaler awareness to prevent overcommit.

12. **YAML as the artifact for NL task creation**: The `fleetlift create` flow always outputs a YAML file, never executes directly from memory. This preserves transparency (the user sees exactly what will run), reproducibility (the YAML can be re-run, committed, or shared), and editability (the user can tweak the generated YAML before running). The schema + examples are embedded in the CLI binary to avoid network dependencies.
