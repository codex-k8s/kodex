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

// GetSupplyWorkAvailability reads tenant-scoped admission and promotion availability under the
// configured policy; maintenance work counts as available, and this query neither claims nor
// mutates artifacts.
// GetSupplyWorkAvailability читает доступность допуска и публикации в области организации при
// настроенной policy; обслуживание тоже считается работой, но запрос не выдаёт claim и не изменяет
// артефакты.
func (repository *Repository) GetSupplyWorkAvailability(ctx context.Context, principal value.Principal) (roleimagerepo.SupplyWorkAvailability, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return roleimagerepo.SupplyWorkAvailability{}, err
	}
	arguments := pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
		"policy_revision": repository.roleImages.PolicyRevision,
		"policy_sha256":   repository.roleImages.PolicySHA256,
	}
	var result roleimagerepo.SupplyWorkAvailability
	if err := repository.pool.QueryRow(ctx, queryRoleImagesGetSupplyWorkAvailability, arguments).Scan(
		&result.AdmissionAvailable, &result.PromotionAvailable); err != nil {
		return roleimagerepo.SupplyWorkAvailability{}, errs.ErrUnavailable
	}
	return result, nil
}

// ClaimAdmission runs a complete admission claim through the existing bounded transaction retry
// policy; each attempt resolves scope and owns its transaction.
// ClaimAdmission выполняет полный запрос admission claim через прежнюю ограниченную политику
// повтора транзакций; каждая попытка разрешает область и владеет своей транзакцией.
func (repository *Repository) ClaimAdmission(ctx context.Context, principal value.Principal, key string) (entity.ImageAdmissionClaim, error) {
	return retryRoleImageTransaction(ctx, func() (entity.ImageAdmissionClaim, error) {
		return repository.claimAdmission(ctx, principal, key)
	})
}

// claimAdmission serializes actor/key in a Serializable transaction, replays only a current-policy
// live admission claim, then rejects stale policy and expires leases before selecting a pending
// artifact. It stores maintenance-only or exact artifact/attempt/risk receipts and commits before
// returning the reconstructed token; maintenance may commit while returning NotFound.
// claimAdmission сериализует actor/key в Serializable транзакции, повторно выдаёт только живой
// admission claim текущей policy, затем отклоняет старую policy и завершает истёкшие leases до
// выбора ожидающего артефакта. Сохраняет квитанцию обслуживания либо точные artifact/attempt/risk
// данные и делает commit до выдачи восстановленного токена; обслуживание может завершиться commit
// с ответом NotFound.
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
	var replay admissionClaimReceipt
	if found, receiptErr := repository.loadRoleImageReceipt(ctx, tx, current, operation, key, intent, &replay); receiptErr != nil {
		return entity.ImageAdmissionClaim{}, receiptErr
	} else if found {
		if replay.Artifact.Ref == "" && (len(replay.Expired) > 0 || len(replay.Rejected) > 0) {
			if err := committed(tx, ctx); err != nil {
				return entity.ImageAdmissionClaim{}, err
			}
			return entity.ImageAdmissionClaim{}, errs.ErrNotFound
		}
		if replay.AdmissionAttemptRef == "" || replay.AdmissionAttempt == 0 {
			return entity.ImageAdmissionClaim{}, errs.ErrNotFound
		}
		if replay.Artifact.PolicyRevision != repository.roleImages.PolicyRevision ||
			replay.Artifact.PolicySHA256 != repository.roleImages.PolicySHA256 {
			if err := committed(tx, ctx); err != nil {
				return entity.ImageAdmissionClaim{}, err
			}
			return entity.ImageAdmissionClaim{}, errs.ErrNotFound
		}
		var live bool
		if err := tx.QueryRow(ctx, queryRoleImagesAdmissionClaimReplayLive, pgx.StrictNamedArgs{"organization_id": current.organizationID, "artifact_ref": replay.Artifact.Ref, "version": replay.Artifact.Version, "fence": replay.Fence, "generation": replay.AuthorityGeneration, "token_sha256": tokenDigest(repository.admissionClaimFromReceipt(replay).ClaimToken)}).Scan(&live); err != nil {
			return entity.ImageAdmissionClaim{}, errs.ErrUnavailable
		}
		if !live {
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
	rejected, err := repository.rejectStaleRoleImageAdmissions(ctx, tx, current, policyArguments)
	if err != nil {
		return entity.ImageAdmissionClaim{}, err
	}
	expired, err := repository.expireRoleImageAdmissions(ctx, tx, current)
	if err != nil {
		return entity.ImageAdmissionClaim{}, err
	}
	var artifactID, artifactRef string
	var version, fence uint64
	err = tx.QueryRow(ctx, queryRoleImagesClaimAdmissionCandidate, policyArguments).Scan(
		&artifactID, &artifactRef, &version, &fence)
	if errors.Is(err, pgx.ErrNoRows) {
		if len(expired) > 0 || len(rejected) > 0 {
			if err := repository.storeRoleImageReceipt(ctx, tx, current, operation, key, intent,
				"IMAGE_ADMISSION_MAINTENANCE_OUTCOME", admissionClaimReceipt{Expired: expired, Rejected: rejected}); err != nil {
				return entity.ImageAdmissionClaim{}, err
			}
		}
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
	attempt, riskJSON, riskSHA, sourceReceipt, sourceEvidence, err := repository.ensureImageAdmissionAttempt(ctx, tx, current, artifact, artifactID, fence)
	if err != nil {
		return entity.ImageAdmissionClaim{}, err
	}
	if err := repository.hydrateImageRiskHistory(ctx, tx, current, &artifact); err != nil {
		return entity.ImageAdmissionClaim{}, err
	}
	receipt := admissionClaimReceipt{Artifact: artifact, Fence: fence,
		AuthorityGeneration: principal.CredentialRevision, ClaimExpiresAt: expiresAt, Expired: expired, Rejected: rejected,
		AdmissionAttemptRef: attempt.Ref, AdmissionAttempt: attempt.Number, RiskAcceptanceJSON: riskJSON, RiskAcceptanceSHA256: riskSHA,
		SourceAdmissionRevision: attempt.SourceAdmissionRevision, SourceAdmissionReceiptSHA256: sourceReceipt, SourceEvidenceManifestDigest: sourceEvidence}
	if err := repository.storeRoleImageReceipt(ctx, tx, current, operation, key, intent,
		"IMAGE_ADMISSION_CLAIM", receipt); err != nil {
		return entity.ImageAdmissionClaim{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.ImageAdmissionClaim{}, err
	}
	return repository.admissionClaimFromReceipt(receipt), nil
}

// rejectStaleRoleImageAdmissions collects SQL-rejected artifact/project references and closes rows
// before loading hydrated artifacts and recording policy-change audits. It uses the caller
// transaction, returns no stale claim and does not invent a scanner verdict or commit.
// rejectStaleRoleImageAdmissions собирает отклонённые SQL ссылки artifact/project и закрывает rows
// до чтения артефактов с историей риска и записи аудита смены policy. Использует транзакцию
// вызывающего метода, не выдаёт устаревший claim, не придумывает вердикт сканера и не делает
// commit.
func (repository *Repository) rejectStaleRoleImageAdmissions(ctx context.Context, tx pgx.Tx, current scope, arguments pgx.StrictNamedArgs) ([]entity.ImageArtifact, error) {
	rows, err := tx.Query(ctx, queryRoleImagesRejectStaleAdmissionCandidates, arguments)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	// rejectedArtifact retains each rejected public artifact reference and internal project ID until
	// the result rows are closed, so subsequent reads and audits can reuse the same transaction.
	// rejectedArtifact хранит публичную ссылку отклонённого артефакта и внутренний project ID до
	// закрытия rows, чтобы последующие чтения и аудит использовали ту же транзакцию.
	type rejectedArtifact struct{ ref, projectID string }
	var rejected []rejectedArtifact
	for rows.Next() {
		var item rejectedArtifact
		if err := rows.Scan(&item.ref, &item.projectID); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		rejected = append(rejected, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	result := make([]entity.ImageArtifact, 0, len(rejected))
	for _, item := range rejected {
		artifact, err := scanRoleImageArtifact(tx.QueryRow(ctx, queryRoleImagesGetActiveArtifact, current.organizationID, item.ref))
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		if err := repository.hydrateImageRiskHistory(ctx, tx, current, &artifact); err != nil {
			return nil, err
		}
		if err := repository.auditRoleImage(ctx, tx, current, item.projectID, "platform.role-images.admission.claim",
			"IMAGE_ARTIFACT", artifact.Ref, "i18n:IMAGE_ADMISSION_POLICY_CHANGED"); err != nil {
			return nil, err
		}
		result = append(result, artifact)
	}
	return result, nil
}

// admissionClaimFromReceipt reconstructs the admission token from saved artifact, fence, authority
// generation and expiry, and projects original attempt/risk/source pins; it does not verify that
// the saved lease remains live.
// admissionClaimFromReceipt восстанавливает admission token по сохранённым artifact, fence,
// authority generation и expiry и переносит исходные attempt/risk/source pins; сама не проверяет,
// что сохранённый lease ещё действует.
func (repository *Repository) admissionClaimFromReceipt(receipt admissionClaimReceipt) entity.ImageAdmissionClaim {
	token := repository.roleImageToken("image-admission", receipt.Artifact.Ref, 0,
		receipt.Fence, receipt.AuthorityGeneration, receipt.ClaimExpiresAt)
	return entity.ImageAdmissionClaim{Artifact: receipt.Artifact, ClaimToken: token,
		Fence: receipt.Fence, AuthorityGeneration: receipt.AuthorityGeneration,
		ClaimExpiresAt: receipt.ClaimExpiresAt, AdmissionAttemptRef: receipt.AdmissionAttemptRef, AdmissionAttempt: receipt.AdmissionAttempt,
		RiskAcceptanceJSON: receipt.RiskAcceptanceJSON, RiskAcceptanceSHA256: receipt.RiskAcceptanceSHA256,
		SourceAdmissionRevision: receipt.SourceAdmissionRevision, SourceAdmissionReceiptSHA256: receipt.SourceAdmissionReceiptSHA256,
		SourceEvidenceManifestDigest: receipt.SourceEvidenceManifestDigest}
}

// RecordAdmission requires an attempt identity, resolves scope and replays the exact receipt
// before fresh-transition checks. A new result locks artifact/version and current attempt, checks
// live token/fence/policy and bound inventory/report, then atomically records verdict,
// vulnerability history, receipt and recipe-change event; commit stays inside this method.
// RecordAdmission требует identity попытки, разрешает область и возвращает точную квитанцию до
// проверок нового перехода. Новый результат блокирует artifact/version и текущую попытку,
// проверяет живой token/fence/policy и связанные inventory/report, затем атомарно сохраняет
// вердикт, историю уязвимостей, квитанцию и событие изменения рецепта; commit выполняется здесь.
func (repository *Repository) RecordAdmission(ctx context.Context, input roleimagerepo.AdmissionRecordInput) (entity.ImageArtifact, error) {
	if input.ExpectedAdmissionAttemptRef == "" || input.ExpectedAdmissionAttempt == 0 {
		return entity.ImageArtifact{}, errs.ErrForbidden
	}
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
	locked, err := lockRoleImageArtifact(ctx, tx, current, input.ArtifactRef)
	if err != nil {
		return entity.ImageArtifact{}, err
	}
	if locked.Artifact.Version != input.ExpectedVersion {
		return entity.ImageArtifact{}, errs.ErrVersionMismatch
	}
	if err := repository.matchImageAdmissionAttempt(ctx, tx, current, locked.ID, input.ExpectedAdmissionAttemptRef, input.ExpectedAdmissionAttempt, "CLAIMED"); err != nil {
		return entity.ImageArtifact{}, err
	}
	var currentArtifact bool
	if err := tx.QueryRow(ctx, queryRoleImagesAdmissionCurrent, pgx.StrictNamedArgs{"organization_id": current.organizationID, "artifact_id": locked.ID}).Scan(&currentArtifact); err != nil {
		return entity.ImageArtifact{}, errs.ErrUnavailable
	}
	if !currentArtifact {
		return entity.ImageArtifact{}, errs.ErrForbidden
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
	if input.ToolInventoryJSON != "" {
		locked.Artifact.ToolInventorySHA256 = input.ToolInventorySHA256
		if hydrateArtifactToolInventory(&locked.Artifact, input.ToolInventoryJSON) != nil {
			return entity.ImageArtifact{}, errs.ErrInvalid
		}
	} else if input.Verdict == "ACCEPTED" || input.ToolInventorySHA256 != "" {
		return entity.ImageArtifact{}, errs.ErrInvalid
	}
	if err := repository.validateAdmissionVulnerabilityReport(ctx, tx, current, locked, input); err != nil {
		return entity.ImageArtifact{}, err
	}
	if err := tx.QueryRow(ctx, queryRoleImagesRecordAdmission, current.organizationID,
		locked.ID, locked.Artifact.Version, input.Verdict, input.SBOMSHA256,
		input.VulnerabilityEvidenceSHA256, input.SignatureIdentity, input.SignatureSHA256,
		input.AdmissionReceiptSHA256, input.AdmissionReceiptOCIManifestDigest, input.ToolInventoryJSON, input.ToolInventorySHA256).Scan(
		&locked.Artifact.Version, &locked.Artifact.AdmissionVerdict,
		&locked.Artifact.AdmissionRevision, &locked.Artifact.UpdatedAt); err != nil {
		return entity.ImageArtifact{}, mapRoleImageWriteError(err)
	}
	locked.Artifact.SBOMSHA256 = input.SBOMSHA256
	locked.Artifact.VulnerabilityEvidenceSHA256 = input.VulnerabilityEvidenceSHA256
	locked.Artifact.SignatureIdentity, locked.Artifact.SignatureSHA256 = input.SignatureIdentity, input.SignatureSHA256
	locked.Artifact.AdmissionReceiptSHA256 = input.AdmissionReceiptSHA256
	locked.Artifact.AdmissionReceiptOCIManifestDigest = input.AdmissionReceiptOCIManifestDigest
	if err := repository.storeAdmissionVulnerabilityReport(ctx, tx, current, locked.ID, locked.Artifact, input); err != nil {
		return entity.ImageArtifact{}, err
	}
	if err := repository.hydrateImageRiskHistory(ctx, tx, current, &locked.Artifact); err != nil {
		return entity.ImageArtifact{}, err
	}
	if err := repository.storeRoleImageReceipt(ctx, tx, current, operation,
		input.IdempotencyKey, intent, "IMAGE_ADMISSION_RECORD", locked.Artifact); err != nil {
		return entity.ImageArtifact{}, err
	}
	// Допуск меняет подробности рецепта и доступность публикации. Сигнал
	// принадлежит той же транзакции; область берётся из заблокированного артефакта.
	if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "ROLE_IMAGE_RECIPE_CHANGED",
		locked.Artifact.ProjectRef, locked.Artifact.RecipeRef, "i18n:ROLE_IMAGE_RECIPE_CHANGED",
		int64(locked.Artifact.RecipeVersion), input.Verdict); err != nil {
		return entity.ImageArtifact{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.ImageArtifact{}, err
	}
	return locked.Artifact, nil
}

// ClaimPromotion applies the existing bounded transaction retry policy to the entire promotion
// claim; the inner method owns scope, idempotent replay and candidate selection.
// ClaimPromotion применяет прежнюю ограниченную политику повтора транзакций ко всему promotion
// claim; внутренний метод владеет областью, повторной выдачей квитанции и выбором кандидата.
func (repository *Repository) ClaimPromotion(ctx context.Context, principal value.Principal, key string) (entity.ImagePromotionClaim, error) {
	return retryRoleImageTransaction(ctx, func() (entity.ImagePromotionClaim, error) {
		return repository.claimPromotion(ctx, principal, key)
	})
}

// claimPromotion locks actor/key and replays a stored promotion claim without rechecking its
// liveness. A new claim locks candidate, request and recipe, validates their exact snapshot and
// reclaim eligibility, advances artifact version/fence, marks the request PROMOTING and stores its
// token coordinates before commit; a missing candidate rolls back with NotFound.
// claimPromotion блокирует actor/key и повторно выдаёт сохранённый promotion claim без новой
// проверки срока. Новый claim блокирует кандидат, запрос и рецепт, проверяет точный snapshot и
// допустимость повторного claim, увеличивает version/fence артефакта, переводит запрос в PROMOTING
// и сохраняет координаты токена до commit; отсутствие кандидата даёт rollback и NotFound.
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

// promotionClaimFromReceipt reconstructs the request-bound promotion token from immutable receipt
// coordinates and returns saved artifact/fence/generation/expiry without querying current state.
// promotionClaimFromReceipt восстанавливает связанный с запросом promotion token по неизменяемым
// координатам квитанции и возвращает сохранённые artifact/fence/generation/expiry без чтения
// текущего состояния.
func (repository *Repository) promotionClaimFromReceipt(receipt promotionClaimReceipt) entity.ImagePromotionClaim {
	token := repository.roleImagePromotionToken("image-promotion", receipt.Artifact,
		receipt.PromotionRequestReceiptSHA256, receipt.Fence, receipt.AuthorityGeneration, receipt.ClaimExpiresAt)
	return entity.ImagePromotionClaim{Artifact: receipt.Artifact, PromotionClaim: token,
		Fence: receipt.Fence, AuthorityGeneration: receipt.AuthorityGeneration,
		ClaimExpiresAt: receipt.ClaimExpiresAt}
}

// AuthorizePromotion runs the complete claim-to-authorization transition under the existing
// bounded transaction retry policy rather than retrying an individual write.
// AuthorizePromotion выполняет полный переход от claim к authorization в прежней ограниченной
// политике повтора транзакций, повторяя целую операцию.
func (repository *Repository) AuthorizePromotion(ctx context.Context, input roleimagerepo.PromotionAuthorizeInput) (entity.ImagePromotionAuthorization, error) {
	return retryRoleImageTransaction(ctx, func() (entity.ImagePromotionAuthorization, error) {
		return repository.authorizePromotion(ctx, input)
	})
}

// authorizePromotion serializes actor/key and returns exact receipt replay before fresh checks. It
// locks artifact/request/recipe, verifies OCC, unexpired request-bound claim, exact credential
// generation and promotion snapshot, then replaces the claim with a version-bound expiring
// authorization and persists its receipt in the same transaction; it performs no registry push.
// authorizePromotion сериализует actor/key и возвращает точную квитанцию до проверок нового
// перехода. Блокирует artifact/request/recipe, проверяет OCC, неистёкший связанный с запросом
// claim, точное поколение credentials и promotion snapshot, затем заменяет claim ограниченной по
// сроку и версии authorization и сохраняет квитанцию в той же транзакции; push в registry не
// выполняется.
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
	locked, err := lockRoleImageArtifact(ctx, tx, current, input.ArtifactRef)
	if err != nil {
		return entity.ImagePromotionAuthorization{}, err
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

// promotionAuthorizationFromReceipt rebuilds the authorization token with the saved artifact
// version, request receipt, fence, generation and expiry; projection alone does not establish
// current authority or lease liveness.
// promotionAuthorizationFromReceipt восстанавливает authorization token по сохранённым версии
// артефакта, квитанции запроса, fence, generation и expiry; само преобразование не подтверждает
// текущие полномочия либо действующий lease.
func (repository *Repository) promotionAuthorizationFromReceipt(receipt promotionAuthorizationReceipt) entity.ImagePromotionAuthorization {
	token := repository.roleImagePromotionToken("image-promotion-authorization", receipt.Artifact,
		receipt.PromotionRequestReceiptSHA256, receipt.Fence, receipt.AuthorityGeneration,
		receipt.AuthorizationExpiresAt)
	return entity.ImagePromotionAuthorization{Artifact: receipt.Artifact,
		AuthorizationToken: token, AuthorizationExpiresAt: receipt.AuthorizationExpiresAt}
}

// CompletePromotion runs the entire promotion completion through the existing bounded transaction
// retry policy; durable receipt replay remains the inner transaction responsibility.
// CompletePromotion выполняет полное завершение публикации через прежнюю ограниченную политику
// повтора транзакций; повторная выдача устойчивой квитанции остаётся обязанностью внутренней
// транзакции.
func (repository *Repository) CompletePromotion(ctx context.Context, input roleimagerepo.PromotionCompleteInput) (entity.ImageArtifact, error) {
	return retryRoleImageTransaction(ctx, func() (entity.ImageArtifact, error) {
		return repository.completePromotion(ctx, input)
	})
}

// completePromotion replays an exact completion receipt or locks artifact/request/recipe and
// verifies OCC, live version-bound authorization, credential generation, evidence and exact
// promoted repository/digest. It atomically completes artifact/request, activates the recipe,
// inserts an immutable promoted revision and records audit/event/receipt, validating returned
// versions and identities before commit; external registry readback is supplied by the caller.
// completePromotion возвращает точную квитанцию завершения либо блокирует artifact/request/recipe
// и проверяет OCC, живую связанную с версией authorization, credential generation, evidence и
// точные promoted repository/digest. Атомарно завершает artifact/request, активирует рецепт,
// вставляет неизменяемую опубликованную revision и сохраняет audit/event/receipt, проверяя
// возвращённые версии и identity до commit; результат внешнего registry readback передаёт
// вызывающая сторона.
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
	locked, err := lockRoleImageArtifact(ctx, tx, current, input.ArtifactRef)
	if err != nil {
		return entity.ImageArtifact{}, err
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
	locked.Artifact.PromotionState = promotionState
	locked.Artifact.PromotionRequested = true
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
