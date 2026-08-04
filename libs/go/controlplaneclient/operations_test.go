package controlplaneclient

import "testing"

func TestControlAPIGatewayOperationSetIsExact(t *testing.T) {
	t.Parallel()

	operations := ControlAPIGatewayOperations()
	if len(operations) != 21 {
		t.Fatalf("control-api-gateway operation set must contain exact materialized methods: %d", len(operations))
	}
	for _, operation := range []string{
		"control.gateway-public-tls.prepare",
		"control.gateway-public-tls.confirm",
		"control.gateway-public-tls.check",
	} {
		if operations[operation] == "" {
			t.Fatalf("specialized TLS operation is absent: %s", operation)
		}
	}
	if _, exists := operations["control.gateway-public-tls.admit"]; exists {
		t.Fatal("legacy broad TLS admission operation must be absent")
	}
}
