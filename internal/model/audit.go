package model

import "time"

// AuditAction names an administrative change recorded in the audit log.
type AuditAction string

// Audit actions.
const (
	AuditFlagCreated      AuditAction = "FLAG_CREATED"
	AuditFlagUpdated      AuditAction = "FLAG_UPDATED"
	AuditFlagStateChanged AuditAction = "FLAG_STATE_CHANGED"
	AuditFlagArchived     AuditAction = "FLAG_ARCHIVED"
	AuditFlagRestored     AuditAction = "FLAG_RESTORED"
	AuditPruned           AuditAction = "AUDIT_PRUNED"
)

// AuditEntry is one immutable audit record. Before and After hold JSON-compatible values
// (strings, bools, numbers, nested maps and slices); stores keep them as JSON, so numbers read back
// as float64. IDs sort by creation (ULIDs), and the log is ordered by ID.
type AuditEntry struct {
	ID             string
	Project        string
	EnvironmentKey string // empty when the change is not environment-specific
	FlagKey        string // empty when the change is not flag-specific
	ActorID        string
	ActorName      string
	Action         AuditAction
	Before         map[string]any
	After          map[string]any
	Reason         string
	CreatedAt      time.Time
}
