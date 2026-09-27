package db_test

import (
	"reflect"
	"testing"
	"time"

	adb "github.com/joaomarcosfurtado/jollyroger/internal/adapter/db"
	"github.com/joaomarcosfurtado/jollyroger/internal/model"
	wiredb "github.com/joaomarcosfurtado/jollyroger/internal/wire/db"
)

func ptr[T any](v T) *T { return &v }

func utc() time.Time { return time.Date(2026, 9, 27, 12, 0, 0, 123456000, time.UTC) }

func TestTimeHelpers(t *testing.T) {
	t.Parallel()
	in := time.Date(2026, 9, 27, 9, 0, 0, 123456789, time.FixedZone("BRT", -3*60*60))
	if got := adb.NormalizeTime(in); !got.Equal(utc()) || got.Location() != time.UTC {
		t.Fatalf("NormalizeTime = %v", got)
	}
	if got, want := adb.TextTime(in), "2026-09-27T12:00:00.123456Z"; got != want {
		t.Fatalf("TextTime = %q, want %q", got, want)
	}
	if got, want := adb.TextTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)), "2026-01-02T03:04:05.000000Z"; got != want {
		t.Fatalf("TextTime keeps six fraction digits for lexical order: %q, want %q", got, want)
	}
}

func TestConfigJSON(t *testing.T) {
	t.Parallel()
	empty, err := adb.ConfigToJSON(model.FlagConfig{})
	if err != nil || empty != `{"rules":[],"fallthrough":{}}` {
		t.Fatalf("empty config = %q, %v", empty, err)
	}
	bad, err := adb.ConfigToJSON(model.FlagConfig{Unparseable: true})
	if err != nil || bad != `{"unparseable":true}` {
		t.Fatalf("unparseable config = %q, %v", bad, err)
	}
	fixed := model.FlagConfig{Fallthrough: model.Serve{Value: ptr(false)}}
	s, err := adb.ConfigToJSON(fixed)
	if err != nil {
		t.Fatal(err)
	}
	if got := adb.ConfigFromJSON(s); !reflect.DeepEqual(got, fixed) {
		t.Fatalf("round trip = %#v, want %#v", got, fixed)
	}
	for _, raw := range []string{`not json`, `{"rules":[],"fallthrough":{},"schedule":{}}`, `{"unparseable":true}`, ``} {
		if got := adb.ConfigFromJSON(raw); !got.Unparseable {
			t.Errorf("ConfigFromJSON(%q) = %#v, want Unparseable", raw, got)
		}
	}
}

func TestRowToEnvironment(t *testing.T) {
	t.Parallel()
	got := adb.RowToEnvironment(wiredb.EnvironmentRow{ID: "e1", Key: "production", Name: "Production", Position: 3, CreatedAt: wiredb.Time{Time: utc()}})
	want := model.Environment{ID: "e1", Key: "production", Name: "Production", Position: 3, CreatedAt: utc()}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestRowToFlag(t *testing.T) {
	t.Parallel()
	row := wiredb.FlagRow{ID: "f1", Key: "k", Name: "N", Description: "D", Kind: "boolean", CreatedAt: wiredb.Time{Time: utc()}, UpdatedAt: wiredb.Time{Time: utc().Add(time.Hour)}}
	want := model.Flag{ID: "f1", Key: "k", Name: "N", Description: "D", Kind: model.KindBoolean, CreatedAt: utc(), UpdatedAt: utc().Add(time.Hour)}
	if got := adb.RowToFlag(row); !reflect.DeepEqual(got, want) {
		t.Fatalf("active: got %#v, want %#v", got, want)
	}
	row.ArchivedAt = wiredb.NullTime{Time: utc().Add(2 * time.Hour), Valid: true}
	want.ArchivedAt = ptr(utc().Add(2 * time.Hour))
	if got := adb.RowToFlag(row); !reflect.DeepEqual(got, want) {
		t.Fatalf("archived: got %#v, want %#v", got, want)
	}
}

func TestRowToEnvState(t *testing.T) {
	t.Parallel()
	row := wiredb.StateRow{EnvironmentKey: "staging", Enabled: true, Config: `{"rules":[],"fallthrough":{"value":false}}`, Version: 4, UpdatedAt: wiredb.Time{Time: utc()}, UpdatedBy: "bob"}
	want := model.EnvState{EnvironmentKey: "staging", Enabled: true, Config: model.FlagConfig{Fallthrough: model.Serve{Value: ptr(false)}}, Version: 4, UpdatedAt: utc(), UpdatedBy: "bob"}
	if got := adb.RowToEnvState(row); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestRowsToSnapshot(t *testing.T) {
	t.Parallel()
	got := adb.RowsToSnapshot("production", 7, []wiredb.SnapshotRow{
		{Key: "a", Enabled: true, Version: 2, Config: `{"rules":[],"fallthrough":{}}`},
		{Key: "b", Enabled: false, Version: 1, Config: `{"rules":[],"fallthrough":{"variation":"x"}}`},
	})
	want := model.Snapshot{Environment: "production", Revision: 7, Flags: []model.SnapshotFlag{
		{Key: "a", Enabled: true, Version: 2},
		{Key: "b", Enabled: false, Version: 1, Config: model.FlagConfig{Unparseable: true}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestAuditRoundTrip(t *testing.T) {
	t.Parallel()
	before, err := adb.AuditJSON(map[string]any{"enabled": false})
	if err != nil {
		t.Fatal(err)
	}
	if none, err := adb.AuditJSON(nil); none != nil || err != nil {
		t.Fatalf("AuditJSON(nil) = %v, %v; want nil, nil", none, err)
	}
	if adb.NullIfEmpty("") != nil || *adb.NullIfEmpty("x") != "x" {
		t.Fatal("NullIfEmpty")
	}
	row := wiredb.AuditRow{ID: "a1", EnvironmentKey: ptr("production"), FlagKey: ptr("k"), ActorID: "u1", ActorName: "Alice",
		Action: "FLAG_STATE_CHANGED", Before: before, After: ptr(`{"enabled":true,"weight":2}`), Reason: ptr("rollout"), CreatedAt: wiredb.Time{Time: utc()}}
	want := model.AuditEntry{ID: "a1", Project: "default", EnvironmentKey: "production", FlagKey: "k", ActorID: "u1", ActorName: "Alice",
		Action: model.AuditFlagStateChanged, Before: map[string]any{"enabled": false}, After: map[string]any{"enabled": true, "weight": float64(2)}, Reason: "rollout", CreatedAt: utc()}
	got, err := adb.RowToAudit("default", row)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, %v; want %#v", got, err, want)
	}
	bare, err := adb.RowToAudit("default", wiredb.AuditRow{ID: "a2", ActorID: "u", ActorName: "U", Action: "AUDIT_PRUNED", CreatedAt: wiredb.Time{Time: utc()}})
	if err != nil || bare.Before != nil || bare.After != nil || bare.FlagKey != "" || bare.Reason != "" {
		t.Fatalf("bare audit row = %#v, %v", bare, err)
	}
	if _, err := adb.RowToAudit("default", wiredb.AuditRow{Before: ptr(`{broken`)}); err == nil {
		t.Fatal("corrupt audit JSON must be an error")
	}
}
