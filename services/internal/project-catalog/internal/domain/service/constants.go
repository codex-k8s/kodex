package service

import projectevents "github.com/codex-k8s/kodex/libs/go/platformevents/project"

const (
	projectEventProjectCreated           = projectevents.EventProjectCreated
	projectEventProjectUpdated           = projectevents.EventProjectUpdated
	projectEventProjectArchived          = projectevents.EventProjectArchived
	projectEventProjectDisabled          = projectevents.EventProjectDisabled
	projectEventRepositoryAttached       = projectevents.EventRepositoryAttached
	projectEventRepositoryUpdated        = projectevents.EventRepositoryUpdated
	projectEventRepositoryDetached       = projectevents.EventRepositoryDetached
	projectEventServicesPolicyImported   = projectevents.EventServicesPolicyImported
	projectEventPolicyOverrideCreated    = projectevents.EventPolicyOverrideCreated
	projectEventPolicyOverrideCancelled  = projectevents.EventPolicyOverrideCancelled
	projectEventDocumentationCreated     = projectevents.EventDocumentationSourceCreated
	projectEventDocumentationUpdated     = projectevents.EventDocumentationSourceUpdated
	projectEventDocumentationDisabled    = projectevents.EventDocumentationSourceDisabled
	projectEventBranchRulesCreated       = projectevents.EventBranchRulesCreated
	projectEventBranchRulesUpdated       = projectevents.EventBranchRulesUpdated
	projectEventBranchRulesDisabled      = projectevents.EventBranchRulesDisabled
	projectEventReleasePolicyCreated     = projectevents.EventReleasePolicyCreated
	projectEventReleasePolicyUpdated     = projectevents.EventReleasePolicyUpdated
	projectEventReleasePolicyArchived    = projectevents.EventReleasePolicyArchived
	projectEventReleasePolicyDisabled    = projectevents.EventReleasePolicyDisabled
	projectEventReleaseLineCreated       = projectevents.EventReleaseLineCreated
	projectEventReleaseLineUpdated       = projectevents.EventReleaseLineUpdated
	projectEventReleaseLineArchived      = projectevents.EventReleaseLineArchived
	projectEventReleaseLineDisabled      = projectevents.EventReleaseLineDisabled
	projectEventPlacementPolicyCreated   = projectevents.EventPlacementPolicyCreated
	projectEventPlacementPolicyUpdated   = projectevents.EventPlacementPolicyUpdated
	projectEventPlacementPolicyDisabled  = projectevents.EventPlacementPolicyDisabled
	projectAggregateProject              = projectevents.AggregateProject
	projectAggregateRepository           = projectevents.AggregateRepository
	projectAggregateServicesPolicy       = projectevents.AggregateServicesPolicy
	projectAggregatePolicyOverride       = projectevents.AggregatePolicyOverride
	projectAggregateDocumentationSource  = projectevents.AggregateDocumentationSource
	projectAggregateBranchRules          = projectevents.AggregateBranchRules
	projectAggregateReleasePolicy        = projectevents.AggregateReleasePolicy
	projectAggregateReleaseLine          = projectevents.AggregateReleaseLine
	projectAggregatePlacementPolicy      = projectevents.AggregatePlacementPolicy
	projectOperationCreateProject        = "domain.Service.CreateProject"
	projectOperationUpdateProject        = "domain.Service.UpdateProject"
	projectOperationAttachRepository     = "domain.Service.AttachRepository"
	projectOperationUpdateRepository     = "domain.Service.UpdateRepository"
	projectOperationDetachRepository     = "domain.Service.DetachRepository"
	projectOperationImportServicesPolicy = "domain.Service.ImportServicesPolicy"
	projectOperationPolicyEditProposal   = "domain.Service.CreatePolicyEditProposal"
	projectOperationPolicyOverride       = "domain.Service.CreatePolicyOverride"
	projectOperationCancelPolicyOverride = "domain.Service.CancelPolicyOverride"
	projectOperationPutDocumentation     = "domain.Service.PutDocumentationSource"
	projectOperationPutBranchRules       = "domain.Service.PutBranchRules"
	projectOperationPutReleasePolicy     = "domain.Service.PutReleasePolicy"
	projectOperationPutReleaseLine       = "domain.Service.PutReleaseLine"
	projectOperationPutPlacementPolicy   = "domain.Service.PutPlacementPolicy"
	projectActionCreate                  = "project.create"
	projectActionUpdate                  = "project.update"
	projectActionRead                    = "project.read"
	projectActionList                    = "project.list"
	projectActionRepositoryAttach        = "repository.attach"
	projectActionRepositoryUpdate        = "repository.update"
	projectActionRepositoryDetach        = "repository.detach"
	projectActionRepositoryRead          = "repository.read"
	projectActionRepositoryList          = "repository.list"
	projectActionPolicyImport            = "project.policy.import"
	projectActionPolicyRead              = "project.policy.read"
	projectActionPolicyPropose           = "project.policy.propose"
	projectActionPolicyOverride          = "project.policy.override"
	projectActionPolicyOverrideRead      = "project.policy.override.read"
	projectActionPolicyOverrideCancel    = "project.policy.override.cancel"
	projectActionDocsUpdate              = "project.docs.update"
	projectActionDocsRead                = "project.docs.read"
	projectActionWorkspaceRead           = "project.workspace.read"
	projectActionBranchRulesUpdate       = "project.branch_rules.update"
	projectActionBranchRulesRead         = "project.branch_rules.read"
	projectActionReleasePolicyUpdate     = "project.release_policy.update"
	projectActionReleasePolicyRead       = "project.release_policy.read"
	projectActionReleaseLineUpdate       = "project.release_line.update"
	projectActionReleaseLineRead         = "project.release_line.read"
	projectActionPlacementPolicyUpdate   = "project.placement_policy.update"
	projectActionPlacementPolicyRead     = "project.placement_policy.read"
	projectProposalStatusPending         = "pending"
)

type resourceRef struct {
	Type      string
	ID        string
	ScopeType string
	ScopeID   string
}
