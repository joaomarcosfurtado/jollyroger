package eval_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/joaomarcosfurtado/jollyroger/internal/adapter/api"
	"github.com/joaomarcosfurtado/jollyroger/internal/logic/eval"
	"github.com/joaomarcosfurtado/jollyroger/internal/model"
	"github.com/joaomarcosfurtado/jollyroger/internal/wire/out"
)

type vectorsFile struct {
	VectorsVersion  int `json:"vectors_version"`
	EvaluationCases []struct {
		Name     string       `json:"name"`
		Snapshot out.Snapshot `json:"snapshot"`
		Flag     string       `json:"flag"`
		Context  struct {
			UserID     string         `json:"user_id"`
			Attributes map[string]any `json:"attributes"`
		} `json:"context"`
		Expected struct {
			Value       bool   `json:"value"`
			Reason      string `json:"reason"`
			ErrorCode   string `json:"error_code"`
			FlagVersion int64  `json:"flag_version"`
		} `json:"expected"`
	} `json:"evaluation_cases"`
	BucketCases []struct {
		FlagKey  string `json:"flag_key"`
		Salt     string `json:"salt"`
		Value    string `json:"value"`
		Expected uint32 `json:"expected"`
	} `json:"bucket_cases"`
	DocumentCases []struct {
		Name     string          `json:"name"`
		Document json.RawMessage `json:"document"`
		Expect   string          `json:"expect"`
	} `json:"document_cases"`
}

// TestProtocolVectors runs protocol/testdata/vectors.json, the file every SDK must pass.
func TestProtocolVectors(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../protocol/testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectorsFile
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode vectors: %v", err)
	}
	if v.VectorsVersion != 1 || len(v.EvaluationCases) == 0 || len(v.BucketCases) == 0 || len(v.DocumentCases) == 0 {
		t.Fatalf("unexpected vectors file: version %d, %d evaluation cases, %d bucket cases, %d document cases",
			v.VectorsVersion, len(v.EvaluationCases), len(v.BucketCases), len(v.DocumentCases))
	}
	for _, dc := range v.DocumentCases {
		t.Run("document/"+dc.Name, func(t *testing.T) {
			var w out.Snapshot
			err := json.Unmarshal(dc.Document, &w)
			if err == nil {
				_, err = api.SnapshotFromWire(w)
			}
			switch dc.Expect {
			case "accept":
				if err != nil {
					t.Fatalf("document must be accepted, got %v", err)
				}
			case "reject":
				if err == nil {
					t.Fatal("document must be rejected")
				}
			default:
				t.Fatalf("unknown expect %q", dc.Expect)
			}
		})
	}
	for _, tc := range v.EvaluationCases {
		t.Run(tc.Name, func(t *testing.T) {
			snap, err := api.SnapshotFromWire(tc.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			got := eval.Compile(snap).Evaluate(tc.Flag, model.Context{UserID: tc.Context.UserID, Attributes: tc.Context.Attributes})
			want := model.Result{
				Value:       tc.Expected.Value,
				Reason:      model.Reason(tc.Expected.Reason),
				ErrorCode:   model.ErrorCode(tc.Expected.ErrorCode),
				FlagVersion: tc.Expected.FlagVersion,
			}
			if got != want {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		})
	}
	for _, bc := range v.BucketCases {
		if got := eval.Bucket(bc.FlagKey, bc.Salt, bc.Value); got != bc.Expected {
			t.Errorf("Bucket(%q, %q, %q) = %d, want %d", bc.FlagKey, bc.Salt, bc.Value, got, bc.Expected)
		}
	}
}
