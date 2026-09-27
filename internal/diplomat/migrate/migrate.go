// Package migrate applies jollyroger's embedded SQL migrations. A run happens in one transaction
// under a database lock, so any number of instances can boot at once: each migration is applied
// exactly once, and a failed run leaves nothing behind. Rules: docs/skills/migrations.md.
package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ErrDrift reports a migration whose file changed after it was applied.
var ErrDrift = errors.New("migration checksum drift")

// Migration is one NNNN_name.sql file.
type Migration struct {
	Version  int
	Name     string
	SQL      string
	Checksum string
}

// Dialect describes how migrations run on one database engine.
type Dialect struct {
	// Begin opens the transaction the whole run happens in: "BEGIN" (PostgreSQL) or
	// "BEGIN IMMEDIATE" (SQLite, which takes the write lock up front).
	Begin string
	// Setup statements run right after Begin, in order (PostgreSQL: the advisory lock, the schema).
	Setup []string
	// Table is the migrations table, qualified if needed.
	Table string
	// Placeholder returns the n-th (1-based) bind parameter: "$1" or "?".
	Placeholder func(n int) string
	// Render replaces dialect tokens such as {schema} in migration SQL. Checksums are computed on
	// the unrendered file, so choosing another schema is not drift.
	Render func(sql string) string
	// Prepare, if set, runs on the connection before Begin (SQLite: set a busy timeout).
	Prepare func(ctx context.Context, conn *sql.Conn) error
}

const (
	createTableSQL = `CREATE TABLE IF NOT EXISTS {table} (version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL, applied_at TEXT NOT NULL)`
	selectSQL      = `SELECT version, checksum FROM {table}`
	insertSQL      = `INSERT INTO {table} (version, name, checksum, applied_at) VALUES ({p1}, {p2}, {p3}, {p4})`
)

// Checksum is the hex SHA-256 of content with CRLF line endings normalized to LF.
func Checksum(content string) string {
	sum := sha256.Sum256([]byte(strings.ReplaceAll(content, "\r\n", "\n")))
	return hex.EncodeToString(sum[:])
}

// Load reads the NNNN_name.sql files at the root of fsys, ordered by version. Other files are
// ignored; a malformed name or a repeated version is an error.
func Load(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("migrate: read migrations: %w", err)
	}
	var out []Migration
	seen := map[int]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		version, name, err := parseName(e.Name())
		if err != nil {
			return nil, err
		}
		if prev, dup := seen[version]; dup {
			return nil, fmt.Errorf("migrate: %s and %s share version %d", prev, e.Name(), version)
		}
		seen[version] = e.Name()
		b, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, fmt.Errorf("migrate: read %s: %w", e.Name(), err)
		}
		out = append(out, Migration{Version: version, Name: name, SQL: string(b), Checksum: Checksum(string(b))})
	}
	slices.SortFunc(out, func(a, b Migration) int { return a.Version - b.Version })
	return out, nil
}

func parseName(file string) (int, string, error) {
	num, name, ok := strings.Cut(strings.TrimSuffix(file, ".sql"), "_")
	if !ok || len(num) != 4 || name == "" {
		return 0, "", fmt.Errorf("migrate: %q is not named NNNN_name.sql", file)
	}
	v, err := strconv.Atoi(num)
	if err != nil || v < 1 {
		return 0, "", fmt.Errorf("migrate: %q has an invalid version", file)
	}
	return v, name, nil
}

// Run applies the migrations not yet recorded, in order, in one transaction, and returns the
// versions it applied. A recorded migration whose checksum differs is ErrDrift. Recorded versions
// this build does not know (a newer build migrated first, during a rolling deploy) are ignored:
// migrations are additive, so older code keeps working.
func Run(ctx context.Context, db *sql.DB, d Dialect, migrations []Migration) (applied []int, err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate: connect: %w", err)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("migrate: release connection: %w", cerr)
		}
	}()
	if d.Prepare != nil {
		if err := d.Prepare(ctx, conn); err != nil {
			return nil, fmt.Errorf("migrate: prepare connection: %w", err)
		}
	}
	if _, err := conn.ExecContext(ctx, d.Begin); err != nil {
		return nil, fmt.Errorf("migrate: begin: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if _, rbErr := conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK"); rbErr != nil && err != nil {
			err = errors.Join(err, fmt.Errorf("migrate: rollback: %w", rbErr))
		}
	}()
	for _, stmt := range d.Setup {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return nil, fmt.Errorf("migrate: setup: %w", err)
		}
	}
	if _, err := conn.ExecContext(ctx, withTable(d, createTableSQL)); err != nil {
		return nil, fmt.Errorf("migrate: create migrations table: %w", err)
	}
	done, err := recorded(ctx, conn, d)
	if err != nil {
		return nil, err
	}
	insert := withTable(d, insertSQL)
	for i := 1; i <= 4; i++ {
		insert = strings.ReplaceAll(insert, "{p"+strconv.Itoa(i)+"}", d.Placeholder(i))
	}
	for _, m := range migrations {
		if sum, ok := done[m.Version]; ok {
			if sum != m.Checksum {
				return nil, fmt.Errorf("migrate: %04d_%s changed after it was applied (recorded %s, file %s); fix forward with a new migration: %w",
					m.Version, m.Name, sum, m.Checksum, ErrDrift)
			}
			continue
		}
		if _, err := conn.ExecContext(ctx, d.Render(m.SQL)); err != nil {
			return nil, fmt.Errorf("migrate: apply %04d_%s: %w", m.Version, m.Name, err)
		}
		if _, err := conn.ExecContext(ctx, insert, m.Version, m.Name, m.Checksum, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return nil, fmt.Errorf("migrate: record %04d_%s: %w", m.Version, m.Name, err)
		}
		applied = append(applied, m.Version)
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return nil, fmt.Errorf("migrate: commit: %w", err)
	}
	committed = true
	return applied, nil
}

func withTable(d Dialect, stmt string) string {
	return strings.ReplaceAll(stmt, "{table}", d.Table)
}

func recorded(ctx context.Context, conn *sql.Conn, d Dialect) (map[int]string, error) {
	rows, err := conn.QueryContext(ctx, withTable(d, selectSQL))
	if err != nil {
		return nil, fmt.Errorf("migrate: read applied migrations: %w", err)
	}
	defer rows.Close()
	done := map[int]string{}
	for rows.Next() {
		var v int
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			return nil, fmt.Errorf("migrate: read applied migrations: %w", err)
		}
		done[v] = sum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrate: read applied migrations: %w", err)
	}
	return done, nil
}
