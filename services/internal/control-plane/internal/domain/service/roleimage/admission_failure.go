package roleimage

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// validAdmissionWorkerFailure allows only the three closed worker failure codes; lease expiry is
// assigned separately by the owner using PostgreSQL time.
// validAdmissionWorkerFailure разрешает только три закрытых кода отказа worker; истечение lease
// назначается отдельно владельцем по времени PostgreSQL.
func validAdmissionWorkerFailure(code string) bool {
	switch code {
	case "ADMISSION_EVIDENCE_ENTRY_EXCEEDS_BOUND", "ADMISSION_EVIDENCE_EXCEEDS_BOUND", "ADMISSION_WORKER_FAILED":
		return true
	default:
		return false
	}
}

// FailAdmission resolves current principal, requires exact image-admission failure authority and
// validates attempt/fence/claim/evidence pins before delegating the failure transition to the
// owner.
// FailAdmission разрешает текущий principal, требует точные полномочия отказа image-admission и
// проверяет attempt, fence, claim и evidence pins до передачи перехода отказа владельцу.
func (service *Service) FailAdmission(ctx context.Context, input roleimage.AdmissionFailureInput) (entity.RoleImageAdmissionFailure, error) {
	principal, err := service.resolvePrincipal(ctx, input.Principal)
	if err != nil {
		return entity.RoleImageAdmissionFailure{}, err
	}
	input.Principal = principal
	if err := authorizeKey(principal, "platform.role-images.admission.fail", "image-admission", input.IdempotencyKey); err != nil {
		return entity.RoleImageAdmissionFailure{}, err
	}
	if !validRef(input.ExpectedAdmissionAttemptRef, "imgadm") || input.ExpectedAdmissionAttempt == 0 || !validAdmissionWorkerFailure(input.ErrorCode) || !validRef(input.ArtifactRef, "imgart") ||
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

// ExpireAdmission requires the distinct owner-expiry operation and complete attempt/build/policy
// pins, leaving expiry determination and atomic failure state to the repository owner.
// ExpireAdmission требует отдельную операцию owner expiry и полные pins attempt, build и policy с
// сохранением определения истечения и атомарного отказа у repository владельца.
func (service *Service) ExpireAdmission(ctx context.Context, input roleimage.AdmissionExpiryInput) (entity.RoleImageAdmissionFailure, error) {
	principal, err := service.resolvePrincipal(ctx, input.Principal)
	if err != nil {
		return entity.RoleImageAdmissionFailure{}, err
	}
	input.Principal = principal
	if err := authorizeKey(principal, "platform.role-images.admission.expire", "image-admission", input.IdempotencyKey); err != nil {
		return entity.RoleImageAdmissionFailure{}, err
	}
	if !validRef(input.ExpectedAdmissionAttemptRef, "imgadm") || input.ExpectedAdmissionAttempt == 0 || !validRef(input.ArtifactRef, "imgart") || !validRef(input.BuildRef, "imgbld") || input.ExpectedVersion == 0 || input.ExpectedFence == 0 || input.ExpectedAuthorityGeneration == 0 || input.ExpectedBuildAttempt == 0 || input.ExpectedBuildAttempt > 10 || input.RecipeGeneration == 0 || input.PolicyRevision == 0 || !manifestPattern.MatchString(input.ManifestDigest) || !sha256Pattern.MatchString(input.ImmutableBuildSHA256) || !sha256Pattern.MatchString(input.ProvenanceSHA256) || !sha256Pattern.MatchString(input.PolicySHA256) || !sha256Pattern.MatchString(input.SpecSHA256) {
		return entity.RoleImageAdmissionFailure{}, errs.ErrInvalid
	}
	return service.repository.ExpireAdmission(ctx, input)
}
