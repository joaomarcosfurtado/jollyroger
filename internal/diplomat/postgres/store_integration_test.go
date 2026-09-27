//go:build integration

package postgres_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joaomarcosfurtado/jollyroger/internal/diplomat/postgres"
	"github.com/joaomarcosfurtado/jollyroger/internal/model"
	"github.com/joaomarcosfurtado/jollyroger/internal/storetest"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// defaultURL is the throwaway local database from docker-compose.yml. Its password is public test
// setup, not a secret, which makes gosec's G101 a false positive here.
const defaultURL = "postgres://jollyroger:jollyroger@localhost:55432/jollyroger_test?sslmode=disable" //nolint:gosec // G101: public local test credentials, see above

func testURL() string {
	if v := os.Getenv("JOLLYROGER_TEST_POSTGRES_URL"); v != "" {
		return v
	}
	return defaultURL
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", testURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("PostgreSQL unreachable at %s: %v (run `docker compose up -d`)", testURL(), err)
	}
	return db
}

// freshSchema returns a schema name no other test uses and drops it when the test ends.
func freshSchema(t *testing.T, db *sql.DB) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	schema := "jr_test_" + hex.EncodeToString(b)
	t.Cleanup(func() {
		stmt := strings.ReplaceAll(`DROP SCHEMA IF EXISTS "{s}" CASCADE`, "{s}", schema)
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			t.Errorf("drop %s: %v", schema, err)
		}
	})
	return schema
}

func migrated(t *testing.T, db *sql.DB, schema string) *postgres.Store {
	t.Helper()
	s, err := postgres.New(db, schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s
}

func newStore(t *testing.T) model.FlagStore {
	db := openDB(t)
	return migrated(t, db, freshSchema(t, db))
}

func TestConformance(t *testing.T) { storetest.Run(t, newStore) }

func TestMigrate_IsIdempotentAndRecordsSchemaMajor(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	schema := freshSchema(t, db)
	s, err := postgres.New(db, schema)
	if err != nil {
		t.Fatal(err)
	}
	if applied, err := s.Migrate(t.Context()); err != nil || !reflect.DeepEqual(applied, []int{1}) {
		t.Fatalf("first Migrate = %v, %v", applied, err)
	}
	if applied, err := s.Migrate(t.Context()); err != nil || len(applied) != 0 {
		t.Fatalf("second Migrate = %v, %v", applied, err)
	}
	var major int
	q := strings.ReplaceAll(`SELECT schema_major FROM "{s}".jollyroger_schema_info WHERE id = 1`, "{s}", schema)
	if err := db.QueryRowContext(t.Context(), q).Scan(&major); err != nil || major != 1 {
		t.Fatalf("schema_major = %d, %v; want 1", major, err)
	}
}

func TestMigrate_ConcurrentRunnersApplyOnce(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	schema := freshSchema(t, db)
	stores := make([]*postgres.Store, 3)
	for i := range stores {
		s, err := postgres.New(openDB(t), schema)
		if err != nil {
			t.Fatal(err)
		}
		stores[i] = s
	}
	results := make([][]int, len(stores))
	errs := make([]error, len(stores))
	var wg sync.WaitGroup
	for i, s := range stores {
		wg.Go(func() { results[i], errs[i] = s.Migrate(t.Context()) })
	}
	wg.Wait()
	total := 0
	for i := range stores {
		if errs[i] != nil {
			t.Fatalf("runner %d: %v", i, errs[i])
		}
		total += len(results[i])
	}
	if total != 1 {
		t.Fatalf("migration 1 applied %d times, want exactly once (results %v)", total, results)
	}
}

func TestAuditLog_IsAppendOnly(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	schema := freshSchema(t, db)
	s := migrated(t, db, schema)
	err := s.InTx(t.Context(), func(tx model.FlagTx) error {
		return tx.AppendAudit(t.Context(), model.AuditEntry{ID: "a1", Project: model.DefaultProject, ActorID: "u", ActorName: "U", Action: model.AuditFlagCreated, CreatedAt: time.Now()})
	})
	if err != nil {
		t.Fatal(err)
	}
	q := func(stmt string) string { return strings.ReplaceAll(stmt, "{s}", `"`+schema+`"`) }
	ctx := t.Context()
	for _, stmt := range []string{
		`UPDATE {s}.jollyroger_audit_log SET actor_name = 'Mallory'`,
		`DELETE FROM {s}.jollyroger_audit_log`,
		`TRUNCATE {s}.jollyroger_audit_log`,
		`INSERT INTO {s}.jollyroger_audit_log (id, project_id, actor_id, actor_name, action, created_at)
		 VALUES ('a1', '00000000000000000000000000', 'm', 'Mallory', 'FORGED', now())
		 ON CONFLICT (id) DO UPDATE SET action = excluded.action`,
	} {
		if _, err := db.ExecContext(ctx, q(stmt)); err == nil {
			t.Fatalf("%s must fail", stmt)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`UPDATE {s}.jollyroger_audit_prune_guard SET active = TRUE WHERE id = 1`,
		`DELETE FROM {s}.jollyroger_audit_log`,
		`UPDATE {s}.jollyroger_audit_prune_guard SET active = FALSE WHERE id = 1`,
	} {
		if _, err := tx.ExecContext(ctx, q(stmt)); err != nil {
			_ = tx.Rollback()
			t.Fatalf("%s: %v (a prune with the guard on must succeed)", stmt, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSnapshot_IsolatesAConfigFromANewerVersion(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	schema := freshSchema(t, db)
	s := migrated(t, db, schema)
	for _, key := range []string{"newer", "plain"} {
		err := s.InTx(t.Context(), func(tx model.FlagTx) error {
			_, err := tx.CreateFlag(t.Context(), model.NewFlag{ID: "id-" + key, Project: model.DefaultProject, Key: key, Name: key, Kind: model.KindBoolean, At: time.Now(), Actor: "t"})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	stmt := strings.ReplaceAll(`UPDATE "{s}".jollyroger_flag_env_states SET config = '{"rules":[],"fallthrough":{},"schedule":{"from":"2027"}}' WHERE flag_id = 'id-newer'`, "{s}", schema)
	if _, err := db.ExecContext(t.Context(), stmt); err != nil {
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
	db := openDB(t)
	schema := freshSchema(t, db)
	s := migrated(t, db, schema)
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
	// The contract's SQL is unqualified: an SDK reads the host's configured schema via search_path.
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := conn.ExecContext(t.Context(), strings.ReplaceAll(`SET search_path TO "{s}"`, "{s}", schema)); err != nil {
		t.Fatal(err)
	}
	stmts := contractQueries(t)
	var rev int64
	if err := conn.QueryRowContext(t.Context(), strings.ReplaceAll(stmts[0], ":project", "$1"), model.DefaultProject).Scan(&rev); err != nil {
		t.Fatalf("contract revision query: %v", err)
	}
	snapSQL := strings.NewReplacer(":project", "$1", ":environment", "$2").Replace(stmts[1])
	rows, err := conn.QueryContext(t.Context(), snapSQL, model.DefaultProject, "production")
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
