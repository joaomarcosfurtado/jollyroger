package sqlite_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/joaomarcosfurtado/jollyroger/internal/diplomat/sqlite"
	"github.com/joaomarcosfurtado/jollyroger/internal/model"
	"github.com/joaomarcosfurtado/jollyroger/internal/storetest"

	_ "modernc.org/sqlite"
)

const hostPragmas = "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"

func openDB(t *testing.T, path, pragmas string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func migrated(t *testing.T, db *sql.DB) *sqlite.Store {
	t.Helper()
	s := sqlite.New(db)
	if _, err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s
}

func newStore(t *testing.T) model.FlagStore {
	return migrated(t, openDB(t, filepath.Join(t.TempDir(), "jr.db"), hostPragmas))
}

func TestConformance(t *testing.T) { storetest.Run(t, newStore) }

func TestMigrate_IsIdempotentAndRecordsSchemaMajor(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "jr.db"), hostPragmas)
	s := sqlite.New(db)
	if applied, err := s.Migrate(t.Context()); err != nil || !reflect.DeepEqual(applied, []int{1}) {
		t.Fatalf("first Migrate = %v, %v", applied, err)
	}
	if applied, err := s.Migrate(t.Context()); err != nil || len(applied) != 0 {
		t.Fatalf("second Migrate = %v, %v", applied, err)
	}
	var major int
	if err := db.QueryRowContext(t.Context(), "SELECT schema_major FROM jollyroger_schema_info WHERE id = 1").Scan(&major); err != nil || major != 1 {
		t.Fatalf("schema_major = %d, %v; want 1", major, err)
	}
}

func TestMigrate_ConcurrentRunnersApplyOnce(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "jr.db")
	dbs := []*sql.DB{openDB(t, path, hostPragmas), openDB(t, path, hostPragmas)}
	for _, db := range dbs {
		if err := db.PingContext(t.Context()); err != nil { // the host opens its pool before booting
			t.Fatal(err)
		}
	}
	stores := []*sqlite.Store{sqlite.New(dbs[0]), sqlite.New(dbs[1])}
	results := make([][]int, len(stores))
	errs := make([]error, len(stores))
	var wg sync.WaitGroup
	for i, s := range stores {
		wg.Go(func() { results[i], errs[i] = s.Migrate(t.Context()) })
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || len(results[0])+len(results[1]) != 1 {
		t.Fatalf("results %v, errors %v; want migration 1 applied exactly once", results, errs)
	}
}

func TestAuditLog_IsAppendOnly(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "jr.db"), hostPragmas)
	s := migrated(t, db)
	err := s.InTx(t.Context(), func(tx model.FlagTx) error {
		return tx.AppendAudit(t.Context(), model.AuditEntry{ID: "a1", Project: model.DefaultProject, ActorID: "u", ActorName: "U", Action: model.AuditFlagCreated, CreatedAt: time.Now()})
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if _, err := db.ExecContext(ctx, "UPDATE jollyroger_audit_log SET actor_name = 'Mallory'"); err == nil {
		t.Fatal("UPDATE on the audit log must fail")
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM jollyroger_audit_log"); err == nil {
		t.Fatal("DELETE on the audit log must fail while the prune guard is off")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, stmt := range []string{"BEGIN IMMEDIATE", "UPDATE jollyroger_audit_prune_guard SET active = 1 WHERE id = 1",
		"DELETE FROM jollyroger_audit_log", "UPDATE jollyroger_audit_prune_guard SET active = 0 WHERE id = 1", "COMMIT"} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v (a prune with the guard on must succeed)", stmt, err)
		}
	}
}

func TestWaitsForAnotherWriterWithoutHostBusyTimeout(t *testing.T) {
	t.Parallel()
	// The host left busy_timeout at SQLite's default of 0, which fails a blocked writer at once
	// with SQLITE_BUSY. Another writer (a second handle, like another process) holds the write
	// lock for a moment; the store must wait for it instead of failing.
	path := filepath.Join(t.TempDir(), "jr.db")
	s := migrated(t, openDB(t, path, "?_pragma=journal_mode(WAL)"))
	holder, err := openDB(t, path, "?_pragma=journal_mode(WAL)").Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if _, err := holder.ExecContext(t.Context(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	released := make(chan error, 1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		_, err := holder.ExecContext(t.Context(), "COMMIT")
		released <- err
	}()
	err = s.InTx(t.Context(), func(tx model.FlagTx) error {
		_, err := tx.BumpRevision(t.Context(), model.DefaultProject)
		return err
	})
	if err != nil {
		t.Fatalf("a write blocked by another writer must wait, not fail: %v", err)
	}
	if err := <-released; err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentWritersAllSucceed(t *testing.T) {
	t.Parallel()
	s := migrated(t, openDB(t, filepath.Join(t.TempDir(), "jr.db"), "?_pragma=journal_mode(WAL)"))
	const writers = 8
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			errs[i] = s.InTx(t.Context(), func(tx model.FlagTx) error {
				key := fmt.Sprintf("f%d", i)
				if _, err := tx.CreateFlag(t.Context(), model.NewFlag{ID: "id-" + key, Project: model.DefaultProject, Key: key, Name: key, Kind: model.KindBoolean, At: time.Now(), Actor: "t"}); err != nil {
					return err
				}
				_, err := tx.BumpRevision(t.Context(), model.DefaultProject)
				return err
			})
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}
	if rev, err := s.Revision(t.Context(), model.DefaultProject); err != nil || rev != writers {
		t.Fatalf("revision = %d, %v; want %d", rev, err, writers)
	}
}

func TestLoadSnapshot_IsolatesAConfigFromANewerVersion(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "jr.db"), hostPragmas)
	s := migrated(t, db)
	for _, key := range []string{"newer", "plain"} {
		err := s.InTx(t.Context(), func(tx model.FlagTx) error {
			_, err := tx.CreateFlag(t.Context(), model.NewFlag{ID: "id-" + key, Project: model.DefaultProject, Key: key, Name: key, Kind: model.KindBoolean, At: time.Now(), Actor: "t"})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE jollyroger_flag_env_states SET config = '{"rules":[],"fallthrough":{},"schedule":{"from":"2027"}}' WHERE flag_id = 'id-newer'`); err != nil {
		t.Fatal(err)
	}
	snap, err := s.LoadSnapshot(t.Context(), model.DefaultProject, "production")
	if err != nil || len(snap.Flags) != 2 || !snap.Flags[0].Config.Unparseable || snap.Flags[1].Config.Unparseable {
		t.Fatalf("snapshot = %+v, %v; want newer Unparseable and plain intact", snap, err)
	}
}
