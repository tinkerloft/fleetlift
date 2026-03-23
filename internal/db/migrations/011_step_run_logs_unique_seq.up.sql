-- Ensure the step_run_logs ON CONFLICT target has a real unique index.
-- Some databases have a non-unique step_run_logs_step_seq index from older schema state,
-- which causes every log insert to fail with "there is no unique or exclusion constraint...".

DELETE FROM step_run_logs a
USING step_run_logs b
WHERE a.step_run_id = b.step_run_id
  AND a.seq = b.seq
  AND a.id > b.id;

DROP INDEX IF EXISTS step_run_logs_step_seq;

CREATE UNIQUE INDEX step_run_logs_step_seq
  ON step_run_logs (step_run_id, seq);
