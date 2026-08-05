package controlplaneclient

import "testing"

func TestControlAPIGatewayOperationSetIsExact(t *testing.T) {
	t.Parallel()

	operations := ControlAPIGatewayOperations()
	if len(operations) != 23 {
		t.Fatalf("control-api-gateway operation set must contain exact materialized methods: %d", len(operations))
	}
	for _, operation := range []string{
		"control.gateway-public-tls.prepare",
		"control.gateway-public-tls.confirm",
		"control.gateway-public-tls.check",
		"control.schedule.manage",
		"control.schedule.run-now",
	} {
		if operations[operation] == "" {
			t.Fatalf("specialized TLS operation is absent: %s", operation)
		}
	}
	if _, exists := operations["control.gateway-public-tls.admit"]; exists {
		t.Fatal("legacy broad TLS admission operation must be absent")
	}
}

func TestAutomationSchedulerOperationSetIsPollingOnly(t *testing.T) {
	t.Parallel()

	operations := AutomationSchedulerOperations()
	if len(operations) != 4 {
		t.Fatalf("automation-scheduler operation set must contain exact polling methods: %d", len(operations))
	}
	for _, operation := range []string{
		"control.automation-scheduler.readiness",
		"control.schedule.claim-due",
		"control.schedule.claim-occurrence",
		"control.schedule.complete-occurrence",
	} {
		if operations[operation] == "" {
			t.Fatalf("automation-scheduler polling operation is absent: %s", operation)
		}
	}
	if _, exists := operations["control.schedule-resource.get"]; exists {
		t.Fatal("polling-only automation-scheduler must not declare a false event hydration operation")
	}
}
