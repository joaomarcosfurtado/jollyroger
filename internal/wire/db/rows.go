// Package db holds the row shapes scanned from jollyroger's tables. The same shapes serve both
// dialects: Time and NullTime accept PostgreSQL's time.Time and SQLite's text timestamps. Shapes
// are asserted in the adapter tests (internal/adapter/db).
package db

import (
	"fmt"
	"time"
)

// TextTimeLayout is how SQLite stores timestamps: UTC with exactly six fraction digits, so that
// lexical order is chronological order.
const TextTimeLayout = "2006-01-02T15:04:05.000000Z"

// Time is a NOT NULL timestamp column.
type Time struct{ time.Time }

// Scan implements sql.Scanner.
func (t *Time) Scan(src any) error {
	v, err := parseTime(src)
	if err != nil {
		return err
	}
	t.Time = v
	return nil
}

// NullTime is a nullable timestamp column.
type NullTime struct {
	Time  time.Time
	Valid bool
}

// Scan implements sql.Scanner.
func (t *NullTime) Scan(src any) error {
	if src == nil {
		*t = NullTime{}
		return nil
	}
	v, err := parseTime(src)
	if err != nil {
		return err
	}
	*t = NullTime{Time: v, Valid: true}
	return nil
}

func parseTime(src any) (time.Time, error) {
	switch v := src.(type) {
	case time.Time:
		return v.UTC(), nil
	case string:
		return parseText(v)
	case []byte:
		return parseText(string(v))
	default:
		return time.Time{}, fmt.Errorf("unsupported timestamp value of type %T", src)
	}
}

func parseText(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}

// EnvironmentRow is a row of jollyroger_environments.
type EnvironmentRow struct {
	ID        string
	Key       string
	Name      string
	Position  int
	CreatedAt Time
}

// FlagRow is a row of jollyroger_flags.
type FlagRow struct {
	ID          string
	Key         string
	Name        string
	Description string
	Kind        string
	CreatedAt   Time
	UpdatedAt   Time
	ArchivedAt  NullTime
}

// StateRow is a row of jollyroger_flag_env_states joined with its environment key.
type StateRow struct {
	EnvironmentKey string
	Enabled        bool
	Config         string
	Version        int64
	UpdatedAt      Time
	UpdatedBy      string
}

// SnapshotRow is one flag of a snapshot read.
type SnapshotRow struct {
	Key     string
	Enabled bool
	Version int64
	Config  string
}

// AuditRow is a row of jollyroger_audit_log.
type AuditRow struct {
	ID             string
	EnvironmentKey *string
	FlagKey        *string
	ActorID        string
	ActorName      string
	Action         string
	Before         *string
	After          *string
	Reason         *string
	CreatedAt      Time
}
