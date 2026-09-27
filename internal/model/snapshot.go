package model

// Snapshot is the raw evaluation data for one environment at one revision, as loaded from a store
// or decoded from the snapshot protocol. Archived flags are never part of a snapshot.
type Snapshot struct {
	Environment string
	Revision    int64
	Flags       []SnapshotFlag
}

// SnapshotFlag is one flag's state in the snapshot's environment.
type SnapshotFlag struct {
	Key     string
	Enabled bool
	Version int64
	Config  FlagConfig
}
