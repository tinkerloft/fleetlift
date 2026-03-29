-- Prompt presets (personal or team-scoped)
CREATE TABLE prompt_presets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    scope       TEXT NOT NULL CHECK (scope IN ('personal', 'team')),
    title       TEXT NOT NULL,
    prompt      TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_prompt_presets_team_scope ON prompt_presets (team_id, scope, created_by);

-- Saved repos (per-user bookmarks)
CREATE TABLE user_repos (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id    UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    url        TEXT NOT NULL,
    label      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, url)
);
