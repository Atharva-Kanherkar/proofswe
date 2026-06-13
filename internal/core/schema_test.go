package core_test

import (
	"testing"

	"github.com/Atharva-Kanherkar/proofswe/internal/core/internal/schemautil"
)

func TestNormalizedEventSchemaGolden(t *testing.T) {
	if err := schemautil.CheckGolden("../../schema/normalized-event.v1.json"); err != nil {
		t.Fatal(err)
	}
}
