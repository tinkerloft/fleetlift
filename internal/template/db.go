package template

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// DBProvider stores workflow templates in PostgreSQL.
type DBProvider struct {
	db *sqlx.DB
}

func NewDBProvider(db *sqlx.DB) *DBProvider {
	return &DBProvider{db: db}
}

func (d *DBProvider) Name() string  { return "database" }
func (d *DBProvider) Writable() bool { return true }

func (d *DBProvider) List(ctx context.Context, teamID string) ([]*model.WorkflowTemplate, error) {
	var templates []*model.WorkflowTemplate
	err := d.db.SelectContext(ctx, &templates,
		`SELECT id, team_id, slug, title, description, tags, yaml_body, created_at, updated_at
		 FROM workflow_templates WHERE team_id = $1 ORDER BY title`, teamID)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	return templates, nil
}

func (d *DBProvider) Get(ctx context.Context, teamID, slug string) (*model.WorkflowTemplate, error) {
	var t model.WorkflowTemplate
	err := d.db.GetContext(ctx, &t,
		`SELECT id, team_id, slug, title, description, tags, yaml_body, created_at, updated_at
		 FROM workflow_templates WHERE team_id = $1 AND slug = $2`, teamID, slug)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get template: %w", err)
	}
	return &t, nil
}

func (d *DBProvider) Save(ctx context.Context, teamID string, t *model.WorkflowTemplate) error {
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO workflow_templates (team_id, slug, title, description, tags, yaml_body)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (team_id, slug) DO UPDATE SET
		   title = EXCLUDED.title,
		   description = EXCLUDED.description,
		   tags = EXCLUDED.tags,
		   yaml_body = EXCLUDED.yaml_body,
		   updated_at = now()`,
		teamID, t.Slug, t.Title, t.Description, t.Tags, t.YAMLBody)
	if err != nil {
		return fmt.Errorf("save template: %w", err)
	}
	return nil
}

func (d *DBProvider) Delete(ctx context.Context, teamID, slug string) error {
	res, err := d.db.ExecContext(ctx,
		`DELETE FROM workflow_templates WHERE team_id = $1 AND slug = $2`, teamID, slug)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
