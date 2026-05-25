// Package accesscatalog contains shared system access action keys.
package accesscatalog

import "strings"

// ActionDescriptor describes one code-owned system access action.
type ActionDescriptor struct {
	Key          string
	ResourceType string
}

var systemActionsByKey = actionDescriptorCatalog(SystemActions())

const (
	ScopeGlobal       = "global"
	ScopeOrganization = "organization"
	ScopeProject      = "project"
	ScopeRepository   = "repository"
)

const (
	ResourceProject                      = "project"
	ResourceRepository                   = "repository"
	ResourceServicesPolicy               = "services_policy"
	ResourcePolicyOverride               = "policy_override"
	ResourceDocumentationSource          = "documentation_source"
	ResourceBranchRules                  = "branch_rules"
	ResourceReleasePolicy                = "release_policy"
	ResourceReleaseLine                  = "release_line"
	ResourcePlacementPolicy              = "placement_policy"
	ResourcePackageSource                = "package_source"
	ResourcePackageCatalog               = "package_catalog"
	ResourcePackage                      = "package"
	ResourcePackageVersion               = "package_version"
	ResourcePackageManifest              = "package_manifest"
	ResourcePackageInstallation          = "package_installation"
	ResourcePackageInstallationSecretRef = "package_installation_secret_ref"
	ResourcePackageSecretSchema          = "package_secret_schema"
	ResourceProviderWorkItem             = "provider_work_item"
	ResourceProviderIssue                = "provider_issue"
	ResourceProviderPullRequest          = "provider_pull_request"
	ResourceProviderRepository           = "provider_repository"
	ResourceProviderComment              = "provider_comment"
	ResourceProviderReviewSignal         = "provider_review_signal"
	ResourceProviderRelationship         = "provider_relationship"
	ResourceProviderReconciliation       = "provider_reconciliation"
	ResourceRuntimeSlot                  = "runtime_slot"
	ResourceRuntimeWorkspace             = "runtime_workspace_materialization"
	ResourceRuntimeJob                   = "runtime_job"
	ResourceRuntimeArtifactRef           = "runtime_artifact_ref"
	ResourceRuntimeCleanupPolicy         = "runtime_cleanup_policy"
	ResourceRuntimePrewarmPool           = "runtime_prewarm_pool"
	ResourceFleetScope                   = "fleet_scope"
	ResourceFleetServer                  = "fleet_server"
	ResourceFleetCluster                 = "fleet_cluster"
	ResourceFleetHealth                  = "fleet_health"
	ResourceFleetPlacementRule           = "fleet_placement_rule"
	ResourceFleetPlacementDecision       = "fleet_placement_decision"
	ResourceAgentFlow                    = "agent_flow"
	ResourceAgentRole                    = "agent_role"
	ResourceAgentPrompt                  = "agent_prompt"
	ResourceAgentSession                 = "agent_session"
	ResourceAgentRun                     = "agent_run"
	ResourceAgentAcceptance              = "agent_acceptance"
	ResourceAgentFollowUp                = "agent_follow_up"
	ResourceAgentHumanGate               = "agent_human_gate"
	ResourceGovernanceRiskProfile        = "governance_risk_profile"
	ResourceGovernanceRiskAssessment     = "governance_risk_assessment"
	ResourceGovernanceSignal             = "governance_signal"
	ResourceGovernanceGate               = "governance_gate"
	ResourceGovernanceReleaseDecision    = "governance_release_decision"
	ResourceGovernanceReleaseSafetyState = "governance_release_safety_state"
	ResourceInteractionThread            = "interaction_thread"
	ResourceInteractionMessage           = "interaction_message"
	ResourceInteractionRequest           = "interaction_request"
	ResourceInteractionResponse          = "interaction_response"
	ResourceInteractionNotification      = "interaction_notification"
	ResourceInteractionSubscription      = "interaction_subscription"
	ResourceInteractionDelivery          = "interaction_delivery"
	ResourceInteractionCallback          = "interaction_callback"
)

const (
	ActionProjectCreate                         = "project.create"
	ActionProjectUpdate                         = "project.update"
	ActionProjectRead                           = "project.read"
	ActionProjectList                           = "project.list"
	ActionRepositoryAttach                      = "repository.attach"
	ActionRepositoryUpdate                      = "repository.update"
	ActionRepositoryDetach                      = "repository.detach"
	ActionRepositoryRead                        = "repository.read"
	ActionRepositoryList                        = "repository.list"
	ActionProjectPolicyImport                   = "project.policy.import"
	ActionProjectPolicyRead                     = "project.policy.read"
	ActionProjectPolicyPropose                  = "project.policy.propose"
	ActionProjectPolicyOverride                 = "project.policy.override"
	ActionProjectPolicyOverrideRead             = "project.policy.override.read"
	ActionProjectPolicyOverrideCancel           = "project.policy.override.cancel"
	ActionProjectDocsUpdate                     = "project.docs.update"
	ActionProjectDocsRead                       = "project.docs.read"
	ActionProjectWorkspaceRead                  = "project.workspace.read"
	ActionProjectBranchRulesUpdate              = "project.branch_rules.update"
	ActionProjectBranchRulesRead                = "project.branch_rules.read"
	ActionProjectReleasePolicyUpdate            = "project.release_policy.update"
	ActionProjectReleasePolicyRead              = "project.release_policy.read"
	ActionProjectReleaseLineUpdate              = "project.release_line.update"
	ActionProjectReleaseLineRead                = "project.release_line.read"
	ActionProjectPlacementUpdate                = "project.placement_policy.update"
	ActionProjectPlacementRead                  = "project.placement_policy.read"
	ActionPackageSourceConnect                  = "package.source.connect"
	ActionPackageSourceUpdate                   = "package.source.update"
	ActionPackageSourceDisable                  = "package.source.disable"
	ActionPackageSourceRead                     = "package.source.read"
	ActionPackageCatalogSync                    = "package.catalog.sync"
	ActionPackageCatalogRead                    = "package.catalog.read"
	ActionPackageManifestRead                   = "package.manifest.read"
	ActionPackageInstall                        = "package.install"
	ActionPackageInstallationUpdate             = "package.installation.update"
	ActionPackageInstallationDisable            = "package.installation.disable"
	ActionPackageUninstall                      = "package.uninstall"
	ActionPackageInstallationRead               = "package.installation.read"
	ActionPackageInstallationSecretRefRead      = "package.installation.secret_ref.read"
	ActionPackageSecretRead                     = "package.secret.read"
	ActionPackageVerify                         = "package.verify"
	ActionProviderWorkItemRead                  = "provider.work_item.read"
	ActionProviderIssueWrite                    = "provider.issue.write"
	ActionProviderPullRequestWrite              = "provider.pull_request.write"
	ActionProviderRepositoryWrite               = "provider.repository.write"
	ActionProviderCommentWrite                  = "provider.comment.write"
	ActionProviderReviewSignalWrite             = "provider.review_signal.write"
	ActionProviderRelationshipWrite             = "provider.relationship.write"
	ActionProviderReconciliationRun             = "provider.reconciliation.run"
	ActionRuntimeSlotReserve                    = "runtime.slot.reserve"
	ActionRuntimeSlotExtendLease                = "runtime.slot.lease.extend"
	ActionRuntimeSlotRelease                    = "runtime.slot.release"
	ActionRuntimeSlotFail                       = "runtime.slot.fail"
	ActionRuntimeSlotRead                       = "runtime.slot.read"
	ActionRuntimeSlotList                       = "runtime.slot.list"
	ActionRuntimeWorkspaceMaterializationStart  = "runtime.workspace.materialization.start"
	ActionRuntimeWorkspaceMaterializationReport = "runtime.workspace.materialization.report"
	ActionRuntimeWorkspaceMaterializationRead   = "runtime.workspace.materialization.read"
	ActionRuntimeWorkspaceMaterializationList   = "runtime.workspace.materialization.list"
	ActionRuntimeJobCreate                      = "runtime.job.create"
	ActionRuntimeJobClaim                       = "runtime.job.claim"
	ActionRuntimeJobStepReport                  = "runtime.job.step.report"
	ActionRuntimeJobComplete                    = "runtime.job.complete"
	ActionRuntimeJobFail                        = "runtime.job.fail"
	ActionRuntimeJobCancel                      = "runtime.job.cancel"
	ActionRuntimeJobRead                        = "runtime.job.read"
	ActionRuntimeJobList                        = "runtime.job.list"
	ActionRuntimeArtifactRefRecord              = "runtime.artifact_ref.record"
	ActionRuntimeArtifactRefList                = "runtime.artifact_ref.list"
	ActionRuntimeCleanupPolicyUpsert            = "runtime.cleanup_policy.upsert"
	ActionRuntimeCleanupRun                     = "runtime.cleanup.run"
	ActionRuntimePrewarmPoolUpsert              = "runtime.prewarm_pool.upsert"
	ActionRuntimePrewarmPoolReconcile           = "runtime.prewarm_pool.reconcile"
	ActionFleetScopeCreate                      = "fleet.scope.create"
	ActionFleetScopeUpdate                      = "fleet.scope.update"
	ActionFleetScopeDisable                     = "fleet.scope.disable"
	ActionFleetScopeEnable                      = "fleet.scope.enable"
	ActionFleetScopeRead                        = "fleet.scope.read"
	ActionFleetScopeList                        = "fleet.scope.list"
	ActionFleetServerRegister                   = "fleet.server.register"
	ActionFleetServerUpdate                     = "fleet.server.update"
	ActionFleetServerDisable                    = "fleet.server.disable"
	ActionFleetServerEnable                     = "fleet.server.enable"
	ActionFleetServerRead                       = "fleet.server.read"
	ActionFleetServerList                       = "fleet.server.list"
	ActionFleetClusterRegister                  = "fleet.cluster.register"
	ActionFleetClusterUpdate                    = "fleet.cluster.update"
	ActionFleetClusterDisable                   = "fleet.cluster.disable"
	ActionFleetClusterEnable                    = "fleet.cluster.enable"
	ActionFleetClusterRead                      = "fleet.cluster.read"
	ActionFleetClusterList                      = "fleet.cluster.list"
	ActionFleetHealthCheckRun                   = "fleet.health.check.run"
	ActionFleetHealthRead                       = "fleet.health.read"
	ActionFleetPlacementRulePut                 = "fleet.placement_rule.put"
	ActionFleetPlacementRuleRead                = "fleet.placement_rule.read"
	ActionFleetPlacementRuleList                = "fleet.placement_rule.list"
	ActionFleetPlacementResolve                 = "fleet.placement.resolve"
	ActionFleetPlacementDecisionRead            = "fleet.placement_decision.read"
	ActionFleetPlacementDecisionList            = "fleet.placement_decision.list"
	ActionAgentFlowRead                         = "agent.flow.read"
	ActionAgentFlowManage                       = "agent.flow.manage"
	ActionAgentRoleRead                         = "agent.role.read"
	ActionAgentRoleManage                       = "agent.role.manage"
	ActionAgentPromptRead                       = "agent.prompt.read"
	ActionAgentPromptManage                     = "agent.prompt.manage"
	ActionAgentSessionStart                     = "agent.session.start"
	ActionAgentSessionRead                      = "agent.session.read"
	ActionAgentSessionUpdate                    = "agent.session.update"
	ActionAgentRunStart                         = "agent.run.start"
	ActionAgentRunRead                          = "agent.run.read"
	ActionAgentRunUpdate                        = "agent.run.update"
	ActionAgentAcceptanceRun                    = "agent.acceptance.run"
	ActionAgentAcceptanceRead                   = "agent.acceptance.read"
	ActionAgentAcceptanceUpdate                 = "agent.acceptance.update"
	ActionAgentFollowUpCreate                   = "agent.follow_up.create"
	ActionAgentHumanGateRequest                 = "agent.human_gate.request"
	ActionGovernancePolicyManage                = "governance.policy.manage"
	ActionGovernancePolicyRead                  = "governance.policy.read"
	ActionGovernanceRiskEvaluate                = "governance.risk.evaluate"
	ActionGovernanceRiskRead                    = "governance.risk.read"
	ActionGovernanceSignalRecord                = "governance.signal.record"
	ActionGovernanceSignalRead                  = "governance.signal.read"
	ActionGovernanceSignalResolve               = "governance.signal.resolve"
	ActionGovernanceGateRequest                 = "governance.gate.request"
	ActionGovernanceGateRead                    = "governance.gate.read"
	ActionGovernanceGateDecide                  = "governance.gate.decide"
	ActionGovernanceReleasePrepare              = "governance.release.prepare"
	ActionGovernanceReleaseRequest              = "governance.release.request"
	ActionGovernanceReleaseRead                 = "governance.release.read"
	ActionGovernanceReleaseDecide               = "governance.release.decide"
	ActionGovernanceReleaseUpdate               = "governance.release.update"
	ActionInteractionThreadCreate               = "interaction.thread.create"
	ActionInteractionThreadRead                 = "interaction.thread.read"
	ActionInteractionMessageRecord              = "interaction.message.record"
	ActionInteractionMessageRead                = "interaction.message.read"
	ActionInteractionFeedbackRequest            = "interaction.feedback.request"
	ActionInteractionApprovalRequest            = "interaction.approval.request"
	ActionInteractionHumanGateRequest           = "interaction.human_gate.request"
	ActionInteractionRequestRespond             = "interaction.request.respond"
	ActionInteractionRequestCancel              = "interaction.request.cancel"
	ActionInteractionRequestExpire              = "interaction.request.expire"
	ActionInteractionRequestRead                = "interaction.request.read"
	ActionInteractionNotificationRequest        = "interaction.notification.request"
	ActionInteractionSubscriptionManage         = "interaction.subscription.manage"
	ActionInteractionSubscriptionRead           = "interaction.subscription.read"
	ActionInteractionDeliveryPlan               = "interaction.delivery.plan"
	ActionInteractionDeliveryUpdate             = "interaction.delivery.update"
	ActionInteractionDeliveryRead               = "interaction.delivery.read"
	ActionInteractionCallbackRecord             = "interaction.callback.record"
)

// ProjectCatalogActions returns system actions owned by the projects-and-repositories domain.
func ProjectCatalogActions() []ActionDescriptor {
	return []ActionDescriptor{
		{Key: ActionProjectCreate, ResourceType: ResourceProject},
		{Key: ActionProjectUpdate, ResourceType: ResourceProject},
		{Key: ActionProjectRead, ResourceType: ResourceProject},
		{Key: ActionProjectList, ResourceType: ResourceProject},
		{Key: ActionRepositoryAttach, ResourceType: ResourceRepository},
		{Key: ActionRepositoryUpdate, ResourceType: ResourceRepository},
		{Key: ActionRepositoryDetach, ResourceType: ResourceRepository},
		{Key: ActionRepositoryRead, ResourceType: ResourceRepository},
		{Key: ActionRepositoryList, ResourceType: ResourceRepository},
		{Key: ActionProjectPolicyImport, ResourceType: ResourceServicesPolicy},
		{Key: ActionProjectPolicyRead, ResourceType: ResourceServicesPolicy},
		{Key: ActionProjectPolicyPropose, ResourceType: ResourceServicesPolicy},
		{Key: ActionProjectPolicyOverride, ResourceType: ResourcePolicyOverride},
		{Key: ActionProjectPolicyOverrideRead, ResourceType: ResourcePolicyOverride},
		{Key: ActionProjectPolicyOverrideCancel, ResourceType: ResourcePolicyOverride},
		{Key: ActionProjectDocsUpdate, ResourceType: ResourceDocumentationSource},
		{Key: ActionProjectDocsRead, ResourceType: ResourceDocumentationSource},
		{Key: ActionProjectWorkspaceRead, ResourceType: ResourceProject},
		{Key: ActionProjectBranchRulesUpdate, ResourceType: ResourceBranchRules},
		{Key: ActionProjectBranchRulesRead, ResourceType: ResourceBranchRules},
		{Key: ActionProjectReleasePolicyUpdate, ResourceType: ResourceReleasePolicy},
		{Key: ActionProjectReleasePolicyRead, ResourceType: ResourceReleasePolicy},
		{Key: ActionProjectReleaseLineUpdate, ResourceType: ResourceReleaseLine},
		{Key: ActionProjectReleaseLineRead, ResourceType: ResourceReleaseLine},
		{Key: ActionProjectPlacementUpdate, ResourceType: ResourcePlacementPolicy},
		{Key: ActionProjectPlacementRead, ResourceType: ResourcePlacementPolicy},
	}
}

// PackageHubActions returns system actions owned by the package platform domain.
func PackageHubActions() []ActionDescriptor {
	return []ActionDescriptor{
		{Key: ActionPackageSourceConnect, ResourceType: ResourcePackageSource},
		{Key: ActionPackageSourceUpdate, ResourceType: ResourcePackageSource},
		{Key: ActionPackageSourceDisable, ResourceType: ResourcePackageSource},
		{Key: ActionPackageSourceRead, ResourceType: ResourcePackageSource},
		{Key: ActionPackageCatalogSync, ResourceType: ResourcePackageCatalog},
		{Key: ActionPackageCatalogRead, ResourceType: ResourcePackage},
		{Key: ActionPackageManifestRead, ResourceType: ResourcePackageManifest},
		{Key: ActionPackageInstall, ResourceType: ResourcePackageInstallation},
		{Key: ActionPackageInstallationUpdate, ResourceType: ResourcePackageInstallation},
		{Key: ActionPackageInstallationDisable, ResourceType: ResourcePackageInstallation},
		{Key: ActionPackageUninstall, ResourceType: ResourcePackageInstallation},
		{Key: ActionPackageInstallationRead, ResourceType: ResourcePackageInstallation},
		{Key: ActionPackageInstallationSecretRefRead, ResourceType: ResourcePackageInstallationSecretRef},
		{Key: ActionPackageSecretRead, ResourceType: ResourcePackageSecretSchema},
		{Key: ActionPackageVerify, ResourceType: ResourcePackageVersion},
	}
}

// ProviderHubActions returns system actions owned by the provider-native work items domain.
func ProviderHubActions() []ActionDescriptor {
	return []ActionDescriptor{
		{Key: ActionProviderWorkItemRead, ResourceType: ResourceProviderWorkItem},
		{Key: ActionProviderIssueWrite, ResourceType: ResourceProviderIssue},
		{Key: ActionProviderPullRequestWrite, ResourceType: ResourceProviderPullRequest},
		{Key: ActionProviderRepositoryWrite, ResourceType: ResourceProviderRepository},
		{Key: ActionProviderCommentWrite, ResourceType: ResourceProviderComment},
		{Key: ActionProviderReviewSignalWrite, ResourceType: ResourceProviderReviewSignal},
		{Key: ActionProviderRelationshipWrite, ResourceType: ResourceProviderRelationship},
		{Key: ActionProviderReconciliationRun, ResourceType: ResourceProviderReconciliation},
	}
}

// RuntimeManagerActions returns system actions owned by the runtime-and-fleet domain.
func RuntimeManagerActions() []ActionDescriptor {
	return []ActionDescriptor{
		{Key: ActionRuntimeSlotReserve, ResourceType: ResourceRuntimeSlot},
		{Key: ActionRuntimeSlotExtendLease, ResourceType: ResourceRuntimeSlot},
		{Key: ActionRuntimeSlotRelease, ResourceType: ResourceRuntimeSlot},
		{Key: ActionRuntimeSlotFail, ResourceType: ResourceRuntimeSlot},
		{Key: ActionRuntimeSlotRead, ResourceType: ResourceRuntimeSlot},
		{Key: ActionRuntimeSlotList, ResourceType: ResourceRuntimeSlot},
		{Key: ActionRuntimeWorkspaceMaterializationStart, ResourceType: ResourceRuntimeWorkspace},
		{Key: ActionRuntimeWorkspaceMaterializationReport, ResourceType: ResourceRuntimeWorkspace},
		{Key: ActionRuntimeWorkspaceMaterializationRead, ResourceType: ResourceRuntimeWorkspace},
		{Key: ActionRuntimeWorkspaceMaterializationList, ResourceType: ResourceRuntimeWorkspace},
		{Key: ActionRuntimeJobCreate, ResourceType: ResourceRuntimeJob},
		{Key: ActionRuntimeJobClaim, ResourceType: ResourceRuntimeJob},
		{Key: ActionRuntimeJobStepReport, ResourceType: ResourceRuntimeJob},
		{Key: ActionRuntimeJobComplete, ResourceType: ResourceRuntimeJob},
		{Key: ActionRuntimeJobFail, ResourceType: ResourceRuntimeJob},
		{Key: ActionRuntimeJobCancel, ResourceType: ResourceRuntimeJob},
		{Key: ActionRuntimeJobRead, ResourceType: ResourceRuntimeJob},
		{Key: ActionRuntimeJobList, ResourceType: ResourceRuntimeJob},
		{Key: ActionRuntimeArtifactRefRecord, ResourceType: ResourceRuntimeArtifactRef},
		{Key: ActionRuntimeArtifactRefList, ResourceType: ResourceRuntimeArtifactRef},
		{Key: ActionRuntimeCleanupPolicyUpsert, ResourceType: ResourceRuntimeCleanupPolicy},
		{Key: ActionRuntimeCleanupRun, ResourceType: ResourceRuntimeCleanupPolicy},
		{Key: ActionRuntimePrewarmPoolUpsert, ResourceType: ResourceRuntimePrewarmPool},
		{Key: ActionRuntimePrewarmPoolReconcile, ResourceType: ResourceRuntimePrewarmPool},
	}
}

// FleetManagerActions returns system actions owned by the runtime-and-fleet domain.
func FleetManagerActions() []ActionDescriptor {
	actions := make([]ActionDescriptor, 0, 26)
	actions = append(actions, actionDescriptorsForResource(ResourceFleetScope,
		ActionFleetScopeCreate,
		ActionFleetScopeUpdate,
		ActionFleetScopeDisable,
		ActionFleetScopeEnable,
		ActionFleetScopeRead,
		ActionFleetScopeList,
	)...)
	actions = append(actions, actionDescriptorsForResource(ResourceFleetServer,
		ActionFleetServerRegister,
		ActionFleetServerUpdate,
		ActionFleetServerDisable,
		ActionFleetServerEnable,
		ActionFleetServerRead,
		ActionFleetServerList,
	)...)
	actions = append(actions, actionDescriptorsForResource(ResourceFleetCluster,
		ActionFleetClusterRegister,
		ActionFleetClusterUpdate,
		ActionFleetClusterDisable,
		ActionFleetClusterEnable,
		ActionFleetClusterRead,
		ActionFleetClusterList,
	)...)
	actions = append(actions, actionDescriptorsForResource(ResourceFleetHealth,
		ActionFleetHealthCheckRun,
		ActionFleetHealthRead,
	)...)
	actions = append(actions, actionDescriptorsForResource(ResourceFleetPlacementRule,
		ActionFleetPlacementRulePut,
		ActionFleetPlacementRuleRead,
		ActionFleetPlacementRuleList,
	)...)
	actions = append(actions, actionDescriptorsForResource(ResourceFleetPlacementDecision,
		ActionFleetPlacementResolve,
		ActionFleetPlacementDecisionRead,
		ActionFleetPlacementDecisionList,
	)...)
	return actions
}

// AgentManagerActions returns system actions owned by the agent orchestration domain.
func AgentManagerActions() []ActionDescriptor {
	actions := make([]ActionDescriptor, 0, 17)
	actions = append(actions, actionDescriptorsForResource(ResourceAgentFlow,
		ActionAgentFlowRead,
		ActionAgentFlowManage,
	)...)
	actions = append(actions, actionDescriptorsForResource(ResourceAgentRole,
		ActionAgentRoleRead,
		ActionAgentRoleManage,
	)...)
	actions = append(actions, actionDescriptorsForResource(ResourceAgentPrompt,
		ActionAgentPromptRead,
		ActionAgentPromptManage,
	)...)
	actions = append(actions, actionDescriptorsForResource(ResourceAgentSession,
		ActionAgentSessionStart,
		ActionAgentSessionRead,
		ActionAgentSessionUpdate,
	)...)
	actions = append(actions, actionDescriptorsForResource(ResourceAgentRun,
		ActionAgentRunStart,
		ActionAgentRunRead,
		ActionAgentRunUpdate,
	)...)
	actions = append(actions, actionDescriptorsForResource(ResourceAgentAcceptance,
		ActionAgentAcceptanceRun,
		ActionAgentAcceptanceRead,
		ActionAgentAcceptanceUpdate,
	)...)
	actions = append(actions, ActionDescriptor{Key: ActionAgentFollowUpCreate, ResourceType: ResourceAgentFollowUp})
	actions = append(actions, ActionDescriptor{Key: ActionAgentHumanGateRequest, ResourceType: ResourceAgentHumanGate})
	return actions
}

// GovernanceManagerActions returns system actions owned by the risk and release governance domain.
func GovernanceManagerActions() []ActionDescriptor {
	actions := make([]ActionDescriptor, 0, 15)
	actions = append(actions, actionDescriptorsForResource(ResourceGovernanceRiskProfile,
		ActionGovernancePolicyManage,
		ActionGovernancePolicyRead,
	)...)
	actions = append(actions, actionDescriptorsForResource(ResourceGovernanceRiskAssessment,
		ActionGovernanceRiskEvaluate,
		ActionGovernanceRiskRead,
	)...)
	actions = append(actions, actionDescriptorsForResource(ResourceGovernanceSignal,
		ActionGovernanceSignalRecord,
		ActionGovernanceSignalRead,
		ActionGovernanceSignalResolve,
	)...)
	actions = append(actions, actionDescriptorsForResource(ResourceGovernanceGate,
		ActionGovernanceGateRequest,
		ActionGovernanceGateRead,
		ActionGovernanceGateDecide,
	)...)
	actions = append(actions, actionDescriptorsForResource(ResourceGovernanceReleaseDecision,
		ActionGovernanceReleasePrepare,
		ActionGovernanceReleaseRequest,
		ActionGovernanceReleaseRead,
		ActionGovernanceReleaseDecide,
	)...)
	actions = append(actions, ActionDescriptor{Key: ActionGovernanceReleaseUpdate, ResourceType: ResourceGovernanceReleaseSafetyState})
	return actions
}

// InteractionHubActions returns system actions owned by the interaction hub domain.
func InteractionHubActions() []ActionDescriptor {
	actions := make([]ActionDescriptor, 0, 18)
	actions = append(actions, actionDescriptorsForResource(ResourceInteractionThread,
		ActionInteractionThreadCreate,
		ActionInteractionThreadRead,
	)...)
	actions = append(actions, actionDescriptorsForResource(ResourceInteractionMessage,
		ActionInteractionMessageRecord,
		ActionInteractionMessageRead,
	)...)
	actions = append(actions, actionDescriptorsForResource(ResourceInteractionRequest,
		ActionInteractionFeedbackRequest,
		ActionInteractionApprovalRequest,
		ActionInteractionHumanGateRequest,
		ActionInteractionRequestCancel,
		ActionInteractionRequestExpire,
		ActionInteractionRequestRead,
	)...)
	actions = append(actions, ActionDescriptor{Key: ActionInteractionRequestRespond, ResourceType: ResourceInteractionResponse})
	actions = append(actions, ActionDescriptor{Key: ActionInteractionNotificationRequest, ResourceType: ResourceInteractionNotification})
	actions = append(actions, actionDescriptorsForResource(ResourceInteractionSubscription,
		ActionInteractionSubscriptionManage,
		ActionInteractionSubscriptionRead,
	)...)
	actions = append(actions, actionDescriptorsForResource(ResourceInteractionDelivery,
		ActionInteractionDeliveryPlan,
		ActionInteractionDeliveryUpdate,
		ActionInteractionDeliveryRead,
	)...)
	actions = append(actions, ActionDescriptor{Key: ActionInteractionCallbackRecord, ResourceType: ResourceInteractionCallback})
	return actions
}

func actionDescriptorsForResource(resourceType string, keys ...string) []ActionDescriptor {
	actions := make([]ActionDescriptor, 0, len(keys))
	for _, key := range keys {
		actions = append(actions, ActionDescriptor{Key: key, ResourceType: resourceType})
	}
	return actions
}

// SystemActions returns all shared code-owned system actions.
func SystemActions() []ActionDescriptor {
	actions := make([]ActionDescriptor, 0,
		len(ProjectCatalogActions())+
			len(PackageHubActions())+
			len(ProviderHubActions())+
			len(RuntimeManagerActions())+
			len(FleetManagerActions())+
			len(AgentManagerActions())+
			len(GovernanceManagerActions())+
			len(InteractionHubActions()))
	actions = append(actions, ProjectCatalogActions()...)
	actions = append(actions, PackageHubActions()...)
	actions = append(actions, ProviderHubActions()...)
	actions = append(actions, RuntimeManagerActions()...)
	actions = append(actions, FleetManagerActions()...)
	actions = append(actions, AgentManagerActions()...)
	actions = append(actions, GovernanceManagerActions()...)
	actions = append(actions, InteractionHubActions()...)
	return actions
}

// SystemActionByKey returns a shared code-owned system action descriptor by key.
func SystemActionByKey(key string) (ActionDescriptor, bool) {
	key = strings.TrimSpace(key)
	action, ok := systemActionsByKey[key]
	return action, ok
}

func actionDescriptorCatalog(actions []ActionDescriptor) map[string]ActionDescriptor {
	catalog := make(map[string]ActionDescriptor, len(actions))
	for _, action := range actions {
		catalog[action.Key] = action
	}
	return catalog
}
