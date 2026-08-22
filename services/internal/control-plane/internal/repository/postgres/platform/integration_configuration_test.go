package platform

import (
	"reflect"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
)

func TestValidateIntegrationConfigurationUsesClosedTypedSchema(t *testing.T) {
	t.Parallel()
	fields := []entity.IntegrationConfigurationField{
		{Key: "server_url", ValueType: "URL", Required: true},
		{Key: "allowed_namespaces", ValueType: "STRING_LIST", Required: true},
	}
	input := map[string]any{
		"server_url":         "https://cluster.example.test/",
		"allowed_namespaces": []any{"sales", "support", "sales"},
	}
	got, ok := validateIntegrationConfiguration(fields, input)
	if !ok {
		t.Fatal("valid typed public configuration was rejected")
	}
	want := map[string]any{"server_url": "https://cluster.example.test", "allowed_namespaces": []string{"sales", "support"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected normalized configuration: got=%#v want=%#v", got, want)
	}
	for name, invalid := range map[string]map[string]any{
		"unknown field": {"server_url": "https://cluster.example.test", "allowed_namespaces": []any{"sales"}, "token": "not-public"},
		"plaintext URL": {"server_url": "http://cluster.example.test", "allowed_namespaces": []any{"sales"}},
		"empty list":    {"server_url": "https://cluster.example.test", "allowed_namespaces": []any{}},
		"wrong type":    {"server_url": "https://cluster.example.test", "allowed_namespaces": "sales"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, valid := validateIntegrationConfiguration(fields, invalid); valid {
				t.Fatal("invalid public configuration was accepted")
			}
		})
	}
}

func TestConnectionActionsAreServerOwnedAndPermissionAware(t *testing.T) {
	t.Parallel()
	connected := entity.IntegrationConnection{Enabled: true, State: "CONNECTED"}
	if got := connectionActions(connected, false, false); !reflect.DeepEqual(got, []string{"OPEN"}) {
		t.Fatalf("read-only actor received mutation actions: %v", got)
	}
	if got := connectionActions(connected, false, true); !reflect.DeepEqual(got, []string{"OPEN", "MANAGE_GRANTS"}) {
		t.Fatalf("project integration manager actions: %v", got)
	}
	if got := connectionActions(connected, true, true); !reflect.DeepEqual(got, []string{"OPEN", "TEST", "DISABLE", "MANAGE_GRANTS"}) {
		t.Fatalf("platform integration manager actions: %v", got)
	}
	testingState := entity.IntegrationConnection{Enabled: true, State: "TESTING"}
	if got := connectionActions(testingState, true, true); !reflect.DeepEqual(got, []string{"OPEN", "DISABLE"}) {
		t.Fatalf("testing connection exposed conflicting actions: %v", got)
	}
}
