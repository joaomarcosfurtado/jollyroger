package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	adb "github.com/joaomarcosfurtado/jollyroger/internal/adapter/db"
	"github.com/joaomarcosfurtado/jollyroger/internal/model"
	wiredb "github.com/joaomarcosfurtado/jollyroger/internal/wire/db"
)

const (
	flagColumns  = `f.id, f.key, f.name, f.description, f.kind, f.created_at, f.updated_at, f.archived_at`
	stateColumns = `e.key, s.enabled, s.config::text, s.version, s.updated_at, s.updated_by`
	auditColumns = `id, environment_key, flag_key, actor_id, actor_name, action, before_state::text, after_state::text, reason, created_at`
)

type scanner interface{ Scan(dest ...any) error }

func scanFlag(r scanner, extra ...any) (wiredb.FlagRow, error) {
	var f wiredb.FlagRow
	dest := append([]any{&f.ID, &f.Key, &f.Name, &f.Description, &f.Kind, &f.CreatedAt, &f.UpdatedAt, &f.ArchivedAt}, extra...)
	return f, r.Scan(dest...)
}

func stateDest(s *wiredb.StateRow) []any {
	return []any{&s.EnvironmentKey, &s.Enabled, &s.Config, &s.Version, &s.UpdatedAt, &s.UpdatedBy}
}

func (s *Store) projectID(ctx context.Context, q querier, key string) (string, error) {
	var id string
	err := q.QueryRowContext(ctx, s.sql(`SELECT id FROM {schema}.jollyroger_projects WHERE key = $1`), key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("project %q: %w", key, model.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("postgres: project %q: %w", key, err)
	}
	return id, nil
}

func (s *Store) environmentID(ctx context.Context, q querier, projectID, key string) (string, error) {
	var id string
	err := q.QueryRowContext(ctx, s.sql(`SELECT id FROM {schema}.jollyroger_environments WHERE project_id = $1 AND key = $2`), projectID, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("environment %q: %w", key, model.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("postgres: environment %q: %w", key, err)
	}
	return id, nil
}

func (s *Store) revision(ctx context.Context, q querier, project string) (int64, error) {
	pid, err := s.projectID(ctx, q, project)
	if err != nil {
		return 0, err
	}
	var rev int64
	if err := q.QueryRowContext(ctx, s.sql(`SELECT revision FROM {schema}.jollyroger_revisions WHERE project_id = $1`), pid).Scan(&rev); err != nil {
		return 0, fmt.Errorf("postgres: revision of %q: %w", project, err)
	}
	return rev, nil
}

func (s *Store) listEnvironments(ctx context.Context, q querier, project string) (_ []model.Environment, err error) {
	pid, err := s.projectID(ctx, q, project)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, s.sql(`SELECT id, key, name, position, created_at FROM {schema}.jollyroger_environments WHERE project_id = $1 ORDER BY position, key`), pid)
	if err != nil {
		return nil, fmt.Errorf("postgres: environments: %w", err)
	}
	defer closeRows(rows, &err)
	var out []model.Environment
	for rows.Next() {
		var r wiredb.EnvironmentRow
		if err := rows.Scan(&r.ID, &r.Key, &r.Name, &r.Position, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres: environments: %w", err)
		}
		out = append(out, adb.RowToEnvironment(r))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: environments: %w", err)
	}
	return out, nil
}

func (s *Store) loadSnapshot(ctx context.Context, q querier, project, environment string) (_ model.Snapshot, err error) {
	pid, err := s.projectID(ctx, q, project)
	if err != nil {
		return model.Snapshot{}, err
	}
	eid, err := s.environmentID(ctx, q, pid, environment)
	if err != nil {
		return model.Snapshot{}, err
	}
	rev, err := s.revision(ctx, q, project)
	if err != nil {
		return model.Snapshot{}, err
	}
	rows, err := q.QueryContext(ctx, s.sql(`SELECT f.key, s.enabled, s.version, s.config::text
		FROM {schema}.jollyroger_flags f JOIN {schema}.jollyroger_flag_env_states s ON s.flag_id = f.id
		WHERE f.project_id = $1 AND s.environment_id = $2 AND f.archived_at IS NULL
		ORDER BY f.key`), pid, eid)
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("postgres: snapshot: %w", err)
	}
	defer closeRows(rows, &err)
	var out []wiredb.SnapshotRow
	for rows.Next() {
		var r wiredb.SnapshotRow
		if err := rows.Scan(&r.Key, &r.Enabled, &r.Version, &r.Config); err != nil {
			return model.Snapshot{}, fmt.Errorf("postgres: snapshot: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return model.Snapshot{}, fmt.Errorf("postgres: snapshot: %w", err)
	}
	return adb.RowsToSnapshot(environment, rev, out), nil
}

func (s *Store) flagRow(ctx context.Context, q querier, projectID, key string) (wiredb.FlagRow, error) {
	f, err := scanFlag(q.QueryRowContext(ctx, s.sql(`SELECT `+flagColumns+` FROM {schema}.jollyroger_flags f WHERE f.project_id = $1 AND f.key = $2`), projectID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return wiredb.FlagRow{}, fmt.Errorf("flag %q: %w", key, model.ErrNotFound)
	}
	if err != nil {
		return wiredb.FlagRow{}, fmt.Errorf("postgres: flag %q: %w", key, err)
	}
	return f, nil
}

func (s *Store) getFlag(ctx context.Context, q querier, project, key string) (_ model.FlagWithStates, err error) {
	pid, err := s.projectID(ctx, q, project)
	if err != nil {
		return model.FlagWithStates{}, err
	}
	f, err := s.flagRow(ctx, q, pid, key)
	if err != nil {
		return model.FlagWithStates{}, err
	}
	rows, err := q.QueryContext(ctx, s.sql(`SELECT `+stateColumns+`
		FROM {schema}.jollyroger_flag_env_states s JOIN {schema}.jollyroger_environments e ON e.id = s.environment_id
		WHERE s.flag_id = $1 ORDER BY e.position, e.key`), f.ID)
	if err != nil {
		return model.FlagWithStates{}, fmt.Errorf("postgres: states of %q: %w", key, err)
	}
	defer closeRows(rows, &err)
	out := model.FlagWithStates{Flag: adb.RowToFlag(f)}
	for rows.Next() {
		var st wiredb.StateRow
		if err := rows.Scan(stateDest(&st)...); err != nil {
			return model.FlagWithStates{}, fmt.Errorf("postgres: states of %q: %w", key, err)
		}
		out.States = append(out.States, adb.RowToEnvState(st))
	}
	if err := rows.Err(); err != nil {
		return model.FlagWithStates{}, fmt.Errorf("postgres: states of %q: %w", key, err)
	}
	return out, nil
}

func (s *Store) listFlags(ctx context.Context, q querier, fq model.FlagQuery) (_ model.FlagPage, err error) {
	pid, err := s.projectID(ctx, q, fq.Project)
	if err != nil {
		return model.FlagPage{}, err
	}
	eid, err := s.environmentID(ctx, q, pid, fq.Environment)
	if err != nil {
		return model.FlagPage{}, err
	}
	limit := fq.EffectiveLimit()
	rows, err := q.QueryContext(ctx, s.sql(`SELECT `+flagColumns+`, `+stateColumns+`
		FROM {schema}.jollyroger_flags f
		JOIN {schema}.jollyroger_flag_env_states s ON s.flag_id = f.id
		JOIN {schema}.jollyroger_environments e ON e.id = s.environment_id
		WHERE f.project_id = $1 AND s.environment_id = $2 AND f.key > $3 AND ($4 OR f.archived_at IS NULL)
		ORDER BY f.key
		LIMIT $5`), pid, eid, fq.AfterKey, fq.IncludeArchived, limit+1)
	if err != nil {
		return model.FlagPage{}, fmt.Errorf("postgres: list flags: %w", err)
	}
	defer closeRows(rows, &err)
	var page model.FlagPage
	for rows.Next() {
		var st wiredb.StateRow
		f, err := scanFlag(rows, stateDest(&st)...)
		if err != nil {
			return model.FlagPage{}, fmt.Errorf("postgres: list flags: %w", err)
		}
		if len(page.Items) == limit {
			page.HasMore = true
			break
		}
		page.Items = append(page.Items, model.FlagView{Flag: adb.RowToFlag(f), State: adb.RowToEnvState(st)})
	}
	if err := rows.Err(); err != nil {
		return model.FlagPage{}, fmt.Errorf("postgres: list flags: %w", err)
	}
	return page, nil
}

func (s *Store) listAudit(ctx context.Context, q querier, aq model.AuditQuery) (_ model.AuditPage, err error) {
	pid, err := s.projectID(ctx, q, aq.Project)
	if err != nil {
		return model.AuditPage{}, err
	}
	limit := aq.EffectiveLimit()
	rows, err := q.QueryContext(ctx, s.sql(`SELECT `+auditColumns+` FROM {schema}.jollyroger_audit_log
		WHERE project_id = $1 AND ($2 = '' OR flag_key = $2) AND ($3 = '' OR environment_key = $3) AND ($4 = '' OR id < $4)
		ORDER BY id DESC
		LIMIT $5`), pid, aq.FlagKey, aq.EnvironmentKey, aq.BeforeID, limit+1)
	if err != nil {
		return model.AuditPage{}, fmt.Errorf("postgres: audit: %w", err)
	}
	defer closeRows(rows, &err)
	var page model.AuditPage
	for rows.Next() {
		var r wiredb.AuditRow
		if err := rows.Scan(&r.ID, &r.EnvironmentKey, &r.FlagKey, &r.ActorID, &r.ActorName, &r.Action, &r.Before, &r.After, &r.Reason, &r.CreatedAt); err != nil {
			return model.AuditPage{}, fmt.Errorf("postgres: audit: %w", err)
		}
		if len(page.Items) == limit {
			page.HasMore = true
			break
		}
		e, err := adb.RowToAudit(aq.Project, r)
		if err != nil {
			return model.AuditPage{}, err
		}
		page.Items = append(page.Items, e)
	}
	if err := rows.Err(); err != nil {
		return model.AuditPage{}, fmt.Errorf("postgres: audit: %w", err)
	}
	return page, nil
}

// closeRows closes rows and reports a close failure unless an earlier error is being returned.
func closeRows(rows *sql.Rows, err *error) {
	if cerr := rows.Close(); cerr != nil && *err == nil {
		*err = cerr
	}
}
