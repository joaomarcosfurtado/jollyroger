package out_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/joaomarcosfurtado/jollyroger/internal/wire/out"
)

func decode(t *testing.T, raw string) (out.Snapshot, error) {
	t.Helper()
	var s out.Snapshot
	err := json.Unmarshal([]byte(raw), &s)
	return s, err
}

func mustDecode(t *testing.T, raw string) out.Snapshot {
	t.Helper()
	s, err := decode(t, raw)
	if err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return s
}

// doc wraps flag JSON objects into a schema-1 snapshot document.
func doc(flags string) string {
	return `{"schema_version":1,"environment":"production","revision":1,"flags":[` + flags + `]}`
}

func TestDecode_ConfigErrorIsIsolatedToItsFlag(t *testing.T) {
	t.Parallel()
	s := mustDecode(t, doc(`
		{"key":"a","enabled":true,"version":1,"config":{"rules":[],"fallthrough":{"value":"blue"}}},
		{"key":"b","enabled":true,"version":2,"config":{"rules":[],"fallthrough":{"value":false}}}`))
	if len(s.Flags) != 2 {
		t.Fatalf("want 2 flags, got %d", len(s.Flags))
	}
	if !s.Flags[0].Config.Invalid {
		t.Error("flag a has a mistyped config and must be marked invalid")
	}
	if s.Flags[1].Config.Invalid || s.Flags[1].Config.Fallthrough.Value == nil || *s.Flags[1].Config.Fallthrough.Value {
		t.Errorf("flag b must decode normally, got %+v", s.Flags[1].Config)
	}
}

func TestDecode_UnknownFieldsInsideConfigMarkItInvalid(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"config":    `{"rules":[],"fallthrough":{},"future":1}`,
		"serve":     `{"rules":[],"fallthrough":{"variation":"blue"}}`,
		"rule":      `{"rules":[{"conditions":[],"serve":{},"priority":1}],"fallthrough":{}}`,
		"condition": `{"rules":[{"conditions":[{"attribute":"a","operator":"in","values":[],"negate":true}],"serve":{}}],"fallthrough":{}}`,
		"split":     `{"rules":[],"fallthrough":{"split":{"variations":[],"bucket_by":"user_id","salt":"","seed":1}}}`,
		"variation": `{"rules":[],"fallthrough":{"split":{"variations":[{"value":true,"weight":1,"name":"x"}],"bucket_by":"user_id","salt":""}}}`,
		"not an object": `5`,
		"reserved unparseable marker": `{"unparseable":true}`,
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := mustDecode(t, doc(`{"key":"f","enabled":true,"version":1,"config":`+config+`}`))
			if !s.Flags[0].Config.Invalid {
				t.Fatalf("config %s must be marked invalid", config)
			}
		})
	}
}

func TestDecode_FieldNamesAreCaseSensitive(t *testing.T) {
	t.Parallel()
	s := mustDecode(t, doc(`{"key":"a","enabled":false,"ENABLED":true,"version":1,"config":{"rules":[],"fallthrough":{}}}`))
	if s.Flags[0].Enabled {
		t.Fatal(`"ENABLED" is an unknown field, not "enabled"`)
	}
	s = mustDecode(t, doc(`{"key":"a","enabled":true,"version":1,"config":{"rules":[],"Fallthrough":{"value":false}}}`))
	if !s.Flags[0].Config.Invalid {
		t.Fatal(`"Fallthrough" is an unknown config field and must mark the config invalid`)
	}
}

func TestDecode_NullMeansAbsent(t *testing.T) {
	t.Parallel()
	cases := []string{
		`{"key":"f","enabled":true,"version":1,"config":null}`,
		`{"key":"f","enabled":true,"version":1}`,
		`{"key":"f","enabled":true,"version":1,"config":{"rules":null,"fallthrough":null}}`,
		`{"key":"f","enabled":true,"version":1,"config":{"rules":[],"fallthrough":{"value":null,"split":null}}}`,
	}
	for _, flag := range cases {
		s := mustDecode(t, doc(flag))
		if got := s.Flags[0].Config; got.Invalid || got.Fallthrough.Value != nil || got.Fallthrough.Split != nil || len(got.Rules) != 0 {
			t.Errorf("%s: want an empty valid config, got %+v", flag, got)
		}
	}
}

func TestDecode_UnknownDocumentAndFlagFieldsAreTolerated(t *testing.T) {
	t.Parallel()
	s := mustDecode(t, `{"schema_version":1,"environment":"e","revision":3,"future_top":{"x":1},
		"flags":[{"key":"f","enabled":true,"version":2,"future_flag":[1,2],"config":{"rules":[],"fallthrough":{}}}]}`)
	if s.SchemaVersion != 1 || s.Environment != "e" || s.Revision != 3 || s.Flags[0].Key != "f" || s.Flags[0].Config.Invalid {
		t.Fatalf("unexpected decode: %+v", s)
	}
}

func TestDecode_CoreFieldTypeErrorsRejectTheDocument(t *testing.T) {
	t.Parallel()
	cases := []string{
		`{"schema_version":1.0,"environment":"e","revision":1,"flags":[]}`,
		`{"schema_version":"1","environment":"e","revision":1,"flags":[]}`,
		`{"schema_version":1,"environment":"e","revision":"1","flags":[]}`,
		`{"schema_version":1,"environment":"e","revision":1,"flags":{}}`,
		doc(`{"key":"f","enabled":"yes","version":1}`),
		doc(`{"key":7,"enabled":true,"version":1}`),
		doc(`{"key":"f","enabled":true,"version":1.5}`),
		`[]`,
	}
	for _, raw := range cases {
		if _, err := decode(t, raw); err == nil {
			t.Errorf("%s: expected a decode error", raw)
		}
	}
}

func TestEncode_InvalidConfigRoundTripsAsInvalid(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(out.FlagConfig{Invalid: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"unparseable":true}` {
		t.Fatalf("invalid config encodes as %s, want {\"unparseable\":true}", b)
	}
	var back out.FlagConfig
	if err := json.Unmarshal(b, &back); err != nil || !back.Invalid {
		t.Fatalf("decoding %s must give an invalid config, got %+v (err %v)", b, back, err)
	}
}

func TestEncodeDecode_RoundTripsAFullDocument(t *testing.T) {
	t.Parallel()
	yes := true
	want := out.Snapshot{SchemaVersion: 1, Environment: "production", Revision: 9, Flags: []out.SnapshotFlag{
		{Key: "plain", Enabled: true, Version: 1, Config: out.FlagConfig{Rules: []out.Rule{}}},
		{Key: "rich", Version: 4, Config: out.FlagConfig{
			Rules: []out.Rule{{
				Conditions: []out.Condition{{Attribute: "country", Operator: "in", Values: []string{"BR", "PT"}}},
				Serve:      out.Serve{Value: &yes},
			}},
			Fallthrough: out.Serve{Split: &out.Split{
				Variations: []out.WeightedVariation{{Value: true, Weight: 25000}, {Value: false, Weight: 75000}},
				BucketBy:   "user_id",
				Salt:       "s1",
			}},
		}},
	}}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got := mustDecode(t, string(b))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch\n got: %#v\nwant: %#v", got, want)
	}
}
