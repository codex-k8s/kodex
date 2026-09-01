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

func (repository *Repository) RequestPromotion(ctx context.Context, input roleimagerepo.PromotionRequestInput) (entity.RoleImagePromotionReceipt, error) {
	return retryRoleImageTransaction(ctx, func() (entity.RoleImagePromotionReceipt, error) {
		return repository.requestPromotion(ctx, input)
	})
}

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

	target, err := repository.resolveRoleImageAccessTarget(ctx, tx, current, input.RecipeRef, "")
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

	lockedArtifact, err := scanLockedArtifact(tx.QueryRow(ctx, queryRoleImagesLockArtifact,
		current.organizationID, input.ArtifactRef))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.RoleImagePromotionReceipt{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.RoleImagePromotionReceipt{}, errs.ErrUnavailable
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

func admittedArtifactCanBePromoted(recipe lockedRecipe, artifact lockedArtifact, expectedProvenance string) bool {
	return recipe.Recipe.State == "ACTIVE" && artifact.RecipeID == recipe.ID &&
		artifact.Artifact.RecipeRef == recipe.Recipe.Ref && artifact.PromotionRequestID == "" &&
		artifact.AdmissionState == "ACCEPTED" && artifact.Artifact.AdmissionVerdict == "ACCEPTED" &&
		artifact.PromotionState == "PENDING" && artifact.Artifact.PromotedReference == "" &&
		artifact.Artifact.RecipeVersion == recipe.Recipe.Version &&
		artifact.Artifact.RecipeGeneration == recipe.Recipe.Generation &&
		artifact.Artifact.SpecSHA256 == recipe.Recipe.SpecSHA256 &&
		artifact.Artifact.PolicyRevision == recipe.Recipe.PolicyRevision &&
		artifact.Artifact.PolicySHA256 == recipe.Recipe.PolicySHA256 &&
		artifact.Artifact.RoleRuntimeContractRevision == recipe.Recipe.RoleRuntimeContractRevision &&
		artifact.Artifact.RoleRuntimeContractSHA256 == recipe.Recipe.RoleRuntimeContractSHA256 &&
		artifact.Artifact.ProvenanceSHA256 == expectedProvenance && exactPromotionEvidence(artifact.Artifact)
}

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

func promotionRequestMatches(request lockedPromotionRequest, artifact lockedArtifact) bool {
	return request.ID != "" && request.ID == artifact.PromotionRequestID && request.RequestedBy != "" &&
		request.ExpectedProvenanceSHA256 == artifact.Artifact.ProvenanceSHA256 &&
		request.ManifestDigest == artifact.Artifact.ManifestDigest && exactSHA256(request.ReceiptSHA256) &&
		request.ReceiptSHA256 == promotionRequestDigest(request.Ref, request.RequestedBy, artifact)
}

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

func promotionRequestDigest(ref, requestedBy string, artifact lockedArtifact) string {
	return roleImageDigest(struct {
		Ref, RequestedBy, RecipeRef, ArtifactRef, BuildRef, StagingReference   string
		SpecSHA256, ManifestDigest, ProvenanceSHA256, ImmutableBuildSHA256     string
		SourceSHA256, ContextSHA256, BuilderSHA256, FrontendSHA256             string
		ToolchainSHA256, PolicySHA256, SBOMSHA256, VulnerabilityEvidenceSHA256 string
		SignatureIdentity, SignatureSHA256, AdmissionReceiptSHA256             string
		AdmissionReceiptOCIManifestDigest, RoleRuntimeContractSHA256           string
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
		Tools:                             artifact.Artifact.Tools,
	})
}
