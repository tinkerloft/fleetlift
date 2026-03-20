INSERT INTO agent_profiles (id, name, description, body)
VALUES (
    gen_random_uuid(),
    'superpowers',
    'Installs the superpowers plugin for TDD and code review workflows',
    '{"plugins":[{"plugin":"superpowers@5.0.1"}]}'
);
