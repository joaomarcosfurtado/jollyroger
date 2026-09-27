-- jollyroger schema 1 (SQLite). Idempotent: every statement may run again safely.
-- Timestamps are text in 2006-01-02T15:04:05.000000Z (UTC, lexical order = chronological order).

CREATE TABLE IF NOT EXISTS jollyroger_schema_info (
    id           INTEGER PRIMARY KEY CHECK (id = 1),
    schema_major INTEGER NOT NULL
);
INSERT INTO jollyroger_schema_info (id, schema_major) VALUES (1, 1) ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS jollyroger_projects (
    id         TEXT PRIMARY KEY,
    key        TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS jollyroger_environments (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES jollyroger_projects (id),
    key        TEXT NOT NULL,
    name       TEXT NOT NULL,
    position   INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (project_id, key)
);

CREATE TABLE IF NOT EXISTS jollyroger_flags (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES jollyroger_projects (id),
    key         TEXT NOT NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    archived_at TEXT,
    UNIQUE (project_id, key)
);

CREATE TABLE IF NOT EXISTS jollyroger_flag_env_states (
    flag_id        TEXT NOT NULL REFERENCES jollyroger_flags (id),
    environment_id TEXT NOT NULL REFERENCES jollyroger_environments (id),
    enabled        INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    config         TEXT NOT NULL,
    version        INTEGER NOT NULL CHECK (version >= 1),
    updated_at     TEXT NOT NULL,
    updated_by     TEXT NOT NULL,
    PRIMARY KEY (flag_id, environment_id)
);

CREATE TABLE IF NOT EXISTS jollyroger_revisions (
    project_id TEXT PRIMARY KEY REFERENCES jollyroger_projects (id),
    revision   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS jollyroger_audit_log (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES jollyroger_projects (id),
    environment_key TEXT,
    flag_key        TEXT,
    actor_id        TEXT NOT NULL,
    actor_name      TEXT NOT NULL,
    action          TEXT NOT NULL,
    before_state    TEXT,
    after_state     TEXT,
    reason          TEXT,
    created_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS jollyroger_audit_log_project_id ON jollyroger_audit_log (project_id, id DESC);

CREATE TABLE IF NOT EXISTS jollyroger_audit_prune_guard (
    id     INTEGER PRIMARY KEY CHECK (id = 1),
    active INTEGER NOT NULL CHECK (active IN (0, 1))
);
INSERT INTO jollyroger_audit_prune_guard (id, active) VALUES (1, 0) ON CONFLICT (id) DO NOTHING;

CREATE TRIGGER IF NOT EXISTS jollyroger_audit_log_no_update
BEFORE UPDATE ON jollyroger_audit_log
BEGIN
    SELECT RAISE(ABORT, 'jollyroger_audit_log is append-only');
END;

CREATE TRIGGER IF NOT EXISTS jollyroger_audit_log_no_delete
BEFORE DELETE ON jollyroger_audit_log
WHEN NOT EXISTS (SELECT 1 FROM jollyroger_audit_prune_guard WHERE id = 1 AND active = 1)
BEGIN
    SELECT RAISE(ABORT, 'jollyroger_audit_log is append-only');
END;

INSERT INTO jollyroger_projects (id, key, name, created_at)
VALUES ('00000000000000000000000000', 'default', 'Default', '2026-01-01T00:00:00.000000Z')
ON CONFLICT (id) DO NOTHING;

INSERT INTO jollyroger_environments (id, project_id, key, name, position, created_at) VALUES
    ('00000000000000000000000001', '00000000000000000000000000', 'development', 'Development', 1, '2026-01-01T00:00:00.000000Z'),
    ('00000000000000000000000002', '00000000000000000000000000', 'staging', 'Staging', 2, '2026-01-01T00:00:00.000000Z'),
    ('00000000000000000000000003', '00000000000000000000000000', 'production', 'Production', 3, '2026-01-01T00:00:00.000000Z')
ON CONFLICT (id) DO NOTHING;

INSERT INTO jollyroger_revisions (project_id, revision)
VALUES ('00000000000000000000000000', 0)
ON CONFLICT (project_id) DO NOTHING;
