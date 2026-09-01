package platform

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) ClaimAdmission(ctx context.Context, principal value.Principal, key string) (entity.ImageAdmissionClaim, error) {
	return retryRoleImageTransaction(ctx, func() (entity.ImageAdmissionClaim, error) {
		return repository.claimAdmission(ctx, principal, key)
	})
}

func (repository *Repository) claimAdmission(ctx context.Context, principal value.Principal, key string) (entity.ImageAdmissionClaim, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.ImageAdmissionClaim{}, err
	}
	operation := "platform.role-images.admission.claim"
	intent := roleImageDigest(struct{ Key string }{key})
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.ImageAdmissionClaim{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.lockRoleImageIdempotency(ctx, tx, current, operation, key); err != nil {
		return entity.ImageAdmissionClaim{}, err
	}
	policyArguments := pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
		"policy_revision": repository.roleImages.PolicyRevision,
		"policy_sha256":   repository.roleImages.PolicySHA256,
	}
	if _, err := tx.Exec(ctx, queryRoleImagesRejectStaleAdmissionCandidates, policyArguments); err != nil {
		return entity.ImageAdmissionClaim{}, errs.ErrUnavailable
	}
	var replay admissionClaimReceipt
	if found, receiptErr := repository.loadRoleImageReceipt(ctx, tx, current, operation, key, intent, &replay); receiptErr != nil {
		return entity.ImageAdmissionClaim{}, receiptErr
	} else if found {
		if replay.Artifact.PolicyRevision != repository.roleImages.PolicyRevision ||
			replay.Artifact.PolicySHA256 != repository.roleImages.PolicySHA256 {
			if err := committed(tx, ctx); err != nil {
				return entity.ImageAdmissionClaim{}, err
			}
			return entity.ImageAdmissionClaim{}, errs.ErrNotFound
		}
		if err := committed(tx, ctx); err != nil {
			return entity.ImageAdmissionClaim{}, err
		}
		return repository.admissionClaimFromReceipt(replay), nil
	}
	var artifactID, artifactRef string
	var version, fence uint64
	err = tx.QueryRow(ctx, queryRoleImagesClaimAdmissionCandidate, policyArguments).Scan(
		&artifactID, &artifactRef, &version, &fence)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := committed(tx, ctx); err != nil {
			return entity.ImageAdmissionClaim{}, err
		}
		return entity.ImageAdmissionClaim{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.ImageAdmissionClaim{}, errs.ErrUnavailable
	}
	fence++
	expiresAt := time.Now().UTC().Add(repository.roleImages.AdmissionClaimTTL)
	token := repository.roleImageToken("image-admission", artifactRef, 0, fence,
		principal.CredentialRevision, expiresAt)
	var updatedID string
	if err := tx.QueryRow(ctx, queryRoleImagesClaimAdmission, current.organizationID,
		artifactID, version, principal.CallerWorkload,
		principal.CredentialRevision, fence, tokenDigest(token), expiresAt).Scan(&updatedID); err != nil {
		return entity.ImageAdmissionClaim{}, mapRoleImageWriteError(err)
	}
	artifact, err := scanRoleImageArtifact(tx.QueryRow(ctx, queryRoleImagesGetActiveArtifact,
		current.organizationID, artifactRef))
	if err != nil {
		return entity.ImageAdmissionClaim{}, errs.ErrUnavailable
	}
	receipt := admissionClaimReceipt{Artifact: artifact, Fence: fence,
		AuthorityGeneration: principal.CredentialRevision, ClaimExpiresAt: expiresAt}
	if err := repository.storeRoleImageReceipt(ctx, tx, current, operation, key, intent,
		"IMAGE_ADMISSION_CLAIM", receipt); err != nil {
		return entity.ImageAdmissionClaim{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.ImageAdmissionClaim{}, err
	}
	return repository.admissionClaimFromReceipt(receipt), nil
}

func (repository *Repository) admissionClaimFromReceipt(receipt admissionClaimReceipt) entity.ImageAdmissionClaim {
	token := repository.roleImageToken("image-admission", receipt.Artifact.Ref, 0,
		receipt.Fence, receipt.AuthorityGeneration, receipt.ClaimExpiresAt)
	return entity.ImageAdmissionClaim{Artifact: receipt.Artifact, ClaimToken: token,
		Fence: receipt.Fence, AuthorityGeneration: receipt.AuthorityGeneration,
		ClaimExpiresAt: receipt.ClaimExpiresAt}
}

func (repository *Repository) RecordAdmission(ctx context.Context, input roleimagerepo.AdmissionRecordInput) (entity.ImageArtifact, error) {
	current, err := repository.resolveScope(ctx, input.Principal)
	if err != nil {
		return entity.ImageArtifact{}, err
	}
	operation := "platform.role-images.admission.record"
	intent := roleImageDigest(input)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.ImageArtifact{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var replay entity.ImageArtifact
	if found, receiptErr := repository.loadRoleImageReceipt(ctx, tx, current, operation,
		input.IdempotencyKey, intent, &replay); receiptErr != nil {
		return entity.ImageArtifact{}, receiptErr
	} else if found {
		if err := committed(tx, ctx); err != nil {
			return entity.ImageArtifact{}, err
		}
		return replay, nil
	}
	locked, err := scanLockedArtifact(tx.QueryRow(ctx, queryRoleImagesLockArtifact,
		current.organizationID, input.ArtifactRef))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ImageArtifact{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.ImageArtifact{}, errs.ErrUnavailable
	}
	if locked.Artifact.Version != input.ExpectedVersion {
		return entity.ImageArtifact{}, errs.ErrVersionMismatch
	}
	if locked.AdmissionState != "CLAIMED" || locked.AdmissionFence != input.ExpectedFence ||
		locked.AdmissionAuthorityGeneration > input.Principal.CredentialRevision ||
		locked.AdmissionExpiresAt == nil || !time.Now().UTC().Before(*locked.AdmissionExpiresAt) ||
		!tokenMatches(input.ClaimToken, locked.AdmissionTokenSHA256) ||
		locked.Artifact.ManifestDigest != input.ManifestDigest ||
		locked.Artifact.ImmutableBuildSHA256 != input.ImmutableBuildSHA256 ||
		locked.Artifact.ProvenanceSHA256 != input.ProvenanceSHA256 ||
		locked.Artifact.PolicyRevision != input.PolicyRevision ||
		locked.Artifact.PolicySHA256 != input.PolicySHA256 ||
		input.PolicyRevision != repository.roleImages.PolicyRevision ||
		input.PolicySHA256 != repository.roleImages.PolicySHA256 {
		return entity.ImageArtifact{}, errs.ErrForbidden
	}
	if err := tx.QueryRow(ctx, queryRoleImagesRecordAdmission, current.organizationID,
		locked.ID, locked.Artifact.Version, input.Verdict, input.SBOMSHA256,
		input.VulnerabilityEvidenceSHA256, input.SignatureIdentity, input.SignatureSHA256,
		input.AdmissionReceiptSHA256, input.AdmissionReceiptOCIManifestDigest).Scan(
		&locked.Artifact.Version, &locked.Artifact.AdmissionVerdict,
		&locked.Artifact.AdmissionRevision, &locked.Artifact.UpdatedAt); err != nil {
		return entity.ImageArtifact{}, mapRoleImageWriteError(err)
	}
	locked.Artifact.SBOMSHA256 = input.SBOMSHA256
	locked.Artifact.VulnerabilityEvidenceSHA256 = input.VulnerabilityEvidenceSHA256
	locked.Artifact.SignatureIdentity, locked.Artifact.SignatureSHA256 = input.SignatureIdentity, input.SignatureSHA256
	locked.Artifact.AdmissionReceiptSHA256 = input.AdmissionReceiptSHA256
	locked.Artifact.AdmissionReceiptOCIManifestDigest = input.AdmissionReceiptOCIManifestDigest
	if err := repository.storeRoleImageReceipt(ctx, tx, current, operation,
		input.IdempotencyKey, intent, "IMAGE_ADMISSION_RECORD", locked.Artifact); err != nil {
		return entity.ImageArtifact{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.ImageArtifact{}, err
	}
	return locked.Artifact, nil
}

func (repository *Repository) ClaimPromotion(ctx context.Context, principal value.Principal, key string) (entity.ImagePromotionClaim, error) {
	return retryRoleImageTransaction(ctx, func() (entity.ImagePromotionClaim, error) {
		return repository.claimPromotion(ctx, principal, key)
	})
}

func (repository *Repository) claimPromotion(ctx context.Context, principal value.Principal, key string) (entity.ImagePromotionClaim, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.ImagePromotionClaim{}, err
	}
	operation := "platform.role-images.promotion.claim"
	intent := roleImageDigest(struct{ Key string }{key})
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.ImagePromotionClaim{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.lockRoleImageIdempotency(ctx, tx, current, operation, key); err != nil {
		return entity.ImagePromotionClaim{}, err
	}
	var replay promotionClaimReceipt
	if found, receiptErr := repository.loadRoleImageReceipt(ctx, tx, current, operation, key, intent, &replay); receiptErr != nil {
		return entity.ImagePromotionClaim{}, receiptErr
	} else if found {
		if err := committed(tx, ctx); err != nil {
			return entity.ImagePromotionClaim{}, err
		}
		return repository.promotionClaimFromReceipt(replay), nil
	}
	var artifactID, artifactRef string
	var version, fence uint64
	err = tx.QueryRow(ctx, queryRoleImagesClaimPromotionCandidate, current.organizationID).Scan(
		&artifactID, &artifactRef, &version, &fence)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ImagePromotionClaim{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.ImagePromotionClaim{}, errs.ErrUnavailable
	}
	locked, err := scanLockedArtifact(tx.QueryRow(ctx, queryRoleImagesLockArtifact,
		current.organizationID, artifactRef))
	if err != nil {
		return entity.ImagePromotionClaim{}, errs.ErrUnavailable
	}
	promotionRequest, err := scanLockedPromotionRequest(tx.QueryRow(ctx,
		queryRoleImagesLockPromotionRequest, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "image_artifact_id": locked.ID,
		}))
	if err != nil {
		return entity.ImagePromotionClaim{}, errs.ErrUnavailable
	}
	lockedRecipe, err := scanLockedRecipe(tx.QueryRow(ctx, queryRoleImagesLockRecipe,
		current.organizationID, locked.Artifact.RecipeRef))
	if err != nil {
		return entity.ImagePromotionClaim{}, errs.ErrUnavailable
	}
	lockedRecipe.Recipe.Ref = locked.Artifact.RecipeRef
	now := time.Now().UTC()
	if locked.ID != artifactID || locked.Artifact.Version != version || locked.PromotionFence != fence ||
		!exactPromotionSnapshot(lockedRecipe, locked) || !promotionRequestMatches(promotionRequest, locked) ||
		!promotionCanBeClaimed(now, promotionRequest, locked) {
		return entity.ImagePromotionClaim{}, errs.ErrConflict
	}
	fence++
	expiresAt := now.Add(repository.roleImages.PromotionClaimTTL)
	claimedArtifact := locked.Artifact
	claimedArtifact.Version++
	token := repository.roleImagePromotionToken("image-promotion", claimedArtifact,
		promotionRequest.ReceiptSHA256, fence, principal.CredentialRevision, expiresAt)
	if err := tx.QueryRow(ctx, queryRoleImagesClaimPromotion, current.organizationID,
		artifactID, version, principal.CallerWorkload,
		principal.CredentialRevision, fence, tokenDigest(token), expiresAt,
		promotionRequest.ID, promotionRequest.ReceiptSHA256).Scan(
		&locked.Artifact.Version, &locked.Artifact.UpdatedAt); err != nil {
		return entity.ImagePromotionClaim{}, mapRoleImageWriteError(err)
	}
	if locked.Artifact.Version != claimedArtifact.Version {
		return entity.ImagePromotionClaim{}, errs.ErrConflict
	}
	if err := tx.QueryRow(ctx, queryRoleImagesMarkPromotionRequestPromoting, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "image_artifact_id": artifactID,
		"promotion_request_id": promotionRequest.ID, "receipt_sha256": promotionRequest.ReceiptSHA256,
		"expected_provenance_sha256": locked.Artifact.ProvenanceSHA256,
		"manifest_digest":            locked.Artifact.ManifestDigest,
	}).Scan(&promotionRequest.ID); err != nil {
		return entity.ImagePromotionClaim{}, mapRoleImageWriteError(err)
	}
	receipt := promotionClaimReceipt{Artifact: locked.Artifact,
		PromotionRequestReceiptSHA256: promotionRequest.ReceiptSHA256,
		Fence:                         fence, AuthorityGeneration: principal.CredentialRevision, ClaimExpiresAt: expiresAt}
	if err := repository.storeRoleImageReceipt(ctx, tx, current, operation, key, intent,
		"IMAGE_PROMOTION_CLAIM", receipt); err != nil {
		return entity.ImagePromotionClaim{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.ImagePromotionClaim{}, err
	}
	return repository.promotionClaimFromReceipt(receipt), nil
}

func (repository *Repository) promotionClaimFromReceipt(receipt promotionClaimReceipt) entity.ImagePromotionClaim {
	token := repository.roleImagePromotionToken("image-promotion", receipt.Artifact,
		receipt.PromotionRequestReceiptSHA256, receipt.Fence, receipt.AuthorityGeneration, receipt.ClaimExpiresAt)
	return entity.ImagePromotionClaim{Artifact: receipt.Artifact, PromotionClaim: token,
		Fence: receipt.Fence, AuthorityGeneration: receipt.AuthorityGeneration,
		ClaimExpiresAt: receipt.ClaimExpiresAt}
}

func (repository *Repository) AuthorizePromotion(ctx context.Context, input roleimagerepo.PromotionAuthorizeInput) (entity.ImagePromotionAuthorization, error) {
	return retryRoleImageTransaction(ctx, func() (entity.ImagePromotionAuthorization, error) {
		return repository.authorizePromotion(ctx, input)
	})
}

func (repository *Repository) authorizePromotion(ctx context.Context, input roleimagerepo.PromotionAuthorizeInput) (entity.ImagePromotionAuthorization, error) {
	current, err := repository.resolveScope(ctx, input.Principal)
	if err != nil {
		return entity.ImagePromotionAuthorization{}, err
	}
	operation := "platform.role-images.promotion.authorize"
	intent := roleImageDigest(input)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.ImagePromotionAuthorization{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.lockRoleImageIdempotency(ctx, tx, current, operation, input.IdempotencyKey); err != nil {
		return entity.ImagePromotionAuthorization{}, err
	}
	var replay promotionAuthorizationReceipt
	if found, receiptErr := repository.loadRoleImageReceipt(ctx, tx, current, operation,
		input.IdempotencyKey, intent, &replay); receiptErr != nil {
		return entity.ImagePromotionAuthorization{}, receiptErr
	} else if found {
		if err := committed(tx, ctx); err != nil {
			return entity.ImagePromotionAuthorization{}, err
		}
		return repository.promotionAuthorizationFromReceipt(replay), nil
	}
	locked, err := scanLockedArtifact(tx.QueryRow(ctx, queryRoleImagesLockArtifact,
		current.organizationID, input.ArtifactRef))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ImagePromotionAuthorization{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.ImagePromotionAuthorization{}, errs.ErrUnavailable
	}
	promotionRequest, err := scanLockedPromotionRequest(tx.QueryRow(ctx,
		queryRoleImagesLockPromotionRequest, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "image_artifact_id": locked.ID,
		}))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ImagePromotionAuthorization{}, errs.ErrForbidden
	}
	if err != nil {
		return entity.ImagePromotionAuthorization{}, errs.ErrUnavailable
	}
	lockedRecipe, err := scanLockedRecipe(tx.QueryRow(ctx, queryRoleImagesLockRecipe,
		current.organizationID, locked.Artifact.RecipeRef))
	if err != nil {
		return entity.ImagePromotionAuthorization{}, errs.ErrUnavailable
	}
	lockedRecipe.Recipe.Ref = locked.Artifact.RecipeRef
	if locked.Artifact.Version != input.ExpectedVersion {
		return entity.ImagePromotionAuthorization{}, errs.ErrVersionMismatch
	}
	expectedClaim := ""
	if locked.PromotionExpiresAt != nil {
		expectedClaim = repository.roleImagePromotionToken("image-promotion", locked.Artifact,
			promotionRequest.ReceiptSHA256, locked.PromotionFence,
			locked.PromotionAuthorityGeneration, *locked.PromotionExpiresAt)
	}
	if locked.PromotionState != "CLAIMED" || locked.PromotionExpiresAt == nil ||
		!time.Now().UTC().Before(*locked.PromotionExpiresAt) ||
		locked.PromotionAuthorityGeneration != input.Principal.CredentialRevision ||
		!tokenMatches(input.PromotionClaim, locked.PromotionTokenSHA256) ||
		!tokenMatches(input.PromotionClaim, tokenDigest(expectedClaim)) ||
		locked.Artifact.ManifestDigest != input.ManifestDigest ||
		!exactPromotionSnapshot(lockedRecipe, locked) || !promotionRequestMatches(promotionRequest, locked) ||
		promotionRequest.State != "PROMOTING" ||
		expectedClaim == "" {
		return entity.ImagePromotionAuthorization{}, errs.ErrForbidden
	}
	expiresAt := time.Now().UTC().Add(repository.roleImages.PromotionClaimTTL)
	authorizedArtifact := locked.Artifact
	authorizedArtifact.Version++
	token := repository.roleImagePromotionToken("image-promotion-authorization", authorizedArtifact,
		promotionRequest.ReceiptSHA256, locked.PromotionFence, input.Principal.CredentialRevision, expiresAt)
	if err := tx.QueryRow(ctx, queryRoleImagesAuthorizePromotion, current.organizationID,
		locked.ID, locked.Artifact.Version, tokenDigest(token), expiresAt,
		input.Principal.CredentialRevision, locked.PromotionFence, promotionRequest.ID,
		locked.PromotionTokenSHA256, locked.Artifact.ManifestDigest,
		locked.Artifact.ProvenanceSHA256, promotionRequest.ReceiptSHA256).Scan(
		&locked.Artifact.Version, &locked.Artifact.UpdatedAt); err != nil {
		return entity.ImagePromotionAuthorization{}, mapRoleImageWriteError(err)
	}
	if locked.Artifact.Version != authorizedArtifact.Version {
		return entity.ImagePromotionAuthorization{}, errs.ErrConflict
	}
	receipt := promotionAuthorizationReceipt{Artifact: locked.Artifact,
		PromotionRequestReceiptSHA256: promotionRequest.ReceiptSHA256,
		Fence:                         locked.PromotionFence, AuthorityGeneration: input.Principal.CredentialRevision,
		AuthorizationExpiresAt: expiresAt}
	if err := repository.storeRoleImageReceipt(ctx, tx, current, operation,
		input.IdempotencyKey, intent, "IMAGE_PROMOTION_AUTHORIZATION", receipt); err != nil {
		return entity.ImagePromotionAuthorization{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.ImagePromotionAuthorization{}, err
	}
	return repository.promotionAuthorizationFromReceipt(receipt), nil
}

func (repository *Repository) promotionAuthorizationFromReceipt(receipt promotionAuthorizationReceipt) entity.ImagePromotionAuthorization {
	token := repository.roleImagePromotionToken("image-promotion-authorization", receipt.Artifact,
		receipt.PromotionRequestReceiptSHA256, receipt.Fence, receipt.AuthorityGeneration,
		receipt.AuthorizationExpiresAt)
	return entity.ImagePromotionAuthorization{Artifact: receipt.Artifact,
		AuthorizationToken: token, AuthorizationExpiresAt: receipt.AuthorizationExpiresAt}
}

func (repository *Repository) CompletePromotion(ctx context.Context, input roleimagerepo.PromotionCompleteInput) (entity.ImageArtifact, error) {
	return retryRoleImageTransaction(ctx, func() (entity.ImageArtifact, error) {
		return repository.completePromotion(ctx, input)
	})
}

func (repository *Repository) completePromotion(ctx context.Context, input roleimagerepo.PromotionCompleteInput) (entity.ImageArtifact, error) {
	current, err := repository.resolveScope(ctx, input.Principal)
	if err != nil {
		return entity.ImageArtifact{}, err
	}
	operation := "platform.role-images.promotion.complete"
	intent := roleImageDigest(input)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.ImageArtifact{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.lockRoleImageIdempotency(ctx, tx, current, operation, input.IdempotencyKey); err != nil {
		return entity.ImageArtifact{}, err
	}
	var replay entity.ImageArtifact
	if found, receiptErr := repository.loadRoleImageReceipt(ctx, tx, current, operation,
		input.IdempotencyKey, intent, &replay); receiptErr != nil {
		return entity.ImageArtifact{}, receiptErr
	} else if found {
		if err := committed(tx, ctx); err != nil {
			return entity.ImageArtifact{}, err
		}
		return replay, nil
	}
	locked, err := scanLockedArtifact(tx.QueryRow(ctx, queryRoleImagesLockArtifact,
		current.organizationID, input.ArtifactRef))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ImageArtifact{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.ImageArtifact{}, errs.ErrUnavailable
	}
	promotionRequest, err := scanLockedPromotionRequest(tx.QueryRow(ctx,
		queryRoleImagesLockPromotionRequest, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "image_artifact_id": locked.ID,
		}))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ImageArtifact{}, errs.ErrForbidden
	}
	if err != nil {
		return entity.ImageArtifact{}, errs.ErrUnavailable
	}
	lockedRecipe, err := scanLockedRecipe(tx.QueryRow(ctx, queryRoleImagesLockRecipe,
		current.organizationID, locked.Artifact.RecipeRef))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ImageArtifact{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.ImageArtifact{}, errs.ErrUnavailable
	}
	lockedRecipe.Recipe.Ref = locked.Artifact.RecipeRef
	if locked.Artifact.Version != input.ExpectedVersion {
		return entity.ImageArtifact{}, errs.ErrVersionMismatch
	}
	expectedAuthorization := ""
	if locked.AuthorizationExpiresAt != nil {
		expectedAuthorization = repository.roleImagePromotionToken("image-promotion-authorization",
			locked.Artifact, promotionRequest.ReceiptSHA256, locked.PromotionFence,
			locked.PromotionAuthorityGeneration, *locked.AuthorizationExpiresAt)
	}
	if locked.PromotionState != "AUTHORIZED" || locked.AuthorizationExpiresAt == nil ||
		!time.Now().UTC().Before(*locked.AuthorizationExpiresAt) ||
		locked.PromotionAuthorityGeneration != input.Principal.CredentialRevision ||
		!tokenMatches(input.AuthorizationToken, locked.AuthorizationTokenSHA256) ||
		!tokenMatches(input.AuthorizationToken, tokenDigest(expectedAuthorization)) ||
		locked.Artifact.ManifestDigest != input.ManifestDigest ||
		input.PromotedReference != repository.roleImages.PromotedRepository+"@"+input.ManifestDigest ||
		promotionRequest.State != "PROMOTING" || !promotionRequestMatches(promotionRequest, locked) ||
		!exactPromotionSnapshot(lockedRecipe, locked) || expectedAuthorization == "" {
		return entity.ImageArtifact{}, errs.ErrForbidden
	}
	if err := tx.QueryRow(ctx, queryRoleImagesCompletePromotion, current.organizationID,
		locked.ID, locked.Artifact.Version, input.PromotedReference,
		input.PromotionReadbackSHA256, locked.PromotionAuthorityGeneration,
		locked.PromotionFence, promotionRequest.ID, locked.AuthorizationTokenSHA256,
		locked.Artifact.ManifestDigest, locked.Artifact.ProvenanceSHA256,
		locked.Artifact.AdmissionReceiptSHA256, locked.Artifact.AdmissionReceiptOCIManifestDigest,
		promotionRequest.ReceiptSHA256).Scan(&locked.Artifact.Version,
		&locked.Artifact.PromotedReference, &locked.Artifact.PromotionReadbackSHA256,
		&locked.Artifact.PromotedAt, &locked.Artifact.UpdatedAt); err != nil {
		return entity.ImageArtifact{}, mapRoleImageWriteError(err)
	}
	var activatedRecipeVersion uint64
	var activatedAt time.Time
	if err := tx.QueryRow(ctx, queryRoleImagesActivateArtifact, current.organizationID,
		locked.RecipeID, locked.ID, locked.Artifact.RecipeVersion).Scan(
		&activatedRecipeVersion, &activatedAt); err != nil {
		return entity.ImageArtifact{}, mapRoleImageWriteError(err)
	}
	if activatedRecipeVersion != locked.Artifact.RecipeVersion+1 || activatedAt.IsZero() ||
		!promotionRequestMatches(promotionRequest, locked) {
		return entity.ImageArtifact{}, errs.ErrConflict
	}
	var promotionState string
	if err := tx.QueryRow(ctx, queryRoleImagesMarkPromotionRequestPromoted, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "promotion_request_id": promotionRequest.ID,
		"image_artifact_id":          locked.ID,
		"expected_provenance_sha256": promotionRequest.ExpectedProvenanceSHA256,
		"manifest_digest":            promotionRequest.ManifestDigest,
		"receipt_sha256":             promotionRequest.ReceiptSHA256,
	}).Scan(&promotionState); err != nil {
		return entity.ImageArtifact{}, mapRoleImageWriteError(err)
	}
	if promotionState != "PROMOTED" {
		return entity.ImageArtifact{}, errs.ErrConflict
	}
	revisionRef, err := newRef("imgrev")
	if err != nil {
		return entity.ImageArtifact{}, errs.ErrUnavailable
	}
	var revision, revisionRecipeVersion, revisionRecipeGeneration uint64
	var revisionCreatedAt time.Time
	if err := tx.QueryRow(ctx, queryRoleImagesInsertPromotedRevision, pgx.StrictNamedArgs{
		"revision_ref": revisionRef, "organization_id": current.organizationID,
		"promotion_request_id": promotionRequest.ID, "image_artifact_id": locked.ID,
		"source_sha256":             locked.Artifact.SourceSHA256,
		"promotion_readback_sha256": locked.Artifact.PromotionReadbackSHA256,
	}).Scan(&revisionRef, &revision, &revisionRecipeVersion, &revisionRecipeGeneration,
		&revisionCreatedAt); err != nil {
		return entity.ImageArtifact{}, mapRoleImageWriteError(err)
	}
	if revisionRef == "" || revision == 0 || revisionRecipeVersion != locked.Artifact.RecipeVersion ||
		revisionRecipeGeneration != locked.Artifact.RecipeGeneration || revisionCreatedAt.IsZero() {
		return entity.ImageArtifact{}, errs.ErrConflict
	}
	if err := repository.auditRoleImage(ctx, tx, current, lockedRecipe.ProjectID, operation,
		"ROLE_IMAGE", locked.Artifact.RecipeRef, "i18n:ROLE_IMAGE_PROMOTED"); err != nil {
		return entity.ImageArtifact{}, err
	}
	if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "ROLE_IMAGE_PROMOTED",
		lockedRecipe.Recipe.ProjectRef, locked.Artifact.RecipeRef, "i18n:ROLE_IMAGE_PROMOTED",
		int64(activatedRecipeVersion), "PROMOTED"); err != nil {
		return entity.ImageArtifact{}, err
	}
	if err := repository.storeRoleImageReceipt(ctx, tx, current, operation,
		input.IdempotencyKey, intent, "IMAGE_PROMOTION_COMPLETION", locked.Artifact); err != nil {
		return entity.ImageArtifact{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.ImageArtifact{}, err
	}
	return locked.Artifact, nil
}
