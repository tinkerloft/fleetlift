-- Store the resolved per-step inputs (e.g. repo URL + ref for each fan-out instance).
-- Enables the UI to show exactly what each step received, rather than the full run parameters.
ALTER TABLE step_runs ADD COLUMN input JSONB;
