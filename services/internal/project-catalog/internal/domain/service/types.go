package service

import (
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

// CreateProjectInput contains fields required to create a project.
type CreateProjectInput struct {
	OrganizationID uuid.UUID
	Slug           string
	DisplayName    string
	Description    string
	IconObjectURI  string
	Status         enum.ProjectStatus
	Meta           value.CommandMeta
}

// UpdateProjectInput changes safe project fields.
type UpdateProjectInput struct {
	ProjectID     uuid.UUID
	Slug          *string
	DisplayName   *string
	Description   *string
	IconObjectURI *string
	Status        enum.ProjectStatus
	Meta          value.CommandMeta
}

// ListProjectsInput selects projects for authoritative reads.
type ListProjectsInput struct {
	OrganizationID *uuid.UUID
	Statuses       []enum.ProjectStatus
	Page           value.PageRequest
	Meta           value.QueryMeta
}

// ListProjectsResult returns projects and paging metadata.
type ListProjectsResult struct {
	Projects []entity.Project
	Page     value.PageResult
}

// AttachRepositoryInput binds a provider repository to a project.
type AttachRepositoryInput struct {
	ProjectID            uuid.UUID
	Provider             enum.RepositoryProvider
	ProviderOwner        string
	ProviderName         string
	WebURL               string
	DefaultBranch        string
	ProviderRepositoryID string
	IconObjectURI        string
	Status               enum.RepositoryStatus
	Meta                 value.CommandMeta
}

// UpdateRepositoryInput changes safe repository binding fields.
type UpdateRepositoryInput struct {
	RepositoryID  uuid.UUID
	DefaultBranch *string
	Status        enum.RepositoryStatus
	IconObjectURI *string
	Meta          value.CommandMeta
}

// ListRepositoriesInput selects repository bindings for authoritative reads.
type ListRepositoriesInput struct {
	ProjectID uuid.UUID
	Statuses  []enum.RepositoryStatus
	Page      value.PageRequest
	Meta      value.QueryMeta
}

// ListRepositoriesResult returns repository bindings and paging metadata.
type ListRepositoriesResult struct {
	Repositories []entity.RepositoryBinding
	Page         value.PageResult
}

// ImportServicesPolicyInput imports a checked services.yaml projection.
type ImportServicesPolicyInput struct {
	ProjectID          uuid.UUID
	SourceRepositoryID *uuid.UUID
	SourcePath         string
	SourceRef          string
	SourceCommitSHA    string
	SourceBlobSHA      string
	ContentHash        string
	ValidatedPayload   []byte
	ServiceDescriptors []entity.ServiceDescriptor
	ValidationStatus   enum.ServicesPolicyValidationStatus
	Meta               value.CommandMeta
}

// GetServicesPolicyInput identifies an active or concrete policy.
type GetServicesPolicyInput struct {
	ProjectID        uuid.UUID
	ServicesPolicyID *uuid.UUID
	Meta             value.QueryMeta
}

// ListServiceDescriptorsInput selects typed services from checked policy.
type ListServiceDescriptorsInput struct {
	ProjectID    uuid.UUID
	RepositoryID *uuid.UUID
	ServiceKeys  []string
	Statuses     []enum.ServiceStatus
	Page         value.PageRequest
	Meta         value.QueryMeta
}

// ListServiceDescriptorsResult returns typed service descriptors and paging metadata.
type ListServiceDescriptorsResult struct {
	ServiceDescriptors []entity.ServiceDescriptor
	Page               value.PageResult
}

// CreatePolicyEditProposalInput requests a PR-backed services.yaml change.
type CreatePolicyEditProposalInput struct {
	ProjectID        uuid.UUID
	RepositoryID     uuid.UUID
	SourcePath       string
	RequestedChanges value.PolicyEditProposalRequestedChanges
	Meta             value.CommandMeta
}

// CreatePolicyOverrideInput creates an emergency policy override.
type CreatePolicyOverrideInput struct {
	ProjectID  uuid.UUID
	TargetType enum.PolicyOverrideTargetType
	TargetID   *uuid.UUID
	Payload    []byte
	ExpiresAt  string
	Meta       value.CommandMeta
}

// PutDocumentationSourceInput creates or updates a documentation source.
type PutDocumentationSourceInput struct {
	DocumentationSourceID *uuid.UUID
	ProjectID             uuid.UUID
	RepositoryID          *uuid.UUID
	ScopeType             enum.DocumentationScopeType
	ScopeID               string
	LocalPath             string
	AccessMode            enum.DocumentationAccessMode
	Status                enum.DocumentationSourceStatus
	Meta                  value.CommandMeta
}

// ListDocumentationSourcesInput selects documentation sources.
type ListDocumentationSourcesInput struct {
	ProjectID    uuid.UUID
	RepositoryID *uuid.UUID
	ScopeType    enum.DocumentationScopeType
	ScopeID      string
	Statuses     []enum.DocumentationSourceStatus
	Page         value.PageRequest
	Meta         value.QueryMeta
}

// ListDocumentationSourcesResult returns documentation sources and paging metadata.
type ListDocumentationSourcesResult struct {
	DocumentationSources []entity.DocumentationSource
	Page                 value.PageResult
}

// GetWorkspacePolicyInput selects sources for an agent workspace.
type GetWorkspacePolicyInput struct {
	ProjectID               uuid.UUID
	RepositoryIDs           []uuid.UUID
	ServiceKeys             []string
	IncludeGuidancePackages bool
	Meta                    value.QueryMeta
}

// PutBranchRulesInput creates or updates branch rules.
type PutBranchRulesInput struct {
	BranchRulesID  *uuid.UUID
	ProjectID      uuid.UUID
	RepositoryID   *uuid.UUID
	Pattern        string
	RequiredChecks []string
	MergePolicy    enum.MergePolicy
	Status         enum.BranchRulesStatus
	Meta           value.CommandMeta
}

// ListBranchRulesInput selects branch rules.
type ListBranchRulesInput struct {
	ProjectID    uuid.UUID
	RepositoryID *uuid.UUID
	Statuses     []enum.BranchRulesStatus
	Page         value.PageRequest
	Meta         value.QueryMeta
}

// ListBranchRulesResult returns branch rules and paging metadata.
type ListBranchRulesResult struct {
	BranchRules []entity.BranchRules
	Page        value.PageResult
}

// PutReleasePolicyInput creates or updates release policy.
type PutReleasePolicyInput struct {
	ReleasePolicyID *uuid.UUID
	ProjectID       uuid.UUID
	Name            string
	BranchPattern   string
	RolloutStrategy enum.RolloutStrategy
	RollbackPolicy  enum.RollbackPolicy
	RiskProfileRef  string
	Status          enum.ReleasePolicyStatus
	Meta            value.CommandMeta
}

// ListReleasePoliciesInput selects release policies.
type ListReleasePoliciesInput struct {
	ProjectID uuid.UUID
	Statuses  []enum.ReleasePolicyStatus
	Page      value.PageRequest
	Meta      value.QueryMeta
}

// ListReleasePoliciesResult returns release policies and paging metadata.
type ListReleasePoliciesResult struct {
	ReleasePolicies []entity.ReleasePolicy
	Page            value.PageResult
}

// PutReleaseLineInput creates or updates a concrete release line.
type PutReleaseLineInput struct {
	ReleaseLineID   *uuid.UUID
	ProjectID       uuid.UUID
	ReleasePolicyID uuid.UUID
	Name            string
	BranchPattern   string
	Status          enum.ReleasePolicyStatus
	Meta            value.CommandMeta
}

// ListReleaseLinesInput selects release lines.
type ListReleaseLinesInput struct {
	ProjectID       uuid.UUID
	ReleasePolicyID *uuid.UUID
	Statuses        []enum.ReleasePolicyStatus
	Page            value.PageRequest
	Meta            value.QueryMeta
}

// ListReleaseLinesResult returns release lines and paging metadata.
type ListReleaseLinesResult struct {
	ReleaseLines []entity.ReleaseLine
	Page         value.PageResult
}

// PutPlacementPolicyInput creates or updates placement policy.
type PutPlacementPolicyInput struct {
	PlacementPolicyID  *uuid.UUID
	ProjectID          uuid.UUID
	RepositoryID       *uuid.UUID
	ServiceKey         string
	AllowedClusterRefs []string
	Status             enum.PlacementPolicyStatus
	Meta               value.CommandMeta
}

// ListPlacementPoliciesInput selects placement policies.
type ListPlacementPoliciesInput struct {
	ProjectID    uuid.UUID
	RepositoryID *uuid.UUID
	ServiceKey   string
	Statuses     []enum.PlacementPolicyStatus
	Page         value.PageRequest
	Meta         value.QueryMeta
}

// ListPlacementPoliciesResult returns placement policies and paging metadata.
type ListPlacementPoliciesResult struct {
	PlacementPolicies []entity.PlacementPolicy
	Page              value.PageResult
}
