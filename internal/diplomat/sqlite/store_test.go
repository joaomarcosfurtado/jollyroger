package sqlite_test

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	// REPLACE deletes the old row without firing delete triggers (recursive_triggers is off by
	// default), so it needs its own guard.
	for _, stmt := range []string{
		`INSERT OR REPLACE INTO jollyroger_audit_log (id, project_id, actor_id, actor_name, action, created_at)
		 VALUES ('a1', '00000000000000000000000000', 'm', 'Mallory', 'FORGED', '2026-01-01T00:00:00.000000Z')`,
		`REPLACE INTO jollyroger_audit_log (id, project_id, actor_id, actor_name, action, created_at)
		 VALUES ('a1', '00000000000000000000000000', 'm', 'Mallory', 'FORGED', '2026-01-01T00:00:00.000000Z')`,
		`INSERT INTO jollyroger_audit_log (id, project_id, actor_id, actor_name, action, created_at)
		 VALUES ('a1', '00000000000000000000000000', 'm', 'Mallory', 'FORGED', '2026-01-01T00:00:00.000000Z')
		 ON CONFLICT (id) DO UPDATE SET action = excluded.action`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err == nil {
			t.Fatalf("%s must fail", stmt)
		}
	}
	var action string
	if err := db.QueryRowContext(ctx, "SELECT action FROM jollyroger_audit_log WHERE id = 'a1'").Scan(&action); err != nil || action != string(model.AuditFlagCreated) {
		t.Fatalf("audit row after forgery attempts: action %q, %v", action, err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}()
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
	defer func() {
		if err := holder.Close(); err != nil {
			t.Error(err)
		}
	}()
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

// contractQueries returns the SQL statements published in protocol/storage-contract.md, so the
// document SDK authors copy is tested against a real database.
func contractQueries(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("../../../protocol/storage-contract.md")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(raw), "```sql\n")
	block, _, ok2 := strings.Cut(rest, "```")
	if !ok || !ok2 {
		t.Fatal("storage-contract.md has no sql block")
	}
	var stmts []string
	for _, s := range strings.Split(block, ";") {
		if s = strings.TrimSpace(s); s != "" {
			stmts = append(stmts, s)
		}
	}
	if len(stmts) != 2 {
		t.Fatalf("want the revision query and the snapshot query, got %d statements", len(stmts))
	}
	return stmts
}

func TestStorageContractQueryMatchesLoadSnapshot(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "jr.db"), hostPragmas)
	s := migrated(t, db)
	err := s.InTx(t.Context(), func(tx model.FlagTx) error {
		for _, key := range []string{"b", "a", "gone"} {
			if _, err := tx.CreateFlag(t.Context(), model.NewFlag{ID: "id-" + key, Project: model.DefaultProject, Key: key, Name: key, Kind: model.KindBoolean, At: time.Now(), Actor: "t"}); err != nil {
				return err
			}
		}
		if _, err := tx.SetEnvState(t.Context(), model.DefaultProject, "a", "production", model.EnvStateChange{Enabled: true, At: time.Now()}, 1); err != nil {
			return err
		}
		if _, err := tx.ArchiveFlag(t.Context(), model.DefaultProject, "gone", time.Now()); err != nil {
			return err
		}
		_, err := tx.BumpRevision(t.Context(), model.DefaultProject)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	stmts := contractQueries(t)
	sqlOf := func(s string) string {
		return strings.NewReplacer(":project", "?", ":environment", "?").Replace(s)
	}
	var rev int64
	if err := db.QueryRowContext(t.Context(), sqlOf(stmts[0]), model.DefaultProject).Scan(&rev); err != nil {
		t.Fatalf("contract revision query: %v", err)
	}
	rows, err := db.QueryContext(t.Context(), sqlOf(stmts[1]), model.DefaultProject, "production")
	if err != nil {
		t.Fatalf("contract snapshot query: %v", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Error(err)
		}
	}()
	type flag struct {
		key     string
		enabled bool
		version int64
	}
	var got []flag
	for rows.Next() {
		var f flag
		var config string
		if err := rows.Scan(&f.key, &f.enabled, &f.version, &config); err != nil {
			t.Fatal(err)
		}
		got = append(got, f)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	snap, err := s.LoadSnapshot(t.Context(), model.DefaultProject, "production")
	if err != nil {
		t.Fatal(err)
	}
	var want []flag
	for _, f := range snap.Flags {
		want = append(want, flag{f.Key, f.Enabled, f.Version})
	}
	if rev != snap.Revision || !reflect.DeepEqual(got, want) {
		t.Fatalf("contract query = rev %d %v; LoadSnapshot = rev %d %v", rev, got, snap.Revision, want)
	}
}

func TestReadsWaitForAWriterWithoutHostBusyTimeout(t *testing.T) {
	t.Parallel()
	// Rollback journal (not WAL) and busy_timeout 0: a reader fails at once with SQLITE_BUSY while
	// another connection holds an exclusive lock, unless the store makes it wait.
	path := filepath.Join(t.TempDir(), "jr.db")
	storeDB := openDB(t, path, "")
	storeDB.SetMaxIdleConns(0) // every call gets a fresh connection, as a busy host pool does
	s := migrated(t, storeDB)
	if err := s.InTx(t.Context(), func(tx model.FlagTx) error {
		_, err := tx.CreateFlag(t.Context(), model.NewFlag{ID: "id-r", Project: model.DefaultProject, Key: "r", Name: "r", Kind: model.KindBoolean, At: time.Now(), Actor: "t"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	other := openDB(t, path, "")
	reads := map[string]func() error{
		"Revision":         func() error { _, err := s.Revision(t.Context(), model.DefaultProject); return err },
		"ListEnvironments": func() error { _, err := s.ListEnvironments(t.Context(), model.DefaultProject); return err },
		"GetFlag":          func() error { _, err := s.GetFlag(t.Context(), model.DefaultProject, "r"); return err },
		"ListFlags": func() error {
			_, err := s.ListFlags(t.Context(), model.FlagQuery{Project: model.DefaultProject, Environment: "production"})
			return err
		},
		"ListAudit": func() error {
			_, err := s.ListAudit(t.Context(), model.AuditQuery{Project: model.DefaultProject})
			return err
		},
		"LoadSnapshot": func() error { _, err := s.LoadSnapshot(t.Context(), model.DefaultProject, "production"); return err },
	}
	for name, read := range reads {
		holder, err := other.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := holder.ExecContext(t.Context(), "BEGIN EXCLUSIVE"); err != nil {
			t.Fatal(err)
		}
		released := make(chan error, 1)
		go func() {
			time.Sleep(150 * time.Millisecond)
			_, err := holder.ExecContext(t.Context(), "COMMIT")
			released <- errors.Join(err, holder.Close())
		}()
		if err := read(); err != nil {
			t.Errorf("%s must wait for the writer, not fail: %v", name, err)
		}
		if err := <-released; err != nil {
			t.Fatal(err)
		}
	}
}
