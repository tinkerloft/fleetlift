# Design: `fleetlift init-local` Wizard

**Date:** 2026-03-15
**Status:** Approved

## Overview

A `make init-local` target that builds the agent sandbox image, builds the CLI, and launches an interactive wizard (`fleetlift init-local`) to configure a local dev environment. The wizard generates `~/.fleetlift/local.env`, patches the integration scripts to source it, optionally starts the Docker Compose stacks, seeds the database with a dev identity, and pre-configures the CLI for unauthenticated local use.

This is a local development tool only — not intended for production deployments.

## Entry Point

```makefile
init-local: sandbox-build
	go build -o ./fleetlift ./cmd/cli && ./fleetlift init-local; rm -f ./fleetlift
```

`sandbox-build` already exists and builds `claude-code-sandbox:latest` from `docker/Dockerfile.sandbox`.

## New Code

**`cmd/cli/cmd/init_local.go`** — new cobra subcommand registered in the CLI root.
Uses `github.com/charmbracelet/huh` for interactive prompts with masked secret input.

## Wizard Flow

### Step 1 — Preflight
- Print welcome banner
- Check Docker daemon is running (`docker info`); abort with instructions if not

### Step 2 — Claude Auth
Prompt user to choose one:
- `ANTHROPIC_API_KEY` (masked input)
- `CLAUDE_CODE_OAUTH_TOKEN` (masked input)

### Step 3 — GitHub Token (optional)
Prompt for `GITHUB_TOKEN` (personal access token). Skippable — required only for workflows that interact with GitHub repos.

### Step 4 — Generate Secrets
Auto-generate using `crypto/rand` (no user input):
- `JWT_SECRET` — 32 random bytes, hex-encoded
- `CREDENTIAL_ENCRYPTION_KEY` — 32 random bytes, hex-encoded
- `DEV_USER_ID` — fixed well-known UUID constant: `00000000-0000-0000-0000-000000000001`
- `DEV_TEAM_ID` — fixed well-known UUID constant: `00000000-0000-0000-0000-000000000002`

Using fixed UUIDs for dev identity avoids DB conflicts on wizard re-runs and makes the dev identity predictable for debugging.

### Step 5 — Write `~/.fleetlift/local.env`
Always includes:
```bash
export DATABASE_URL="postgres://fleetlift:fleetlift@localhost:5432/fleetlift?sslmode=disable"
export TEMPORAL_ADDRESS="localhost:7233"
export TEMPORAL_UI_URL="http://localhost:8233"
export OPENSANDBOX_DOMAIN="http://localhost:8090"
export OPENSANDBOX_API_KEY=""
export AGENT_IMAGE="claude-code-sandbox:latest"
export GIT_USER_EMAIL="claude-agent@noreply.localhost"
export GIT_USER_NAME="Claude Code Agent"
export JWT_SECRET="<generated>"
export CREDENTIAL_ENCRYPTION_KEY="<generated>"
export DEV_NO_AUTH=1
export DEV_USER_ID="<generated>"
export DEV_TEAM_ID="<generated>"
# One of:
export ANTHROPIC_API_KEY="..."
# or:
export CLAUDE_CODE_OAUTH_TOKEN="..."
# Optional:
export GITHUB_TOKEN="..."
```

If `~/.fleetlift/local.env` already exists, prompt to overwrite or skip.

### Step 6 — Patch Integration Scripts
Patch `scripts/integration/dev-env.sh` only. All other scripts that need env vars already source `dev-env.sh`. Insert after the shebang line (idempotent — skip if already present):
```bash
[ -f ~/.fleetlift/local.env ] && source ~/.fleetlift/local.env
```
Insert position: after line 1 (`#!/usr/bin/env bash`), before any `set` or other lines.

### Step 7 — Start Docker Stacks
Prompt: _"Start Docker Compose stacks now? (Temporal + Postgres + OpenSandbox)"_
On confirm, run sequentially:
```bash
docker compose up -d
docker compose -f docker-compose.opensandbox.yaml up -d
```
On decline, prompt: _"Are the stacks already running? Proceed with database setup? (y/N)"_
- Yes → continue to Step 8
- No → print the start commands and exit with next-steps instructions

### Step 8 — Seed Dev Identity
If Docker stacks were started (or user confirms they're already running):
- Poll `DATABASE_URL` until Postgres accepts connections — 1s interval, 30s max. On timeout, abort with instructions to run `docker compose up -d` and re-run the wizard.
- Upsert team row: `id = DEV_TEAM_ID, name = "dev-team", slug = "dev-team"`
- Upsert user row: `id = DEV_USER_ID, name = "Dev User", provider = "dev", provider_id = DEV_USER_ID`
- Upsert team_members row: `team_id = DEV_TEAM_ID, user_id = DEV_USER_ID, role = "admin"`
- Write `~/.fleetlift/auth.json`: `{"token": "dev-token"}` (dummy non-JWT value — `devAuthBypass` attempts JWT validation, it fails for a non-JWT string, and falls through to injecting `DEV_USER_ID`/`DEV_TEAM_ID` claims from env)

### Step 9 — Print Next Steps
```
Local environment ready.

Start the server and worker:
  scripts/integration/start.sh

Tail logs:
  scripts/integration/logs.sh

Temporal UI: http://localhost:8233
Fleetlift UI: http://localhost:8080
```

## Server-Side Dev Auth (Already Implemented)

`internal/server/router.go` already implements `devAuthBypass` middleware:
- Enabled when `DEV_NO_AUTH=1`
- Reads `DEV_USER_ID` and `DEV_TEAM_ID` from env and injects them as JWT claims
- Falls through to real JWT validation if an `Authorization` header is present

No server changes required.

## Database Schema

Verified against `internal/db/schema.sql`. Upserts required:

```sql
-- teams
INSERT INTO teams (id, name, slug) VALUES ($1, 'dev-team', 'dev-team')
ON CONFLICT (slug) DO NOTHING;

-- users
INSERT INTO users (id, name, provider, provider_id) VALUES ($1, 'Dev User', 'dev', $1)
ON CONFLICT (provider, provider_id) DO NOTHING;

-- team_members
INSERT INTO team_members (team_id, user_id, role) VALUES ($2, $1, 'admin')
ON CONFLICT (team_id, user_id) DO NOTHING;
```

## Dependencies

- `github.com/charmbracelet/huh v0.6+` — interactive TUI prompts with masked input (add to go.mod)
- `github.com/google/uuid` — UUID generation (already in go.mod)
- Standard library `crypto/rand`, `encoding/hex`, `database/sql`

## Out of Scope

- Production deployment configuration
- Homebrew packaging / binary distribution
- Multi-user or multi-team local setups
- Automated upgrades to an existing local.env
