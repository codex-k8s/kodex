package controlplaneclient

import controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"

// ControlAPIGatewayOperations возвращает закрытый owner-facing реестр.
func ControlAPIGatewayOperations() map[string]string {
	return map[string]string{
		"platform.query.bootstrap.get":                 controlplanev1.PlatformQueryService_GetBootstrapState_FullMethodName,
		"platform.query.overview.get":                  controlplanev1.PlatformQueryService_GetOverview_FullMethodName,
		"platform.query.capabilities.list":             controlplanev1.PlatformQueryService_ListPlatformCapabilities_FullMethodName,
		"platform.query.projects.list":                 controlplanev1.PlatformQueryService_ListProjects_FullMethodName,
		"platform.query.projects.get":                  controlplanev1.PlatformQueryService_GetProject_FullMethodName,
		"platform.query.memberships.list":              controlplanev1.PlatformQueryService_ListProjectMemberships_FullMethodName,
		"platform.query.agents.list":                   controlplanev1.PlatformQueryService_ListAgents_FullMethodName,
		"platform.query.agents.get":                    controlplanev1.PlatformQueryService_GetAgent_FullMethodName,
		"platform.query.workflows.list":                controlplanev1.PlatformQueryService_ListWorkflows_FullMethodName,
		"platform.query.workflows.get":                 controlplanev1.PlatformQueryService_GetWorkflow_FullMethodName,
		"platform.query.runs.list":                     controlplanev1.PlatformQueryService_ListRuns_FullMethodName,
		"platform.query.runs.get":                      controlplanev1.PlatformQueryService_GetRun_FullMethodName,
		"platform.query.run-graph.get":                 controlplanev1.PlatformQueryService_GetRunGraph_FullMethodName,
		"platform.query.run-events.list":               controlplanev1.PlatformQueryService_ListRunEvents_FullMethodName,
		"platform.query.owner-gates.list":              controlplanev1.PlatformQueryService_ListOwnerGates_FullMethodName,
		"platform.query.owner-gates.get":               controlplanev1.PlatformQueryService_GetOwnerGate_FullMethodName,
		"platform.query.artifacts.list":                controlplanev1.PlatformQueryService_ListArtifacts_FullMethodName,
		"platform.query.artifacts.get":                 controlplanev1.PlatformQueryService_GetArtifact_FullMethodName,
		"platform.query.schedules.list":                controlplanev1.PlatformQueryService_ListSchedules_FullMethodName,
		"platform.query.integration-definitions.list":  controlplanev1.PlatformQueryService_ListIntegrationDefinitions_FullMethodName,
		"platform.query.integration-connections.list":  controlplanev1.PlatformQueryService_ListIntegrationConnections_FullMethodName,
		"platform.query.integration-connections.get":   controlplanev1.PlatformQueryService_GetIntegrationConnection_FullMethodName,
		"platform.query.administration.get":            controlplanev1.PlatformQueryService_GetAdministration_FullMethodName,
		"platform.query.audit.list":                    controlplanev1.PlatformQueryService_ListAuditEvents_FullMethodName,
		"platform.role-images.environments.list":       controlplanev1.RoleImageService_ListRoleEnvironments_FullMethodName,
		"platform.role-images.recipes.list":            controlplanev1.RoleImageService_ListRoleImageRecipes_FullMethodName,
		"platform.role-images.recipes.get":             controlplanev1.RoleImageService_GetRoleImageRecipe_FullMethodName,
		"platform.role-images.recipes.manage":          controlplanev1.RoleImageService_ManageRoleImageRecipe_FullMethodName,
		"platform.command.onboarding.complete":         controlplanev1.PlatformCommandService_CompleteOnboarding_FullMethodName,
		"platform.command.projects.create":             controlplanev1.PlatformCommandService_CreateProject_FullMethodName,
		"platform.command.projects.update":             controlplanev1.PlatformCommandService_UpdateProject_FullMethodName,
		"platform.command.memberships.add":             controlplanev1.PlatformCommandService_AddProjectMembership_FullMethodName,
		"platform.command.memberships.change":          controlplanev1.PlatformCommandService_ChangeProjectMembership_FullMethodName,
		"platform.command.memberships.remove":          controlplanev1.PlatformCommandService_RemoveProjectMembership_FullMethodName,
		"platform.command.agents.create":               controlplanev1.PlatformCommandService_CreateAgent_FullMethodName,
		"platform.command.agents.update":               controlplanev1.PlatformCommandService_UpdateAgent_FullMethodName,
		"platform.command.agents.enable":               controlplanev1.PlatformCommandService_SetAgentEnabled_FullMethodName,
		"platform.command.agents.archive":              controlplanev1.PlatformCommandService_ArchiveAgent_FullMethodName,
		"platform.command.instructions.create-draft":   controlplanev1.PlatformCommandService_CreateInstructionDraft_FullMethodName,
		"platform.command.instructions.validate":       controlplanev1.PlatformCommandService_ValidateInstructionDraft_FullMethodName,
		"platform.command.instructions.publish":        controlplanev1.PlatformCommandService_PublishInstructionDraft_FullMethodName,
		"platform.command.instructions.rollback":       controlplanev1.PlatformCommandService_RollbackInstructions_FullMethodName,
		"platform.command.agent-capabilities.change":   controlplanev1.PlatformCommandService_ChangeAgentCapability_FullMethodName,
		"platform.command.agent-grants.change":         controlplanev1.PlatformCommandService_ChangeAgentIntegrationGrant_FullMethodName,
		"platform.command.workflows.create":            controlplanev1.PlatformCommandService_CreateWorkflow_FullMethodName,
		"platform.command.workflows.update-draft":      controlplanev1.PlatformCommandService_UpdateWorkflowDraft_FullMethodName,
		"platform.command.workflows.validate":          controlplanev1.PlatformCommandService_ValidateWorkflowDraft_FullMethodName,
		"platform.command.workflows.publish":           controlplanev1.PlatformCommandService_PublishWorkflowDraft_FullMethodName,
		"platform.command.workflows.archive":           controlplanev1.PlatformCommandService_ArchiveWorkflow_FullMethodName,
		"platform.command.runs.launch":                 controlplanev1.PlatformCommandService_LaunchRun_FullMethodName,
		"platform.command.sessions.add-turn":           controlplanev1.PlatformCommandService_AddSessionTurn_FullMethodName,
		"platform.command.runs.cancel":                 controlplanev1.PlatformCommandService_CancelRun_FullMethodName,
		"platform.command.runs.retry":                  controlplanev1.PlatformCommandService_RetryRun_FullMethodName,
		"platform.command.owner-gates.resolve":         controlplanev1.PlatformCommandService_ResolveOwnerGate_FullMethodName,
		"platform.command.artifacts.upload":            controlplanev1.PlatformCommandService_UploadArtifact_FullMethodName,
		"platform.command.artifacts.download":          controlplanev1.PlatformCommandService_DownloadArtifact_FullMethodName,
		"platform.command.artifact-bindings.change":    controlplanev1.PlatformCommandService_ChangeArtifactBinding_FullMethodName,
		"platform.command.schedules.create":            controlplanev1.PlatformCommandService_CreateSchedule_FullMethodName,
		"platform.command.schedules.update":            controlplanev1.PlatformCommandService_UpdateSchedule_FullMethodName,
		"platform.command.schedules.enable":            controlplanev1.PlatformCommandService_SetScheduleEnabled_FullMethodName,
		"platform.command.integrations.create":         controlplanev1.PlatformCommandService_CreateIntegrationConnection_FullMethodName,
		"platform.command.integrations.test":           controlplanev1.PlatformCommandService_TestIntegrationConnection_FullMethodName,
		"platform.command.integrations.enable":         controlplanev1.PlatformCommandService_SetIntegrationConnectionEnabled_FullMethodName,
		"platform.command.integration-grants.change":   controlplanev1.PlatformCommandService_ChangeIntegrationGrant_FullMethodName,
		"platform.assistant.get":                       controlplanev1.SystemAssistantService_GetSystemAssistant_FullMethodName,
		"platform.assistant.conversations.list":        controlplanev1.SystemAssistantService_ListAssistantConversations_FullMethodName,
		"platform.assistant.conversations.create":      controlplanev1.SystemAssistantService_CreateAssistantConversation_FullMethodName,
		"platform.assistant.turns.add":                 controlplanev1.SystemAssistantService_AddAssistantTurn_FullMethodName,
		"platform.assistant.plans.apply":               controlplanev1.SystemAssistantService_ApplyAssistantPlan_FullMethodName,
		"platform.assistant.owner-instructions.update": controlplanev1.SystemAssistantService_UpdateAssistantOwnerInstructions_FullMethodName,
		"platform.assistant.recover":                   controlplanev1.SystemAssistantService_RecoverSystemAssistant_FullMethodName,
	}
}

func RuntimeOperations() map[string]string {
	return map[string]string{
		"platform.runtime.execution.claim":        controlplanev1.RuntimeWorkService_ClaimExecution_FullMethodName,
		"platform.runtime.execution.renew":        controlplanev1.RuntimeWorkService_RenewExecution_FullMethodName,
		"platform.runtime.execution.progress":     controlplanev1.RuntimeWorkService_ReportExecutionProgress_FullMethodName,
		"platform.runtime.execution.complete":     controlplanev1.RuntimeWorkService_CompleteExecution_FullMethodName,
		"platform.runtime.execution.delegate":     controlplanev1.RuntimeWorkService_DelegateExecution_FullMethodName,
		"platform.runtime.assistant.plan.propose": controlplanev1.RuntimeWorkService_ProposeAssistantPlan_FullMethodName,
		"platform.runtime.warm.reconcile":         controlplanev1.RuntimeWorkService_ReconcileWarmRuntime_FullMethodName,
		"platform.runtime.warm.report":            controlplanev1.RuntimeWorkService_ReportWarmRuntime_FullMethodName,
		"platform.runtime.integration.resolve":    controlplanev1.RuntimeWorkService_ResolveIntegrationInvocation_FullMethodName,
		"platform.runtime.integration.get":        controlplanev1.RuntimeWorkService_GetIntegrationInvocation_FullMethodName,
	}
}

// RoleImageBuilderOperations возвращает только операции fenced lifecycle
// сборки образа роли. Admission и promotion принадлежат отдельным workload.
func RoleImageBuilderOperations() map[string]string {
	return map[string]string{
		"platform.role-images.builds.claim":    controlplanev1.RoleImageService_ClaimImageBuild_FullMethodName,
		"platform.role-images.builds.renew":    controlplanev1.RoleImageService_RenewImageBuild_FullMethodName,
		"platform.role-images.builds.progress": controlplanev1.RoleImageService_ReportImageBuildProgress_FullMethodName,
		"platform.role-images.builds.complete": controlplanev1.RoleImageService_CompleteImageBuild_FullMethodName,
		"platform.role-images.builds.fail":     controlplanev1.RoleImageService_FailImageBuild_FullMethodName,
	}
}

// ImageAdmissionOperations изолирует проверку supply-chain evidence от
// builder и promotion workload.
func ImageAdmissionOperations() map[string]string {
	return map[string]string{
		"platform.role-images.admission.claim":  controlplanev1.RoleImageService_ClaimImageAdmission_FullMethodName,
		"platform.role-images.admission.record": controlplanev1.RoleImageService_RecordImageAdmission_FullMethodName,
	}
}

// ImagePromotionOperations разрешает только одноразовый перенос уже
// допущенного immutable image artifact в promoted registry.
func ImagePromotionOperations() map[string]string {
	return map[string]string{
		"platform.role-images.promotion.claim":     controlplanev1.RoleImageService_ClaimImagePromotion_FullMethodName,
		"platform.role-images.promotion.authorize": controlplanev1.RoleImageService_AuthorizeImagePromotion_FullMethodName,
		"platform.role-images.promotion.complete":  controlplanev1.RoleImageService_CompleteImagePromotion_FullMethodName,
	}
}

// AutomationSchedulerOperations возвращает минимальный профиль job, которая
// только материализует server-owned due occurrences.
func AutomationSchedulerOperations() map[string]string {
	return map[string]string{
		"platform.runtime.schedules.claim":       controlplanev1.RuntimeWorkService_ClaimDueSchedules_FullMethodName,
		"platform.runtime.schedules.materialize": controlplanev1.RuntimeWorkService_MaterializeScheduleOccurrence_FullMethodName,
	}
}

func IntegrationGatewayOperations() map[string]string {
	return map[string]string{
		"platform.runtime.integration-tests.claim":    controlplanev1.RuntimeWorkService_ClaimIntegrationConnectionTests_FullMethodName,
		"platform.runtime.integration-tests.complete": controlplanev1.RuntimeWorkService_CompleteIntegrationConnectionTest_FullMethodName,
		"platform.runtime.integrations.claim":         controlplanev1.RuntimeWorkService_ClaimIntegrationInvocations_FullMethodName,
		"platform.runtime.integrations.complete":      controlplanev1.RuntimeWorkService_CompleteIntegrationInvocation_FullMethodName,
	}
}

func InteractionGatewayOperations() map[string]string { return map[string]string{} }

// ControlAPIGatewayProjectRequiredOperations возвращает операции, для которых
// proof обязан содержать повторно проверенную project boundary. Операции над
// ресурсами вне project route повторно разрешают project по самому opaque ref в
// control-plane и поэтому не доверяют locator из браузера.
func ControlAPIGatewayProjectRequiredOperations() map[string]struct{} {
	return map[string]struct{}{
		"platform.query.projects.get":         {},
		"platform.query.memberships.list":     {},
		"platform.query.agents.list":          {},
		"platform.query.workflows.list":       {},
		"platform.query.artifacts.list":       {},
		"platform.query.schedules.list":       {},
		"platform.command.projects.update":    {},
		"platform.command.memberships.add":    {},
		"platform.command.memberships.change": {},
		"platform.command.memberships.remove": {},
		"platform.command.agents.create":      {},
		"platform.command.workflows.create":   {},
		"platform.command.artifacts.upload":   {},
		"platform.command.schedules.create":   {},
		"platform.role-images.recipes.list":   {},
		"platform.role-images.recipes.manage": {},
	}
}
