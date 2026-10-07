package httptransport

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func publicRoleImageAdmissionFailure(failure *controlplanev1.RoleImageAdmissionFailure,
	recipe *controlplanev1.RoleImageRecipe, builds []*controlplanev1.ImageBuild,
) (*generated.RoleImageAdmissionFailure, bool) {
	if failure == nil {
		return nil, true
	}
	if recipe == nil || failure.GetState() != "FAILED" || failure.GetRecipeRef() != recipe.GetRef() ||
		failure.GetRecipeGeneration() != recipe.GetGeneration() || failure.GetScopeKind() != recipe.GetScopeKind() ||
		failure.GetOrganizationRef() != recipe.GetOrganizationRef() || failure.GetProjectRef() != recipe.GetProjectRef() ||
		!effectiveCapabilityRef(failure.GetImageArtifactRef()) || !effectiveCapabilityRef(failure.GetBuildRef()) ||
		failure.GetVersion() == 0 || failure.GetVersion() > 9007199254740991 ||
		failure.GetRecipeGeneration() == 0 || failure.GetRecipeGeneration() > 9007199254740991 ||
		failure.GetBuildAttempt() == 0 || failure.GetBuildAttempt() > 10 {
		return nil, false
	}
	scope := runtimeResourceScopeKind(failure.GetScopeKind().String())
	if !validRuntimeResourceScope(scope, failure.GetOrganizationRef(), failure.GetProjectRef()) {
		return nil, false
	}
	switch failure.GetErrorCode() {
	case "ADMISSION_EVIDENCE_ENTRY_EXCEEDS_BOUND", "ADMISSION_EVIDENCE_EXCEEDS_BOUND", "ADMISSION_WORKER_FAILED", "ADMISSION_LEASE_EXPIRED":
	default:
		return nil, false
	}
	matched := false
	var latest *controlplanev1.ImageBuild
	for _, build := range builds {
		if build == nil || build.GetCreatedAt() == nil || build.GetUpdatedAt() == nil || build.GetCreatedAt().CheckValid() != nil || build.GetUpdatedAt().CheckValid() != nil {
			return nil, false
		}
		if latest == nil || build.GetCreatedAt().AsTime().After(latest.GetCreatedAt().AsTime()) ||
			(build.GetCreatedAt().AsTime().Equal(latest.GetCreatedAt().AsTime()) && (build.GetUpdatedAt().AsTime().After(latest.GetUpdatedAt().AsTime()) ||
				(build.GetUpdatedAt().AsTime().Equal(latest.GetUpdatedAt().AsTime()) && (build.GetAttempt() > latest.GetAttempt() ||
					(build.GetAttempt() == latest.GetAttempt() && build.GetRef() > latest.GetRef()))))) {
			latest = build
		}
	}
	if latest == nil || latest.GetRef() != failure.GetBuildRef() {
		return nil, false
	}
	for _, build := range builds {
		if build != nil && build.GetRef() == failure.GetBuildRef() {
			matched = build.GetRecipeRef() == failure.GetRecipeRef() && build.GetRecipeGeneration() == failure.GetRecipeGeneration() &&
				build.GetAttempt() == failure.GetBuildAttempt() && build.GetScopeKind() == failure.GetScopeKind() &&
				build.GetOrganizationRef() == failure.GetOrganizationRef() && build.GetProjectRef() == failure.GetProjectRef() &&
				build.GetStage() == controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_COMPLETED
			break
		}
	}
	if !matched {
		return nil, false
	}
	return &generated.RoleImageAdmissionFailure{ImageArtifactRef: failure.GetImageArtifactRef(), Version: int64(failure.GetVersion()),
		RecipeRef: failure.GetRecipeRef(), RecipeGeneration: int64(failure.GetRecipeGeneration()), BuildRef: failure.GetBuildRef(),
		BuildAttempt: int(failure.GetBuildAttempt()), ScopeKind: scope, OrganizationRef: failure.GetOrganizationRef(),
		ProjectRef: failure.GetProjectRef(), State: generated.RoleImageAdmissionFailureState(failure.GetState()),
		ErrorCode: generated.RoleImageAdmissionFailureErrorCode(failure.GetErrorCode())}, true
}
