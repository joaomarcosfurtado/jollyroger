package migrate_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/joaomarcosfurtado/jollyroger/internal/diplomat/migrate"

	_ "modernc.org/sqlite"
)

func dialect() migrate.Dialect {
	return migrate.Dialect{
		Begin:       "BEGIN IMMEDIATE",
		Table:       "jollyroger_schema_migrations",
		Placeholder: func(int) string { return "?" },
		Render:      func(s string) string { return s },
	}
}

// openDB opens a SQLite file shared by every handle opened on the same path.
func openDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func load(t *testing.T, files fstest.MapFS) []migrate.Migration {
	t.Helper()
	ms, err := migrate.Load(files)
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

// The migrations below are deliberately NOT idempotent: applying one twice fails, which is how
// these tests prove exactly-once.
func twoMigrations() fstest.MapFS {
	return fstest.MapFS{
		"0002_second.sql": {Data: []byte("CREATE TABLE t2 (id INTEGER);")},
		"0001_first.sql":  {Data: []byte("CREATE TABLE t1 (id INTEGER);\r\nINSERT INTO t1 VALUES (1);")},
		"README.md":       {Data: []byte("not a migration")},
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()
	ms := load(t, twoMigrations())
	if len(ms) != 2 || ms[0].Version != 1 || ms[0].Name != "first" || ms[1].Version != 2 || ms[1].Name != "second" {
		t.Fatalf("Load = %+v", ms)
	}
	for name, files := range map[string]fstest.MapFS{
		"short number":      {"1_a.sql": {Data: []byte("x")}},
		"no name":           {"0001_.sql": {Data: []byte("x")}},
		"zero":              {"0000_a.sql": {Data: []byte("x")}},
		"not a number":      {"00a1_a.sql": {Data: []byte("x")}},
		"duplicate version": {"0001_a.sql": {Data: []byte("x")}, "0001_b.sql": {Data: []byte("y")}},
	} {
		if _, err := migrate.Load(files); err == nil {
			t.Errorf("%s: Load must fail", name)
		}
	}
}

func TestChecksum_IgnoresLineEndings(t *testing.T) {
	t.Parallel()
	if migrate.Checksum("a;\r\nb;\r\n") != migrate.Checksum("a;\nb;\n") {
		t.Fatal("CRLF and LF content must have the same checksum")
	}
	if migrate.Checksum("a;") == migrate.Checksum("b;") {
		t.Fatal("different content must have different checksums")
	}
}

func TestRun_AppliesInOrderAndIsIdempotent(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "m.db"))
	ms := load(t, twoMigrations())
	applied, err := migrate.Run(t.Context(), db, dialect(), ms)
	if err != nil || !reflect.DeepEqual(applied, []int{1, 2}) {
		t.Fatalf("first run = %v, %v; want [1 2]", applied, err)
	}
	applied, err = migrate.Run(t.Context(), db, dialect(), ms)
	if err != nil || len(applied) != 0 {
		t.Fatalf("second run = %v, %v; want nothing applied", applied, err)
	}
	var n int
	if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM jollyroger_schema_migrations").Scan(&n); err != nil || n != 2 {
		t.Fatalf("recorded migrations = %d, %v; want 2", n, err)
	}
}

func TestRun_DetectsDrift(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "m.db"))
	if _, err := migrate.Run(t.Context(), db, dialect(), load(t, twoMigrations())); err != nil {
		t.Fatal(err)
	}
	changed := twoMigrations()
	changed["0001_first.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE t1 (id INTEGER, extra TEXT);")}
	if _, err := migrate.Run(t.Context(), db, dialect(), load(t, changed)); !errors.Is(err, migrate.ErrDrift) {
		t.Fatalf("err = %v, want ErrDrift", err)
	}
}

func TestRun_ToleratesNewerAppliedVersions(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "m.db"))
	if _, err := migrate.Run(t.Context(), db, dialect(), load(t, twoMigrations())); err != nil {
		t.Fatal(err)
	}
	older := twoMigrations()
	delete(older, "0002_second.sql") // an older build during a rolling deploy
	applied, err := migrate.Run(t.Context(), db, dialect(), load(t, older))
	if err != nil || len(applied) != 0 {
		t.Fatalf("older build run = %v, %v; want nothing applied and no error", applied, err)
	}
}

func TestRun_IsAtomic(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "m.db"))
	broken := twoMigrations()
	broken["0002_second.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE ;")}
	if _, err := migrate.Run(t.Context(), db, dialect(), load(t, broken)); err == nil {
		t.Fatal("a broken migration must fail the run")
	}
	var n int
	if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM sqlite_master WHERE name IN ('t1', 'jollyroger_schema_migrations')").Scan(&n); err != nil || n != 0 {
		t.Fatalf("tables left behind = %d, %v; the whole run must roll back", n, err)
	}
}

func TestRun_ConcurrentRunnersApplyOnce(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "m.db")
	dbs := []*sql.DB{openDB(t, path), openDB(t, path), openDB(t, path)}
	// Open each handle's first connection before the race: switching the file to WAL (a
	// connection-open pragma) is the host's setup, not what this test measures.
	for _, db := range dbs {
		if err := db.PingContext(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	ms := load(t, twoMigrations())
	results := make([][]int, len(dbs))
	errs := make([]error, len(dbs))
	var wg sync.WaitGroup
	for i, db := range dbs {
		wg.Go(func() { results[i], errs[i] = migrate.Run(t.Context(), db, dialect(), ms) })
	}
	wg.Wait()
	total := 0
	for i := range dbs {
		if errs[i] != nil {
			t.Fatalf("runner %d: %v", i, errs[i])
		}
		total += len(results[i])
	}
	if total != 2 {
		t.Fatalf("migrations applied %d times in total, want exactly 2 (results %v)", total, results)
	}
}
