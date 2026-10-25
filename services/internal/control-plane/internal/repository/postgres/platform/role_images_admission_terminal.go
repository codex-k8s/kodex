package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repositoryport "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryRoleImagesGetAdmissionTerminal selects a finished terminal attempt bound to receipt tenant,
// artifact/build/recipe/evidence and risk/source pins, requiring a newer terminal artifact version
// and cleared admission claim metadata.
// queryRoleImagesGetAdmissionTerminal выбирает завершённую terminal attempt, связанную с tenant,
// artifact/build/recipe/evidence и risk/source pins квитанции; требует более новую terminal версию
// артефакта и очищенные admission claim metadata.
//
//go:embed sql/role_images_get_admission_terminal.sql
var queryRoleImagesGetAdmissionTerminal string

// queryRoleImagesGetAdmissionClaimReceipt reads only an unexpired IMAGE_ADMISSION_CLAIM receipt
// for exact organization, actor and original claim key; maintenance-only claim receipts cannot
// establish terminal proof.
// queryRoleImagesGetAdmissionClaimReceipt читает только неистёкшую IMAGE_ADMISSION_CLAIM квитанцию
// точных organization, actor и исходного claim key; квитанции одного обслуживания не подтверждают
// terminal proof.
//
//go:embed sql/role_images_get_admission_claim_receipt.sql
var queryRoleImagesGetAdmissionClaimReceipt string

// matchesAdmissionTerminalReceipt compares the original artifact version, fence, authority and
// attempt plus immutable build/policy/specification/risk/source coordinates against the requested
// terminal input. It is a pure equality predicate and does not establish receipt authenticity or
// terminal state.
// matchesAdmissionTerminalReceipt сравнивает исходные artifact version, fence, authority и
// attempt, а также неизменяемые build/policy/specification/risk/source координаты с terminal
// input. Это чистая проверка равенства, которая сама не доказывает подлинность квитанции либо
// terminal state.
func matchesAdmissionTerminalReceipt(receipt admissionClaimReceipt, input repositoryport.AdmissionTerminalInput) bool {
	a := receipt.Artifact
	return receipt.RiskAcceptanceSHA256 == input.RiskAcceptanceSHA256 && receipt.SourceAdmissionRevision == input.SourceAdmissionRevision && receipt.SourceAdmissionReceiptSHA256 == input.SourceAdmissionReceiptSHA256 && receipt.SourceEvidenceManifestDigest == input.SourceEvidenceManifestDigest && a.Ref == input.ArtifactRef && a.Version == input.ExpectedVersion && receipt.Fence == input.ExpectedFence &&
		receipt.AuthorityGeneration == input.ExpectedAuthorityGeneration && receipt.AdmissionAttemptRef == input.ExpectedAdmissionAttemptRef && receipt.AdmissionAttempt == input.ExpectedAdmissionAttempt &&
		a.ManifestDigest == input.ManifestDigest && a.ImmutableBuildSHA256 == input.ImmutableBuildSHA256 && a.ProvenanceSHA256 == input.ProvenanceSHA256 &&
		a.PolicyRevision == input.PolicyRevision && a.PolicySHA256 == input.PolicySHA256 && a.BuildRef == input.BuildRef && a.BuildAttempt == input.ExpectedBuildAttempt && a.RecipeGeneration == input.RecipeGeneration && a.SpecSHA256 == input.SpecSHA256
}

// GetAdmissionTerminal reads terminal proof using the original claim key and all caller-supplied
// expected coordinates; the shared read path also bounds receipt authority by current credential
// revision.
// GetAdmissionTerminal читает terminal proof по исходному claim key и всем ожидаемым координатам
// вызывающей стороны; общий путь чтения дополнительно ограничивает authority квитанции текущей
// credential revision.
func (repository *Repository) GetAdmissionTerminal(ctx context.Context, input repositoryport.AdmissionTerminalInput) (repositoryport.AdmissionTerminalProof, error) {
	return repository.getAdmissionTerminal(ctx, input.Principal, input.ClaimIdempotencyKey, &input)
}

// GetAdmissionRecoveryTerminal permits only a validated image-admission-controller with the exact
// recovery-terminal permission, then derives original coordinates from the actor-bound receipt
// instead of a caller-provided expected tuple.
// GetAdmissionRecoveryTerminal допускает только проверенный image-admission-controller с точным
// recovery-terminal permission, затем получает исходные координаты из связанной с actor квитанции
// вместо ожидаемого tuple вызывающей стороны.
func (repository *Repository) GetAdmissionRecoveryTerminal(ctx context.Context, principal value.Principal, claimKey string) (repositoryport.AdmissionTerminalProof, error) {
	if principal.Validate() != nil || principal.CallerWorkload != "image-admission-controller" || principal.Permission != "platform.role-images.admission.recovery-terminal.get" {
		return repositoryport.AdmissionTerminalProof{}, errs.ErrForbidden
	}
	return repository.getAdmissionTerminal(ctx, principal, claimKey, nil)
}

// getAdmissionTerminal resolves scope and uses a read-only RepeatableRead transaction to verify
// the actor-bound claim receipt digest, decoded identity and optional expected tuple/generation.
// It queries terminal proof using saved immutable coordinates, maps missing or invalid proof to
// Forbidden and infrastructure failures to Unavailable, and commits before returning; recovery
// omits only the optional expected-input check.
// getAdmissionTerminal разрешает область и в read-only RepeatableRead транзакции проверяет digest
// связанной с actor claim квитанции, декодированную identity и необязательные ожидаемые
// tuple/generation. Читает terminal proof по сохранённым неизменяемым координатам, возвращает
// Forbidden при отсутствии либо неверном proof и Unavailable при инфраструктурном отказе, делает
// commit до ответа; recovery пропускает только необязательную проверку expected input.
func (repository *Repository) getAdmissionTerminal(ctx context.Context, principal value.Principal, claimKey string, expected *repositoryport.AdmissionTerminalInput) (repositoryport.AdmissionTerminalProof, error) {
	current, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return repositoryport.AdmissionTerminalProof{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var receipt admissionClaimReceipt
	var intent string
	var payload []byte
	err = tx.QueryRow(ctx, queryRoleImagesGetAdmissionClaimReceipt, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "claim_key": claimKey}).Scan(&intent, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return repositoryport.AdmissionTerminalProof{}, errs.ErrForbidden
	}
	if err != nil {
		return repositoryport.AdmissionTerminalProof{}, errs.ErrUnavailable
	}
	if intent != roleImageDigest(struct{ Key string }{claimKey}) || json.Unmarshal(payload, &receipt) != nil ||
		receipt.Artifact.Ref == "" || receipt.Artifact.Version == 0 || receipt.Fence == 0 || receipt.AuthorityGeneration == 0 || receipt.AdmissionAttemptRef == "" || receipt.AdmissionAttempt == 0 ||
		(expected != nil && (!matchesAdmissionTerminalReceipt(receipt, *expected) || receipt.AuthorityGeneration > principal.CredentialRevision)) {
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
