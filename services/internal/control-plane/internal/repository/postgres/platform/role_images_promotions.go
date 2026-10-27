package platform

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// RequestPromotion forces PROJECT scope and delegates the full promotion request to the existing
// transaction retry policy.
// RequestPromotion принудительно задаёт PROJECT scope и передаёт полный promotion request прежней
// transaction retry policy.
func (repository *Repository) RequestPromotion(ctx context.Context, input roleimagerepo.PromotionRequestInput) (entity.RoleImagePromotionReceipt, error) {
	input.ScopeKind = "PROJECT"
	return retryRoleImageTransaction(ctx, func() (entity.RoleImagePromotionReceipt, error) {
		return repository.requestPromotion(ctx, input)
	})
}

// requestPromotion requires current scoped image.promote before idempotent replay, locks exact
// artifact/recipe and OCC, verifies admitted immutable evidence and records request linkage,
// audit, event and receipt in one serializable transaction.
// requestPromotion требует текущий scoped image.promote до idempotent replay, блокирует точные
// artifact/recipe и OCC, проверяет admitted immutable evidence и сохраняет request linkage, audit,
// event и receipt одной serializable транзакцией.
func (repository *Repository) requestPromotion(ctx context.Context, input roleimagerepo.PromotionRequestInput) (entity.RoleImagePromotionReceipt, error) {
	current, err := repository.resolveScope(ctx, input.Principal)
	if err != nil {
		return entity.RoleImagePromotionReceipt{}, err
	}
	if input.Mutation.ExpectedVersion == nil {
		return entity.RoleImagePromotionReceipt{}, errs.ErrInvalid
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.RoleImagePromotionReceipt{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if input.ScopeKind == "ORGANIZATION" {
		if err := repository.requireOrganizationRoleImageAccess(ctx, tx, current); err != nil {
			return entity.RoleImagePromotionReceipt{}, err
		}
	}

	target, err := repository.resolveScopedRoleImageAccessTarget(ctx, tx, current, input.RecipeRef, "", input.ScopeKind)
	if err != nil {
		return entity.RoleImagePromotionReceipt{}, err
	}
	authorization, err := repository.loadRoleImageAccessContext(ctx, tx, current)
	if err != nil {
		return entity.RoleImagePromotionReceipt{}, err
	}
	if !authorization.allowed("image.promote", target) {
		return entity.RoleImagePromotionReceipt{}, errs.ErrNotFound
	}
	if err := repository.lockRoleImageIdempotency(ctx, tx, current,
		input.Mutation.Operation, input.Mutation.IdempotencyKey); err != nil {
		return entity.RoleImagePromotionReceipt{}, err
	}
	var replay entity.RoleImagePromotionReceipt
	if found, receiptErr := repository.loadRoleImageReceipt(ctx, tx, current,
		input.Mutation.Operation, input.Mutation.IdempotencyKey, input.Mutation.IntentDigest, &replay); receiptErr != nil {
		return entity.RoleImagePromotionReceipt{}, receiptErr
	} else if found {
		if err := committed(tx, ctx); err != nil {
			return entity.RoleImagePromotionReceipt{}, err
		}
		return replay, nil
	}

	lockedArtifact, err := lockRoleImageArtifact(ctx, tx, current, input.ArtifactRef)
	if err != nil {
		return entity.RoleImagePromotionReceipt{}, err
	}
	lockedRecipe, err := scanLockedRecipe(tx.QueryRow(ctx, queryRoleImagesLockRecipe,
		current.organizationID, input.RecipeRef))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.RoleImagePromotionReceipt{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.RoleImagePromotionReceipt{}, errs.ErrUnavailable
	}
	lockedRecipe.Recipe.Ref = input.RecipeRef
	if lockedRecipe.Recipe.Version != uint64(*input.Mutation.ExpectedVersion) {
		return entity.RoleImagePromotionReceipt{}, errs.ErrVersionMismatch
	}
	if !admittedArtifactCanBePromoted(lockedRecipe, lockedArtifact, input.ExpectedProvenanceSHA256) {
		return entity.RoleImagePromotionReceipt{}, errs.ErrConflict
	}

	receiptRef, err := newRef("imgprom")
	if err != nil {
		return entity.RoleImagePromotionReceipt{}, errs.ErrUnavailable
	}
	receiptSHA256 := promotionRequestDigest(receiptRef, current.actorID, lockedArtifact)
	var requestID, state string
	var createdAt time.Time
	if err := tx.QueryRow(ctx, queryRoleImagesInsertPromotionRequest, pgx.StrictNamedArgs{
		"ref": receiptRef, "organization_id": current.organizationID,
		"project_id": lockedRecipe.ProjectID, "recipe_id": lockedRecipe.ID,
		"image_artifact_id":          lockedArtifact.ID,
		"expected_provenance_sha256": input.ExpectedProvenanceSHA256,
		"manifest_digest":            lockedArtifact.Artifact.ManifestDigest,
		"receipt_sha256":             receiptSHA256, "requested_by": current.actorID,
	}).Scan(&requestID, &state, &createdAt); err != nil {
		return entity.RoleImagePromotionReceipt{}, mapRoleImageWriteError(err)
	}
	if err := tx.QueryRow(ctx, queryRoleImagesLinkPromotionRequest, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "promotion_request_id": requestID,
		"image_artifact_id": lockedArtifact.ID, "recipe_id": lockedRecipe.ID,
		"expected_version":                      lockedArtifact.Artifact.Version,
		"recipe_version":                        lockedArtifact.Artifact.RecipeVersion,
		"recipe_generation":                     lockedArtifact.Artifact.RecipeGeneration,
		"spec_sha256":                           lockedArtifact.Artifact.SpecSHA256,
		"expected_provenance_sha256":            input.ExpectedProvenanceSHA256,
		"manifest_digest":                       lockedArtifact.Artifact.ManifestDigest,
		"immutable_build_sha256":                lockedArtifact.Artifact.ImmutableBuildSHA256,
		"sbom_sha256":                           lockedArtifact.Artifact.SBOMSHA256,
		"vulnerability_evidence_sha256":         lockedArtifact.Artifact.VulnerabilityEvidenceSHA256,
		"signature_identity":                    lockedArtifact.Artifact.SignatureIdentity,
		"signature_sha256":                      lockedArtifact.Artifact.SignatureSHA256,
		"admission_revision":                    lockedArtifact.Artifact.AdmissionRevision,
		"admission_receipt_sha256":              lockedArtifact.Artifact.AdmissionReceiptSHA256,
		"admission_receipt_oci_manifest_digest": lockedArtifact.Artifact.AdmissionReceiptOCIManifestDigest,
		"policy_revision":                       lockedArtifact.Artifact.PolicyRevision,
		"policy_sha256":                         lockedArtifact.Artifact.PolicySHA256,
		"role_runtime_contract_revision":        lockedArtifact.Artifact.RoleRuntimeContractRevision,
		"role_runtime_contract_sha256":          lockedArtifact.Artifact.RoleRuntimeContractSHA256,
	}).Scan(&lockedArtifact.Artifact.Version, &lockedArtifact.Artifact.UpdatedAt); err != nil {
		return entity.RoleImagePromotionReceipt{}, mapRoleImageWriteError(err)
	}
	receipt := entity.RoleImagePromotionReceipt{
		Ref: receiptRef, RecipeRef: input.RecipeRef, ImageArtifactRef: input.ArtifactRef,
		ProvenanceSHA256: input.ExpectedProvenanceSHA256,
		ManifestDigest:   lockedArtifact.Artifact.ManifestDigest,
		ReceiptSHA256:    receiptSHA256, State: state, CreatedAt: createdAt,
	}
	if err := repository.auditRoleImage(ctx, tx, current, lockedRecipe.ProjectID,
		input.Mutation.Operation, "ROLE_IMAGE", input.RecipeRef,
		"i18n:ROLE_IMAGE_PROMOTION_REQUESTED"); err != nil {
		return entity.RoleImagePromotionReceipt{}, err
	}
	if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "ROLE_IMAGE_PROMOTION_REQUESTED",
		lockedRecipe.Recipe.ProjectRef, input.RecipeRef, "i18n:ROLE_IMAGE_PROMOTION_REQUESTED",
		int64(lockedRecipe.Recipe.Version), "QUEUED"); err != nil {
		return entity.RoleImagePromotionReceipt{}, err
	}
	if err := repository.storeRoleImageReceipt(ctx, tx, current, input.Mutation.Operation,
		input.Mutation.IdempotencyKey, input.Mutation.IntentDigest,
		"ROLE_IMAGE_PROMOTION_REQUEST", receipt); err != nil {
		return entity.RoleImagePromotionReceipt{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.RoleImagePromotionReceipt{}, err
	}
	return receipt, nil
}

// admittedArtifactCanBePromoted requires an active matching admitted recipe snapshot and exact
// evidence, no existing promotion request/reference, PENDING promotion state and the caller-pinned
// provenance digest.
// admittedArtifactCanBePromoted требует активный совпадающий admitted recipe snapshot и точное
// evidence, отсутствие существующих promotion request/reference, PENDING promotion state и
// закреплённый caller provenance digest.
func admittedArtifactCanBePromoted(recipe lockedRecipe, artifact lockedArtifact, expectedProvenance string) bool {
	return exactPromotionSnapshot(recipe, artifact) && artifact.PromotionRequestID == "" &&
		artifact.PromotionState == "PENDING" && artifact.Artifact.PromotedReference == "" &&
		artifact.Artifact.ProvenanceSHA256 == expectedProvenance
}

// exactPromotionEvidence validates required manifest, hash and signature-identity shapes plus
// positive admission/policy/runtime/build versions; shape validation is not signature verification
// or promotion authority.
// exactPromotionEvidence проверяет обязательные формы manifest, hash и signature identity и
// положительные admission/policy/runtime/build versions; проверка формы не является проверкой
// подписи либо promotion authority.
func exactPromotionEvidence(artifact entity.ImageArtifact) bool {
	return exactManifestDigest(artifact.ManifestDigest) && exactSHA256(artifact.ImmutableBuildSHA256) &&
		exactSHA256(artifact.ProvenanceSHA256) && exactSHA256(artifact.SBOMSHA256) &&
		exactSHA256(artifact.VulnerabilityEvidenceSHA256) && exactSignatureIdentity(artifact.SignatureIdentity) &&
		exactSHA256(artifact.SignatureSHA256) && artifact.AdmissionRevision > 0 &&
		exactSHA256(artifact.AdmissionReceiptSHA256) && exactManifestDigest(artifact.AdmissionReceiptOCIManifestDigest) &&
		exactSHA256(artifact.SpecSHA256) && exactSHA256(artifact.SourceSHA256) &&
		exactSHA256(artifact.PolicySHA256) && artifact.PolicyRevision > 0 &&
		exactSHA256(artifact.RoleRuntimeContractSHA256) && artifact.RoleRuntimeContractRevision > 0 &&
		artifact.BuildAttempt > 0 && artifact.BuildVersion > 0
}

// exactPromotionSnapshot compares active recipe identity, version/generation/specification,
// admission result and policy/runtime pins with the artifact and requires exact evidence,
// independently of promotion-request state.
// exactPromotionSnapshot сравнивает active recipe identity, version/generation/specification,
// admission result и policy/runtime pins с artifact и требует точное evidence независимо от
// promotion-request state.
func exactPromotionSnapshot(recipe lockedRecipe, artifact lockedArtifact) bool {
	return recipe.Recipe.State == "ACTIVE" && artifact.RecipeID == recipe.ID &&
		artifact.Artifact.RecipeRef == recipe.Recipe.Ref &&
		artifact.AdmissionState == "ACCEPTED" && artifact.Artifact.AdmissionVerdict == "ACCEPTED" &&
		artifact.Artifact.RecipeVersion == recipe.Recipe.Version &&
		artifact.Artifact.RecipeGeneration == recipe.Recipe.Generation &&
		artifact.Artifact.SpecSHA256 == recipe.Recipe.SpecSHA256 &&
		artifact.Artifact.PolicyRevision == recipe.Recipe.PolicyRevision &&
		artifact.Artifact.PolicySHA256 == recipe.Recipe.PolicySHA256 &&
		artifact.Artifact.RoleRuntimeContractRevision == recipe.Recipe.RoleRuntimeContractRevision &&
		artifact.Artifact.RoleRuntimeContractSHA256 == recipe.Recipe.RoleRuntimeContractSHA256 &&
		exactPromotionEvidence(artifact.Artifact)
}

// promotionRequestMatches requires the exact linked request ID, nonempty requester and matching
// immutable manifest/provenance/receipt commitment; it does not infer permission from request
// fields.
// promotionRequestMatches требует точный linked request ID, непустого requester и совпадение
// immutable manifest/provenance/receipt commitment; не выводит permission из полей запроса.
func promotionRequestMatches(request lockedPromotionRequest, artifact lockedArtifact) bool {
	return request.ID != "" && request.ID == artifact.PromotionRequestID && request.RequestedBy != "" &&
		request.ExpectedProvenanceSHA256 == artifact.Artifact.ProvenanceSHA256 &&
		request.ManifestDigest == artifact.Artifact.ManifestDigest && exactSHA256(request.ReceiptSHA256) &&
		request.ReceiptSHA256 == promotionRequestDigest(request.Ref, request.RequestedBy, artifact)
}

// promotionCanBeClaimed allows an unpromoted PENDING artifact for QUEUED request or an expired
// CLAIMED/AUTHORIZED lifetime for PROMOTING request, rejecting other states.
// promotionCanBeClaimed допускает unpromoted PENDING artifact для QUEUED request либо истёкший
// срок CLAIMED/AUTHORIZED для PROMOTING request, отклоняя другие состояния.
func promotionCanBeClaimed(now time.Time, request lockedPromotionRequest, artifact lockedArtifact) bool {
	if request.State == "QUEUED" {
		return artifact.PromotionState == "PENDING" && artifact.Artifact.PromotedReference == ""
	}
	if request.State != "PROMOTING" {
		return false
	}
	if artifact.PromotionState == "CLAIMED" {
		return artifact.PromotionExpiresAt != nil && !now.Before(*artifact.PromotionExpiresAt)
	}
	if artifact.PromotionState == "AUTHORIZED" {
		return artifact.AuthorizationExpiresAt != nil && !now.Before(*artifact.AuthorizationExpiresAt)
	}
	return false
}

// promotionRequestDigest hashes the fixed request/requester and immutable
// recipe/build/evidence/runtime/platform/tool tuple, excluding the mutable artifact lifecycle
// version.
// promotionRequestDigest хеширует фиксированные request/requester и неизменяемый
// recipe/build/evidence/runtime/platform/tool tuple без mutable artifact lifecycle version.
func promotionRequestDigest(ref, requestedBy string, artifact lockedArtifact) string {
	return roleImageDigest(struct {
		Ref, RequestedBy, RecipeRef, ArtifactRef, BuildRef, StagingReference   string
		SpecSHA256, ManifestDigest, ProvenanceSHA256, ImmutableBuildSHA256     string
		SourceSHA256, ContextSHA256, BuilderSHA256, FrontendSHA256             string
		ToolchainSHA256, PolicySHA256, SBOMSHA256, VulnerabilityEvidenceSHA256 string
		SignatureIdentity, SignatureSHA256, AdmissionReceiptSHA256             string
		AdmissionReceiptOCIManifestDigest, RoleRuntimeContractSHA256           string
		ToolInventorySHA256                                                    string
		RecipeVersion, RecipeGeneration, BuildVersion, PolicyRevision          uint64
		AdmissionRevision, RoleRuntimeContractRevision                         uint64
		BuildAttempt                                                           uint32
		Platforms                                                              []entity.RoleImagePlatform
		Tools                                                                  []entity.RoleImageTool
	}{
		Ref: ref, RequestedBy: requestedBy, RecipeRef: artifact.Artifact.RecipeRef,
		ArtifactRef: artifact.Artifact.Ref, BuildRef: artifact.Artifact.BuildRef,
		StagingReference: artifact.Artifact.StagingReference,
		SpecSHA256:       artifact.Artifact.SpecSHA256, ManifestDigest: artifact.Artifact.ManifestDigest,
		ProvenanceSHA256:                  artifact.Artifact.ProvenanceSHA256,
		ImmutableBuildSHA256:              artifact.Artifact.ImmutableBuildSHA256,
		SourceSHA256:                      artifact.Artifact.SourceSHA256,
		ContextSHA256:                     artifact.Artifact.ContextSHA256,
		BuilderSHA256:                     artifact.Artifact.BuilderSHA256,
		FrontendSHA256:                    artifact.Artifact.FrontendSHA256,
		ToolchainSHA256:                   artifact.Artifact.ToolchainSHA256,
		PolicySHA256:                      artifact.Artifact.PolicySHA256,
		SBOMSHA256:                        artifact.Artifact.SBOMSHA256,
		VulnerabilityEvidenceSHA256:       artifact.Artifact.VulnerabilityEvidenceSHA256,
		SignatureIdentity:                 artifact.Artifact.SignatureIdentity,
		SignatureSHA256:                   artifact.Artifact.SignatureSHA256,
		AdmissionReceiptSHA256:            artifact.Artifact.AdmissionReceiptSHA256,
		AdmissionReceiptOCIManifestDigest: artifact.Artifact.AdmissionReceiptOCIManifestDigest,
		RoleRuntimeContractSHA256:         artifact.Artifact.RoleRuntimeContractSHA256,
		RecipeVersion:                     artifact.Artifact.RecipeVersion,
		RecipeGeneration:                  artifact.Artifact.RecipeGeneration,
		BuildVersion:                      artifact.Artifact.BuildVersion,
		PolicyRevision:                    artifact.Artifact.PolicyRevision,
		AdmissionRevision:                 artifact.Artifact.AdmissionRevision,
		RoleRuntimeContractRevision:       artifact.Artifact.RoleRuntimeContractRevision,
		BuildAttempt:                      artifact.Artifact.BuildAttempt,
		Platforms:                         artifact.Artifact.Platforms,
		Tools:                             artifact.Artifact.DeclaredTools,
		ToolInventorySHA256:               artifact.Artifact.ToolInventorySHA256,
	})
}
