# Task File Reference

Complete YAML specification for task files.

## Overview

Task files define what the orchestrator should do. They specify:
- Which repositories to target
- What changes or analysis to perform
- How to validate results
- Whether to create pull requests

## Minimal Example

```yaml
version: 1
id: my-task
title: "My task description"

repositories:
  - url: https://github.com/org/repo.git

execution:
  agentic:
    prompt: "Do something"
```

## Required Fields

### version
```yaml
version: 1  # Always 1 (schema version)
```

### id
```yaml
id: unique-task-id  # Unique identifier, alphanumeric with hyphens
```

Becomes workflow ID: `transform-unique-task-id`

### title
```yaml
title: "Human-readable task description"
```

Displayed in UI and workflow listings.

## Mode Selection

### mode
```yaml
mode: transform  # Default: creates PRs
# OR
mode: report     # Discovery: collects data, no PRs
```

**Transform mode:**
- Makes code changes
- Creates pull requests
- Waits for approval (if configured)

**Report mode:**
- Analyzes code
- Collects structured data
- No pull requests
- No code changes

## Repository Configuration

### Simple Pattern (Legacy)

```yaml
repositories:
  - url: https://github.com/org/repo.git
    branch: main             # Optional, defaults to "main"
    name: repo               # Optional, derived from URL
    setup:                   # Optional, run after clone
      - npm install
      - go mod download
```

### Transformation Repository Pattern

```yaml
transformation:
  url: https://github.com/org/toolkit.git
  branch: main
  setup:
    - npm install

targets:
  - url: https://github.com/org/service-a.git
    name: service-a
  - url: https://github.com/org/service-b.git
    name: service-b
```

**Workspace layout:**
```
/workspace/              # Transformation repo
└── targets/
    ├── service-a/       # Target repos
    └── service-b/
```

### Setup Commands

```yaml
repositories:
  - url: https://github.com/org/repo.git
    setup:
      # Install dependencies
      - npm install
      - go mod download

      # Build tools
      - make build-tools

      # Run initialization
      - ./scripts/init.sh
```

Runs after cloning, before execution.

## Execution Configuration

### Agentic Mode (Claude Code)

```yaml
execution:
  agentic:
    prompt: |
      Your instructions for Claude Code.

      Be specific about what to change and how to validate it.

    verifiers:                # Optional validation commands
      - name: test
        command: [go, test, ./...]
      - name: lint
        command: [golangci-lint, run]

    limits:                   # Optional resource limits
      max_iterations: 50      # Max Claude Code iterations
      max_tokens: 200000      # Max tokens per execution

    output:                   # For report mode
      schema:                 # JSON Schema for validation
        type: object
        required: [field1]
        properties:
          field1: {type: string}
```

**Verifiers:**
- Run after Claude Code completes
- Must exit with code 0 (success)
- Workflow fails if any verifier fails

**Limits:**
- Prevents runaway executions
- Defaults are usually sufficient

**Output Schema (Report Mode):**
- Validates YAML frontmatter
- Ensures consistent structure
- Based on JSON Schema spec

### Deterministic Mode (Docker)

```yaml
execution:
  deterministic:
    image: openrewrite/rewrite:latest
    args:
      - "rewrite:run"
      - "-Drewrite.activeRecipes=..."
    env:
      MAVEN_OPTS: "-Xmx2g"
      LOG_LEVEL: info

    verifiers:
      - name: build
        command: [mvn, compile]
```

**Image Requirements:**
- Accepts repository at `/workspace`
- Makes changes in place
- Exits with 0 on success

## Discovery Features

### forEach (Multi-Target)

```yaml
mode: report

repositories:
  - url: https://github.com/org/repo.git

for_each:
  - name: target-1
    context: "Context about target 1"
    custom_field: "value"

  - name: target-2
    context: "Context about target 2"
    custom_field: "other"

execution:
  agentic:
    prompt: |
      Analyze {{.Name}}.
      Context: {{.Context}}
      Custom: {{.CustomField}}
```

**Template Variables:**
- `{{.Name}}` - Target name
- `{{.Context}}` - Target context
- `{{.FieldName}}` - Any custom field (PascalCase)

**Report Files:**
- Creates `REPORT-{name}.md` for each target
- One Claude Code execution per target
- Results grouped by target in output

### Combining forEach with Transformation Repo

```yaml
transformation:
  url: https://github.com/org/toolkit.git

targets:
  - url: https://github.com/org/repo-a.git
  - url: https://github.com/org/repo-b.git

for_each:
  - name: endpoint-1
  - name: endpoint-2

# Runs: 2 targets × 2 forEach = 4 executions total
```

## Pull Request Configuration

```yaml
pull_request:
  branch_prefix: "fix/"          # Default: "fix/claude-"
  title: "PR title"
  body: |
    PR description with markdown

    ## Summary
    ...
  labels:
    - automated
    - security
  reviewers:
    - alice
    - bob
  team_reviewers:
    - platform-team
```

**Branch Naming:**
- Format: `{prefix}claude-{timestamp}`
- Example: `fix/claude-1234567890`

## Timeout

```yaml
timeout: 30m  # Workflow timeout (h/m/s)
```

**Formats:**
- `30m` - 30 minutes
- `2h` - 2 hours
- `90s` - 90 seconds

**Guidelines:**
- Agentic: 15-30 minutes per repo
- Deterministic: 5-10 minutes per repo
- forEach: Multiply by number of targets

## Approval

```yaml
require_approval: true   # Wait for human approval (transform mode)
# OR
require_approval: false  # Auto-proceed after verifiers pass
```

**When to use approval:**
- ✅ Security-sensitive changes
- ✅ Complex refactorings
- ✅ First-time automation
- ❌ Proven, tested transformations
- ❌ Report mode (no code changes)

## Parallel Execution

```yaml
repositories:
  - url: https://github.com/org/repo-1.git
  - url: https://github.com/org/repo-2.git
  - url: https://github.com/org/repo-3.git

parallel: true  # Create all 3 PRs simultaneously
```

**Default:** Sequential (one at a time)

**Use parallel when:**
- Repositories are independent
- Want faster completion
- Have sufficient worker capacity

## Optional Metadata

```yaml
description: "Detailed description of this task"
ticket_url: "https://jira.example.com/PROJ-123"
slack_channel: "#deployments"
owner: "platform-team"
```

Metadata is stored with the workflow but doesn't affect execution.

## Complete Example

```yaml
version: 1
id: upgrade-dependencies
title: "Upgrade Go dependencies to latest patch versions"
description: "Security updates for Q1 2024"
mode: transform

repositories:
  - url: https://github.com/org/service-a.git
    branch: main
    setup:
      - go mod download

  - url: https://github.com/org/service-b.git
    branch: develop
    setup:
      - go mod download

execution:
  agentic:
    prompt: |
      Upgrade all Go dependencies to the latest patch versions.

      Run `go get -u=patch ./...` and ensure tests pass.

    verifiers:
      - name: test
        command: [go, test, ./...]
      - name: build
        command: [go, build, ./...]
      - name: mod-tidy
        command: [go, mod, tidy]

    limits:
      max_iterations: 30
      max_tokens: 100000

pull_request:
  branch_prefix: "deps/"
  title: "chore: upgrade dependencies to latest patch versions"
  body: |
    ## Summary
    Automated dependency upgrades for security and stability.

    ## Testing
    - [x] All tests pass
    - [x] Build succeeds
    - [x] go.mod is tidy
  labels:
    - dependencies
    - automated
  reviewers:
    - security-team

timeout: 45m
require_approval: true
parallel: false

# Metadata
ticket_url: "https://jira.example.com/SEC-456"
owner: "security-team"
slack_channel: "#security-updates"
```

## Validation

The CLI validates task files before starting workflows:

```bash
./bin/cli run -f task.yaml
# Error: missing required field 'id'
# Error: invalid mode 'transforms' (must be 'transform' or 'report')
# Error: timeout must be a valid duration (e.g., '30m', '2h')
```

## Best Practices

1. **Use descriptive IDs**
   - Good: `fix-auth-vulnerability-2024`
   - Bad: `task1`, `test`

2. **Set appropriate timeouts**
   - Agentic: 15-30 minutes per repo
   - Add buffer for verifiers

3. **Always use verifiers**
   - At minimum: build + test
   - Catches issues before PR creation

4. **Start with approval enabled**
   - Validate approach on test repo first
   - Disable after confirming it works

5. **Use setup commands**
   - Install dependencies
   - Initialize tools
   - Prepare environment

6. **Be specific in prompts**
   - Provide examples
   - Specify file paths
   - Define success criteria

## See Also

- [Examples](examples/) - Example task files
- [CLI Reference](CLI_REFERENCE.md) - Running task files
- [Troubleshooting](TROUBLESHOOTING.md) - Common issues
