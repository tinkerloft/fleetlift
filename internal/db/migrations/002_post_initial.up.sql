-- Added 2026-03-12: temporal_workflow_id for HITL signal routing
ALTER TABLE step_runs ADD COLUMN IF NOT EXISTS temporal_workflow_id TEXT;

-- Added 2026-03-12: performance indexes
CREATE INDEX IF NOT EXISTS step_run_logs_stream_cursor
    ON step_run_logs (step_run_id, id);

CREATE INDEX IF NOT EXISTS runs_team_created
    ON runs (team_id, created_at DESC);

CREATE INDEX IF NOT EXISTS runs_team_completed
    ON runs (team_id, status, completed_at DESC);

-- Added 2026-03-12: LISTEN/NOTIFY for SSE streaming
CREATE OR REPLACE FUNCTION notify_run_event() RETURNS trigger AS $$
BEGIN
  IF TG_TABLE_NAME = 'step_run_logs' THEN
    PERFORM pg_notify('run_events',
      (SELECT run_id::text FROM step_runs WHERE id = NEW.step_run_id));
  ELSIF TG_TABLE_NAME = 'step_runs' THEN
    PERFORM pg_notify('run_events', NEW.run_id::text);
  ELSIF TG_TABLE_NAME = 'runs' THEN
    PERFORM pg_notify('run_events', NEW.id::text);
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER step_run_logs_notify
  AFTER INSERT ON step_run_logs FOR EACH ROW EXECUTE FUNCTION notify_run_event();
CREATE OR REPLACE TRIGGER step_runs_notify
  AFTER UPDATE OF status ON step_runs FOR EACH ROW EXECUTE FUNCTION notify_run_event();
CREATE OR REPLACE TRIGGER runs_notify
  AFTER UPDATE OF status ON runs FOR EACH ROW EXECUTE FUNCTION notify_run_event();

-- Added 2026-03-15: allow system-wide credentials (team_id = NULL)
ALTER TABLE credentials ALTER COLUMN team_id DROP NOT NULL;
ALTER TABLE credentials DROP CONSTRAINT IF EXISTS credentials_team_id_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS credentials_team_name_unique
  ON credentials (team_id, name) WHERE team_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS credentials_system_name_unique
  ON credentials (name) WHERE team_id IS NULL;
