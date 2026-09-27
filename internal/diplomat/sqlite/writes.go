package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	adb "github.com/joaomarcosfurtado/jollyroger/internal/adapter/db"
	"github.com/joaomarcosfurtado/jollyroger/internal/model"
)

// tx implements model.FlagTx on one connection inside BEGIN IMMEDIATE.
type tx struct{ q querier }

func (t *tx) GetFlag(ctx context.Context, project, key string) (model.FlagWithStates, error) {
	return getFlag(ctx, t.q, project, key)
}

func (t *tx) CreateFlag(ctx context.Context, f model.NewFlag) (model.FlagWithStates, error) {
	pid, err := projectID(ctx, t.q, f.Project)
	if err != nil {
		return model.FlagWithStates{}, err
	}
	at := adb.TextTime(f.At)
	res, err := t.q.ExecContext(ctx, `INSERT INTO jollyroger_flags (id, project_id, key, name, description, kind, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		f.ID, pid, f.Key, f.Name, f.Description, string(f.Kind), at, at)
	if err != nil {
		return model.FlagWithStates{}, fmt.Errorf("sqlite: create flag %q: %w", f.Key, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return model.FlagWithStates{}, fmt.Errorf("sqlite: create flag %q: %w", f.Key, err)
	}
	if n == 0 {
		return model.FlagWithStates{}, fmt.Errorf("flag %q (id %s): key or id already used: %w", f.Key, f.ID, model.ErrAlreadyExists)
	}
	config, err := adb.ConfigToJSON(model.FlagConfig{})
	if err != nil {
		return model.FlagWithStates{}, err
	}
	if _, err := t.q.ExecContext(ctx, `INSERT INTO jollyroger_flag_env_states (flag_id, environment_id, enabled, config, version, updated_at, updated_by)
		SELECT ?, e.id, 0, ?, 1, ?, ? FROM jollyroger_environments e WHERE e.project_id = ?`,
		f.ID, config, at, f.Actor, pid); err != nil {
		return model.FlagWithStates{}, fmt.Errorf("sqlite: create states of %q: %w", f.Key, err)
	}
	return getFlag(ctx, t.q, f.Project, f.Key)
}

func (t *tx) UpdateFlagMeta(ctx context.Context, project, key string, m model.FlagMeta) (model.Flag, error) {
	return t.updateFlag(ctx, project, key, `UPDATE jollyroger_flags SET name = ?, description = ?, updated_at = ? WHERE project_id = ? AND key = ?`,
		m.Name, m.Description, adb.TextTime(m.At))
}

func (t *tx) ArchiveFlag(ctx context.Context, project, key string, at time.Time) (model.Flag, error) {
	ts := adb.TextTime(at)
	return t.updateFlag(ctx, project, key, `UPDATE jollyroger_flags SET archived_at = ?, updated_at = ? WHERE project_id = ? AND key = ? AND archived_at IS NULL`, ts, ts)
}

func (t *tx) RestoreFlag(ctx context.Context, project, key string, at time.Time) (model.Flag, error) {
	return t.updateFlag(ctx, project, key, `UPDATE jollyroger_flags SET archived_at = NULL, updated_at = ? WHERE project_id = ? AND key = ? AND archived_at IS NOT NULL`, adb.TextTime(at))
}

// updateFlag runs stmt with args followed by (project_id, key) and returns the flag as stored.
// An UPDATE that matched nothing is fine (archive/restore are idempotent); a missing flag is not.
func (t *tx) updateFlag(ctx context.Context, project, key, stmt string, args ...any) (model.Flag, error) {
	pid, err := projectID(ctx, t.q, project)
	if err != nil {
		return model.Flag{}, err
	}
	if _, err := t.q.ExecContext(ctx, stmt, append(args, pid, key)...); err != nil {
		return model.Flag{}, fmt.Errorf("sqlite: update flag %q: %w", key, err)
	}
	f, err := flagRow(ctx, t.q, pid, key)
	if err != nil {
		return model.Flag{}, err
	}
	return adb.RowToFlag(f), nil
}

func (t *tx) SetEnvState(ctx context.Context, project, key, environment string, c model.EnvStateChange, expectedVersion int64) (model.EnvState, error) {
	if c.Config.Unparseable {
		return model.EnvState{}, fmt.Errorf("flag %q in %q: refusing to store a config this version cannot read (it would destroy the stored rules): %w", key, environment, model.ErrInvalid)
	}
	pid, err := projectID(ctx, t.q, project)
	if err != nil {
		return model.EnvState{}, err
	}
	f, err := flagRow(ctx, t.q, pid, key)
	if err != nil {
		return model.EnvState{}, err
	}
	eid, err := environmentID(ctx, t.q, pid, environment)
	if err != nil {
		return model.EnvState{}, err
	}
	config, err := adb.ConfigToJSON(c.Config)
	if err != nil {
		return model.EnvState{}, err
	}
	enabled := 0
	if c.Enabled {
		enabled = 1
	}
	res, err := t.q.ExecContext(ctx, `UPDATE jollyroger_flag_env_states
		SET enabled = ?, config = ?, version = version + 1, updated_at = ?, updated_by = ?
		WHERE flag_id = ? AND environment_id = ? AND version = ?`,
		enabled, config, adb.TextTime(c.At), c.Actor, f.ID, eid, expectedVersion)
	if err != nil {
		return model.EnvState{}, fmt.Errorf("sqlite: set state of %q in %q: %w", key, environment, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return model.EnvState{}, fmt.Errorf("sqlite: set state of %q in %q: %w", key, environment, err)
	}
	if n == 0 {
		return model.EnvState{}, fmt.Errorf("flag %q in %q is not at version %d: %w", key, environment, expectedVersion, model.ErrConflict)
	}
	full, err := getFlag(ctx, t.q, project, key)
	if err != nil {
		return model.EnvState{}, err
	}
	for _, st := range full.States {
		if st.EnvironmentKey == environment {
			return st, nil
		}
	}
	return model.EnvState{}, fmt.Errorf("sqlite: state of %q in %q vanished: %w", key, environment, model.ErrNotFound)
}

func (t *tx) AppendAudit(ctx context.Context, e model.AuditEntry) error {
	pid, err := projectID(ctx, t.q, e.Project)
	if err != nil {
		return err
	}
	var taken int
	if err := t.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM jollyroger_audit_log WHERE id = ?`, e.ID).Scan(&taken); err != nil {
		return fmt.Errorf("sqlite: append audit %s: %w", e.ID, err)
	}
	if taken > 0 {
		return fmt.Errorf("audit id %s: %w", e.ID, model.ErrAlreadyExists)
	}
	before, err := adb.AuditJSON(e.Before)
	if err != nil {
		return err
	}
	after, err := adb.AuditJSON(e.After)
	if err != nil {
		return err
	}
	if _, err := t.q.ExecContext(ctx, `INSERT INTO jollyroger_audit_log (`+auditColumns+`, project_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, adb.NullIfEmpty(e.EnvironmentKey), adb.NullIfEmpty(e.FlagKey), e.ActorID, e.ActorName, string(e.Action),
		before, after, adb.NullIfEmpty(e.Reason), adb.TextTime(e.CreatedAt), pid); err != nil {
		return fmt.Errorf("sqlite: append audit %s: %w", e.ID, err)
	}
	return nil
}

func (t *tx) BumpRevision(ctx context.Context, project string) (int64, error) {
	pid, err := projectID(ctx, t.q, project)
	if err != nil {
		return 0, err
	}
	var rev int64
	err = t.q.QueryRowContext(ctx, `UPDATE jollyroger_revisions SET revision = revision + 1 WHERE project_id = ? RETURNING revision`, pid).Scan(&rev)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("revision of %q: %w", project, model.ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("sqlite: bump revision of %q: %w", project, err)
	}
	return rev, nil
}
