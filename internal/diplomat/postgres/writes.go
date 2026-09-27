package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	adb "github.com/joaomarcosfurtado/jollyroger/internal/adapter/db"
	"github.com/joaomarcosfurtado/jollyroger/internal/model"
)

// tx implements model.FlagTx inside a database/sql transaction.
type tx struct {
	s *Store
	q querier
}

func (t *tx) GetFlag(ctx context.Context, project, key string) (model.FlagWithStates, error) {
	return t.s.getFlag(ctx, t.q, project, key)
}

func (t *tx) CreateFlag(ctx context.Context, f model.NewFlag) (model.FlagWithStates, error) {
	pid, err := t.s.projectID(ctx, t.q, f.Project)
	if err != nil {
		return model.FlagWithStates{}, err
	}
	at := adb.NormalizeTime(f.At)
	res, err := t.q.ExecContext(ctx, t.s.sql(`INSERT INTO {schema}.jollyroger_flags (id, project_id, key, name, description, kind, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7) ON CONFLICT DO NOTHING`),
		f.ID, pid, f.Key, f.Name, f.Description, string(f.Kind), at)
	if err != nil {
		return model.FlagWithStates{}, fmt.Errorf("postgres: create flag %q: %w", f.Key, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return model.FlagWithStates{}, fmt.Errorf("postgres: create flag %q: %w", f.Key, err)
	}
	if n == 0 {
		return model.FlagWithStates{}, fmt.Errorf("flag %q (id %s): key or id already used: %w", f.Key, f.ID, model.ErrAlreadyExists)
	}
	config, err := adb.ConfigToJSON(model.FlagConfig{})
	if err != nil {
		return model.FlagWithStates{}, err
	}
	if _, err := t.q.ExecContext(ctx, t.s.sql(`INSERT INTO {schema}.jollyroger_flag_env_states (flag_id, environment_id, enabled, config, version, updated_at, updated_by)
		SELECT $1, e.id, FALSE, $2::jsonb, 1, $3, $4 FROM {schema}.jollyroger_environments e WHERE e.project_id = $5`),
		f.ID, config, at, f.Actor, pid); err != nil {
		return model.FlagWithStates{}, fmt.Errorf("postgres: create states of %q: %w", f.Key, err)
	}
	return t.s.getFlag(ctx, t.q, f.Project, f.Key)
}

func (t *tx) UpdateFlagMeta(ctx context.Context, project, key string, m model.FlagMeta) (model.Flag, error) {
	return t.updateFlag(ctx, project, key, `UPDATE {schema}.jollyroger_flags SET name = $3, description = $4, updated_at = $5 WHERE project_id = $1 AND key = $2`,
		m.Name, m.Description, adb.NormalizeTime(m.At))
}

func (t *tx) ArchiveFlag(ctx context.Context, project, key string, at time.Time) (model.Flag, error) {
	return t.updateFlag(ctx, project, key, `UPDATE {schema}.jollyroger_flags SET archived_at = $3, updated_at = $3 WHERE project_id = $1 AND key = $2 AND archived_at IS NULL`,
		adb.NormalizeTime(at))
}

func (t *tx) RestoreFlag(ctx context.Context, project, key string, at time.Time) (model.Flag, error) {
	return t.updateFlag(ctx, project, key, `UPDATE {schema}.jollyroger_flags SET archived_at = NULL, updated_at = $3 WHERE project_id = $1 AND key = $2 AND archived_at IS NOT NULL`,
		adb.NormalizeTime(at))
}

// updateFlag runs stmt with ($1 project_id, $2 key, args...) and returns the flag as stored. An
// UPDATE that matched nothing is fine (archive/restore are idempotent); a missing flag is not.
func (t *tx) updateFlag(ctx context.Context, project, key, stmt string, args ...any) (model.Flag, error) {
	pid, err := t.s.projectID(ctx, t.q, project)
	if err != nil {
		return model.Flag{}, err
	}
	if _, err := t.q.ExecContext(ctx, t.s.sql(stmt), append([]any{pid, key}, args...)...); err != nil {
		return model.Flag{}, fmt.Errorf("postgres: update flag %q: %w", key, err)
	}
	f, err := t.s.flagRow(ctx, t.q, pid, key)
	if err != nil {
		return model.Flag{}, err
	}
	return adb.RowToFlag(f), nil
}

func (t *tx) SetEnvState(ctx context.Context, project, key, environment string, c model.EnvStateChange, expectedVersion int64) (model.EnvState, error) {
	if c.Config.Unparseable {
		return model.EnvState{}, fmt.Errorf("flag %q in %q: refusing to store a config this version cannot read (it would destroy the stored rules): %w", key, environment, model.ErrInvalid)
	}
	pid, err := t.s.projectID(ctx, t.q, project)
	if err != nil {
		return model.EnvState{}, err
	}
	f, err := t.s.flagRow(ctx, t.q, pid, key)
	if err != nil {
		return model.EnvState{}, err
	}
	eid, err := t.s.environmentID(ctx, t.q, pid, environment)
	if err != nil {
		return model.EnvState{}, err
	}
	config, err := adb.ConfigToJSON(c.Config)
	if err != nil {
		return model.EnvState{}, err
	}
	res, err := t.q.ExecContext(ctx, t.s.sql(`UPDATE {schema}.jollyroger_flag_env_states
		SET enabled = $1, config = $2::jsonb, version = version + 1, updated_at = $3, updated_by = $4
		WHERE flag_id = $5 AND environment_id = $6 AND version = $7`),
		c.Enabled, config, adb.NormalizeTime(c.At), c.Actor, f.ID, eid, expectedVersion)
	if err != nil {
		return model.EnvState{}, fmt.Errorf("postgres: set state of %q in %q: %w", key, environment, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return model.EnvState{}, fmt.Errorf("postgres: set state of %q in %q: %w", key, environment, err)
	}
	if n == 0 {
		return model.EnvState{}, fmt.Errorf("flag %q in %q is not at version %d: %w", key, environment, expectedVersion, model.ErrConflict)
	}
	full, err := t.s.getFlag(ctx, t.q, project, key)
	if err != nil {
		return model.EnvState{}, err
	}
	for _, st := range full.States {
		if st.EnvironmentKey == environment {
			return st, nil
		}
	}
	return model.EnvState{}, fmt.Errorf("postgres: state of %q in %q vanished: %w", key, environment, model.ErrNotFound)
}

func (t *tx) AppendAudit(ctx context.Context, e model.AuditEntry) error {
	pid, err := t.s.projectID(ctx, t.q, e.Project)
	if err != nil {
		return err
	}
	before, err := adb.AuditJSON(e.Before)
	if err != nil {
		return err
	}
	after, err := adb.AuditJSON(e.After)
	if err != nil {
		return err
	}
	res, err := t.q.ExecContext(ctx, t.s.sql(`INSERT INTO {schema}.jollyroger_audit_log
		(id, environment_key, flag_key, actor_id, actor_name, action, before_state, after_state, reason, created_at, project_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9, $10, $11) ON CONFLICT (id) DO NOTHING`),
		e.ID, adb.NullIfEmpty(e.EnvironmentKey), adb.NullIfEmpty(e.FlagKey), e.ActorID, e.ActorName, string(e.Action),
		before, after, adb.NullIfEmpty(e.Reason), adb.NormalizeTime(e.CreatedAt), pid)
	if err != nil {
		return fmt.Errorf("postgres: append audit %s: %w", e.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres: append audit %s: %w", e.ID, err)
	}
	if n == 0 {
		return fmt.Errorf("audit id %s: %w", e.ID, model.ErrAlreadyExists)
	}
	return nil
}

func (t *tx) BumpRevision(ctx context.Context, project string) (int64, error) {
	pid, err := t.s.projectID(ctx, t.q, project)
	if err != nil {
		return 0, err
	}
	var rev int64
	err = t.q.QueryRowContext(ctx, t.s.sql(`UPDATE {schema}.jollyroger_revisions SET revision = revision + 1 WHERE project_id = $1 RETURNING revision`), pid).Scan(&rev)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("revision of %q: %w", project, model.ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("postgres: bump revision of %q: %w", project, err)
	}
	return rev, nil
}
