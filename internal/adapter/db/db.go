// Package db translates between jollyroger's table rows (internal/wire/db) and the domain model.
// It is pure. The config column holds the protocol config JSON, so it is encoded and decoded
// through internal/adapter/api exactly like a snapshot document.
package db

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/joaomarcosfurtado/jollyroger/internal/adapter/api"
	"github.com/joaomarcosfurtado/jollyroger/internal/model"
	wiredb "github.com/joaomarcosfurtado/jollyroger/internal/wire/db"
	"github.com/joaomarcosfurtado/jollyroger/internal/wire/out"
)

// NormalizeTime converts t to UTC with the precision every store keeps.
func NormalizeTime(t time.Time) time.Time {
	return t.UTC().Truncate(model.TimePrecision)
}

// TextTime formats t as SQLite stores it.
func TextTime(t time.Time) string {
	return NormalizeTime(t).Format(wiredb.TextTimeLayout)
}

// ConfigToJSON encodes a flag config as the protocol config JSON.
func ConfigToJSON(c model.FlagConfig) (string, error) {
	b, err := json.Marshal(api.ConfigToWire(c))
	if err != nil {
		return "", fmt.Errorf("encode flag config: %w", err)
	}
	return string(b), nil
}

// ConfigFromJSON decodes a stored config. Anything that is not a config this version understands
// (invalid JSON included) is Unparseable, per protocol/evaluation-spec.md section 2.
func ConfigFromJSON(s string) model.FlagConfig {
	var w out.FlagConfig
	if json.Unmarshal([]byte(s), &w) != nil {
		return model.FlagConfig{Unparseable: true}
	}
	return api.ConfigFromWire(w)
}

// AuditJSON encodes an audit Before/After value; nil stays NULL.
func AuditJSON(m map[string]any) (*string, error) {
	if m == nil {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode audit value: %w", err)
	}
	s := string(b)
	return &s, nil
}

// NullIfEmpty maps "" to NULL for optional text columns.
func NullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// RowToEnvironment converts an environment row.
func RowToEnvironment(r wiredb.EnvironmentRow) model.Environment {
	return model.Environment{ID: r.ID, Key: r.Key, Name: r.Name, Position: r.Position, CreatedAt: r.CreatedAt.Time}
}

// RowToFlag converts a flag row.
func RowToFlag(r wiredb.FlagRow) model.Flag {
	f := model.Flag{
		ID: r.ID, Key: r.Key, Name: r.Name, Description: r.Description, Kind: model.Kind(r.Kind),
		CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time,
	}
	if r.ArchivedAt.Valid {
		at := r.ArchivedAt.Time
		f.ArchivedAt = &at
	}
	return f
}

// RowToEnvState converts a state row.
func RowToEnvState(r wiredb.StateRow) model.EnvState {
	return model.EnvState{
		EnvironmentKey: r.EnvironmentKey, Enabled: r.Enabled, Config: ConfigFromJSON(r.Config),
		Version: r.Version, UpdatedAt: r.UpdatedAt.Time, UpdatedBy: r.UpdatedBy,
	}
}

// RowsToSnapshot builds a snapshot from rows already ordered by key.
func RowsToSnapshot(environment string, revision int64, rows []wiredb.SnapshotRow) model.Snapshot {
	snap := model.Snapshot{Environment: environment, Revision: revision}
	for _, r := range rows {
		snap.Flags = append(snap.Flags, model.SnapshotFlag{Key: r.Key, Enabled: r.Enabled, Version: r.Version, Config: ConfigFromJSON(r.Config)})
	}
	return snap
}

// RowToAudit converts an audit row of project. Corrupt JSON in Before/After is an error: the audit
// log is evidence and must not be silently altered on read.
func RowToAudit(project string, r wiredb.AuditRow) (model.AuditEntry, error) {
	e := model.AuditEntry{
		ID: r.ID, Project: project, EnvironmentKey: deref(r.EnvironmentKey), FlagKey: deref(r.FlagKey),
		ActorID: r.ActorID, ActorName: r.ActorName, Action: model.AuditAction(r.Action),
		Reason: deref(r.Reason), CreatedAt: r.CreatedAt.Time,
	}
	var err error
	if e.Before, err = auditValue(r.Before); err != nil {
		return model.AuditEntry{}, fmt.Errorf("audit %s before: %w", r.ID, err)
	}
	if e.After, err = auditValue(r.After); err != nil {
		return model.AuditEntry{}, fmt.Errorf("audit %s after: %w", r.ID, err)
	}
	return e, nil
}

func auditValue(s *string) (map[string]any, error) {
	if s == nil {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(*s), &m); err != nil {
		return nil, err
	}
	return m, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
