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
