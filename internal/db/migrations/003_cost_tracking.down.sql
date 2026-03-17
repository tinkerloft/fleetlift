-- Reverse of 003_cost_tracking.up.sql
ALTER TABLE step_runs DROP COLUMN IF EXISTS cost_usd;
ALTER TABLE runs      DROP COLUMN IF EXISTS total_cost_usd;
