// Package enum contains closed domain vocabularies for project-catalog.
package enum

// ProjectStatus describes the lifecycle state of a project.
type ProjectStatus string

const (
	ProjectStatusActive   ProjectStatus = "active"
	ProjectStatusArchived ProjectStatus = "archived"
	ProjectStatusDisabled ProjectStatus = "disabled"
)

// RepositoryProvider identifies a supported repository provider.
type RepositoryProvider string

const (
	RepositoryProviderGitHub RepositoryProvider = "github"
	RepositoryProviderGitLab RepositoryProvider = "gitlab"
)

// RepositoryStatus describes the lifecycle state of a repository binding.
type RepositoryStatus string

const (
	RepositoryStatusActive   RepositoryStatus = "active"
	RepositoryStatusPending  RepositoryStatus = "pending"
	RepositoryStatusBlocked  RepositoryStatus = "blocked"
	RepositoryStatusArchived RepositoryStatus = "archived"
)

// ServicesPolicyValidationStatus describes services.yaml validation result.
type ServicesPolicyValidationStatus string

const (
	ServicesPolicyValidationValid   ServicesPolicyValidationStatus = "valid"
	ServicesPolicyValidationInvalid ServicesPolicyValidationStatus = "invalid"
	ServicesPolicyValidationStale   ServicesPolicyValidationStatus = "stale"
)

// ServicesPolicyProjectionStatus describes checked policy projection state.
type ServicesPolicyProjectionStatus string

const (
	ServicesPolicyProjectionSynced     ServicesPolicyProjectionStatus = "synced"
	ServicesPolicyProjectionPending    ServicesPolicyProjectionStatus = "pending"
	ServicesPolicyProjectionFailed     ServicesPolicyProjectionStatus = "failed"
	ServicesPolicyProjectionOverridden ServicesPolicyProjectionStatus = "overridden"
)

// ServiceKind classifies a service descriptor from checked services.yaml.
type ServiceKind string

const (
	ServiceKindBackend       ServiceKind = "backend"
	ServiceKindFrontend      ServiceKind = "frontend"
	ServiceKindWorker        ServiceKind = "worker"
	ServiceKindDocumentation ServiceKind = "documentation"
	ServiceKindPackage       ServiceKind = "package"
	ServiceKindOther         ServiceKind = "other"
)

// ServiceStatus describes whether a descriptor is usable.
type ServiceStatus string

const (
	ServiceStatusActive   ServiceStatus = "active"
	ServiceStatusDisabled ServiceStatus = "disabled"
	ServiceStatusStale    ServiceStatus = "stale"
)

// DocumentationScopeType classifies where a documentation source applies.
type DocumentationScopeType string

const (
	DocumentationScopeProject     DocumentationScopeType = "project"
	DocumentationScopeService     DocumentationScopeType = "service"
	DocumentationScopeDependency  DocumentationScopeType = "dependency"
	DocumentationScopeGuidanceRef DocumentationScopeType = "guidance_ref"
)

// DocumentationAccessMode controls whether agents may edit documentation source.
type DocumentationAccessMode string

const (
	DocumentationAccessRead  DocumentationAccessMode = "read"
	DocumentationAccessWrite DocumentationAccessMode = "write"
)

// SourceAccessMode controls whether agents may edit a workspace source.
type SourceAccessMode string

const (
	SourceAccessRead  SourceAccessMode = "read"
	SourceAccessWrite SourceAccessMode = "write"
)

// DocumentationSourceStatus describes documentation source lifecycle.
type DocumentationSourceStatus string

const (
	DocumentationSourceStatusActive   DocumentationSourceStatus = "active"
	DocumentationSourceStatusDisabled DocumentationSourceStatus = "disabled"
	DocumentationSourceStatusBlocked  DocumentationSourceStatus = "blocked"
)

// MergePolicy defines an allowed merge strategy.
type MergePolicy string

const (
	MergePolicyMerge  MergePolicy = "merge"
	MergePolicySquash MergePolicy = "squash"
	MergePolicyRebase MergePolicy = "rebase"
	MergePolicyManual MergePolicy = "manual"
)

// BranchRulesStatus describes branch rules lifecycle.
type BranchRulesStatus string

const (
	BranchRulesStatusActive   BranchRulesStatus = "active"
	BranchRulesStatusDisabled BranchRulesStatus = "disabled"
)

// RolloutStrategy defines release rollout strategy.
type RolloutStrategy string

const (
	RolloutStrategyDirect RolloutStrategy = "direct"
	RolloutStrategyStaged RolloutStrategy = "staged"
	RolloutStrategyCanary RolloutStrategy = "canary"
)

// RollbackPolicy defines release rollback rule.
type RollbackPolicy string

const (
	RollbackPolicyManual           RollbackPolicy = "manual"
	RollbackPolicyAutomaticOnGate  RollbackPolicy = "automatic_on_gate"
	RollbackPolicyAutomaticOnAlert RollbackPolicy = "automatic_on_alert"
)

// ReleasePolicyStatus describes release policy lifecycle.
type ReleasePolicyStatus string

const (
	ReleasePolicyStatusActive   ReleasePolicyStatus = "active"
	ReleasePolicyStatusDisabled ReleasePolicyStatus = "disabled"
	ReleasePolicyStatusArchived ReleasePolicyStatus = "archived"
)

// PlacementPolicyStatus describes placement policy lifecycle.
type PlacementPolicyStatus string

const (
	PlacementPolicyStatusActive   PlacementPolicyStatus = "active"
	PlacementPolicyStatusDisabled PlacementPolicyStatus = "disabled"
)

// PolicyOverrideTargetType classifies overridden policy area.
type PolicyOverrideTargetType string

const (
	PolicyOverrideTargetServicesPolicy      PolicyOverrideTargetType = "services_policy"
	PolicyOverrideTargetBranchRules         PolicyOverrideTargetType = "branch_rules"
	PolicyOverrideTargetReleasePolicy       PolicyOverrideTargetType = "release_policy"
	PolicyOverrideTargetReleaseLine         PolicyOverrideTargetType = "release_line"
	PolicyOverrideTargetPlacementPolicy     PolicyOverrideTargetType = "placement_policy"
	PolicyOverrideTargetDocumentationSource PolicyOverrideTargetType = "documentation_source"
)

// PolicyOverrideStatus describes override lifecycle.
type PolicyOverrideStatus string

const (
	PolicyOverrideStatusActive    PolicyOverrideStatus = "active"
	PolicyOverrideStatusExpired   PolicyOverrideStatus = "expired"
	PolicyOverrideStatusCancelled PolicyOverrideStatus = "cancelled"
)
