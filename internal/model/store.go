package model

import (
	"context"
	"time"
)

// List limits for FlagQuery and AuditQuery.
const (
	DefaultListLimit = 50
	MaxListLimit     = 1000
)

func effectiveLimit(n int) int {
	switch {
	case n <= 0:
		return DefaultListLimit
	case n > MaxListLimit:
		return MaxListLimit
	default:
		return n
	}
}

// FlagQuery selects a page of flags, ordered by key (byte order).
type FlagQuery struct {
	Project         string
	Environment     string // the environment whose state each FlagView carries
	AfterKey        string // keyset position: only keys that sort after this one
	Limit           int    // see EffectiveLimit
	IncludeArchived bool
}

// EffectiveLimit is Limit bounded to [1, MaxListLimit], with DefaultListLimit for 0 or less.
func (q FlagQuery) EffectiveLimit() int { return effectiveLimit(q.Limit) }

// FlagPage is one page of flags. HasMore reports whether a next page exists.
type FlagPage struct {
	Items   []FlagView
	HasMore bool
}

// AuditQuery selects a page of audit entries, newest first.
type AuditQuery struct {
	Project        string
	FlagKey        string // optional filter
	EnvironmentKey string // optional filter
	BeforeID       string // keyset position: only entries older than this ID
	Limit          int    // see EffectiveLimit
}

// EffectiveLimit is Limit bounded to [1, MaxListLimit], with DefaultListLimit for 0 or less.
func (q AuditQuery) EffectiveLimit() int { return effectiveLimit(q.Limit) }

// AuditPage is one page of audit entries. HasMore reports whether a next page exists.
type AuditPage struct {
	Items   []AuditEntry
	HasMore bool
}

// FlagStore is the storage port. Every implementation passes internal/storetest.
//
// Contract shared by all methods: an unknown project or environment is ErrNotFound; timestamps
// come back in UTC with TimePrecision; returned values never alias the store's memory.
type FlagStore interface {
	// LoadSnapshot returns the active (non-archived) flags of one environment, ordered by key,
	// with the revision they were read at, from one consistent read.
	LoadSnapshot(ctx context.Context, project, environment string) (Snapshot, error)
	// Revision returns the project's revision, which every write transaction bumps.
	Revision(ctx context.Context, project string) (int64, error)
	// ListEnvironments returns the project's environments ordered by position.
	ListEnvironments(ctx context.Context, project string) ([]Environment, error)
	// GetFlag returns a flag (archived or not) with its states; ErrNotFound if absent.
	GetFlag(ctx context.Context, project, key string) (FlagWithStates, error)
	// ListFlags returns a page of flags with their state in q.Environment.
	ListFlags(ctx context.Context, q FlagQuery) (FlagPage, error)
	// ListAudit returns a page of audit entries, newest first.
	ListAudit(ctx context.Context, q AuditQuery) (AuditPage, error)
	// InTx runs fn in one transaction: its writes commit if fn returns nil and are discarded
	// otherwise, and fn's error is returned unchanged.
	InTx(ctx context.Context, fn func(FlagTx) error) error
}

// FlagTx is the write side of FlagStore, valid only inside InTx.
type FlagTx interface {
	// GetFlag reads a flag inside the transaction (it sees the transaction's own writes).
	GetFlag(ctx context.Context, project, key string) (FlagWithStates, error)
	// CreateFlag inserts a flag and a disabled state (version 1, empty config) in every
	// environment of the project. ErrAlreadyExists if the key is taken, archived flags included.
	CreateFlag(ctx context.Context, f NewFlag) (FlagWithStates, error)
	// UpdateFlagMeta replaces the name and description.
	UpdateFlagMeta(ctx context.Context, project, key string, m FlagMeta) (Flag, error)
	// ArchiveFlag marks the flag archived at at; archiving an archived flag changes nothing.
	ArchiveFlag(ctx context.Context, project, key string, at time.Time) (Flag, error)
	// RestoreFlag clears the archive mark; restoring an active flag changes nothing.
	RestoreFlag(ctx context.Context, project, key string, at time.Time) (Flag, error)
	// SetEnvState replaces the state in one environment if its version is expectedVersion
	// (ErrConflict otherwise) and returns the new state, whose version is expectedVersion+1.
	SetEnvState(ctx context.Context, project, key, environment string, c EnvStateChange, expectedVersion int64) (EnvState, error)
	// AppendAudit records an audit entry. There is no way to change or delete one.
	AppendAudit(ctx context.Context, e AuditEntry) error
	// BumpRevision increments and returns the project's revision.
	BumpRevision(ctx context.Context, project string) (int64, error)
}
