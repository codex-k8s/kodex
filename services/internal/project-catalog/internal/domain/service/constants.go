package service

import (
	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	projectevents "github.com/codex-k8s/kodex/libs/go/platformevents/project"
)

const (
	projectEventProjectCreated            = projectevents.EventProjectCreated
	projectEventProjectUpdated            = projectevents.EventProjectUpdated
	projectEventProjectArchived           = projectevents.EventProjectArchived
	projectEventProjectDisabled           = projectevents.EventProjectDisabled
	projectEventRepositoryAttached        = projectevents.EventRepositoryAttached
	projectEventRepositoryUpdated         = projectevents.EventRepositoryUpdated
	projectEventRepositoryDetached        = projectevents.EventRepositoryDetached
	projectEventServicesPolicyImported    = projectevents.EventServicesPolicyImported
	projectEventPolicyOverrideCreated     = projectevents.EventPolicyOverrideCreated
	projectEventPolicyOverrideCancelled   = projectevents.EventPolicyOverrideCancelled
	projectEventDocumentationCreated      = projectevents.EventDocumentationSourceCreated
	projectEventDocumentationUpdated      = projectevents.EventDocumentationSourceUpdated
	projectEventDocumentationDisabled     = projectevents.EventDocumentationSourceDisabled
	projectEventBranchRulesCreated        = projectevents.EventBranchRulesCreated
	projectEventBranchRulesUpdated        = projectevents.EventBranchRulesUpdated
	projectEventBranchRulesDisabled       = projectevents.EventBranchRulesDisabled
	projectEventReleasePolicyCreated      = projectevents.EventReleasePolicyCreated
	projectEventReleasePolicyUpdated      = projectevents.EventReleasePolicyUpdated
	projectEventReleasePolicyArchived     = projectevents.EventReleasePolicyArchived
	projectEventReleasePolicyDisabled     = projectevents.EventReleasePolicyDisabled
	projectEventReleaseLineCreated        = projectevents.EventReleaseLineCreated
	projectEventReleaseLineUpdated        = projectevents.EventReleaseLineUpdated
	projectEventReleaseLineArchived       = projectevents.EventReleaseLineArchived
	projectEventReleaseLineDisabled       = projectevents.EventReleaseLineDisabled
	projectEventPlacementPolicyCreated    = projectevents.EventPlacementPolicyCreated
	projectEventPlacementPolicyUpdated    = projectevents.EventPlacementPolicyUpdated
	projectEventPlacementPolicyDisabled   = projectevents.EventPlacementPolicyDisabled
	projectAggregateProject               = projectevents.AggregateProject
	projectAggregateRepository            = projectevents.AggregateRepository
	projectAggregateServicesPolicy        = projectevents.AggregateServicesPolicy
	projectAggregatePolicyEditProposal    = "policy_edit_proposal"
	projectAggregatePolicyOverride        = projectevents.AggregatePolicyOverride
	projectAggregateDocumentationSource   = projectevents.AggregateDocumentationSource
	projectAggregateBranchRules           = projectevents.AggregateBranchRules
	projectAggregateReleasePolicy         = projectevents.AggregateReleasePolicy
	projectAggregateReleaseLine           = projectevents.AggregateReleaseLine
	projectAggregatePlacementPolicy       = projectevents.AggregatePlacementPolicy
	projectOperationCreateProject         = "domain.Service.CreateProject"
	projectOperationUpdateProject         = "domain.Service.UpdateProject"
	projectOperationAttachRepository      = "domain.Service.AttachRepository"
	projectOperationCreateProviderRepo    = "domain.Service.CreateProviderRepository"
	projectOperationBootstrapRepository   = "domain.Service.CreateRepositoryBootstrapPullRequest"
	projectOperationUpdateRepository      = "domain.Service.UpdateRepository"
	projectOperationDetachRepository      = "domain.Service.DetachRepository"
	projectOperationImportServicesPolicy  = "domain.Service.ImportServicesPolicy"
	projectOperationImportBootstrapPolicy = "domain.Service.ImportBootstrapServicesPolicy"
	projectOperationPolicyEditProposal    = "domain.Service.CreatePolicyEditProposal"
	projectOperationPolicyOverride        = "domain.Service.CreatePolicyOverride"
	projectOperationCancelPolicyOverride  = "domain.Service.CancelPolicyOverride"
	projectOperationPutDocumentation      = "domain.Service.PutDocumentationSource"
	projectOperationPutBranchRules        = "domain.Service.PutBranchRules"
	projectOperationPutReleasePolicy      = "domain.Service.PutReleasePolicy"
	projectOperationPutReleaseLine        = "domain.Service.PutReleaseLine"
	projectOperationPutPlacementPolicy    = "domain.Service.PutPlacementPolicy"
	projectActionCreate                   = accesscatalog.ActionProjectCreate
	projectActionUpdate                   = accesscatalog.ActionProjectUpdate
	projectActionRead                     = accesscatalog.ActionProjectRead
	projectActionList                     = accesscatalog.ActionProjectList
	projectActionRepositoryAttach         = accesscatalog.ActionRepositoryAttach
	projectActionRepositoryBootstrap      = accesscatalog.ActionRepositoryBootstrap
	projectActionRepositoryUpdate         = accesscatalog.ActionRepositoryUpdate
	projectActionRepositoryDetach         = accesscatalog.ActionRepositoryDetach
	projectActionRepositoryRead           = accesscatalog.ActionRepositoryRead
	projectActionRepositoryList           = accesscatalog.ActionRepositoryList
	projectActionPolicyImport             = accesscatalog.ActionProjectPolicyImport
	projectActionPolicyRead               = accesscatalog.ActionProjectPolicyRead
	projectActionPolicyPropose            = accesscatalog.ActionProjectPolicyPropose
	projectActionPolicyOverride           = accesscatalog.ActionProjectPolicyOverride
	projectActionPolicyOverrideRead       = accesscatalog.ActionProjectPolicyOverrideRead
	projectActionPolicyOverrideCancel     = accesscatalog.ActionProjectPolicyOverrideCancel
	projectActionDocsUpdate               = accesscatalog.ActionProjectDocsUpdate
	projectActionDocsRead                 = accesscatalog.ActionProjectDocsRead
	projectActionWorkspaceRead            = accesscatalog.ActionProjectWorkspaceRead
	projectActionBranchRulesUpdate        = accesscatalog.ActionProjectBranchRulesUpdate
	projectActionBranchRulesRead          = accesscatalog.ActionProjectBranchRulesRead
	projectActionReleasePolicyUpdate      = accesscatalog.ActionProjectReleasePolicyUpdate
	projectActionReleasePolicyRead        = accesscatalog.ActionProjectReleasePolicyRead
	projectActionReleaseLineUpdate        = accesscatalog.ActionProjectReleaseLineUpdate
	projectActionReleaseLineRead          = accesscatalog.ActionProjectReleaseLineRead
	projectActionPlacementPolicyUpdate    = accesscatalog.ActionProjectPlacementUpdate
	projectActionPlacementPolicyRead      = accesscatalog.ActionProjectPlacementRead
	projectProposalStatusPending          = "pending"
)

type resourceRef struct {
	Type      string
	ID        string
	ScopeType string
	ScopeID   string
}
