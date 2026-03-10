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

## Transformation Repo Pattern (alternative to repositories)

```yaml
transformation:
  url: https://github.com/org/recipe.git
  branch: main
  setup: ["npm install"]
targets:
  - url: https://github.com/org/target.git
    name: target-name
```

## forEach (report mode only)

```yaml
for_each:
  - name: item-name       # Used in {{.Name}} template
    context: "..."         # Used in {{.Context}} template
```

## Common Optional Fields

```yaml
id: my-task-slug          # alphanumeric + hyphens; auto-generated if omitted
description: "..."        # longer explanation
mode: transform           # "transform" (default, creates PRs) or "report" (analysis only)
require_approval: false   # true = workflow pauses for human approval before creating PRs
timeout: 30m              # e.g. "15m", "30m", "1h", "2h" (default: "1h")
max_parallel: 5           # groups processed concurrently (default: 5)
max_steering_iterations: 5 # max HITL steering rounds (default: 5)
requester: "user@example.com" # optional metadata
ticket_url: "..."         # optional metadata
slack_channel: "#channel"  # optional: Slack notification channel

pull_request:
  branch_prefix: "auto/feature"
  title: "PR title"
  labels: ["automated"]
  reviewers: ["username"]  # GitHub usernames

sandbox:
  image: custom-image:latest  # override default sandbox image

knowledge:
  capture_disabled: false  # disable auto-capture
  enrich_disabled: false   # disable knowledge injection
  max_items: 10
  tags: ["go", "logging"]
```

## Grouped Execution

```yaml
groups:
  - name: group-name
    repositories:
      - url: https://github.com/org/repo.git

failure:
  threshold_percent: 20    # pause/abort if failure rate exceeds this
  action: pause            # "pause" or "abort"
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
