# Claude Code Orchestrator

![CI](https://github.com/andreweacott/agent-orchestrator/workflows/CI/badge.svg)
![Go Version](https://img.shields.io/github/go-mod/go-version/andreweacott/agent-orchestrator)
![License](https://img.shields.io/badge/license-MIT-blue.svg)

A durable, scalable platform for automating code transformations and discovery across repositories using Claude Code and Temporal workflows.

## What is This?

This project enables you to run automated code changes and analysis tasks across multiple repositories with enterprise-grade reliability. Think of it as "Turbolift with AI capabilities" - it coordinates complex, multi-step transformations using Claude Code while Temporal ensures durability and recoverability.

### Key Features

- **Dual Execution Modes**
  - **Agentic**: Uses Claude Code CLI for AI-powered transformations
  - **Deterministic**: Runs custom Docker images for reproducible changes

- **Two Operation Modes**
  - **Transform Mode**: Makes code changes and creates pull requests
  - **Report Mode**: Analyzes repositories and collects structured data (discovery)

- **Enterprise Reliability**
  - Durable execution via Temporal (survives failures, network issues)
  - Human-in-the-loop approval gates
  - Parallel or sequential PR creation
  - Automatic retry and error handling

- **Advanced Discovery Patterns**
  - **forEach**: Analyze multiple targets within a single repository
  - **Transformation Repositories**: Reusable skills and tools across projects
  - **Schema Validation**: Structured output with JSON Schema enforcement

### Use Cases

**Code Transformations:**
- Apply security patches across microservices
- Migrate APIs or dependencies fleet-wide
- Refactor patterns across multiple repositories
- Enforce coding standards automatically

**Discovery & Analysis:**
- Security audits across all services
- Dependency inventory and version tracking
- Technical debt assessment before migrations
- Compliance scanning and reporting

## Architecture

```
┌─────────────┐         ┌──────────────┐         ┌─────────────┐
│   CLI/API   │────────▶│   Temporal   │────────▶│   Workers   │
└─────────────┘         └──────────────┘         └─────────────┘
                              │                         │
                              │                         ▼
                              │                  ┌─────────────┐
                              │                  │   Docker    │
                              │                  │  Sandbox    │
                              │                  └─────────────┘
                              │                         │
                              ▼                         ▼
                        ┌──────────────┐         ┌─────────────┐
                        │   Results    │         │ Claude Code │
                        │   Storage    │         │     CLI     │
                        └──────────────┘         └─────────────┘
```

## Quick Start

### Prerequisites

- **Go 1.21+**
- **Docker** (running) - for sandbox containers
- **Temporal CLI** - `brew install temporal`
- **API Keys**:
  ```bash
  export ANTHROPIC_API_KEY=sk-ant-...  # Required for Claude Code
  export GITHUB_TOKEN=ghp_...          # Optional, for creating PRs
  ```

### Installation

```bash
# Clone the repository
git clone https://github.com/andreweacott/agent-orchestrator.git
cd agent-orchestrator

# Build binaries
make build

# Run tests
make test
```

### Running Your First Workflow

**Terminal 1: Start Temporal**
```bash
make temporal-dev
```

**Terminal 2: Start Worker**
```bash
export ANTHROPIC_API_KEY=sk-ant-...
make run-worker
```

**Terminal 3: Run Smoke Test**
```bash
# Run a discovery workflow
./bin/cli run -f examples/smoke-test-discovery.yaml

# Monitor progress in Temporal UI
open http://localhost:8233

# Check result (wait ~1-2 minutes)
./bin/cli result --workflow-id transform-smoke-test-discovery

# View the collected report
./bin/cli reports transform-smoke-test-discovery -o json
```

## Example Workflows

<details>
<summary><strong>Quick Example: Repository Discovery</strong> - Click to expand</summary>

### Task File: `examples/smoke-test-discovery.yaml`

```yaml
version: 1
id: smoke-test-discovery
title: "Smoke Test: Codebase Discovery"
mode: report

repositories:
  - url: https://github.com/cespare/xxhash.git
    branch: main

execution:
  agentic:
    prompt: |
      Analyze this Go repository and produce a discovery report.

      Your task:
      1. Identify the main purpose of this library
      2. Count the number of Go files
      3. Check if tests exist
      4. Note the Go version requirements (if any)

      Write your findings to /workspace/xxhash/REPORT.md with YAML frontmatter.

    output:
      schema:
        type: object
        required: [library_name, purpose, go_files_count, has_tests]
        properties:
          library_name: {type: string}
          purpose: {type: string}
          go_files_count: {type: integer, minimum: 1}
          has_tests: {type: boolean}
          go_version: {type: string}

timeout: 5m
require_approval: false
```

### Running It

```bash
./bin/cli run -f examples/smoke-test-discovery.yaml
./bin/cli reports transform-smoke-test-discovery -o json
```

### Output

```json
{
  "repository": "xxhash",
  "frontmatter": {
    "library_name": "xxhash",
    "purpose": "Fast non-cryptographic hash algorithm",
    "go_files_count": 8,
    "has_tests": true,
    "go_version": "1.18"
  },
  "body": "# Discovery Report\n\n..."
}
```

</details>

### More Examples

Explore detailed examples for different use cases:

- **[Security Audit](docs/examples/01-security-audit.md)** - Analyze authentication across repositories (Report Mode)
- **[Multi-Target Discovery](docs/examples/02-multi-target-discovery.md)** - Analyze multiple API endpoints with forEach
- **[Code Transformation](docs/examples/03-code-transformation.md)** - Apply security fixes and create PRs (Transform Mode)
- **[Deterministic Transformation](docs/examples/04-deterministic-transform.md)** - Use Docker images for reproducible changes
- **[Transformation Repository](docs/examples/05-transformation-repository.md)** - Share skills and tools across projects

**[View All Examples →](docs/examples/)**

## Common Commands

```bash
# Start a workflow
./bin/cli run -f task.yaml

# List workflows
./bin/cli list
./bin/cli list --status Running

# Check workflow status (while running)
./bin/cli status --workflow-id transform-<task-id>

# Get workflow result (after completion)
./bin/cli result --workflow-id transform-<task-id>

# View reports (report mode)
./bin/cli reports transform-<task-id>
./bin/cli reports transform-<task-id> -o json

# Approve/reject changes
./bin/cli approve --workflow-id transform-<task-id>
./bin/cli reject --workflow-id transform-<task-id>
```

**[Full CLI Reference →](docs/CLI_REFERENCE.md)**

## Important Notes

- **Workflow IDs**: Automatically prefixed with `transform-` (e.g., `my-task` becomes `transform-my-task`)
- **Status vs Result**: Use `status` for running workflows, `result` for completed ones
- **Temporal UI**: View detailed execution at http://localhost:8233
- **Report Mode**: Set `require_approval: false` to skip manual approval for discovery tasks

## Documentation

- **[Examples](docs/examples/)** - Detailed workflow examples
- **[CLI Reference](docs/CLI_REFERENCE.md)** - Complete command documentation
- **[Task File Reference](docs/TASK_FILE_REFERENCE.md)** - Full YAML specification
- **[Troubleshooting](docs/TROUBLESHOOTING.md)** - Common issues and solutions
- **[Design Document](docs/DESIGN.md)** - Technical architecture and design decisions
- **[Implementation Plan](docs/IMPLEMENTATION_PLAN.md)** - Phased development roadmap
- **[Overview](docs/OVERVIEW.md)** - User-facing feature overview

## Development

```bash
# Run tests
make test

# Run tests with coverage
make test-coverage

# Lint code
make lint

# Build binaries
make build

# Format code
make fmt

# Build sandbox Docker image
make sandbox-build
```

### Project Structure

```
.
├── cmd/
│   ├── cli/          # CLI entry point
│   └── worker/       # Worker entry point
├── internal/
│   ├── activity/     # Temporal activities
│   ├── workflow/     # Temporal workflows
│   ├── model/        # Data models
│   ├── sandbox/      # Sandbox providers (Docker/K8s)
│   ├── client/       # Temporal client utilities
│   └── config/       # Configuration loading
├── examples/         # Example task files
├── docs/
│   ├── examples/     # Detailed example documentation
│   ├── DESIGN.md
│   ├── IMPLEMENTATION_PLAN.md
│   └── ...
└── CLAUDE.md        # Claude Code instructions
```

## Contributing

See [CLAUDE.md](CLAUDE.md) for development conventions and requirements.

Before submitting changes:
1. Run `make lint` - all code must pass linter
2. Run `go test ./...` - all tests must pass
3. Run `go build ./...` - code must compile
4. Update `docs/IMPLEMENTATION_PLAN.md` when completing phases

## License

MIT License - see [LICENSE](LICENSE) for details
