package activity

import (
	"context"

	"github.com/jmoiron/sqlx"
	"github.com/tinkerloft/fleetlift/internal/agent"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

// CredentialStore resolves team-scoped credentials by name.
type CredentialStore interface {
	Get(ctx context.Context, teamID, name string) (string, error)
}

// Activities holds all Temporal activity implementations and their shared dependencies.
type Activities struct {
	Sandbox      sandbox.Client
	DB           *sqlx.DB
	CredStore    CredentialStore
	AgentRunners map[string]agent.Runner
}
