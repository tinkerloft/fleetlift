# Contributing to FleetLift

Thanks for your interest in contributing to FleetLift! This guide will help you get started.

## Code of Conduct

This project follows the [Contributor Covenant v2.1](https://www.contributor-covenant.org/version/2/1/code_of_conduct/). By participating, you agree to uphold its standards. Please report unacceptable behavior to the maintainers.

## Quick Start

### Prerequisites

- Docker
- Go 1.24+
- Node 20+

### Setup

1. Fork and clone the repository.

2. Start infrastructure (Temporal + PostgreSQL):

   ```bash
   docker compose up -d
   ```

3. Install frontend dependencies and build the SPA:

   ```bash
   cd web && npm install && npm run build
   ```

4. Verify everything compiles:

   ```bash
   go build ./...
   ```

5. Run the test suite:

   ```bash
   go test ./...
   ```

You are now ready to develop. The project has three entry points:

| Binary | Source | Purpose |
|--------|--------|---------|
| `fleetlift` | `cmd/cli/` | Cobra CLI |
| worker | `cmd/worker/` | Temporal worker |
| server | `cmd/server/` | REST API + SSE server (chi, port 8080) |

All core Go packages live under `internal/`, and the React 19 + TypeScript + Vite frontend lives in `web/`.

For deeper orientation, see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Code Style and Conventions

### Go

- All code must pass `make lint` (golangci-lint) with zero errors.
- Use Temporal SDK patterns for activities and workflows. Register new activities and workflows in `cmd/worker/main.go` and add activity name constants to `internal/activity/constants.go`.
- **Temporal determinism:** Never use `slog.*`, `fmt.Print*`, `log.*`, `time.Now()`, or `math/rand` inside workflow functions (`internal/workflow/`). Use `workflow.GetLogger(ctx)`, `workflow.Now(ctx)`, and `workflow.SideEffect` instead. Activities are exempt.
- **Input validation:** Validate user-supplied values at trust boundaries. Repo URLs must be `https://` only. Credential names used as env vars must match `^[A-Z][A-Z0-9_]*$`. File paths must stay within `/workspace/`.
- **Shell commands:** Every user-controlled string interpolated into a shell command must be wrapped with `shellQuote()` (see `internal/agent/quote.go` and `internal/activity/util.go`).
- **PostgreSQL types:** Use `pq.StringArray` for `TEXT[]` columns and the project's `JSONMap` type (from `internal/model/types.go`) for `JSONB` columns.
- **YAML parsing:** Use `yaml.Unmarshal` from `gopkg.in/yaml.v3` for YAML content. Never use `json.Unmarshal` for YAML.

### Frontend (web/)

- All `fetch`/promise chains must have a `.catch()` handler or be wrapped in `try/catch`.
- Never call `res.json()` without first checking `res.status !== 204` and `res.headers.get('content-length') !== '0'`.
- DELETE endpoints return 204 No Content -- do not parse the response body.

## Testing Requirements

Before submitting a pull request, run:

```bash
make lint            # linter must pass cleanly
go test ./...        # all unit tests must pass
go build ./...       # must compile without errors
```

Integration tests (requires Docker infrastructure running):

```bash
go test -tags integration ./tests/integration/...
```

### What to test

- Add unit tests for all new functionality.
- `internal/auth/middleware.go` -- auth middleware and SSE ticket lifecycle must have tests.
- `internal/server/handlers/auth.go` -- OAuth CSRF state validation must have tests.
- Any new encryption or credential handling code must have tests.
- New Temporal workflows must include at least one `go.temporal.io/sdk/testsuite` test.

## Pull Request Process

1. Create a feature branch from `main`.
2. Make your changes, following the conventions above.
3. Run `make lint`, `go test ./...`, and `go build ./...` locally. All must pass.
4. Write a clear PR description explaining **what** changed and **why**.
5. Keep PRs focused -- one logical change per PR.
6. A maintainer will review your PR. Address feedback and push updates to the same branch.
7. Once approved, a maintainer will merge your PR.

## Where to Contribute

Not sure where to start? Here are some ideas:

- Check [ENHANCEMENTS.md](ENHANCEMENTS.md) for the enhancement backlog -- these are features and improvements the project is actively looking for help with.
- Look for issues labeled `good first issue` or `help wanted`.
- Improve documentation or add examples.
- Write tests for under-covered areas.

For more context on the project, see:

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) -- system design and package overview
- [docs/WORKFLOW_REFERENCE.md](docs/WORKFLOW_REFERENCE.md) -- workflow authoring guide
- [docs/CLI_REFERENCE.md](docs/CLI_REFERENCE.md) -- CLI usage
- [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) -- common issues and fixes

## Reporting Issues

When filing a bug report, please include:

- Steps to reproduce the issue
- Expected behavior vs. actual behavior
- Go version (`go version`), Node version (`node --version`), and OS
- Relevant log output or error messages

For feature requests, describe the use case and the behavior you would like to see.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
