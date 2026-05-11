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
	ResourceProject                = "project"
	ResourceRepository             = "repository"
	ResourceServicesPolicy         = "services_policy"
	ResourcePolicyOverride         = "policy_override"
	ResourceDocumentationSource    = "documentation_source"
	ResourceBranchRules            = "branch_rules"
	ResourceReleasePolicy          = "release_policy"
	ResourceReleaseLine            = "release_line"
	ResourcePlacementPolicy        = "placement_policy"
	ResourcePackageSource          = "package_source"
	ResourcePackageCatalog         = "package_catalog"
	ResourcePackage                = "package"
	ResourcePackageVersion         = "package_version"
	ResourcePackageManifest        = "package_manifest"
	ResourcePackageInstallation    = "package_installation"
	ResourcePackageSecretSchema    = "package_secret_schema"
	ResourceProviderWorkItem       = "provider_work_item"
	ResourceProviderIssue          = "provider_issue"
	ResourceProviderPullRequest    = "provider_pull_request"
	ResourceProviderComment        = "provider_comment"
	ResourceProviderReviewSignal   = "provider_review_signal"
	ResourceProviderRelationship   = "provider_relationship"
	ResourceProviderReconciliation = "provider_reconciliation"
	ResourceRuntimeSlot            = "runtime_slot"
	ResourceRuntimeWorkspace       = "runtime_workspace_materialization"
	ResourceRuntimeJob             = "runtime_job"
	ResourceRuntimeArtifactRef     = "runtime_artifact_ref"
	ResourceRuntimeCleanupPolicy   = "runtime_cleanup_policy"
	ResourceRuntimePrewarmPool     = "runtime_prewarm_pool"
	ResourceFleetScope             = "fleet_scope"
	ResourceFleetServer            = "fleet_server"
	ResourceFleetCluster           = "fleet_cluster"
	ResourceFleetHealth            = "fleet_health"
	ResourceFleetPlacementRule     = "fleet_placement_rule"
	ResourceFleetPlacementDecision = "fleet_placement_decision"
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
	ActionPackageSecretRead                     = "package.secret.read"
	ActionPackageVerify                         = "package.verify"
	ActionProviderWorkItemRead                  = "provider.work_item.read"
	ActionProviderIssueWrite                    = "provider.issue.write"
	ActionProviderPullRequestWrite              = "provider.pull_request.write"
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
	ActionFleetScopeRead                        = "fleet.scope.read"
	ActionFleetScopeList                        = "fleet.scope.list"
	ActionFleetServerRegister                   = "fleet.server.register"
	ActionFleetServerUpdate                     = "fleet.server.update"
	ActionFleetServerDisable                    = "fleet.server.disable"
	ActionFleetServerRead                       = "fleet.server.read"
	ActionFleetServerList                       = "fleet.server.list"
	ActionFleetClusterRegister                  = "fleet.cluster.register"
	ActionFleetClusterUpdate                    = "fleet.cluster.update"
	ActionFleetClusterDisable                   = "fleet.cluster.disable"
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
	return []ActionDescriptor{
		{Key: ActionFleetScopeCreate, ResourceType: ResourceFleetScope},
		{Key: ActionFleetScopeUpdate, ResourceType: ResourceFleetScope},
		{Key: ActionFleetScopeDisable, ResourceType: ResourceFleetScope},
		{Key: ActionFleetScopeRead, ResourceType: ResourceFleetScope},
		{Key: ActionFleetScopeList, ResourceType: ResourceFleetScope},
		{Key: ActionFleetServerRegister, ResourceType: ResourceFleetServer},
		{Key: ActionFleetServerUpdate, ResourceType: ResourceFleetServer},
		{Key: ActionFleetServerDisable, ResourceType: ResourceFleetServer},
		{Key: ActionFleetServerRead, ResourceType: ResourceFleetServer},
		{Key: ActionFleetServerList, ResourceType: ResourceFleetServer},
		{Key: ActionFleetClusterRegister, ResourceType: ResourceFleetCluster},
		{Key: ActionFleetClusterUpdate, ResourceType: ResourceFleetCluster},
		{Key: ActionFleetClusterDisable, ResourceType: ResourceFleetCluster},
		{Key: ActionFleetClusterRead, ResourceType: ResourceFleetCluster},
		{Key: ActionFleetClusterList, ResourceType: ResourceFleetCluster},
		{Key: ActionFleetHealthCheckRun, ResourceType: ResourceFleetHealth},
		{Key: ActionFleetHealthRead, ResourceType: ResourceFleetHealth},
		{Key: ActionFleetPlacementRulePut, ResourceType: ResourceFleetPlacementRule},
		{Key: ActionFleetPlacementRuleRead, ResourceType: ResourceFleetPlacementRule},
		{Key: ActionFleetPlacementRuleList, ResourceType: ResourceFleetPlacementRule},
		{Key: ActionFleetPlacementResolve, ResourceType: ResourceFleetPlacementDecision},
		{Key: ActionFleetPlacementDecisionRead, ResourceType: ResourceFleetPlacementDecision},
		{Key: ActionFleetPlacementDecisionList, ResourceType: ResourceFleetPlacementDecision},
	}
}

// SystemActions returns all shared code-owned system actions.
func SystemActions() []ActionDescriptor {
	actions := make([]ActionDescriptor, 0, len(ProjectCatalogActions())+len(PackageHubActions())+len(ProviderHubActions())+len(RuntimeManagerActions())+len(FleetManagerActions()))
	actions = append(actions, ProjectCatalogActions()...)
	actions = append(actions, PackageHubActions()...)
	actions = append(actions, ProviderHubActions()...)
	actions = append(actions, RuntimeManagerActions()...)
	actions = append(actions, FleetManagerActions()...)
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
