-- Saved repos are per-user bookmarks only; team context is not relevant.
ALTER TABLE user_repos DROP COLUMN team_id;
ALTER TABLE user_repos DROP CONSTRAINT IF EXISTS user_repos_user_id_url_key;
ALTER TABLE user_repos ADD CONSTRAINT user_repos_user_id_url_key UNIQUE (user_id, url);
