# Claude Code Instructions

Project-specific instructions for Claude Code when working on this repository.

## Before Completing Any Task

**Required checks before marking work complete:**

1. **Run linter**: `make lint`
   - All code must pass golangci-lint with no errors
   - Fix any lint issues before completing the task

2. **Run tests**: `go test ./...`
   - All tests must pass
   - Add tests for new functionality

3. **Build verification**: `go build ./...`
   - Code must compile without errors

## Project Structure

- `cmd/cli/` - cobra CLI (`fleetlift` binary)
- `cmd/worker/` - Temporal worker (registers DAGWorkflow, StepWorkflow, all activities)
- `cmd/server/` - REST API + SSE server (chi, port 8080)
- `internal/activity/` - Temporal activity implementations
- `internal/agent/` - AgentRunner interface + ClaudeCodeRunner
- `internal/auth/` - JWT, GitHub OAuth, HTTP middleware
- `internal/db/` - PostgreSQL connection helper + schema
- `internal/knowledge/` - Local knowledge store (v1 holdover — needs decision: wire in or remove)
- `internal/logging/` - slog adapter
- `internal/metrics/` - Prometheus interceptor
- `internal/model/` - All entity types (Run, StepRun, WorkflowTemplate, etc.)
- `internal/sandbox/` - sandbox.Client interface + opensandbox/ REST implementation
- `internal/server/` - chi router + handlers (auth, workflows, runs, inbox, reports, credentials)
- `internal/template/` - BuiltinProvider, DBProvider, Registry, RenderPrompt; 9 builtin YAML workflows
- `internal/workflow/` - DAGWorkflow + StepWorkflow (Temporal)
- `web/` - React 19 + TypeScript + Vite SPA (embedded in server binary via web/embed.go)
- `docs/plans/` - Design doc and implementation plan

## Key Conventions

- Use Temporal SDK patterns for activities and workflows
- Register new activities/workflows in `cmd/worker/main.go`
- Add activity name constants to `internal/activity/constants.go`
- Update `docs/plans/2026-03-11-platform-redesign-impl.md` when completing phases

## Environment Variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `DATABASE_URL` | PostgreSQL DSN | `postgres://fleetlift:fleetlift@localhost:5432/fleetlift` |
| `TEMPORAL_ADDRESS` | Temporal server | `localhost:7233` |
| `OPENSANDBOX_DOMAIN` | OpenSandbox API base URL | — |
| `OPENSANDBOX_API_KEY` | OpenSandbox auth key | — |
| `AGENT_IMAGE` | Default sandbox image (Claude Code) | `claude-code:latest` |
| `JWT_SECRET` | Server JWT signing key | — |
| `CREDENTIAL_ENCRYPTION_KEY` | 32-byte hex key for AES-256-GCM | — |
| `GITHUB_CLIENT_ID` | OAuth app client ID | — |
| `GITHUB_CLIENT_SECRET` | OAuth app client secret | — |
| `GIT_USER_EMAIL` | Git commit identity for agent | `claude-agent@noreply.localhost` |
| `GIT_USER_NAME` | Git commit identity for agent | `Claude Code Agent` |
| `FLEETLIFT_API_URL` | CLI base URL | `http://localhost:8080` |
