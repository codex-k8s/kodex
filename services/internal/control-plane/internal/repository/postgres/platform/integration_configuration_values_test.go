package platform

import (
	"math"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// TestIntegrationConfigurationValuesUsesPinnedScalarTypes checks browser/protobuf integer and boolean values become canonical storage strings without coercing text fields, unknown keys or lossy numbers.
// TestIntegrationConfigurationValuesUsesPinnedScalarTypes проверяет преобразование browser/protobuf integer и boolean в canonical storage strings без coerce текстовых полей, unknown keys либо lossy numbers.
func TestIntegrationConfigurationValuesUsesPinnedScalarTypes(t *testing.T) {
	t.Parallel()
	definition := integrationpackage.Package{Spec: integrationpackage.Spec{ConfigurationFields: []integrationpackage.Field{
		{Key: "text", Type: "STRING"}, {Key: "count", Type: "INTEGER"}, {Key: "enabled", Type: "BOOLEAN"},
	}}}
	value, ok := integrationConfigurationValues(definition, map[string]any{"text": " fixture ", "count": float64(12345678), "enabled": false})
	if !ok || !reflect.DeepEqual(value, map[string]string{"text": "fixture", "count": "12345678", "enabled": "false"}) {
		t.Fatal("typed configuration lost canonical storage form")
	}
	for _, input := range []map[string]any{
		{"text": true}, {"text": float64(1)}, {"count": true}, {"enabled": float64(1)}, {"count": 1.5}, {"count": math.NaN()}, {"count": math.Inf(1)},
		{"count": float64(9007199254740992)}, {"unknown": "value"}, {"text": nil}, {"text": []any{"value"}}, {"text": map[string]any{}},
	} {
		if _, ok := integrationConfigurationValues(definition, input); ok {
			t.Fatal("pinned configuration accepted a wrong or lossy scalar")
		}
	}
}
