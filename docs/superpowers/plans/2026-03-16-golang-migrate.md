# golang-migrate Integration Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current raw `db.Exec(schema)` migration with golang-migrate v4 using an embedded `migrations/` directory, so schema changes are versioned, tracked, and applied automatically at server/worker startup.

**Architecture:** Three versioned `.up.sql` files cover the full schema history; golang-migrate applies them in order on first run and tracks applied versions in a `schema_migrations` table. `db.Migrate(*sqlx.DB)` is the only function that changes — call sites in `cmd/server/main.go` and `cmd/worker/main.go` are untouched.

**Tech Stack:** Go, `github.com/golang-migrate/migrate/v4`, `source/iofs` (embed.FS), `database/postgres`, PostgreSQL

---

## File Map

| Action | File | Purpose |
|--------|------|---------|
| Rename | `internal/db/migrations/001_initial.sql` → `001_initial.up.sql` | Baseline schema (all CREATE TABLE blocks) |
| Create | `internal/db/migrations/002_post_initial.up.sql` | Incremental DDL from 2026-03-12/15 (column, indexes, triggers, credentials nullability) |
| Create | `internal/db/migrations/003_cost_tracking.up.sql` | cost_usd columns added 2026-03-16 |
| Modify | `internal/db/db.go` | Replace Migrate() to use golang-migrate; update embed and imports |
| Modify | `internal/db/schema.sql` | Remove lines 181–236 (now in migration files) |
| Modify | `internal/db/db_test.go` | Add Migrate() idempotency test |
| Modify | `go.mod` / `go.sum` | Add golang-migrate/migrate/v4 dependency |

---

## Task 1: Create versioned migration files

**Files:**
- Rename: `internal/db/migrations/001_initial.sql` → `internal/db/migrations/001_initial.up.sql`
- Create: `internal/db/migrations/002_post_initial.up.sql`
- Create: `internal/db/migrations/003_cost_tracking.up.sql`

- [ ] **Step 1: Rename the existing migration file**

```bash
mv internal/db/migrations/001_initial.sql internal/db/migrations/001_initial.up.sql
```

golang-migrate requires the filename pattern `{version}_{title}.up.sql`. The content is unchanged.

- [ ] **Step 2: Create `002_post_initial.up.sql`**

This covers all incremental DDL currently in `schema.sql` lines 181–232. The `DO $$` guard is replaced with a plain `ALTER TABLE` — golang-migrate's version tracking ensures this runs exactly once.

Create `internal/db/migrations/002_post_initial.up.sql`:

```sql
-- Added 2026-03-12: temporal_workflow_id for HITL signal routing
ALTER TABLE step_runs ADD COLUMN IF NOT EXISTS temporal_workflow_id TEXT;

-- Added 2026-03-12: performance indexes
CREATE INDEX IF NOT EXISTS step_run_logs_stream_cursor
    ON step_run_logs (step_run_id, id);

CREATE INDEX IF NOT EXISTS runs_team_created
    ON runs (team_id, created_at DESC);

CREATE INDEX IF NOT EXISTS runs_team_completed
    ON runs (team_id, status, completed_at DESC);

-- Added 2026-03-12: LISTEN/NOTIFY for SSE streaming
CREATE OR REPLACE FUNCTION notify_run_event() RETURNS trigger AS $$
BEGIN
  IF TG_TABLE_NAME = 'step_run_logs' THEN
    PERFORM pg_notify('run_events',
      (SELECT run_id::text FROM step_runs WHERE id = NEW.step_run_id));
  ELSIF TG_TABLE_NAME = 'step_runs' THEN
    PERFORM pg_notify('run_events', NEW.run_id::text);
  ELSIF TG_TABLE_NAME = 'runs' THEN
    PERFORM pg_notify('run_events', NEW.id::text);
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER step_run_logs_notify
  AFTER INSERT ON step_run_logs FOR EACH ROW EXECUTE FUNCTION notify_run_event();
CREATE OR REPLACE TRIGGER step_runs_notify
  AFTER UPDATE OF status ON step_runs FOR EACH ROW EXECUTE FUNCTION notify_run_event();
CREATE OR REPLACE TRIGGER runs_notify
  AFTER UPDATE OF status ON runs FOR EACH ROW EXECUTE FUNCTION notify_run_event();

-- Added 2026-03-15: allow system-wide credentials (team_id = NULL)
ALTER TABLE credentials ALTER COLUMN team_id DROP NOT NULL;
ALTER TABLE credentials DROP CONSTRAINT IF EXISTS credentials_team_id_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS credentials_team_name_unique
  ON credentials (team_id, name) WHERE team_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS credentials_system_name_unique
  ON credentials (name) WHERE team_id IS NULL;
```

- [ ] **Step 3: Create `003_cost_tracking.up.sql`**

Create `internal/db/migrations/003_cost_tracking.up.sql`:

```sql
-- Added 2026-03-16: per-step and per-run cost tracking
ALTER TABLE step_runs ADD COLUMN IF NOT EXISTS cost_usd NUMERIC(10,6);
ALTER TABLE runs      ADD COLUMN IF NOT EXISTS total_cost_usd NUMERIC(10,6);
```

- [ ] **Step 4: Verify file listing**

```bash
ls internal/db/migrations/
```

Expected output:
```
001_initial.up.sql
002_post_initial.up.sql
003_cost_tracking.up.sql
```

- [ ] **Step 5: Commit**

```bash
git add internal/db/migrations/
git commit -m "feat(db): add versioned migration files for golang-migrate"
```

---

## Task 2: Wire golang-migrate into db.Migrate()

**Files:**
- Modify: `internal/db/db_test.go`
- Modify: `internal/db/db.go`
- Modify: `internal/db/schema.sql`
- Modify: `go.mod`, `go.sum` (via `go get`)

### Step 2a: Failing test first

- [ ] **Step 1: Write the failing test in `internal/db/db_test.go`**

Add after `TestConnect`:

```go
func TestMigrate(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set")
	}
	db, err := Connect(context.Background())
	require.NoError(t, err)
	defer db.Close()

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
	require.Equal(t, 3, version) // version 3 = all three migrations applied
	require.False(t, dirty)      // dirty=true means a migration failed mid-run
}
```

- [ ] **Step 2: Run the test — verify it fails**

```bash
DATABASE_URL=postgres://fleetlift:fleetlift@localhost:5432/fleetlift \
  go test ./internal/db/ -run TestMigrate -v
```

Expected: FAIL — `schema_migrations` table does not exist with the current `db.Exec(schema)` implementation.

### Step 2b: Add the dependency

- [ ] **Step 3: Add golang-migrate**

```bash
go get github.com/golang-migrate/migrate/v4
go get github.com/golang-migrate/migrate/v4/database/postgres
go get github.com/golang-migrate/migrate/v4/source/iofs
```

- [ ] **Step 4: Tidy**

```bash
go mod tidy
```

### Step 2c: Replace Migrate()

- [ ] **Step 5: Rewrite `internal/db/db.go`**

Replace the entire file with:

```go
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func Connect(ctx context.Context) (*sqlx.DB, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is required")
	}
	db, err := sqlx.ConnectContext(ctx, "postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
	return db, nil
}

func Migrate(db *sqlx.DB) error {
	src, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("open migrations: %w", err)
	}
	driver, err := postgres.WithInstance(db.DB, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("migration driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}
```

- [ ] **Step 6: Run the test — verify it passes**

```bash
DATABASE_URL=postgres://fleetlift:fleetlift@localhost:5432/fleetlift \
  go test ./internal/db/ -run TestMigrate -v
```

Expected: PASS — `schema_migrations` table now exists with 1 row: `version=3, dirty=false`.

- [ ] **Step 7: Run all db tests**

```bash
DATABASE_URL=postgres://fleetlift:fleetlift@localhost:5432/fleetlift \
  go test ./internal/db/ -v
```

Expected: all tests PASS.

### Step 2d: Clean up schema.sql

- [ ] **Step 8: Remove incremental DDL from `internal/db/schema.sql`**

Delete everything from `-- Added 2026-03-12` through the end of the file (lines 181–236). This covers both the content now in `002_post_initial.up.sql` (lines 181–232) and `003_cost_tracking.up.sql` (lines 235–236). The file should end at the `knowledge_items` indexes.

Verify the file ends correctly:
```bash
tail -5 internal/db/schema.sql
```

Expected last lines (approximately):
```sql
CREATE INDEX IF NOT EXISTS knowledge_items_team_status ON knowledge_items(team_id, status);
CREATE INDEX IF NOT EXISTS knowledge_items_workflow ON knowledge_items(workflow_template_id, status);
```

- [ ] **Step 9: Build check**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 10: Run full test suite**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 11: Lint**

```bash
make lint
```

Expected: no errors.

- [ ] **Step 12: Commit**

```bash
git add internal/db/db.go internal/db/db_test.go internal/db/schema.sql go.mod go.sum
git commit -m "feat(db): replace raw schema exec with golang-migrate versioned migrations"
```

---

## Pre-merge Checklist

- [ ] `go build ./...` passes
- [ ] `go test ./...` passes (with `DATABASE_URL` set for `internal/db/` tests)
- [ ] `make lint` passes
- [ ] `ls internal/db/migrations/` shows exactly three `.up.sql` files
- [ ] `schema.sql` no longer contains any `ALTER TABLE` or `CREATE OR REPLACE` statements
- [ ] `schema_migrations` table exists in local DB with 3 rows after running the server/worker once
