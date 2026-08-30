package controlplaneclient

import controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"

// ControlAPIGatewayOperations возвращает закрытый owner-facing реестр.
func ControlAPIGatewayOperations() map[string]string {
	return map[string]string{
		"platform.query.bootstrap.get":                             controlplanev1.PlatformQueryService_GetBootstrapState_FullMethodName,
		"platform.query.event-cursor.get":                          controlplanev1.PlatformQueryService_GetPlatformEventCursor_FullMethodName,
		"platform.query.overview.get":                              controlplanev1.PlatformQueryService_GetOverview_FullMethodName,
		"platform.query.capabilities.list":                         controlplanev1.PlatformQueryService_ListPlatformCapabilities_FullMethodName,
		"platform.query.runtimes.list":                             controlplanev1.PlatformQueryService_ListRuntimeSelections_FullMethodName,
		"platform.query.search":                                    controlplanev1.PlatformQueryService_SearchPlatform_FullMethodName,
		"platform.query.projects.list":                             controlplanev1.PlatformQueryService_ListProjects_FullMethodName,
		"platform.query.projects.get":                              controlplanev1.PlatformQueryService_GetProject_FullMethodName,
		"platform.query.organization-memberships.list":             controlplanev1.PlatformQueryService_ListPlatformMemberships_FullMethodName,
		"platform.query.organization-membership-candidates.list":   controlplanev1.PlatformQueryService_ListPlatformMembershipCandidates_FullMethodName,
		"platform.query.memberships.list":                          controlplanev1.PlatformQueryService_ListProjectMemberships_FullMethodName,
		"platform.query.membership-candidates.list":                controlplanev1.PlatformQueryService_ListProjectMembershipCandidates_FullMethodName,
		"platform.query.agents.list":                               controlplanev1.PlatformQueryService_ListAgents_FullMethodName,
		"platform.query.agents.get":                                controlplanev1.PlatformQueryService_GetAgent_FullMethodName,
		"platform.query.agent-instruction-versions.list":           controlplanev1.PlatformQueryService_ListAgentInstructionVersions_FullMethodName,
		"platform.query.workflows.list":                            controlplanev1.PlatformQueryService_ListWorkflows_FullMethodName,
		"platform.query.workflows.get":                             controlplanev1.PlatformQueryService_GetWorkflow_FullMethodName,
		"platform.query.runs.list":                                 controlplanev1.PlatformQueryService_ListRuns_FullMethodName,
		"platform.query.runs.get":                                  controlplanev1.PlatformQueryService_GetRun_FullMethodName,
		"platform.query.run-graph.get":                             controlplanev1.PlatformQueryService_GetRunGraph_FullMethodName,
		"platform.query.run-events.list":                           controlplanev1.PlatformQueryService_ListRunEvents_FullMethodName,
		"platform.query.owner-gates.list":                          controlplanev1.PlatformQueryService_ListOwnerGates_FullMethodName,
		"platform.query.owner-gates.get":                           controlplanev1.PlatformQueryService_GetOwnerGate_FullMethodName,
		"platform.query.artifacts.list":                            controlplanev1.PlatformQueryService_ListArtifacts_FullMethodName,
		"platform.query.artifacts.get":                             controlplanev1.PlatformQueryService_GetArtifact_FullMethodName,
		"platform.query.artifacts.impact.get":                      controlplanev1.PlatformQueryService_GetArtifactImpact_FullMethodName,
		"platform.query.attachment-sets.get":                       controlplanev1.PlatformQueryService_GetAttachmentSet_FullMethodName,
		"platform.query.schedules.list":                            controlplanev1.PlatformQueryService_ListSchedules_FullMethodName,
		"platform.query.schedules.get":                             controlplanev1.PlatformQueryService_GetSchedule_FullMethodName,
		"platform.query.schedule-revisions.list":                   controlplanev1.PlatformQueryService_ListScheduleRevisions_FullMethodName,
		"platform.query.schedule-runs.list":                        controlplanev1.PlatformQueryService_ListScheduleRuns_FullMethodName,
		"platform.query.provider-definitions.list":                 controlplanev1.PlatformQueryService_ListProviderDefinitions_FullMethodName,
		"platform.query.provider-accounts.list":                    controlplanev1.PlatformQueryService_ListProviderAccounts_FullMethodName,
		"platform.query.provider-accounts.get":                     controlplanev1.PlatformQueryService_GetProviderAccount_FullMethodName,
		"platform.query.integration-definitions.list":              controlplanev1.PlatformQueryService_ListIntegrationDefinitions_FullMethodName,
		"platform.query.integration-connections.list":              controlplanev1.PlatformQueryService_ListIntegrationConnections_FullMethodName,
		"platform.query.integration-connections.get":               controlplanev1.PlatformQueryService_GetIntegrationConnection_FullMethodName,
		"platform.query.administration.get":                        controlplanev1.PlatformQueryService_GetAdministration_FullMethodName,
		"platform.query.audit.list":                                controlplanev1.PlatformQueryService_ListAuditEvents_FullMethodName,
		"platform.access.permissions.list":                         controlplanev1.AccessService_ListPermissionRegistry_FullMethodName,
		"platform.access.subjects.list":                            controlplanev1.AccessService_ListAccessSubjects_FullMethodName,
		"platform.access.oidc-groups.list":                         controlplanev1.AccessService_ListOIDCGroups_FullMethodName,
		"platform.access.roles.list":                               controlplanev1.AccessService_ListAccessRoles_FullMethodName,
		"platform.access.role-versions.list":                       controlplanev1.AccessService_ListAccessRoleVersions_FullMethodName,
		"platform.access.bindings.list":                            controlplanev1.AccessService_ListAccessBindings_FullMethodName,
		"platform.access.effective.query":                          controlplanev1.AccessService_QueryEffectiveAccess_FullMethodName,
		"platform.access.effective.explain":                        controlplanev1.AccessService_ExplainAccess_FullMethodName,
		"platform.access.effective.simulate":                       controlplanev1.AccessService_SimulateAccess_FullMethodName,
		"platform.access.roles.create":                             controlplanev1.AccessService_CreateAccessRole_FullMethodName,
		"platform.access.role-versions.create":                     controlplanev1.AccessService_CreateAccessRoleVersion_FullMethodName,
		"platform.access.roles.archive":                            controlplanev1.AccessService_ArchiveAccessRole_FullMethodName,
		"platform.access.bindings.create":                          controlplanev1.AccessService_CreateAccessBinding_FullMethodName,
		"platform.access.bindings.change":                          controlplanev1.AccessService_ChangeAccessBinding_FullMethodName,
		"platform.access.bindings.revoke":                          controlplanev1.AccessService_RevokeAccessBinding_FullMethodName,
		"platform.query.agent-runtime-configuration.get":           controlplanev1.PlatformQueryService_GetAgentRuntimeConfiguration_FullMethodName,
		"platform.query.agent-runtime-configuration-versions.list": controlplanev1.PlatformQueryService_ListAgentRuntimeConfigurationVersions_FullMethodName,
		"platform.query.runtime-environments.list":                 controlplanev1.PlatformQueryService_ListRuntimeEnvironmentSets_FullMethodName,
		"platform.query.runtime-environments.get":                  controlplanev1.PlatformQueryService_GetRuntimeEnvironmentSet_FullMethodName,
		"platform.query.runtime-environment-versions.list":         controlplanev1.PlatformQueryService_ListRuntimeEnvironmentVersions_FullMethodName,
		"platform.query.runtime-environments.readiness.get":        controlplanev1.PlatformQueryService_GetRuntimeEnvironmentReadiness_FullMethodName,
		"platform.query.runtime-environments.agents.list":          controlplanev1.PlatformQueryService_ListRuntimeEnvironmentAgents_FullMethodName,
		"platform.query.template-variables.list":                   controlplanev1.PlatformQueryService_ListTemplateVariables_FullMethodName,
		"platform.query.prompt-templates.validate":                 controlplanev1.PlatformQueryService_ValidatePromptTemplate_FullMethodName,
		"platform.query.prompt-templates.preview":                  controlplanev1.PlatformQueryService_PreviewPromptTemplate_FullMethodName,
		"platform.query.role-image-revisions.list":                 controlplanev1.PlatformQueryService_ListRoleImageRecipeRevisions_FullMethodName,
		"platform.query.runtime-secrets.list":                      controlplanev1.PlatformQueryService_ListRuntimeSecrets_FullMethodName,
		"platform.query.runtime-secrets.get":                       controlplanev1.PlatformQueryService_GetRuntimeSecret_FullMethodName,
		"platform.role-images.environments.list":                   controlplanev1.RoleImageService_ListRoleEnvironments_FullMethodName,
		"platform.role-images.recipes.list":                        controlplanev1.RoleImageService_ListRoleImageRecipes_FullMethodName,
		"platform.role-images.recipes.get":                         controlplanev1.RoleImageService_GetRoleImageRecipe_FullMethodName,
		"platform.role-images.recipes.manage":                      controlplanev1.RoleImageService_ManageRoleImageRecipe_FullMethodName,
		"platform.command.onboarding.complete":                     controlplanev1.PlatformCommandService_CompleteOnboarding_FullMethodName,
		"platform.command.projects.create":                         controlplanev1.PlatformCommandService_CreateProject_FullMethodName,
		"platform.command.projects.update":                         controlplanev1.PlatformCommandService_UpdateProject_FullMethodName,
		"platform.command.organization-memberships.add":            controlplanev1.PlatformCommandService_AddPlatformMembership_FullMethodName,
		"platform.command.organization-memberships.change":         controlplanev1.PlatformCommandService_ChangePlatformMembership_FullMethodName,
		"platform.command.organization-memberships.remove":         controlplanev1.PlatformCommandService_RemovePlatformMembership_FullMethodName,
		"platform.command.memberships.add":                         controlplanev1.PlatformCommandService_AddProjectMembership_FullMethodName,
		"platform.command.memberships.change":                      controlplanev1.PlatformCommandService_ChangeProjectMembership_FullMethodName,
		"platform.command.memberships.remove":                      controlplanev1.PlatformCommandService_RemoveProjectMembership_FullMethodName,
		"platform.command.agents.create":                           controlplanev1.PlatformCommandService_CreateAgent_FullMethodName,
		"platform.command.agents.update":                           controlplanev1.PlatformCommandService_UpdateAgent_FullMethodName,
		"platform.command.agents.enable":                           controlplanev1.PlatformCommandService_SetAgentEnabled_FullMethodName,
		"platform.command.agents.archive":                          controlplanev1.PlatformCommandService_ArchiveAgent_FullMethodName,
		"platform.command.agents.avatar.set":                       controlplanev1.PlatformCommandService_SetAgentAvatar_FullMethodName,
		"platform.command.agents.avatar.remove":                    controlplanev1.PlatformCommandService_RemoveAgentAvatar_FullMethodName,
		"platform.command.instructions.create-draft":               controlplanev1.PlatformCommandService_CreateInstructionDraft_FullMethodName,
		"platform.command.instructions.validate":                   controlplanev1.PlatformCommandService_ValidateInstructionDraft_FullMethodName,
		"platform.command.instructions.publish":                    controlplanev1.PlatformCommandService_PublishInstructionDraft_FullMethodName,
		"platform.command.instructions.rollback":                   controlplanev1.PlatformCommandService_RollbackInstructions_FullMethodName,
		"platform.command.agent-capabilities.change":               controlplanev1.PlatformCommandService_ChangeAgentCapability_FullMethodName,
		"platform.command.agent-grants.change":                     controlplanev1.PlatformCommandService_ChangeAgentIntegrationGrant_FullMethodName,
		"platform.command.workflows.create":                        controlplanev1.PlatformCommandService_CreateWorkflow_FullMethodName,
		"platform.command.workflows.update-draft":                  controlplanev1.PlatformCommandService_UpdateWorkflowDraft_FullMethodName,
		"platform.command.workflows.validate":                      controlplanev1.PlatformCommandService_ValidateWorkflowDraft_FullMethodName,
		"platform.command.workflows.publish":                       controlplanev1.PlatformCommandService_PublishWorkflowDraft_FullMethodName,
		"platform.command.workflows.archive":                       controlplanev1.PlatformCommandService_ArchiveWorkflow_FullMethodName,
		"platform.command.runs.launch":                             controlplanev1.PlatformCommandService_LaunchRun_FullMethodName,
		"platform.command.sessions.add-turn":                       controlplanev1.PlatformCommandService_AddSessionTurn_FullMethodName,
		"platform.command.runs.cancel":                             controlplanev1.PlatformCommandService_CancelRun_FullMethodName,
		"platform.command.runs.retry":                              controlplanev1.PlatformCommandService_RetryRun_FullMethodName,
		"platform.command.owner-gates.resolve":                     controlplanev1.PlatformCommandService_ResolveOwnerGate_FullMethodName,
		"platform.command.artifacts.upload":                        controlplanev1.PlatformCommandService_UploadArtifact_FullMethodName,
		"platform.command.organization-artifacts.upload":           controlplanev1.PlatformCommandService_UploadOrganizationArtifact_FullMethodName,
		"platform.command.attachment-sets.create-draft":            controlplanev1.PlatformCommandService_CreateAttachmentSetDraft_FullMethodName,
		"platform.command.attachment-sets.add-items":               controlplanev1.PlatformCommandService_AddAttachmentSetItems_FullMethodName,
		"platform.command.attachment-sets.remove-items":            controlplanev1.PlatformCommandService_RemoveAttachmentSetItems_FullMethodName,
		"platform.command.attachment-sets.finalize":                controlplanev1.PlatformCommandService_FinalizeAttachmentSet_FullMethodName,
		"platform.command.artifacts.download":                      controlplanev1.PlatformCommandService_DownloadArtifact_FullMethodName,
		"platform.command.artifact-bindings.change":                controlplanev1.PlatformCommandService_ChangeArtifactBinding_FullMethodName,
		"platform.command.artifacts.delete":                        controlplanev1.PlatformCommandService_DeleteArtifact_FullMethodName,
		"platform.command.artifacts.restore":                       controlplanev1.PlatformCommandService_RestoreArtifact_FullMethodName,
		"platform.command.artifacts.purge":                         controlplanev1.PlatformCommandService_PurgeArtifact_FullMethodName,
		"platform.command.schedules.create":                        controlplanev1.PlatformCommandService_CreateSchedule_FullMethodName,
		"platform.command.schedules.update":                        controlplanev1.PlatformCommandService_UpdateSchedule_FullMethodName,
		"platform.command.schedules.enable":                        controlplanev1.PlatformCommandService_SetScheduleEnabled_FullMethodName,
		"platform.command.schedules.archive":                       controlplanev1.PlatformCommandService_ArchiveSchedule_FullMethodName,
		"platform.command.schedules.delete":                        controlplanev1.PlatformCommandService_DeleteSchedule_FullMethodName,
		"platform.command.provider-accounts.create":                controlplanev1.PlatformCommandService_CreateProviderAccount_FullMethodName,
		"platform.command.provider-accounts.device-authorize":      controlplanev1.PlatformCommandService_StartProviderAccountDeviceAuthorization_FullMethodName,
		"platform.command.provider-accounts.api-key-authorize":     controlplanev1.PlatformCommandService_AuthorizeProviderAccountAPIKey_FullMethodName,
		"platform.command.provider-accounts.authorization.refresh": controlplanev1.PlatformCommandService_RefreshProviderAccountAuthorization_FullMethodName,
		"platform.command.provider-accounts.revoke":                controlplanev1.PlatformCommandService_RevokeProviderAccount_FullMethodName,
		"platform.command.provider-accounts.enable":                controlplanev1.PlatformCommandService_SetProviderAccountEnabled_FullMethodName,
		"platform.command.integrations.create":                     controlplanev1.PlatformCommandService_CreateIntegrationConnection_FullMethodName,
		"platform.command.integrations.update":                     controlplanev1.PlatformCommandService_UpdateIntegrationConnection_FullMethodName,
		"platform.command.integrations.delete":                     controlplanev1.PlatformCommandService_DeleteIntegrationConnection_FullMethodName,
		"platform.command.integrations.configure-credential":       controlplanev1.PlatformCommandService_ConfigureIntegrationConnectionCredential_FullMethodName,
		"platform.command.integrations.test":                       controlplanev1.PlatformCommandService_TestIntegrationConnection_FullMethodName,
		"platform.command.integrations.enable":                     controlplanev1.PlatformCommandService_SetIntegrationConnectionEnabled_FullMethodName,
		"platform.command.integration-grants.change":               controlplanev1.PlatformCommandService_ChangeIntegrationGrant_FullMethodName,
		"platform.command.agent-runtime-configuration.publish":     controlplanev1.PlatformCommandService_PublishAgentRuntimeConfiguration_FullMethodName,
		"platform.command.config-overlays.create-draft":            controlplanev1.PlatformCommandService_CreateConfigOverlayDraft_FullMethodName,
		"platform.command.config-overlays.validate":                controlplanev1.PlatformCommandService_ValidateConfigOverlayDraft_FullMethodName,
		"platform.command.config-overlays.publish":                 controlplanev1.PlatformCommandService_PublishConfigOverlayDraft_FullMethodName,
		"platform.command.config-overlays.rollback":                controlplanev1.PlatformCommandService_RollbackConfigOverlay_FullMethodName,
		"platform.command.runtime-environments.create":             controlplanev1.PlatformCommandService_CreateRuntimeEnvironmentSet_FullMethodName,
		"platform.command.runtime-environments.publish":            controlplanev1.PlatformCommandService_PublishRuntimeEnvironmentVersion_FullMethodName,
		"platform.command.runtime-environments.rollback":           controlplanev1.PlatformCommandService_RollbackRuntimeEnvironment_FullMethodName,
		"platform.command.runtime-environments.enable":             controlplanev1.PlatformCommandService_SetRuntimeEnvironmentEnabled_FullMethodName,
		"platform.command.runtime-environments.delete":             controlplanev1.PlatformCommandService_DeleteRuntimeEnvironment_FullMethodName,
		"platform.command.role-images.promote":                     controlplanev1.PlatformCommandService_PromoteRoleImage_FullMethodName,
		"platform.command.agent-runtime-environment.bind":          controlplanev1.PlatformCommandService_BindAgentRuntimeEnvironment_FullMethodName,
		"platform.command.runtime-secrets.create":                  controlplanev1.PlatformCommandService_PrepareCreateRuntimeSecret_FullMethodName,
		"platform.command.runtime-secrets.rotate":                  controlplanev1.PlatformCommandService_PrepareRotateRuntimeSecret_FullMethodName,
		"platform.command.runtime-secrets.reveal":                  controlplanev1.PlatformCommandService_PrepareRevealRuntimeSecret_FullMethodName,
		"platform.command.runtime-secrets.revoke":                  controlplanev1.PlatformCommandService_PrepareRevokeRuntimeSecret_FullMethodName,
		"platform.assistant.get":                                   controlplanev1.SystemAssistantService_GetSystemAssistant_FullMethodName,
		"platform.assistant.conversations.list":                    controlplanev1.SystemAssistantService_ListAssistantConversations_FullMethodName,
		"platform.assistant.conversations.create":                  controlplanev1.SystemAssistantService_CreateAssistantConversation_FullMethodName,
		"platform.assistant.conversations.title.update":            controlplanev1.SystemAssistantService_UpdateAssistantConversationTitle_FullMethodName,
		"platform.assistant.turns.add":                             controlplanev1.SystemAssistantService_AddAssistantTurn_FullMethodName,
		"platform.assistant.plans.apply":                           controlplanev1.SystemAssistantService_ApplyAssistantPlan_FullMethodName,
		"platform.assistant.plans.draft.update":                    controlplanev1.SystemAssistantService_UpdateAssistantPlanDraft_FullMethodName,
		"platform.assistant.plans.validate":                        controlplanev1.SystemAssistantService_ValidateAssistantPlan_FullMethodName,
		"platform.assistant.plans.reject":                          controlplanev1.SystemAssistantService_RejectAssistantPlan_FullMethodName,
		"platform.assistant.owner-instructions.update":             controlplanev1.SystemAssistantService_UpdateAssistantOwnerInstructions_FullMethodName,
		"platform.assistant.recover":                               controlplanev1.SystemAssistantService_RecoverSystemAssistant_FullMethodName,
	}
}

func SecretBrokerOperations() map[string]string {
	return map[string]string{
		"platform.runtime-secrets.readiness.check":         controlplanev1.RuntimeSecretWorkService_CheckRuntimeSecretWorkReadiness_FullMethodName,
		"platform.runtime-secrets.operations.consume":      controlplanev1.RuntimeSecretWorkService_ConsumeRuntimeSecretOperation_FullMethodName,
		"platform.runtime-secrets.operations.complete":     controlplanev1.RuntimeSecretWorkService_CompleteRuntimeSecretOperation_FullMethodName,
		"platform.runtime-secrets.operations.fail":         controlplanev1.RuntimeSecretWorkService_FailRuntimeSecretOperation_FullMethodName,
		"platform.runtime-secrets.operations.recover":      controlplanev1.RuntimeSecretWorkService_ListRuntimeSecretRecoveryWork_FullMethodName,
		"platform.runtime-secrets.materialization.recover": controlplanev1.RuntimeSecretWorkService_RecoverRuntimeSecretMaterialization_FullMethodName,
	}
}

// ProviderCredentialMaterializerOperations возвращает exact API изолированного
// materializer, ответы которого содержат только Secret descriptors.
func ProviderCredentialMaterializerOperations() map[string]string {
	return map[string]string{
		"platform.provider-credentials.readiness.check":        controlplanev1.ProviderCredentialMaterializerService_CheckProviderCredentialMaterializerReadiness_FullMethodName,
		"platform.provider-credentials.device-authorize.start": controlplanev1.ProviderCredentialMaterializerService_StartDeviceAuthorization_FullMethodName,
		"platform.provider-credentials.device-authorize.get":   controlplanev1.ProviderCredentialMaterializerService_ObserveDeviceAuthorization_FullMethodName,
		"platform.provider-credentials.api-key.materialize":    controlplanev1.ProviderCredentialMaterializerService_MaterializeAPIKey_FullMethodName,
	}
}

func RuntimeOperations() map[string]string {
	return map[string]string{
		"platform.runtime.execution.claim":            controlplanev1.RuntimeWorkService_ClaimExecution_FullMethodName,
		"platform.runtime.execution.artifact.read":    controlplanev1.RuntimeWorkService_ReadExecutionArtifact_FullMethodName,
		"platform.runtime.execution.renew":            controlplanev1.RuntimeWorkService_RenewExecution_FullMethodName,
		"platform.runtime.execution.progress":         controlplanev1.RuntimeWorkService_ReportExecutionProgress_FullMethodName,
		"platform.runtime.execution.complete":         controlplanev1.RuntimeWorkService_CompleteExecution_FullMethodName,
		"platform.runtime.execution.delegate":         controlplanev1.RuntimeWorkService_DelegateExecution_FullMethodName,
		"platform.runtime.assistant.metadata.propose": controlplanev1.RuntimeWorkService_ProposeAssistantMetadata_FullMethodName,
		"platform.runtime.assistant.plan.propose":     controlplanev1.RuntimeWorkService_ProposeAssistantPlan_FullMethodName,
		"platform.runtime.run.metadata.propose":       controlplanev1.RuntimeWorkService_ProposeRunMetadata_FullMethodName,
		"platform.runtime.tool-call.record":           controlplanev1.RuntimeWorkService_RecordRunToolCall_FullMethodName,
		"platform.runtime.warm.reconcile":             controlplanev1.RuntimeWorkService_ReconcileWarmRuntime_FullMethodName,
		"platform.runtime.warm.report":                controlplanev1.RuntimeWorkService_ReportWarmRuntime_FullMethodName,
		"platform.runtime.integration.resolve":        controlplanev1.RuntimeWorkService_ResolveIntegrationInvocation_FullMethodName,
		"platform.runtime.integration.get":            controlplanev1.RuntimeWorkService_GetIntegrationInvocation_FullMethodName,
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

// SessionArchiveOperations возвращает только fenced lifecycle snapshot,
// restore, удаления PVC и object GC.
func SessionArchiveOperations() map[string]string {
	return map[string]string{
		"platform.session-archive.tasks.claim":            controlplanev1.SessionArchiveWorkService_ClaimSessionArchiveTasks_FullMethodName,
		"platform.session-archive.tasks.renew":            controlplanev1.SessionArchiveWorkService_RenewSessionArchiveTask_FullMethodName,
		"platform.session-archive.snapshot.complete":      controlplanev1.SessionArchiveWorkService_CompleteSessionSnapshot_FullMethodName,
		"platform.session-archive.restore.complete":       controlplanev1.SessionArchiveWorkService_CompleteSessionRestore_FullMethodName,
		"platform.session-archive.pvc-delete.complete":    controlplanev1.SessionArchiveWorkService_CompleteSessionPVCDeletion_FullMethodName,
		"platform.session-archive.object-delete.complete": controlplanev1.SessionArchiveWorkService_CompleteSessionObjectDeletion_FullMethodName,
		"platform.session-archive.tasks.fail":             controlplanev1.SessionArchiveWorkService_FailSessionArchiveTask_FullMethodName,
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

func InteractionGatewayOperations() map[string]string {
	return map[string]string{
		"platform.interactions.sources.list":        controlplanev1.InteractionWorkService_ListInteractionSources_FullMethodName,
		"platform.interactions.deliveries.claim":    controlplanev1.InteractionWorkService_ClaimInteractionDeliveries_FullMethodName,
		"platform.interactions.deliveries.complete": controlplanev1.InteractionWorkService_CompleteInteractionDelivery_FullMethodName,
		"platform.interactions.messages.accept":     controlplanev1.InteractionWorkService_AcceptInteractionMessage_FullMethodName,
	}
}

// ControlAPIGatewayProjectRequiredOperations возвращает операции, для которых
// proof обязан содержать повторно проверенную project boundary. Операции над
// ресурсами вне project route повторно разрешают project по самому opaque ref в
// control-plane и поэтому не доверяют locator из браузера.
func ControlAPIGatewayProjectRequiredOperations() map[string]struct{} {
	return map[string]struct{}{
		"platform.query.projects.get":                   {},
		"platform.query.memberships.list":               {},
		"platform.query.membership-candidates.list":     {},
		"platform.query.agents.list":                    {},
		"platform.query.workflows.list":                 {},
		"platform.query.artifacts.list":                 {},
		"platform.query.schedules.list":                 {},
		"platform.query.runtime-environments.list":      {},
		"platform.query.runtime-secrets.list":           {},
		"platform.query.template-variables.list":        {},
		"platform.query.role-image-revisions.list":      {},
		"platform.command.projects.update":              {},
		"platform.command.memberships.add":              {},
		"platform.command.memberships.change":           {},
		"platform.command.memberships.remove":           {},
		"platform.command.agents.create":                {},
		"platform.command.workflows.create":             {},
		"platform.command.artifacts.upload":             {},
		"platform.command.attachment-sets.create-draft": {},
		"platform.command.schedules.create":             {},
		"platform.command.runtime-environments.create":  {},
		"platform.command.runtime-secrets.create":       {},
		"platform.command.role-images.promote":          {},
		"platform.role-images.recipes.list":             {},
		"platform.role-images.recipes.manage":           {},
	}
}
