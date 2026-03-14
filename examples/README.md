# Example Workflows

Ready-to-use YAML workflow templates demonstrating FleetLift's capabilities. Use these as starting points for your own workflows.

For the full YAML schema reference, see [docs/WORKFLOW_REFERENCE.md](../docs/WORKFLOW_REFERENCE.md).

## Multi-Repository Operations

These templates demonstrate fleet-scale operations across multiple repos:

| File | Description |
|------|-------------|
| [multi-repo-security-fix.yaml](multi-repo-security-fix.yaml) | Apply a security fix (JWT expiration) across 5 microservices in parallel with human approval and PR creation |
| [multi-repo-grouped-migration.yaml](multi-repo-grouped-migration.yaml) | Migrate auth across grouped repos (backend, gateway, frontend) with shared sandboxes per group |
| [multi-repo-dependency-inventory.yaml](multi-repo-dependency-inventory.yaml) | Research dependencies across repos and produce an aggregated inventory report |

## Single-Repository Tasks

Templates for common single-repo operations:

| File | Description |
|------|-------------|
| [task-agentic.yaml](task-agentic.yaml) | AI agent executes a freeform task with iterative refinement |
| [task-transformation.yaml](task-transformation.yaml) | Code transformation with build/test verification |
| [task-report.yaml](task-report.yaml) | Report-mode step that produces structured JSON output (no code changes) |
| [task-deterministic.yaml](task-deterministic.yaml) | Shell-only step (no AI agent) for deterministic operations |
| [task-foreach.yaml](task-foreach.yaml) | Fan-out a step across a dynamic list of items |
| [security-audit.yml](security-audit.yml) | Security audit scan with structured findings report |

## Error Handling

Templates demonstrating failure handling strategies:

| File | Description |
|------|-------------|
| [task-with-failure-threshold.yaml](task-with-failure-threshold.yaml) | Pause fan-out after N repo failures instead of failing immediately |
| [task-with-abort-on-failure.yaml](task-with-abort-on-failure.yaml) | Abort entire run on first step failure |

## Execution Patterns

Templates showing DAG orchestration features:

| File | Description |
|------|-------------|
| [execution-patterns.yaml](execution-patterns.yaml) | Demonstrates parallel steps, dependencies, conditions, and shared sandboxes |
| [smoke-test-minimal.yaml](smoke-test-minimal.yaml) | Simplest possible workflow — one step, no dependencies |
| [smoke-test-parallel.yaml](smoke-test-parallel.yaml) | Two independent steps running in parallel |
| [smoke-test-discovery.yaml](smoke-test-discovery.yaml) | Discovery step feeding into a transform step |

## Writing Your Own

Start from any template above and customize. The minimal structure is:

```yaml
version: 1
id: my-workflow
title: "My custom workflow"
description: "What this workflow does"

steps:
  - id: do-the-thing
    mode: transform
    prompt: "Your instructions to the AI agent..."
    approval: on_changes
```

Key concepts:
- **`mode: transform`** — agent can modify code; diff is captured
- **`mode: report`** — agent produces structured output only
- **`depends_on: [step-id]`** — wait for another step to complete first
- **`approval: always|never|agent|on_changes`** — when to pause for human review
- **`verifiers`** — commands that must pass after the agent finishes (build, test, lint)
- **`{{ .Steps.prior_step.Output }}`** — reference output from a completed step

See the [Workflow Reference](../docs/WORKFLOW_REFERENCE.md) for all available fields.
