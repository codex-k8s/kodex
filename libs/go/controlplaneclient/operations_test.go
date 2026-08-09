package controlplaneclient

import "testing"

func TestControlAPIGatewayOperationSetIsExact(t *testing.T) {
	t.Parallel()

	operations := ControlAPIGatewayOperations()
	if len(operations) != 87 {
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
		"control.role-definition.manage",
		"control.agent.manage",
		"control.agent-assignment.manage",
		"control.instruction-set.manage",
		"control.owner-configuration.catalog",
		"control.provider-reference.get",
		"control.provider-pool.manage",
		"control.schedule.bind",
		"control.schedule.create-from-selections",
		"control.owner-schedule.manage",
		"control.owner-schedule.get",
		"control.owner-schedule.list",
		"control.run.manage",
		"control.run.list",
		"control.run.timeline",
		"control.workspace-backup.manage",
		"control.workspace-restore.manage",
		"control.runtime-incident.manage",
		"control.workspace-mapping.get",
		"control.legacy-cutover.get",
		"control.legacy-cutover.list",
		"control.legacy-cutover.resolve",
	} {
		if operations[operation] == "" {
			t.Fatalf("specialized control API operation is absent: %s", operation)
		}
	}
	if _, exists := operations["control.gateway-public-tls.admit"]; exists {
		t.Fatal("legacy broad TLS admission operation must be absent")
	}
}

func TestIntegrationGatewayOperationSetContainsOnlyProviderSeam(t *testing.T) {
	t.Parallel()

	operations := IntegrationGatewayOperations()
	if len(operations) != 17 {
		t.Fatalf("integration-gateway operation set must contain exact methods: %d", len(operations))
	}
	for _, operation := range []string{
		"control.integration.provider-reference.manage",
		"control.integration.provider-reference.get",
		"control.integration.provider-reference.list",
		"control.role-definition.git.reconcile",
		"control.agent.git.reconcile",
		"control.instruction-set.git.reconcile",
		"control.provider-pool.git.reconcile",
	} {
		if operations[operation] == "" {
			t.Fatalf("specialized integration operation is absent: %s", operation)
		}
	}
	if _, exists := operations["control.provider-pool.manage"]; exists {
		t.Fatal("integration gateway must not manage owner provider pools")
	}
	if _, exists := operations["control.integration.workspace-mapping.manage"]; exists {
		t.Fatal("integration gateway must not own Mattermost mapping")
	}
}

func TestInteractionGatewayOperationSetOwnsMattermostProviderSeams(t *testing.T) {
	t.Parallel()
	operations := InteractionGatewayOperations()
	for _, operation := range []string{
		"control.interaction.agent-bot.manage",
		"control.interaction.agent-bot.get",
		"control.interaction.workspace-mapping.manage",
		"control.interaction.workspace-mapping.get",
		"control.interaction.workspace-mapping.list",
	} {
		if operations[operation] == "" {
			t.Fatalf("specialized interaction operation is absent: %s", operation)
		}
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

func TestLegacyDataMigrationOperationSetIsMaterializerOnly(t *testing.T) {
	t.Parallel()

	operations := LegacyDataMigrationOperations()
	if len(operations) != 5 {
		t.Fatalf("legacy-data-migration operation set must contain exact owner methods: %d", len(operations))
	}
	for _, operation := range []string{
		"control.legacy-data-migration.readiness",
		"control.legacy-graph-migration.prepare",
		"control.legacy-graph-migration.materialize",
		"control.legacy-graph-migration.read",
		"control.legacy-graph-migration.abort",
	} {
		if operations[operation] == "" {
			t.Fatalf("legacy materializer operation is absent: %s", operation)
		}
	}
	if _, exists := operations["control.resource.create"]; exists {
		t.Fatal("legacy migration job must not receive generic resource mutation")
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
