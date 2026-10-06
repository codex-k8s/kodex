package imageowner

import (
	"context"
	"errors"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Старый callback не подтверждает terminal; только отдельное exact owner read.
func (client *Client) FailWithTerminalRecovery(ctx context.Context, key, claimKey string, claim Claim, code string) error {
	err := client.Fail(ctx, key, claim, code)
	if status.Code(err) != codes.PermissionDenied && status.Code(err) != codes.Aborted && status.Code(err) != codes.NotFound {
		return err
	}
	return client.GetTerminal(ctx, claimKey, claim)
}

func (client *Client) GetTerminal(ctx context.Context, claimKey string, claim Claim) error {
	if err := ValidateAdmissionClaim(claim); err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	proof, err := client.shared.RoleImages.GetImageAdmissionTerminal(callCtx, &cp.GetImageAdmissionTerminalRequest{
		ClaimIdempotencyKey: claimKey, ImageArtifactRef: claim.ArtifactID, ExpectedVersion: claim.Version, ExpectedFence: claim.Fence, ExpectedAuthorityGeneration: claim.AuthorityGeneration,
		ManifestDigest: claim.ManifestDigest, ImmutableBuildSha256: claim.ImmutableBuildSHA256, ProvenanceSha256: claim.ProvenanceSHA256, PolicyRevision: claim.PolicyRevision, PolicySha256: claim.PolicySHA256,
		BuildRef: claim.BuildID, ExpectedBuildAttempt: claim.BuildAttempt, RecipeGeneration: claim.RecipeGeneration, SpecSha256: claim.SpecSHA256, ExpectedAdmissionAttemptRef: claim.AdmissionAttemptRef, ExpectedAdmissionAttempt: claim.AdmissionAttempt,
		RiskAcceptanceSha256: claim.RiskAcceptanceSHA256, SourceAdmissionRevision: claim.SourceAdmissionRevision, SourceAdmissionReceiptSha256: claim.SourceAdmissionReceiptSHA256, SourceEvidenceManifestDigest: claim.SourceEvidenceManifestDigest,
	}, grpc.WaitForReady(true))
	if err != nil {
		return err
	}
	return validateTerminal(proof, claim)
}

func validateTerminal(proof *cp.GetImageAdmissionTerminalResponse, claim Claim) error {
	switch proof.GetTerminalState() {
	case cp.ImageAdmissionTerminalState_IMAGE_ADMISSION_TERMINAL_STATE_ACCEPTED, cp.ImageAdmissionTerminalState_IMAGE_ADMISSION_TERMINAL_STATE_REJECTED, cp.ImageAdmissionTerminalState_IMAGE_ADMISSION_TERMINAL_STATE_FAILED, cp.ImageAdmissionTerminalState_IMAGE_ADMISSION_TERMINAL_STATE_CANCELLED:
	default:
		return errors.New("image admission terminal state is invalid")
	}
	a := proof.GetClaimedArtifact()
	if proof.GetRiskAcceptanceSha256() != claim.RiskAcceptanceSHA256 || proof.GetSourceAdmissionRevision() != claim.SourceAdmissionRevision || proof.GetSourceAdmissionReceiptSha256() != claim.SourceAdmissionReceiptSHA256 || proof.GetSourceEvidenceManifestDigest() != claim.SourceEvidenceManifestDigest || a == nil || a.GetRef() != claim.ArtifactID || a.GetVersion() != claim.Version || a.GetRecipeRef() != claim.RecipeID || a.GetRecipeGeneration() != claim.RecipeGeneration ||
		a.GetBuildRef() != claim.BuildID || a.GetBuildAttempt() != claim.BuildAttempt || a.GetManifestDigest() != claim.ManifestDigest || a.GetImmutableBuildSha256() != claim.ImmutableBuildSHA256 || a.GetProvenanceSha256() != claim.ProvenanceSHA256 ||
		a.GetPolicyRevision() != claim.PolicyRevision || a.GetPolicySha256() != claim.PolicySHA256 || a.GetSpecSha256() != claim.SpecSHA256 || scopeName(a.GetScopeKind()) != claim.ScopeKind || a.GetOrganizationRef() != claim.OrganizationRef || a.GetProjectRef() != claim.ProjectRef ||
		proof.GetAdmissionAttemptRef() != claim.AdmissionAttemptRef || proof.GetAdmissionAttempt() != claim.AdmissionAttempt || proof.GetClaimFence() != claim.Fence || proof.GetClaimAuthorityGeneration() != claim.AuthorityGeneration ||
		proof.GetTerminalArtifactVersion() <= claim.Version || proof.GetTerminalFence() < claim.Fence || proof.GetTerminalAttemptVersion() < 2 {
		return errors.New("image admission terminal proof is incomplete")
	}
	return nil
}
