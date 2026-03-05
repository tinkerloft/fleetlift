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

- `cmd/` - CLI and worker entry points
- `internal/activity/` - Temporal activity implementations
- `internal/workflow/` - Temporal workflow definitions
- `internal/model/` - Data models and types
- `internal/agent/fleetproto/` - Fleetlift-specific protocol types (extends agentbox/protocol)
- `docs/` - Design documents and implementation plan

> Note: `internal/sandbox/` was deleted (sandbox logic moved to `github.com/tinkerloft/agentbox`).
> Note: `internal/agent/protocol/` shim was deleted in Phase AB-4; import `fleetproto` or `agentboxproto` directly.

## Key Conventions

- Use Temporal SDK patterns for activities and workflows
- Register new activities in `cmd/worker/main.go`
- Add activity name constants to `internal/activity/constants.go`
- Update `docs/IMPLEMENTATION_PLAN.md` when completing phases
