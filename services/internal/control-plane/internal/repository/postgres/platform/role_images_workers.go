package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// buildClaimReceipt stores the exact build/input snapshot, fence, credential generation and expiry
// needed to reconstruct the original deterministic lease token on replay; it is not fresh liveness
// proof.
// buildClaimReceipt сохраняет точные build/input snapshot, fence, credential generation и expiry
// для восстановления исходного детерминированного lease token при replay; не является свежим
// liveness proof.
type buildClaimReceipt struct {
	Build               entity.ImageBuild
	Input               entity.RoleImageBuildInput
	Fence               uint64
	AuthorityGeneration uint64
	LeaseExpiresAt      time.Time
}

// buildExpiryOutcome records one build reference, updated version and EXPIRED/DEAD_LETTER stage
// after an owner expiry transition.
// buildExpiryOutcome фиксирует одну build reference, обновлённую version и стадию
// EXPIRED/DEAD_LETTER после owner expiry transition.
type buildExpiryOutcome struct {
	BuildRef string
	Version  uint64
	Stage    string
}

// buildExpiryReceipt stores an explicit nonempty batch of expiry-only changes so replay cannot
// confuse no new claim with an empty build snapshot.
// buildExpiryReceipt сохраняет явную непустую пачку expiry-only изменений, исключая смешение
// отсутствия нового claim с пустым build snapshot.
type buildExpiryReceipt struct {
	Changes []buildExpiryOutcome
}

// admissionClaimReceipt pins the attempt, accepted-risk/source evidence, expiry/rejection side
// effects and artifact claim coordinates required by admission replay.
// admissionClaimReceipt закрепляет attempt, accepted-risk/source evidence, побочные
// expiry/rejection изменения и artifact claim coordinates для admission replay.
type admissionClaimReceipt struct {
	AdmissionAttemptRef                                        string
	AdmissionAttempt                                           uint32
	RiskAcceptanceJSON, RiskAcceptanceSHA256                   string
	SourceAdmissionReceiptSHA256, SourceEvidenceManifestDigest string
	SourceAdmissionRevision                                    uint64
	Expired                                                    []entity.RoleImageAdmissionFailure
	Rejected                                                   []entity.ImageArtifact
	Artifact                                                   entity.ImageArtifact
	Fence                                                      uint64
	AuthorityGeneration                                        uint64
	ClaimExpiresAt                                             time.Time
}

// promotionClaimReceipt stores the exact artifact/request receipt commitment, fence, authority
// generation and claim deadline used for promotion replay.
// promotionClaimReceipt сохраняет точные artifact/request receipt commitment, fence, authority
// generation и claim deadline для promotion replay.
type promotionClaimReceipt struct {
	Artifact                      entity.ImageArtifact
	PromotionRequestReceiptSHA256 string
	Fence                         uint64
	AuthorityGeneration           uint64
	ClaimExpiresAt                time.Time
}

// promotionAuthorizationReceipt stores the exact promotion artifact/request receipt and fenced
// authority lifetime used to reconstruct authorization replay.
// promotionAuthorizationReceipt сохраняет точные promotion artifact/request receipt и ограниченный
// срок fenced authority для восстановления authorization replay.
type promotionAuthorizationReceipt struct {
	Artifact                      entity.ImageArtifact
	PromotionRequestReceiptSHA256 string
	Fence                         uint64
	AuthorityGeneration           uint64
	AuthorizationExpiresAt        time.Time
}

// ClaimBuild runs the complete build claim through the existing bounded role-image transaction
// retry policy rather than retrying an individual SQL statement.
// ClaimBuild выполняет полный build claim через прежнюю ограниченную role-image transaction retry
// policy вместо повтора отдельного SQL statement.
func (repository *Repository) ClaimBuild(ctx context.Context, principal value.Principal, key string) (entity.ImageBuildClaim, error) {
	return retryRoleImageTransaction(ctx, func() (entity.ImageBuildClaim, error) {
		return repository.claimBuild(ctx, principal, key)
	})
}

// claimBuild locks actor/key idempotency, replays an explicit claim or expiry-only outcome,
// otherwise expires old leases and claims one bounded-attempt candidate with fresh token/fence
// before storing receipt and event in the same commit.
// claimBuild блокирует actor/key idempotency, повторяет явный claim либо expiry-only outcome,
// иначе завершает прежние leases и забирает одного кандидата с ограничением attempts и свежими
// token/fence, сохраняя receipt и event одним commit.
func (repository *Repository) claimBuild(ctx context.Context, principal value.Principal, key string) (entity.ImageBuildClaim, error) {
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
	if err := repository.lockRoleImageIdempotency(ctx, tx, current, operation, key); err != nil {
		return entity.ImageBuildClaim{}, err
	}
	if replay, expiryOnly, found, receiptErr := repository.loadBuildClaimOutcome(ctx, tx, current, operation, key, intent); receiptErr != nil {
		return entity.ImageBuildClaim{}, receiptErr
	} else if found {
		if err := committed(tx, ctx); err != nil {
			return entity.ImageBuildClaim{}, err
		}
		if expiryOnly {
			return entity.ImageBuildClaim{}, errs.ErrNotFound
		}
		return repository.buildClaimFromReceipt(replay), nil
	}
	var buildID, buildRef, recipeID, stage string
	expired, err := repository.expireRoleImageBuilds(ctx, tx, current)
	if err != nil {
		return entity.ImageBuildClaim{}, err
	}
	var version, fence uint64
	var attempt, maximumAttempts uint32
	err = tx.QueryRow(ctx, queryRoleImagesClaimBuildCandidate, current.organizationID).Scan(
		&buildID, &buildRef, &version, &attempt, &maximumAttempts, &fence, &recipeID, &stage)
	if errors.Is(err, pgx.ErrNoRows) {
		if len(expired) > 0 {
			if err := repository.storeRoleImageReceipt(ctx, tx, current, operation, key, intent,
				"IMAGE_BUILD_EXPIRY_OUTCOME", buildExpiryReceipt{Changes: expired}); err != nil {
				return entity.ImageBuildClaim{}, err
			}
			if err := committed(tx, ctx); err != nil {
				return entity.ImageBuildClaim{}, err
			}
		}
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
	var projectID string
	var specification []byte
	var immutable string
	err = tx.QueryRow(ctx, queryRoleImagesGetBuildInput, current.organizationID, buildID).Scan(
		&recipe.Ref, &projectID, &recipe.ProjectRef,
		&recipe.Version, &recipe.Generation, &recipe.SpecSHA256,
		&specification, &immutable, &recipe.PolicyRevision, &recipe.PolicySHA256,
		&recipe.RoleRuntimeContractRevision, &recipe.RoleRuntimeContractSHA256,
		&recipe.ScopeKind, &recipe.OrganizationRef)
	if err != nil || decodeJSON(specification, &recipe.Input) != nil {
		return entity.ImageBuildClaim{}, errs.ErrConflict
	}
	if !repository.roleImageRecipePolicyCurrent(recipe) {
		return entity.ImageBuildClaim{}, errs.ErrConflict
	}
	receipt := buildClaimReceipt{Build: build, Input: newRoleImageBuildInput(recipe, immutable),
		Fence: fence, AuthorityGeneration: principal.CredentialRevision, LeaseExpiresAt: expiresAt}
	if err := repository.storeRoleImageReceipt(ctx, tx, current, operation, key, intent,
		"IMAGE_BUILD_CLAIM", receipt); err != nil {
		return entity.ImageBuildClaim{}, err
	}
	if err := repository.emitRoleImageBuildChanged(ctx, tx, current, lockedBuild{
		ProjectID: projectID,
		Build:     build,
	}); err != nil {
		return entity.ImageBuildClaim{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.ImageBuildClaim{}, err
	}
	return repository.buildClaimFromReceipt(receipt), nil
}

// expireRoleImageBuilds expires up to32 eligible leases, clears old claim authority and increments
// fences in SQL, then emits audit/events and returns exact changed outcomes in the caller
// transaction, including DEAD_LETTER without a new claim.
// expireRoleImageBuilds завершает до32 допустимых leases, очищает прежнюю claim authority и
// увеличивает fences в SQL, затем пишет audit/events и возвращает точные outcomes в транзакции
// caller, включая DEAD_LETTER без нового claim.
func (repository *Repository) expireRoleImageBuilds(ctx context.Context, tx pgx.Tx, current scope) ([]buildExpiryOutcome, error) {
	rows, err := tx.Query(ctx, queryRoleImagesExpireBuilds, current.organizationID)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	var expired []lockedBuild
	for rows.Next() {
		var build lockedBuild
		if err := rows.Scan(&build.Build.Ref, &build.Build.RecipeRef, &build.Build.Version, &build.Build.Stage, &build.ProjectID, &build.Build.ScopeKind); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		expired = append(expired, build)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	outcomes := make([]buildExpiryOutcome, 0, len(expired))
	for _, build := range expired {
		if err := repository.auditRoleImage(ctx, tx, current, build.ProjectID,
			"platform.role-images.builds.expire", "IMAGE_BUILD", build.Build.Ref,
			"i18n:ROLE_IMAGE_BUILD_LEASE_EXPIRED"); err != nil {
			return nil, err
		}
		if err := repository.emitRoleImageBuildChanged(ctx, tx, current, build); err != nil {
			return nil, err
		}
		outcomes = append(outcomes, buildExpiryOutcome{BuildRef: build.Build.Ref, Version: build.Build.Version, Stage: build.Build.Stage})
	}
	return outcomes, nil
}

// loadBuildClaimOutcome checks exact stored intent and explicit response kind, validating either
// required claim identity/lifetime or one-to32 expiry outcomes; unknown or malformed receipts fail
// as conflict.
// loadBuildClaimOutcome проверяет точные сохранённые intent и response kind, проверяя обязательные
// claim identity/lifetime либо от одного до32 expiry outcomes; неизвестные и неверные receipts
// дают conflict.
func (repository *Repository) loadBuildClaimOutcome(ctx context.Context, tx pgx.Tx, current scope,
	operation, key, intent string,
) (buildClaimReceipt, bool, bool, error) {
	var storedIntent, responseType string
	var payload []byte
	err := tx.QueryRow(ctx, queryRoleImagesClaimOutcomeReceipt, current.organizationID,
		current.actorID, operation, key).Scan(&storedIntent, &responseType, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return buildClaimReceipt{}, false, false, nil
	}
	if err != nil {
		return buildClaimReceipt{}, false, false, errs.ErrUnavailable
	}
	if storedIntent != intent {
		return buildClaimReceipt{}, false, false, errs.ErrIdempotencyReuse
	}
	switch responseType {
	case "IMAGE_BUILD_CLAIM":
		var receipt buildClaimReceipt
		if json.Unmarshal(payload, &receipt) != nil || receipt.Build.Ref == "" || receipt.Build.Attempt == 0 ||
			receipt.Fence == 0 || receipt.LeaseExpiresAt.IsZero() {
			return buildClaimReceipt{}, false, false, errs.ErrConflict
		}
		return receipt, false, true, nil
	case "IMAGE_BUILD_EXPIRY_OUTCOME":
		var receipt buildExpiryReceipt
		if json.Unmarshal(payload, &receipt) != nil || len(receipt.Changes) == 0 || len(receipt.Changes) > 32 {
			return buildClaimReceipt{}, false, false, errs.ErrConflict
		}
		for _, change := range receipt.Changes {
			if change.BuildRef == "" || change.Version == 0 || !contains([]string{"EXPIRED", "DEAD_LETTER"}, change.Stage) {
				return buildClaimReceipt{}, false, false, errs.ErrConflict
			}
		}
		return buildClaimReceipt{}, true, true, nil
	default:
		return buildClaimReceipt{}, false, false, errs.ErrConflict
	}
}

// buildClaimFromReceipt reconstructs the deterministic token and public claim from saved receipt
// coordinates without issuing a new attempt, extending expiry or checking current lease liveness.
// buildClaimFromReceipt восстанавливает детерминированные token и публичный claim по сохранённым
// receipt coordinates без новой attempt, продления expiry либо проверки текущей liveness lease.
func (repository *Repository) buildClaimFromReceipt(receipt buildClaimReceipt) entity.ImageBuildClaim {
	token := repository.roleImageToken("image-build", receipt.Build.Ref, receipt.Build.Attempt,
		receipt.Fence, receipt.AuthorityGeneration, receipt.LeaseExpiresAt)
	return entity.ImageBuildClaim{Build: receipt.Build, Input: receipt.Input, LeaseToken: token,
		Fence: receipt.Fence, AuthorityGeneration: receipt.AuthorityGeneration,
		LeaseExpiresAt: receipt.LeaseExpiresAt}
}

// RenewBuild locks the build and honors an exact renewal replay before live-claim validation;
// otherwise rotates token/credential generation and expiry, stores the renewed immutable input and
// commits without emitting a build event here.
// RenewBuild блокирует build и принимает точный renewal replay до live-claim validation; иначе
// меняет token/credential generation и expiry, сохраняет renewed immutable input и фиксирует без
// build event в этом методе.
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
			ScopeKind: locked.Build.ScopeKind, OrganizationRef: locked.Build.OrganizationRef, ProjectRef: locked.Build.ProjectRef,
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

// ReportBuildProgress honors exact progress replay, validates the live fenced claim and stores
// updated stage/percent, receipt and build-changed event in the caller-owned serializable
// transaction.
// ReportBuildProgress принимает точный progress replay, проверяет живой fenced claim и сохраняет
// обновлённые stage/percent, receipt и build-changed event в serializable транзакции владельца.
func (repository *Repository) ReportBuildProgress(ctx context.Context, input roleimagerepo.BuildProgressInput) (entity.ImageBuild, error) {
	current, tx, locked, operation, intent, err := repository.beginBuildMutation(ctx, input.BuildLeaseInput, "platform.role-images.builds.progress")
	if err != nil {
		return entity.ImageBuild{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var replay entity.ImageBuild
	if found, receiptErr := repository.loadRoleImageReceipt(ctx, tx, current, operation,
		input.IdempotencyKey, roleImageBuildProgressDigest(input), &replay); receiptErr != nil {
		return entity.ImageBuild{}, receiptErr
	} else if found {
		if err := committed(tx, ctx); err != nil {
			return entity.ImageBuild{}, err
		}
		return replay, nil
	}
	intent = roleImageBuildProgressDigest(input)
	if err := validateBuildClaim(input.BuildLeaseInput, locked, time.Now().UTC()); err != nil {
		return entity.ImageBuild{}, err
	}
	if err := tx.QueryRow(ctx, queryRoleImagesProgressBuild, current.organizationID, locked.ID,
		locked.Build.Version, input.Stage, input.ProgressPercent).Scan(&locked.Build.Version,
		&locked.Build.Stage, &locked.Build.ProgressPercent, &locked.Build.UpdatedAt); err != nil {
		return entity.ImageBuild{}, mapRoleImageWriteError(err)
	}
	return repository.finishBuildMutation(ctx, tx, current, locked, operation, input.IdempotencyKey, intent, "IMAGE_BUILD_PROGRESS")
}

// CompleteBuild replays the saved build/artifact pair or verifies live claim plus exact staging
// and immutable input, then completes the build, clears its lease, inserts the bound artifact and
// commits audit/receipt/event atomically.
// CompleteBuild повторяет сохранённую пару build/artifact либо проверяет live claim, точные
// staging и immutable input, затем завершает build, очищает lease, добавляет связанный artifact и
// атомарно фиксирует audit/receipt/event.
func (repository *Repository) CompleteBuild(ctx context.Context, input roleimagerepo.BuildCompletionInput) (entity.ImageBuild, entity.ImageArtifact, error) {
	current, tx, locked, operation, _, err := repository.beginBuildMutation(ctx, input.BuildLeaseInput, "platform.role-images.builds.complete")
	if err != nil {
		return entity.ImageBuild{}, entity.ImageArtifact{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	intent := roleImageBuildCompletionDigest(input)
	// completionReceipt stores both completed build and artifact as one replay result so retry cannot
	// create a second artifact or return an incomplete completion pair.
	// completionReceipt сохраняет завершённые build и artifact единым replay result, исключая второй
	// artifact при повторе либо неполную completion pair.
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
	if err := repository.emitRoleImageBuildChanged(ctx, tx, current, locked); err != nil {
		return entity.ImageBuild{}, entity.ImageArtifact{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.ImageBuild{}, entity.ImageArtifact{}, err
	}
	return locked.Build, artifact, nil
}

// FailBuild replays a saved failure or validates the live claim, applies SQL failure state, clears
// the local lease coordinates and commits the exact updated build with receipt/event.
// FailBuild повторяет сохранённый failure либо проверяет live claim, применяет SQL failure state,
// очищает локальные lease coordinates и фиксирует точный обновлённый build с receipt/event.
func (repository *Repository) FailBuild(ctx context.Context, input roleimagerepo.BuildFailureInput) (entity.ImageBuild, error) {
	current, tx, locked, operation, _, err := repository.beginBuildMutation(ctx, input.BuildLeaseInput, "platform.role-images.builds.fail")
	if err != nil {
		return entity.ImageBuild{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	intent := roleImageBuildFailureDigest(input)
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
	return repository.finishBuildMutation(ctx, tx, current, locked, operation, input.IdempotencyKey, intent, "IMAGE_BUILD_FAILURE")
}

// beginBuildMutation resolves trusted scope, opens a serializable transaction and locks the exact
// tenant/build, rolling back on missing/read failure; successful callers own rollback/commit and
// use the correlation-normalized lease digest.
// beginBuildMutation разрешает доверенную область, открывает serializable транзакцию и блокирует
// точные tenant/build с rollback при отсутствии либо ошибке чтения; успешные callers владеют
// rollback/commit и используют lease digest без correlation.
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
	return current, tx, locked, operation, roleImageBuildLeaseDigest(input), nil
}

// emitRoleImageBuildChanged resolves the project locator and requires valid scope plus
// recipe/version pins before emitting the build-stage snapshot through the caller transaction.
// emitRoleImageBuildChanged разрешает project locator и требует допустимые scope и recipe/version
// pins до записи build-stage snapshot через транзакцию caller.
func (repository *Repository) emitRoleImageBuildChanged(ctx context.Context, tx pgx.Tx, current scope, locked lockedBuild) error {
	projectRef := projectRefByID(ctx, tx, locked.ProjectID)
	if (locked.Build.ScopeKind != "PROJECT" && locked.Build.ScopeKind != "ORGANIZATION") ||
		(locked.Build.ScopeKind == "PROJECT" && projectRef == "") || locked.Build.RecipeRef == "" || locked.Build.Version == 0 {
		return errs.ErrUnavailable
	}
	return repository.emitPlatformEventSnapshot(ctx, tx, current, "ROLE_IMAGE_RECIPE_CHANGED",
		projectRef, locked.Build.RecipeRef, "i18n:ROLE_IMAGE_RECIPE_CHANGED",
		int64(locked.Build.Version), locked.Build.Stage)
}

// validateBuildClaim distinguishes version/attempt/fence mismatch from forbidden workload,
// credential generation, missing/expired lease, token or stage; it uses the existing
// pipe-delimited stage matching expression.
// validateBuildClaim различает version/attempt/fence mismatch и forbidden workload, credential
// generation, отсутствующий/истёкший lease, token либо stage; использует прежнее pipe-delimited
// выражение проверки стадии.
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

// finishBuildMutation stores the already-applied build result, emits its snapshot and commits
// in the original order; any failure returns a zero build. Specialized callers retain claim
// validation, SQL transition, receipt kind and their existing deferred rollback.
// finishBuildMutation сохраняет уже применённый build result, записывает snapshot и выполняет
// commit в прежнем порядке; любой отказ возвращает нулевой build. Специализированные callers
// сохраняют claim validation, SQL transition, receipt kind и прежний deferred rollback.
func (repository *Repository) finishBuildMutation(ctx context.Context, tx pgx.Tx, current scope, locked lockedBuild, operation, key, intent, responseType string) (entity.ImageBuild, error) {
	if err := repository.storeRoleImageReceipt(ctx, tx, current, operation, key, intent, responseType, locked.Build); err != nil {
		return entity.ImageBuild{}, err
	}
	if err := repository.emitRoleImageBuildChanged(ctx, tx, current, locked); err != nil {
		return entity.ImageBuild{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.ImageBuild{}, err
	}
	return locked.Build, nil
}
