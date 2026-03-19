-- REFERENCE ONLY — not executed at runtime.
-- Authoritative schema is in internal/db/migrations/*.up.sql (applied by golang-migrate at startup).
-- This file reflects migration 001 baseline only; see 002_post_initial.up.sql and 003_cost_tracking.up.sql for subsequent changes.

-- Teams
CREATE TABLE IF NOT EXISTS teams (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Users
CREATE TABLE IF NOT EXISTS users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT,
    name            TEXT NOT NULL,
    provider        TEXT NOT NULL,   -- 'github', 'okta', etc.
    provider_id     TEXT NOT NULL,
    platform_admin  BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_id)
);

-- Team membership
CREATE TABLE IF NOT EXISTS team_members (
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role        TEXT NOT NULL DEFAULT 'member',  -- 'member' | 'admin'
    PRIMARY KEY (team_id, user_id)
);

-- Refresh tokens
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Credentials (team-scoped secrets)
CREATE TABLE IF NOT EXISTS credentials (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    value_enc   BYTEA NOT NULL,   -- AES-256-GCM encrypted
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, name)
);

-- Workflow templates (team-owned; builtins stored separately in binary)
CREATE TABLE IF NOT EXISTS workflow_templates (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    slug        TEXT NOT NULL,
    title       TEXT NOT NULL,
    description TEXT,
    tags        TEXT[] NOT NULL DEFAULT '{}',
    yaml_body   TEXT NOT NULL,
    created_by  UUID REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, slug)
);

-- Runs
CREATE TABLE IF NOT EXISTS runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id         UUID NOT NULL REFERENCES teams(id),
    workflow_id     TEXT NOT NULL,   -- slug; may reference builtin
    workflow_title  TEXT NOT NULL,
    parameters      JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending',
    -- pending | running | awaiting_input | complete | failed | cancelled
    temporal_id     TEXT,
    triggered_by    UUID REFERENCES users(id),
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    error_message   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS runs_team_status ON runs(team_id, status);

-- Step runs
CREATE TABLE IF NOT EXISTS step_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    step_id         TEXT NOT NULL,
    step_title      TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',
    -- pending | cloning | running | verifying | awaiting_input | complete | failed | skipped
    sandbox_id      TEXT,
    sandbox_group   TEXT,
    output          JSONB,         -- structured output from agent
    diff            TEXT,          -- git diff (transform steps)
    pr_url          TEXT,
    branch_name     TEXT,
    error_message   TEXT,
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS step_runs_run_id ON step_runs(run_id);

-- Streaming logs (append-only)
CREATE TABLE IF NOT EXISTS step_run_logs (
    id              BIGSERIAL PRIMARY KEY,
    step_run_id     UUID NOT NULL REFERENCES step_runs(id) ON DELETE CASCADE,
    seq             BIGINT NOT NULL,
    stream          TEXT NOT NULL DEFAULT 'stdout',  -- 'stdout' | 'stderr' | 'system'
    content         TEXT NOT NULL,
    ts              TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS step_run_logs_step_seq ON step_run_logs(step_run_id, seq);

-- Artifacts
CREATE TABLE IF NOT EXISTS artifacts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    step_run_id     UUID NOT NULL REFERENCES step_runs(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    path            TEXT NOT NULL,      -- original path in sandbox
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    content_type    TEXT NOT NULL DEFAULT 'text/plain',
    storage         TEXT NOT NULL,      -- 'inline' | 'object_store'
    data            BYTEA,              -- for 'inline' storage
    object_key      TEXT,               -- for 'object_store'
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Inbox items
CREATE TABLE IF NOT EXISTS inbox_items (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id         UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    run_id          UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    step_run_id     UUID REFERENCES step_runs(id) ON DELETE CASCADE,
    kind            TEXT NOT NULL,  -- 'awaiting_input' | 'output_ready' | 'notify' | 'request_input'
    title           TEXT NOT NULL,
    summary         TEXT,
    artifact_id     UUID REFERENCES artifacts(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS inbox_team ON inbox_items(team_id, created_at DESC);

-- Inbox read receipts (per user)
CREATE TABLE IF NOT EXISTS inbox_reads (
    inbox_item_id   UUID NOT NULL REFERENCES inbox_items(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    read_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (inbox_item_id, user_id)
);

-- Service account API keys
CREATE TABLE IF NOT EXISTS api_keys (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    key_hash    TEXT NOT NULL UNIQUE,
    role        TEXT NOT NULL DEFAULT 'member',
    created_by  UUID REFERENCES users(id),
    expires_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Knowledge items (captured from agent step runs, enriched into future prompts)
CREATE TABLE IF NOT EXISTS knowledge_items (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id              UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    workflow_template_id UUID REFERENCES workflow_templates(id) ON DELETE SET NULL,
    step_run_id          UUID REFERENCES step_runs(id) ON DELETE SET NULL,
    type                 TEXT NOT NULL,    -- pattern | correction | gotcha | context
    summary              TEXT NOT NULL,
    details              TEXT,
    source               TEXT NOT NULL,   -- auto_captured | manual
    tags                 TEXT[] NOT NULL DEFAULT '{}',
    confidence           FLOAT NOT NULL DEFAULT 1.0,
    status               TEXT NOT NULL DEFAULT 'pending',  -- pending | approved | rejected
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS knowledge_items_team_status ON knowledge_items(team_id, status);
CREATE INDEX IF NOT EXISTS knowledge_items_workflow ON knowledge_items(workflow_template_id, status);
