package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
