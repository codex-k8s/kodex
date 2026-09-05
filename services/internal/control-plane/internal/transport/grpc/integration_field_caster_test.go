package grpc

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"testing"
)

func TestIntegrationFieldPreservesZeroBoundAndConstraints(t *testing.T) {
	zero, maximum := int64(0), int64(15)
	field := entity.IntegrationConfigurationField{Key: "limit", Label: "Limit", Help: "Bounded limit", ValueType: "INTEGER", Required: true, Placeholder: "0", Format: "PLAIN", AllowedValues: []string{"example"}, Minimum: &zero, Maximum: &maximum, MaximumLength: 32}
	result := castIntegrationField(field)
	if !result.HasMinimum || result.Minimum != 0 || !result.HasMaximum || result.Maximum != 15 || result.MaximumLength != 32 || result.Format != "PLAIN" || len(result.AllowedValues) != 1 || result.AllowedValues[0] != "example" {
		t.Fatal("integration field constraints lost")
	}
	field.AllowedValues[0] = "changed"
	if result.AllowedValues[0] != "example" {
		t.Fatal("immutable constraints alias input")
	}
	if empty := castIntegrationField(entity.IntegrationConfigurationField{}); empty.HasMinimum || empty.HasMaximum {
		t.Fatal("missing numeric bounds invented")
	}
}
