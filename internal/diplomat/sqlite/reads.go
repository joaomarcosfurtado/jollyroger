package sqlite

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
	stateColumns = `e.key, s.enabled, s.config, s.version, s.updated_at, s.updated_by`
	auditColumns = `id, environment_key, flag_key, actor_id, actor_name, action, before_state, after_state, reason, created_at`
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

func projectID(ctx context.Context, q querier, key string) (string, error) {
	var id string
	err := q.QueryRowContext(ctx, `SELECT id FROM jollyroger_projects WHERE key = ?`, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("project %q: %w", key, model.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("sqlite: project %q: %w", key, err)
	}
	return id, nil
}

func environmentID(ctx context.Context, q querier, projectID, key string) (string, error) {
	var id string
	err := q.QueryRowContext(ctx, `SELECT id FROM jollyroger_environments WHERE project_id = ? AND key = ?`, projectID, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("environment %q: %w", key, model.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("sqlite: environment %q: %w", key, err)
	}
	return id, nil
}

func revision(ctx context.Context, q querier, project string) (int64, error) {
	pid, err := projectID(ctx, q, project)
	if err != nil {
		return 0, err
	}
	var rev int64
	if err := q.QueryRowContext(ctx, `SELECT revision FROM jollyroger_revisions WHERE project_id = ?`, pid).Scan(&rev); err != nil {
		return 0, fmt.Errorf("sqlite: revision of %q: %w", project, err)
	}
	return rev, nil
}

func listEnvironments(ctx context.Context, q querier, project string) ([]model.Environment, error) {
	pid, err := projectID(ctx, q, project)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT id, key, name, position, created_at FROM jollyroger_environments WHERE project_id = ? ORDER BY position, key`, pid)
	if err != nil {
		return nil, fmt.Errorf("sqlite: environments: %w", err)
	}
	defer rows.Close()
	var out []model.Environment
	for rows.Next() {
		var r wiredb.EnvironmentRow
		if err := rows.Scan(&r.ID, &r.Key, &r.Name, &r.Position, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("sqlite: environments: %w", err)
		}
		out = append(out, adb.RowToEnvironment(r))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: environments: %w", err)
	}
	return out, nil
}

func loadSnapshot(ctx context.Context, q querier, project, environment string) (model.Snapshot, error) {
	pid, err := projectID(ctx, q, project)
	if err != nil {
		return model.Snapshot{}, err
	}
	eid, err := environmentID(ctx, q, pid, environment)
	if err != nil {
		return model.Snapshot{}, err
	}
	rev, err := revision(ctx, q, project)
	if err != nil {
		return model.Snapshot{}, err
	}
	rows, err := q.QueryContext(ctx, `SELECT f.key, s.enabled, s.version, s.config
		FROM jollyroger_flags f JOIN jollyroger_flag_env_states s ON s.flag_id = f.id
		WHERE f.project_id = ? AND s.environment_id = ? AND f.archived_at IS NULL
		ORDER BY f.key`, pid, eid)
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("sqlite: snapshot: %w", err)
	}
	defer rows.Close()
	var out []wiredb.SnapshotRow
	for rows.Next() {
		var r wiredb.SnapshotRow
		if err := rows.Scan(&r.Key, &r.Enabled, &r.Version, &r.Config); err != nil {
			return model.Snapshot{}, fmt.Errorf("sqlite: snapshot: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return model.Snapshot{}, fmt.Errorf("sqlite: snapshot: %w", err)
	}
	return adb.RowsToSnapshot(environment, rev, out), nil
}

func flagRow(ctx context.Context, q querier, projectID, key string) (wiredb.FlagRow, error) {
	f, err := scanFlag(q.QueryRowContext(ctx, `SELECT `+flagColumns+` FROM jollyroger_flags f WHERE f.project_id = ? AND f.key = ?`, projectID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return wiredb.FlagRow{}, fmt.Errorf("flag %q: %w", key, model.ErrNotFound)
	}
	if err != nil {
		return wiredb.FlagRow{}, fmt.Errorf("sqlite: flag %q: %w", key, err)
	}
	return f, nil
}

func getFlag(ctx context.Context, q querier, project, key string) (model.FlagWithStates, error) {
	pid, err := projectID(ctx, q, project)
	if err != nil {
		return model.FlagWithStates{}, err
	}
	f, err := flagRow(ctx, q, pid, key)
	if err != nil {
		return model.FlagWithStates{}, err
	}
	rows, err := q.QueryContext(ctx, `SELECT `+stateColumns+`
		FROM jollyroger_flag_env_states s JOIN jollyroger_environments e ON e.id = s.environment_id
		WHERE s.flag_id = ? ORDER BY e.position, e.key`, f.ID)
	if err != nil {
		return model.FlagWithStates{}, fmt.Errorf("sqlite: states of %q: %w", key, err)
	}
	defer rows.Close()
	out := model.FlagWithStates{Flag: adb.RowToFlag(f)}
	for rows.Next() {
		var s wiredb.StateRow
		if err := rows.Scan(stateDest(&s)...); err != nil {
			return model.FlagWithStates{}, fmt.Errorf("sqlite: states of %q: %w", key, err)
		}
		out.States = append(out.States, adb.RowToEnvState(s))
	}
	if err := rows.Err(); err != nil {
		return model.FlagWithStates{}, fmt.Errorf("sqlite: states of %q: %w", key, err)
	}
	return out, nil
}

func listFlags(ctx context.Context, q querier, fq model.FlagQuery) (model.FlagPage, error) {
	pid, err := projectID(ctx, q, fq.Project)
	if err != nil {
		return model.FlagPage{}, err
	}
	eid, err := environmentID(ctx, q, pid, fq.Environment)
	if err != nil {
		return model.FlagPage{}, err
	}
	includeArchived := 0
	if fq.IncludeArchived {
		includeArchived = 1
	}
	limit := fq.EffectiveLimit()
	rows, err := q.QueryContext(ctx, `SELECT `+flagColumns+`, `+stateColumns+`
		FROM jollyroger_flags f
		JOIN jollyroger_flag_env_states s ON s.flag_id = f.id
		JOIN jollyroger_environments e ON e.id = s.environment_id
		WHERE f.project_id = ? AND s.environment_id = ? AND f.key > ? AND (? = 1 OR f.archived_at IS NULL)
		ORDER BY f.key
		LIMIT ?`, pid, eid, fq.AfterKey, includeArchived, limit+1)
	if err != nil {
		return model.FlagPage{}, fmt.Errorf("sqlite: list flags: %w", err)
	}
	defer rows.Close()
	var page model.FlagPage
	for rows.Next() {
		var s wiredb.StateRow
		f, err := scanFlag(rows, stateDest(&s)...)
		if err != nil {
			return model.FlagPage{}, fmt.Errorf("sqlite: list flags: %w", err)
		}
		if len(page.Items) == limit {
			page.HasMore = true
			break
		}
		page.Items = append(page.Items, model.FlagView{Flag: adb.RowToFlag(f), State: adb.RowToEnvState(s)})
	}
	if err := rows.Err(); err != nil {
		return model.FlagPage{}, fmt.Errorf("sqlite: list flags: %w", err)
	}
	return page, nil
}

func listAudit(ctx context.Context, q querier, aq model.AuditQuery) (model.AuditPage, error) {
	pid, err := projectID(ctx, q, aq.Project)
	if err != nil {
		return model.AuditPage{}, err
	}
	limit := aq.EffectiveLimit()
	rows, err := q.QueryContext(ctx, `SELECT `+auditColumns+` FROM jollyroger_audit_log
		WHERE project_id = ? AND (? = '' OR flag_key = ?) AND (? = '' OR environment_key = ?) AND (? = '' OR id < ?)
		ORDER BY id DESC
		LIMIT ?`, pid, aq.FlagKey, aq.FlagKey, aq.EnvironmentKey, aq.EnvironmentKey, aq.BeforeID, aq.BeforeID, limit+1)
	if err != nil {
		return model.AuditPage{}, fmt.Errorf("sqlite: audit: %w", err)
	}
	defer rows.Close()
	var page model.AuditPage
	for rows.Next() {
		var r wiredb.AuditRow
		if err := rows.Scan(&r.ID, &r.EnvironmentKey, &r.FlagKey, &r.ActorID, &r.ActorName, &r.Action, &r.Before, &r.After, &r.Reason, &r.CreatedAt); err != nil {
			return model.AuditPage{}, fmt.Errorf("sqlite: audit: %w", err)
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
		return model.AuditPage{}, fmt.Errorf("sqlite: audit: %w", err)
	}
	return page, nil
}
