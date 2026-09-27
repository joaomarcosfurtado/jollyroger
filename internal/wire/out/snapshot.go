// Package out holds the shapes jollyroger sends across its boundary: JSON API responses and the
// snapshot protocol. Field names are snake_case and form a public contract (protocol/). Shapes are
// asserted in adapter tests, never validated on the hot path.
package out

// SnapshotSchemaVersion is the snapshot protocol version this build reads and writes.
const SnapshotSchemaVersion = 1

// Snapshot is the local-evaluation protocol document for one environment.
type Snapshot struct {
	SchemaVersion int            `json:"schema_version"`
	Environment   string         `json:"environment"`
	Revision      int64          `json:"revision"`
	Flags         []SnapshotFlag `json:"flags"`
}

// SnapshotFlag is one flag in a Snapshot.
type SnapshotFlag struct {
	Key     string     `json:"key"`
	Enabled bool       `json:"enabled"`
	Version int64      `json:"version"`
	Config  FlagConfig `json:"config"`
}

// FlagConfig is a flag's evaluation configuration.
type FlagConfig struct {
	Rules       []Rule `json:"rules"`
	Fallthrough Serve  `json:"fallthrough"`
}

// Rule serves Serve when every condition matches.
type Rule struct {
	Conditions []Condition `json:"conditions"`
	Serve      Serve       `json:"serve"`
}

// Condition compares one context attribute with values.
type Condition struct {
	Attribute string   `json:"attribute"`
	Operator  string   `json:"operator"`
	Values    []string `json:"values"`
}

// Serve is a fixed value or a split; an empty object means "serve true".
type Serve struct {
	Value *bool  `json:"value,omitempty"`
	Split *Split `json:"split,omitempty"`
}

// Split assigns contexts to variations by deterministic bucketing.
type Split struct {
	Variations []WeightedVariation `json:"variations"`
	BucketBy   string              `json:"bucket_by"`
	Salt       string              `json:"salt"`
}

// WeightedVariation is one outcome of a Split, weight in units of 0.001%.
type WeightedVariation struct {
	Value  bool `json:"value"`
	Weight int  `json:"weight"`
}
