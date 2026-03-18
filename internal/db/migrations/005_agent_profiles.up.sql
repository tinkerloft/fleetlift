CREATE TABLE marketplaces (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    repo_url    TEXT NOT NULL,
    credential  TEXT NOT NULL DEFAULT '',
    team_id     UUID REFERENCES teams(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX marketplaces_team_name_idx   ON marketplaces (team_id, name) WHERE team_id IS NOT NULL;
CREATE UNIQUE INDEX marketplaces_system_name_idx ON marketplaces (name)          WHERE team_id IS NULL;

CREATE TABLE agent_profiles (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id     UUID REFERENCES teams(id),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    body        JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX agent_profiles_team_name_idx   ON agent_profiles (team_id, name) WHERE team_id IS NOT NULL;
CREATE UNIQUE INDEX agent_profiles_system_name_idx ON agent_profiles (name)          WHERE team_id IS NULL;

INSERT INTO agent_profiles (id, name, description, body)
VALUES (gen_random_uuid(), 'baseline', 'System-wide baseline profile',
        '{"plugins":[],"skills":[],"mcps":[]}');

ALTER TABLE workflow_templates ADD COLUMN agent_profile TEXT;
