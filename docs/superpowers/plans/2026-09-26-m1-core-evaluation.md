# M1 Core: Evaluation Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the pure core of jollyroger: the domain model, input validation, the deterministic
evaluation engine with bucketing, the snapshot wire format, and the cross-SDK test vectors that
every future SDK must pass.

**Architecture:** Diplomat layers (see `docs/skills/diplomat-architecture.md`). `internal/model`
holds types and sentinel errors (stdlib only). `internal/logic/validate` and `internal/logic/eval`
are pure. `internal/wire/out` holds the snapshot JSON shape (the local-evaluation protocol), and
`internal/adapter/api` is the only translation between that shape and the model. Nothing in M1
performs I/O; `internal/archtest` must stay green.

**Tech Stack:** Go 1.26 standard library only (`crypto/sha256`, `encoding/binary`,
`encoding/json`, `regexp`, `unicode`, `unicode/utf8`). Test vectors in JSON under `protocol/`.

**Spec:** `docs/superpowers/specs/2026-09-26-jollyroger-design.md` (sections 2, 3, 6, 7).

## Global Constraints

- Module `github.com/joaomarcosfurtado/jollyroger`, `go 1.26.0`; no new dependency (stdlib only).
- Layer table enforced by `internal/archtest`; pure layers must not import `os`, `net/http`,
  `database/sql`, `log`, `log/slog`, and similar I/O packages.
- No package-level mutable state; package-level values limited to constants, sentinel errors,
  compiled regexps (`CLAUDE.md` rule 1, `code-organization.md`).
- Flag key pattern: `^[a-z0-9][a-z0-9._-]{0,127}$`. Name max 200 characters, description max
  2000, reason max 500 (spec section 6, `secure-by-design.md`).
- Reasons: `DISABLED`, `STATIC`, `TARGETING_MATCH`, `SPLIT`, `DEFAULT`, `ERROR`. Error codes:
  `FLAG_NOT_FOUND`, `PARSE_ERROR`, `GENERAL` (spec section 6).
- Bucketing: `uint32 big-endian(first 4 bytes of SHA-256(flagKey + "." + salt + "." + value)) mod
  100000` (spec section 6).
- v0.1 engine supports: no rules, fallthrough fixed value (default `true` when unset). Rules and
  splits are rejected by validation and compile to `PARSE_ERROR` in the engine.
- `(*eval.Snapshot).Evaluate` must be 0 allocs/op (spec success criterion 2).
- JSON wire fields are snake_case; adapters are the only translation point.
- No em-dash character anywhere; never mention any company as the origin of the architecture;
  English only. Commit trailer: `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **A snapshot written by a newer admin plane** (rules or a split this engine does not support):
   that one flag must evaluate to `false` / `ERROR` / `PARSE_ERROR`; every other flag keeps working.
   Pinned in Task 4 (`TestEvaluate` rows "rules unsupported", "split unsupported").
2. **Evaluation before any snapshot is loaded** (nil `*Snapshot`): must return `false` / `ERROR` /
   `GENERAL`, never panic. Pinned in Task 4 (`TestEvaluate_NilSnapshot`).
3. **Hostile text input** (invalid UTF-8, control characters such as `\x1b` or `\x00`, a name that
   is only spaces): must be a `ValidationError`, never stored. Pinned in Task 2.
4. **Duplicate flag keys in one snapshot** (corrupt data): the key must deterministically evaluate
   to `PARSE_ERROR` rather than "last one wins". Pinned in Task 4 (`TestEvaluate` row "duplicate
   key").
5. **Snapshot JSON from an incompatible writer** (`schema_version` other than 1) must be rejected
   with `ErrInvalid`, while unknown extra fields from a compatible newer writer are tolerated.
   Pinned in Task 5 (`TestSnapshotFromWire_RejectsUnknownSchemaVersion`,
   `TestSnapshotFromWire_ToleratesUnknownFields`).

---

## File structure

| File | Responsibility |
|---|---|
| `internal/model/errors.go` | sentinel errors + `ValidationError` |
| `internal/model/config.go` | `FlagConfig`, `Rule`, `Condition`, `Serve`, `Split`, `WeightedVariation` |
| `internal/model/evaluation.go` | `Context`, `Result`, `Reason`, `ErrorCode` |
| `internal/model/snapshot.go` | `Snapshot`, `SnapshotFlag` (raw data for one environment) |
| `internal/logic/validate/validate.go` | pure input validation, returns `*model.ValidationError` |
| `internal/logic/eval/bucket.go` | deterministic bucketing |
| `internal/logic/eval/eval.go` | `Compile` a `model.Snapshot` into an immutable `*Snapshot`; `Evaluate` |
| `internal/wire/out/snapshot.go` | snapshot JSON shape (protocol) |
| `internal/adapter/api/snapshot.go` | `SnapshotFromWire` / `SnapshotToWire` |
| `protocol/evaluation-spec.md` | normative evaluation spec for all SDKs |
| `protocol/testdata/vectors.json` | cross-SDK evaluation + bucketing vectors |
| `internal/logic/eval/vectors_test.go` | runs the vectors against the Go engine |

Deliberate refinements of the spec, applied in Task 7's doc update: `logic/snapshot` is merged
into `logic/eval` (compiling and evaluating are one concern and share the private compiled type),
and `adapter/http` is named `adapter/api` (a package named `http` would shadow `net/http` in every
file that uses both).

---

### Task 1: Domain model types and errors

**Files:**
- Create: `internal/model/errors.go`, `internal/model/config.go`, `internal/model/evaluation.go`,
  `internal/model/snapshot.go`
- Test: `internal/model/errors_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `model.ErrNotFound`, `ErrConflict`, `ErrAlreadyExists`, `ErrInvalid`, `ErrForbidden`,
  `ErrUnauthenticated`, `ErrLocked`; `type ValidationError struct{ Fields map[string]string }`
  with `Error() string` and `Is(error) bool`; `FlagConfig{Rules []Rule; Fallthrough Serve}`,
  `Rule{Conditions []Condition; Serve Serve}`, `Condition{Attribute, Operator string; Values
  []string}`, `Serve{Value *bool; Split *Split}`, `Split{Variations []WeightedVariation; BucketBy,
  Salt string}`, `WeightedVariation{Value bool; Weight int}`; `Context{UserID string; Attributes
  map[string]any}`; `Reason` and `ErrorCode` string types with the constants listed in Global
  Constraints; `Result{Value bool; Reason Reason; FlagVersion int64; ErrorCode ErrorCode}`;
  `Snapshot{Environment string; Revision int64; Flags []SnapshotFlag}`, `SnapshotFlag{Key string;
  Enabled bool; Version int64; Config FlagConfig}`.

- [ ] **Step 1: Write the failing test** `internal/model/errors_test.go`

```go
package model_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/joaomarcosfurtado/jollyroger/internal/model"
)

func TestValidationError_IsErrInvalid(t *testing.T) {
	t.Parallel()
	var err error = &model.ValidationError{Fields: map[string]string{"key": "bad"}}
	wrapped := fmt.Errorf("create flag: %w", err)

	if !errors.Is(wrapped, model.ErrInvalid) {
		t.Fatal("a wrapped ValidationError must match model.ErrInvalid")
	}
	var ve *model.ValidationError
	if !errors.As(wrapped, &ve) || ve.Fields["key"] != "bad" {
		t.Fatalf("errors.As must recover the field map, got %#v", ve)
	}
	for _, other := range []error{model.ErrNotFound, model.ErrConflict, model.ErrAlreadyExists,
		model.ErrForbidden, model.ErrUnauthenticated, model.ErrLocked} {
		if errors.Is(wrapped, other) {
			t.Errorf("a ValidationError must not match %v", other)
		}
	}
}

func TestValidationError_MessageIsSortedAndStable(t *testing.T) {
	t.Parallel()
	err := &model.ValidationError{Fields: map[string]string{"name": "is required", "key": "bad"}}
	if got, want := err.Error(), "invalid input: key: bad; name: is required"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if got, want := (&model.ValidationError{}).Error(), "invalid input"; got != want {
		t.Fatalf("empty Error() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/model/`
Expected: FAIL, build error `undefined: model.ValidationError` (package does not exist yet).

- [ ] **Step 3: Implement** `internal/model/errors.go`

```go
// Package model holds jollyroger's domain types, sentinel errors, and (from M2) the port
// interfaces. It depends on the standard library only and performs no I/O.
package model

import (
	"errors"
	"maps"
	"slices"
	"strings"
)

// Sentinel errors for expected failures. Wrap them with context using fmt.Errorf("...: %w", err)
// and test with errors.Is. Each one maps to exactly one HTTP response in the error registry.
var (
	ErrNotFound        = errors.New("not found")
	ErrConflict        = errors.New("conflict")
	ErrAlreadyExists   = errors.New("already exists")
	ErrInvalid         = errors.New("invalid")
	ErrForbidden       = errors.New("forbidden")
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrLocked          = errors.New("locked")
)

// ValidationError reports field-level validation failures. Fields maps a stable field name
// ("key", "name", ...) to a human-readable message. It matches ErrInvalid under errors.Is.
type ValidationError struct {
	Fields map[string]string
}

// Error lists the failures sorted by field name, so the message is stable.
func (e *ValidationError) Error() string {
	if len(e.Fields) == 0 {
		return "invalid input"
	}
	keys := slices.Sorted(maps.Keys(e.Fields))
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + ": " + e.Fields[k]
	}
	return "invalid input: " + strings.Join(parts, "; ")
}

// Is makes errors.Is(err, ErrInvalid) true for a ValidationError.
func (e *ValidationError) Is(target error) bool {
	return target == ErrInvalid
}
```

`internal/model/config.go`:

```go
package model

// FlagConfig is the evaluation configuration of a flag in one environment. It is designed for
// growth: rules and percentage splits have a place here even though the v0.1 engine supports only
// an empty rule list and a fixed fallthrough value.
type FlagConfig struct {
	Rules       []Rule
	Fallthrough Serve // served when no rule matches; zero value means "serve true"
}

// Rule serves Serve when every condition matches the evaluation context.
type Rule struct {
	Conditions []Condition
	Serve      Serve
}

// Condition compares one context attribute with a list of values using Operator.
type Condition struct {
	Attribute string
	Operator  string
	Values    []string
}

// Serve is what a rule or the fallthrough returns: a fixed Value or a percentage Split. Setting
// both is invalid.
type Serve struct {
	Value *bool
	Split *Split
}

// Split assigns a context to a variation by deterministic bucketing on BucketBy
// ("user_id" or "attr:<name>"). Weights are in units of 0.001% and sum to 100000.
type Split struct {
	Variations []WeightedVariation
	BucketBy   string
	Salt       string
}

// WeightedVariation is one outcome of a Split.
type WeightedVariation struct {
	Value  bool
	Weight int
}
```

`internal/model/evaluation.go`:

```go
package model

// Context is the evaluation context an application passes with a flag check.
type Context struct {
	UserID     string
	Attributes map[string]any
}

// Reason explains why an evaluation produced its value. The values are shared by every SDK.
type Reason string

// Evaluation reasons, aligned with OpenFeature.
const (
	ReasonDisabled       Reason = "DISABLED"
	ReasonStatic         Reason = "STATIC"
	ReasonTargetingMatch Reason = "TARGETING_MATCH"
	ReasonSplit          Reason = "SPLIT"
	ReasonDefault        Reason = "DEFAULT"
	ReasonError          Reason = "ERROR"
)

// ErrorCode qualifies a Result whose Reason is ReasonError. It is empty otherwise.
type ErrorCode string

// Evaluation error codes, aligned with OpenFeature.
const (
	ErrorCodeFlagNotFound ErrorCode = "FLAG_NOT_FOUND"
	ErrorCodeParseError   ErrorCode = "PARSE_ERROR"
	ErrorCodeGeneral      ErrorCode = "GENERAL"
)

// Result is the outcome of one evaluation.
type Result struct {
	Value       bool
	Reason      Reason
	FlagVersion int64
	ErrorCode   ErrorCode
}
```

`internal/model/snapshot.go`:

```go
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
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/model/ ./internal/archtest/`
Expected: PASS (archtest confirms `model` imports nothing internal and no I/O).

- [ ] **Step 5: Commit**

```bash
git add internal/model
git commit -m "feat(model): domain types, evaluation result, sentinel errors" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Input validation

**Files:**
- Create: `internal/logic/validate/validate.go`
- Test: `internal/logic/validate/validate_test.go`

**Interfaces:**
- Consumes: `model.ValidationError`, `model.ErrInvalid`, `model.FlagConfig` (Task 1).
- Produces (each returns `nil` or a `*model.ValidationError`):
  `FlagKey(key string) error` (field `"key"`), `EnvironmentKey(key string) error` (field
  `"environment"`), `FlagName(name string) error` (field `"name"`), `Description(d string) error`
  (field `"description"`), `ChangeReason(r string) error` (field `"reason"`),
  `FlagConfig(c model.FlagConfig) error` (fields `"rules"`, `"fallthrough"`);
  constants `MaxNameLength = 200`, `MaxDescriptionLength = 2000`, `MaxReasonLength = 500`.

- [ ] **Step 1: Write the failing test** `internal/logic/validate/validate_test.go`

```go
package validate_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/joaomarcosfurtado/jollyroger/internal/logic/validate"
	"github.com/joaomarcosfurtado/jollyroger/internal/model"
)

// expectField asserts err is a ValidationError on exactly field, or nil when field is "".
func expectField(t *testing.T, err error, field string) {
	t.Helper()
	if field == "" {
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		return
	}
	var ve *model.ValidationError
	if !errors.As(err, &ve) || !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("expected a ValidationError on %q, got %v", field, err)
	}
	if _, ok := ve.Fields[field]; !ok || len(ve.Fields) != 1 {
		t.Fatalf("expected exactly field %q, got %v", field, ve.Fields)
	}
}

func TestFlagKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		key   string
		field string
	}{
		{"new-checkout", ""},
		{"a", ""},
		{"0day", ""},
		{"billing.v2_beta-1", ""},
		{strings.Repeat("a", 128), ""},
		{strings.Repeat("a", 129), "key"},
		{"", "key"},
		{"New-Checkout", "key"},
		{"-leading-dash", "key"},
		{".leading-dot", "key"},
		{"has space", "key"},
		{"slash/inside", "key"},
		{"emoji-\U0001F3F4", "key"},
		{"new-checkout\n", "key"},
	}
	for _, tc := range cases {
		expectField(t, validate.FlagKey(tc.key), tc.field)
	}
}

func TestEnvironmentKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		key   string
		field string
	}{
		{"production", ""},
		{"eu-west-1", ""},
		{strings.Repeat("e", 64), ""},
		{strings.Repeat("e", 65), "environment"},
		{"", "environment"},
		{"Production", "environment"},
		{"prod.eu", "environment"},
		{"prod_eu", "environment"},
	}
	for _, tc := range cases {
		expectField(t, validate.EnvironmentKey(tc.key), tc.field)
	}
}

func TestFlagName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		value string
		field string
	}{
		{"plain", "New Checkout", ""},
		{"unicode letters count as one", strings.Repeat("é", validate.MaxNameLength), ""},
		{"too long", strings.Repeat("a", validate.MaxNameLength+1), "name"},
		{"empty", "", "name"},
		{"only spaces", "   ", "name"},
		{"newline", "New\nCheckout", "name"},
		{"escape sequence", "New \x1b[31mCheckout", "name"},
		{"nul byte", "New\x00Checkout", "name"},
		{"invalid utf8", "New \xff Checkout", "name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			expectField(t, validate.FlagName(tc.value), tc.field)
		})
	}
}

func TestDescription(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		value string
		field string
	}{
		{"empty is allowed", "", ""},
		{"multiline is allowed", "Line one.\r\nLine two.\n\tIndented.", ""},
		{"max length", strings.Repeat("d", validate.MaxDescriptionLength), ""},
		{"too long", strings.Repeat("d", validate.MaxDescriptionLength+1), "description"},
		{"escape sequence", "x\x1b[2J", "description"},
		{"invalid utf8", "\xc3\x28", "description"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			expectField(t, validate.Description(tc.value), tc.field)
		})
	}
}

func TestChangeReason(t *testing.T) {
	t.Parallel()
	expectField(t, validate.ChangeReason(""), "")
	expectField(t, validate.ChangeReason("Rollback after incident #42"), "")
	expectField(t, validate.ChangeReason(strings.Repeat("r", validate.MaxReasonLength+1)), "reason")
	expectField(t, validate.ChangeReason("bell\a"), "reason")
}

func TestFlagConfig(t *testing.T) {
	t.Parallel()
	yes := true
	cases := []struct {
		name   string
		config model.FlagConfig
		field  string
	}{
		{"empty config", model.FlagConfig{}, ""},
		{"fixed fallthrough", model.FlagConfig{Fallthrough: model.Serve{Value: &yes}}, ""},
		{"rules not supported yet", model.FlagConfig{Rules: []model.Rule{{Serve: model.Serve{Value: &yes}}}}, "rules"},
		{"split not supported yet", model.FlagConfig{Fallthrough: model.Serve{Split: &model.Split{}}}, "fallthrough"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			expectField(t, validate.FlagConfig(tc.config), tc.field)
		})
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/logic/validate/`
Expected: FAIL, build error (package `validate` does not exist).

- [ ] **Step 3: Implement** `internal/logic/validate/validate.go`

```go
// Package validate holds jollyroger's pure input validation. Every function returns nil or a
// *model.ValidationError naming the offending field, so callers can report every failure at the
// boundary and fail closed.
package validate

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/joaomarcosfurtado/jollyroger/internal/model"
)

// Length limits, in characters (runes).
const (
	MaxNameLength        = 200
	MaxDescriptionLength = 2000
	MaxReasonLength      = 500
)

var (
	flagKeyPattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	environmentKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

func invalid(field, message string) error {
	return &model.ValidationError{Fields: map[string]string{field: message}}
}

// FlagKey validates a flag key: 1 to 128 characters of lowercase letters, digits, '.', '_' or '-',
// starting with a letter or digit.
func FlagKey(key string) error {
	if !flagKeyPattern.MatchString(key) {
		return invalid("key", "must be 1 to 128 characters of lowercase letters, digits, '.', '_' or '-', starting with a letter or digit")
	}
	return nil
}

// EnvironmentKey validates an environment key: 1 to 64 characters of lowercase letters, digits or
// '-', starting with a letter or digit.
func EnvironmentKey(key string) error {
	if !environmentKeyPattern.MatchString(key) {
		return invalid("environment", "must be 1 to 64 characters of lowercase letters, digits or '-', starting with a letter or digit")
	}
	return nil
}

// FlagName validates a display name: required, single line, at most MaxNameLength characters.
func FlagName(name string) error {
	if strings.TrimSpace(name) == "" {
		return invalid("name", "is required")
	}
	return text("name", name, MaxNameLength, false)
}

// Description validates an optional, possibly multi-line description.
func Description(d string) error {
	return text("description", d, MaxDescriptionLength, true)
}

// ChangeReason validates the optional reason an operator gives for a change.
func ChangeReason(r string) error {
	return text("reason", r, MaxReasonLength, true)
}

// text checks encoding, length, and control characters. Control characters are rejected because
// they enable terminal-escape and log-injection tricks when an audit log is printed.
func text(field, s string, maxLen int, multiline bool) error {
	if !utf8.ValidString(s) {
		return invalid(field, "must be valid UTF-8")
	}
	if utf8.RuneCountInString(s) > maxLen {
		return invalid(field, "is too long")
	}
	for _, r := range s {
		if !unicode.IsControl(r) {
			continue
		}
		if multiline && (r == '\n' || r == '\r' || r == '\t') {
			continue
		}
		return invalid(field, "must not contain control characters")
	}
	return nil
}

// FlagConfig validates a configuration against what this version of the engine supports. v0.1
// supports no targeting rules and only a fixed fallthrough value.
func FlagConfig(c model.FlagConfig) error {
	if len(c.Rules) > 0 {
		return invalid("rules", "targeting rules are not supported yet")
	}
	if c.Fallthrough.Split != nil {
		return invalid("fallthrough", "percentage splits are not supported yet")
	}
	return nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/logic/validate/ ./internal/archtest/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/logic/validate
git commit -m "feat(validate): fail-closed validation for keys, names and configs" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Deterministic bucketing

**Files:**
- Create: `internal/logic/eval/bucket.go`
- Test: `internal/logic/eval/bucket_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `const BucketCount = 100000`; `func Bucket(flagKey, salt, value string) uint32`
  returning a value in `[0, BucketCount)`.

The expected values below were computed independently with Python `hashlib` and cross-checked with
`sha256sum`, not with the Go code under test.

- [ ] **Step 1: Write the failing test** `internal/logic/eval/bucket_test.go`

```go
package eval

import (
	"strconv"
	"testing"
)

func TestBucket_MatchesIndependentlyComputedValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		flagKey, salt, value string
		want                 uint32
	}{
		{"new-checkout", "", "user-1", 83561},
		{"new-checkout", "s1", "user-1", 16082},
		{"new-checkout", "", "user-2", 41251},
		{"a", "b", "c", 65252},
		{"flag.with-dots_1", "salt", "ümlaut-user", 77465},
		{"x", "", "", 30962},
	}
	for _, tc := range cases {
		if got := Bucket(tc.flagKey, tc.salt, tc.value); got != tc.want {
			t.Errorf("Bucket(%q, %q, %q) = %d, want %d", tc.flagKey, tc.salt, tc.value, got, tc.want)
		}
	}
}

func TestBucket_StaysInRange(t *testing.T) {
	t.Parallel()
	for i := range 10000 {
		if b := Bucket("range-check", "", "user-"+strconv.Itoa(i)); b >= BucketCount {
			t.Fatalf("bucket %d out of range", b)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/logic/eval/`
Expected: FAIL, `undefined: Bucket`.

- [ ] **Step 3: Implement** `internal/logic/eval/bucket.go`

```go
package eval

import (
	"crypto/sha256"
	"encoding/binary"
)

// BucketCount is the number of buckets; one bucket is 0.001% of traffic.
const BucketCount = 100000

// Bucket deterministically maps (flagKey, salt, value) to [0, BucketCount). The same inputs give
// the same bucket in every SDK: the first 4 bytes of SHA-256(flagKey + "." + salt + "." + value),
// read as a big-endian uint32, modulo BucketCount. Strings are hashed as UTF-8.
func Bucket(flagKey, salt, value string) uint32 {
	sum := sha256.Sum256([]byte(flagKey + "." + salt + "." + value))
	return binary.BigEndian.Uint32(sum[:4]) % BucketCount
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/logic/eval/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/logic/eval/bucket.go internal/logic/eval/bucket_test.go
git commit -m "feat(eval): deterministic SHA-256 bucketing" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Compile and evaluate snapshots

**Files:**
- Create: `internal/logic/eval/eval.go`
- Test: `internal/logic/eval/eval_test.go`

**Interfaces:**
- Consumes: `model.Snapshot`, `model.SnapshotFlag`, `model.FlagConfig`, `model.Context`,
  `model.Result`, reason and error-code constants (Task 1).
- Produces: `type Snapshot struct` (opaque, immutable); `func Compile(s model.Snapshot) *Snapshot`;
  `func (s *Snapshot) Evaluate(key string, evalCtx model.Context) model.Result`;
  `func (s *Snapshot) Revision() int64`; `func (s *Snapshot) Environment() string`;
  `func (s *Snapshot) Len() int`. M3's client stores `*eval.Snapshot` in an `atomic.Pointer`.

- [ ] **Step 1: Write the failing test** `internal/logic/eval/eval_test.go`

```go
package eval

import (
	"sync"
	"testing"

	"github.com/joaomarcosfurtado/jollyroger/internal/model"
)

func boolPtr(b bool) *bool { return &b }

func TestEvaluate(t *testing.T) {
	t.Parallel()
	snap := Compile(model.Snapshot{
		Environment: "production",
		Revision:    7,
		Flags: []model.SnapshotFlag{
			{Key: "default-true", Enabled: true, Version: 3},
			{Key: "fixed-false", Enabled: true, Version: 1, Config: model.FlagConfig{Fallthrough: model.Serve{Value: boolPtr(false)}}},
			{Key: "fixed-true", Enabled: true, Version: 2, Config: model.FlagConfig{Fallthrough: model.Serve{Value: boolPtr(true)}}},
			{Key: "off", Enabled: false, Version: 5},
			{Key: "rules-unsupported", Enabled: true, Version: 4, Config: model.FlagConfig{Rules: []model.Rule{{Serve: model.Serve{Value: boolPtr(true)}}}}},
			{Key: "split-unsupported", Enabled: true, Version: 6, Config: model.FlagConfig{Fallthrough: model.Serve{Split: &model.Split{}}}},
			{Key: "off-with-bad-config", Enabled: false, Version: 8, Config: model.FlagConfig{Fallthrough: model.Serve{Split: &model.Split{}}}},
			{Key: "dup", Enabled: true, Version: 1},
			{Key: "dup", Enabled: false, Version: 2},
		},
	})
	cases := []struct {
		name string
		key  string
		want model.Result
	}{
		{"enabled with empty config serves true", "default-true", model.Result{Value: true, Reason: model.ReasonStatic, FlagVersion: 3}},
		{"fixed fallthrough false", "fixed-false", model.Result{Value: false, Reason: model.ReasonStatic, FlagVersion: 1}},
		{"fixed fallthrough true", "fixed-true", model.Result{Value: true, Reason: model.ReasonStatic, FlagVersion: 2}},
		{"disabled", "off", model.Result{Value: false, Reason: model.ReasonDisabled, FlagVersion: 5}},
		{"missing", "nope", model.Result{Value: false, Reason: model.ReasonError, ErrorCode: model.ErrorCodeFlagNotFound}},
		{"keys are case-sensitive", "Default-True", model.Result{Value: false, Reason: model.ReasonError, ErrorCode: model.ErrorCodeFlagNotFound}},
		{"rules unsupported", "rules-unsupported", model.Result{Value: false, Reason: model.ReasonError, ErrorCode: model.ErrorCodeParseError, FlagVersion: 4}},
		{"split unsupported", "split-unsupported", model.Result{Value: false, Reason: model.ReasonError, ErrorCode: model.ErrorCodeParseError, FlagVersion: 6}},
		{"disabled wins over a bad config", "off-with-bad-config", model.Result{Value: false, Reason: model.ReasonDisabled, FlagVersion: 8}},
		{"duplicate key", "dup", model.Result{Value: false, Reason: model.ReasonError, ErrorCode: model.ErrorCodeParseError}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := snap.Evaluate(tc.key, model.Context{}); got != tc.want {
				t.Fatalf("Evaluate(%q) = %+v, want %+v", tc.key, got, tc.want)
			}
		})
	}
}

func TestEvaluate_NilSnapshot(t *testing.T) {
	t.Parallel()
	var snap *Snapshot
	want := model.Result{Value: false, Reason: model.ReasonError, ErrorCode: model.ErrorCodeGeneral}
	if got := snap.Evaluate("anything", model.Context{}); got != want {
		t.Fatalf("nil snapshot Evaluate = %+v, want %+v", got, want)
	}
	if snap.Len() != 0 || snap.Revision() != 0 || snap.Environment() != "" {
		t.Fatal("nil snapshot accessors must return zero values")
	}
}

func TestEvaluate_StaticFlagsIgnoreContext(t *testing.T) {
	t.Parallel()
	snap := Compile(model.Snapshot{Flags: []model.SnapshotFlag{{Key: "f", Enabled: true, Version: 1}}})
	a := snap.Evaluate("f", model.Context{})
	b := snap.Evaluate("f", model.Context{UserID: "123", Attributes: map[string]any{"plan": "premium"}})
	if a != b {
		t.Fatalf("static flag depended on context: %+v vs %+v", a, b)
	}
}

func TestCompile_DoesNotAliasInput(t *testing.T) {
	t.Parallel()
	value := true
	in := model.Snapshot{Flags: []model.SnapshotFlag{{Key: "f", Enabled: true, Config: model.FlagConfig{Fallthrough: model.Serve{Value: &value}}}}}
	snap := Compile(in)
	value = false
	in.Flags[0].Enabled = false
	if got := snap.Evaluate("f", model.Context{}); !got.Value {
		t.Fatalf("mutating the input after Compile changed the snapshot: %+v", got)
	}
}

func TestSnapshot_Accessors(t *testing.T) {
	t.Parallel()
	snap := Compile(model.Snapshot{Environment: "staging", Revision: 42, Flags: []model.SnapshotFlag{{Key: "a"}, {Key: "b"}}})
	if snap.Environment() != "staging" || snap.Revision() != 42 || snap.Len() != 2 {
		t.Fatalf("accessors = (%q, %d, %d)", snap.Environment(), snap.Revision(), snap.Len())
	}
}

func TestEvaluate_ConcurrentReaders(t *testing.T) {
	t.Parallel()
	snap := Compile(model.Snapshot{Flags: []model.SnapshotFlag{{Key: "f", Enabled: true, Version: 1}}})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 1000 {
				if !snap.Evaluate("f", model.Context{UserID: "u"}).Value {
					t.Error("concurrent Evaluate returned false")
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestEvaluate_ZeroAllocations(t *testing.T) {
	snap := Compile(model.Snapshot{Flags: []model.SnapshotFlag{{Key: "new-checkout", Enabled: true, Version: 1}}})
	ctx := model.Context{UserID: "123", Attributes: map[string]any{"country": "BR"}}
	for name, fn := range map[string]func(){
		"found":        func() { _ = snap.Evaluate("new-checkout", model.Context{}) },
		"with context": func() { _ = snap.Evaluate("new-checkout", ctx) },
		"not found":    func() { _ = snap.Evaluate("missing", ctx) },
	} {
		if allocs := testing.AllocsPerRun(1000, fn); allocs != 0 {
			t.Errorf("%s: Evaluate allocated %.1f times per run, want 0", name, allocs)
		}
	}
}

func BenchmarkEvaluate(b *testing.B) {
	snap := Compile(model.Snapshot{Flags: []model.SnapshotFlag{{Key: "new-checkout", Enabled: true, Version: 1}}})
	ctx := model.Context{UserID: "123", Attributes: map[string]any{"country": "BR", "plan": "premium"}}
	b.Run("enabled", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = snap.Evaluate("new-checkout", model.Context{})
		}
	})
	b.Run("with context", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = snap.Evaluate("new-checkout", ctx)
		}
	})
	b.Run("not found", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = snap.Evaluate("missing", ctx)
		}
	})
}

func BenchmarkBucket(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = Bucket("new-checkout", "salt", "user-123")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/logic/eval/`
Expected: FAIL, `undefined: Compile` / `undefined: Snapshot`.

- [ ] **Step 3: Implement** `internal/logic/eval/eval.go`

```go
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
	if len(f.Config.Rules) > 0 || serve.Split != nil {
		// Written by a newer admin plane, or invalid (value and split both set).
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
// not allocate. The algorithm is protocol/evaluation-spec.md section "Algorithm".
func (s *Snapshot) Evaluate(key string, evalCtx model.Context) model.Result {
	_ = evalCtx // used by targeting rules and splits in later milestones
	if s == nil {
		return model.Result{Reason: model.ReasonError, ErrorCode: model.ErrorCodeGeneral}
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
```

The check order is the spec's algorithm: duplicate before disabled (a corrupt key is an error
whatever its state), disabled before configuration errors (a switched-off flag is simply off).

- [ ] **Step 4: Run to verify it passes**

Run: `go test -count=1 ./internal/logic/eval/ ./internal/archtest/`
Expected: PASS, including `TestEvaluate_ZeroAllocations`.
Run: `go test -run '^$' -bench . -benchmem ./internal/logic/eval/`
Expected: every `BenchmarkEvaluate` sub-benchmark reports `0 allocs/op`.

- [ ] **Step 5: Commit**

```bash
git add internal/logic/eval/eval.go internal/logic/eval/eval_test.go
git commit -m "feat(eval): compile and evaluate snapshots with zero allocations" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Snapshot wire format and adapter

**Files:**
- Create: `internal/wire/out/snapshot.go`, `internal/adapter/api/snapshot.go`
- Test: `internal/adapter/api/snapshot_test.go`

**Interfaces:**
- Consumes: model snapshot and config types (Task 1).
- Produces: `out.SnapshotSchemaVersion = 1`; wire structs `out.Snapshot{SchemaVersion int
  "schema_version"; Environment string "environment"; Revision int64 "revision"; Flags
  []out.SnapshotFlag "flags"}`, `out.SnapshotFlag{Key, Enabled, Version, Config}`,
  `out.FlagConfig{Rules "rules"; Fallthrough "fallthrough"}`, `out.Rule{Conditions, Serve}`,
  `out.Condition{Attribute, Operator, Values}`, `out.Serve{Value *bool "value,omitempty"; Split
  *out.Split "split,omitempty"}`, `out.Split{Variations, BucketBy "bucket_by", Salt}`,
  `out.WeightedVariation{Value, Weight}`; `api.SnapshotFromWire(out.Snapshot) (model.Snapshot,
  error)` and `api.SnapshotToWire(model.Snapshot) out.Snapshot`.

- [ ] **Step 1: Write the failing test** `internal/adapter/api/snapshot_test.go`

```go
package api_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/joaomarcosfurtado/jollyroger/internal/adapter/api"
	"github.com/joaomarcosfurtado/jollyroger/internal/model"
	"github.com/joaomarcosfurtado/jollyroger/internal/wire/out"
)

func boolPtr(b bool) *bool { return &b }

func fullModel() model.Snapshot {
	return model.Snapshot{
		Environment: "production",
		Revision:    9,
		Flags: []model.SnapshotFlag{
			{Key: "plain", Enabled: true, Version: 1},
			{Key: "rich", Enabled: false, Version: 4, Config: model.FlagConfig{
				Rules: []model.Rule{{
					Conditions: []model.Condition{{Attribute: "country", Operator: "in", Values: []string{"BR", "PT"}}},
					Serve:      model.Serve{Value: boolPtr(true)},
				}},
				Fallthrough: model.Serve{Split: &model.Split{
					Variations: []model.WeightedVariation{{Value: true, Weight: 25000}, {Value: false, Weight: 75000}},
					BucketBy:   "user_id",
					Salt:       "s1",
				}},
			}},
		},
	}
}

func fullWire() out.Snapshot {
	return out.Snapshot{
		SchemaVersion: out.SnapshotSchemaVersion,
		Environment:   "production",
		Revision:      9,
		Flags: []out.SnapshotFlag{
			{Key: "plain", Enabled: true, Version: 1, Config: out.FlagConfig{Rules: []out.Rule{}}},
			{Key: "rich", Enabled: false, Version: 4, Config: out.FlagConfig{
				Rules: []out.Rule{{
					Conditions: []out.Condition{{Attribute: "country", Operator: "in", Values: []string{"BR", "PT"}}},
					Serve:      out.Serve{Value: boolPtr(true)},
				}},
				Fallthrough: out.Serve{Split: &out.Split{
					Variations: []out.WeightedVariation{{Value: true, Weight: 25000}, {Value: false, Weight: 75000}},
					BucketBy:   "user_id",
					Salt:       "s1",
				}},
			}},
		},
	}
}

func TestSnapshotToWire_MapsEveryField(t *testing.T) {
	t.Parallel()
	if got, want := api.SnapshotToWire(fullModel()), fullWire(); !reflect.DeepEqual(got, want) {
		t.Fatalf("SnapshotToWire mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSnapshotFromWire_MapsEveryField(t *testing.T) {
	t.Parallel()
	got, err := api.SnapshotFromWire(fullWire())
	if err != nil {
		t.Fatal(err)
	}
	if want := fullModel(); !reflect.DeepEqual(got, want) {
		t.Fatalf("SnapshotFromWire mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSnapshotToWire_EmitsEmptyArraysNotNull(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(api.SnapshotToWire(model.Snapshot{Environment: "e", Flags: []model.SnapshotFlag{{Key: "k"}}}))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"rules":[]`, `"schema_version":1`, `"fallthrough":{}`} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON %s does not contain %s", s, want)
		}
	}
	empty, _ := json.Marshal(api.SnapshotToWire(model.Snapshot{}))
	if !strings.Contains(string(empty), `"flags":[]`) {
		t.Errorf("empty snapshot JSON %s must contain \"flags\":[]", empty)
	}
}

func TestSnapshotFromWire_RejectsUnknownSchemaVersion(t *testing.T) {
	t.Parallel()
	for _, v := range []int{0, 2, -1} {
		w := fullWire()
		w.SchemaVersion = v
		if _, err := api.SnapshotFromWire(w); !errors.Is(err, model.ErrInvalid) {
			t.Errorf("schema_version %d: err = %v, want ErrInvalid", v, err)
		}
	}
}

func TestSnapshotFromWire_ToleratesUnknownFields(t *testing.T) {
	t.Parallel()
	raw := `{"schema_version":1,"environment":"production","revision":3,"future_top":true,
		"flags":[{"key":"f","enabled":true,"version":2,"future_flag":"x",
		"config":{"rules":[],"fallthrough":{"value":false},"future_config":1}}]}`
	var w out.Snapshot
	if err := json.Unmarshal([]byte(raw), &w); err != nil {
		t.Fatal(err)
	}
	got, err := api.SnapshotFromWire(w)
	if err != nil {
		t.Fatal(err)
	}
	want := model.Snapshot{Environment: "production", Revision: 3, Flags: []model.SnapshotFlag{
		{Key: "f", Enabled: true, Version: 2, Config: model.FlagConfig{Fallthrough: model.Serve{Value: boolPtr(false)}}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestSnapshotAdapters_DoNotAliasPointers(t *testing.T) {
	t.Parallel()
	m := fullModel()
	w := api.SnapshotToWire(m)
	*m.Flags[1].Config.Rules[0].Serve.Value = false
	m.Flags[1].Config.Fallthrough.Split.Variations[0].Weight = 1
	if !*w.Flags[1].Config.Rules[0].Serve.Value || w.Flags[1].Config.Fallthrough.Split.Variations[0].Weight != 25000 {
		t.Fatal("SnapshotToWire output shares memory with its input")
	}
	w2 := fullWire()
	back, err := api.SnapshotFromWire(w2)
	if err != nil {
		t.Fatal(err)
	}
	*w2.Flags[1].Config.Rules[0].Serve.Value = false
	w2.Flags[1].Config.Rules[0].Conditions[0].Values[0] = "XX"
	if !*back.Flags[1].Config.Rules[0].Serve.Value || back.Flags[1].Config.Rules[0].Conditions[0].Values[0] != "BR" {
		t.Fatal("SnapshotFromWire output shares memory with its input")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/adapter/api/`
Expected: FAIL, build error (packages `api` and `out` do not exist).

- [ ] **Step 3: Implement** `internal/wire/out/snapshot.go`

```go
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
```

`internal/adapter/api/snapshot.go`:

```go
// Package api translates between the JSON API / snapshot protocol shapes (internal/wire) and the
// domain model. It is the only place those shapes are mapped, and it is pure.
package api

import (
	"fmt"

	"github.com/joaomarcosfurtado/jollyroger/internal/model"
	"github.com/joaomarcosfurtado/jollyroger/internal/wire/out"
)

// SnapshotToWire converts a model snapshot to the protocol document. Collections are always
// non-nil so the JSON carries [] rather than null.
func SnapshotToWire(s model.Snapshot) out.Snapshot {
	flags := make([]out.SnapshotFlag, 0, len(s.Flags))
	for _, f := range s.Flags {
		flags = append(flags, out.SnapshotFlag{
			Key:     f.Key,
			Enabled: f.Enabled,
			Version: f.Version,
			Config:  configToWire(f.Config),
		})
	}
	return out.Snapshot{
		SchemaVersion: out.SnapshotSchemaVersion,
		Environment:   s.Environment,
		Revision:      s.Revision,
		Flags:         flags,
	}
}

// SnapshotFromWire converts a protocol document to a model snapshot. It rejects a document whose
// schema_version this build does not understand; unknown fields are ignored by the JSON decoder,
// so additive changes from newer writers are tolerated. Empty collections become nil.
func SnapshotFromWire(w out.Snapshot) (model.Snapshot, error) {
	if w.SchemaVersion != out.SnapshotSchemaVersion {
		return model.Snapshot{}, fmt.Errorf("snapshot schema_version %d (supported: %d): %w",
			w.SchemaVersion, out.SnapshotSchemaVersion, model.ErrInvalid)
	}
	var flags []model.SnapshotFlag
	for _, f := range w.Flags {
		flags = append(flags, model.SnapshotFlag{
			Key:     f.Key,
			Enabled: f.Enabled,
			Version: f.Version,
			Config:  configFromWire(f.Config),
		})
	}
	return model.Snapshot{Environment: w.Environment, Revision: w.Revision, Flags: flags}, nil
}

func configToWire(c model.FlagConfig) out.FlagConfig {
	rules := make([]out.Rule, 0, len(c.Rules))
	for _, r := range c.Rules {
		conds := make([]out.Condition, 0, len(r.Conditions))
		for _, cd := range r.Conditions {
			conds = append(conds, out.Condition{Attribute: cd.Attribute, Operator: cd.Operator, Values: append([]string{}, cd.Values...)})
		}
		rules = append(rules, out.Rule{Conditions: conds, Serve: serveToWire(r.Serve)})
	}
	return out.FlagConfig{Rules: rules, Fallthrough: serveToWire(c.Fallthrough)}
}

func serveToWire(s model.Serve) out.Serve {
	var w out.Serve
	if s.Value != nil {
		v := *s.Value
		w.Value = &v
	}
	if s.Split != nil {
		vars := make([]out.WeightedVariation, 0, len(s.Split.Variations))
		for _, v := range s.Split.Variations {
			vars = append(vars, out.WeightedVariation{Value: v.Value, Weight: v.Weight})
		}
		w.Split = &out.Split{Variations: vars, BucketBy: s.Split.BucketBy, Salt: s.Split.Salt}
	}
	return w
}

func configFromWire(w out.FlagConfig) model.FlagConfig {
	var rules []model.Rule
	for _, r := range w.Rules {
		var conds []model.Condition
		for _, cd := range r.Conditions {
			var values []string
			if len(cd.Values) > 0 {
				values = append([]string{}, cd.Values...)
			}
			conds = append(conds, model.Condition{Attribute: cd.Attribute, Operator: cd.Operator, Values: values})
		}
		rules = append(rules, model.Rule{Conditions: conds, Serve: serveFromWire(r.Serve)})
	}
	return model.FlagConfig{Rules: rules, Fallthrough: serveFromWire(w.Fallthrough)}
}

func serveFromWire(w out.Serve) model.Serve {
	var s model.Serve
	if w.Value != nil {
		v := *w.Value
		s.Value = &v
	}
	if w.Split != nil {
		var vars []model.WeightedVariation
		for _, v := range w.Split.Variations {
			vars = append(vars, model.WeightedVariation{Value: v.Value, Weight: v.Weight})
		}
		s.Split = &model.Split{Variations: vars, BucketBy: w.Split.BucketBy, Salt: w.Split.Salt}
	}
	return s
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -count=1 ./internal/adapter/api/ ./internal/archtest/`
Expected: PASS (archtest: `adapter` imports only `model` and `wire`; `wire` imports nothing
internal).

- [ ] **Step 5: Commit**

```bash
git add internal/wire/out internal/adapter/api
git commit -m "feat(api): snapshot protocol shape and adapter" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Protocol spec and cross-SDK vectors

**Files:**
- Create: `protocol/README.md`, `protocol/evaluation-spec.md`, `protocol/testdata/vectors.json`
- Test: `internal/logic/eval/vectors_test.go`

**Interfaces:**
- Consumes: `eval.Compile`, `(*eval.Snapshot).Evaluate`, `eval.Bucket` (Tasks 3, 4);
  `api.SnapshotFromWire`, `out.Snapshot` (Task 5).
- Produces: the vectors file format every SDK consumes (documented in `evaluation-spec.md`).

- [ ] **Step 1: Write the vectors file** `protocol/testdata/vectors.json`

```json
{
  "vectors_version": 1,
  "evaluation_cases": [
    {
      "name": "enabled flag with empty config serves true",
      "snapshot": {"schema_version": 1, "environment": "production", "revision": 1,
        "flags": [{"key": "new-checkout", "enabled": true, "version": 3, "config": {"rules": [], "fallthrough": {}}}]},
      "flag": "new-checkout",
      "context": {"user_id": "", "attributes": {}},
      "expected": {"value": true, "reason": "STATIC", "error_code": "", "flag_version": 3}
    },
    {
      "name": "fixed fallthrough false",
      "snapshot": {"schema_version": 1, "environment": "production", "revision": 1,
        "flags": [{"key": "dark-mode", "enabled": true, "version": 2, "config": {"rules": [], "fallthrough": {"value": false}}}]},
      "flag": "dark-mode",
      "context": {"user_id": "u1", "attributes": {}},
      "expected": {"value": false, "reason": "STATIC", "error_code": "", "flag_version": 2}
    },
    {
      "name": "disabled flag serves false",
      "snapshot": {"schema_version": 1, "environment": "production", "revision": 1,
        "flags": [{"key": "new-checkout", "enabled": false, "version": 5, "config": {"rules": [], "fallthrough": {"value": true}}}]},
      "flag": "new-checkout",
      "context": {"user_id": "u1", "attributes": {"plan": "premium"}},
      "expected": {"value": false, "reason": "DISABLED", "error_code": "", "flag_version": 5}
    },
    {
      "name": "missing flag",
      "snapshot": {"schema_version": 1, "environment": "production", "revision": 1,
        "flags": [{"key": "other", "enabled": true, "version": 1, "config": {"rules": [], "fallthrough": {}}}]},
      "flag": "new-checkout",
      "context": {"user_id": "", "attributes": {}},
      "expected": {"value": false, "reason": "ERROR", "error_code": "FLAG_NOT_FOUND", "flag_version": 0}
    },
    {
      "name": "empty snapshot",
      "snapshot": {"schema_version": 1, "environment": "production", "revision": 0, "flags": []},
      "flag": "new-checkout",
      "context": {"user_id": "", "attributes": {}},
      "expected": {"value": false, "reason": "ERROR", "error_code": "FLAG_NOT_FOUND", "flag_version": 0}
    },
    {
      "name": "keys are case-sensitive",
      "snapshot": {"schema_version": 1, "environment": "production", "revision": 1,
        "flags": [{"key": "new-checkout", "enabled": true, "version": 1, "config": {"rules": [], "fallthrough": {}}}]},
      "flag": "New-Checkout",
      "context": {"user_id": "", "attributes": {}},
      "expected": {"value": false, "reason": "ERROR", "error_code": "FLAG_NOT_FOUND", "flag_version": 0}
    },
    {
      "name": "static flag ignores context",
      "snapshot": {"schema_version": 1, "environment": "production", "revision": 1,
        "flags": [{"key": "new-checkout", "enabled": true, "version": 1, "config": {"rules": [], "fallthrough": {"value": true}}}]},
      "flag": "new-checkout",
      "context": {"user_id": "123", "attributes": {"country": "BR", "plan": "premium", "age": 42}},
      "expected": {"value": true, "reason": "STATIC", "error_code": "", "flag_version": 1}
    },
    {
      "name": "value and split both set is invalid",
      "snapshot": {"schema_version": 1, "environment": "production", "revision": 1,
        "flags": [{"key": "broken", "enabled": true, "version": 7, "config": {"rules": [],
          "fallthrough": {"value": true, "split": {"variations": [{"value": true, "weight": 100000}], "bucket_by": "user_id", "salt": ""}}}}]},
      "flag": "broken",
      "context": {"user_id": "u1", "attributes": {}},
      "expected": {"value": false, "reason": "ERROR", "error_code": "PARSE_ERROR", "flag_version": 7}
    },
    {
      "name": "disabled wins over an invalid config",
      "snapshot": {"schema_version": 1, "environment": "production", "revision": 1,
        "flags": [{"key": "broken-off", "enabled": false, "version": 8, "config": {"rules": [],
          "fallthrough": {"value": true, "split": {"variations": [{"value": true, "weight": 100000}], "bucket_by": "user_id", "salt": ""}}}}]},
      "flag": "broken-off",
      "context": {"user_id": "u1", "attributes": {}},
      "expected": {"value": false, "reason": "DISABLED", "error_code": "", "flag_version": 8}
    },
    {
      "name": "one invalid flag does not affect another",
      "snapshot": {"schema_version": 1, "environment": "production", "revision": 1,
        "flags": [
          {"key": "broken", "enabled": true, "version": 1, "config": {"rules": [],
            "fallthrough": {"value": true, "split": {"variations": [], "bucket_by": "user_id", "salt": ""}}}},
          {"key": "healthy", "enabled": true, "version": 2, "config": {"rules": [], "fallthrough": {}}}
        ]},
      "flag": "healthy",
      "context": {"user_id": "", "attributes": {}},
      "expected": {"value": true, "reason": "STATIC", "error_code": "", "flag_version": 2}
    }
  ],
  "bucket_cases": [
    {"flag_key": "new-checkout", "salt": "", "value": "user-1", "expected": 83561},
    {"flag_key": "new-checkout", "salt": "s1", "value": "user-1", "expected": 16082},
    {"flag_key": "new-checkout", "salt": "", "value": "user-2", "expected": 41251},
    {"flag_key": "a", "salt": "b", "value": "c", "expected": 65252},
    {"flag_key": "flag.with-dots_1", "salt": "salt", "value": "ümlaut-user", "expected": 77465},
    {"flag_key": "x", "salt": "", "value": "", "expected": 30962}
  ]
}
```

- [ ] **Step 2: Write the failing test** `internal/logic/eval/vectors_test.go`

```go
package eval_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/joaomarcosfurtado/jollyroger/internal/adapter/api"
	"github.com/joaomarcosfurtado/jollyroger/internal/logic/eval"
	"github.com/joaomarcosfurtado/jollyroger/internal/model"
	"github.com/joaomarcosfurtado/jollyroger/internal/wire/out"
)

type vectorsFile struct {
	VectorsVersion  int `json:"vectors_version"`
	EvaluationCases []struct {
		Name     string       `json:"name"`
		Snapshot out.Snapshot `json:"snapshot"`
		Flag     string       `json:"flag"`
		Context  struct {
			UserID     string         `json:"user_id"`
			Attributes map[string]any `json:"attributes"`
		} `json:"context"`
		Expected struct {
			Value       bool   `json:"value"`
			Reason      string `json:"reason"`
			ErrorCode   string `json:"error_code"`
			FlagVersion int64  `json:"flag_version"`
		} `json:"expected"`
	} `json:"evaluation_cases"`
	BucketCases []struct {
		FlagKey  string `json:"flag_key"`
		Salt     string `json:"salt"`
		Value    string `json:"value"`
		Expected uint32 `json:"expected"`
	} `json:"bucket_cases"`
}

// TestProtocolVectors runs protocol/testdata/vectors.json, the file every SDK must pass.
func TestProtocolVectors(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../protocol/testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectorsFile
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode vectors: %v", err)
	}
	if v.VectorsVersion != 1 || len(v.EvaluationCases) == 0 || len(v.BucketCases) == 0 {
		t.Fatalf("unexpected vectors file: version %d, %d evaluation cases, %d bucket cases",
			v.VectorsVersion, len(v.EvaluationCases), len(v.BucketCases))
	}
	for _, tc := range v.EvaluationCases {
		t.Run(tc.Name, func(t *testing.T) {
			snap, err := api.SnapshotFromWire(tc.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			got := eval.Compile(snap).Evaluate(tc.Flag, model.Context{UserID: tc.Context.UserID, Attributes: tc.Context.Attributes})
			want := model.Result{
				Value:       tc.Expected.Value,
				Reason:      model.Reason(tc.Expected.Reason),
				ErrorCode:   model.ErrorCode(tc.Expected.ErrorCode),
				FlagVersion: tc.Expected.FlagVersion,
			}
			if got != want {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		})
	}
	for _, bc := range v.BucketCases {
		if got := eval.Bucket(bc.FlagKey, bc.Salt, bc.Value); got != bc.Expected {
			t.Errorf("Bucket(%q, %q, %q) = %d, want %d", bc.FlagKey, bc.Salt, bc.Value, got, bc.Expected)
		}
	}
}
```

- [ ] **Step 3: Run to verify**

Run: `go test -count=1 -run TestProtocolVectors -v ./internal/logic/eval/`
Expected: PASS for all 10 evaluation cases (the engine already exists from Tasks 3 to 5). To prove
the test bites, temporarily change `"expected": 83561` to `83562` in the vectors file, run again,
Expected: FAIL naming `Bucket("new-checkout", "", "user-1")`; then revert the edit.

- [ ] **Step 4: Write** `protocol/README.md`

```markdown
# jollyroger protocol

Normative documents for anyone implementing a jollyroger SDK or client. The Go implementation in
this repository is tested against every file here; an SDK in another language must be too.

| Document | Contract |
|---|---|
| [evaluation-spec.md](evaluation-spec.md) | the snapshot format, the evaluation algorithm, and bucketing |
| [testdata/vectors.json](testdata/vectors.json) | cases every SDK must pass, byte for byte |

Planned (later milestones): `openapi.yaml` (HTTP API v1), `storage-contract.md` (the tables an SDK
may read), `metrics.md` (shared metric names).

Versioning: each document carries a version. Within a version, changes are additive only.
```

`protocol/evaluation-spec.md`:

````markdown
# Evaluation specification (version 1)

This document defines how a jollyroger SDK evaluates a flag. The key words MUST, MUST NOT and
SHOULD are used as in RFC 2119. Every SDK MUST pass `testdata/vectors.json`.

## 1. Snapshot document

A snapshot is the complete evaluation data for one environment at one revision:

```json
{
  "schema_version": 1,
  "environment": "production",
  "revision": 42,
  "flags": [
    {"key": "new-checkout", "enabled": true, "version": 3,
     "config": {"rules": [], "fallthrough": {"value": true}}}
  ]
}
```

- A reader MUST reject a document whose `schema_version` it does not support.
- A reader MUST ignore unknown fields (newer writers may add fields within a schema version).
- Archived flags are never present in a snapshot.
- `fallthrough` is a Serve object: `{"value": bool}`, `{"split": Split}`, or `{}` (serve `true`).
  Setting both `value` and `split` is invalid.
- `Split` is `{"variations": [{"value": bool, "weight": int}], "bucket_by": "user_id" |
  "attr:<name>", "salt": string}`, with weights in units of 0.001% summing to 100000.

## 2. Algorithm

Given a snapshot, a flag key, and a context `{user_id, attributes}`:

1. If the snapshot is not loaded: `value=false`, `reason=ERROR`, `error_code=GENERAL`.
2. If the key appears more than once in the snapshot: `value=false`, `reason=ERROR`,
   `error_code=PARSE_ERROR`.
3. If the key is absent (keys are case-sensitive): `value=false`, `reason=ERROR`,
   `error_code=FLAG_NOT_FOUND`, `flag_version=0`.
4. If `enabled` is false: `value=false`, `reason=DISABLED`, whatever the configuration.
5. If the configuration is invalid, or uses a feature this SDK does not implement:
   `value=false`, `reason=ERROR`, `error_code=PARSE_ERROR`. Other flags MUST be unaffected.
6. Rules, in order (version 1 SDKs implement none and apply step 5 to any rule).
7. Otherwise serve `fallthrough`: a fixed value gives `reason=STATIC`; `{}` gives `value=true`,
   `reason=STATIC`.

Every result carries the flag's `version` as `flag_version` (0 when the flag is absent or its
key is duplicated). An SDK
MUST NOT throw or panic on evaluation, and SHOULD NOT allocate or block.

## 3. Bucketing

For percentage splits (specified now, evaluated from a later version):

```
bucket = uint32_big_endian(SHA-256(utf8(flag_key + "." + salt + "." + value))[0:4]) mod 100000
```

`value` is the context's `user_id` when `bucket_by` is `user_id`, or the string form of attribute
`<name>` for `attr:<name>`. A split whose bucketing value is empty or missing serves its first
variation with `reason=DEFAULT`; it is never random. `bucket_cases` in the vectors pin this
function.

## 4. Vectors file

`testdata/vectors.json` has `vectors_version`, `evaluation_cases` (each a snapshot document, a
`flag`, a `context`, and `expected` `{value, reason, error_code, flag_version}`, where an empty
`error_code` means none), and `bucket_cases` (`flag_key`, `salt`, `value`, `expected`).
````

- [ ] **Step 5: Run the package and archtest again, then commit**

Run: `go test -count=1 ./internal/logic/eval/ ./internal/archtest/`
Expected: PASS.

```bash
git add protocol internal/logic/eval/vectors_test.go
git commit -m "feat(protocol): evaluation spec and cross-SDK test vectors" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: CI benchmark smoke, docs, full gate, PR

**Files:**
- Modify: `.github/workflows/ci.yml` (test job), `CHANGELOG.md`, `docs/backlog.md`,
  `CLAUDE.md`, `docs/skills/diplomat-architecture.md`, `docs/skills/api-security.md`,
  `docs/superpowers/specs/2026-09-26-jollyroger-design.md`, `internal/archtest/archtest_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces: an open PR `feat/m1-core` -> `main`.

- [ ] **Step 1: Add a benchmark smoke step** to the `test` job in `.github/workflows/ci.yml`,
  after "integration tests":

```yaml
      - name: benchmarks (smoke)
        run: go test -run '^$' -bench . -benchtime 1000x ./...
```

- [ ] **Step 2: Rename `adapter/http` to `adapter/api` in docs** and record the spec refinements.
  Run from the worktree root:

```bash
sed -i 's#internal/adapter/{http,db}#internal/adapter/{api,db}#' CLAUDE.md docs/superpowers/specs/2026-09-26-jollyroger-design.md
sed -i 's#adapter/http#adapter/api#g' docs/skills/diplomat-architecture.md docs/skills/api-security.md docs/superpowers/specs/2026-09-26-jollyroger-design.md internal/archtest/archtest_test.go
sed -i 's#`logic/{eval,snapshot,validate,pagination}`#`logic/{eval,validate,pagination}`#; s#`model`, `logic/eval`, `logic/snapshot`, `logic/validate`#`model`, `logic/eval`, `logic/validate`#' docs/superpowers/specs/2026-09-26-jollyroger-design.md
grep -rn "adapter/http\|logic/snapshot" --include=*.md --include=*.go --exclude-dir=plans . || echo "no stale references"
```

Expected: `no stale references`. Then add to the spec, at the end of section 2, this paragraph:

```markdown
Refinements made while planning M1: compiling and evaluating a snapshot are one concern, so
`logic/snapshot` is part of `logic/eval`; and the HTTP translation package is `adapter/api`,
because a package named `http` would shadow `net/http` wherever both are used.
```

- [ ] **Step 3: Update CHANGELOG and backlog.** In `CHANGELOG.md` under `## [Unreleased]` /
  `### Added`, insert as the first bullets:

```markdown
- Evaluation engine: compiled, immutable snapshots evaluated with zero allocations; deterministic
  SHA-256 bucketing.
- Protocol: `protocol/evaluation-spec.md` and `protocol/testdata/vectors.json`, the cases every
  SDK must pass.
- Input validation for flag keys, environment keys, names, descriptions, reasons and configs.
```

In `docs/backlog.md`, replace the first bullet with
`- Write the M2 (storage) implementation plan.` and change the second to
`- Milestones M2 to M8 as listed in the README roadmap.`

- [ ] **Step 4: Run the full gate**

```bash
gofmt -l .
go build ./... && go vet ./...
go test -count=1 ./...
go test -count=1 -tags integration ./...
go test -run '^$' -bench . -benchmem ./internal/logic/eval/
golangci-lint run ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
git grep -n $'\xe2\x80\x94'
```

Expected: `gofmt` prints nothing; all tests PASS; every `BenchmarkEvaluate` line shows
`0 allocs/op`; golangci-lint `0 issues.`; govulncheck `No vulnerabilities found.`; the grep
prints nothing. (The race detector needs cgo on Windows; it runs in CI.)

- [ ] **Step 5: Review before commit.** Run `/code-review` on the branch diff and walk
  `docs/skills/review-checklist.md` and `docs/skills/security-review.md`. Resolve or dismiss (with
  a written reason) every finding.

- [ ] **Step 6: Commit, push, open the PR**

```bash
git add -A
git commit -m "docs: M1 refinements, changelog, benchmark smoke in CI" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push
gh pr create --base main --title "feat: M1 core evaluation engine" --body "Implements docs/superpowers/plans/2026-09-26-m1-core-evaluation.md: domain model, validation, zero-allocation evaluation engine, deterministic bucketing, snapshot protocol adapter, and the cross-SDK vectors.

🤖 Generated with [Claude Code](https://claude.com/claude-code)"
gh pr checks --watch
```

Expected: all four checks pass.
