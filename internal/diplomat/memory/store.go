// Package memory is an in-memory model.FlagStore for tests (jollyroger's own, and adopters'
// through jollyrogertest). It must behave exactly like the SQL stores: it passes the same
// internal/storetest suite, keeps microsecond UTC times, passes configs and audit values through
// JSON (numbers read back as float64), enforces unique IDs, and serializes transactions like a
// database would.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	adb "github.com/joaomarcosfurtado/jollyroger/internal/adapter/db"
	"github.com/joaomarcosfurtado/jollyroger/internal/model"
)

// Store is an in-memory model.FlagStore. The zero value is not usable; call New.
type Store struct {
	mu   sync.Mutex
	data *dataset
}

type dataset struct {
	projects map[string]*project // by key
}

type project struct {
	id       string
	revision int64
	envs     []model.Environment    // ordered by position
	flags    map[string]*flagRecord // by key
	audit    []model.AuditEntry
}

type flagRecord struct {
	flag   model.Flag
	states map[string]model.EnvState // by environment key
}

// New returns a store seeded like a freshly migrated database: project "default" with the
// development, staging and production environments, at revision 0.
func New() *Store {
	seeded := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &project{
		id: "00000000000000000000000000",
		envs: []model.Environment{
			{ID: "00000000000000000000000001", Key: "development", Name: "Development", Position: 1, CreatedAt: seeded},
			{ID: "00000000000000000000000002", Key: "staging", Name: "Staging", Position: 2, CreatedAt: seeded},
			{ID: "00000000000000000000000003", Key: "production", Name: "Production", Position: 3, CreatedAt: seeded},
		},
		flags: map[string]*flagRecord{},
	}
	return &Store{data: &dataset{projects: map[string]*project{model.DefaultProject: p}}}
}

func norm(t time.Time) time.Time { return t.UTC().Truncate(model.TimePrecision) }

func notFound(what, key string) error { return fmt.Errorf("%s %q: %w", what, key, model.ErrNotFound) }

func (d *dataset) project(key string) (*project, error) {
	p, ok := d.projects[key]
	if !ok {
		return nil, notFound("project", key)
	}
	return p, nil
}

func (p *project) hasEnv(key string) bool {
	return slices.ContainsFunc(p.envs, func(e model.Environment) bool { return e.Key == key })
}

func (p *project) flagWithStates(key string) (model.FlagWithStates, error) {
	rec, ok := p.flags[key]
	if !ok {
		return model.FlagWithStates{}, notFound("flag", key)
	}
	out := model.FlagWithStates{Flag: cloneFlag(rec.flag)}
	for _, e := range p.envs {
		out.States = append(out.States, cloneState(rec.states[e.Key]))
	}
	return out, nil
}

// LoadSnapshot implements model.FlagStore.
func (s *Store) LoadSnapshot(_ context.Context, projectKey, environment string) (model.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.data.project(projectKey)
	if err != nil {
		return model.Snapshot{}, err
	}
	if !p.hasEnv(environment) {
		return model.Snapshot{}, notFound("environment", environment)
	}
	snap := model.Snapshot{Environment: environment, Revision: p.revision}
	for _, key := range sortedKeys(p.flags) {
		rec := p.flags[key]
		if rec.flag.ArchivedAt != nil {
			continue
		}
		st := rec.states[environment]
		snap.Flags = append(snap.Flags, model.SnapshotFlag{Key: key, Enabled: st.Enabled, Version: st.Version, Config: cloneConfig(st.Config)})
	}
	return snap, nil
}

// Revision implements model.FlagStore.
func (s *Store) Revision(_ context.Context, projectKey string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.data.project(projectKey)
	if err != nil {
		return 0, err
	}
	return p.revision, nil
}

// ListEnvironments implements model.FlagStore.
func (s *Store) ListEnvironments(_ context.Context, projectKey string) ([]model.Environment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.data.project(projectKey)
	if err != nil {
		return nil, err
	}
	return slices.Clone(p.envs), nil
}

// GetFlag implements model.FlagStore.
func (s *Store) GetFlag(_ context.Context, projectKey, key string) (model.FlagWithStates, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.data.project(projectKey)
	if err != nil {
		return model.FlagWithStates{}, err
	}
	return p.flagWithStates(key)
}

// ListFlags implements model.FlagStore.
func (s *Store) ListFlags(_ context.Context, q model.FlagQuery) (model.FlagPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.data.project(q.Project)
	if err != nil {
		return model.FlagPage{}, err
	}
	if !p.hasEnv(q.Environment) {
		return model.FlagPage{}, notFound("environment", q.Environment)
	}
	limit := q.EffectiveLimit()
	var page model.FlagPage
	for _, key := range sortedKeys(p.flags) {
		rec := p.flags[key]
		if key <= q.AfterKey || (!q.IncludeArchived && rec.flag.ArchivedAt != nil) {
			continue
		}
		if len(page.Items) == limit {
			page.HasMore = true
			break
		}
		page.Items = append(page.Items, model.FlagView{Flag: cloneFlag(rec.flag), State: cloneState(rec.states[q.Environment])})
	}
	return page, nil
}

// ListAudit implements model.FlagStore.
func (s *Store) ListAudit(_ context.Context, q model.AuditQuery) (model.AuditPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.data.project(q.Project)
	if err != nil {
		return model.AuditPage{}, err
	}
	entries := slices.Clone(p.audit)
	slices.SortFunc(entries, func(a, b model.AuditEntry) int { return strings.Compare(b.ID, a.ID) })
	limit := q.EffectiveLimit()
	var page model.AuditPage
	for _, e := range entries {
		if (q.FlagKey != "" && e.FlagKey != q.FlagKey) ||
			(q.EnvironmentKey != "" && e.EnvironmentKey != q.EnvironmentKey) ||
			(q.BeforeID != "" && e.ID >= q.BeforeID) {
			continue
		}
		if len(page.Items) == limit {
			page.HasMore = true
			break
		}
		c, err := cloneAudit(e)
		if err != nil {
			return model.AuditPage{}, err
		}
		page.Items = append(page.Items, c)
	}
	return page, nil
}

// InTx implements model.FlagStore. Transactions are serialized: fn runs on a private copy of the
// data, which replaces the data only if fn returns nil.
func (s *Store) InTx(_ context.Context, fn func(model.FlagTx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	work := s.data.clone()
	if err := fn(&tx{d: work}); err != nil {
		return err
	}
	s.data = work
	return nil
}

type tx struct{ d *dataset }

func (t *tx) GetFlag(_ context.Context, projectKey, key string) (model.FlagWithStates, error) {
	p, err := t.d.project(projectKey)
	if err != nil {
		return model.FlagWithStates{}, err
	}
	return p.flagWithStates(key)
}

func (t *tx) CreateFlag(_ context.Context, f model.NewFlag) (model.FlagWithStates, error) {
	p, err := t.d.project(f.Project)
	if err != nil {
		return model.FlagWithStates{}, err
	}
	if _, taken := p.flags[f.Key]; taken || t.d.flagIDUsed(f.ID) {
		return model.FlagWithStates{}, fmt.Errorf("flag %q (id %s): key or id already used: %w", f.Key, f.ID, model.ErrAlreadyExists)
	}
	at := norm(f.At)
	rec := &flagRecord{
		flag:   model.Flag{ID: f.ID, Key: f.Key, Name: f.Name, Description: f.Description, Kind: f.Kind, CreatedAt: at, UpdatedAt: at},
		states: map[string]model.EnvState{},
	}
	for _, e := range p.envs {
		rec.states[e.Key] = model.EnvState{EnvironmentKey: e.Key, Version: 1, UpdatedAt: at, UpdatedBy: f.Actor}
	}
	p.flags[f.Key] = rec
	return p.flagWithStates(f.Key)
}

func (t *tx) record(projectKey, key string) (*flagRecord, error) {
	p, err := t.d.project(projectKey)
	if err != nil {
		return nil, err
	}
	rec, ok := p.flags[key]
	if !ok {
		return nil, notFound("flag", key)
	}
	return rec, nil
}

func (t *tx) UpdateFlagMeta(_ context.Context, projectKey, key string, m model.FlagMeta) (model.Flag, error) {
	rec, err := t.record(projectKey, key)
	if err != nil {
		return model.Flag{}, err
	}
	rec.flag.Name, rec.flag.Description, rec.flag.UpdatedAt = m.Name, m.Description, norm(m.At)
	return cloneFlag(rec.flag), nil
}

func (t *tx) ArchiveFlag(_ context.Context, projectKey, key string, at time.Time) (model.Flag, error) {
	rec, err := t.record(projectKey, key)
	if err != nil {
		return model.Flag{}, err
	}
	if rec.flag.ArchivedAt == nil {
		n := norm(at)
		rec.flag.ArchivedAt, rec.flag.UpdatedAt = &n, n
	}
	return cloneFlag(rec.flag), nil
}

func (t *tx) RestoreFlag(_ context.Context, projectKey, key string, at time.Time) (model.Flag, error) {
	rec, err := t.record(projectKey, key)
	if err != nil {
		return model.Flag{}, err
	}
	if rec.flag.ArchivedAt != nil {
		rec.flag.ArchivedAt, rec.flag.UpdatedAt = nil, norm(at)
	}
	return cloneFlag(rec.flag), nil
}

func (t *tx) SetEnvState(_ context.Context, projectKey, key, environment string, c model.EnvStateChange, expectedVersion int64) (model.EnvState, error) {
	if c.Config.Unparseable {
		return model.EnvState{}, fmt.Errorf("flag %q in %q: refusing to store a config this version cannot read (it would destroy the stored rules): %w", key, environment, model.ErrInvalid)
	}
	p, err := t.d.project(projectKey)
	if err != nil {
		return model.EnvState{}, err
	}
	rec, ok := p.flags[key]
	if !ok {
		return model.EnvState{}, notFound("flag", key)
	}
	if !p.hasEnv(environment) {
		return model.EnvState{}, notFound("environment", environment)
	}
	stored, err := storedConfig(c.Config)
	if err != nil {
		return model.EnvState{}, err
	}
	cur := rec.states[environment]
	if cur.Version != expectedVersion {
		return model.EnvState{}, fmt.Errorf("flag %q in %q is at version %d, not %d: %w", key, environment, cur.Version, expectedVersion, model.ErrConflict)
	}
	next := model.EnvState{EnvironmentKey: environment, Enabled: c.Enabled, Config: stored, Version: cur.Version + 1, UpdatedAt: norm(c.At), UpdatedBy: c.Actor}
	rec.states[environment] = next
	return cloneState(next), nil
}

func (t *tx) AppendAudit(_ context.Context, e model.AuditEntry) error {
	p, err := t.d.project(e.Project)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(p.audit, func(a model.AuditEntry) bool { return a.ID == e.ID }) {
		return fmt.Errorf("audit id %s: %w", e.ID, model.ErrAlreadyExists)
	}
	stored, err := cloneAudit(e)
	if err != nil {
		return err
	}
	stored.CreatedAt = norm(e.CreatedAt)
	p.audit = append(p.audit, stored)
	return nil
}

func (t *tx) BumpRevision(_ context.Context, projectKey string) (int64, error) {
	p, err := t.d.project(projectKey)
	if err != nil {
		return 0, err
	}
	p.revision++
	return p.revision, nil
}

// flagIDUsed reports whether any flag of any project already has id (IDs are primary keys).
func (d *dataset) flagIDUsed(id string) bool {
	for _, p := range d.projects {
		for _, rec := range p.flags {
			if rec.flag.ID == id {
				return true
			}
		}
	}
	return false
}

// storedConfig passes a config through the stored JSON form, exactly as the SQL stores do.
func storedConfig(c model.FlagConfig) (model.FlagConfig, error) {
	s, err := adb.ConfigToJSON(c)
	if err != nil {
		return model.FlagConfig{}, err
	}
	return adb.ConfigFromJSON(s), nil
}

func sortedKeys(m map[string]*flagRecord) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys) // byte order, like the SQL stores
	return keys
}

func (d *dataset) clone() *dataset {
	c := &dataset{projects: make(map[string]*project, len(d.projects))}
	for k, p := range d.projects {
		np := &project{id: p.id, revision: p.revision, envs: slices.Clone(p.envs), flags: make(map[string]*flagRecord, len(p.flags)), audit: slices.Clone(p.audit)}
		for fk, rec := range p.flags {
			states := make(map[string]model.EnvState, len(rec.states))
			for ek, st := range rec.states {
				states[ek] = cloneState(st)
			}
			np.flags[fk] = &flagRecord{flag: cloneFlag(rec.flag), states: states}
		}
		c.projects[k] = np
	}
	return c
}

func cloneFlag(f model.Flag) model.Flag {
	if f.ArchivedAt != nil {
		at := *f.ArchivedAt
		f.ArchivedAt = &at
	}
	return f
}

func cloneState(s model.EnvState) model.EnvState {
	s.Config = cloneConfig(s.Config)
	return s
}

func cloneConfig(c model.FlagConfig) model.FlagConfig {
	out := model.FlagConfig{Fallthrough: cloneServe(c.Fallthrough), Unparseable: c.Unparseable}
	for _, r := range c.Rules {
		nr := model.Rule{Serve: cloneServe(r.Serve)}
		for _, cd := range r.Conditions {
			nr.Conditions = append(nr.Conditions, model.Condition{Attribute: cd.Attribute, Operator: cd.Operator, Values: slices.Clone(cd.Values)})
		}
		out.Rules = append(out.Rules, nr)
	}
	return out
}

func cloneServe(s model.Serve) model.Serve {
	var out model.Serve
	if s.Value != nil {
		v := *s.Value
		out.Value = &v
	}
	if s.Split != nil {
		out.Split = &model.Split{Variations: slices.Clone(s.Split.Variations), BucketBy: s.Split.BucketBy, Salt: s.Split.Salt}
	}
	return out
}

// cloneAudit copies an entry, passing Before/After through JSON exactly as the SQL stores do.
func cloneAudit(e model.AuditEntry) (model.AuditEntry, error) {
	var err error
	if e.Before, err = jsonCopy(e.Before); err != nil {
		return model.AuditEntry{}, fmt.Errorf("audit %s before: %w", e.ID, err)
	}
	if e.After, err = jsonCopy(e.After); err != nil {
		return model.AuditEntry{}, fmt.Errorf("audit %s after: %w", e.ID, err)
	}
	return e, nil
}

func jsonCopy(m map[string]any) (map[string]any, error) {
	if m == nil {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal(b, &out)
}
