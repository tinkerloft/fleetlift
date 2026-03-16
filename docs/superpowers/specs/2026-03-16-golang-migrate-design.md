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
| `internal/db/migrations/001_initial.up.sql` | Renamed from `001_initial.sql`; existing full schema DDL unchanged |
| `internal/db/migrations/002_cost_tracking.up.sql` | The two `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` lines currently at the bottom of `schema.sql` |

`schema.sql` retains the `CREATE TABLE` blocks as a human-readable reference but loses the two `ALTER TABLE` lines — they move into `002_cost_tracking.up.sql`. `schema.sql` is no longer executed at runtime.

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

The `schema` embed variable and its import are removed.

### `internal/db/schema.sql`

Remove the two `ALTER TABLE` lines at the bottom (they move to `002_cost_tracking.up.sql`). The rest of the file is unchanged and serves as human-readable schema reference only.

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
