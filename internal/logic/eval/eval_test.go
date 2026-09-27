package eval

import (
	"sync"
	"testing"

	"github.com/joaomarcosfurtado/jollyroger/internal/model"
)

func boolPtr(b bool) *bool { return &b }

func TestEvaluate(t *testing.T) {
	t.Parallel()
	snap := Compile(model.Snapshot{
		Environment: "production",
		Revision:    7,
		Flags: []model.SnapshotFlag{
			{Key: "default-true", Enabled: true, Version: 3},
			{Key: "fixed-false", Enabled: true, Version: 1, Config: model.FlagConfig{Fallthrough: model.Serve{Value: boolPtr(false)}}},
			{Key: "fixed-true", Enabled: true, Version: 2, Config: model.FlagConfig{Fallthrough: model.Serve{Value: boolPtr(true)}}},
			{Key: "off", Enabled: false, Version: 5},
			{Key: "rules-unsupported", Enabled: true, Version: 4, Config: model.FlagConfig{Rules: []model.Rule{{Serve: model.Serve{Value: boolPtr(true)}}}}},
			{Key: "split-unsupported", Enabled: true, Version: 6, Config: model.FlagConfig{Fallthrough: model.Serve{Split: &model.Split{}}}},
			{Key: "off-with-bad-config", Enabled: false, Version: 8, Config: model.FlagConfig{Fallthrough: model.Serve{Split: &model.Split{}}}},
			{Key: "unparseable", Enabled: true, Version: 9, Config: model.FlagConfig{Unparseable: true}},
			{Key: "off-unparseable", Enabled: false, Version: 10, Config: model.FlagConfig{Unparseable: true}},
			{Key: "dup", Enabled: true, Version: 1},
			{Key: "dup", Enabled: false, Version: 2},
		},
	})
	cases := []struct {
		name string
		key  string
		want model.Result
	}{
		{"enabled with empty config serves true", "default-true", model.Result{Value: true, Reason: model.ReasonStatic, FlagVersion: 3}},
		{"fixed fallthrough false", "fixed-false", model.Result{Value: false, Reason: model.ReasonStatic, FlagVersion: 1}},
		{"fixed fallthrough true", "fixed-true", model.Result{Value: true, Reason: model.ReasonStatic, FlagVersion: 2}},
		{"disabled", "off", model.Result{Value: false, Reason: model.ReasonDisabled, FlagVersion: 5}},
		{"missing", "nope", model.Result{Value: false, Reason: model.ReasonError, ErrorCode: model.ErrorCodeFlagNotFound}},
		{"keys are case-sensitive", "Default-True", model.Result{Value: false, Reason: model.ReasonError, ErrorCode: model.ErrorCodeFlagNotFound}},
		{"rules unsupported", "rules-unsupported", model.Result{Value: false, Reason: model.ReasonError, ErrorCode: model.ErrorCodeParseError, FlagVersion: 4}},
		{"split unsupported", "split-unsupported", model.Result{Value: false, Reason: model.ReasonError, ErrorCode: model.ErrorCodeParseError, FlagVersion: 6}},
		{"disabled wins over a bad config", "off-with-bad-config", model.Result{Value: false, Reason: model.ReasonDisabled, FlagVersion: 8}},
		{"unparseable config", "unparseable", model.Result{Value: false, Reason: model.ReasonError, ErrorCode: model.ErrorCodeParseError, FlagVersion: 9}},
		{"disabled wins over an unparseable config", "off-unparseable", model.Result{Value: false, Reason: model.ReasonDisabled, FlagVersion: 10}},
		{"duplicate key", "dup", model.Result{Value: false, Reason: model.ReasonError, ErrorCode: model.ErrorCodeParseError}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := snap.Evaluate(tc.key, model.Context{}); got != tc.want {
				t.Fatalf("Evaluate(%q) = %+v, want %+v", tc.key, got, tc.want)
			}
		})
	}
}

func TestEvaluate_NilSnapshot(t *testing.T) {
	t.Parallel()
	var snap *Snapshot
	want := model.Result{Value: false, Reason: model.ReasonError, ErrorCode: model.ErrorCodeGeneral}
	if got := snap.Evaluate("anything", model.Context{}); got != want {
		t.Fatalf("nil snapshot Evaluate = %+v, want %+v", got, want)
	}
	if snap.Len() != 0 || snap.Revision() != 0 || snap.Environment() != "" {
		t.Fatal("nil snapshot accessors must return zero values")
	}
}

func TestEvaluate_StaticFlagsIgnoreContext(t *testing.T) {
	t.Parallel()
	snap := Compile(model.Snapshot{Flags: []model.SnapshotFlag{{Key: "f", Enabled: true, Version: 1}}})
	a := snap.Evaluate("f", model.Context{})
	b := snap.Evaluate("f", model.Context{UserID: "123", Attributes: map[string]any{"plan": "premium"}})
	if a != b {
		t.Fatalf("static flag depended on context: %+v vs %+v", a, b)
	}
}

func TestCompile_DoesNotAliasInput(t *testing.T) {
	t.Parallel()
	value := true
	in := model.Snapshot{Flags: []model.SnapshotFlag{{Key: "f", Enabled: true, Config: model.FlagConfig{Fallthrough: model.Serve{Value: &value}}}}}
	snap := Compile(in)
	value = false
	in.Flags[0].Enabled = false
	if got := snap.Evaluate("f", model.Context{}); !got.Value {
		t.Fatalf("mutating the input after Compile changed the snapshot: %+v", got)
	}
}

func TestSnapshot_Accessors(t *testing.T) {
	t.Parallel()
	snap := Compile(model.Snapshot{Environment: "staging", Revision: 42, Flags: []model.SnapshotFlag{{Key: "a"}, {Key: "b"}}})
	if snap.Environment() != "staging" || snap.Revision() != 42 || snap.Len() != 2 {
		t.Fatalf("accessors = (%q, %d, %d)", snap.Environment(), snap.Revision(), snap.Len())
	}
}

func TestEvaluate_ConcurrentReaders(t *testing.T) {
	t.Parallel()
	snap := Compile(model.Snapshot{Flags: []model.SnapshotFlag{{Key: "f", Enabled: true, Version: 1}}})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 1000 {
				if !snap.Evaluate("f", model.Context{UserID: "u"}).Value {
					t.Error("concurrent Evaluate returned false")
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestEvaluate_ZeroAllocations(t *testing.T) {
	snap := Compile(model.Snapshot{Flags: []model.SnapshotFlag{{Key: "new-checkout", Enabled: true, Version: 1}}})
	ctx := model.Context{UserID: "123", Attributes: map[string]any{"country": "BR"}}
	for name, fn := range map[string]func(){
		"found":        func() { _ = snap.Evaluate("new-checkout", model.Context{}) },
		"with context": func() { _ = snap.Evaluate("new-checkout", ctx) },
		"not found":    func() { _ = snap.Evaluate("missing", ctx) },
	} {
		if allocs := testing.AllocsPerRun(1000, fn); allocs != 0 {
			t.Errorf("%s: Evaluate allocated %.1f times per run, want 0", name, allocs)
		}
	}
}

func BenchmarkEvaluate(b *testing.B) {
	snap := Compile(model.Snapshot{Flags: []model.SnapshotFlag{{Key: "new-checkout", Enabled: true, Version: 1}}})
	ctx := model.Context{UserID: "123", Attributes: map[string]any{"country": "BR", "plan": "premium"}}
	b.Run("enabled", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = snap.Evaluate("new-checkout", model.Context{})
		}
	})
	b.Run("with context", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = snap.Evaluate("new-checkout", ctx)
		}
	})
	b.Run("not found", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = snap.Evaluate("missing", ctx)
		}
	})
}

func BenchmarkBucket(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = Bucket("new-checkout", "salt", "user-123")
	}
}
