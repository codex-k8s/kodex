package integrationpackage

import (
	"strings"
	"testing"
)

// TestFieldDisplayMetadataUsesUnicodeBounds verifies multi-byte labels/help at the JSON Schema boundary while retaining malformed UTF-8 and single-line rejection.
// TestFieldDisplayMetadataUsesUnicodeBounds проверяет multi-byte label/help на границе JSON Schema, сохраняя отказ для неверного UTF-8 и нарушения single-line ограничения.
func TestFieldDisplayMetadataUsesUnicodeBounds(t *testing.T) {
	if validateStringValue(Field{Key: "value", Type: "STRING", Format: "PLAIN", MaximumLength: 4}, "ЯЯЯ", false) == nil {
		t.Fatal("display Unicode handling widened an execution payload byte budget")
	}
	for _, character := range []string{"Я", "🙂"} {
		field := Field{Key: "value", Type: "STRING", Format: "PLAIN", MaximumLength: 32, Label: strings.Repeat(character, 120), Help: strings.Repeat(character, 500)}
		if _, err := validateFields([]Field{field}); err != nil {
			t.Fatal("schema-valid Unicode display metadata rejected", err)
		}
		for _, key := range []string{"label", "help"} {
			invalid := field
			if key == "label" {
				invalid.Label += character
			} else {
				invalid.Help += character
			}
			if _, err := validateFields([]Field{invalid}); err == nil {
				t.Fatal("display metadata exceeded Unicode boundary", key)
			}
		}
	}
	for _, value := range []string{string([]byte{0xff}), "bad\x00value", "bad\rvalue", "bad\nvalue"} {
		for _, key := range []string{"label", "help"} {
			field := Field{Key: "value", Type: "STRING", Format: "PLAIN", MaximumLength: 32}
			if key == "label" {
				field.Label = value
			} else {
				field.Help = value
			}
			if _, err := validateFields([]Field{field}); err == nil {
				t.Fatal("invalid display metadata accepted", key)
			}
		}
	}
}

// TestPackageDisplayMetadataUsesUnicodeBounds covers analogous package/capability captions without widening execution keys, approvals or input/output byte budgets.
// TestPackageDisplayMetadataUsesUnicodeBounds проверяет аналогичные package/capability captions без расширения execution keys, approvals либо input/output byte budgets.
func TestPackageDisplayMetadataUsesUnicodeBounds(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	baseline := definitions["synthetic"]
	for name, setter := range map[string]func(*Package, string){
		"package-name":           func(value *Package, text string) { value.Spec.Name = text },
		"package-description":    func(value *Package, text string) { value.Spec.Description = text },
		"capability-name":        func(value *Package, text string) { value.Spec.Capabilities[0].Name = text },
		"capability-description": func(value *Package, text string) { value.Spec.Capabilities[0].Description = text },
	} {
		maximum := 120
		if strings.HasSuffix(name, "description") {
			maximum = 500
		}
		value := baseline
		value.Spec.Capabilities = append([]Capability{}, baseline.Spec.Capabilities...)
		setter(&value, strings.Repeat("Я", maximum))
		if err := validate(&value); err != nil {
			t.Fatal("schema-valid package Unicode metadata rejected", name, err)
		}
		for _, invalid := range []string{strings.Repeat("Я", maximum+1), string([]byte{0xff}), ""} {
			setter(&value, invalid)
			if validate(&value) == nil {
				t.Fatal("invalid package display metadata accepted", name)
			}
		}
	}
}

// TestApprovalScopeCaptionUsesUnicodeBounds preserves exact scope identity and numeric limits while treating its non-executable caption as bounded Unicode text.
// TestApprovalScopeCaptionUsesUnicodeBounds сохраняет exact scope identity и numeric limits, считая неисполняемый caption ограниченным Unicode text.
func TestApprovalScopeCaptionUsesUnicodeBounds(t *testing.T) {
	capability := Capability{ApprovalScope: &ApprovalScopeContract{Name: strings.Repeat("Я", 128), RequiredInputPaths: []string{}}}
	if err := capability.validateApprovalScopeContract(); err != nil {
		t.Fatal("schema-valid Unicode scope caption rejected", err)
	}
	for _, invalid := range []string{strings.Repeat("Я", 129), string([]byte{0xff}), "", "bad\x00value", "bad\rvalue", "bad\nvalue"} {
		capability.ApprovalScope.Name = invalid
		if capability.validateApprovalScopeContract() == nil {
			t.Fatal("invalid approval scope caption accepted")
		}
	}
}
