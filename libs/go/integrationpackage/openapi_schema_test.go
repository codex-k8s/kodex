package integrationpackage

import "testing"

func TestValidateOpenAPIInputSchema(t *testing.T) {
	schema := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"path": map[string]any{"type": "object", "additionalProperties": false,
				"properties": map[string]any{"id": map[string]any{"type": "string", "minLength": 1}}, "required": []any{"id"}},
			"body": map[string]any{"type": "object", "additionalProperties": false,
				"properties": map[string]any{"count": map[string]any{"type": "integer", "minimum": 0}}},
		}, "required": []any{"path"},
	}
	_, compiled, err := validateOpenAPIInputSchema(schema)
	if err != nil {
		t.Fatalf("bounded schema rejected: %v", err)
	}
	for name, raw := range map[string]string{
		"valid":      `{"path":{"id":"3"},"body":{"count":7}}`,
		"unknown":    `{"path":{"id":"3"},"extra":true}`,
		"wrong type": `{"path":{"id":"3"},"body":{"count":"7"}}`,
		"missing":    `{"body":{"count":7}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := validateOpenAPIInput([]byte(raw), compiled)
			if (err == nil) != (name == "valid") {
				t.Fatalf("unexpected input validation outcome: %v", err)
			}
		})
	}
}

func TestValidateOpenAPIInputSchemaRejectsExternalReferences(t *testing.T) {
	for name, schema := range map[string]map[string]any{
		"external ref": {"type": "object", "additionalProperties": false, "properties": map[string]any{"x": map[string]any{"$ref": "https://example.invalid/schema"}}},
		"dynamic ref":  {"type": "object", "additionalProperties": false, "properties": map[string]any{"x": map[string]any{"$dynamicRef": "#foo"}}},
		"open root":    {"type": "object", "properties": map[string]any{}},
		"open nested body": {"type": "object", "additionalProperties": false,
			"properties": map[string]any{"body": map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}}}}},
		"dynamic nested body": {"type": "object", "additionalProperties": false,
			"properties": map[string]any{"body": map[string]any{"type": "object", "additionalProperties": false,
				"patternProperties": map[string]any{".*": map[string]any{"type": "string"}}}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := validateOpenAPIInputSchema(schema); err == nil {
				t.Fatal("unbounded schema accepted")
			}
		})
	}
}
