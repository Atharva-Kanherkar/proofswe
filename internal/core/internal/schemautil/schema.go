package schemautil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/invopop/jsonschema"
)

func Bytes() ([]byte, error) {
	schema := Schema()
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func Schema() *jsonschema.Schema {
	reflector := &jsonschema.Reflector{
		Anonymous:                 true,
		DoNotReference:            false,
		AllowAdditionalProperties: true,
	}

	root := &jsonschema.Schema{
		Version:     jsonschema.Version,
		ID:          "https://proofswe.dev/schema/normalized-event.v1.json",
		Title:       "proofswe NormalizedEvent v1",
		Description: "Stable normalized event contract emitted by proofswe source adapters.",
		OneOf: []*jsonschema.Schema{
			{Ref: "#/$defs/SessionStart"},
			{Ref: "#/$defs/UserPrompt"},
			{Ref: "#/$defs/AssistantMessage"},
			{Ref: "#/$defs/ToolCall"},
			{Ref: "#/$defs/ToolResult"},
			{Ref: "#/$defs/SessionEnd"},
			{Ref: "#/$defs/Unknown"},
		},
		Definitions: jsonschema.Definitions{},
	}

	reflectInto(root, reflector, &core.SessionStart{}, "SessionStart", core.EventTypeSessionStart)
	reflectInto(root, reflector, &core.UserPrompt{}, "UserPrompt", core.EventTypeUserPrompt)
	reflectInto(root, reflector, &core.AssistantMessage{}, "AssistantMessage", core.EventTypeAssistantMessage)
	reflectInto(root, reflector, &core.ToolCall{}, "ToolCall", core.EventTypeToolCall)
	reflectInto(root, reflector, &core.ToolResult{}, "ToolResult", core.EventTypeToolResult)
	reflectInto(root, reflector, &core.SessionEnd{}, "SessionEnd", core.EventTypeSessionEnd)
	reflectInto(root, reflector, &core.Unknown{}, "Unknown", "")

	return root
}

func CheckGolden(path string) error {
	want, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	got, err := Bytes()
	if err != nil {
		return err
	}
	if !bytes.Equal(normalizeNewlines(want), normalizeNewlines(got)) {
		return fmt.Errorf("%s is stale; run go generate ./... before committing", path)
	}
	return nil
}

func normalizeNewlines(data []byte) []byte {
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
}

func reflectInto(root *jsonschema.Schema, reflector *jsonschema.Reflector, value any, name string, eventType core.EventType) {
	reflected := reflector.Reflect(value)
	for defName, def := range reflected.Definitions {
		root.Definitions[defName] = def
	}

	def := root.Definitions[name]
	if def == nil || def.Properties == nil {
		return
	}

	if prop, ok := def.Properties.Get("schema_version"); ok {
		prop.Const = core.SchemaVersion
		prop.Enum = nil
	}
	if eventType != "" {
		if prop, ok := def.Properties.Get("type"); ok {
			prop.Const = string(eventType)
			prop.Enum = nil
		}
	}
}
