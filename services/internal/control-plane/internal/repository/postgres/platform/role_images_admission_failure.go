package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// admissionFailure copies exact artifact/recipe/build/scope lineage into a FAILED projection with
// the supplied safe error code, without changing stored state.
// admissionFailure копирует точную artifact/recipe/build/scope lineage в FAILED projection с
// заданным безопасным error code без изменения сохранённого состояния.
func admissionFailure(artifact entity.ImageArtifact, code string) entity.RoleImageAdmissionFailure {
	return entity.RoleImageAdmissionFailure{ImageArtifactRef: artifact.Ref, Version: artifact.Version,
		RecipeRef: artifact.RecipeRef, RecipeGeneration: artifact.RecipeGeneration,
		BuildRef: artifact.BuildRef, BuildAttempt: artifact.BuildAttempt, ScopeKind: artifact.ScopeKind,
		OrganizationRef: artifact.OrganizationRef, ProjectRef: artifact.ProjectRef, State: "FAILED", ErrorCode: code}
}

// FailAdmission runs the complete explicit failure through the existing bounded role-image retry
// policy while retaining the normal live-claim path.
// FailAdmission выполняет полный явный failure через прежнюю ограниченную role-image retry policy,
// сохраняя обычный live-claim path.
func (repository *Repository) FailAdmission(ctx context.Context, input roleimagerepo.AdmissionFailureInput) (entity.RoleImageAdmissionFailure, error) {
	return retryRoleImageTransaction(ctx, func() (entity.RoleImageAdmissionFailure, error) {
		return repository.failAdmission(ctx, input)
	})
}

// failAdmission selects the normal terminal-admission path with expiry disabled; the common
// implementation still owns all claim and authority checks.
// failAdmission выбирает обычный terminal-admission path с выключенным expiry; все claim и
// authority checks остаются в общей реализации.
func (repository *Repository) failAdmission(ctx context.Context, input roleimagerepo.AdmissionFailureInput) (entity.RoleImageAdmissionFailure, error) {
	return repository.terminalAdmission(ctx, input, false)
}

// ExpireAdmission maps exact expiry pins to the common terminal input and fixed
// ADMISSION_LEASE_EXPIRED code, selecting expiry semantics within the existing retry policy.
// ExpireAdmission переносит точные expiry pins в общий terminal input с фиксированным
// ADMISSION_LEASE_EXPIRED и выбирает expiry semantics в прежней retry policy.
func (repository *Repository) ExpireAdmission(ctx context.Context, input roleimagerepo.AdmissionExpiryInput) (entity.RoleImageAdmissionFailure, error) {
	return retryRoleImageTransaction(ctx, func() (entity.RoleImageAdmissionFailure, error) {
		return repository.terminalAdmission(ctx, roleimagerepo.AdmissionFailureInput{ExpectedAdmissionAttemptRef: input.ExpectedAdmissionAttemptRef, ExpectedAdmissionAttempt: input.ExpectedAdmissionAttempt, Principal: input.Principal, IdempotencyKey: input.IdempotencyKey, ArtifactRef: input.ArtifactRef, ExpectedVersion: input.ExpectedVersion, ExpectedFence: input.ExpectedFence, ExpectedAuthorityGeneration: input.ExpectedAuthorityGeneration, ManifestDigest: input.ManifestDigest, ImmutableBuildSHA256: input.ImmutableBuildSHA256, ProvenanceSHA256: input.ProvenanceSHA256, PolicyRevision: input.PolicyRevision, PolicySHA256: input.PolicySHA256, BuildRef: input.BuildRef, ExpectedBuildAttempt: input.ExpectedBuildAttempt, RecipeGeneration: input.RecipeGeneration, SpecSHA256: input.SpecSHA256, ErrorCode: "ADMISSION_LEASE_EXPIRED"}, true)
	})
}

// terminalAdmission locks actor/key and artifact/attempt in a serializable transaction, separating
// live-token failure from expiry and confirmed prior-expiry readback; fresh transitions store
// state/audit/receipt/event, while prior expiry adds only its receipt.
// terminalAdmission блокирует actor/key и artifact/attempt в serializable транзакции, разделяя
// live-token failure, expiry и подтверждённый prior-expiry readback; свежий переход сохраняет
// state/audit/receipt/event, прежний expiry добавляет только receipt.
func (repository *Repository) terminalAdmission(ctx context.Context, input roleimagerepo.AdmissionFailureInput, expiry bool) (entity.RoleImageAdmissionFailure, error) {
	if input.ExpectedAdmissionAttemptRef == "" || input.ExpectedAdmissionAttempt == 0 {
		return entity.RoleImageAdmissionFailure{}, errs.ErrForbidden
	}
	current, err := repository.resolveScope(ctx, input.Principal)
	if err != nil {
		return entity.RoleImageAdmissionFailure{}, err
	}
	intentInput := input
	// Fresh proof correlation и credential rotation не меняют immutable command intent.
	intentInput.Principal = value.Principal{}
	operation, intent := "platform.role-images.admission.fail", roleImageDigest(intentInput)
	if expiry {
		operation = "platform.role-images.admission.expire"
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.RoleImageAdmissionFailure{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.lockRoleImageIdempotency(ctx, tx, current, operation, input.IdempotencyKey); err != nil {
		return entity.RoleImageAdmissionFailure{}, err
	}
	locked, err := scanLockedArtifact(tx.QueryRow(ctx, queryRoleImagesLockArtifact, current.organizationID, input.ArtifactRef))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.RoleImageAdmissionFailure{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.RoleImageAdmissionFailure{}, errs.ErrUnavailable
	}
	attempt, err := repository.readImageAdmissionAttempt(ctx, tx, current, locked.ID, locked.Artifact.Ref)
	if err != nil || attempt.Attempt.Ref != input.ExpectedAdmissionAttemptRef || attempt.Attempt.Number != input.ExpectedAdmissionAttempt {
		return entity.RoleImageAdmissionFailure{}, errs.ErrForbidden
	}
	var replay entity.RoleImageAdmissionFailure
	if found, err := repository.loadRoleImageReceipt(ctx, tx, current, operation, input.IdempotencyKey, intent, &replay); err != nil {
		return entity.RoleImageAdmissionFailure{}, err
	} else if found {
		if err := committed(tx, ctx); err != nil {
			return entity.RoleImageAdmissionFailure{}, err
		}
		return replay, nil
	}
	// Scope и immutable lineage разрешены владельцем; payload и JobUID не выдают authority.
	if expiry && locked.AdmissionState == "FAILED" && locked.Artifact.Version == input.ExpectedVersion+1 && locked.AdmissionFence == input.ExpectedFence {
		var previousGeneration uint64
		var code string
		if err := tx.QueryRow(ctx, queryRoleImagesReadFailureClaim, pgx.StrictNamedArgs{"organization_id": current.organizationID, "artifact_id": locked.ID}).Scan(&previousGeneration, &code); err != nil {
			return entity.RoleImageAdmissionFailure{}, errs.ErrUnavailable
		}
		if code != "ADMISSION_LEASE_EXPIRED" || previousGeneration != input.ExpectedAuthorityGeneration || previousGeneration > input.Principal.CredentialRevision || !matchesAdmissionFailureTuple(locked.Artifact, input) {
			return entity.RoleImageAdmissionFailure{}, errs.ErrForbidden
		}
		result := admissionFailure(locked.Artifact, code)
		if err := repository.storeRoleImageReceipt(ctx, tx, current, operation, input.IdempotencyKey, intent, "IMAGE_ADMISSION_FAILURE", result); err != nil {
			return entity.RoleImageAdmissionFailure{}, err
		}
		if err := committed(tx, ctx); err != nil {
			return entity.RoleImageAdmissionFailure{}, err
		}
		return result, nil
	}
	if locked.AdmissionState != "CLAIMED" || locked.AdmissionFence != input.ExpectedFence ||
		locked.AdmissionAuthorityGeneration != input.ExpectedAuthorityGeneration ||
		locked.AdmissionAuthorityGeneration > input.Principal.CredentialRevision ||
		(!expiry && !tokenMatches(input.ClaimToken, locked.AdmissionTokenSHA256)) ||
		!matchesAdmissionFailureTuple(locked.Artifact, input) ||
		(!expiry && (input.PolicyRevision != repository.roleImages.PolicyRevision || input.PolicySHA256 != repository.roleImages.PolicySHA256)) {
		return entity.RoleImageAdmissionFailure{}, errs.ErrForbidden
	}
	if locked.Artifact.Version != input.ExpectedVersion {
		return entity.RoleImageAdmissionFailure{}, errs.ErrVersionMismatch
	}
	query := queryRoleImagesFailAdmission
	if expiry {
		query = queryRoleImagesExpireAdmissionClaim
	} else {
		var eligible bool
		if err := tx.QueryRow(ctx, queryRoleImagesAdmissionCurrent, pgx.StrictNamedArgs{"organization_id": current.organizationID, "artifact_id": locked.ID}).Scan(&eligible); err != nil {
			return entity.RoleImageAdmissionFailure{}, errs.ErrUnavailable
		}
		if !eligible {
			return entity.RoleImageAdmissionFailure{}, errs.ErrForbidden
		}
	}
	if err := tx.QueryRow(ctx, query, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "artifact_id": locked.ID,
		"expected_version": input.ExpectedVersion, "error_code": input.ErrorCode,
	}).Scan(&locked.Artifact.Version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.RoleImageAdmissionFailure{}, errs.ErrForbidden
		}
		return entity.RoleImageAdmissionFailure{}, mapRoleImageWriteError(err)
	}
	result := admissionFailure(locked.Artifact, input.ErrorCode)
	if err := repository.auditRoleImage(ctx, tx, current, mustProjectID(ctx, tx, current.organizationID, result.ProjectRef), operation,
		"IMAGE_ARTIFACT", result.ImageArtifactRef, "i18n:IMAGE_ADMISSION_FAILED"); err != nil {
		return entity.RoleImageAdmissionFailure{}, err
	}
	if err := repository.storeRoleImageReceipt(ctx, tx, current, operation, input.IdempotencyKey, intent, "IMAGE_ADMISSION_FAILURE", result); err != nil {
		return entity.RoleImageAdmissionFailure{}, err
	}
	if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "ROLE_IMAGE_RECIPE_CHANGED", result.ProjectRef,
		result.RecipeRef, "i18n:ROLE_IMAGE_RECIPE_CHANGED", int64(locked.Artifact.RecipeVersion), result.ErrorCode); err != nil {
		return entity.RoleImageAdmissionFailure{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.RoleImageAdmissionFailure{}, err
	}
	return result, nil
}

// matchesAdmissionFailureTuple compares immutable manifest/build/provenance/policy/specification
// and recipe-generation pins only; version, fence, authority and token are checked separately by
// the caller.
// matchesAdmissionFailureTuple сравнивает только неизменяемые
// manifest/build/provenance/policy/specification и recipe-generation pins; version, fence,
// authority и token проверяет caller отдельно.
func matchesAdmissionFailureTuple(a entity.ImageArtifact, input roleimagerepo.AdmissionFailureInput) bool {
	return a.ManifestDigest == input.ManifestDigest && a.ImmutableBuildSHA256 == input.ImmutableBuildSHA256 && a.ProvenanceSHA256 == input.ProvenanceSHA256 && a.PolicyRevision == input.PolicyRevision && a.PolicySHA256 == input.PolicySHA256 && a.SpecSHA256 == input.SpecSHA256 && a.BuildRef == input.BuildRef && a.BuildAttempt == input.ExpectedBuildAttempt && a.RecipeGeneration == input.RecipeGeneration
}

// expireRoleImageAdmissions collects the bounded SQL expiry results before emitting one
// audit/event per changed artifact in the caller transaction; it returns the changed projections
// without owning commit.
// expireRoleImageAdmissions собирает ограниченные результаты SQL expiry до записи одного
// audit/event на изменённый artifact в транзакции caller; возвращает изменённые projections без
// собственного commit.
func (repository *Repository) expireRoleImageAdmissions(ctx context.Context, tx pgx.Tx, current scope) ([]entity.RoleImageAdmissionFailure, error) {
	rows, err := tx.Query(ctx, queryRoleImagesExpireAdmissions, pgx.StrictNamedArgs{"organization_id": current.organizationID})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	// expiredAdmission retains the failure projection plus internal project and recipe-version pins
	// needed to emit exact expiry audit and event after closing rows.
	// expiredAdmission хранит failure projection и внутренние project и recipe-version pins для точных
	// expiry audit и event после закрытия rows.
	type expiredAdmission struct {
		failure       entity.RoleImageAdmissionFailure
		projectID     string
		recipeVersion uint64
	}
	var expired []expiredAdmission
	for rows.Next() {
		var item expiredAdmission
		failure := &item.failure
		if err := rows.Scan(&failure.ImageArtifactRef, &failure.Version, &failure.RecipeRef, &failure.RecipeGeneration,
			&failure.BuildRef, &failure.BuildAttempt, &failure.ScopeKind, &failure.OrganizationRef, &failure.ProjectRef,
			&item.projectID, &item.recipeVersion); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		failure.State, failure.ErrorCode = "FAILED", "ADMISSION_LEASE_EXPIRED"
		expired = append(expired, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	result := make([]entity.RoleImageAdmissionFailure, 0, len(expired))
	for _, item := range expired {
		failure := item.failure
		if err := repository.auditRoleImage(ctx, tx, current, item.projectID, "platform.role-images.admission.expire",
			"IMAGE_ARTIFACT", failure.ImageArtifactRef, "i18n:IMAGE_ADMISSION_LEASE_EXPIRED"); err != nil {
			return nil, err
		}
		if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "ROLE_IMAGE_RECIPE_CHANGED", failure.ProjectRef,
			failure.RecipeRef, "i18n:ROLE_IMAGE_RECIPE_CHANGED", int64(item.recipeVersion), failure.ErrorCode); err != nil {
			return nil, err
		}
		result = append(result, failure)
	}
	return result, nil
}
