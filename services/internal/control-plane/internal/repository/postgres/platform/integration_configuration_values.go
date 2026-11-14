package platform

import (
	"math"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// integrationConfigurationValues converts browser/protobuf scalars only according to the exact pinned package declaration; storage and background consumers retain canonical string configuration.
// integrationConfigurationValues преобразует browser/protobuf scalars лишь по точному pinned package declaration; storage и background consumers сохраняют canonical string configuration.
func integrationConfigurationValues(definition integrationpackage.Package, input map[string]any) (map[string]string, bool) {
	fields := make(map[string]integrationpackage.Field, len(definition.Spec.ConfigurationFields))
	for _, field := range definition.Spec.ConfigurationFields {
		fields[field.Key] = field
	}
	result := make(map[string]string, len(input))
	for key, raw := range input {
		field, exists := fields[key]
		if !exists {
			return nil, false
		}
		if value, ok := raw.(string); ok {
			value = strings.TrimSpace(value)
			if value == "" {
				return nil, false
			}
			result[key] = value
			continue
		}
		switch value := raw.(type) {
		case float64:
			if field.Type != "INTEGER" || math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || math.Abs(value) > 9007199254740991 {
				return nil, false
			}
			result[key] = strconv.FormatFloat(value, 'f', 0, 64)
		case bool:
			if field.Type != "BOOLEAN" {
				return nil, false
			}
			result[key] = strconv.FormatBool(value)
		default:
			return nil, false
		}
	}
	// The declaration validator remains the owner of required fields, bounds and provider-specific canonical scope rules.
	// Declaration validator сохраняет владение required fields, bounds и provider-specific canonical scope rules.
	return result, true
}
