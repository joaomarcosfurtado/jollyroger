// Package storetest is the conformance suite every model.FlagStore implementation must pass: the
// in-memory store, SQLite, and PostgreSQL run the same cases, so a test double can never be more
// generous than a real database. It stays internal until the store port becomes public API.
package storetest

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/joaomarcosfurtado/jollyroger/internal/model"
)

// Factory returns a fresh, migrated store no other test uses: project "default" with environments
// development, staging and production (positions 1, 2, 3) and revision 0.
type Factory func(t *testing.T) model.FlagStore

// Run runs every conformance case against stores from newStore.
func Run(t *testing.T, newStore Factory) {
	t.Helper()
	cases := []struct {
		name string
		fn   func(t *testing.T, s model.FlagStore)
	}{
		{"SeedsDefaultProject", seedsDefaultProject},
		{"UnknownProjectOrEnvironmentIsNotFound", unknownProjectOrEnvironment},
		{"CreateFlagCreatesDisabledStates", createFlag},
		{"CreateDuplicateKeyFailsAndRollsBack", createDuplicate},
		{"CreateInUnknownProjectIsNotFound", createUnknownProject},
		{"FailedTransactionLeavesNothing", failedTransaction},
		{"TransactionReadsItsOwnWrites", readsOwnWrites},
		{"BumpRevision", bumpRevision},
		{"SetEnvState", setEnvState},
		{"ConcurrentSetEnvStateOneWins", concurrentSetEnvState},
		{"ConfigRoundTrip", configRoundTrip},
		{"UpdateFlagMeta", updateFlagMeta},
		{"ArchiveAndRestore", archiveAndRestore},
		{"LoadSnapshot", loadSnapshot},
		{"ListFlagsPaginatesInByteOrder", listFlagsPagination},
		{"AuditLog", auditLog},
		{"TimesAreUTCWithMicroseconds", timesAreUTC},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			c.fn(t, newStore(t))
		})
	}
}

// baseTime has nanoseconds on purpose: stores keep microseconds.
func baseTime() time.Time { return time.Date(2026, 9, 27, 12, 0, 0, 123456789, time.UTC) }

func us(t time.Time) time.Time { return t.UTC().Truncate(model.TimePrecision) }

func ptr[T any](v T) *T { return &v }

const project = model.DefaultProject

func inTx(t *testing.T, s model.FlagStore, fn func(ctx context.Context, tx model.FlagTx) error) error {
	t.Helper()
	return s.InTx(t.Context(), func(tx model.FlagTx) error { return fn(t.Context(), tx) })
}

func mustTx(t *testing.T, s model.FlagStore, fn func(ctx context.Context, tx model.FlagTx) error) {
	t.Helper()
	if err := inTx(t, s, fn); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

func newFlag(key string, at time.Time) model.NewFlag {
	return model.NewFlag{ID: "flag-" + key, Project: project, Key: key, Name: "Flag " + key,
		Description: "about " + key, Kind: model.KindBoolean, At: at, Actor: "alice"}
}

func mustCreate(t *testing.T, s model.FlagStore, key string, at time.Time) model.FlagWithStates {
	t.Helper()
	var out model.FlagWithStates
	mustTx(t, s, func(ctx context.Context, tx model.FlagTx) error {
		var err error
		out, err = tx.CreateFlag(ctx, newFlag(key, at))
		return err
	})
	return out
}

func mustGet(t *testing.T, s model.FlagStore, key string) model.FlagWithStates {
	t.Helper()
	f, err := s.GetFlag(t.Context(), project, key)
	if err != nil {
		t.Fatalf("GetFlag(%q): %v", key, err)
	}
	return f
}

func setState(t *testing.T, s model.FlagStore, key, env string, c model.EnvStateChange, expected int64) (model.EnvState, error) {
	t.Helper()
	var st model.EnvState
	err := inTx(t, s, func(ctx context.Context, tx model.FlagTx) error {
		var err error
		st, err = tx.SetEnvState(ctx, project, key, env, c, expected)
		return err
	})
	return st, err
}

func expectErr(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s: err = %v, want %v", what, err, want)
	}
}

func seedsDefaultProject(t *testing.T, s model.FlagStore) {
	envs, err := s.ListEnvironments(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	type env struct {
		key, name string
		pos       int
	}
	var got []env
	ids := map[string]bool{}
	for _, e := range envs {
		got = append(got, env{e.Key, e.Name, e.Position})
		if e.ID == "" || ids[e.ID] {
			t.Errorf("environment %q has an empty or repeated ID %q", e.Key, e.ID)
		}
		ids[e.ID] = true
	}
	want := []env{{"development", "Development", 1}, {"staging", "Staging", 2}, {"production", "Production", 3}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environments = %v, want %v", got, want)
	}
	if rev, err := s.Revision(t.Context(), project); err != nil || rev != 0 {
		t.Fatalf("Revision = %d, %v; want 0", rev, err)
	}
	snap, err := s.LoadSnapshot(t.Context(), project, "production")
	if err != nil || snap.Environment != "production" || snap.Revision != 0 || len(snap.Flags) != 0 {
		t.Fatalf("LoadSnapshot = %+v, %v; want an empty production snapshot at revision 0", snap, err)
	}
}

func unknownProjectOrEnvironment(t *testing.T, s model.FlagStore) {
	ctx := t.Context()
	_, err := s.Revision(ctx, "nope")
	expectErr(t, "Revision(nope)", err, model.ErrNotFound)
	_, err = s.ListEnvironments(ctx, "nope")
	expectErr(t, "ListEnvironments(nope)", err, model.ErrNotFound)
	_, err = s.LoadSnapshot(ctx, "nope", "production")
	expectErr(t, "LoadSnapshot(nope)", err, model.ErrNotFound)
	_, err = s.LoadSnapshot(ctx, project, "qa")
	expectErr(t, "LoadSnapshot(qa)", err, model.ErrNotFound)
	_, err = s.GetFlag(ctx, project, "missing")
	expectErr(t, "GetFlag(missing)", err, model.ErrNotFound)
	_, err = s.ListFlags(ctx, model.FlagQuery{Project: "nope", Environment: "production"})
	expectErr(t, "ListFlags(nope)", err, model.ErrNotFound)
	_, err = s.ListFlags(ctx, model.FlagQuery{Project: project, Environment: "qa"})
	expectErr(t, "ListFlags(qa)", err, model.ErrNotFound)
	_, err = s.ListAudit(ctx, model.AuditQuery{Project: "nope"})
	expectErr(t, "ListAudit(nope)", err, model.ErrNotFound)
	err = inTx(t, s, func(ctx context.Context, tx model.FlagTx) error {
		_, err := tx.BumpRevision(ctx, "nope")
		return err
	})
	expectErr(t, "BumpRevision(nope)", err, model.ErrNotFound)
}

func createFlag(t *testing.T, s model.FlagStore) {
	at := baseTime()
	got := mustCreate(t, s, "new-checkout", at)
	want := model.FlagWithStates{
		Flag: model.Flag{ID: "flag-new-checkout", Key: "new-checkout", Name: "Flag new-checkout", Description: "about new-checkout",
			Kind: model.KindBoolean, CreatedAt: us(at), UpdatedAt: us(at)},
	}
	for _, env := range []string{"development", "staging", "production"} {
		want.States = append(want.States, model.EnvState{EnvironmentKey: env, Enabled: false, Version: 1, UpdatedAt: us(at), UpdatedBy: "alice"})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CreateFlag = %#v\nwant %#v", got, want)
	}
	if again := mustGet(t, s, "new-checkout"); !reflect.DeepEqual(again, want) {
		t.Fatalf("GetFlag = %#v\nwant %#v", again, want)
	}
}

func createDuplicate(t *testing.T, s model.FlagStore) {
	mustCreate(t, s, "dup", baseTime())
	err := inTx(t, s, func(ctx context.Context, tx model.FlagTx) error {
		if _, err := tx.BumpRevision(ctx, project); err != nil {
			return err
		}
		f := newFlag("dup", baseTime())
		f.ID = "flag-dup-2"
		_, err := tx.CreateFlag(ctx, f)
		return err
	})
	expectErr(t, "second CreateFlag(dup)", err, model.ErrAlreadyExists)
	if rev, _ := s.Revision(t.Context(), project); rev != 0 {
		t.Fatalf("revision = %d after a failed transaction, want 0", rev)
	}
	mustTx(t, s, func(ctx context.Context, tx model.FlagTx) error {
		_, err := tx.ArchiveFlag(ctx, project, "dup", baseTime())
		return err
	})
	err = inTx(t, s, func(ctx context.Context, tx model.FlagTx) error {
		f := newFlag("dup", baseTime())
		f.ID = "flag-dup-3"
		_, err := tx.CreateFlag(ctx, f)
		return err
	})
	expectErr(t, "CreateFlag over an archived key", err, model.ErrAlreadyExists)
}

func createUnknownProject(t *testing.T, s model.FlagStore) {
	err := inTx(t, s, func(ctx context.Context, tx model.FlagTx) error {
		f := newFlag("x", baseTime())
		f.Project = "nope"
		_, err := tx.CreateFlag(ctx, f)
		return err
	})
	expectErr(t, "CreateFlag in unknown project", err, model.ErrNotFound)
}

func failedTransaction(t *testing.T, s model.FlagStore) {
	boom := errors.New("boom")
	err := inTx(t, s, func(ctx context.Context, tx model.FlagTx) error {
		if _, err := tx.CreateFlag(ctx, newFlag("x", baseTime())); err != nil {
			return err
		}
		if _, err := tx.BumpRevision(ctx, project); err != nil {
			return err
		}
		return boom
	})
	expectErr(t, "InTx", err, boom)
	_, err = s.GetFlag(t.Context(), project, "x")
	expectErr(t, "GetFlag after rollback", err, model.ErrNotFound)
	if rev, _ := s.Revision(t.Context(), project); rev != 0 {
		t.Fatalf("revision = %d after rollback, want 0", rev)
	}
}

func readsOwnWrites(t *testing.T, s model.FlagStore) {
	mustTx(t, s, func(ctx context.Context, tx model.FlagTx) error {
		if _, err := tx.CreateFlag(ctx, newFlag("x", baseTime())); err != nil {
			return err
		}
		f, err := tx.GetFlag(ctx, project, "x")
		if err != nil {
			return err
		}
		if f.Flag.Key != "x" || len(f.States) != 3 {
			t.Errorf("GetFlag inside the transaction = %+v", f)
		}
		return nil
	})
}

func bumpRevision(t *testing.T, s model.FlagStore) {
	mustTx(t, s, func(ctx context.Context, tx model.FlagTx) error {
		for want := int64(1); want <= 2; want++ {
			got, err := tx.BumpRevision(ctx, project)
			if err != nil {
				return err
			}
			if got != want {
				t.Errorf("BumpRevision = %d, want %d", got, want)
			}
		}
		return nil
	})
	if rev, err := s.Revision(t.Context(), project); err != nil || rev != 2 {
		t.Fatalf("Revision = %d, %v; want 2", rev, err)
	}
}

func setEnvState(t *testing.T, s model.FlagStore) {
	mustCreate(t, s, "f", baseTime())
	t1 := baseTime().Add(time.Minute)
	change := model.EnvStateChange{Enabled: true, Config: model.FlagConfig{Fallthrough: model.Serve{Value: ptr(false)}}, At: t1, Actor: "bob"}
	got, err := setState(t, s, "f", "production", change, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := model.EnvState{EnvironmentKey: "production", Enabled: true, Config: change.Config, Version: 2, UpdatedAt: us(t1), UpdatedBy: "bob"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SetEnvState = %#v, want %#v", got, want)
	}
	f := mustGet(t, s, "f")
	if !reflect.DeepEqual(f.States[2], want) || f.States[1].Version != 1 || f.States[1].Enabled {
		t.Fatalf("states after update = %#v", f.States)
	}
	_, err = setState(t, s, "f", "production", change, 1)
	expectErr(t, "stale version", err, model.ErrConflict)
	if again := mustGet(t, s, "f"); again.States[2].Version != 2 {
		t.Fatalf("a conflicting write changed the state: %#v", again.States[2])
	}
	_, err = setState(t, s, "f", "qa", change, 1)
	expectErr(t, "unknown environment", err, model.ErrNotFound)
	_, err = setState(t, s, "missing", "production", change, 1)
	expectErr(t, "unknown flag", err, model.ErrNotFound)
}

func concurrentSetEnvState(t *testing.T, s model.FlagStore) {
	mustCreate(t, s, "race", baseTime())
	const writers = 4
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			change := model.EnvStateChange{Enabled: i%2 == 0, At: baseTime(), Actor: "writer"}
			_, errs[i] = setState(t, s, "race", "production", change, 1)
		})
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, model.ErrConflict):
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d writers succeeded with the same expected version, want exactly 1", wins)
	}
	if v := mustGet(t, s, "race").States[2].Version; v != 2 {
		t.Fatalf("version = %d, want 2", v)
	}
}

func configRoundTrip(t *testing.T, s model.FlagStore) {
	mustCreate(t, s, "c", baseTime())
	if _, err := setState(t, s, "c", "production", model.EnvStateChange{Enabled: true, Config: model.FlagConfig{Unparseable: true}, At: baseTime()}, 1); err != nil {
		t.Fatal(err)
	}
	snap, err := s.LoadSnapshot(t.Context(), project, "production")
	if err != nil || len(snap.Flags) != 1 || !snap.Flags[0].Config.Unparseable {
		t.Fatalf("an unparseable config must read back as Unparseable: %+v, %v", snap, err)
	}
	if _, err := setState(t, s, "c", "production", model.EnvStateChange{Enabled: true, Config: model.FlagConfig{Fallthrough: model.Serve{Value: ptr(true)}}, At: baseTime()}, 2); err != nil {
		t.Fatal(err)
	}
	snap, _ = s.LoadSnapshot(t.Context(), project, "production")
	if want := (model.FlagConfig{Fallthrough: model.Serve{Value: ptr(true)}}); !reflect.DeepEqual(snap.Flags[0].Config, want) {
		t.Fatalf("config = %#v, want %#v", snap.Flags[0].Config, want)
	}
}

func updateFlagMeta(t *testing.T, s model.FlagStore) {
	mustCreate(t, s, "m", baseTime())
	t1 := baseTime().Add(time.Hour)
	var got model.Flag
	mustTx(t, s, func(ctx context.Context, tx model.FlagTx) error {
		var err error
		got, err = tx.UpdateFlagMeta(ctx, project, "m", model.FlagMeta{Name: "Renamed", Description: "", At: t1})
		return err
	})
	want := model.Flag{ID: "flag-m", Key: "m", Name: "Renamed", Description: "", Kind: model.KindBoolean, CreatedAt: us(baseTime()), UpdatedAt: us(t1)}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(mustGet(t, s, "m").Flag, want) {
		t.Fatalf("UpdateFlagMeta = %#v, want %#v", got, want)
	}
	err := inTx(t, s, func(ctx context.Context, tx model.FlagTx) error {
		_, err := tx.UpdateFlagMeta(ctx, project, "missing", model.FlagMeta{Name: "x", At: t1})
		return err
	})
	expectErr(t, "UpdateFlagMeta(missing)", err, model.ErrNotFound)
}

func archiveAndRestore(t *testing.T, s model.FlagStore) {
	mustCreate(t, s, "a", baseTime())
	mustCreate(t, s, "b", baseTime())
	t1, t2, t3, t4 := baseTime().Add(time.Hour), baseTime().Add(2*time.Hour), baseTime().Add(3*time.Hour), baseTime().Add(4*time.Hour)
	call := func(archive bool, key string, at time.Time) (model.Flag, error) {
		var f model.Flag
		err := inTx(t, s, func(ctx context.Context, tx model.FlagTx) error {
			var err error
			if archive {
				f, err = tx.ArchiveFlag(ctx, project, key, at)
			} else {
				f, err = tx.RestoreFlag(ctx, project, key, at)
			}
			return err
		})
		return f, err
	}
	f, err := call(true, "a", t1)
	if err != nil || f.ArchivedAt == nil || !f.ArchivedAt.Equal(us(t1)) || !f.UpdatedAt.Equal(us(t1)) {
		t.Fatalf("ArchiveFlag = %+v, %v", f, err)
	}
	if f, _ := call(true, "a", t2); f.ArchivedAt == nil || !f.ArchivedAt.Equal(us(t1)) || !f.UpdatedAt.Equal(us(t1)) {
		t.Fatalf("archiving an archived flag must change nothing: %+v", f)
	}
	snap, _ := s.LoadSnapshot(t.Context(), project, "production")
	if len(snap.Flags) != 1 || snap.Flags[0].Key != "b" {
		t.Fatalf("snapshot must exclude archived flags: %+v", snap.Flags)
	}
	page, _ := s.ListFlags(t.Context(), model.FlagQuery{Project: project, Environment: "production"})
	if len(page.Items) != 1 {
		t.Fatalf("ListFlags must exclude archived flags by default: %d items", len(page.Items))
	}
	page, _ = s.ListFlags(t.Context(), model.FlagQuery{Project: project, Environment: "production", IncludeArchived: true})
	if len(page.Items) != 2 {
		t.Fatalf("ListFlags with IncludeArchived: %d items, want 2", len(page.Items))
	}
	if g := mustGet(t, s, "a"); g.Flag.ArchivedAt == nil {
		t.Fatal("GetFlag must return archived flags")
	}
	f, err = call(false, "a", t3)
	if err != nil || f.ArchivedAt != nil || !f.UpdatedAt.Equal(us(t3)) {
		t.Fatalf("RestoreFlag = %+v, %v", f, err)
	}
	if f, _ := call(false, "a", t4); !f.UpdatedAt.Equal(us(t3)) {
		t.Fatalf("restoring an active flag must change nothing: %+v", f)
	}
	snap, _ = s.LoadSnapshot(t.Context(), project, "production")
	if len(snap.Flags) != 2 {
		t.Fatalf("restored flag must be back in the snapshot: %+v", snap.Flags)
	}
	_, err = call(true, "missing", t1)
	expectErr(t, "ArchiveFlag(missing)", err, model.ErrNotFound)
	_, err = call(false, "missing", t1)
	expectErr(t, "RestoreFlag(missing)", err, model.ErrNotFound)
}

func loadSnapshot(t *testing.T, s model.FlagStore) {
	for _, k := range []string{"b", "a", "c"} {
		mustCreate(t, s, k, baseTime())
	}
	if _, err := setState(t, s, "a", "production", model.EnvStateChange{Enabled: true, Config: model.FlagConfig{Fallthrough: model.Serve{Value: ptr(false)}}, At: baseTime()}, 1); err != nil {
		t.Fatal(err)
	}
	mustTx(t, s, func(ctx context.Context, tx model.FlagTx) error {
		_, err := tx.BumpRevision(ctx, project)
		return err
	})
	snap, err := s.LoadSnapshot(t.Context(), project, "production")
	if err != nil {
		t.Fatal(err)
	}
	want := model.Snapshot{Environment: "production", Revision: 1, Flags: []model.SnapshotFlag{
		{Key: "a", Enabled: true, Version: 2, Config: model.FlagConfig{Fallthrough: model.Serve{Value: ptr(false)}}},
		{Key: "b", Enabled: false, Version: 1},
		{Key: "c", Enabled: false, Version: 1},
	}}
	if !reflect.DeepEqual(snap, want) {
		t.Fatalf("LoadSnapshot(production) = %#v\nwant %#v", snap, want)
	}
	staging, _ := s.LoadSnapshot(t.Context(), project, "staging")
	if staging.Flags[0].Enabled || staging.Flags[0].Version != 1 {
		t.Fatalf("staging must be untouched: %+v", staging.Flags[0])
	}
}

func listFlagsPagination(t *testing.T, s model.FlagStore) {
	// Byte order: '-' (0x2d) < '.' (0x2e) < '0' (0x30) < '_' (0x5f) < 'b' (0x62). A locale
	// collation would order these differently; every store must use byte order.
	for _, k := range []string{"ab", "a_b", "a0", "a.b", "a-b"} {
		mustCreate(t, s, k, baseTime())
	}
	var keys []string
	after := ""
	for pages := 0; ; pages++ {
		if pages > 3 {
			t.Fatal("pagination did not terminate")
		}
		page, err := s.ListFlags(t.Context(), model.FlagQuery{Project: project, Environment: "staging", AfterKey: after, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range page.Items {
			keys = append(keys, v.Flag.Key)
			if v.State.EnvironmentKey != "staging" {
				t.Errorf("view of %q carries %q state, want staging", v.Flag.Key, v.State.EnvironmentKey)
			}
		}
		if !page.HasMore {
			break
		}
		after = page.Items[len(page.Items)-1].Flag.Key
	}
	if want := []string{"a-b", "a.b", "a0", "a_b", "ab"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	all, err := s.ListFlags(t.Context(), model.FlagQuery{Project: project, Environment: "staging"})
	if err != nil || len(all.Items) != 5 || all.HasMore {
		t.Fatalf("default limit page = %d items, HasMore %v, %v", len(all.Items), all.HasMore, err)
	}
}

func auditLog(t *testing.T, s model.FlagStore) {
	at := baseTime()
	e1 := model.AuditEntry{ID: "audit-0001", Project: project, ActorID: "u1", ActorName: "Alice", Action: model.AuditPruned, CreatedAt: us(at)}
	e2 := model.AuditEntry{ID: "audit-0002", Project: project, EnvironmentKey: "production", FlagKey: "x", ActorID: "u2", ActorName: "Bob",
		Action: model.AuditFlagStateChanged, Before: map[string]any{"enabled": false}, After: map[string]any{"enabled": true, "name": "X"},
		Reason: "rollout", CreatedAt: us(at.Add(time.Second))}
	e3 := model.AuditEntry{ID: "audit-0003", Project: project, FlagKey: "y", ActorID: "u1", ActorName: "Alice",
		Action: model.AuditFlagCreated, After: map[string]any{"key": "y"}, CreatedAt: us(at.Add(2 * time.Second))}
	mustTx(t, s, func(ctx context.Context, tx model.FlagTx) error {
		for _, e := range []model.AuditEntry{e1, e2, e3} {
			if err := tx.AppendAudit(ctx, e); err != nil {
				return err
			}
		}
		return nil
	})
	check := func(q model.AuditQuery, want []model.AuditEntry, wantMore bool) {
		t.Helper()
		q.Project = project
		page, err := s.ListAudit(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(page.Items, want) || page.HasMore != wantMore {
			t.Fatalf("ListAudit(%+v) = %#v (more %v)\nwant %#v (more %v)", q, page.Items, page.HasMore, want, wantMore)
		}
	}
	check(model.AuditQuery{}, []model.AuditEntry{e3, e2, e1}, false)
	check(model.AuditQuery{FlagKey: "x"}, []model.AuditEntry{e2}, false)
	check(model.AuditQuery{EnvironmentKey: "production"}, []model.AuditEntry{e2}, false)
	check(model.AuditQuery{BeforeID: "audit-0003", Limit: 1}, []model.AuditEntry{e2}, true)
	err := inTx(t, s, func(ctx context.Context, tx model.FlagTx) error {
		e := e1
		e.ID, e.Project = "audit-0009", "nope"
		return tx.AppendAudit(ctx, e)
	})
	expectErr(t, "AppendAudit in unknown project", err, model.ErrNotFound)
}

func timesAreUTC(t *testing.T, s model.FlagStore) {
	brt := time.FixedZone("BRT", -3*60*60)
	at := time.Date(2026, 9, 27, 9, 0, 0, 999999999, brt)
	got := mustCreate(t, s, "tz", at)
	want := time.Date(2026, 9, 27, 12, 0, 0, 999999000, time.UTC)
	for _, ts := range []time.Time{got.Flag.CreatedAt, got.Flag.UpdatedAt, got.States[0].UpdatedAt, mustGet(t, s, "tz").Flag.CreatedAt} {
		if !ts.Equal(want) || ts.Location() != time.UTC {
			t.Fatalf("timestamp %v (%v), want %v in UTC", ts, ts.Location(), want)
		}
	}
}
