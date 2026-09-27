# Skill: Migrations

**Read when:** writing or changing any SQL migration, or touching `internal/diplomat/migrate`.

jollyroger's tables live in *someone else's* database, next to their tables, and are read by SDKs in
other languages. That makes migrations far more dangerous than in an ordinary application.

## Rules
- **MUST** write every migration for BOTH dialects, in
  `internal/diplomat/postgres/migrations/NNNN_name.sql` and
  `internal/diplomat/sqlite/migrations/NNNN_name.sql`, with the same number and name.
- **MUST** make every statement idempotent: `CREATE TABLE IF NOT EXISTS`,
  `CREATE INDEX IF NOT EXISTS`, `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` (PostgreSQL; SQLite needs
  the column-exists check done by the runner, see below), `CREATE OR REPLACE` for functions,
  `DROP TRIGGER IF EXISTS` before `CREATE TRIGGER`.
- **MUST** prefix every object with `jollyroger_` (tables, indexes, triggers, functions). When the
  host configures `WithSchema`, PostgreSQL objects go into that schema instead of the prefix-only
  default; never create anything outside our prefix or schema.
- **MUST** be additive within a major version: add tables, add nullable columns or columns with a
  default, add indexes. **MUST NOT** drop, rename, or change the type of a column, or tighten a
  constraint, in a minor release. Rolling deploys run the old and new code against one schema, and
  SDKs in other languages read these tables (`protocol/storage-contract.md`).
- **MUST NOT** edit a migration after it has been released. The runner stores a checksum and refuses
  to start on drift. Fix forward with a new migration.
- **MUST** run migrations under a lock so N instances booting at once migrate exactly once. A run
  is ONE transaction under the lock (`pg_advisory_xact_lock` on PostgreSQL, `BEGIN IMMEDIATE` on
  SQLite): all pending migrations apply, or none do.
- **MUST NOT** write a migration that cannot run inside a transaction (`CREATE INDEX CONCURRENTLY`)
  until the runner supports a `-- jollyroger:no-transaction` directive; add that support, with
  tests, in the same change as the first migration that needs it.
- **MUST** ship an integration test for every migration: apply on an empty DB, apply again
  (idempotent), and apply concurrently from two goroutines (the lock holds).
- **MUST** update `protocol/storage-contract.md` when a migration adds anything SDKs may read, and
  bump the schema version recorded there.

## The runner contract (`internal/diplomat/migrate`)
- Tracks applied migrations in `jollyroger_schema_migrations(version, name, checksum, applied_at)`.
- Checksums are computed over the file content with line endings normalized to `\n`, so a Windows
  checkout with CRLF does not look like drift.
- Applies pending migrations in numeric order in one transaction; on any error everything rolls
  back and `New` returns the error (fail closed, the host sees it at startup).
- Ignores recorded versions it does not know (a newer build migrated first during a rolling
  deploy): migrations are additive, so older code keeps working.
- Migration 0001 records the schema MAJOR version in `jollyroger_schema_info`; SDKs refuse to start
  on an incompatible major and say why (`protocol/storage-contract.md`).
- Auto-migrate runs on `jollyroger.New` by default. `WithoutAutoMigrate()` disables it; then `New`
  verifies the schema is current and returns an error naming `jollyroger migrate` if it is not.
- `jollyroger migrate --print-sql --dialect postgres` prints the SQL for teams using their own
  migration tooling (goose, atlas, flyway).

## SQLite notes
- SQLite has no `ADD COLUMN IF NOT EXISTS`. The first SQLite migration that adds a column must also
  add a runner directive (for example `-- jollyroger:add-column table column`, checking
  `pragma_table_info` first), with tests, in the same change.
- Recommend (document, do not force) WAL mode and a `busy_timeout` on the host's connection. The
  store raises a `busy_timeout` of 0 to 5 seconds on the connections it uses, so concurrent writers
  wait instead of failing with `SQLITE_BUSY`.

## Why these rules exist
- An idempotent-only rule was learned the hard way in the source project: its runner re-ran every
  script on boot, and a non-idempotent `CREATE` bricked startup.
- A migration that looks right is not verified until it runs against a real database. Both
  dialects, every time.

## Related
- [testing-standards](testing-standards.md) · [diplomat-architecture](diplomat-architecture.md) ·
  [secure-by-design](secure-by-design.md)
