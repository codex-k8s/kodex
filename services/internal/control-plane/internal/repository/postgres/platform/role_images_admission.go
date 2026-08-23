package platform

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) ClaimAdmission(ctx context.Context, principal value.Principal, key string) (entity.ImageAdmissionClaim, error) {
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
	var replay admissionClaimReceipt
	if found, receiptErr := repository.loadRoleImageReceipt(ctx, tx, current, operation, key, intent, &replay); receiptErr != nil {
		return entity.ImageAdmissionClaim{}, receiptErr
	} else if found {
		if err := committed(tx, ctx); err != nil {
			return entity.ImageAdmissionClaim{}, err
		}
		return repository.admissionClaimFromReceipt(replay), nil
	}
	var artifactID, artifactRef string
	var version, fence uint64
	err = tx.QueryRow(ctx, queryRoleImagesClaimAdmissionCandidate, current.organizationID).Scan(
		&artifactID, &artifactRef, &version, &fence)
	if errors.Is(err, pgx.ErrNoRows) {
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
	fence++
	expiresAt := time.Now().UTC().Add(repository.roleImages.PromotionClaimTTL)
	token := repository.roleImageToken("image-promotion", artifactRef, 0, fence,
		principal.CredentialRevision, expiresAt)
	var updatedID string
	if err := tx.QueryRow(ctx, queryRoleImagesClaimPromotion, current.organizationID,
		artifactID, version, principal.CallerWorkload,
		principal.CredentialRevision, fence, tokenDigest(token), expiresAt).Scan(&updatedID); err != nil {
		return entity.ImagePromotionClaim{}, mapRoleImageWriteError(err)
	}
	artifact, err := scanRoleImageArtifact(tx.QueryRow(ctx, queryRoleImagesGetActiveArtifact,
		current.organizationID, artifactRef))
	if err != nil {
		return entity.ImagePromotionClaim{}, errs.ErrUnavailable
	}
	receipt := promotionClaimReceipt{Artifact: artifact, Fence: fence,
		AuthorityGeneration: principal.CredentialRevision, ClaimExpiresAt: expiresAt}
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
	token := repository.roleImageToken("image-promotion", receipt.Artifact.Ref, 0,
		receipt.Fence, receipt.AuthorityGeneration, receipt.ClaimExpiresAt)
	return entity.ImagePromotionClaim{Artifact: receipt.Artifact, PromotionClaim: token,
		Fence: receipt.Fence, AuthorityGeneration: receipt.AuthorityGeneration,
		ClaimExpiresAt: receipt.ClaimExpiresAt}
}

func (repository *Repository) AuthorizePromotion(ctx context.Context, input roleimagerepo.PromotionAuthorizeInput) (entity.ImagePromotionAuthorization, error) {
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
	if locked.Artifact.Version != input.ExpectedVersion {
		return entity.ImagePromotionAuthorization{}, errs.ErrVersionMismatch
	}
	if locked.PromotionState != "CLAIMED" || locked.PromotionExpiresAt == nil ||
		!time.Now().UTC().Before(*locked.PromotionExpiresAt) ||
		locked.PromotionAuthorityGeneration > input.Principal.CredentialRevision ||
		!tokenMatches(input.PromotionClaim, locked.PromotionTokenSHA256) ||
		locked.Artifact.ManifestDigest != input.ManifestDigest ||
		locked.Artifact.AdmissionVerdict != "ACCEPTED" {
		return entity.ImagePromotionAuthorization{}, errs.ErrForbidden
	}
	expiresAt := time.Now().UTC().Add(repository.roleImages.PromotionClaimTTL)
	token := repository.roleImageToken("image-promotion-authorization", locked.Artifact.Ref, 0,
		locked.PromotionFence, input.Principal.CredentialRevision, expiresAt)
	if err := tx.QueryRow(ctx, queryRoleImagesAuthorizePromotion, current.organizationID,
		locked.ID, locked.Artifact.Version, tokenDigest(token), expiresAt).Scan(
		&locked.Artifact.Version, &locked.Artifact.UpdatedAt); err != nil {
		return entity.ImagePromotionAuthorization{}, mapRoleImageWriteError(err)
	}
	receipt := promotionAuthorizationReceipt{Artifact: locked.Artifact,
		Fence: locked.PromotionFence, AuthorityGeneration: input.Principal.CredentialRevision,
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
	token := repository.roleImageToken("image-promotion-authorization", receipt.Artifact.Ref, 0,
		receipt.Fence, receipt.AuthorityGeneration, receipt.AuthorizationExpiresAt)
	return entity.ImagePromotionAuthorization{Artifact: receipt.Artifact,
		AuthorizationToken: token, AuthorizationExpiresAt: receipt.AuthorizationExpiresAt}
}

func (repository *Repository) CompletePromotion(ctx context.Context, input roleimagerepo.PromotionCompleteInput) (entity.ImageArtifact, error) {
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
	if locked.PromotionState != "AUTHORIZED" || locked.AuthorizationExpiresAt == nil ||
		!time.Now().UTC().Before(*locked.AuthorizationExpiresAt) ||
		!tokenMatches(input.AuthorizationToken, locked.AuthorizationTokenSHA256) ||
		locked.Artifact.ManifestDigest != input.ManifestDigest ||
		input.PromotedReference != repository.roleImages.PromotedRepository+"@"+input.ManifestDigest {
		return entity.ImageArtifact{}, errs.ErrForbidden
	}
	if err := tx.QueryRow(ctx, queryRoleImagesCompletePromotion, current.organizationID,
		locked.ID, locked.Artifact.Version, input.PromotedReference,
		input.PromotionReadbackSHA256).Scan(&locked.Artifact.Version,
		&locked.Artifact.PromotedReference, &locked.Artifact.PromotionReadbackSHA256,
		&locked.Artifact.PromotedAt, &locked.Artifact.UpdatedAt); err != nil {
		return entity.ImageArtifact{}, mapRoleImageWriteError(err)
	}
	if _, err := tx.Exec(ctx, queryRoleImagesActivateArtifact, current.organizationID,
		locked.RecipeID, locked.ID); err != nil {
		return entity.ImageArtifact{}, errs.ErrConflict
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
