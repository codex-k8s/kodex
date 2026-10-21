package integrationpackage

import (
	"strings"
	"testing"
)

// TestExplicitEmptyPayloadKeepsConfigurationClosed permits explicit empty plain payloads but not
// empty connection identities or empty strings for formats that require an address or identifier.
// TestExplicitEmptyPayloadKeepsConfigurationClosed допускает явно пустой простой payload, но не
// пустые координаты подключения или форматы, которым нужен адрес либо идентификатор.
func TestExplicitEmptyPayloadKeepsConfigurationClosed(t *testing.T) {
	field := Field{Key: "content", Type: "STRING", Format: "PLAIN", Required: true, MaximumLength: 32, AllowEmpty: true}
	capability := Capability{InputFields: []Field{field}, OutputFields: []Field{field}}
	if _, err := capability.ValidateInput([]byte(`{"content":""}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := capability.ValidateOutput([]byte(`{"content":""}`)); err != nil {
		t.Fatal(err)
	}
	schema, err := capability.InputSchema()
	if err != nil || !strings.Contains(string(schema), `"minLength":0`) {
		t.Fatalf("empty schema: %s %v", schema, err)
	}
	field.AllowEmpty = false
	capability.InputFields[0] = field
	if _, err := capability.ValidateInput([]byte(`{"content":""}`)); err == nil {
		t.Fatal("implicit empty input accepted")
	}
	field.AllowEmpty = true
	if validateStringValue(field, "", false) == nil {
		t.Fatal("empty configuration accepted")
	}
	for _, format := range []string{"IDENTIFIER", "HTTPS_URL", "EMAIL", "HOST"} {
		field.Format = format
		if _, err := validateFields([]Field{field}); err == nil {
			t.Fatal("empty identity format accepted")
		}
	}
}
