package archtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

const mod = "example.com/jr"

func TestClassify(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path     string
		want     Layer
		inModule bool
	}{
		{mod, LayerRoot, true},
		{mod + "/cmd/jollyroger", LayerCmd, true},
		{mod + "/jollyrogertest", LayerTestHelper, true},
		{mod + "/storetest", LayerStoreTest, true},
		{mod + "/internal/archtest", LayerTooling, true},
		{mod + "/internal/model", LayerModel, true},
		{mod + "/internal/wire/in", LayerWire, true},
		{mod + "/internal/wire/db", LayerWire, true},
		{mod + "/internal/logic/eval", LayerLogic, true},
		{mod + "/internal/controller", LayerController, true},
		{mod + "/internal/adapter/http", LayerAdapter, true},
		{mod + "/internal/diplomat/postgres", LayerDiplomat, true},
		{mod + "/internal/diplomat/httpserver", LayerDiplomatEntry, true},
		{mod + "/internal/diplomat/httpserver/api", LayerDiplomatEntry, true},
		{mod + "/internal/diplomat/poller", LayerDiplomatEntry, true},
		{mod + "/internal/diplomat/pollerx", LayerDiplomat, true}, // prefix match must respect path segments
		{mod + "/internal/modelx", LayerUnclassified, true},
		{mod + "/internal/util", LayerUnclassified, true},
		{mod + "x/internal/model", "", false},
		{"net/http", "", false},
	}
	for _, tc := range cases {
		got, inModule := Classify(mod, tc.path)
		if got != tc.want || inModule != tc.inModule {
			t.Errorf("Classify(%q) = (%q, %v), want (%q, %v)", tc.path, got, inModule, tc.want, tc.inModule)
		}
	}
}

func TestCheck_AllowedGraphHasNoViolations(t *testing.T) {
	t.Parallel()
	pkgs := []Package{
		{ImportPath: mod, Imports: []string{mod + "/internal/diplomat/httpserver", mod + "/internal/controller", "log/slog", "net/http"}},
		{ImportPath: mod + "/cmd/jollyroger", Imports: []string{mod, "os"}},
		{ImportPath: mod + "/internal/model", Imports: []string{"context", "errors", "time"}},
		{ImportPath: mod + "/internal/wire/in", Imports: []string{"encoding/json", "regexp"}},
		{ImportPath: mod + "/internal/logic/eval", Imports: []string{mod + "/internal/model", "crypto/sha256"}},
		{ImportPath: mod + "/internal/controller", Imports: []string{mod + "/internal/model", mod + "/internal/logic/eval", "context", "log/slog"}},
		{ImportPath: mod + "/internal/adapter/db", Imports: []string{mod + "/internal/model", mod + "/internal/wire/db"}},
		{ImportPath: mod + "/internal/diplomat/postgres", Imports: []string{mod + "/internal/adapter/db", mod + "/internal/model", "database/sql"}},
		{ImportPath: mod + "/internal/diplomat/httpserver", Imports: []string{mod + "/internal/controller", mod + "/internal/diplomat/ui", "net/http"}},
		{ImportPath: mod + "/storetest", Imports: []string{mod + "/internal/model", "testing"}},
		{ImportPath: mod + "/internal/archtest", Imports: []string{"os/exec"}},
		{ImportPath: "github.com/other/dep", Imports: []string{"os"}}, // outside the module: ignored
	}
	if v := Check(mod, pkgs); len(v) != 0 {
		t.Fatalf("expected no violations, got:\n%s", join(v))
	}
}

func TestCheck_ReportsEachForbiddenCrossing(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		pkg  Package
		want []string // expected violating imports; "" means the package itself is misplaced
	}{
		{"controller imports wire", Package{mod + "/internal/controller", []string{mod + "/internal/wire/in"}}, []string{mod + "/internal/wire/in"}},
		{"controller imports adapter", Package{mod + "/internal/controller", []string{mod + "/internal/adapter/http"}}, []string{mod + "/internal/adapter/http"}},
		{"controller imports diplomat", Package{mod + "/internal/controller", []string{mod + "/internal/diplomat/postgres"}}, []string{mod + "/internal/diplomat/postgres"}},
		{"controller imports database/sql", Package{mod + "/internal/controller", []string{"database/sql"}}, []string{"database/sql"}},
		{"controller imports net/http", Package{mod + "/internal/controller", []string{"net/http"}}, []string{"net/http"}},
		{"logic imports controller", Package{mod + "/internal/logic/eval", []string{mod + "/internal/controller"}}, []string{mod + "/internal/controller"}},
		{"logic logs", Package{mod + "/internal/logic/eval", []string{"log/slog"}}, []string{"log/slog"}},
		{"logic reads files", Package{mod + "/internal/logic/eval", []string{"os"}}, []string{"os"}},
		{"model imports logic", Package{mod + "/internal/model", []string{mod + "/internal/logic/eval"}}, []string{mod + "/internal/logic/eval"}},
		{"wire imports model", Package{mod + "/internal/wire/out", []string{mod + "/internal/model"}}, []string{mod + "/internal/model"}},
		{"adapter imports diplomat", Package{mod + "/internal/adapter/db", []string{mod + "/internal/diplomat/postgres"}}, []string{mod + "/internal/diplomat/postgres"}},
		{"adapter imports controller", Package{mod + "/internal/adapter/http", []string{mod + "/internal/controller"}}, []string{mod + "/internal/controller"}},
		{"adapter does I/O", Package{mod + "/internal/adapter/http", []string{"net/http"}}, []string{"net/http"}},
		{"store calls controller", Package{mod + "/internal/diplomat/postgres", []string{mod + "/internal/controller"}}, []string{mod + "/internal/controller"}},
		{"store imports entry point", Package{mod + "/internal/diplomat/postgres", []string{mod + "/internal/diplomat/httpserver"}}, []string{mod + "/internal/diplomat/httpserver"}},
		{"storetest imports a store", Package{mod + "/storetest", []string{mod + "/internal/diplomat/memory"}}, []string{mod + "/internal/diplomat/memory"}},
		{"internal imports root", Package{mod + "/internal/diplomat/cache", []string{mod}}, []string{mod}},
		{"unclassified package", Package{mod + "/internal/util", nil}, []string{""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, v := range Check(mod, []Package{tc.pkg}) {
				got = append(got, v.Import)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("violating imports = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestModuleRespectsLayers is the real guard: it loads every package of this module and fails on
// any import that breaks the table in docs/skills/diplomat-architecture.md.
func TestModuleRespectsLayers(t *testing.T) {
	var mainModule struct{ Path, Dir string }
	if err := json.Unmarshal([]byte(run(t, exec.CommandContext(t.Context(), "go", "list", "-m", "-json"))), &mainModule); err != nil {
		t.Fatalf("decode go list -m output: %v", err)
	}
	if mainModule.Path == "" || mainModule.Dir == "" {
		t.Fatalf("go list -m returned an incomplete module: %+v", mainModule)
	}

	listAll := exec.CommandContext(t.Context(), "go", "list", "-json", "./...")
	listAll.Dir = mainModule.Dir
	pkgs := decodePackages(t, run(t, listAll))
	if len(pkgs) == 0 {
		t.Fatalf("go list found no packages in %s; the guard would pass vacuously", mainModule.Path)
	}
	if v := Check(mainModule.Path, pkgs); len(v) != 0 {
		t.Fatalf("layer violations (see docs/skills/diplomat-architecture.md):\n%s", join(v))
	}
}

// run executes cmd and returns its stdout, failing the test with stderr on error. Callers build
// cmd with literal arguments only.
func run(t *testing.T, cmd *exec.Cmd) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(cmd.Args, " "), err, stderr.String())
	}
	return stdout.String()
}

func decodePackages(t *testing.T, stream string) []Package {
	t.Helper()
	var pkgs []Package
	dec := json.NewDecoder(strings.NewReader(stream))
	for {
		var p Package
		err := dec.Decode(&p)
		if errors.Is(err, io.EOF) {
			return pkgs
		}
		if err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		pkgs = append(pkgs, p)
	}
}

func join(vs []Violation) string {
	lines := make([]string, len(vs))
	for i, v := range vs {
		lines[i] = "  " + v.String()
	}
	return strings.Join(lines, "\n")
}
