package platform

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

type buildClaimReceipt struct {
	Build               entity.ImageBuild
	Input               entity.RoleImageBuildInput
	Fence               uint64
	AuthorityGeneration uint64
	LeaseExpiresAt      time.Time
}

type admissionClaimReceipt struct {
	Artifact            entity.ImageArtifact
	Fence               uint64
	AuthorityGeneration uint64
	ClaimExpiresAt      time.Time
}

type promotionClaimReceipt struct {
	Artifact            entity.ImageArtifact
	Fence               uint64
	AuthorityGeneration uint64
	ClaimExpiresAt      time.Time
}

type promotionAuthorizationReceipt struct {
	Artifact               entity.ImageArtifact
	Fence                  uint64
	AuthorityGeneration    uint64
	AuthorizationExpiresAt time.Time
}

func (repository *Repository) ClaimBuild(ctx context.Context, principal value.Principal, key string) (entity.ImageBuildClaim, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.ImageBuildClaim{}, err
	}
	operation := "platform.role-images.builds.claim"
	intent := roleImageDigest(struct{ Key string }{key})
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.ImageBuildClaim{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var replay buildClaimReceipt
	if found, receiptErr := repository.loadRoleImageReceipt(ctx, tx, current, operation, key, intent, &replay); receiptErr != nil {
		return entity.ImageBuildClaim{}, receiptErr
	} else if found {
		if err := committed(tx, ctx); err != nil {
			return entity.ImageBuildClaim{}, err
		}
		return repository.buildClaimFromReceipt(replay), nil
	}
	var buildID, buildRef, recipeID, stage string
	var version, fence uint64
	var attempt, maximumAttempts uint32
	err = tx.QueryRow(ctx, queryRoleImagesClaimBuildCandidate, current.organizationID).Scan(
		&buildID, &buildRef, &version, &attempt, &maximumAttempts, &fence, &recipeID, &stage)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ImageBuildClaim{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.ImageBuildClaim{}, errs.ErrUnavailable
	}
	if stage != "QUEUED" {
		attempt++
	}
	if attempt == 0 || attempt > maximumAttempts {
		return entity.ImageBuildClaim{}, errs.ErrConflict
	}
	fence++
	expiresAt := time.Now().UTC().Add(repository.roleImages.BuildLeaseDuration)
	token := repository.roleImageToken("image-build", buildRef, attempt, fence,
		principal.CredentialRevision, expiresAt)
	build, err := scanBuild(tx.QueryRow(ctx, queryRoleImagesClaimBuild,
		current.organizationID, buildID, version, attempt, principal.CallerWorkload,
		principal.CredentialRevision, fence, tokenDigest(token), expiresAt))
	if err != nil {
		return entity.ImageBuildClaim{}, mapRoleImageWriteError(err)
	}
	var recipe entity.RoleImageRecipe
	var specification []byte
	var immutable string
	err = tx.QueryRow(ctx, queryRoleImagesGetBuildInput, current.organizationID, buildID).Scan(
		&recipe.Ref, &recipe.Version, &recipe.Generation, &recipe.SpecSHA256,
		&specification, &immutable, &recipe.PolicyRevision, &recipe.PolicySHA256,
		&recipe.RoleRuntimeContractRevision, &recipe.RoleRuntimeContractSHA256)
	if err != nil || decodeJSON(specification, &recipe.Input) != nil {
		return entity.ImageBuildClaim{}, errs.ErrConflict
	}
	if recipe.PolicyRevision != repository.roleImages.PolicyRevision ||
		recipe.PolicySHA256 != repository.roleImages.PolicySHA256 ||
		recipe.RoleRuntimeContractRevision != repository.roleImages.RoleRuntimeContractRevision ||
		recipe.RoleRuntimeContractSHA256 != repository.roleImages.RoleRuntimeContractSHA256 {
		return entity.ImageBuildClaim{}, errs.ErrConflict
	}
	receipt := buildClaimReceipt{Build: build, Input: newRoleImageBuildInput(recipe, immutable),
		Fence: fence, AuthorityGeneration: principal.CredentialRevision, LeaseExpiresAt: expiresAt}
	if err := repository.storeRoleImageReceipt(ctx, tx, current, operation, key, intent,
		"IMAGE_BUILD_CLAIM", receipt); err != nil {
		return entity.ImageBuildClaim{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.ImageBuildClaim{}, err
	}
	return repository.buildClaimFromReceipt(receipt), nil
}

func (repository *Repository) buildClaimFromReceipt(receipt buildClaimReceipt) entity.ImageBuildClaim {
	token := repository.roleImageToken("image-build", receipt.Build.Ref, receipt.Build.Attempt,
		receipt.Fence, receipt.AuthorityGeneration, receipt.LeaseExpiresAt)
	return entity.ImageBuildClaim{Build: receipt.Build, Input: receipt.Input, LeaseToken: token,
		Fence: receipt.Fence, AuthorityGeneration: receipt.AuthorityGeneration,
		LeaseExpiresAt: receipt.LeaseExpiresAt}
}

func (repository *Repository) RenewBuild(ctx context.Context, input roleimagerepo.BuildLeaseInput) (entity.ImageBuildClaim, error) {
	current, tx, locked, operation, intent, err := repository.beginBuildMutation(ctx, input, "platform.role-images.builds.renew")
	if err != nil {
		return entity.ImageBuildClaim{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var replay buildClaimReceipt
	if found, receiptErr := repository.loadRoleImageReceipt(ctx, tx, current, operation,
		input.IdempotencyKey, intent, &replay); receiptErr != nil {
		return entity.ImageBuildClaim{}, receiptErr
	} else if found {
		if err := committed(tx, ctx); err != nil {
			return entity.ImageBuildClaim{}, err
		}
		return repository.buildClaimFromReceipt(replay), nil
	}
	if err := validateBuildClaim(input, locked, time.Now().UTC()); err != nil {
		return entity.ImageBuildClaim{}, err
	}
	expiresAt := time.Now().UTC().Add(repository.roleImages.BuildLeaseDuration)
	token := repository.roleImageToken("image-build", locked.Build.Ref, locked.Build.Attempt,
		locked.Build.Fence, input.Principal.CredentialRevision, expiresAt)
	var version uint64
	if err := tx.QueryRow(ctx, queryRoleImagesRenewBuild, current.organizationID, locked.ID,
		locked.Build.Version, tokenDigest(token), expiresAt, input.Principal.CredentialRevision).Scan(&version, &expiresAt, &locked.Build.UpdatedAt); err != nil {
		return entity.ImageBuildClaim{}, mapRoleImageWriteError(err)
	}
	locked.Build.Version, locked.Build.AuthorityGeneration = version, input.Principal.CredentialRevision
	locked.Build.LeaseExpiresAt, locked.Build.LeaseTokenSHA256 = &expiresAt, tokenDigest(token)
	receipt := buildClaimReceipt{Build: locked.Build,
		Input: newRoleImageBuildInput(entity.RoleImageRecipe{Ref: locked.Build.RecipeRef,
			Version: locked.Build.RecipeVersion, Generation: locked.Build.RecipeGeneration,
			SpecSHA256: locked.Build.SpecSHA256, Input: locked.Specification,
			PolicyRevision: locked.PolicyRevision, PolicySHA256: locked.PolicySHA256,
			RoleRuntimeContractRevision: locked.ContractRevision,
			RoleRuntimeContractSHA256:   locked.ContractSHA256}, locked.Build.ImmutableBuildSHA256),
		Fence: locked.Build.Fence, AuthorityGeneration: input.Principal.CredentialRevision,
		LeaseExpiresAt: expiresAt}
	if err := repository.storeRoleImageReceipt(ctx, tx, current, operation, input.IdempotencyKey,
		intent, "IMAGE_BUILD_RENEWAL", receipt); err != nil {
		return entity.ImageBuildClaim{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.ImageBuildClaim{}, err
	}
	return repository.buildClaimFromReceipt(receipt), nil
}

func (repository *Repository) ReportBuildProgress(ctx context.Context, input roleimagerepo.BuildProgressInput) (entity.ImageBuild, error) {
	current, tx, locked, operation, intent, err := repository.beginBuildMutation(ctx, input.BuildLeaseInput, "platform.role-images.builds.progress")
	if err != nil {
		return entity.ImageBuild{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var replay entity.ImageBuild
	if found, receiptErr := repository.loadRoleImageReceipt(ctx, tx, current, operation,
		input.IdempotencyKey, roleImageDigest(input), &replay); receiptErr != nil {
		return entity.ImageBuild{}, receiptErr
	} else if found {
		if err := committed(tx, ctx); err != nil {
			return entity.ImageBuild{}, err
		}
		return replay, nil
	}
	intent = roleImageDigest(input)
	if err := validateBuildClaim(input.BuildLeaseInput, locked, time.Now().UTC()); err != nil {
		return entity.ImageBuild{}, err
	}
	if err := tx.QueryRow(ctx, queryRoleImagesProgressBuild, current.organizationID, locked.ID,
		locked.Build.Version, input.Stage, input.ProgressPercent).Scan(&locked.Build.Version,
		&locked.Build.Stage, &locked.Build.ProgressPercent, &locked.Build.UpdatedAt); err != nil {
		return entity.ImageBuild{}, mapRoleImageWriteError(err)
	}
	if err := repository.storeRoleImageReceipt(ctx, tx, current, operation, input.IdempotencyKey,
		intent, "IMAGE_BUILD_PROGRESS", locked.Build); err != nil {
		return entity.ImageBuild{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.ImageBuild{}, err
	}
	return locked.Build, nil
}

func (repository *Repository) CompleteBuild(ctx context.Context, input roleimagerepo.BuildCompletionInput) (entity.ImageBuild, entity.ImageArtifact, error) {
	current, tx, locked, operation, _, err := repository.beginBuildMutation(ctx, input.BuildLeaseInput, "platform.role-images.builds.complete")
	if err != nil {
		return entity.ImageBuild{}, entity.ImageArtifact{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	intent := roleImageDigest(input)
	type completionReceipt struct {
		Build    entity.ImageBuild
		Artifact entity.ImageArtifact
	}
	var replay completionReceipt
	if found, receiptErr := repository.loadRoleImageReceipt(ctx, tx, current, operation,
		input.IdempotencyKey, intent, &replay); receiptErr != nil {
		return entity.ImageBuild{}, entity.ImageArtifact{}, receiptErr
	} else if found {
		if err := committed(tx, ctx); err != nil {
			return entity.ImageBuild{}, entity.ImageArtifact{}, err
		}
		return replay.Build, replay.Artifact, nil
	}
	if err := validateBuildClaim(input.BuildLeaseInput, locked, time.Now().UTC()); err != nil {
		return entity.ImageBuild{}, entity.ImageArtifact{}, err
	}
	if input.StagingReference != repository.roleImages.StagingRepository+"@"+input.ManifestDigest ||
		input.ImmutableBuildSHA256 != locked.Build.ImmutableBuildSHA256 {
		return entity.ImageBuild{}, entity.ImageArtifact{}, errs.ErrForbidden
	}
	if err := tx.QueryRow(ctx, queryRoleImagesCompleteBuild, current.organizationID, locked.ID,
		locked.Build.Version, input.StagingReference, input.ManifestDigest,
		input.ProvenanceSHA256, input.ImmutableBuildSHA256).Scan(&locked.Build.Version,
		&locked.Build.Stage, &locked.Build.ProgressPercent, &locked.Build.StagingReference,
		&locked.Build.ManifestDigest, &locked.Build.ProvenanceSHA256,
		&locked.Build.ImmutableBuildSHA256, &locked.Build.UpdatedAt); err != nil {
		return entity.ImageBuild{}, entity.ImageArtifact{}, mapRoleImageWriteError(err)
	}
	locked.Build.LeaseExpiresAt, locked.Build.LeaseTokenSHA256, locked.Build.ClaimantWorkload = nil, "", ""
	locked.Build.AuthorityGeneration = 0
	artifactRef, _ := newRef("imgart")
	var artifactID string
	if err := tx.QueryRow(ctx, queryRoleImagesInsertArtifact, artifactRef, current.organizationID,
		locked.ProjectID, locked.RecipeID, locked.Build.RecipeVersion,
		locked.Build.RecipeGeneration, locked.Build.SpecSHA256, locked.ID,
		locked.Build.Version, locked.Build.Attempt, asJSON(locked.Specification),
		locked.PolicyRevision, locked.PolicySHA256, locked.ContractRevision,
		locked.ContractSHA256, input.StagingReference, input.ManifestDigest,
		input.ImmutableBuildSHA256, input.ProvenanceSHA256).Scan(&artifactID); err != nil {
		return entity.ImageBuild{}, entity.ImageArtifact{}, mapRoleImageWriteError(err)
	}
	artifact, err := scanRoleImageArtifact(tx.QueryRow(ctx, queryRoleImagesGetActiveArtifact,
		current.organizationID, artifactRef))
	if err != nil {
		return entity.ImageBuild{}, entity.ImageArtifact{}, errs.ErrUnavailable
	}
	receipt := completionReceipt{Build: locked.Build, Artifact: artifact}
	if err := repository.auditRoleImage(ctx, tx, current, locked.ProjectID, operation,
		"IMAGE_BUILD", locked.Build.Ref, "i18n:ROLE_IMAGE_BUILD_COMPLETED"); err != nil {
		return entity.ImageBuild{}, entity.ImageArtifact{}, err
	}
	if err := repository.storeRoleImageReceipt(ctx, tx, current, operation, input.IdempotencyKey,
		intent, "IMAGE_BUILD_COMPLETION", receipt); err != nil {
		return entity.ImageBuild{}, entity.ImageArtifact{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.ImageBuild{}, entity.ImageArtifact{}, err
	}
	return locked.Build, artifact, nil
}

func (repository *Repository) FailBuild(ctx context.Context, input roleimagerepo.BuildFailureInput) (entity.ImageBuild, error) {
	current, tx, locked, operation, _, err := repository.beginBuildMutation(ctx, input.BuildLeaseInput, "platform.role-images.builds.fail")
	if err != nil {
		return entity.ImageBuild{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	intent := roleImageDigest(input)
	var replay entity.ImageBuild
	if found, receiptErr := repository.loadRoleImageReceipt(ctx, tx, current, operation,
		input.IdempotencyKey, intent, &replay); receiptErr != nil {
		return entity.ImageBuild{}, receiptErr
	} else if found {
		if err := committed(tx, ctx); err != nil {
			return entity.ImageBuild{}, err
		}
		return replay, nil
	}
	if err := validateBuildClaim(input.BuildLeaseInput, locked, time.Now().UTC()); err != nil {
		return entity.ImageBuild{}, err
	}
	if err := tx.QueryRow(ctx, queryRoleImagesFailBuild, current.organizationID, locked.ID,
		locked.Build.Version, input.ErrorCode, input.DiagnosticCode, input.DiagnosticSummary).Scan(
		&locked.Build.Version, &locked.Build.Stage, &locked.Build.SafeErrorCode,
		&locked.Build.DiagnosticCode, &locked.Build.DiagnosticSummary,
		&locked.Build.UpdatedAt); err != nil {
		return entity.ImageBuild{}, mapRoleImageWriteError(err)
	}
	locked.Build.LeaseExpiresAt, locked.Build.LeaseTokenSHA256, locked.Build.ClaimantWorkload = nil, "", ""
	locked.Build.AuthorityGeneration = 0
	if err := repository.storeRoleImageReceipt(ctx, tx, current, operation, input.IdempotencyKey,
		intent, "IMAGE_BUILD_FAILURE", locked.Build); err != nil {
		return entity.ImageBuild{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.ImageBuild{}, err
	}
	return locked.Build, nil
}

func (repository *Repository) beginBuildMutation(ctx context.Context, input roleimagerepo.BuildLeaseInput, operation string) (scope, pgx.Tx, lockedBuild, string, string, error) {
	current, err := repository.resolveScope(ctx, input.Principal)
	if err != nil {
		return scope{}, nil, lockedBuild{}, "", "", err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return scope{}, nil, lockedBuild{}, "", "", errs.ErrUnavailable
	}
	locked, err := scanLockedBuild(tx.QueryRow(ctx, queryRoleImagesLockBuild,
		current.organizationID, input.BuildRef))
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		return scope{}, nil, lockedBuild{}, "", "", errs.ErrNotFound
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return scope{}, nil, lockedBuild{}, "", "", errs.ErrUnavailable
	}
	return current, tx, locked, operation, roleImageDigest(input), nil
}

func validateBuildClaim(input roleimagerepo.BuildLeaseInput, locked lockedBuild, now time.Time) error {
	if locked.Build.Version != input.ExpectedVersion || locked.Build.Attempt != input.ExpectedAttempt ||
		locked.Build.Fence != input.ExpectedFence {
		return errs.ErrVersionMismatch
	}
	if locked.Build.ClaimantWorkload != input.Principal.CallerWorkload ||
		input.Principal.CredentialRevision < locked.Build.AuthorityGeneration ||
		locked.Build.LeaseExpiresAt == nil || !now.Before(*locked.Build.LeaseExpiresAt) ||
		!tokenMatches(input.LeaseToken, locked.Build.LeaseTokenSHA256) ||
		!strings.Contains("|MATERIALIZATION|CONTEXT_VALIDATION|BASE_PULL|SOLVING|INSTALLATION|TRUSTED_RUNTIME_FINALIZATION|STAGING_PUSH|PROVENANCE|", "|"+locked.Build.Stage+"|") {
		return errs.ErrForbidden
	}
	return nil
}
