# Storage read contract (schema major 1)

An SDK evaluates flags from the host's database. This document lists what it MAY read. SDKs MUST
NOT write, and MUST NOT run migrations (the Go admin plane owns the schema).

## Compatibility
1. Read `schema_major` from `jollyroger_schema_info` (single row, `id = 1`). If it is not a major
   this SDK supports, refuse to start and name both versions.
2. Within a major, changes are additive only: new tables, new nullable columns, new rows. Ignore
   what you do not know.

## Tables an SDK may read

| Table | Columns |
|---|---|
| `jollyroger_schema_info` | `id`, `schema_major` |
| `jollyroger_projects` | `id`, `key` |
| `jollyroger_environments` | `id`, `project_id`, `key` |
| `jollyroger_flags` | `id`, `project_id`, `key`, `archived_at` |
| `jollyroger_flag_env_states` | `flag_id`, `environment_id`, `enabled`, `config`, `version` |
| `jollyroger_revisions` | `project_id`, `revision` |

On PostgreSQL the tables live in the schema the host configured (default `public`).

## Polling
Read `revision` for the project; only when it changed, load the snapshot. Every write transaction
increments the revision.

## Snapshot query
Run both reads in one read transaction (PostgreSQL: `REPEATABLE READ`; SQLite: one `BEGIN`) so the
revision matches the flags:

```sql
SELECT r.revision
FROM jollyroger_revisions r JOIN jollyroger_projects p ON p.id = r.project_id
WHERE p.key = :project;

SELECT f.key, s.enabled, s.version, s.config
FROM jollyroger_flags f
JOIN jollyroger_flag_env_states s ON s.flag_id = f.id
JOIN jollyroger_environments e ON e.id = s.environment_id
JOIN jollyroger_projects p ON p.id = f.project_id
WHERE p.key = :project AND e.key = :environment AND f.archived_at IS NULL
ORDER BY f.key;
```

## Column formats
- `config` is the protocol config JSON (`evaluation-spec.md` section 1), decoded with the rules of
  section 2: a value the SDK cannot decode makes that flag `PARSE_ERROR`, never the snapshot.
  PostgreSQL stores it as `JSONB` (read it as text).
- `enabled` is `BOOLEAN` on PostgreSQL and `INTEGER` 0/1 on SQLite.
- Keys compare by bytes (PostgreSQL columns use `COLLATE "C"`).
- Timestamps (not needed for evaluation) are `TIMESTAMPTZ` on PostgreSQL and UTC text in
  `2006-01-02T15:04:05.000000Z` form on SQLite.
