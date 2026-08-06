package controlplaneclient

import "testing"

func TestControlAPIGatewayOperationSetIsExact(t *testing.T) {
	t.Parallel()

	operations := ControlAPIGatewayOperations()
	if len(operations) != 37 {
		t.Fatalf("control-api-gateway operation set must contain exact materialized methods: %d", len(operations))
	}
	for _, operation := range []string{
		"control.gateway-public-tls.prepare",
		"control.gateway-public-tls.confirm",
		"control.gateway-public-tls.check",
		"control.schedule.manage",
		"control.schedule.run-now",
		"control.schedule.occurrences.list",
		"control.schedule.recovery.resolve",
		"control.project.update",
		"control.project.delete",
		"control.owner-gate.resolve",
		"control.backup.list",
		"control.backup.get",
		"control.backup.restore",
		"control.restore-operation.get",
		"control.role-image-recipe.manage",
		"control.role-image-recipe.get",
		"control.image-build.manage",
		"control.image-build.get",
	} {
		if operations[operation] == "" {
			t.Fatalf("specialized control API operation is absent: %s", operation)
		}
	}
	if _, exists := operations["control.gateway-public-tls.admit"]; exists {
		t.Fatal("legacy broad TLS admission operation must be absent")
	}
}

func TestAutomationSchedulerOperationSetIsPollingOnly(t *testing.T) {
	t.Parallel()

	operations := AutomationSchedulerOperations()
	if len(operations) != 5 {
		t.Fatalf("automation-scheduler operation set must contain exact polling methods: %d", len(operations))
	}
	for _, operation := range []string{
		"control.automation-scheduler.readiness",
		"control.schedule.claim-due",
		"control.schedule.claim-occurrence",
		"control.schedule.materialize-occurrence",
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

func TestImagePromotionOperationSetConsumesClaimBeforeSideEffect(t *testing.T) {
	t.Parallel()
	operations := ImagePromotionOperations()
	if len(operations) != 4 {
		t.Fatalf("image promotion operation set must contain exact methods: %d", len(operations))
	}
	for _, operation := range []string{
		"control.image-promotion.readiness",
		"control.image-promotion.claim",
		"control.image-promotion.authorize",
		"control.image-promotion.complete",
	} {
		if operations[operation] == "" {
			t.Fatalf("image promotion operation is absent: %s", operation)
		}
	}
}
