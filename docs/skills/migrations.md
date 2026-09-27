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
- **MUST** run migrations under a lock so N instances booting at once migrate exactly once:
  `pg_advisory_lock` on PostgreSQL, `BEGIN IMMEDIATE` on SQLite.
- **MUST** keep each migration in one transaction where the dialect allows it (PostgreSQL DDL is
  transactional). A statement that cannot run in a transaction (`CREATE INDEX CONCURRENTLY`) gets
  its own migration file with a `-- jollyroger:no-transaction` header.
- **MUST** ship an integration test for every migration: apply on an empty DB, apply again
  (idempotent), and apply concurrently from two goroutines (the lock holds).
- **MUST** update `protocol/storage-contract.md` when a migration adds anything SDKs may read, and
  bump the schema version recorded there.

## The runner contract (`internal/diplomat/migrate`)
- Tracks applied migrations in `jollyroger_schema_migrations(version, name, checksum, applied_at)`.
- Checksums are computed over the file content with line endings normalized to `\n`, so a Windows
  checkout with CRLF does not look like drift.
- Applies pending migrations in numeric order; on any error the transaction rolls back and `New`
  returns the error (fail closed, the host sees it at startup).
- Records the schema MAJOR version; SDKs refuse to start on an incompatible major and say why.
- Auto-migrate runs on `jollyroger.New` by default. `WithoutAutoMigrate()` disables it; then `New`
  verifies the schema is current and returns an error naming `jollyroger migrate` if it is not.
- `jollyroger migrate --print-sql --dialect postgres` prints the SQL for teams using their own
  migration tooling (goose, atlas, flyway).

## SQLite notes
- SQLite has no `ADD COLUMN IF NOT EXISTS`. The runner supports a `-- jollyroger:add-column table column`
  directive that checks `pragma_table_info` before running the `ALTER TABLE`.
- Recommend (document, do not force) WAL mode and `busy_timeout` on the host's connection; writes
  are rare, reads come from the in-memory snapshot.

## Why these rules exist
- An idempotent-only rule was learned the hard way in the source project: its runner re-ran every
  script on boot, and a non-idempotent `CREATE` bricked startup.
- A migration that looks right is not verified until it runs against a real database. Both
  dialects, every time.

## Related
- [testing-standards](testing-standards.md) · [diplomat-architecture](diplomat-architecture.md) ·
  [secure-by-design](secure-by-design.md)
