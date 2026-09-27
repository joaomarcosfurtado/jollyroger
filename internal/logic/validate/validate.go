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
