# Fleetlift Task YAML Reference

Generate ONLY valid YAML. No explanations, no markdown fences, no prose — just the raw YAML.

## Always Required

```yaml
version: 1
title: "Short descriptive title"
```

## Repositories

At least one repository is required (unless using `transformation` + `targets`):

```yaml
repositories:
  - url: https://github.com/org/repo.git
    branch: main          # optional, default: main
    name: repo            # optional shortname for display
    setup:                # optional: commands to run after clone
      - go mod download
```

## Execution — choose EXACTLY ONE

### Agentic (Claude Code agent makes code changes):
```yaml
execution:
  agentic:
    prompt: |
      Detailed instructions for the agent.
      Be specific: what to change, how, and why.
    verifiers:            # optional but recommended
      - name: build
        command: ["go", "build", "./..."]
      - name: test
        command: ["go", "test", "./..."]
```

### Deterministic (runs a Docker image):
```yaml
execution:
  deterministic:
    image: openrewrite/rewrite:latest
    args: ["rewrite:run", "-Drewrite.activeRecipes=..."]
    env:                  # optional
      KEY: value
    verifiers:
      - name: build
        command: ["mvn", "compile"]
```

## Common Optional Fields

```yaml
id: my-task-slug          # alphanumeric + hyphens; auto-generated if omitted
description: "..."        # longer explanation
mode: transform           # "transform" (default, creates PRs) or "report" (analysis only)
require_approval: false   # true = workflow pauses for human approval before creating PRs
timeout: 30m              # e.g. "15m", "30m", "1h", "2h" (default: "1h")
max_parallel: 5           # repos processed concurrently (default: 5)

pull_request:
  branch_prefix: "auto/feature"
  title: "PR title"
  labels: ["automated"]
```

## Report Mode — structured output schema (optional):
```yaml
execution:
  agentic:
    prompt: |
      Analyze the codebase and write findings to /workspace/REPORT.md
      Use YAML frontmatter for structured data.
    output:
      schema:
        type: object
        required: [score]
        properties:
          score:
            type: integer
            minimum: 1
            maximum: 10
```
