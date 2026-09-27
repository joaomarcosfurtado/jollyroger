package model

import "time"

// Kind is the value type of a flag. v0.1 has boolean flags only.
type Kind string

// KindBoolean is an on/off flag.
const KindBoolean Kind = "boolean"

// DefaultProject is the key of the project every installation starts with.
const DefaultProject = "default"

// TimePrecision is the precision every store keeps for timestamps (PostgreSQL's TIMESTAMPTZ).
const TimePrecision = time.Microsecond

// Environment is a deployment stage (development, staging, production, ...) of a project.
type Environment struct {
	ID        string
	Key       string
	Name      string
	Position  int
	CreatedAt time.Time
}

// Flag is a feature flag's identity and descriptive metadata, shared by all environments.
type Flag struct {
	ID          string
	Key         string
	Name        string
	Description string
	Kind        Kind
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ArchivedAt  *time.Time // nil while the flag is active
}

// EnvState is a flag's state in one environment.
type EnvState struct {
	EnvironmentKey string
	Enabled        bool
	Config         FlagConfig
	Version        int64 // starts at 1 and increases by 1 on every change
	UpdatedAt      time.Time
	UpdatedBy      string
}

// FlagWithStates is a flag with its state in every environment, ordered by environment position.
type FlagWithStates struct {
	Flag   Flag
	States []EnvState
}

// FlagView is a flag with its state in one environment, as listed by FlagStore.ListFlags.
type FlagView struct {
	Flag  Flag
	State EnvState
}

// NewFlag is the input of FlagTx.CreateFlag. The caller generates the ID.
type NewFlag struct {
	ID          string
	Project     string
	Key         string
	Name        string
	Description string
	Kind        Kind
	At          time.Time // creation time
	Actor       string    // recorded as UpdatedBy of the initial states
}

// FlagMeta is the input of FlagTx.UpdateFlagMeta.
type FlagMeta struct {
	Name        string
	Description string
	At          time.Time
}

// EnvStateChange is the input of FlagTx.SetEnvState.
type EnvStateChange struct {
	Enabled bool
	Config  FlagConfig
	At      time.Time
	Actor   string
}
