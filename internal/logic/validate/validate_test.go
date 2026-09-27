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
