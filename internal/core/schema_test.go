package core_test

import (
	"testing"

	"github.com/Atharva-Kanherkar/proofswe/internal/core/internal/schemautil"
	"github.com/invopop/jsonschema"
)

func TestNormalizedEventSchemaGolden(t *testing.T) {
	if err := schemautil.CheckGolden("../../schema/normalized-event.v1.json"); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizedEventSchemaAllowsAdditionalProperties(t *testing.T) {
	schema := schemautil.Schema()

	for name, definition := range schema.Definitions {
		if definition.AdditionalProperties != nil && definition.AdditionalProperties != jsonschema.TrueSchema {
			t.Fatalf("%s disallows additional properties", name)
		}
	}
}
