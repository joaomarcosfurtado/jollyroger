package api_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/joaomarcosfurtado/jollyroger/internal/adapter/api"
	"github.com/joaomarcosfurtado/jollyroger/internal/model"
	"github.com/joaomarcosfurtado/jollyroger/internal/wire/out"
)

func boolPtr(b bool) *bool { return &b }

func fullModel() model.Snapshot {
	return model.Snapshot{
		Environment: "production",
		Revision:    9,
		Flags: []model.SnapshotFlag{
			{Key: "plain", Enabled: true, Version: 1},
			{Key: "rich", Enabled: false, Version: 4, Config: model.FlagConfig{
				Rules: []model.Rule{{
					Conditions: []model.Condition{{Attribute: "country", Operator: "in", Values: []string{"BR", "PT"}}},
					Serve:      model.Serve{Value: boolPtr(true)},
				}},
				Fallthrough: model.Serve{Split: &model.Split{
					Variations: []model.WeightedVariation{{Value: true, Weight: 25000}, {Value: false, Weight: 75000}},
					BucketBy:   "user_id",
					Salt:       "s1",
				}},
			}},
		},
	}
}

func fullWire() out.Snapshot {
	return out.Snapshot{
		SchemaVersion: out.SnapshotSchemaVersion,
		Environment:   "production",
		Revision:      9,
		Flags: []out.SnapshotFlag{
			{Key: "plain", Enabled: true, Version: 1, Config: out.FlagConfig{Rules: []out.Rule{}}},
			{Key: "rich", Enabled: false, Version: 4, Config: out.FlagConfig{
				Rules: []out.Rule{{
					Conditions: []out.Condition{{Attribute: "country", Operator: "in", Values: []string{"BR", "PT"}}},
					Serve:      out.Serve{Value: boolPtr(true)},
				}},
				Fallthrough: out.Serve{Split: &out.Split{
					Variations: []out.WeightedVariation{{Value: true, Weight: 25000}, {Value: false, Weight: 75000}},
					BucketBy:   "user_id",
					Salt:       "s1",
				}},
			}},
		},
	}
}

func TestSnapshotToWire_MapsEveryField(t *testing.T) {
	t.Parallel()
	if got, want := api.SnapshotToWire(fullModel()), fullWire(); !reflect.DeepEqual(got, want) {
		t.Fatalf("SnapshotToWire mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSnapshotFromWire_MapsEveryField(t *testing.T) {
	t.Parallel()
	got, err := api.SnapshotFromWire(fullWire())
	if err != nil {
		t.Fatal(err)
	}
	if want := fullModel(); !reflect.DeepEqual(got, want) {
		t.Fatalf("SnapshotFromWire mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSnapshotToWire_EmitsEmptyArraysNotNull(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(api.SnapshotToWire(model.Snapshot{Environment: "e", Flags: []model.SnapshotFlag{{Key: "k"}}}))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"rules":[]`, `"schema_version":1`, `"fallthrough":{}`} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON %s does not contain %s", s, want)
		}
	}
	empty, err := json.Marshal(api.SnapshotToWire(model.Snapshot{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(empty), `"flags":[]`) {
		t.Errorf("empty snapshot JSON %s must contain \"flags\":[]", empty)
	}
}

func TestSnapshotFromWire_RejectsUnknownSchemaVersion(t *testing.T) {
	t.Parallel()
	for _, v := range []int{0, 2, -1} {
		w := fullWire()
		w.SchemaVersion = v
		if _, err := api.SnapshotFromWire(w); !errors.Is(err, model.ErrInvalid) {
			t.Errorf("schema_version %d: err = %v, want ErrInvalid", v, err)
		}
	}
}

func TestSnapshotFromWire_ToleratesUnknownFields(t *testing.T) {
	t.Parallel()
	raw := `{"schema_version":1,"environment":"production","revision":3,"future_top":true,
		"flags":[{"key":"f","enabled":true,"version":2,"future_flag":"x",
		"config":{"rules":[],"fallthrough":{"value":false},"future_config":1}}]}`
	var w out.Snapshot
	if err := json.Unmarshal([]byte(raw), &w); err != nil {
		t.Fatal(err)
	}
	got, err := api.SnapshotFromWire(w)
	if err != nil {
		t.Fatal(err)
	}
	want := model.Snapshot{Environment: "production", Revision: 3, Flags: []model.SnapshotFlag{
		{Key: "f", Enabled: true, Version: 2, Config: model.FlagConfig{Fallthrough: model.Serve{Value: boolPtr(false)}}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestSnapshotAdapters_DoNotAliasPointers(t *testing.T) {
	t.Parallel()
	m := fullModel()
	w := api.SnapshotToWire(m)
	*m.Flags[1].Config.Rules[0].Serve.Value = false
	m.Flags[1].Config.Fallthrough.Split.Variations[0].Weight = 1
	if !*w.Flags[1].Config.Rules[0].Serve.Value || w.Flags[1].Config.Fallthrough.Split.Variations[0].Weight != 25000 {
		t.Fatal("SnapshotToWire output shares memory with its input")
	}
	w2 := fullWire()
	back, err := api.SnapshotFromWire(w2)
	if err != nil {
		t.Fatal(err)
	}
	*w2.Flags[1].Config.Rules[0].Serve.Value = false
	w2.Flags[1].Config.Rules[0].Conditions[0].Values[0] = "XX"
	if !*back.Flags[1].Config.Rules[0].Serve.Value || back.Flags[1].Config.Rules[0].Conditions[0].Values[0] != "BR" {
		t.Fatal("SnapshotFromWire output shares memory with its input")
	}
}
