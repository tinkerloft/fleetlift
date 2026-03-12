# Fleetlift

Multi-tenant agentic workflow platform. Define DAG-based workflows as YAML templates, run them at scale across repositories in isolated sandboxes, and collaborate with AI agents via real-time streaming and human-in-the-loop (HITL) signals.

## What it does

- **Workflow templates** — YAML-defined DAGs with parallel steps, dependencies, conditions
- **Agent execution** — Claude Code runs each step in an OpenSandbox container
- **HITL** — approve, reject, or steer any step mid-execution
- **Knowledge loop** — capture agent insights; inject approved items into future runs
- **Multi-tenant** — teams, JWT auth, GitHub OAuth
- **Reports** — structured output from report-mode steps, exportable as Markdown
- **9 built-in templates** — add tests, fix lint, upgrade deps, security audit, and more

## Quick start

**Prerequisites:** Docker, Go 1.24+, Node 20+

```bash
# Start Temporal + PostgreSQL
docker compose up -d

# Run database migrations + start the API server
go run ./cmd/server

# In a separate terminal: start the worker
go run ./cmd/worker

# Build the web UI
cd web && npm install && npm run build && cd ..

# Authenticate
fleetlift auth login

# List built-in workflows
fleetlift workflow list

# Trigger a run
fleetlift run start --workflow add-tests --param repo=https://github.com/org/repo.git

# Watch progress
fleetlift run list
```

## Environment variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `DATABASE_URL` | PostgreSQL DSN | `postgres://fleetlift:fleetlift@localhost:5432/fleetlift` |
| `TEMPORAL_ADDRESS` | Temporal gRPC address | `localhost:7233` |
| `OPENSANDBOX_DOMAIN` | OpenSandbox API base URL | — |
| `OPENSANDBOX_API_KEY` | OpenSandbox auth key | — |
| `AGENT_IMAGE` | Default sandbox image | `claude-code:latest` |
| `JWT_SECRET` | Server JWT signing key | — |
| `CREDENTIAL_ENCRYPTION_KEY` | 32-byte hex key for AES-256-GCM | — |
| `GITHUB_CLIENT_ID` | OAuth app client ID | — |
| `GITHUB_CLIENT_SECRET` | OAuth app client secret | — |
| `ANTHROPIC_API_KEY` | Claude API key for agent | — |
| `FLEETLIFT_API_URL` | CLI base URL | `http://localhost:8080` |

## Documentation

- [Workflow Reference](docs/WORKFLOW_REFERENCE.md) — YAML schema for workflow templates
- [CLI Reference](docs/CLI_REFERENCE.md) — all CLI commands
- [Architecture](docs/ARCHITECTURE.md) — system design
- [Troubleshooting](docs/TROUBLESHOOTING.md) — common issues

## Development

```bash
make lint       # golangci-lint
go test ./...   # unit tests
go test -tags integration ./tests/integration/...  # integration tests
cd web && npm run build  # build SPA
```
