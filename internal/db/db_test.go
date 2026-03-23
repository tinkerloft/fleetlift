package db

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnect(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set")
	}
	db, err := Connect(context.Background())
	require.NoError(t, err)
	require.NoError(t, db.Ping())
}

func TestMigrate(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set")
	}
	db, err := Connect(context.Background())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// First call: applies all pending migrations
	require.NoError(t, Migrate(db))

	// Second call: all migrations already applied — must not error
	require.NoError(t, Migrate(db))

	// golang-migrate creates a schema_migrations table with a single row
	// holding the highest applied version number (not one row per migration).
	var version int
	var dirty bool
	err = db.QueryRowContext(context.Background(),
		"SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty)
	require.NoError(t, err)
	require.Equal(t, 11, version) // version 11 = all migrations applied
	require.False(t, dirty)       // dirty=true means a migration failed mid-run
}

func TestMigrate_EnsuresStepRunLogsSeqIndexIsUnique(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set")
	}
	db, err := Connect(context.Background())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, Migrate(db))

	var unique bool
	err = db.QueryRowContext(context.Background(), `
		SELECT i.indisunique
		FROM pg_class c
		JOIN pg_index i ON i.indexrelid = c.oid
		WHERE c.relname = 'step_run_logs_step_seq'
	`).Scan(&unique)
	require.NoError(t, err)
	require.True(t, unique, "step_run_logs_step_seq must be unique for ON CONFLICT log inserts to work")
}
