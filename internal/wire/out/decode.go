package out

import (
	"encoding/json"
	"errors"
	"fmt"
)

// The snapshot is a protocol document read by every SDK, so decoding follows
// protocol/evaluation-spec.md rather than encoding/json defaults:
//
//   - field names match exactly (encoding/json alone would match "ENABLED" to "enabled", which no
//     other language does);
//   - null means absent;
//   - unknown fields are ignored at the document and flag level (compatible additions), but any
//     unknown field inside a config means "a feature this reader does not implement";
//   - a config that cannot be decoded marks only its own flag invalid, so one flag from a newer
//     writer never takes the rest of the document down.

// UnmarshalJSON decodes a snapshot document. Unknown fields are ignored.
func (s *Snapshot) UnmarshalJSON(data []byte) error {
	var v Snapshot
	err := decodeObject(data, false, map[string]any{
		"schema_version": &v.SchemaVersion,
		"environment":    &v.Environment,
		"revision":       &v.Revision,
		"flags":          &v.Flags,
	})
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	*s = v
	return nil
}

// UnmarshalJSON decodes one flag. Unknown fields are ignored; a config that cannot be decoded is
// kept as an invalid config instead of failing the document.
func (f *SnapshotFlag) UnmarshalJSON(data []byte) error {
	var v SnapshotFlag
	err := decodeObject(data, false, map[string]any{
		"key":     &v.Key,
		"enabled": &v.Enabled,
		"version": &v.Version,
		"config":  &v.Config,
	})
	if err != nil {
		return fmt.Errorf("flag: %w", err)
	}
	*f = v
	return nil
}

// MarshalJSON encodes a config; an invalid config is written as the reserved {"unparseable":true},
// which every strict reader decodes as invalid again.
func (c FlagConfig) MarshalJSON() ([]byte, error) {
	if c.Invalid {
		return []byte(`{"unparseable":true}`), nil
	}
	type plain FlagConfig // drops the methods, so this does not recurse
	return json.Marshal(plain(c))
}

// UnmarshalJSON decodes a config strictly. It never returns an error: a config that is not an
// object, has a wrongly typed field, or has any unknown field decodes as FlagConfig{Invalid: true}.
func (c *FlagConfig) UnmarshalJSON(data []byte) error {
	*c = decodeConfig(data)
	return nil
}

// decodeConfig converts a config that cannot be decoded into FlagConfig{Invalid: true}. This is
// the protocol's per-flag isolation (evaluation-spec.md section 2, rule 4), not a hidden error:
// the invalid state reaches the engine and evaluates as PARSE_ERROR.
func decodeConfig(data []byte) FlagConfig {
	var v FlagConfig
	if decodeObject(data, true, map[string]any{"rules": &v.Rules, "fallthrough": &v.Fallthrough}) != nil {
		return FlagConfig{Invalid: true}
	}
	return v
}

// UnmarshalJSON decodes a rule strictly.
func (r *Rule) UnmarshalJSON(data []byte) error {
	var v Rule
	if err := decodeObject(data, true, map[string]any{"conditions": &v.Conditions, "serve": &v.Serve}); err != nil {
		return err
	}
	*r = v
	return nil
}

// UnmarshalJSON decodes a condition strictly.
func (c *Condition) UnmarshalJSON(data []byte) error {
	var v Condition
	if err := decodeObject(data, true, map[string]any{"attribute": &v.Attribute, "operator": &v.Operator, "values": &v.Values}); err != nil {
		return err
	}
	*c = v
	return nil
}

// UnmarshalJSON decodes a serve strictly.
func (s *Serve) UnmarshalJSON(data []byte) error {
	var v Serve
	if err := decodeObject(data, true, map[string]any{"value": &v.Value, "split": &v.Split}); err != nil {
		return err
	}
	*s = v
	return nil
}

// UnmarshalJSON decodes a split strictly.
func (s *Split) UnmarshalJSON(data []byte) error {
	var v Split
	if err := decodeObject(data, true, map[string]any{"variations": &v.Variations, "bucket_by": &v.BucketBy, "salt": &v.Salt}); err != nil {
		return err
	}
	*s = v
	return nil
}

// UnmarshalJSON decodes a weighted variation strictly.
func (w *WeightedVariation) UnmarshalJSON(data []byte) error {
	var v WeightedVariation
	if err := decodeObject(data, true, map[string]any{"value": &v.Value, "weight": &v.Weight}); err != nil {
		return err
	}
	*w = v
	return nil
}

// errNotObject reports a JSON value that should have been an object.
var errNotObject = errors.New("expected a JSON object")

// decodeObject decodes the JSON object in data into targets, matching field names exactly. JSON
// null is treated as an empty object, and a null member leaves its target at the zero value. With
// strict set, a member with no target is an error; otherwise it is ignored.
func decodeObject(data []byte, strict bool, targets map[string]any) error {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return errNotObject
	}
	for name, raw := range members {
		target, known := targets[name]
		if !known {
			if strict {
				return fmt.Errorf("unknown field %q", name)
			}
			continue
		}
		if err := json.Unmarshal(raw, target); err != nil {
			return fmt.Errorf("field %q: %w", name, err)
		}
	}
	return nil
}
