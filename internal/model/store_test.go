package model_test

import (
	"testing"

	"github.com/joaomarcosfurtado/jollyroger/internal/model"
)

func TestEffectiveLimit(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want int }{
		{0, model.DefaultListLimit},
		{-5, model.DefaultListLimit},
		{1, 1},
		{model.MaxListLimit, model.MaxListLimit},
		{model.MaxListLimit + 1, model.MaxListLimit},
	}
	for _, tc := range cases {
		if got := (model.FlagQuery{Limit: tc.in}).EffectiveLimit(); got != tc.want {
			t.Errorf("FlagQuery{Limit: %d}.EffectiveLimit() = %d, want %d", tc.in, got, tc.want)
		}
		if got := (model.AuditQuery{Limit: tc.in}).EffectiveLimit(); got != tc.want {
			t.Errorf("AuditQuery{Limit: %d}.EffectiveLimit() = %d, want %d", tc.in, got, tc.want)
		}
	}
}
