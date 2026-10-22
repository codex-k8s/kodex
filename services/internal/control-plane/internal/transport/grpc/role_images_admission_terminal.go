package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetImageAdmissionTerminal resolves its exact operation principal and asks the domain for terminal
// proof under the full expected claim/authority/fence/attempt/artifact/build/policy/risk/source tuple,
// then applies closed terminal-state conversion.
// GetImageAdmissionTerminal разрешает principal точной операции и запрашивает у домена terminal proof
// по полному ожидаемому набору claim, полномочий, fence, попытки, артефакта, сборки, политики, риска и
// исходных свидетельств, затем применяет закрытое преобразование terminal state.
func (server *RoleImageServer) GetImageAdmissionTerminal(ctx context.Context, request *cp.GetImageAdmissionTerminalRequest) (*cp.GetImageAdmissionTerminalResponse, error) {
	p, err := roleImagePrincipal(ctx, cp.RoleImageService_GetImageAdmissionTerminal_FullMethodName)
	if err != nil {
		return nil, err
	}
	proof, err := server.service.GetAdmissionTerminal(ctx, repository.AdmissionTerminalInput{ClaimIdempotencyKey: request.GetClaimIdempotencyKey(), RiskAcceptanceSHA256: request.GetRiskAcceptanceSha256(), SourceAdmissionRevision: request.GetSourceAdmissionRevision(), SourceAdmissionReceiptSHA256: request.GetSourceAdmissionReceiptSha256(), SourceEvidenceManifestDigest: request.GetSourceEvidenceManifestDigest(), AdmissionExpiryInput: repository.AdmissionExpiryInput{
		Principal: p, ArtifactRef: request.GetImageArtifactRef(), ExpectedVersion: request.GetExpectedVersion(), ExpectedFence: request.GetExpectedFence(), ExpectedAuthorityGeneration: request.GetExpectedAuthorityGeneration(), ManifestDigest: request.GetManifestDigest(), ImmutableBuildSHA256: request.GetImmutableBuildSha256(), ProvenanceSHA256: request.GetProvenanceSha256(), PolicyRevision: request.GetPolicyRevision(), PolicySHA256: request.GetPolicySha256(), BuildRef: request.GetBuildRef(), ExpectedBuildAttempt: request.GetExpectedBuildAttempt(), RecipeGeneration: request.GetRecipeGeneration(), SpecSHA256: request.GetSpecSha256(), ExpectedAdmissionAttemptRef: request.GetExpectedAdmissionAttemptRef(), ExpectedAdmissionAttempt: request.GetExpectedAdmissionAttempt(),
	}})
	if err != nil {
		return nil, transportError(err)
	}
	return castImageAdmissionTerminal(proof)
}

// castImageAdmissionTerminal accepts only ACCEPTED, REJECTED, FAILED or CANCELLED and rejects
// live/unknown state with Internal before emitting a proof. It projects the domain's exact
// attempt/fence/authority/version/risk/source pins without generating authority itself.
// castImageAdmissionTerminal принимает только ACCEPTED, REJECTED, FAILED или CANCELLED и отклоняет
// активное либо неизвестное состояние с Internal до выдачи proof. Переносит точные доменные привязки
// попытки, fence, полномочий, версий, риска и исходных свидетельств, не создавая полномочия
// самостоятельно.
func castImageAdmissionTerminal(proof repository.AdmissionTerminalProof) (*cp.GetImageAdmissionTerminalResponse, error) {
	var state cp.ImageAdmissionTerminalState
	switch proof.State {
	case "ACCEPTED":
		state = cp.ImageAdmissionTerminalState_IMAGE_ADMISSION_TERMINAL_STATE_ACCEPTED
	case "REJECTED":
		state = cp.ImageAdmissionTerminalState_IMAGE_ADMISSION_TERMINAL_STATE_REJECTED
	case "FAILED":
		state = cp.ImageAdmissionTerminalState_IMAGE_ADMISSION_TERMINAL_STATE_FAILED
	case "CANCELLED":
		state = cp.ImageAdmissionTerminalState_IMAGE_ADMISSION_TERMINAL_STATE_CANCELLED
	default:
		return nil, status.Error(codes.Internal, "image admission terminal state is invalid")
	}
	return &cp.GetImageAdmissionTerminalResponse{TerminalState: state, ClaimedArtifact: castImageArtifact(proof.ClaimedArtifact), AdmissionAttemptRef: proof.AttemptRef, AdmissionAttempt: proof.Attempt, ClaimFence: proof.ClaimFence, ClaimAuthorityGeneration: proof.ClaimAuthorityGeneration, TerminalArtifactVersion: proof.TerminalArtifactVersion, TerminalFence: proof.TerminalFence, TerminalAttemptVersion: proof.TerminalAttemptVersion, RiskAcceptanceSha256: proof.RiskAcceptanceSHA256, SourceAdmissionRevision: proof.SourceAdmissionRevision, SourceAdmissionReceiptSha256: proof.SourceAdmissionReceiptSHA256, SourceEvidenceManifestDigest: proof.SourceEvidenceManifestDigest}, nil
}

// GetImageAdmissionRecoveryTerminal resolves the exact recovery principal and domain run-bound
// terminal proof, reuses the closed converter and wraps only its successful result in the recovery
// response.
// GetImageAdmissionRecoveryTerminal разрешает точный recovery principal и доменный terminal proof,
// связанный с запуском, переиспользует закрытый converter и помещает в recovery response только
// успешный результат.
func (server *RoleImageServer) GetImageAdmissionRecoveryTerminal(ctx context.Context, request *cp.GetImageAdmissionRecoveryTerminalRequest) (*cp.GetImageAdmissionRecoveryTerminalResponse, error) {
	p, err := roleImagePrincipal(ctx, cp.RoleImageService_GetImageAdmissionRecoveryTerminal_FullMethodName)
	if err != nil {
		return nil, err
	}
	proof, err := server.service.GetAdmissionRecoveryTerminal(ctx, p, request.GetAdmissionRunId())
	if err != nil {
		return nil, transportError(err)
	}
	terminal, err := castImageAdmissionTerminal(proof)
	if err != nil {
		return nil, err
	}
	return &cp.GetImageAdmissionRecoveryTerminalResponse{TerminalProof: terminal}, nil
}
