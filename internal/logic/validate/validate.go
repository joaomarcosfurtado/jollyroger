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

// FlagName validates a display name: required (at least one visible character), single line, at
// most MaxNameLength characters.
func FlagName(name string) error {
	if err := text("name", name, MaxNameLength, false); err != nil {
		return err
	}
	if !hasVisibleRune(name) {
		return invalid("name", "is required")
	}
	return nil
}

// Description validates an optional, possibly multi-line description.
func Description(d string) error {
	return text("description", d, MaxDescriptionLength, true)
}

// ChangeReason validates the optional reason an operator gives for a change.
func ChangeReason(r string) error {
	return text("reason", r, MaxReasonLength, true)
}

// text checks encoding, length, and characters that change how text is displayed. Control
// characters, bidirectional formatting characters, and line/paragraph separators are rejected
// because they enable terminal-escape, log-injection, and spoofing tricks when an audit log or
// the dashboard shows the text. Multi-line fields accept '\n', '\t', and '\r' only as part of a
// "\r\n" pair (a lone '\r' rewrites the printed line).
func text(field, s string, maxLen int, multiline bool) error {
	if !utf8.ValidString(s) {
		return invalid(field, "must be valid UTF-8")
	}
	if utf8.RuneCountInString(s) > maxLen {
		return invalid(field, "is too long")
	}
	for i, r := range s {
		switch {
		case r == '\n' || r == '\t':
			if !multiline {
				return invalid(field, "must be a single line")
			}
		case r == '\r':
			if !multiline || !strings.HasPrefix(s[i+1:], "\n") {
				return invalid(field, "must not contain a carriage return outside a line break")
			}
		case unicode.IsControl(r) || isBidiControl(r) || unicode.In(r, unicode.Zl, unicode.Zp):
			return invalid(field, "must not contain control or text-direction characters")
		}
	}
	return nil
}

// isBidiControl reports whether r is a Unicode bidirectional formatting character (the embedding,
// override, isolate, and mark characters), which can make displayed text differ from stored text.
func isBidiControl(r rune) bool {
	switch {
	case r >= 0x202A && r <= 0x202E, // LRE, RLE, PDF, LRO, RLO
		r >= 0x2066 && r <= 0x2069, // LRI, RLI, FSI, PDI
		r == 0x200E, r == 0x200F, r == 0x061C: // LRM, RLM, ALM
		return true
	default:
		return false
	}
}

// hasVisibleRune reports whether s contains at least one character that renders as something:
// not whitespace, not an invisible format character (such as U+200B), and not a Hangul filler.
func hasVisibleRune(s string) bool {
	for _, r := range s {
		switch {
		case unicode.IsSpace(r), unicode.Is(unicode.Cf, r):
		case r == 0x115F, r == 0x1160, r == 0x3164, r == 0xFFA0: // Hangul fillers render blank
		default:
			return true
		}
	}
	return false
}

// FlagConfig validates a configuration against what this version of the engine supports. v0.1
// supports no targeting rules and only a fixed fallthrough value.
func FlagConfig(c model.FlagConfig) error {
	if c.Unparseable {
		return invalid("config", "could not be parsed")
	}
	if len(c.Rules) > 0 {
		return invalid("rules", "targeting rules are not supported yet")
	}
	if c.Fallthrough.Split != nil {
		return invalid("fallthrough", "percentage splits are not supported yet")
	}
	return nil
}
