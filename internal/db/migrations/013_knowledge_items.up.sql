CREATE TABLE IF NOT EXISTS knowledge_items (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id              UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    workflow_template_id UUID REFERENCES workflow_templates(id) ON DELETE SET NULL,
    step_run_id          UUID REFERENCES step_runs(id) ON DELETE SET NULL,
    type                 TEXT NOT NULL,
    summary              TEXT NOT NULL,
    details              TEXT,
    source               TEXT NOT NULL,
    tags                 TEXT[] NOT NULL DEFAULT '{}',
    confidence           FLOAT NOT NULL DEFAULT 1.0,
    status               TEXT NOT NULL DEFAULT 'pending',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS knowledge_items_team_status ON knowledge_items(team_id, status);
CREATE INDEX IF NOT EXISTS knowledge_items_workflow ON knowledge_items(workflow_template_id, status);
