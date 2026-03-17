-- Rollback E3: inbox interactive tools
DROP INDEX IF EXISTS step_runs_parent;
ALTER TABLE step_runs DROP COLUMN IF EXISTS checkpoint_artifact_id;
ALTER TABLE step_runs DROP COLUMN IF EXISTS checkpoint_branch;
ALTER TABLE step_runs DROP COLUMN IF EXISTS parent_step_run_id;

DO $$ BEGIN
  ALTER TABLE inbox_items DROP CONSTRAINT IF EXISTS inbox_items_kind_check;
  ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_kind_check
    CHECK (kind IN ('awaiting_input','output_ready'));
EXCEPTION WHEN others THEN NULL;
END $$;

ALTER TABLE inbox_items DROP COLUMN IF EXISTS urgency;
ALTER TABLE inbox_items DROP COLUMN IF EXISTS answered_by;
ALTER TABLE inbox_items DROP COLUMN IF EXISTS answered_at;
ALTER TABLE inbox_items DROP COLUMN IF EXISTS answer;
ALTER TABLE inbox_items DROP COLUMN IF EXISTS options;
ALTER TABLE inbox_items DROP COLUMN IF EXISTS question;
