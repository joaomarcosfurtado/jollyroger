// Package postgres is the PostgreSQL implementation of model.FlagStore. The host passes its own
// *sql.DB opened with any database/sql PostgreSQL driver; jollyroger imports no driver. Requires
// PostgreSQL 12+. Objects live in the configured schema (default public) with the jollyroger_
// prefix.
package postgres

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/joaomarcosfurtado/jollyroger/internal/diplomat/migrate"
	"github.com/joaomarcosfurtado/jollyroger/internal/model"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

var schemaPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// advisoryLockSQL serializes migration runs across every instance sharing the database.
const advisoryLockSQL = `SELECT pg_advisory_xact_lock(7064998412)`

// Store is a model.FlagStore on a host-provided PostgreSQL database.
type Store struct {
	db     *sql.DB
	schema string // validated, unquoted
}

// New returns a Store on db using schema ("" means public). The name must match
// ^[a-z_][a-z0-9_]{0,62}$; anything else is model.ErrInvalid. Run Migrate before using the store.
func New(db *sql.DB, schema string) (*Store, error) {
	if schema == "" {
		schema = "public"
	}
	if !schemaPattern.MatchString(schema) {
		return nil, fmt.Errorf("postgres schema %q must match %s: %w", schema, schemaPattern, model.ErrInvalid)
	}
	return &Store{db: db, schema: schema}, nil
}

// sql renders a statement template: {schema} becomes the quoted schema name. The name was
// validated in New, so it cannot carry SQL.
func (s *Store) sql(stmt string) string {
	return strings.ReplaceAll(stmt, "{schema}", `"`+s.schema+`"`)
}

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Migrate applies pending migrations and returns the versions applied.
func (s *Store) Migrate(ctx context.Context) ([]int, error) {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("postgres: migrations: %w", err)
	}
	ms, err := migrate.Load(sub)
	if err != nil {
		return nil, err
	}
	setup := []string{advisoryLockSQL}
	if s.schema != "public" {
		setup = append(setup, s.sql(`CREATE SCHEMA IF NOT EXISTS {schema}`))
	}
	return migrate.Run(ctx, s.db, migrate.Dialect{
		Begin:       "BEGIN",
		Setup:       setup,
		Table:       s.sql(`{schema}.jollyroger_schema_migrations`),
		Placeholder: func(n int) string { return fmt.Sprintf("$%d", n) },
		Render:      s.sql,
	}, ms)
}

// InTx implements model.FlagStore.
func (s *Store) InTx(ctx context.Context, fn func(model.FlagTx) error) (err error) {
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: begin: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rbErr := sqlTx.Rollback(); rbErr != nil && err != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("postgres: rollback: %w", rbErr))
		}
	}()
	if err := fn(&tx{s: s, q: sqlTx}); err != nil {
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return fmt.Errorf("postgres: commit: %w", err)
	}
	committed = true
	return nil
}

// LoadSnapshot implements model.FlagStore. The revision and the flags come from one
// REPEATABLE READ transaction, so they are consistent.
func (s *Store) LoadSnapshot(ctx context.Context, project, environment string) (snap model.Snapshot, err error) {
	sqlTx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("postgres: begin: %w", err)
	}
	defer func() {
		if rbErr := sqlTx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) && err == nil {
			err = fmt.Errorf("postgres: end read: %w", rbErr)
		}
	}()
	return s.loadSnapshot(ctx, sqlTx, project, environment)
}

// Revision implements model.FlagStore.
func (s *Store) Revision(ctx context.Context, project string) (int64, error) {
	return s.revision(ctx, s.db, project)
}

// ListEnvironments implements model.FlagStore.
func (s *Store) ListEnvironments(ctx context.Context, project string) ([]model.Environment, error) {
	return s.listEnvironments(ctx, s.db, project)
}

// GetFlag implements model.FlagStore.
func (s *Store) GetFlag(ctx context.Context, project, key string) (model.FlagWithStates, error) {
	return s.getFlag(ctx, s.db, project, key)
}

// ListFlags implements model.FlagStore.
func (s *Store) ListFlags(ctx context.Context, q model.FlagQuery) (model.FlagPage, error) {
	return s.listFlags(ctx, s.db, q)
}

// ListAudit implements model.FlagStore.
func (s *Store) ListAudit(ctx context.Context, q model.AuditQuery) (model.AuditPage, error) {
	return s.listAudit(ctx, s.db, q)
}
