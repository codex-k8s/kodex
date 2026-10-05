package roleimage

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// Expiry назначает только owner по времени PostgreSQL, не вызывающий worker.
func validAdmissionWorkerFailure(code string) bool {
	switch code {
	case "ADMISSION_EVIDENCE_ENTRY_EXCEEDS_BOUND", "ADMISSION_EVIDENCE_EXCEEDS_BOUND", "ADMISSION_WORKER_FAILED":
		return true
	default:
		return false
	}
}

func (service *Service) FailAdmission(ctx context.Context, input roleimage.AdmissionFailureInput) (entity.RoleImageAdmissionFailure, error) {
	principal, err := service.resolvePrincipal(ctx, input.Principal)
	if err != nil {
		return entity.RoleImageAdmissionFailure{}, err
	}
	input.Principal = principal
	if err := authorizeKey(principal, "platform.role-images.admission.fail", "image-admission", input.IdempotencyKey); err != nil {
		return entity.RoleImageAdmissionFailure{}, err
	}
	if !validAdmissionWorkerFailure(input.ErrorCode) || !validRef(input.ArtifactRef, "imgart") ||
		!validRef(input.BuildRef, "imgbld") || input.ExpectedVersion == 0 || input.ExpectedFence == 0 ||
		input.ExpectedAuthorityGeneration == 0 || input.ExpectedBuildAttempt == 0 || input.ExpectedBuildAttempt > 10 ||
		input.RecipeGeneration == 0 || len(input.ClaimToken) < 32 || len(input.ClaimToken) > 512 || input.PolicyRevision == 0 ||
		!manifestPattern.MatchString(input.ManifestDigest) || !sha256Pattern.MatchString(input.ImmutableBuildSHA256) ||
		!sha256Pattern.MatchString(input.ProvenanceSHA256) || !sha256Pattern.MatchString(input.PolicySHA256) ||
		!sha256Pattern.MatchString(input.SpecSHA256) {
		return entity.RoleImageAdmissionFailure{}, errs.ErrInvalid
	}
	return service.repository.FailAdmission(ctx, input)
}

func (service *Service) ExpireAdmission(ctx context.Context, input roleimage.AdmissionExpiryInput) (entity.RoleImageAdmissionFailure, error) {
	principal, err := service.resolvePrincipal(ctx, input.Principal)
	if err != nil {
		return entity.RoleImageAdmissionFailure{}, err
	}
	input.Principal = principal
	if err := authorizeKey(principal, "platform.role-images.admission.expire", "image-admission", input.IdempotencyKey); err != nil {
		return entity.RoleImageAdmissionFailure{}, err
	}
	if !validRef(input.ArtifactRef, "imgart") || !validRef(input.BuildRef, "imgbld") || input.ExpectedVersion == 0 || input.ExpectedFence == 0 || input.ExpectedAuthorityGeneration == 0 || input.ExpectedBuildAttempt == 0 || input.ExpectedBuildAttempt > 10 || input.RecipeGeneration == 0 || input.PolicyRevision == 0 || !manifestPattern.MatchString(input.ManifestDigest) || !sha256Pattern.MatchString(input.ImmutableBuildSHA256) || !sha256Pattern.MatchString(input.ProvenanceSHA256) || !sha256Pattern.MatchString(input.PolicySHA256) || !sha256Pattern.MatchString(input.SpecSHA256) {
		return entity.RoleImageAdmissionFailure{}, errs.ErrInvalid
	}
	return service.repository.ExpireAdmission(ctx, input)
}
