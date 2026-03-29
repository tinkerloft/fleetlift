ALTER TABLE users
    ADD COLUMN IF NOT EXISTS personal_team_id UUID REFERENCES teams(id);

CREATE UNIQUE INDEX IF NOT EXISTS users_personal_team_id_unique
    ON users(personal_team_id)
    WHERE personal_team_id IS NOT NULL;

ALTER TABLE runs
    ADD COLUMN IF NOT EXISTS model TEXT;
