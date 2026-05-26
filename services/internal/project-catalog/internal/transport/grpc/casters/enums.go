package casters

import (
	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
)

type domainEnum interface {
	~string
}

var projectStatuses = map[projectsv1.ProjectStatus]enum.ProjectStatus{
	projectsv1.ProjectStatus_PROJECT_STATUS_ACTIVE:   enum.ProjectStatusActive,
	projectsv1.ProjectStatus_PROJECT_STATUS_ARCHIVED: enum.ProjectStatusArchived,
	projectsv1.ProjectStatus_PROJECT_STATUS_DISABLED: enum.ProjectStatusDisabled,
}

var repositoryProviders = map[projectsv1.RepositoryProvider]enum.RepositoryProvider{
	projectsv1.RepositoryProvider_REPOSITORY_PROVIDER_GITHUB: enum.RepositoryProviderGitHub,
	projectsv1.RepositoryProvider_REPOSITORY_PROVIDER_GITLAB: enum.RepositoryProviderGitLab,
}

var repositoryStatuses = map[projectsv1.RepositoryStatus]enum.RepositoryStatus{
	projectsv1.RepositoryStatus_REPOSITORY_STATUS_ACTIVE:   enum.RepositoryStatusActive,
	projectsv1.RepositoryStatus_REPOSITORY_STATUS_PENDING:  enum.RepositoryStatusPending,
	projectsv1.RepositoryStatus_REPOSITORY_STATUS_BLOCKED:  enum.RepositoryStatusBlocked,
	projectsv1.RepositoryStatus_REPOSITORY_STATUS_ARCHIVED: enum.RepositoryStatusArchived,
}

var repositoryOwnerKinds = map[projectsv1.RepositoryOwnerKind]enum.RepositoryOwnerKind{
	projectsv1.RepositoryOwnerKind_REPOSITORY_OWNER_KIND_ORGANIZATION:       enum.RepositoryOwnerKindOrganization,
	projectsv1.RepositoryOwnerKind_REPOSITORY_OWNER_KIND_AUTHENTICATED_USER: enum.RepositoryOwnerKindAuthenticatedUser,
}

var repositoryVisibilities = map[projectsv1.RepositoryVisibility]enum.RepositoryVisibility{
	projectsv1.RepositoryVisibility_REPOSITORY_VISIBILITY_PUBLIC:   enum.RepositoryVisibilityPublic,
	projectsv1.RepositoryVisibility_REPOSITORY_VISIBILITY_PRIVATE:  enum.RepositoryVisibilityPrivate,
	projectsv1.RepositoryVisibility_REPOSITORY_VISIBILITY_INTERNAL: enum.RepositoryVisibilityInternal,
}

var validationStatuses = map[projectsv1.ServicesPolicyValidationStatus]enum.ServicesPolicyValidationStatus{
	projectsv1.ServicesPolicyValidationStatus_SERVICES_POLICY_VALIDATION_STATUS_VALID:   enum.ServicesPolicyValidationValid,
	projectsv1.ServicesPolicyValidationStatus_SERVICES_POLICY_VALIDATION_STATUS_INVALID: enum.ServicesPolicyValidationInvalid,
	projectsv1.ServicesPolicyValidationStatus_SERVICES_POLICY_VALIDATION_STATUS_STALE:   enum.ServicesPolicyValidationStale,
}

var projectionStatuses = map[enum.ServicesPolicyProjectionStatus]projectsv1.ServicesPolicyProjectionStatus{
	enum.ServicesPolicyProjectionSynced:     projectsv1.ServicesPolicyProjectionStatus_SERVICES_POLICY_PROJECTION_STATUS_SYNCED,
	enum.ServicesPolicyProjectionPending:    projectsv1.ServicesPolicyProjectionStatus_SERVICES_POLICY_PROJECTION_STATUS_PENDING,
	enum.ServicesPolicyProjectionFailed:     projectsv1.ServicesPolicyProjectionStatus_SERVICES_POLICY_PROJECTION_STATUS_FAILED,
	enum.ServicesPolicyProjectionOverridden: projectsv1.ServicesPolicyProjectionStatus_SERVICES_POLICY_PROJECTION_STATUS_OVERRIDDEN,
}

var serviceKinds = map[projectsv1.ServiceKind]enum.ServiceKind{
	projectsv1.ServiceKind_SERVICE_KIND_BACKEND:       enum.ServiceKindBackend,
	projectsv1.ServiceKind_SERVICE_KIND_FRONTEND:      enum.ServiceKindFrontend,
	projectsv1.ServiceKind_SERVICE_KIND_WORKER:        enum.ServiceKindWorker,
	projectsv1.ServiceKind_SERVICE_KIND_DOCUMENTATION: enum.ServiceKindDocumentation,
	projectsv1.ServiceKind_SERVICE_KIND_PACKAGE:       enum.ServiceKindPackage,
	projectsv1.ServiceKind_SERVICE_KIND_OTHER:         enum.ServiceKindOther,
}

var serviceStatuses = map[projectsv1.ServiceStatus]enum.ServiceStatus{
	projectsv1.ServiceStatus_SERVICE_STATUS_ACTIVE:   enum.ServiceStatusActive,
	projectsv1.ServiceStatus_SERVICE_STATUS_DISABLED: enum.ServiceStatusDisabled,
	projectsv1.ServiceStatus_SERVICE_STATUS_STALE:    enum.ServiceStatusStale,
}

var documentationScopes = map[projectsv1.DocumentationScopeType]enum.DocumentationScopeType{
	projectsv1.DocumentationScopeType_DOCUMENTATION_SCOPE_TYPE_PROJECT:      enum.DocumentationScopeProject,
	projectsv1.DocumentationScopeType_DOCUMENTATION_SCOPE_TYPE_SERVICE:      enum.DocumentationScopeService,
	projectsv1.DocumentationScopeType_DOCUMENTATION_SCOPE_TYPE_DEPENDENCY:   enum.DocumentationScopeDependency,
	projectsv1.DocumentationScopeType_DOCUMENTATION_SCOPE_TYPE_GUIDANCE_REF: enum.DocumentationScopeGuidanceRef,
}

var documentationAccessModes = map[projectsv1.DocumentationAccessMode]enum.DocumentationAccessMode{
	projectsv1.DocumentationAccessMode_DOCUMENTATION_ACCESS_MODE_READ:  enum.DocumentationAccessRead,
	projectsv1.DocumentationAccessMode_DOCUMENTATION_ACCESS_MODE_WRITE: enum.DocumentationAccessWrite,
}

var sourceAccessModes = map[enum.SourceAccessMode]projectsv1.SourceAccessMode{
	enum.SourceAccessRead:  projectsv1.SourceAccessMode_SOURCE_ACCESS_MODE_READ,
	enum.SourceAccessWrite: projectsv1.SourceAccessMode_SOURCE_ACCESS_MODE_WRITE,
}

var documentationStatuses = map[projectsv1.DocumentationSourceStatus]enum.DocumentationSourceStatus{
	projectsv1.DocumentationSourceStatus_DOCUMENTATION_SOURCE_STATUS_ACTIVE:   enum.DocumentationSourceStatusActive,
	projectsv1.DocumentationSourceStatus_DOCUMENTATION_SOURCE_STATUS_DISABLED: enum.DocumentationSourceStatusDisabled,
	projectsv1.DocumentationSourceStatus_DOCUMENTATION_SOURCE_STATUS_BLOCKED:  enum.DocumentationSourceStatusBlocked,
}

var mergePolicies = map[projectsv1.MergePolicy]enum.MergePolicy{
	projectsv1.MergePolicy_MERGE_POLICY_MERGE:  enum.MergePolicyMerge,
	projectsv1.MergePolicy_MERGE_POLICY_SQUASH: enum.MergePolicySquash,
	projectsv1.MergePolicy_MERGE_POLICY_REBASE: enum.MergePolicyRebase,
	projectsv1.MergePolicy_MERGE_POLICY_MANUAL: enum.MergePolicyManual,
}

var branchRulesStatuses = map[projectsv1.BranchRulesStatus]enum.BranchRulesStatus{
	projectsv1.BranchRulesStatus_BRANCH_RULES_STATUS_ACTIVE:   enum.BranchRulesStatusActive,
	projectsv1.BranchRulesStatus_BRANCH_RULES_STATUS_DISABLED: enum.BranchRulesStatusDisabled,
}

var rolloutStrategies = map[projectsv1.RolloutStrategy]enum.RolloutStrategy{
	projectsv1.RolloutStrategy_ROLLOUT_STRATEGY_DIRECT: enum.RolloutStrategyDirect,
	projectsv1.RolloutStrategy_ROLLOUT_STRATEGY_STAGED: enum.RolloutStrategyStaged,
	projectsv1.RolloutStrategy_ROLLOUT_STRATEGY_CANARY: enum.RolloutStrategyCanary,
}

var rollbackPolicies = map[projectsv1.RollbackPolicy]enum.RollbackPolicy{
	projectsv1.RollbackPolicy_ROLLBACK_POLICY_MANUAL:             enum.RollbackPolicyManual,
	projectsv1.RollbackPolicy_ROLLBACK_POLICY_AUTOMATIC_ON_GATE:  enum.RollbackPolicyAutomaticOnGate,
	projectsv1.RollbackPolicy_ROLLBACK_POLICY_AUTOMATIC_ON_ALERT: enum.RollbackPolicyAutomaticOnAlert,
}

var releaseStatuses = map[projectsv1.ReleasePolicyStatus]enum.ReleasePolicyStatus{
	projectsv1.ReleasePolicyStatus_RELEASE_POLICY_STATUS_ACTIVE:   enum.ReleasePolicyStatusActive,
	projectsv1.ReleasePolicyStatus_RELEASE_POLICY_STATUS_DISABLED: enum.ReleasePolicyStatusDisabled,
	projectsv1.ReleasePolicyStatus_RELEASE_POLICY_STATUS_ARCHIVED: enum.ReleasePolicyStatusArchived,
}

var placementStatuses = map[projectsv1.PlacementPolicyStatus]enum.PlacementPolicyStatus{
	projectsv1.PlacementPolicyStatus_PLACEMENT_POLICY_STATUS_ACTIVE:   enum.PlacementPolicyStatusActive,
	projectsv1.PlacementPolicyStatus_PLACEMENT_POLICY_STATUS_DISABLED: enum.PlacementPolicyStatusDisabled,
}

var policyOverrideTargets = map[projectsv1.PolicyOverrideTargetType]enum.PolicyOverrideTargetType{
	projectsv1.PolicyOverrideTargetType_POLICY_OVERRIDE_TARGET_TYPE_SERVICES_POLICY:      enum.PolicyOverrideTargetServicesPolicy,
	projectsv1.PolicyOverrideTargetType_POLICY_OVERRIDE_TARGET_TYPE_BRANCH_RULES:         enum.PolicyOverrideTargetBranchRules,
	projectsv1.PolicyOverrideTargetType_POLICY_OVERRIDE_TARGET_TYPE_RELEASE_POLICY:       enum.PolicyOverrideTargetReleasePolicy,
	projectsv1.PolicyOverrideTargetType_POLICY_OVERRIDE_TARGET_TYPE_RELEASE_LINE:         enum.PolicyOverrideTargetReleaseLine,
	projectsv1.PolicyOverrideTargetType_POLICY_OVERRIDE_TARGET_TYPE_PLACEMENT_POLICY:     enum.PolicyOverrideTargetPlacementPolicy,
	projectsv1.PolicyOverrideTargetType_POLICY_OVERRIDE_TARGET_TYPE_DOCUMENTATION_SOURCE: enum.PolicyOverrideTargetDocumentationSource,
}

var policyOverrideStatuses = map[projectsv1.PolicyOverrideStatus]enum.PolicyOverrideStatus{
	projectsv1.PolicyOverrideStatus_POLICY_OVERRIDE_STATUS_ACTIVE:    enum.PolicyOverrideStatusActive,
	projectsv1.PolicyOverrideStatus_POLICY_OVERRIDE_STATUS_EXPIRED:   enum.PolicyOverrideStatusExpired,
	projectsv1.PolicyOverrideStatus_POLICY_OVERRIDE_STATUS_CANCELLED: enum.PolicyOverrideStatusCancelled,
}

func projectStatusFromProto(status projectsv1.ProjectStatus) (enum.ProjectStatus, error) {
	return enumFromProto(status, projectsv1.ProjectStatus_PROJECT_STATUS_UNSPECIFIED, projectStatuses, true)
}

func ProjectStatusToProto(status enum.ProjectStatus) projectsv1.ProjectStatus {
	return enumToProto(status, projectsv1.ProjectStatus_PROJECT_STATUS_UNSPECIFIED, invertEnum(projectStatuses))
}

func repositoryProviderFromProto(provider projectsv1.RepositoryProvider) (enum.RepositoryProvider, error) {
	return enumFromProto(provider, projectsv1.RepositoryProvider_REPOSITORY_PROVIDER_UNSPECIFIED, repositoryProviders, false)
}

func RepositoryProviderToProto(provider enum.RepositoryProvider) projectsv1.RepositoryProvider {
	return enumToProto(provider, projectsv1.RepositoryProvider_REPOSITORY_PROVIDER_UNSPECIFIED, invertEnum(repositoryProviders))
}

func repositoryStatusFromProto(status projectsv1.RepositoryStatus) (enum.RepositoryStatus, error) {
	return enumFromProto(status, projectsv1.RepositoryStatus_REPOSITORY_STATUS_UNSPECIFIED, repositoryStatuses, true)
}

func RepositoryStatusToProto(status enum.RepositoryStatus) projectsv1.RepositoryStatus {
	return enumToProto(status, projectsv1.RepositoryStatus_REPOSITORY_STATUS_UNSPECIFIED, invertEnum(repositoryStatuses))
}

func repositoryOwnerKindFromProto(kind projectsv1.RepositoryOwnerKind) (enum.RepositoryOwnerKind, error) {
	return enumFromProto(kind, projectsv1.RepositoryOwnerKind_REPOSITORY_OWNER_KIND_UNSPECIFIED, repositoryOwnerKinds, false)
}

func repositoryVisibilityFromProto(visibility projectsv1.RepositoryVisibility) (enum.RepositoryVisibility, error) {
	return enumFromProto(visibility, projectsv1.RepositoryVisibility_REPOSITORY_VISIBILITY_UNSPECIFIED, repositoryVisibilities, false)
}

func validationStatusFromProto(status projectsv1.ServicesPolicyValidationStatus) (enum.ServicesPolicyValidationStatus, error) {
	return enumFromProto(status, projectsv1.ServicesPolicyValidationStatus_SERVICES_POLICY_VALIDATION_STATUS_UNSPECIFIED, validationStatuses, true)
}

func ValidationStatusToProto(status enum.ServicesPolicyValidationStatus) projectsv1.ServicesPolicyValidationStatus {
	return enumToProto(status, projectsv1.ServicesPolicyValidationStatus_SERVICES_POLICY_VALIDATION_STATUS_UNSPECIFIED, invertEnum(validationStatuses))
}

func ProjectionStatusToProto(status enum.ServicesPolicyProjectionStatus) projectsv1.ServicesPolicyProjectionStatus {
	return enumToProto(status, projectsv1.ServicesPolicyProjectionStatus_SERVICES_POLICY_PROJECTION_STATUS_UNSPECIFIED, projectionStatuses)
}

func serviceKindFromProto(kind projectsv1.ServiceKind) (enum.ServiceKind, error) {
	return enumFromProto(kind, projectsv1.ServiceKind_SERVICE_KIND_UNSPECIFIED, serviceKinds, false)
}

func ServiceKindToProto(kind enum.ServiceKind) projectsv1.ServiceKind {
	return enumToProto(kind, projectsv1.ServiceKind_SERVICE_KIND_UNSPECIFIED, invertEnum(serviceKinds))
}

func serviceStatusFromProto(status projectsv1.ServiceStatus) (enum.ServiceStatus, error) {
	return enumFromProto(status, projectsv1.ServiceStatus_SERVICE_STATUS_UNSPECIFIED, serviceStatuses, true)
}

func ServiceStatusToProto(status enum.ServiceStatus) projectsv1.ServiceStatus {
	return enumToProto(status, projectsv1.ServiceStatus_SERVICE_STATUS_UNSPECIFIED, invertEnum(serviceStatuses))
}

func documentationScopeFromProto(scope projectsv1.DocumentationScopeType) (enum.DocumentationScopeType, error) {
	return enumFromProto(scope, projectsv1.DocumentationScopeType_DOCUMENTATION_SCOPE_TYPE_UNSPECIFIED, documentationScopes, true)
}

func DocumentationScopeToProto(scope enum.DocumentationScopeType) projectsv1.DocumentationScopeType {
	return enumToProto(scope, projectsv1.DocumentationScopeType_DOCUMENTATION_SCOPE_TYPE_UNSPECIFIED, invertEnum(documentationScopes))
}

func documentationAccessFromProto(mode projectsv1.DocumentationAccessMode) (enum.DocumentationAccessMode, error) {
	return enumFromProto(mode, projectsv1.DocumentationAccessMode_DOCUMENTATION_ACCESS_MODE_UNSPECIFIED, documentationAccessModes, true)
}

func DocumentationAccessToProto(mode enum.DocumentationAccessMode) projectsv1.DocumentationAccessMode {
	return enumToProto(mode, projectsv1.DocumentationAccessMode_DOCUMENTATION_ACCESS_MODE_UNSPECIFIED, invertEnum(documentationAccessModes))
}

func sourceAccessToProto(mode enum.SourceAccessMode) projectsv1.SourceAccessMode {
	return enumToProto(mode, projectsv1.SourceAccessMode_SOURCE_ACCESS_MODE_UNSPECIFIED, sourceAccessModes)
}

func documentationStatusFromProto(status projectsv1.DocumentationSourceStatus) (enum.DocumentationSourceStatus, error) {
	return enumFromProto(status, projectsv1.DocumentationSourceStatus_DOCUMENTATION_SOURCE_STATUS_UNSPECIFIED, documentationStatuses, true)
}

func DocumentationStatusToProto(status enum.DocumentationSourceStatus) projectsv1.DocumentationSourceStatus {
	return enumToProto(status, projectsv1.DocumentationSourceStatus_DOCUMENTATION_SOURCE_STATUS_UNSPECIFIED, invertEnum(documentationStatuses))
}

func mergePolicyFromProto(policy projectsv1.MergePolicy) (enum.MergePolicy, error) {
	return enumFromProto(policy, projectsv1.MergePolicy_MERGE_POLICY_UNSPECIFIED, mergePolicies, true)
}

func MergePolicyToProto(policy enum.MergePolicy) projectsv1.MergePolicy {
	return enumToProto(policy, projectsv1.MergePolicy_MERGE_POLICY_UNSPECIFIED, invertEnum(mergePolicies))
}

func branchRulesStatusFromProto(status projectsv1.BranchRulesStatus) (enum.BranchRulesStatus, error) {
	return enumFromProto(status, projectsv1.BranchRulesStatus_BRANCH_RULES_STATUS_UNSPECIFIED, branchRulesStatuses, true)
}

func BranchRulesStatusToProto(status enum.BranchRulesStatus) projectsv1.BranchRulesStatus {
	return enumToProto(status, projectsv1.BranchRulesStatus_BRANCH_RULES_STATUS_UNSPECIFIED, invertEnum(branchRulesStatuses))
}

func rolloutStrategyFromProto(strategy projectsv1.RolloutStrategy) (enum.RolloutStrategy, error) {
	return enumFromProto(strategy, projectsv1.RolloutStrategy_ROLLOUT_STRATEGY_UNSPECIFIED, rolloutStrategies, true)
}

func RolloutStrategyToProto(strategy enum.RolloutStrategy) projectsv1.RolloutStrategy {
	return enumToProto(strategy, projectsv1.RolloutStrategy_ROLLOUT_STRATEGY_UNSPECIFIED, invertEnum(rolloutStrategies))
}

func rollbackPolicyFromProto(policy projectsv1.RollbackPolicy) (enum.RollbackPolicy, error) {
	return enumFromProto(policy, projectsv1.RollbackPolicy_ROLLBACK_POLICY_UNSPECIFIED, rollbackPolicies, true)
}

func RollbackPolicyToProto(policy enum.RollbackPolicy) projectsv1.RollbackPolicy {
	return enumToProto(policy, projectsv1.RollbackPolicy_ROLLBACK_POLICY_UNSPECIFIED, invertEnum(rollbackPolicies))
}

func releaseStatusFromProto(status projectsv1.ReleasePolicyStatus) (enum.ReleasePolicyStatus, error) {
	return enumFromProto(status, projectsv1.ReleasePolicyStatus_RELEASE_POLICY_STATUS_UNSPECIFIED, releaseStatuses, true)
}

func ReleaseStatusToProto(status enum.ReleasePolicyStatus) projectsv1.ReleasePolicyStatus {
	return enumToProto(status, projectsv1.ReleasePolicyStatus_RELEASE_POLICY_STATUS_UNSPECIFIED, invertEnum(releaseStatuses))
}

func placementStatusFromProto(status projectsv1.PlacementPolicyStatus) (enum.PlacementPolicyStatus, error) {
	return enumFromProto(status, projectsv1.PlacementPolicyStatus_PLACEMENT_POLICY_STATUS_UNSPECIFIED, placementStatuses, true)
}

func PlacementStatusToProto(status enum.PlacementPolicyStatus) projectsv1.PlacementPolicyStatus {
	return enumToProto(status, projectsv1.PlacementPolicyStatus_PLACEMENT_POLICY_STATUS_UNSPECIFIED, invertEnum(placementStatuses))
}

func policyOverrideTargetFromProto(target projectsv1.PolicyOverrideTargetType) (enum.PolicyOverrideTargetType, error) {
	return enumFromProto(target, projectsv1.PolicyOverrideTargetType_POLICY_OVERRIDE_TARGET_TYPE_UNSPECIFIED, policyOverrideTargets, false)
}

func PolicyOverrideTargetToProto(target enum.PolicyOverrideTargetType) projectsv1.PolicyOverrideTargetType {
	return enumToProto(target, projectsv1.PolicyOverrideTargetType_POLICY_OVERRIDE_TARGET_TYPE_UNSPECIFIED, invertEnum(policyOverrideTargets))
}

func policyOverrideStatusFromProto(status projectsv1.PolicyOverrideStatus) (enum.PolicyOverrideStatus, error) {
	return enumFromProto(status, projectsv1.PolicyOverrideStatus_POLICY_OVERRIDE_STATUS_UNSPECIFIED, policyOverrideStatuses, true)
}

func PolicyOverrideStatusToProto(status enum.PolicyOverrideStatus) projectsv1.PolicyOverrideStatus {
	return enumToProto(status, projectsv1.PolicyOverrideStatus_POLICY_OVERRIDE_STATUS_UNSPECIFIED, invertEnum(policyOverrideStatuses))
}

func enumFromProto[P comparable, D domainEnum](value P, unspecified P, mapping map[P]D, allowUnspecified bool) (D, error) {
	var zero D
	if value == unspecified {
		if allowUnspecified {
			return zero, nil
		}
		return zero, errs.ErrInvalidArgument
	}
	casted, ok := mapping[value]
	if !ok {
		return zero, errs.ErrInvalidArgument
	}
	return casted, nil
}

func enumToProto[D domainEnum, P comparable](value D, unspecified P, mapping map[D]P) P {
	casted, ok := mapping[value]
	if !ok {
		return unspecified
	}
	return casted
}

func invertEnum[P comparable, D domainEnum](mapping map[P]D) map[D]P {
	result := make(map[D]P, len(mapping))
	for protoValue, domainValue := range mapping {
		result[domainValue] = protoValue
	}
	return result
}
