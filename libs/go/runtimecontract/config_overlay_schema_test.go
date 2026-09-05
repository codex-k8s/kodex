package runtimecontract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOverlayDiagnosticsPreservePositionsWithoutValues(t *testing.T) {
	for name, test := range map[string]struct {
		raw, code, key string
		line           int32
	}{
		"syntax":    {"personality = [\n", OverlaySyntaxInvalid, "", 1},
		"protected": {"\nmodel = \"private-value\"", OverlayKeyForbidden, "model", 2},
		"unknown":   {"\"private-key\" = \"private-value\"", OverlayKeyForbidden, "", 1},
		"quoted":    {"\"personality\" = \"private-value\"", OverlayValueInvalid, "personality", 1},
		"dotted":    {"history.persistence = \"private-value\"", OverlayValueInvalid, "history.persistence", 1},
		"table":     {"[history]\npersistence = \"private-value\"", OverlayValueInvalid, "history.persistence", 2},
		"inline":    {"history = { persistence = \"private-value\" }", OverlayValueInvalid, "history.persistence", 1},
		"type":      {"personality = 42", OverlayValueInvalid, "personality", 1},
		"effort":    {"model_reasoning_effort = \"max\"", OverlayEffortUnsupported, "model_reasoning_effort", 1},
	} {
		t.Run(name, func(t *testing.T) {
			diagnostics := DiagnoseConfigOverlay(test.raw, []string{"medium"})
			if len(diagnostics) != 1 || diagnostics[0].Code != test.code || diagnostics[0].Key != test.key || diagnostics[0].Line != test.line || diagnostics[0].Column < 1 {
				t.Fatalf("diagnostics = %+v", diagnostics)
			}
			raw, _ := json.Marshal(diagnostics)
			if strings.Contains(string(raw), "private-") {
				t.Fatal("diagnostic exposed input")
			}
		})
	}
}

func TestOverlaySchemaMatchesValidatorAndModelCapabilities(t *testing.T) {
	schema := OverlaySchema([]string{"medium", "max"}, "medium")
	if schema.Revision != "cos_"+schema.Digest || len(schema.Digest) != 64 || len(schema.Fields) != 4 || schema.MaximumBytes != 65536 {
		t.Fatalf("schema = %+v", schema)
	}
	if other := OverlaySchema([]string{"medium"}, "medium"); other.Digest == schema.Digest {
		t.Fatal("model capability change did not change schema pin")
	}
	for _, field := range schema.Fields {
		for _, value := range field.AllowedValues {
			literal := `"` + value + `"`
			if field.ValueType == "boolean" {
				literal = value
			}
			if diagnostics := DiagnoseConfigOverlay(field.Key+" = "+literal, schema.Fields[0].AllowedValues); len(diagnostics) != 0 {
				t.Fatalf("advertised value rejected: %s: %+v", field.Key, diagnostics)
			}
		}
	}
	if _, err := ParseConfigOverlay(`model_reasoning_effort = "custom-model-effort"`); err != nil {
		t.Fatal(err)
	}
	if diagnostics := DiagnoseConfigOverlay(`model_reasoning_effort = "custom-model-effort"`, []string{}); len(diagnostics) != 1 || diagnostics[0].Code != OverlayEffortUnsupported {
		t.Fatalf("missing model capability accepted: %+v", diagnostics)
	}
}

func TestEffectiveReasoningEffortRequiresOwnerValue(t *testing.T) {
	for _, test := range []struct{ overlay, effective string }{
		{"", ""}, {"", "bad effort!"}, {`model_reasoning_effort = "high"`, "medium"},
	} {
		if ValidateEffectiveReasoningEffort(test.overlay, test.effective) == nil {
			t.Fatal("invalid effective effort accepted")
		}
	}
	if ValidateEffectiveReasoningEffort("", "custom-effort") != nil || ValidateEffectiveReasoningEffort(`model_reasoning_effort = "high"`, "high") != nil {
		t.Fatal("owner effort rejected")
	}
}
