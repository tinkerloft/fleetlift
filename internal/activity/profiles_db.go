package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// DBProfileStore implements ProfileStore backed by PostgreSQL.
type DBProfileStore struct {
	DB *sqlx.DB
}

// GetProfile fetches a profile by team and name.
// It first checks for a team-scoped profile, then falls back to a system-level profile (team_id IS NULL).
func (s *DBProfileStore) GetProfile(ctx context.Context, teamID, name string) (*model.AgentProfile, error) {
	// Try team-scoped first.
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, team_id, name, description, body, created_at, updated_at
		   FROM agent_profiles
		  WHERE team_id = $1 AND name = $2`,
		teamID, name,
	)
	p, err := scanProfile(row)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("query team profile: %w", err)
	}
	if p != nil {
		return p, nil
	}

	// Fall back to system-level.
	row = s.DB.QueryRowContext(ctx,
		`SELECT id, team_id, name, description, body, created_at, updated_at
		   FROM agent_profiles
		  WHERE team_id IS NULL AND name = $1`,
		name,
	)
	p, err = scanProfile(row)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("query system profile: %w", err)
	}
	return p, nil
}

func scanProfile(row *sql.Row) (*model.AgentProfile, error) {
	var p model.AgentProfile
	var bodyJSON []byte
	err := row.Scan(&p.ID, &p.TeamID, &p.Name, &p.Description, &bodyJSON, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(bodyJSON, &p.Body); err != nil {
		return nil, fmt.Errorf("unmarshal profile body: %w", err)
	}
	return &p, nil
}
