# Design: Store GITHUB_TOKEN as DB Credential in init-local

## Problem

`init-local` currently writes `GITHUB_TOKEN` to `~/.fleetlift/local.env` as a process environment variable. However, workflows fetch credentials from the database via `DBCredentialStore.GetBatch()` — not from the worker process env. As a result, the token stored in `local.env` is never injected into sandboxes or used by `ExecuteAction()`, making it effectively dead for workflow use.

## Solution

Store `GITHUB_TOKEN` as an encrypted team credential in the `credentials` table (scoped to `devTeamID`) during `init-local` setup. Remove it from `local.env`.

## Scope

Single file: `cmd/cli/init_local.go`. No schema, handler, or `DBCredentialStore` changes required.

## Design

### Pre-prompt DB check (replaces old local.env token detection)

Two existing code paths that read `GITHUB_TOKEN` from `local.env` must be removed:

1. `githubToken = existing["GITHUB_TOKEN"]` — remove this assignment entirely; the DB check replaces it.
2. The `if githubToken != "" { fmt.Println("Using existing GITHUB_TOKEN from local.env") }` early-exit — remove entirely.

Before showing the GitHub token input prompt, query the `credentials` table for an existing `GITHUB_TOKEN` row with `team_id = devTeamID`. The DB URL is the hardcoded default and is known at this point even though `applySchema()` hasn't run yet.

- **DB not yet set up, credentials table missing, or any connection error:** treat as "not found" and proceed to the normal input prompt. Expected on first-time runs.
- **If found:** show a confirm prompt — "GitHub token already configured. Overwrite it?" — skip the token input prompt if the user declines.
- **If not found:** show the existing optional, password-masked token input prompt.

### `seedGitHubToken(dbURL, encKey, token string) error`

New function with the same signature pattern as `seedDevIdentity` (takes `dbURL string`, opens its own plain `*sql.DB` — no sqlx dependency). Called after `applySchema()` and `seedDevIdentity()`, only when the user has provided or confirmed a new token.

**Encryption key invariant:** `encKey` must always be the value that ends up in `local.env` for this run — either reused from `existing["CREDENTIAL_ENCRYPTION_KEY"]` (re-run) or freshly generated (first run). This key is resolved at Step 4, before Step 8 where `seedGitHubToken` is called, so the invariant holds in all code paths including when the user skips the `local.env` overwrite prompt (in which case `encKey` is `existing["CREDENTIAL_ENCRYPTION_KEY"]`).

Steps:
1. Call `crypto.EncryptAESGCM(encKey, token)` to produce `value_enc`.
2. UPSERT into `credentials`:
   ```sql
   INSERT INTO credentials (team_id, name, value_enc)
   VALUES ($1, 'GITHUB_TOKEN', $2)
   ON CONFLICT (team_id, name) WHERE team_id IS NOT NULL
   DO UPDATE SET value_enc = EXCLUDED.value_enc, updated_at = now()
   ```
   with `team_id = devTeamID`. Targets the `credentials_team_name_unique` partial index created as part of `applySchema()`.
3. Return any error to the caller, which surfaces it to the user (same pattern as `seedDevIdentity`).

### Remove GITHUB_TOKEN from `writeLocalEnv()` and `localEnvConfig`

- Remove `GITHUB_TOKEN` from `writeLocalEnv()` — it must not emit the token to `local.env`.
- Remove the `GitHubToken string` field from the `localEnvConfig` struct.
- Remove the `GitHubToken: strings.TrimSpace(githubToken)` assignment in the struct literal.

Since `writeLocalEnv()` rewrites the entire file from the in-memory struct, not emitting the field naturally scrubs any stale `GITHUB_TOKEN` line from an existing `local.env` on re-runs.

### Idempotency

The UPSERT is safe to re-run. Re-running `init-local` with a new token updates the stored value; declining the overwrite prompt leaves the existing DB entry untouched.

## What Does Not Change

- `DBCredentialStore` (read-only, no new methods needed)
- HTTP credential handlers
- Database schema
- The token prompt UX (password-masked, optional) — only the pre-check logic and storage destination change

## Error Handling

- DB pre-check failure (first-time run, DB not yet created): treat as "not found", proceed normally
- Encryption or DB insert failure in `seedGitHubToken`: return error, surface to user, abort setup
- Empty token (user skipped or declined overwrite): skip `seedGitHubToken` entirely
