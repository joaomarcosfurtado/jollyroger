// Package api translates between the JSON API / snapshot protocol shapes (internal/wire) and the
// domain model. It is the only place those shapes are mapped, and it is pure.
package api

import (
	"fmt"

	"github.com/joaomarcosfurtado/jollyroger/internal/model"
	"github.com/joaomarcosfurtado/jollyroger/internal/wire/out"
)

// SnapshotToWire converts a model snapshot to the protocol document. Collections are always
// non-nil so the JSON carries [] rather than null.
func SnapshotToWire(s model.Snapshot) out.Snapshot {
	flags := make([]out.SnapshotFlag, 0, len(s.Flags))
	for _, f := range s.Flags {
		flags = append(flags, out.SnapshotFlag{
			Key:     f.Key,
			Enabled: f.Enabled,
			Version: f.Version,
			Config:  configToWire(f.Config),
		})
	}
	return out.Snapshot{
		SchemaVersion: out.SnapshotSchemaVersion,
		Environment:   s.Environment,
		Revision:      s.Revision,
		Flags:         flags,
	}
}

// SnapshotFromWire converts a protocol document to a model snapshot. It rejects a document whose
// schema_version this build does not understand; unknown fields are ignored by the JSON decoder,
// so additive changes from newer writers are tolerated. Empty collections become nil.
func SnapshotFromWire(w out.Snapshot) (model.Snapshot, error) {
	if w.SchemaVersion != out.SnapshotSchemaVersion {
		return model.Snapshot{}, fmt.Errorf("snapshot schema_version %d (supported: %d): %w",
			w.SchemaVersion, out.SnapshotSchemaVersion, model.ErrInvalid)
	}
	var flags []model.SnapshotFlag
	for _, f := range w.Flags {
		flags = append(flags, model.SnapshotFlag{
			Key:     f.Key,
			Enabled: f.Enabled,
			Version: f.Version,
			Config:  configFromWire(f.Config),
		})
	}
	return model.Snapshot{Environment: w.Environment, Revision: w.Revision, Flags: flags}, nil
}

func configToWire(c model.FlagConfig) out.FlagConfig {
	if c.Unparseable {
		return out.FlagConfig{Invalid: true}
	}
	rules := make([]out.Rule, 0, len(c.Rules))
	for _, r := range c.Rules {
		conds := make([]out.Condition, 0, len(r.Conditions))
		for _, cd := range r.Conditions {
			conds = append(conds, out.Condition{Attribute: cd.Attribute, Operator: cd.Operator, Values: append([]string{}, cd.Values...)})
		}
		rules = append(rules, out.Rule{Conditions: conds, Serve: serveToWire(r.Serve)})
	}
	return out.FlagConfig{Rules: rules, Fallthrough: serveToWire(c.Fallthrough)}
}

func serveToWire(s model.Serve) out.Serve {
	var w out.Serve
	if s.Value != nil {
		v := *s.Value
		w.Value = &v
	}
	if s.Split != nil {
		vars := make([]out.WeightedVariation, 0, len(s.Split.Variations))
		for _, v := range s.Split.Variations {
			vars = append(vars, out.WeightedVariation{Value: v.Value, Weight: v.Weight})
		}
		w.Split = &out.Split{Variations: vars, BucketBy: s.Split.BucketBy, Salt: s.Split.Salt}
	}
	return w
}

func configFromWire(w out.FlagConfig) model.FlagConfig {
	if w.Invalid {
		return model.FlagConfig{Unparseable: true}
	}
	var rules []model.Rule
	for _, r := range w.Rules {
		var conds []model.Condition
		for _, cd := range r.Conditions {
			var values []string
			if len(cd.Values) > 0 {
				values = append([]string{}, cd.Values...)
			}
			conds = append(conds, model.Condition{Attribute: cd.Attribute, Operator: cd.Operator, Values: values})
		}
		rules = append(rules, model.Rule{Conditions: conds, Serve: serveFromWire(r.Serve)})
	}
	return model.FlagConfig{Rules: rules, Fallthrough: serveFromWire(w.Fallthrough)}
}

func serveFromWire(w out.Serve) model.Serve {
	var s model.Serve
	if w.Value != nil {
		v := *w.Value
		s.Value = &v
	}
	if w.Split != nil {
		var vars []model.WeightedVariation
		for _, v := range w.Split.Variations {
			vars = append(vars, model.WeightedVariation{Value: v.Value, Weight: v.Weight})
		}
		s.Split = &model.Split{Variations: vars, BucketBy: w.Split.BucketBy, Salt: w.Split.Salt}
	}
	return s
}
