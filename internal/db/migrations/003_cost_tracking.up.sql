-- Added 2026-03-16: per-step and per-run cost tracking
ALTER TABLE step_runs ADD COLUMN IF NOT EXISTS cost_usd NUMERIC(10,6);
ALTER TABLE runs      ADD COLUMN IF NOT EXISTS total_cost_usd NUMERIC(10,6);
