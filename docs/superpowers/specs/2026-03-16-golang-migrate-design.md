# golang-migrate Integration Design

**Date:** 2026-03-16
**Status:** Approved

## Goal

Replace the current `db.Migrate()` implementation (which executes `schema.sql` verbatim) with golang-migrate v4, using an embedded `migrations/` directory as the source. Migrations are applied automatically at server and worker startup; applied versions are tracked in a `schema_migrations` table managed by golang-migrate.

## Context

The project already has:
- `internal/db/db.go` — `Connect()` + `Migrate()` called at startup in both `cmd/server/main.go` and `cmd/worker/main.go`
- `internal/db/migrations/001_initial.sql` — baseline schema (all `CREATE TABLE IF NOT EXISTS` DDL)
- `internal/db/schema.sql` — embedded via `//go:embed`, currently executed by `Migrate()`; has two `ALTER TABLE` lines appended at the bottom for recent cost tracking columns

No call sites change — only the `Migrate()` implementation is replaced.

## Migration Files

| File | Content |
|------|---------|
| `internal/db/migrations/001_initial.up.sql` | Renamed from `001_initial.sql`; existing full schema DDL unchanged — all `CREATE TABLE IF NOT EXISTS` blocks and their associated indexes (lines 1–180 of `schema.sql`) |
| `internal/db/migrations/002_post_initial.up.sql` | Incremental DDL added 2026-03-12 and 2026-03-15 — currently lives in `schema.sql` lines 181–232: `temporal_workflow_id` column, 3 indexes, `notify_run_event()` function + 3 triggers, `credentials.team_id` nullability change + 2 partial unique indexes |
| `internal/db/migrations/003_cost_tracking.up.sql` | The two `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` lines at the very bottom of `schema.sql` (lines 235–236): `step_runs.cost_usd` and `runs.total_cost_usd` |

`schema.sql` retains all `CREATE TABLE` blocks as a human-readable reference. Lines 181–236 (all incremental DDL) are removed from `schema.sql` since they are now covered by `002_post_initial.up.sql` and `003_cost_tracking.up.sql`. `schema.sql` is no longer executed at runtime.

No down migration files — up-only.

## Dependencies

Add to `go.mod`:
```
github.com/golang-migrate/migrate/v4
```

Sub-packages used:
- `github.com/golang-migrate/migrate/v4/source/iofs` — read migrations from `embed.FS`
- `github.com/golang-migrate/migrate/v4/database/postgres` — postgres driver for golang-migrate

## Implementation

### `internal/db/db.go`

Replace the existing `schema` embed and `Migrate()` with:

```go
import (
    "embed"
    "errors"
    "fmt"

    "github.com/golang-migrate/migrate/v4"
    "github.com/golang-migrate/migrate/v4/database/postgres"
    "github.com/golang-migrate/migrate/v4/source/iofs"
    "github.com/jmoiron/sqlx"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

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

`db.DB` unwraps the `*sql.DB` from `*sqlx.DB` — reuses the existing connection pool, no second connection needed.

The existing `schema` embed variable, its `//go:embed schema.sql` directive, and the `_ "embed"` blank import (if present) are removed and replaced with the `migrationFiles` embed above. The `"errors"` package must be added to imports.

### `internal/db/schema.sql`

Remove lines 181–236 (all incremental DDL — `temporal_workflow_id`, indexes, triggers, credentials nullability changes, and cost columns). These move into `002_post_initial.up.sql` and `003_cost_tracking.up.sql`. The remaining file (lines 1–180) serves as human-readable schema reference only and is no longer executed at runtime.

Note: the DO block in `002_post_initial.up.sql` (credentials nullability guard) may be simplified to a plain `ALTER TABLE credentials ALTER COLUMN team_id DROP NOT NULL;` since golang-migrate's version tracking provides idempotency — the migration runs exactly once.

## Error Handling

- `migrate.ErrNoChange` is not an error — swallowed silently
- All other errors from `m.Up()` are fatal (server/worker calls `log.Fatalf` on `Migrate()` error)
- golang-migrate uses an advisory lock to prevent concurrent migration runs

## Testing

Extend `internal/db/db_test.go`:
- Call `Migrate()` on a clean DB — assert no error
- Call `Migrate()` again — assert no error (idempotency, `ErrNoChange` swallowed)

Test skips if `DATABASE_URL` is unset (existing behaviour preserved).

## What Does Not Change

- `cmd/server/main.go` — call site unchanged
- `cmd/worker/main.go` — call site unchanged
- All handler, activity, and workflow code — no changes
- Future migrations: add `NNN_description.up.sql` to `internal/db/migrations/`, rebuild
