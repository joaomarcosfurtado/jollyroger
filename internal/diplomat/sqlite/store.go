// Package sqlite is the SQLite implementation of model.FlagStore. The host passes its own *sql.DB
// opened with any database/sql SQLite driver; jollyroger imports no driver. Requires SQLite 3.35+.
//
// Write transactions start with BEGIN IMMEDIATE so a writer takes the lock before reading (a
// deferred transaction that reads and then writes fails at once under contention). If the host
// left busy_timeout at SQLite's default of 0, the store raises it to 5 seconds on the connections
// it uses, so concurrent writers wait instead of failing.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/joaomarcosfurtado/jollyroger/internal/diplomat/migrate"
	"github.com/joaomarcosfurtado/jollyroger/internal/model"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// setBusyTimeoutSQL is applied only when the host has not set a busy timeout.
const setBusyTimeoutSQL = "PRAGMA busy_timeout = 5000"

// Store is a model.FlagStore on a host-provided SQLite database.
type Store struct{ db *sql.DB }

// New returns a Store on db. Run Migrate before using it.
func New(db *sql.DB) *Store { return &Store{db: db} }

// querier is what reads and writes need; *sql.DB and *sql.Conn satisfy it.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Migrate applies pending migrations and returns the versions applied.
func (s *Store) Migrate(ctx context.Context) ([]int, error) {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("sqlite: migrations: %w", err)
	}
	ms, err := migrate.Load(sub)
	if err != nil {
		return nil, err
	}
	return migrate.Run(ctx, s.db, migrate.Dialect{
		Begin:       "BEGIN IMMEDIATE",
		Table:       "jollyroger_schema_migrations",
		Placeholder: func(int) string { return "?" },
		Render:      func(sql string) string { return sql },
		Prepare:     ensureBusyTimeout,
	}, ms)
}

// ensureBusyTimeout sets a busy timeout on conn only if the host left it at 0.
func ensureBusyTimeout(ctx context.Context, conn *sql.Conn) error {
	var ms int
	if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&ms); err != nil {
		return fmt.Errorf("sqlite: read busy_timeout: %w", err)
	}
	if ms > 0 {
		return nil
	}
	if _, err := conn.ExecContext(ctx, setBusyTimeoutSQL); err != nil {
		return fmt.Errorf("sqlite: set busy_timeout: %w", err)
	}
	return nil
}

// withConn runs fn inside a transaction opened with begin ("BEGIN" for reads, "BEGIN IMMEDIATE"
// for writes) on one dedicated connection, committing if fn returns nil.
func (s *Store) withConn(ctx context.Context, begin string, fn func(q querier) error) (err error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("sqlite: connect: %w", err)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("sqlite: release connection: %w", cerr)
		}
	}()
	if err := ensureBusyTimeout(ctx, conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, begin); err != nil {
		return fmt.Errorf("sqlite: begin: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if _, rbErr := conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK"); rbErr != nil && err != nil {
			err = errors.Join(err, fmt.Errorf("sqlite: rollback: %w", rbErr))
		}
	}()
	if err := fn(conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("sqlite: commit: %w", err)
	}
	committed = true
	return nil
}

// InTx implements model.FlagStore.
func (s *Store) InTx(ctx context.Context, fn func(model.FlagTx) error) error {
	return s.withConn(ctx, "BEGIN IMMEDIATE", func(q querier) error { return fn(&tx{q: q}) })
}

// LoadSnapshot implements model.FlagStore. The revision and the flags come from one read
// transaction, so they are consistent.
func (s *Store) LoadSnapshot(ctx context.Context, project, environment string) (model.Snapshot, error) {
	var snap model.Snapshot
	err := s.withConn(ctx, "BEGIN", func(q querier) error {
		var err error
		snap, err = loadSnapshot(ctx, q, project, environment)
		return err
	})
	return snap, err
}

// Revision implements model.FlagStore.
func (s *Store) Revision(ctx context.Context, project string) (int64, error) {
	return revision(ctx, s.db, project)
}

// ListEnvironments implements model.FlagStore.
func (s *Store) ListEnvironments(ctx context.Context, project string) ([]model.Environment, error) {
	return listEnvironments(ctx, s.db, project)
}

// GetFlag implements model.FlagStore.
func (s *Store) GetFlag(ctx context.Context, project, key string) (model.FlagWithStates, error) {
	return getFlag(ctx, s.db, project, key)
}

// ListFlags implements model.FlagStore.
func (s *Store) ListFlags(ctx context.Context, q model.FlagQuery) (model.FlagPage, error) {
	return listFlags(ctx, s.db, q)
}

// ListAudit implements model.FlagStore.
func (s *Store) ListAudit(ctx context.Context, q model.AuditQuery) (model.AuditPage, error) {
	return listAudit(ctx, s.db, q)
}
