package controlplaneclient

import controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"

func EmailBridgeOperations() map[string]string {
	return map[string]string{
		"platform.email.configuration.report":   controlplanev1.RuntimeWorkService_ReportEmailConfigurationReadback_FullMethodName,
		"platform.email.authorization.resolve":  controlplanev1.RuntimeWorkService_ResolveEmailAuthorization_FullMethodName,
		"platform.email.effect-receipts.report": controlplanev1.RuntimeWorkService_ReportEmailEffectReceipt_FullMethodName,
		"platform.email.reconciliation.resolve": controlplanev1.RuntimeWorkService_ResolveEmailReconciliation_FullMethodName,
	}
}

func STTGatewayOperations() map[string]string {
	return map[string]string{
		"platform.stt.transcribe":        "/stt.v1.SpeechToTextService/Transcribe",
		"platform.stt.model-catalog.get": "/stt.v1.SpeechToTextService/GetModelCatalog",
	}
}

func SecretDraftGatewayOperations() map[string]string {
	return map[string]string{
		"platform.runtime-secret-drafts.save":      "/secretbroker.v1.SecretBrokerService/SaveSecretDraft",
		"platform.runtime-secret-drafts.validate":  "/secretbroker.v1.SecretBrokerService/ValidateSecretDraft",
		"platform.runtime-secret-drafts.publish":   "/secretbroker.v1.SecretBrokerService/PublishSecretDraft",
		"platform.runtime-secret-drafts.discard":   "/secretbroker.v1.SecretBrokerService/DiscardSecretDraft",
		"platform.runtime-secret-drafts.readiness": "/secretbroker.v1.SecretBrokerService/CheckSecretDraftReadiness",
	}
}

func STTPolicyProjectionOperations() map[string]string {
	return map[string]string{"platform.stt.policy.resolve": "/stt.v1.TranscriptionPolicyProjectionService/ResolveTranscriptionPolicy"}
}

// ControlAPIGatewayOperations возвращает закрытый owner-facing реестр.
func ControlAPIGatewayOperations() map[string]string {
	return map[string]string{
		"platform.query.email-mailbox.configurations.list":         controlplanev1.PlatformQueryService_ListEmailMailboxConfigurations_FullMethodName,
		"platform.query.email-mailbox.configurations.get":          controlplanev1.PlatformQueryService_GetEmailMailboxConfiguration_FullMethodName,
		"platform.query.email-mailbox.configurations.preview":      controlplanev1.PlatformQueryService_PreviewEmailMailboxConfiguration_FullMethodName,
		"platform.query.email-mailbox.credentials.list":            controlplanev1.PlatformQueryService_ListEmailMailboxCredentials_FullMethodName,
		"platform.query.email-mailbox.credential-receipts.get":     controlplanev1.PlatformQueryService_GetEmailMailboxCredentialReceipt_FullMethodName,
		"platform.command.email-mailbox.drafts.create":             controlplanev1.PlatformCommandService_CreateEmailMailboxDraft_FullMethodName,
		"platform.command.email-mailbox.drafts.save":               controlplanev1.PlatformCommandService_SaveEmailMailboxDraft_FullMethodName,
		"platform.command.email-mailbox.drafts.validate":           controlplanev1.PlatformCommandService_ValidateEmailMailboxDraft_FullMethodName,
		"platform.command.email-mailbox.drafts.publish":            controlplanev1.PlatformCommandService_PublishEmailMailboxDraft_FullMethodName,
		"platform.command.email-mailbox.drafts.discard":            controlplanev1.PlatformCommandService_DiscardEmailMailboxDraft_FullMethodName,
		"platform.command.email-mailbox.configurations.bind":       controlplanev1.PlatformCommandService_BindEmailMailboxConfiguration_FullMethodName,
		"platform.command.email-mailbox.configurations.unbind":     controlplanev1.PlatformCommandService_UnbindEmailMailboxConfiguration_FullMethodName,
		"platform.command.runtime-secret-drafts.save":              controlplanev1.PlatformCommandService_PrepareSaveRuntimeSecretDraft_FullMethodName,
		"platform.command.runtime-secret-drafts.impact.prepare":    controlplanev1.PlatformCommandService_PrepareRuntimeSecretDraftImpact_FullMethodName,
		"platform.query.runtime-secret-drafts.impact.get":          controlplanev1.PlatformQueryService_GetRuntimeSecretDraftImpact_FullMethodName,
		"platform.command.runtime-secret-drafts.validate":          controlplanev1.PlatformCommandService_PrepareValidateRuntimeSecretDraft_FullMethodName,
		"platform.command.runtime-secret-drafts.publish":           controlplanev1.PlatformCommandService_PreparePublishRuntimeSecretDraft_FullMethodName,
		"platform.command.runtime-secret-drafts.discard":           controlplanev1.PlatformCommandService_PrepareDiscardRuntimeSecretDraft_FullMethodName,
		"platform.query.runtime-secret-drafts.get":                 controlplanev1.PlatformQueryService_GetRuntimeSecretDraft_FullMethodName,
		"platform.command.prompt-templates.save-draft":             controlplanev1.PlatformCommandService_SavePromptTemplateDraft_FullMethodName,
		"platform.command.prompt-templates.discard-draft":          controlplanev1.PlatformCommandService_DiscardPromptTemplateDraft_FullMethodName,
		"platform.command.role-image-revisions.save-draft":         controlplanev1.PlatformCommandService_SaveRoleImageRevisionDraft_FullMethodName,
		"platform.command.role-image-revisions.discard-draft":      controlplanev1.PlatformCommandService_DiscardRoleImageRevisionDraft_FullMethodName,
		"platform.command.integration-definitions.save-draft":      controlplanev1.PlatformCommandService_SaveIntegrationDefinitionDraft_FullMethodName,
		"platform.command.integration-definitions.discard-draft":   controlplanev1.PlatformCommandService_DiscardIntegrationDefinitionDraft_FullMethodName,
		"platform.command.system-stt.save-draft":                   controlplanev1.PlatformCommandService_SaveSystemSTTConfigurationDraft_FullMethodName,
		"platform.command.system-stt.discard-draft":                controlplanev1.PlatformCommandService_DiscardSystemSTTConfigurationDraft_FullMethodName,
		"platform.query.email-effect-receipts.get":                 controlplanev1.PlatformQueryService_GetEmailEffectReceipt_FullMethodName,
		"platform.command.email-effects.reconcile":                 controlplanev1.PlatformCommandService_ReconcileEmailEffect_FullMethodName,
		"platform.command.email-mailbox.configure-credential":      controlplanev1.PlatformCommandService_ConfigureEmailMailboxCredential_FullMethodName,
		"platform.query.skill-bundles.list":                        controlplanev1.PlatformQueryService_ListSkillBundles_FullMethodName,
		"platform.query.skill-bundles.get":                         controlplanev1.PlatformQueryService_GetSkillBundle_FullMethodName,
		"platform.query.skill-bundle-revisions.list":               controlplanev1.PlatformQueryService_ListSkillBundleRevisions_FullMethodName,
		"platform.query.memory-records.list":                       controlplanev1.PlatformQueryService_ListMemoryRecords_FullMethodName,
		"platform.query.memory-records.get":                        controlplanev1.PlatformQueryService_GetMemoryRecord_FullMethodName,
		"platform.query.memory-record-revisions.list":              controlplanev1.PlatformQueryService_ListMemoryRecordRevisions_FullMethodName,
		"platform.command.skill-bundle-drafts.create":              controlplanev1.PlatformCommandService_CreateSkillBundleDraft_FullMethodName,
		"platform.command.skill-bundle-drafts.save":                controlplanev1.PlatformCommandService_SaveSkillBundleDraft_FullMethodName,
		"platform.command.skill-bundle-drafts.validate":            controlplanev1.PlatformCommandService_ValidateSkillBundleDraft_FullMethodName,
		"platform.command.skill-bundle-drafts.review":              controlplanev1.PlatformCommandService_ReviewSkillBundleDraft_FullMethodName,
		"platform.command.skill-bundle-drafts.publish":             controlplanev1.PlatformCommandService_PublishSkillBundleDraft_FullMethodName,
		"platform.command.skill-bundle-drafts.discard":             controlplanev1.PlatformCommandService_DiscardSkillBundleDraft_FullMethodName,
		"platform.command.skill-bundles.archive":                   controlplanev1.PlatformCommandService_ArchiveSkillBundle_FullMethodName,
		"platform.command.skill-bundles.restore":                   controlplanev1.PlatformCommandService_RestoreSkillBundle_FullMethodName,
		"platform.command.skill-bundles.purge":                     controlplanev1.PlatformCommandService_PurgeSkillBundle_FullMethodName,
		"platform.command.agent-skill-bundles.bind":                controlplanev1.PlatformCommandService_BindAgentSkillBundle_FullMethodName,
		"platform.command.agent-skill-bundles.unbind":              controlplanev1.PlatformCommandService_UnbindAgentSkillBundle_FullMethodName,
		"platform.command.memory-records.create":                   controlplanev1.PlatformCommandService_CreateMemoryRecord_FullMethodName,
		"platform.command.memory-records.revise":                   controlplanev1.PlatformCommandService_ReviseMemoryRecord_FullMethodName,
		"platform.command.memory-records.archive":                  controlplanev1.PlatformCommandService_ArchiveMemoryRecord_FullMethodName,
		"platform.command.memory-records.restore":                  controlplanev1.PlatformCommandService_RestoreMemoryRecord_FullMethodName,
		"platform.command.memory-records.purge":                    controlplanev1.PlatformCommandService_PurgeMemoryRecord_FullMethodName,
		"platform.command.agent-memory-records.bind":               controlplanev1.PlatformCommandService_BindAgentMemoryRecord_FullMethodName,
		"platform.command.agent-memory-records.unbind":             controlplanev1.PlatformCommandService_UnbindAgentMemoryRecord_FullMethodName,
		"platform.query.bootstrap.get":                             controlplanev1.PlatformQueryService_GetBootstrapState_FullMethodName,
		"platform.query.event-cursor.get":                          controlplanev1.PlatformQueryService_GetPlatformEventCursor_FullMethodName,
		"platform.query.overview.get":                              controlplanev1.PlatformQueryService_GetOverview_FullMethodName,
		"platform.query.capabilities.list":                         controlplanev1.PlatformQueryService_ListPlatformCapabilities_FullMethodName,
		"platform.query.runtimes.list":                             controlplanev1.PlatformQueryService_ListRuntimeSelections_FullMethodName,
		"platform.query.search":                                    controlplanev1.PlatformQueryService_SearchPlatform_FullMethodName,
		"platform.query.vfs.list":                                  controlplanev1.PlatformQueryService_ListVFSNodes_FullMethodName,
		"platform.query.vfs.search":                                controlplanev1.PlatformQueryService_SearchVFS_FullMethodName,
		"platform.query.projects.list":                             controlplanev1.PlatformQueryService_ListProjects_FullMethodName,
		"platform.query.projects.get":                              controlplanev1.PlatformQueryService_GetProject_FullMethodName,
		"platform.query.organization-memberships.list":             controlplanev1.PlatformQueryService_ListPlatformMemberships_FullMethodName,
		"platform.query.organization-membership-candidates.list":   controlplanev1.PlatformQueryService_ListPlatformMembershipCandidates_FullMethodName,
		"platform.query.memberships.list":                          controlplanev1.PlatformQueryService_ListProjectMemberships_FullMethodName,
		"platform.query.runtime-environment-drafts.get":            controlplanev1.PlatformQueryService_GetRuntimeEnvironmentDraft_FullMethodName,
		"platform.query.interaction-identities.list":               controlplanev1.PlatformQueryService_ListInteractionIdentities_FullMethodName,
		"platform.command.interaction-identities.bind":             controlplanev1.PlatformCommandService_BindInteractionIdentity_FullMethodName,
		"platform.command.interaction-identities.revoke":           controlplanev1.PlatformCommandService_RevokeInteractionIdentity_FullMethodName,
		"platform.query.runtime-environments.impact":               controlplanev1.PlatformQueryService_GetRuntimeEnvironmentImpact_FullMethodName,
		"platform.query.runtime-secrets.impact":                    controlplanev1.PlatformQueryService_GetRuntimeSecretImpact_FullMethodName,
		"platform.command.runtime-secrets.rebind":                  controlplanev1.PlatformCommandService_RebindRuntimeSecret_FullMethodName,
		"platform.command.runtime-environments.rebind":             controlplanev1.PlatformCommandService_RebindRuntimeEnvironment_FullMethodName,
		"platform.command.runtime-environment-drafts.create":       controlplanev1.PlatformCommandService_CreateRuntimeEnvironmentDraft_FullMethodName,
		"platform.command.runtime-environment-drafts.save":         controlplanev1.PlatformCommandService_SaveRuntimeEnvironmentDraft_FullMethodName,
		"platform.command.runtime-environment-drafts.validate":     controlplanev1.PlatformCommandService_ValidateRuntimeEnvironmentDraft_FullMethodName,
		"platform.command.runtime-environment-drafts.publish":      controlplanev1.PlatformCommandService_PublishRuntimeEnvironmentDraft_FullMethodName,
		"platform.command.runtime-environment-drafts.discard":      controlplanev1.PlatformCommandService_DiscardRuntimeEnvironmentDraft_FullMethodName,
		"platform.query.membership-candidates.list":                controlplanev1.PlatformQueryService_ListProjectMembershipCandidates_FullMethodName,
		"platform.query.agents.list":                               controlplanev1.PlatformQueryService_ListAgents_FullMethodName,
		"platform.query.agents.get":                                controlplanev1.PlatformQueryService_GetAgent_FullMethodName,
		"platform.query.agent-instruction-versions.list":           controlplanev1.PlatformQueryService_ListAgentInstructionVersions_FullMethodName,
		"platform.query.workflows.list":                            controlplanev1.PlatformQueryService_ListWorkflows_FullMethodName,
		"platform.query.workflows.get":                             controlplanev1.PlatformQueryService_GetWorkflow_FullMethodName,
		"platform.query.runs.list":                                 controlplanev1.PlatformQueryService_ListRuns_FullMethodName,
		"platform.query.runs.get":                                  controlplanev1.PlatformQueryService_GetRun_FullMethodName,
		"platform.query.runtime-revisions.diff":                    controlplanev1.PlatformQueryService_GetRuntimeRevisionDiff_FullMethodName,
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
		"platform.query.schedules.preview":                         controlplanev1.PlatformQueryService_PreviewSchedule_FullMethodName,
		"platform.query.provider-definitions.list":                 controlplanev1.PlatformQueryService_ListProviderDefinitions_FullMethodName,
		"platform.query.models.list":                               controlplanev1.PlatformQueryService_ListModelCapabilities_FullMethodName,
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
		"platform.query.managed-configurations.history.list":       controlplanev1.PlatformQueryService_ListManagedConfigurationHistory_FullMethodName,
		"platform.query.managed-configurations.list":               controlplanev1.PlatformQueryService_ListManagedConfigurations_FullMethodName,
		"platform.query.managed-configurations.impact.get":         controlplanev1.PlatformQueryService_GetManagedConfigurationImpact_FullMethodName,
		"platform.query.system-stt.get":                            controlplanev1.PlatformQueryService_GetSystemSTTConfiguration_FullMethodName,
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
		"platform.command.agents.avatar.upload":                    controlplanev1.PlatformCommandService_UploadAgentAvatar_FullMethodName,
		"platform.command.artifacts.upload":                        controlplanev1.PlatformCommandService_UploadArtifact_FullMethodName,
		"platform.command.organization-artifacts.upload":           controlplanev1.PlatformCommandService_UploadOrganizationArtifact_FullMethodName,
		"platform.command.attachment-sets.create-draft":            controlplanev1.PlatformCommandService_CreateAttachmentSetDraft_FullMethodName,
		"platform.command.organization-attachment-sets.create":     controlplanev1.PlatformCommandService_CreateOrganizationAttachmentSetDraft_FullMethodName,
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
		"platform.command.provider-accounts.device-verify":         controlplanev1.PlatformCommandService_VerifyProviderAccountDeviceAuthorization_FullMethodName,
		"platform.command.provider-accounts.device-reauthorize":    controlplanev1.PlatformCommandService_ReauthorizeProviderAccountDeviceCode_FullMethodName,
		"platform.command.provider-accounts.revoke":                controlplanev1.PlatformCommandService_RevokeProviderAccount_FullMethodName,
		"platform.command.provider-accounts.delete":                controlplanev1.PlatformCommandService_DeleteProviderAccount_FullMethodName,
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
		"platform.command.prompt-templates.create-draft":           controlplanev1.PlatformCommandService_CreatePromptTemplateDraft_FullMethodName,
		"platform.command.prompt-templates.validate":               controlplanev1.PlatformCommandService_ValidatePromptTemplateDraft_FullMethodName,
		"platform.command.prompt-templates.publish":                controlplanev1.PlatformCommandService_PublishPromptTemplateDraft_FullMethodName,
		"platform.command.prompt-templates.rebind":                 controlplanev1.PlatformCommandService_RebindPromptTemplateConsumers_FullMethodName,
		"platform.command.role-image-revisions.create-draft":       controlplanev1.PlatformCommandService_CreateRoleImageRevisionDraft_FullMethodName,
		"platform.command.role-image-revisions.validate":           controlplanev1.PlatformCommandService_ValidateRoleImageRevisionDraft_FullMethodName,
		"platform.command.role-image-revisions.publish":            controlplanev1.PlatformCommandService_PublishRoleImageRevisionDraft_FullMethodName,
		"platform.command.role-image-revisions.rebind":             controlplanev1.PlatformCommandService_RebindRoleImageConsumers_FullMethodName,
		"platform.command.integration-definitions.create-draft":    controlplanev1.PlatformCommandService_CreateIntegrationDefinitionDraft_FullMethodName,
		"platform.command.integration-definitions.validate":        controlplanev1.PlatformCommandService_ValidateIntegrationDefinitionDraft_FullMethodName,
		"platform.command.integration-definitions.publish":         controlplanev1.PlatformCommandService_PublishIntegrationDefinitionDraft_FullMethodName,
		"platform.command.integration-definitions.rebind":          controlplanev1.PlatformCommandService_RebindIntegrationDefinitionConsumers_FullMethodName,
		"platform.command.system-stt.create-draft":                 controlplanev1.PlatformCommandService_CreateSystemSTTConfigurationDraft_FullMethodName,
		"platform.command.system-stt.validate":                     controlplanev1.PlatformCommandService_ValidateSystemSTTConfigurationDraft_FullMethodName,
		"platform.command.system-stt.publish":                      controlplanev1.PlatformCommandService_PublishSystemSTTConfigurationDraft_FullMethodName,
		"platform.command.system-stt.rebind":                       controlplanev1.PlatformCommandService_RebindSystemSTTConsumers_FullMethodName,
		"platform.command.managed-configurations.detach":           controlplanev1.PlatformCommandService_DetachGitManagedConfiguration_FullMethodName,
		"platform.command.managed-configurations.copy":             controlplanev1.PlatformCommandService_CopyGitManagedConfiguration_FullMethodName,
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
		"platform.assistant.conversations.archive":                 controlplanev1.SystemAssistantService_ArchiveAssistantConversation_FullMethodName,
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
		"platform.runtime-secret-drafts.readiness.check":         controlplanev1.RuntimeSecretDraftWorkService_CheckRuntimeSecretDraftWorkReadiness_FullMethodName,
		"platform.runtime-secret-drafts.operations.consume":      controlplanev1.RuntimeSecretDraftWorkService_ConsumeRuntimeSecretDraftOperation_FullMethodName,
		"platform.runtime-secret-drafts.operations.complete":     controlplanev1.RuntimeSecretDraftWorkService_CompleteRuntimeSecretDraftOperation_FullMethodName,
		"platform.runtime-secret-drafts.operations.fail":         controlplanev1.RuntimeSecretDraftWorkService_FailRuntimeSecretDraftOperation_FullMethodName,
		"platform.runtime-secret-drafts.operations.recover":      controlplanev1.RuntimeSecretDraftWorkService_ListRuntimeSecretDraftRecoveryWork_FullMethodName,
		"platform.runtime-secret-drafts.materialization.recover": controlplanev1.RuntimeSecretDraftWorkService_RecoverRuntimeSecretDraftMaterialization_FullMethodName,
		"platform.runtime-secret-drafts.cleanup.complete":        controlplanev1.RuntimeSecretDraftWorkService_CompleteRuntimeSecretDraftCleanup_FullMethodName,
		"platform.runtime-secrets.readiness.check":               controlplanev1.RuntimeSecretWorkService_CheckRuntimeSecretWorkReadiness_FullMethodName,
		"platform.credential-projections.readiness.check":        controlplanev1.RuntimeSecretWorkService_CheckCredentialProjectionWorkReadiness_FullMethodName,
		"platform.credential-projections.runtime.resolve":        controlplanev1.RuntimeSecretWorkService_ResolveRuntimeCredentialProjection_FullMethodName,
		"platform.credential-projections.runtime.validate":       controlplanev1.RuntimeSecretWorkService_ValidateRuntimeCredentialProjection_FullMethodName,
		"platform.credential-projections.stt.resolve":            controlplanev1.RuntimeSecretWorkService_ResolveTranscriptionCredentialProjection_FullMethodName,
		"platform.runtime-secrets.operations.consume":            controlplanev1.RuntimeSecretWorkService_ConsumeRuntimeSecretOperation_FullMethodName,
		"platform.runtime-secrets.operations.complete":           controlplanev1.RuntimeSecretWorkService_CompleteRuntimeSecretOperation_FullMethodName,
		"platform.runtime-secrets.operations.fail":               controlplanev1.RuntimeSecretWorkService_FailRuntimeSecretOperation_FullMethodName,
		"platform.runtime-secrets.operations.recover":            controlplanev1.RuntimeSecretWorkService_ListRuntimeSecretRecoveryWork_FullMethodName,
		"platform.runtime-secrets.materialization.recover":       controlplanev1.RuntimeSecretWorkService_RecoverRuntimeSecretMaterialization_FullMethodName,
	}
}

// ProviderCredentialMaterializerOperations возвращает exact API изолированного
// materializer, ответы которого содержат только Secret descriptors.
func ProviderCredentialMaterializerOperations() map[string]string {
	return map[string]string{
		"platform.provider-credentials.readiness.check":         controlplanev1.ProviderCredentialMaterializerService_CheckProviderCredentialMaterializerReadiness_FullMethodName,
		"platform.provider-credentials.device-authorize.start":  controlplanev1.ProviderCredentialMaterializerService_StartDeviceAuthorization_FullMethodName,
		"platform.provider-credentials.device-authorize.get":    controlplanev1.ProviderCredentialMaterializerService_ObserveDeviceAuthorization_FullMethodName,
		"platform.provider-credentials.api-key.materialize":     controlplanev1.ProviderCredentialMaterializerService_MaterializeAPIKey_FullMethodName,
		"platform.provider-credentials.materialization.discard": controlplanev1.ProviderCredentialMaterializerService_DiscardProviderCredentialMaterialization_FullMethodName,
		"platform.provider-credentials.cleanup":                 controlplanev1.ProviderCredentialMaterializerService_CleanupProviderCredential_FullMethodName,
	}
}

// RuntimeCredentialProjectionOperations возвращает exact secret-broker API,
// который материализует и проверяет credentials одной execution lease.
func RuntimeCredentialProjectionOperations() map[string]string {
	return map[string]string{
		"platform.runtime.credentials.materialize":                  "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeRuntimeCredentials",
		"platform.runtime.credentials.system-assistant.materialize": "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeSystemAssistantCredentials",
		"platform.runtime.credentials.readiness.check":              "/secretbroker.v1.RuntimeCredentialProjectionService/CheckRuntimeCredentialProjectionReadiness",
	}
}

// STTCredentialProjectionOperations возвращает exact credential producer API
// для одного защищённого STT-запроса.
func STTCredentialProjectionOperations() map[string]string {
	return map[string]string{
		"platform.stt.credential.project": "/stt.v1.TranscriptionCredentialProjectionService/ProjectTranscriptionCredential",
	}
}

func RuntimeOperations() map[string]string {
	return map[string]string{
		"platform.runtime.execution.claim":                    controlplanev1.RuntimeWorkService_ClaimExecution_FullMethodName,
		"platform.runtime.role-image-configuration.get":       controlplanev1.RuntimeWorkService_GetRuntimeEnvironmentRoleImageConfiguration_FullMethodName,
		"platform.runtime.execution.artifact.read":            controlplanev1.RuntimeWorkService_ReadExecutionArtifact_FullMethodName,
		"platform.runtime.execution.renew":                    controlplanev1.RuntimeWorkService_RenewExecution_FullMethodName,
		"platform.runtime.execution.progress":                 controlplanev1.RuntimeWorkService_ReportExecutionProgress_FullMethodName,
		"platform.runtime.provider-credential.refresh.commit": controlplanev1.RuntimeWorkService_CommitProviderCredentialRefresh_FullMethodName,
		"platform.runtime.execution.complete":                 controlplanev1.RuntimeWorkService_CompleteExecution_FullMethodName,
		"platform.runtime.execution.delegate":                 controlplanev1.RuntimeWorkService_DelegateExecution_FullMethodName,
		"platform.runtime.assistant.metadata.propose":         controlplanev1.RuntimeWorkService_ProposeAssistantMetadata_FullMethodName,
		"platform.runtime.assistant.plan.propose":             controlplanev1.RuntimeWorkService_ProposeAssistantPlan_FullMethodName,
		"platform.runtime.run.metadata.propose":               controlplanev1.RuntimeWorkService_ProposeRunMetadata_FullMethodName,
		"platform.runtime.tool-call.record":                   controlplanev1.RuntimeWorkService_RecordRunToolCall_FullMethodName,
		"platform.runtime.warm.reconcile":                     controlplanev1.RuntimeWorkService_ReconcileWarmRuntime_FullMethodName,
		"platform.runtime.warm.report":                        controlplanev1.RuntimeWorkService_ReportWarmRuntime_FullMethodName,
		"platform.runtime.integration.resolve":                controlplanev1.RuntimeWorkService_ResolveIntegrationInvocation_FullMethodName,
		"platform.runtime.integration.get":                    controlplanev1.RuntimeWorkService_GetIntegrationInvocation_FullMethodName,
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
		"platform.runtime.schedules.renew":       controlplanev1.RuntimeWorkService_RenewScheduleOccurrence_FullMethodName,
		"platform.runtime.schedules.materialize": controlplanev1.RuntimeWorkService_MaterializeScheduleOccurrence_FullMethodName,
		"platform.runtime.schedules.fail":        controlplanev1.RuntimeWorkService_FailScheduleOccurrence_FullMethodName,
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
		"platform.runtime.integration-definition.get": controlplanev1.RuntimeWorkService_GetIntegrationConnectionDefinitionConfiguration_FullMethodName,
		"platform.runtime.integration-tests.claim":    controlplanev1.RuntimeWorkService_ClaimIntegrationConnectionTests_FullMethodName,
		"platform.runtime.integration-tests.complete": controlplanev1.RuntimeWorkService_CompleteIntegrationConnectionTest_FullMethodName,
		"platform.runtime.integrations.claim":         controlplanev1.RuntimeWorkService_ClaimIntegrationInvocations_FullMethodName,
		"platform.runtime.integrations.complete":      controlplanev1.RuntimeWorkService_CompleteIntegrationInvocation_FullMethodName,
	}
}

func InteractionGatewayOperations() map[string]string {
	return map[string]string{
		"platform.interactions.connection-tests.claim":    controlplanev1.RuntimeWorkService_ClaimIntegrationConnectionTests_FullMethodName,
		"platform.interactions.connection-tests.complete": controlplanev1.RuntimeWorkService_CompleteIntegrationConnectionTest_FullMethodName,
		"platform.interactions.invocations.claim":         controlplanev1.RuntimeWorkService_ClaimIntegrationInvocations_FullMethodName,
		"platform.interactions.invocations.complete":      controlplanev1.RuntimeWorkService_CompleteIntegrationInvocation_FullMethodName,
		"platform.interactions.sources.list":              controlplanev1.InteractionWorkService_ListInteractionSources_FullMethodName,
		"platform.interactions.deliveries.claim":          controlplanev1.InteractionWorkService_ClaimInteractionDeliveries_FullMethodName,
		"platform.interactions.deliveries.complete":       controlplanev1.InteractionWorkService_CompleteInteractionDelivery_FullMethodName,
		"platform.interactions.messages.accept":           controlplanev1.InteractionWorkService_AcceptInteractionMessage_FullMethodName,
	}
}

// ControlAPIGatewayProjectRequiredOperations возвращает операции, для которых
// proof обязан содержать повторно проверенную project boundary. Операции над
// ресурсами вне project route повторно разрешают project по самому opaque ref в
// control-plane и поэтому не доверяют locator из браузера.
func ControlAPIGatewayProjectRequiredOperations() map[string]struct{} {
	return map[string]struct{}{
		"platform.command.skill-bundle-drafts.create":           {},
		"platform.command.memory-records.create":                {},
		"platform.query.projects.get":                           {},
		"platform.query.membership-candidates.list":             {},
		"platform.query.template-variables.list":                {},
		"platform.query.role-image-revisions.list":              {},
		"platform.command.projects.update":                      {},
		"platform.command.memberships.add":                      {},
		"platform.command.memberships.change":                   {},
		"platform.command.memberships.remove":                   {},
		"platform.command.agents.create":                        {},
		"platform.command.agents.avatar.upload":                 {},
		"platform.command.workflows.create":                     {},
		"platform.command.artifacts.upload":                     {},
		"platform.command.attachment-sets.create-draft":         {},
		"platform.command.schedules.create":                     {},
		"platform.command.runtime-environments.create":          {},
		"platform.command.prompt-templates.create-draft":        {},
		"platform.command.role-image-revisions.create-draft":    {},
		"platform.command.integration-definitions.create-draft": {},
		"platform.command.runtime-secrets.create":               {},
		"platform.command.role-images.promote":                  {},
		"platform.role-images.recipes.list":                     {},
		"platform.role-images.recipes.manage":                   {},
	}
}
