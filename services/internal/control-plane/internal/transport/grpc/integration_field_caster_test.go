package grpc

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"testing"
)

// TestIntegrationFieldPreservesZeroBoundAndConstraints checks zero-valued numeric bounds remain
// present, other constraints survive and allowed values do not alias input; absent bounds stay
// absent.
// TestIntegrationFieldPreservesZeroBoundAndConstraints проверяет наличие числовой границы ноль,
// сохранение ограничений и независимость allowed values от input; отсутствующие границы не
// создаются.
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

// TestIntegrationDefinitionPreservesAuthoritativeConnectionSummary checks definition projection
// copies total and healthy connection counts without recomputing the owner summary.
// TestIntegrationDefinitionPreservesAuthoritativeConnectionSummary проверяет копирование общего и
// healthy connection counts представлением определения без пересчёта owner summary.
func TestIntegrationDefinitionPreservesAuthoritativeConnectionSummary(t *testing.T) {
	result := castDefinition(entity.IntegrationDefinition{
		ConnectionCount:        41,
		HealthyConnectionCount: 7,
	})
	if result.ConnectionCount != 41 || result.HealthyConnectionCount != 7 {
		t.Fatal("integration connection summary lost")
	}
}
