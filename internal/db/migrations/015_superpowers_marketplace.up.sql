-- Seed the official Claude plugins marketplace (system-wide).
INSERT INTO marketplaces (name, repo_url, credential, team_id)
VALUES ('claude-plugins-official', 'github@claude-plugins-official', 'GITHUB_TOKEN', NULL)
ON CONFLICT (name) WHERE team_id IS NULL DO NOTHING;

-- Seed the superpowers agent profile (system-wide).
INSERT INTO agent_profiles (id, team_id, name, description, body)
VALUES (
    gen_random_uuid(),
    NULL,
    'superpowers',
    'Agent profile with code-review, TDD, and verification skills via the superpowers plugin',
    '{"plugins": [{"plugin": "superpowers"}]}'::jsonb
)
ON CONFLICT (name) WHERE team_id IS NULL DO NOTHING;
