-- Added 2026-03-16 (E3): inbox interactive tools
ALTER TABLE inbox_items ADD COLUMN IF NOT EXISTS question     TEXT;
ALTER TABLE inbox_items ADD COLUMN IF NOT EXISTS options      TEXT[];
ALTER TABLE inbox_items ADD COLUMN IF NOT EXISTS answer       TEXT;
ALTER TABLE inbox_items ADD COLUMN IF NOT EXISTS answered_at  TIMESTAMPTZ;
ALTER TABLE inbox_items ADD COLUMN IF NOT EXISTS answered_by  TEXT;
ALTER TABLE inbox_items ADD COLUMN IF NOT EXISTS urgency      TEXT NOT NULL DEFAULT 'normal';

DO $$ BEGIN
  ALTER TABLE inbox_items DROP CONSTRAINT IF EXISTS inbox_items_kind_check;
  ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_kind_check
    CHECK (kind IN ('awaiting_input','output_ready','notify','request_input'));
EXCEPTION WHEN others THEN NULL;
END $$;

-- Added 2026-03-16 (E3): continuation step support
ALTER TABLE step_runs ADD COLUMN IF NOT EXISTS parent_step_run_id     UUID REFERENCES step_runs(id);
ALTER TABLE step_runs ADD COLUMN IF NOT EXISTS checkpoint_branch      TEXT;
ALTER TABLE step_runs ADD COLUMN IF NOT EXISTS checkpoint_artifact_id UUID REFERENCES artifacts(id);

CREATE INDEX IF NOT EXISTS step_runs_parent
    ON step_runs(parent_step_run_id) WHERE parent_step_run_id IS NOT NULL;
