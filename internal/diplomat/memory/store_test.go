package memory_test

import (
	"testing"

	"github.com/joaomarcosfurtado/jollyroger/internal/diplomat/memory"
	"github.com/joaomarcosfurtado/jollyroger/internal/model"
	"github.com/joaomarcosfurtado/jollyroger/internal/storetest"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, func(*testing.T) model.FlagStore { return memory.New() })
}
