package service

import (
	"context"

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

// CreateProviderRepositoryInput creates a provider repository and links it to a project binding.
type CreateProviderRepositoryInput struct {
	ProjectID         uuid.UUID
	Provider          enum.RepositoryProvider
	OwnerKind         enum.RepositoryOwnerKind
	ProviderOwner     string
	ProviderName      string
	Visibility        enum.RepositoryVisibility
	Description       string
	IconObjectURI     string
	ExternalAccountID uuid.UUID
	Meta              value.CommandMeta
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

// RepositoryBootstrapFile is one prepared text file for empty-repository bootstrap.
type RepositoryBootstrapFile struct {
	Path       string
	Content    string
	Executable bool
}

// RepositoryBootstrapServicesPolicy links prepared files to a checked services.yaml projection.
type RepositoryBootstrapServicesPolicy struct {
	SourcePath       string
	ContentHash      string
	ValidatedPayload []byte
}

// RepositoryBootstrapProviderTarget is the provider repository target derived from project binding.
type RepositoryBootstrapProviderTarget struct {
	ProviderSlug         string
	RepositoryFullName   string
	ProviderRepositoryID string
	WebURL               string
}

// RepositoryBootstrapProviderResult contains provider-hub refs returned from the delegated write.
type RepositoryBootstrapProviderResult struct {
	ProviderOperationID          string
	ProviderResultRef            string
	ProviderWorkItemProjectionID string
	ProviderWebURL               string
	ProviderObjectID             string
}

// RepositoryProviderCreateProviderResult contains safe refs returned by provider-hub repository creation.
type RepositoryProviderCreateProviderResult struct {
	ProviderOperationID  string
	ProviderResultRef    string
	ProviderRepositoryID string
	ProviderWebURL       string
	ProviderObjectID     string
	ProviderVersion      string
	BaseBranch           string
	RepositoryFullName   string
}

// RepositoryProviderCreateResult returns project binding and provider refs for repository bootstrap.
type RepositoryProviderCreateResult struct {
	Repository     entity.RepositoryBinding
	ProviderTarget RepositoryBootstrapProviderTarget
	BaseBranch     string
	ProviderResult RepositoryProviderCreateProviderResult
}

// ProviderRepositoryCreateInput is the domain port request sent to provider-hub.
type ProviderRepositoryCreateInput struct {
	ProjectID         uuid.UUID
	RepositoryID      uuid.UUID
	ProviderSlug      string
	OwnerKind         enum.RepositoryOwnerKind
	ProviderOwner     string
	RepositoryName    string
	Visibility        enum.RepositoryVisibility
	Description       string
	ExternalAccountID uuid.UUID
	Meta              value.CommandMeta
}

// CreateRepositoryBootstrapPullRequestInput creates or updates a provider-side bootstrap PR for a bound repository.
type CreateRepositoryBootstrapPullRequestInput struct {
	ProjectID         uuid.UUID
	RepositoryID      uuid.UUID
	BaseBranch        string
	BootstrapBranch   string
	CommitMessage     string
	Title             string
	Body              string
	Draft             bool
	Files             []RepositoryBootstrapFile
	WatermarkJSON     []byte
	ServicesPolicy    RepositoryBootstrapServicesPolicy
	ExternalAccountID uuid.UUID
	Meta              value.CommandMeta
}

// RepositoryBootstrapPullRequestResult returns project binding and provider refs for bootstrap.
type RepositoryBootstrapPullRequestResult struct {
	Repository      entity.RepositoryBinding
	ProviderTarget  RepositoryBootstrapProviderTarget
	BaseBranch      string
	BootstrapBranch string
	ServicesPolicy  RepositoryBootstrapServicesPolicy
	ProviderResult  RepositoryBootstrapProviderResult
}

// ImportBootstrapServicesPolicyInput imports checked services.yaml after a merged bootstrap PR.
type ImportBootstrapServicesPolicyInput struct {
	ProjectID                    uuid.UUID
	RepositoryID                 uuid.UUID
	ProviderTarget               RepositoryBootstrapProviderTarget
	BaseBranch                   string
	SourceRef                    string
	SourceCommitSHA              string
	SourceBlobSHA                string
	SourcePath                   string
	ContentHash                  string
	ValidatedPayload             []byte
	WatermarkJSON                []byte
	ProviderWorkItemProjectionID string
	ProviderWebURL               string
	ProviderObjectID             string
	MergeObservedAt              string
	ReconciliationFingerprint    string
	OnboardingSignal             *OnboardingSignalReconciliationInput
	Meta                         value.CommandMeta
}

// BootstrapRepositoryMergeSignal contains safe provider refs from a merged bootstrap PR.
type BootstrapRepositoryMergeSignal struct {
	SignalID                     string
	SignalKey                    string
	SignalKind                   string
	ProviderTarget               RepositoryBootstrapProviderTarget
	BaseBranch                   string
	SourceRef                    string
	MergeCommitSHA               string
	SourceBlobSHA                string
	WatermarkDigest              string
	WatermarkJSON                []byte
	ProviderWorkItemProjectionID string
	ProviderWebURL               string
	ProviderObjectID             string
	MergeObservedAt              string
	MergedAt                     string
}

// CheckedBootstrapServicesPolicyArtifact contains checked policy artifact metadata prepared by the caller contour.
type CheckedBootstrapServicesPolicyArtifact struct {
	ArtifactRef      string
	ArtifactDigest   string
	ArtifactVersion  string
	SourcePath       string
	ContentHash      string
	ValidatedPayload []byte
}

// RepositoryAdoptionMergeSignal contains safe provider refs from a merged adoption PR.
type RepositoryAdoptionMergeSignal = BootstrapRepositoryMergeSignal

// CheckedAdoptionServicesPolicyArtifact contains checked policy artifact metadata for adoption import.
type CheckedAdoptionServicesPolicyArtifact = CheckedBootstrapServicesPolicyArtifact

// ReconcileBootstrapMergeSignalInput closes bootstrap after provider-hub records a safe merge signal.
type ReconcileBootstrapMergeSignalInput struct {
	ProjectID     uuid.UUID
	RepositoryID  uuid.UUID
	MergeSignal   BootstrapRepositoryMergeSignal
	CheckedPolicy CheckedBootstrapServicesPolicyArtifact
	Meta          value.CommandMeta
}

// ReconcileAdoptionMergeSignalInput imports checked services.yaml after provider-hub records a safe adoption merge signal.
type ReconcileAdoptionMergeSignalInput struct {
	ProjectID     uuid.UUID
	RepositoryID  uuid.UUID
	MergeSignal   RepositoryAdoptionMergeSignal
	CheckedPolicy CheckedAdoptionServicesPolicyArtifact
	Meta          value.CommandMeta
}

// ProviderOwnedDataStatus описывает готовность safe read ответа provider-hub.
type ProviderOwnedDataStatus string

const (
	ProviderOwnedDataStatusReady       ProviderOwnedDataStatus = "ready"
	ProviderOwnedDataStatusNotFound    ProviderOwnedDataStatus = "not_found"
	ProviderOwnedDataStatusNotVerified ProviderOwnedDataStatus = "not_verified"
	ProviderOwnedDataStatusStale       ProviderOwnedDataStatus = "stale"
)

// RepositoryChangePathSummaryStatus отражает готовность safe provider path summary.
type RepositoryChangePathSummaryStatus string

const (
	RepositoryChangePathSummaryStatusReady       RepositoryChangePathSummaryStatus = "ready"
	RepositoryChangePathSummaryStatusUnavailable RepositoryChangePathSummaryStatus = "unavailable"
	RepositoryChangePathSummaryStatusTruncated   RepositoryChangePathSummaryStatus = "truncated"
)

// RepositoryChangeSignalReadInput идентифицирует один provider-owned repository change signal.
type RepositoryChangeSignalReadInput struct {
	SignalID  string
	SignalKey string
	Meta      value.QueryMeta
}

// RepositoryChangePathCategoryCount содержит safe счётчики provider path categories.
type RepositoryChangePathCategoryCount struct {
	Category enum.SelfDeployPathCategory
	Count    int64
}

// RepositoryChangeSignal содержит safe provider refs для project-side enrichment.
type RepositoryChangeSignal struct {
	SignalID              string
	SignalKey             string
	Kind                  string
	ProviderSlug          string
	ProjectID             string
	RepositoryID          string
	RepositoryFullName    string
	ProviderRepositoryID  string
	Ref                   string
	BaseBranch            string
	CommitSHA             string
	BeforeSHA             string
	SourceRef             string
	PullRequestNumber     int64
	PathSummaryStatus     RepositoryChangePathSummaryStatus
	ChangedPathCount      int64
	PathDigest            string
	PathCategories        []RepositoryChangePathCategoryCount
	ServicesPolicyChanged bool
	DeployRelevantChanged bool
	ChangeFingerprint     string
	ObservedAt            string
	Status                string
	Version               int64
	ETag                  string
}

// RepositoryChangeSignalReadResult возвращает safe provider signal или явную готовность.
type RepositoryChangeSignalReadResult struct {
	Status ProviderOwnedDataStatus
	Signal RepositoryChangeSignal
}

// RepositoryChangeSignalReader читает safe repository change signals из provider-hub.
type RepositoryChangeSignalReader interface {
	GetRepositoryChangeSignal(context.Context, RepositoryChangeSignalReadInput) (RepositoryChangeSignalReadResult, error)
}

// GetSelfDeploySignalInput идентифицирует provider/project facts для self-deploy enrichment.
type GetSelfDeploySignalInput struct {
	ProjectID         uuid.UUID
	RepositoryID      *uuid.UUID
	ProviderSignalID  string
	ProviderSignalKey string
	Meta              value.QueryMeta
}

// SelfDeployServicesYamlProjection описывает checked services.yaml metadata без payload.
type SelfDeployServicesYamlProjection struct {
	ServicesYamlRef         string
	ServicesYamlDigest      string
	ServicesYamlFingerprint string
	ServicesPolicyID        uuid.UUID
	SourceRepositoryID      *uuid.UUID
	SourcePath              string
	SourceRef               string
	SourceCommitSHA         string
	PolicyVersion           int64
	ValidationStatus        enum.ServicesPolicyValidationStatus
	ProjectionStatus        enum.ServicesPolicyProjectionStatus
	ImportedAt              string
}

// SelfDeployGovernanceRequirement содержит safe governance hints.
type SelfDeployGovernanceRequirement struct {
	GateRequired   bool
	RiskProfileRef string
	GatePolicyRef  string
}

// SelfDeploySignal — project-side safe input для agent-manager SelfDeployPlan.
type SelfDeploySignal struct {
	ProviderSignalRef         string
	ProviderSignalID          string
	ProviderSignalKey         string
	ProjectRef                string
	RepositoryRef             string
	ProviderSlug              string
	RepositoryFullName        string
	ProviderRepositoryID      string
	SourceRef                 string
	MergeCommitSHA            string
	ServicesYaml              SelfDeployServicesYamlProjection
	AffectedServiceKeys       []string
	PathCategories            []RepositoryChangePathCategoryCount
	ServicesYamlChanged       bool
	DeployRelevantChanged     bool
	ExpectedRuntimeJobTypes   []enum.SelfDeployExpectedRuntimeJobType
	GovernanceRequirement     SelfDeployGovernanceRequirement
	ProviderChangeFingerprint string
	ProjectSignalFingerprint  string
	ProviderETag              string
	SafeSummary               string
	ObservedAt                string
	Version                   int64
}

// SelfDeploySignalResult возвращает готовность и bounded diagnostic reason.
type SelfDeploySignalResult struct {
	Status     enum.SelfDeploySignalStatus
	Signal     SelfDeploySignal
	SafeReason string
}

// BootstrapMergeSignalDiagnosticInput records a safe bootstrap merge signal that cannot yet import policy.
type BootstrapMergeSignalDiagnosticInput struct {
	ProjectID         uuid.UUID
	RepositoryID      uuid.UUID
	MergeSignal       BootstrapRepositoryMergeSignal
	SignalFingerprint string
	ErrorCode         string
	ErrorSummary      string
	Summary           string
}

// AdoptionMergeSignalDiagnosticInput records a safe adoption merge signal that cannot yet import policy.
type AdoptionMergeSignalDiagnosticInput struct {
	ProjectID         uuid.UUID
	RepositoryID      uuid.UUID
	MergeSignal       RepositoryAdoptionMergeSignal
	SignalFingerprint string
	ErrorCode         string
	ErrorSummary      string
	Summary           string
}

// OnboardingSignalReconciliationInput contains safe metadata for project-side signal status.
type OnboardingSignalReconciliationInput struct {
	ProjectID            uuid.UUID
	RepositoryID         uuid.UUID
	SignalKind           enum.OnboardingSignalKind
	SignalKey            string
	SignalFingerprint    string
	ProviderSlug         string
	RepositoryFullName   string
	ProviderRepositoryID string
	BaseBranch           string
	SourceRef            string
	SourceCommitSHA      string
	ArtifactRef          string
	ArtifactDigest       string
	ArtifactVersion      string
	ContentHash          string
	Summary              string
	ObservedAt           string
}

// BootstrapServicesPolicyImportResult returns activated binding and checked policy state.
type BootstrapServicesPolicyImportResult struct {
	Repository      entity.RepositoryBinding
	ServicesPolicy  entity.ServicesPolicy
	SourceRef       string
	SourceCommitSHA string
	Summary         string
}

// ProviderBootstrapPullRequestInput is the domain port request sent to provider-hub.
type ProviderBootstrapPullRequestInput struct {
	ProjectID         uuid.UUID
	RepositoryID      uuid.UUID
	ProviderSlug      string
	RepositoryTarget  RepositoryBootstrapProviderTarget
	BaseBranch        string
	BootstrapBranch   string
	CommitMessage     string
	Title             string
	Body              string
	Draft             bool
	Files             []RepositoryBootstrapFile
	WatermarkJSON     []byte
	ServicesPolicy    RepositoryBootstrapServicesPolicy
	ExternalAccountID uuid.UUID
	Meta              value.CommandMeta
}

// BootstrapProvider delegates provider-native repository onboarding writes to provider-hub.
type BootstrapProvider interface {
	CreateProviderRepository(context.Context, ProviderRepositoryCreateInput) (RepositoryProviderCreateProviderResult, error)
	CreateRepositoryBootstrapPullRequest(context.Context, ProviderBootstrapPullRequestInput) (RepositoryBootstrapProviderResult, error)
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

// CancelPolicyOverrideInput cancels an active emergency policy override.
type CancelPolicyOverrideInput struct {
	PolicyOverrideID uuid.UUID
	Meta             value.CommandMeta
}

// ListPolicyOverridesInput selects operator policy overrides.
type ListPolicyOverridesInput struct {
	ProjectID   uuid.UUID
	TargetTypes []enum.PolicyOverrideTargetType
	TargetID    *uuid.UUID
	Statuses    []enum.PolicyOverrideStatus
	ActiveOnly  bool
	Page        value.PageRequest
	Meta        value.QueryMeta
}

// ListPolicyOverridesResult returns operator overrides and paging metadata.
type ListPolicyOverridesResult struct {
	PolicyOverrides []entity.PolicyOverride
	Page            value.PageResult
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
