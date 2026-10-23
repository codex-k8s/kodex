package imageowner

import (
	"context"
	"errors"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FailWithTerminalRecovery first tries Fail, including its dedicated expiry path.
// Only a final PermissionDenied, Aborted or NotFound triggers exact owner terminal
// readback using the original claim receipt; Unavailable/other uncertain outcomes
// are returned unchanged rather than interpreted as successful termination.
// FailWithTerminalRecovery сначала выполняет Fail с его отдельным путём expiry.
// Только итоговые PermissionDenied, Aborted либо NotFound запускают точное чтение
// terminal владельца по исходной claim-квитанции; Unavailable и прочие неопределённые
// исходы возвращаются без превращения в успешное завершение.
func (client *Client) FailWithTerminalRecovery(ctx context.Context, key, claimKey string, claim Claim, code string) error {
	err := client.Fail(ctx, key, claim, code)
	if status.Code(err) != codes.PermissionDenied && status.Code(err) != codes.Aborted && status.Code(err) != codes.NotFound {
		return err
	}
	return client.GetTerminal(ctx, claimKey, claim)
}

// GetTerminal validates the retained claim and reads its exact terminal proof with
// original receipt, fencing/build/policy/risk/source pins and bounded WaitForReady.
// It mutates no owner state and accepts success only after validateTerminal matches
// the returned proof to that snapshot, not merely because a prior write was denied.
// GetTerminal проверяет сохранённый claim и читает точное terminal-доказательство
// с исходными receipt/fencing/build/policy/risk/source и ограниченным WaitForReady.
// Состояние владельца не меняется; успех требует совпадения снимка в validateTerminal,
// а не одного лишь отказа предыдущей записи.
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

// validateTerminal permits only ACCEPTED/REJECTED/FAILED/CANCELLED proofs whose
// historical artifact and attempt/fence/authority/risk/source pins match the claim.
// Terminal artifact version must advance, terminal fence cannot regress, and
// terminal attempt version must be at least two; denial or unknown state is not proof.
// validateTerminal принимает только ACCEPTED/REJECTED/FAILED/CANCELLED с совпадающими
// историческими artifact и attempt/fence/authority/risk/source полями claim.
// Terminal version артефакта должна вырасти, fence не может уменьшиться, версия
// terminal попытки — не ниже двух; отказ либо неизвестное состояние не доказательство.
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
