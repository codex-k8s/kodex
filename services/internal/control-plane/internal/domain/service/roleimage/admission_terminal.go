package roleimage

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
)

func (service *Service) GetAdmissionTerminal(ctx context.Context, input repository.AdmissionTerminalInput) (repository.AdmissionTerminalProof, error) {
	principal, err := service.resolvePrincipal(ctx, input.Principal)
	if err != nil {
		return repository.AdmissionTerminalProof{}, err
	}
	input.Principal = principal
	if err := authorizeKey(principal, "platform.role-images.admission.terminal.get", "image-admission", input.ClaimIdempotencyKey); err != nil {
		return repository.AdmissionTerminalProof{}, err
	}
	if (input.RiskAcceptanceSHA256 != "" && !sha256Pattern.MatchString(input.RiskAcceptanceSHA256)) || (input.SourceAdmissionReceiptSHA256 != "" && !sha256Pattern.MatchString(input.SourceAdmissionReceiptSHA256)) || (input.SourceEvidenceManifestDigest != "" && !manifestPattern.MatchString(input.SourceEvidenceManifestDigest)) {
		return repository.AdmissionTerminalProof{}, errs.ErrInvalid
	}
	if !validRef(input.ExpectedAdmissionAttemptRef, "imgadm") || input.ExpectedAdmissionAttempt == 0 || !validRef(input.ArtifactRef, "imgart") || !validRef(input.BuildRef, "imgbld") || input.ExpectedVersion == 0 || input.ExpectedFence == 0 || input.ExpectedAuthorityGeneration == 0 || input.ExpectedBuildAttempt == 0 || input.ExpectedBuildAttempt > 10 || input.RecipeGeneration == 0 || input.PolicyRevision == 0 || !manifestPattern.MatchString(input.ManifestDigest) || !sha256Pattern.MatchString(input.ImmutableBuildSHA256) || !sha256Pattern.MatchString(input.ProvenanceSHA256) || !sha256Pattern.MatchString(input.PolicySHA256) || !sha256Pattern.MatchString(input.SpecSHA256) {
		return repository.AdmissionTerminalProof{}, errs.ErrInvalid
	}
	return service.repository.GetAdmissionTerminal(ctx, input)
}
