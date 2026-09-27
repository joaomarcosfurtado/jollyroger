-- jollyroger schema 1 (PostgreSQL 12+). Idempotent: every statement may run again safely.
-- {schema} is replaced with the quoted schema name. Keys and IDs use COLLATE "C" (byte order),
-- so ordering and keyset pagination match every other store and SDK.

CREATE TABLE IF NOT EXISTS {schema}.jollyroger_schema_info (
    id           INTEGER PRIMARY KEY CHECK (id = 1),
    schema_major INTEGER NOT NULL
);
INSERT INTO {schema}.jollyroger_schema_info (id, schema_major) VALUES (1, 1) ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS {schema}.jollyroger_projects (
    id         TEXT COLLATE "C" PRIMARY KEY,
    key        TEXT COLLATE "C" NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS {schema}.jollyroger_environments (
    id         TEXT COLLATE "C" PRIMARY KEY,
    project_id TEXT COLLATE "C" NOT NULL REFERENCES {schema}.jollyroger_projects (id),
    key        TEXT COLLATE "C" NOT NULL,
    name       TEXT NOT NULL,
    position   INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (project_id, key)
);

CREATE TABLE IF NOT EXISTS {schema}.jollyroger_flags (
    id          TEXT COLLATE "C" PRIMARY KEY,
    project_id  TEXT COLLATE "C" NOT NULL REFERENCES {schema}.jollyroger_projects (id),
    key         TEXT COLLATE "C" NOT NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    archived_at TIMESTAMPTZ,
    UNIQUE (project_id, key)
);

CREATE TABLE IF NOT EXISTS {schema}.jollyroger_flag_env_states (
    flag_id        TEXT COLLATE "C" NOT NULL REFERENCES {schema}.jollyroger_flags (id),
    environment_id TEXT COLLATE "C" NOT NULL REFERENCES {schema}.jollyroger_environments (id),
    enabled        BOOLEAN NOT NULL,
    config         JSONB NOT NULL,
    version        BIGINT NOT NULL CHECK (version >= 1),
    updated_at     TIMESTAMPTZ NOT NULL,
    updated_by     TEXT NOT NULL,
    PRIMARY KEY (flag_id, environment_id)
);

CREATE TABLE IF NOT EXISTS {schema}.jollyroger_revisions (
    project_id TEXT COLLATE "C" PRIMARY KEY REFERENCES {schema}.jollyroger_projects (id),
    revision   BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS {schema}.jollyroger_audit_log (
    id              TEXT COLLATE "C" PRIMARY KEY,
    project_id      TEXT COLLATE "C" NOT NULL REFERENCES {schema}.jollyroger_projects (id),
    environment_key TEXT COLLATE "C",
    flag_key        TEXT COLLATE "C",
    actor_id        TEXT NOT NULL,
    actor_name      TEXT NOT NULL,
    action          TEXT NOT NULL,
    before_state    JSONB,
    after_state     JSONB,
    reason          TEXT,
    created_at      TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS jollyroger_audit_log_project_id ON {schema}.jollyroger_audit_log (project_id, id DESC);

CREATE TABLE IF NOT EXISTS {schema}.jollyroger_audit_prune_guard (
    id     INTEGER PRIMARY KEY CHECK (id = 1),
    active BOOLEAN NOT NULL
);
INSERT INTO {schema}.jollyroger_audit_prune_guard (id, active) VALUES (1, FALSE) ON CONFLICT (id) DO NOTHING;

CREATE OR REPLACE FUNCTION {schema}.jollyroger_audit_log_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' AND EXISTS (SELECT 1 FROM {schema}.jollyroger_audit_prune_guard WHERE id = 1 AND active) THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'jollyroger_audit_log is append-only';
END
$$;

DROP TRIGGER IF EXISTS jollyroger_audit_log_guard ON {schema}.jollyroger_audit_log;
CREATE TRIGGER jollyroger_audit_log_guard
    BEFORE UPDATE OR DELETE ON {schema}.jollyroger_audit_log
    FOR EACH ROW EXECUTE FUNCTION {schema}.jollyroger_audit_log_guard();

DROP TRIGGER IF EXISTS jollyroger_audit_log_no_truncate ON {schema}.jollyroger_audit_log;
CREATE TRIGGER jollyroger_audit_log_no_truncate
    BEFORE TRUNCATE ON {schema}.jollyroger_audit_log
    FOR EACH STATEMENT EXECUTE FUNCTION {schema}.jollyroger_audit_log_guard();

INSERT INTO {schema}.jollyroger_projects (id, key, name, created_at)
VALUES ('00000000000000000000000000', 'default', 'Default', '2026-01-01T00:00:00Z')
ON CONFLICT (id) DO NOTHING;

INSERT INTO {schema}.jollyroger_environments (id, project_id, key, name, position, created_at) VALUES
    ('00000000000000000000000001', '00000000000000000000000000', 'development', 'Development', 1, '2026-01-01T00:00:00Z'),
    ('00000000000000000000000002', '00000000000000000000000000', 'staging', 'Staging', 2, '2026-01-01T00:00:00Z'),
    ('00000000000000000000000003', '00000000000000000000000000', 'production', 'Production', 3, '2026-01-01T00:00:00Z')
ON CONFLICT (id) DO NOTHING;

INSERT INTO {schema}.jollyroger_revisions (project_id, revision)
VALUES ('00000000000000000000000000', 0)
ON CONFLICT (project_id) DO NOTHING;
