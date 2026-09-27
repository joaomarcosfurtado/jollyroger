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
