package postgres_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/joaomarcosfurtado/jollyroger/internal/diplomat/postgres"
	"github.com/joaomarcosfurtado/jollyroger/internal/model"
)

func TestNew_ValidatesSchema(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"", "public", "jollyroger", "_flags", "app_2"} {
		if _, err := postgres.New(nil, ok); err != nil {
			t.Errorf("New(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"Public", "1app", "app-flags", `x"; DROP SCHEMA public; --`, "a.b", strings.Repeat("a", 64)} {
		if _, err := postgres.New(nil, bad); !errors.Is(err, model.ErrInvalid) {
			t.Errorf("New(%q) = %v, want ErrInvalid", bad, err)
		}
	}
}
