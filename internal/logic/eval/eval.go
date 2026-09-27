// Package eval is jollyroger's evaluation engine: it compiles a model.Snapshot into an immutable,
// lock-free lookup structure and evaluates flags against it. It is pure (no I/O) and its behaviour
// is specified in protocol/evaluation-spec.md, which every SDK implements.
package eval

import "github.com/joaomarcosfurtado/jollyroger/internal/model"

// Snapshot is a compiled, immutable snapshot of one environment. It is safe for concurrent use;
// a new revision is a new Snapshot, never a mutation.
type Snapshot struct {
	environment string
	revision    int64
	flags       map[string]compiledFlag
}

type compiledFlag struct {
	enabled   bool
	duplicate bool // the key appeared more than once in the source snapshot
	version   int64
	value     bool
	errorCode model.ErrorCode // non-empty when the flag cannot be evaluated by this engine
}

// Compile builds an evaluable snapshot. It never fails: a flag this engine cannot evaluate (an
// unsupported or invalid configuration, or a duplicated key) is compiled to evaluate as
// PARSE_ERROR, so one bad flag never takes down the others. Compile copies everything it keeps.
func Compile(s model.Snapshot) *Snapshot {
	flags := make(map[string]compiledFlag, len(s.Flags))
	for _, f := range s.Flags {
		if _, dup := flags[f.Key]; dup {
			flags[f.Key] = compiledFlag{duplicate: true}
			continue
		}
		flags[f.Key] = compileFlag(f)
	}
	return &Snapshot{environment: s.Environment, revision: s.Revision, flags: flags}
}

func compileFlag(f model.SnapshotFlag) compiledFlag {
	c := compiledFlag{enabled: f.Enabled, version: f.Version}
	serve := f.Config.Fallthrough
	if f.Config.Unparseable || len(f.Config.Rules) > 0 || serve.Split != nil {
		// Undecodable, written by a newer admin plane, or invalid (value and split both set).
		c.errorCode = model.ErrorCodeParseError
		return c
	}
	c.value = true // an unset fallthrough serves true
	if serve.Value != nil {
		c.value = *serve.Value
	}
	return c
}

// Evaluate returns the value of flag key for evalCtx. It never panics, never blocks, and does
// not allocate. The algorithm is protocol/evaluation-spec.md section "Algorithm"; the check order
// matters: a duplicated key is an error whatever its state, and a disabled flag is simply off
// even when its configuration is unusable.
func (s *Snapshot) Evaluate(key string, evalCtx model.Context) model.Result {
	_ = evalCtx // used by targeting rules and splits in later milestones
	if s == nil {
		return model.Result{Reason: model.ReasonError, ErrorCode: model.ErrorCodeProviderNotReady}
	}
	f, ok := s.flags[key]
	if !ok {
		return model.Result{Reason: model.ReasonError, ErrorCode: model.ErrorCodeFlagNotFound}
	}
	if f.duplicate {
		return model.Result{Reason: model.ReasonError, ErrorCode: model.ErrorCodeParseError}
	}
	if !f.enabled {
		return model.Result{Reason: model.ReasonDisabled, FlagVersion: f.version}
	}
	if f.errorCode != "" {
		return model.Result{Reason: model.ReasonError, ErrorCode: f.errorCode, FlagVersion: f.version}
	}
	return model.Result{Value: f.value, Reason: model.ReasonStatic, FlagVersion: f.version}
}

// Revision returns the store revision this snapshot was built from (0 for a nil snapshot).
func (s *Snapshot) Revision() int64 {
	if s == nil {
		return 0
	}
	return s.revision
}

// Environment returns the environment key of this snapshot ("" for a nil snapshot).
func (s *Snapshot) Environment() string {
	if s == nil {
		return ""
	}
	return s.environment
}

// Len returns the number of distinct flag keys in the snapshot (0 for a nil snapshot).
func (s *Snapshot) Len() int {
	if s == nil {
		return 0
	}
	return len(s.flags)
}
