// Package archtest enforces the Diplomat layer import table documented in
// docs/skills/diplomat-architecture.md. The rules live here as pure functions; the test in this
// package loads the real module graph with `go list` and fails on any violation.
package archtest

import (
	"fmt"
	"slices"
	"strings"
)

// Layer names one architectural layer of the module.
type Layer string

// The layers of the module. A package that matches none of them is reported as unclassified, so
// adding a new top-level package forces a conscious update of these rules.
const (
	LayerRoot          Layer = "root"           // the public facade and composition root
	LayerCmd           Layer = "cmd"            // CLI composition roots
	LayerTestHelper    Layer = "jollyrogertest" // public test helper for adopters (composition)
	LayerStoreTest     Layer = "storetest"      // public store conformance suite
	LayerModel         Layer = "model"          // entities, sentinel errors, ports
	LayerWire          Layer = "wire"           // boundary shapes
	LayerLogic         Layer = "logic"          // pure functions
	LayerController    Layer = "controller"     // use-case orchestration
	LayerAdapter       Layer = "adapter"        // pure wire <-> model translation
	LayerDiplomat      Layer = "diplomat"       // I/O that is not an entry point
	LayerDiplomatEntry Layer = "diplomat-entry" // I/O entry points that drive controllers
	LayerTooling       Layer = "tooling"        // repository tooling such as this package
	LayerUnclassified  Layer = "unclassified"
)

// isEntryPoint reports whether a diplomat package may call controllers (sanctioned exception [4]):
// it receives a request or a timer tick and drives a use case, exactly like an HTTP handler.
func isEntryPoint(rel string) bool {
	return under(rel, "internal/diplomat/httpserver") || under(rel, "internal/diplomat/poller")
}

// allowedInternal returns the layers l may import inside the module. restricted is false for
// composition roots (root, cmd, jollyrogertest) and tooling, which may import anything.
func allowedInternal(l Layer) (allowed []Layer, restricted bool) {
	switch l {
	case LayerModel, LayerWire:
		return nil, true
	case LayerLogic:
		return []Layer{LayerLogic, LayerModel}, true
	case LayerController:
		return []Layer{LayerModel, LayerLogic}, true
	case LayerAdapter:
		return []Layer{LayerModel, LayerWire, LayerLogic}, true
	case LayerDiplomat:
		return []Layer{LayerDiplomat, LayerAdapter, LayerWire, LayerModel, LayerLogic}, true
	case LayerDiplomatEntry:
		return []Layer{LayerDiplomat, LayerDiplomatEntry, LayerAdapter, LayerWire, LayerModel, LayerLogic, LayerController}, true
	case LayerStoreTest:
		return []Layer{LayerModel, LayerLogic}, true
	default:
		return nil, false
	}
}

// forbiddenStd reports whether layer l must not import the standard-library package imp. Pure
// layers must stay free of I/O: a pure layer that can open a socket, read a file, or log is no
// longer pure. Controllers may take a *slog.Logger through their dependencies, so log/slog is
// allowed there and nowhere else in the domain.
func forbiddenStd(l Layer, imp string) bool {
	switch l {
	case LayerModel, LayerWire, LayerLogic, LayerAdapter:
		return isIOPackage(imp) || imp == "log/slog"
	case LayerController:
		return isIOPackage(imp)
	default:
		return false
	}
}

// isIOPackage reports whether imp is a standard-library package that performs I/O.
func isIOPackage(imp string) bool {
	switch imp {
	case "database/sql", "database/sql/driver",
		"io/ioutil", "log",
		"net", "net/http",
		"os", "os/exec", "os/signal",
		"syscall":
		return true
	default:
		return false
	}
}

// Package is the subset of `go list -json` output the rules need.
type Package struct {
	ImportPath string
	Imports    []string
}

// Violation is one broken rule: Package imports Import, which its layer forbids. Import is empty
// when the package itself is misplaced.
type Violation struct {
	Package string
	Import  string
	Reason  string
}

func (v Violation) String() string {
	if v.Import == "" {
		return fmt.Sprintf("%s: %s", v.Package, v.Reason)
	}
	return fmt.Sprintf("%s imports %s: %s", v.Package, v.Import, v.Reason)
}

// Classify returns the layer of importPath within module, and whether the path belongs to the
// module at all.
func Classify(module, importPath string) (Layer, bool) {
	if importPath == module {
		return LayerRoot, true
	}
	rel, ok := strings.CutPrefix(importPath, module+"/")
	if !ok {
		return "", false
	}
	switch {
	case under(rel, "cmd"):
		return LayerCmd, true
	case under(rel, "jollyrogertest"):
		return LayerTestHelper, true
	case under(rel, "storetest"):
		return LayerStoreTest, true
	case under(rel, "internal/archtest"):
		return LayerTooling, true
	case under(rel, "internal/model"):
		return LayerModel, true
	case under(rel, "internal/wire"):
		return LayerWire, true
	case under(rel, "internal/logic"):
		return LayerLogic, true
	case under(rel, "internal/controller"):
		return LayerController, true
	case under(rel, "internal/adapter"):
		return LayerAdapter, true
	case under(rel, "internal/diplomat"):
		if isEntryPoint(rel) {
			return LayerDiplomatEntry, true
		}
		return LayerDiplomat, true
	default:
		return LayerUnclassified, true
	}
}

// under reports whether rel is dir itself or a package below it.
func under(rel, dir string) bool {
	return rel == dir || strings.HasPrefix(rel, dir+"/")
}

// Check returns every violation of the layer rules in pkgs, sorted for stable output.
func Check(module string, pkgs []Package) []Violation {
	var out []Violation
	for _, p := range pkgs {
		layer, inModule := Classify(module, p.ImportPath)
		if !inModule {
			continue
		}
		if layer == LayerUnclassified {
			out = append(out, Violation{
				Package: p.ImportPath,
				Reason:  "package matches no layer; place it per docs/skills/diplomat-architecture.md or extend internal/archtest",
			})
			continue
		}
		allowed, restricted := allowedInternal(layer)
		for _, imp := range p.Imports {
			if impLayer, ok := Classify(module, imp); ok {
				if restricted && !slices.Contains(allowed, impLayer) {
					out = append(out, Violation{
						Package: p.ImportPath,
						Import:  imp,
						Reason:  fmt.Sprintf("layer %q must not import layer %q", layer, impLayer),
					})
				}
				continue
			}
			if forbiddenStd(layer, imp) {
				out = append(out, Violation{
					Package: p.ImportPath,
					Import:  imp,
					Reason:  fmt.Sprintf("layer %q must stay free of I/O and must not import %q", layer, imp),
				})
			}
		}
	}
	slices.SortFunc(out, func(a, b Violation) int { return strings.Compare(a.String(), b.String()) })
	return out
}
