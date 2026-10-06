package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repositoryport "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/role_images_get_admission_terminal.sql
var queryRoleImagesGetAdmissionTerminal string

//go:embed sql/role_images_get_admission_claim_receipt.sql
var queryRoleImagesGetAdmissionClaimReceipt string

func matchesAdmissionTerminalReceipt(receipt admissionClaimReceipt, input repositoryport.AdmissionTerminalInput) bool {
	a := receipt.Artifact
	return receipt.RiskAcceptanceSHA256 == input.RiskAcceptanceSHA256 && receipt.SourceAdmissionRevision == input.SourceAdmissionRevision && receipt.SourceAdmissionReceiptSHA256 == input.SourceAdmissionReceiptSHA256 && receipt.SourceEvidenceManifestDigest == input.SourceEvidenceManifestDigest && a.Ref == input.ArtifactRef && a.Version == input.ExpectedVersion && receipt.Fence == input.ExpectedFence &&
		receipt.AuthorityGeneration == input.ExpectedAuthorityGeneration && receipt.AdmissionAttemptRef == input.ExpectedAdmissionAttemptRef && receipt.AdmissionAttempt == input.ExpectedAdmissionAttempt &&
		a.ManifestDigest == input.ManifestDigest && a.ImmutableBuildSHA256 == input.ImmutableBuildSHA256 && a.ProvenanceSHA256 == input.ProvenanceSHA256 &&
		a.PolicyRevision == input.PolicyRevision && a.PolicySHA256 == input.PolicySHA256 && a.BuildRef == input.BuildRef && a.BuildAttempt == input.ExpectedBuildAttempt && a.RecipeGeneration == input.RecipeGeneration && a.SpecSHA256 == input.SpecSHA256
}

func (repository *Repository) GetAdmissionTerminal(ctx context.Context, input repositoryport.AdmissionTerminalInput) (repositoryport.AdmissionTerminalProof, error) {
	current, err := repository.resolveScope(ctx, input.Principal)
	if err != nil {
		return repositoryport.AdmissionTerminalProof{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return repositoryport.AdmissionTerminalProof{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var receipt admissionClaimReceipt
	var intent string
	var payload []byte
	err = tx.QueryRow(ctx, queryRoleImagesGetAdmissionClaimReceipt, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "claim_key": input.ClaimIdempotencyKey}).Scan(&intent, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return repositoryport.AdmissionTerminalProof{}, errs.ErrForbidden
	}
	if err != nil {
		return repositoryport.AdmissionTerminalProof{}, errs.ErrUnavailable
	}
	if intent != roleImageDigest(struct{ Key string }{input.ClaimIdempotencyKey}) || json.Unmarshal(payload, &receipt) != nil || !matchesAdmissionTerminalReceipt(receipt, input) || receipt.AuthorityGeneration > input.Principal.CredentialRevision {
		return repositoryport.AdmissionTerminalProof{}, errs.ErrForbidden
	}
	result := repositoryport.AdmissionTerminalProof{ClaimedArtifact: receipt.Artifact, AttemptRef: receipt.AdmissionAttemptRef, Attempt: receipt.AdmissionAttempt, ClaimFence: receipt.Fence, ClaimAuthorityGeneration: receipt.AuthorityGeneration, RiskAcceptanceSHA256: receipt.RiskAcceptanceSHA256, SourceAdmissionRevision: receipt.SourceAdmissionRevision, SourceAdmissionReceiptSHA256: receipt.SourceAdmissionReceiptSHA256, SourceEvidenceManifestDigest: receipt.SourceEvidenceManifestDigest}
	err = tx.QueryRow(ctx, queryRoleImagesGetAdmissionTerminal, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "artifact_ref": receipt.Artifact.Ref, "attempt_ref": receipt.AdmissionAttemptRef, "attempt": receipt.AdmissionAttempt,
		"scope_kind": receipt.Artifact.ScopeKind, "organization_ref": receipt.Artifact.OrganizationRef, "project_ref": receipt.Artifact.ProjectRef,
		"build_ref": receipt.Artifact.BuildRef, "build_attempt": receipt.Artifact.BuildAttempt, "recipe_generation": receipt.Artifact.RecipeGeneration,
		"recipe_ref":      receipt.Artifact.RecipeRef,
		"manifest_digest": receipt.Artifact.ManifestDigest, "immutable_build_sha256": receipt.Artifact.ImmutableBuildSHA256, "provenance_sha256": receipt.Artifact.ProvenanceSHA256,
		"policy_revision": receipt.Artifact.PolicyRevision, "policy_sha256": receipt.Artifact.PolicySHA256, "spec_sha256": receipt.Artifact.SpecSHA256,
		"claim_version": receipt.Artifact.Version, "claim_fence": receipt.Fence,
		"source_admission_revision": receipt.SourceAdmissionRevision, "source_receipt_sha256": receipt.SourceAdmissionReceiptSHA256, "source_evidence_manifest_digest": receipt.SourceEvidenceManifestDigest, "risk_acceptance_sha256": receipt.RiskAcceptanceSHA256,
	}).Scan(&result.State, &result.TerminalAttemptVersion, &result.TerminalFence, &result.TerminalArtifactVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return repositoryport.AdmissionTerminalProof{}, errs.ErrForbidden
	}
	if err != nil {
		return repositoryport.AdmissionTerminalProof{}, errs.ErrUnavailable
	}
	if err := committed(tx, ctx); err != nil {
		return repositoryport.AdmissionTerminalProof{}, err
	}
	return result, nil
}
