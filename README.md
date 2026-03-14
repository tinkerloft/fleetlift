# FleetLift

<p align="center">
  <img src="docs/images/header.jpg" alt="FleetLift" width="800">
</p>

**Open-source orchestration platform for AI agent workflows - using DAG pipelines, human approval gates, and a knowledge loop that gets smarter over time.**

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

<p align="center">
  <video src="https://github.com/user-attachments/assets/793c829e-7d01-4a72-980a-85eb4a9926dc" controls width="800"></video>
</p>

---

## Why FleetLift?

AI coding agents are powerful - but running one agent on one repo is just the beginning. Sometimes you need to apply changes **consistently across dozens of repositories**, with human oversight, audit trails, and the ability to stop or redirect agents mid-execution.

FleetLift gives you:

- **DAG workflows** - not just "run an agent." Define multi-step pipelines where analysis feeds into transformation, transformation gets verified, and PRs only get created after human approval.
- **Fleet-scale fan-out** - one template, 50 repos, max 10 parallel. Aggregated results in one report. Failure thresholds so one bad repo doesn't tank the whole run.
- **Human-in-the-loop** - approve, reject, or *steer* any step mid-execution. Four approval policies (`always`, `never`, `agent`, `on_changes`) so you control exactly where humans gate the pipeline.
- **Knowledge loop** - agents capture insights during execution. You curate them. Future runs get enriched with approved knowledge, so agents get better over time.
- **Self-hosted** - your infrastructure, your data, your API keys. No vendor lock-in.
- **Scales from laptop to production** - run locally with Docker Compose for development; deploy to Kubernetes for enterprise-scale throughput. Same code, same API, no changes required.
- **Multiple agents supported** - designed to support multiple coding agents (Claude Code, Gemini, Codex, bring your own)

## Who is this for?

- **Platform teams** managing 10+ repositories that need consistent changes
- **Security teams** running fleet-wide audits and automated remediation
- **DevOps** coordinating large migrations (framework swaps, API changes, dependency upgrades)
- **Anyone** who's tired of manually running the same AI-assisted changes across multiple repos

---

## Features

### DAG Workflows
Define workflows as YAML. Steps run in parallel where possible, with explicit dependencies, conditional execution, and shared sandbox VMs for multi-step operations.

```yaml
steps:
  - id: analyze
    mode: report
    prompt: "Analyze the codebase for security vulnerabilities..."

  - id: fix
    mode: transform
    depends_on: [analyze]
    prompt: "Fix the issues found: {{ .Steps.analyze.Output }}"
    approval: on_changes

  - id: verify
    depends_on: [fix]
    verifiers:
      - command: ["go", "test", "./..."]
      - command: ["golangci-lint", "run"]
```

### Fleet-Scale Operations
Fan out across repositories with configurable parallelism and failure thresholds:

```yaml
repositories:
  - url: https://github.com/org/service-a.git
  - url: https://github.com/org/service-b.git
  - url: https://github.com/org/service-c.git
max_parallel: 5
failure_threshold: 2   # pause after 2 repo failures
```

### Human-in-the-Loop
Approve, reject, or redirect agents from the web UI or CLI:

```bash
# Approve a step awaiting input
fleetlift run approve <run-id>

# Redirect an agent mid-execution
fleetlift run steer <run-id> --prompt "Also handle the edge case for empty arrays"

# Reject and stop
fleetlift run reject <run-id>
```

<img src="docs/images/demo.gif" alt="FleetLift CLI interaction" width="500">

### 10 Built-in Templates
Ready-to-use workflows for common platform operations:

| Template | What it does |
|----------|-------------|
| `audit` | Fleet-wide security/compliance scan with executive report |
| `bug-fix` | Analyze → fix → self-review in shared sandbox |
| `dependency-update` | Find and upgrade outdated dependencies |
| `fleet-research` | Parallel research across repos with aggregated findings |
| `fleet-transform` | Apply code changes across fleet with approval gates |
| `incident-response` | Triage → root cause → fix → verify |
| `migration` | Impact analysis → transform → validate |
| `pr-review` | AI-assisted code review |
| `triage` | Issue analysis, classification, labeling |
| `add-tests` | Generate test coverage for under-tested code |

### Web UI
Real-time DAG visualization, live log streaming, HITL controls, inbox notifications, knowledge management, and structured reports - all in one place.

### Knowledge Loop
Agents capture insights during execution. You curate them via the Inbox. Approved knowledge is injected into future runs matching relevant tags - so your agents improve over time.

---

## How it compares

| Capability | FleetLift | Cursor Automations | GitHub Actions | Raw Temporal |
|---|---|---|---|---|
| DAG workflows | Multi-step with dependencies, conditions | Single agent per automation | YAML workflows (CI-focused) | Build from scratch |
| Multi-repo fan-out | Native, with parallelism + failure thresholds | One repo per run | Matrix strategy (CI-focused) | Build from scratch |
| Human-in-the-loop | Approve / reject / steer mid-execution | Send follow-ups to agents | Manual approval gates (no steering) | Build from scratch |
| Knowledge loop | Capture → curate → inject into future runs | Agent memory tool | None | Build from scratch |
| Event triggers | API-driven (external scheduler/webhook) | Native: cron, Slack, Linear, PagerDuty | Native: push, PR, schedule, webhook | Build from scratch |
| Setup | Docker Compose (local) or Kubernetes (prod) | Zero (SaaS) | Zero (GitHub-hosted) | Self-hosted cluster |
| Open source | Yes (MIT) | No | Runners are OSS, platform is not | Yes (MIT) |

FleetLift is strongest when you need **complex, multi-step workflows across many repos with human oversight**. See [docs/COMPARISON.md](docs/COMPARISON.md) for a detailed breakdown.

---

## Quick Start

**Prerequisites:** Docker, Go 1.24+, Node 20+, an [Anthropic API key](https://console.anthropic.com/) (or Claude Code OAUTH token). For Kubernetes deployment see [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md).

```bash
# Clone the repo
git clone https://github.com/your-org/fleetlift.git && cd fleetlift

# Start Temporal + PostgreSQL
docker compose up -d

# Set required env vars 
# One of ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN is required
export ANTHROPIC_API_KEY="sk-ant-..."
export CLAUDE_CODE_OAUTH_TOKEN="sk-..."  
export JWT_SECRET="$(openssl rand -hex 32)"
export CREDENTIAL_ENCRYPTION_KEY="$(openssl rand -hex 32)"

# Start the API server (builds + serves the web UI)
go run ./cmd/server &

# Start the Temporal worker
go run ./cmd/worker &

# Build the web UI
cd web && npm install && npm run build && cd ..

# Open the web UI
open http://localhost:8080

# Or use the CLI
fleetlift auth login
fleetlift workflow list
fleetlift run start --workflow audit --param repo=https://github.com/org/repo.git
fleetlift run logs <run-id> -f
```

For a detailed walkthrough, see the [Getting Started guide](docs/GETTING_STARTED.md).

---

## Architecture

```mermaid
flowchart LR
    CLI["**CLI**"]
    WebUI["**Web UI**"]
    API["**API Server**"]
    Temporal["**Temporal Workflows**"]
    Worker["**Fleetlift Temporal Worker**"]
    Sandbox["**OpenSandbox**<br/>ephemeral containers per step<br/>Coding agents run inside VMs"]

    CLI <-->|REST/SSE| API
    WebUI <-->|REST/SSE| API
    API -->|"Temporal SDK<br/>start/signal"| Temporal
    Temporal -->|"task queue: fleetlift"| Worker
    Worker -->|REST| Sandbox
```

For the full architecture deep-dive, see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

---

## Deployment

FleetLift runs the same way whether you're developing locally or running a production platform — the only difference is the sandbox runtime underneath.

| Environment | Sandbox runtime | When to use |
|---|---|---|
| **Local / Docker** | Docker containers via Docker Compose | Development, testing, small teams |
| **Kubernetes** | Kubernetes-native scheduling | Production, high throughput, enterprise scale |

Agent sandboxes are powered by [OpenSandbox](https://github.com/alibaba/OpenSandbox), which provides a **unified API across both runtimes** — no code changes required when moving from local to production.

### What OpenSandbox brings

- **Strong isolation** — each agent step runs in its own ephemeral sandbox with gVisor, Kata Containers, or Firecracker microVM isolation (depending on your runtime config)
- **Per-sandbox resource controls** — CPU, memory quotas, and timeouts per execution step
- **Network policies** — per-sandbox egress controls and unified ingress gateways
- **Kubernetes scheduling** — automatic resource management and distributed scheduling at scale
- **Protocol-based extensibility** — custom runtimes can implement the sandbox protocol, keeping FleetLift vendor-agnostic

For deployment instructions, see [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md).

---

## Environment Variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `DATABASE_URL` | PostgreSQL DSN | `postgres://fleetlift:fleetlift@localhost:5432/fleetlift` |
| `TEMPORAL_ADDRESS` | Temporal gRPC address | `localhost:7233` |
| `OPENSANDBOX_DOMAIN` | OpenSandbox API base URL | - |
| `OPENSANDBOX_API_KEY` | OpenSandbox auth key | - |
| `AGENT_IMAGE` | Default sandbox image | `claude-code:latest` |
| `JWT_SECRET` | Server JWT signing key | - |
| `CREDENTIAL_ENCRYPTION_KEY` | 32-byte hex key for AES-256-GCM | - |
| `GITHUB_CLIENT_ID` | OAuth app client ID | - |
| `GITHUB_CLIENT_SECRET` | OAuth app client secret | - |
| `ANTHROPIC_API_KEY` | Claude API key for agent | - |
| `FLEETLIFT_API_URL` | CLI base URL | `http://localhost:8080` |

---

## Documentation

- [Use Cases](docs/USE_CASES.md) - concrete scenarios and how FleetLift handles them
- [Comparison](docs/COMPARISON.md) - FleetLift vs Cursor Automations, GitHub Actions, and others
- [Workflow Reference](docs/WORKFLOW_REFERENCE.md) - YAML schema for workflow templates
- [CLI Reference](docs/CLI_REFERENCE.md) - all CLI commands
- [Architecture](docs/ARCHITECTURE.md) - system design deep-dive
- [Getting Started](docs/GETTING_STARTED.md) - first workflow tutorial
- [Deployment](docs/DEPLOYMENT.md) - production deployment guide
- [Troubleshooting](docs/TROUBLESHOOTING.md) - common issues and fixes
- [Examples](examples/) - 15 example workflow templates

---

## Development

```bash
make lint                    # golangci-lint
go test ./...                # unit tests
go test -tags integration ./tests/integration/...  # integration tests
go build ./...               # build all binaries
cd web && npm run build      # build SPA
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

---

## License

[MIT](LICENSE)
