package platform

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const roleImageTransactionAttempts = 3

var errRoleImageTransactionRetry = errors.New("retry role image transaction")

type roleImageRowScanner interface{ Scan(...any) error }

type lockedRecipe struct {
	ID, ProjectID, RoleDefinitionID, ActiveArtifactID string
	Recipe                                            entity.RoleImageRecipe
}

type lockedArtifact struct {
	ID, RecipeID, AdmissionState, AdmissionTokenSHA256, PromotionState string
	PromotionTokenSHA256, AuthorizationTokenSHA256                     string
	PromotionRequestID                                                 string
	AdmissionFence, AdmissionAuthorityGeneration                       uint64
	PromotionFence, PromotionAuthorityGeneration                       uint64
	AdmissionExpiresAt, PromotionExpiresAt, AuthorizationExpiresAt     *time.Time
	Artifact                                                           entity.ImageArtifact
}

type lockedPromotionRequest struct {
	ID, Ref, ExpectedProvenanceSHA256, ManifestDigest, ReceiptSHA256 string
	State, RequestedBy                                               string
	CreatedAt                                                        time.Time
}

type lockedBuild struct {
	ID, RecipeID, ProjectID string
	Specification           entity.RoleImageRecipeInput
	PolicyRevision          uint64
	PolicySHA256            string
	ContractRevision        uint64
	ContractSHA256          string
	Build                   entity.ImageBuild
}

func scanRecipe(row roleImageRowScanner) (entity.RoleImageRecipe, string, error) {
	var recipe entity.RoleImageRecipe
	var specification []byte
	var ownerSubjectRef string
	err := row.Scan(&recipe.Ref, &recipe.ProjectRef, &recipe.RoleDefinitionRef, &recipe.Name,
		&recipe.State, &specification, &recipe.Generation, &recipe.SpecSHA256,
		&recipe.PolicyRevision, &recipe.PolicySHA256, &recipe.RoleRuntimeContractRevision,
		&recipe.RoleRuntimeContractSHA256, &recipe.ActiveImageArtifactRef,
		&recipe.PromotedImageReference, &recipe.Version, &recipe.CreatedAt, &recipe.UpdatedAt,
		&ownerSubjectRef)
	if err != nil {
		return entity.RoleImageRecipe{}, "", err
	}
	if err := json.Unmarshal(specification, &recipe.Input); err != nil {
		return entity.RoleImageRecipe{}, "", errors.New("decode role image recipe specification")
	}
	return recipe, ownerSubjectRef, nil
}

func scanLockedRecipe(row roleImageRowScanner) (lockedRecipe, error) {
	var result lockedRecipe
	var specification []byte
	err := row.Scan(&result.ID, &result.ProjectID, &result.Recipe.ProjectRef,
		&result.RoleDefinitionID, &result.Recipe.RoleDefinitionRef, &result.Recipe.Name,
		&result.Recipe.State, &specification, &result.Recipe.Generation,
		&result.Recipe.SpecSHA256, &result.Recipe.PolicyRevision, &result.Recipe.PolicySHA256,
		&result.Recipe.RoleRuntimeContractRevision, &result.Recipe.RoleRuntimeContractSHA256,
		&result.ActiveArtifactID, &result.Recipe.Version, &result.Recipe.CreatedAt,
		&result.Recipe.UpdatedAt)
	if err != nil {
		return lockedRecipe{}, err
	}
	if err := json.Unmarshal(specification, &result.Recipe.Input); err != nil {
		return lockedRecipe{}, errors.New("decode locked role image recipe specification")
	}
	result.Recipe.NextActions = roleImageActions(result.Recipe, true)
	return result, nil
}

func scanBuild(row roleImageRowScanner) (entity.ImageBuild, error) {
	var result entity.ImageBuild
	var specification []byte
	err := row.Scan(&result.Ref, &result.RecipeRef, &result.SpecSHA256, &result.Stage,
		&result.StagingReference, &result.ManifestDigest, &result.ProvenanceSHA256,
		&result.ImmutableBuildSHA256, &result.SafeErrorCode, &result.DiagnosticCode,
		&result.DiagnosticSummary, &result.LeaseTokenSHA256, &result.ClaimantWorkload,
		&result.Version, &result.RecipeVersion, &result.RecipeGeneration, &result.Fence,
		&result.AuthorityGeneration, &result.Attempt, &result.ProgressPercent,
		&result.LeaseExpiresAt, &result.CreatedAt, &result.UpdatedAt, &specification)
	if err != nil {
		return entity.ImageBuild{}, err
	}
	var recipe entity.RoleImageRecipeInput
	if err := json.Unmarshal(specification, &recipe); err != nil {
		return entity.ImageBuild{}, errors.New("decode image build specification")
	}
	result.Dockerfile = recipe.Dockerfile
	result.SourceAvailable = true
	return result, nil
}

func scanLockedBuild(row roleImageRowScanner) (lockedBuild, error) {
	var result lockedBuild
	var specification []byte
	err := row.Scan(&result.ID, &result.Build.Ref, &result.Build.RecipeRef,
		&result.Build.SpecSHA256, &result.Build.Stage, &result.Build.StagingReference,
		&result.Build.ManifestDigest, &result.Build.ProvenanceSHA256,
		&result.Build.ImmutableBuildSHA256, &result.Build.SafeErrorCode,
		&result.Build.DiagnosticCode, &result.Build.DiagnosticSummary,
		&result.Build.LeaseTokenSHA256, &result.Build.ClaimantWorkload,
		&result.Build.Version, &result.Build.RecipeVersion, &result.Build.RecipeGeneration,
		&result.Build.Fence, &result.Build.AuthorityGeneration, &result.Build.Attempt,
		&result.Build.ProgressPercent, &result.Build.LeaseExpiresAt,
		&result.Build.CreatedAt, &result.Build.UpdatedAt, &result.RecipeID,
		&result.ProjectID, &specification, &result.PolicyRevision, &result.PolicySHA256,
		&result.ContractRevision, &result.ContractSHA256)
	if err != nil {
		return lockedBuild{}, err
	}
	if err := json.Unmarshal(specification, &result.Specification); err != nil {
		return lockedBuild{}, errors.New("decode image build specification")
	}
	return result, nil
}

func scanRoleImageArtifact(row roleImageRowScanner) (entity.ImageArtifact, error) {
	return scanRoleImageArtifactWith(row)
}

func scanRoleImageArtifactWith(row roleImageRowScanner, additionalDestinations ...any) (entity.ImageArtifact, error) {
	var result entity.ImageArtifact
	var specification []byte
	destinations := []any{&result.Ref, &result.RecipeRef, &result.SpecSHA256, &result.BuildRef,
		&result.StagingReference, &result.ManifestDigest, &result.ImmutableBuildSHA256,
		&result.ProvenanceSHA256, &specification, &result.PolicySHA256,
		&result.SBOMSHA256, &result.VulnerabilityEvidenceSHA256,
		&result.AdmissionVerdict, &result.SignatureIdentity, &result.SignatureSHA256,
		&result.AdmissionReceiptSHA256, &result.AdmissionReceiptOCIManifestDigest,
		&result.PromotedReference, &result.PromotionReadbackSHA256,
		&result.RoleRuntimeContractSHA256, &result.Version, &result.RecipeVersion,
		&result.RecipeGeneration, &result.BuildVersion, &result.PolicyRevision,
		&result.AdmissionRevision, &result.RoleRuntimeContractRevision,
		&result.BuildAttempt, &result.PromotedAt, &result.CreatedAt, &result.UpdatedAt}
	destinations = append(destinations, additionalDestinations...)
	err := row.Scan(destinations...)
	if err != nil {
		return entity.ImageArtifact{}, err
	}
	var recipe entity.RoleImageRecipeInput
	if err := json.Unmarshal(specification, &recipe); err != nil {
		return entity.ImageArtifact{}, errors.New("decode image artifact specification")
	}
	result.BaseImageDigest, result.SourceSHA256 = recipe.BaseImageDigest, recipe.SourceSHA256
	result.ContextSHA256, result.BuilderSHA256 = recipe.ContextSHA256, recipe.BuilderSHA256
	result.FrontendSHA256, result.ToolchainSHA256 = recipe.FrontendSHA256, recipe.ToolchainSHA256
	result.Platforms = append([]entity.RoleImagePlatform(nil), recipe.Platforms...)
	result.Tools = append([]entity.RoleImageTool(nil), recipe.Tools...)
	return result, nil
}

func scanLockedArtifact(row roleImageRowScanner) (lockedArtifact, error) {
	var result lockedArtifact
	var specification []byte
	err := row.Scan(&result.ID, &result.Artifact.Ref, &result.Artifact.RecipeRef,
		&result.Artifact.SpecSHA256, &result.Artifact.BuildRef,
		&result.Artifact.StagingReference, &result.Artifact.ManifestDigest,
		&result.Artifact.ImmutableBuildSHA256, &result.Artifact.ProvenanceSHA256,
		&specification, &result.Artifact.PolicySHA256, &result.Artifact.SBOMSHA256,
		&result.Artifact.VulnerabilityEvidenceSHA256, &result.Artifact.AdmissionVerdict,
		&result.Artifact.SignatureIdentity, &result.Artifact.SignatureSHA256,
		&result.Artifact.AdmissionReceiptSHA256,
		&result.Artifact.AdmissionReceiptOCIManifestDigest,
		&result.Artifact.PromotedReference, &result.Artifact.PromotionReadbackSHA256,
		&result.Artifact.RoleRuntimeContractSHA256, &result.Artifact.Version,
		&result.Artifact.RecipeVersion, &result.Artifact.RecipeGeneration,
		&result.Artifact.BuildVersion, &result.Artifact.PolicyRevision,
		&result.Artifact.AdmissionRevision, &result.Artifact.RoleRuntimeContractRevision,
		&result.Artifact.BuildAttempt, &result.Artifact.PromotedAt,
		&result.Artifact.CreatedAt, &result.Artifact.UpdatedAt, &result.AdmissionState,
		&result.AdmissionTokenSHA256, &result.AdmissionFence,
		&result.AdmissionAuthorityGeneration, &result.AdmissionExpiresAt,
		&result.PromotionState, &result.PromotionTokenSHA256, &result.PromotionFence,
		&result.PromotionAuthorityGeneration, &result.PromotionExpiresAt,
		&result.AuthorizationTokenSHA256, &result.AuthorizationExpiresAt,
		&result.RecipeID, &result.PromotionRequestID)
	if err != nil {
		return lockedArtifact{}, err
	}
	var recipe entity.RoleImageRecipeInput
	if err := json.Unmarshal(specification, &recipe); err != nil {
		return lockedArtifact{}, errors.New("decode locked image artifact specification")
	}
	result.Artifact.BaseImageDigest, result.Artifact.SourceSHA256 = recipe.BaseImageDigest, recipe.SourceSHA256
	result.Artifact.ContextSHA256, result.Artifact.BuilderSHA256 = recipe.ContextSHA256, recipe.BuilderSHA256
	result.Artifact.FrontendSHA256, result.Artifact.ToolchainSHA256 = recipe.FrontendSHA256, recipe.ToolchainSHA256
	result.Artifact.Platforms = append([]entity.RoleImagePlatform(nil), recipe.Platforms...)
	result.Artifact.Tools = append([]entity.RoleImageTool(nil), recipe.Tools...)
	return result, nil
}

func scanLockedPromotionRequest(row roleImageRowScanner) (lockedPromotionRequest, error) {
	var result lockedPromotionRequest
	if err := row.Scan(&result.ID, &result.Ref, &result.ExpectedProvenanceSHA256,
		&result.ManifestDigest, &result.ReceiptSHA256, &result.State,
		&result.RequestedBy, &result.CreatedAt); err != nil {
		return lockedPromotionRequest{}, err
	}
	return result, nil
}

func roleImageActions(recipe entity.RoleImageRecipe, canManage bool) []string {
	actions := []string{"OPEN"}
	if !canManage || shippedRoleImage(recipe) {
		return actions
	}
	if recipe.State == "ACTIVE" {
		actions = append(actions, "UPDATE", "REQUEST_BUILD", "ARCHIVE")
	} else {
		actions = append(actions, "RESTORE")
	}
	return actions
}

func roleImageDigest(value any) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func (repository *Repository) roleImageToken(purpose, ref string, attempt uint32, fence, generation uint64, expiresAt time.Time) string {
	payload := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d\x00%d", purpose, ref, attempt, fence, generation, expiresAt.UTC().Unix())
	mac := hmac.New(sha256.New, repository.roleImages.LeaseSigningKey)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (repository *Repository) roleImagePromotionToken(purpose string, artifact entity.ImageArtifact,
	promotionRequestReceiptSHA256 string, fence, generation uint64, expiresAt time.Time) string {
	payload, _ := json.Marshal(struct {
		Purpose, ArtifactRef, RecipeRef, BuildRef, ManifestDigest, ProvenanceSHA256 string
		ImmutableBuildSHA256, SBOMSHA256, VulnerabilityEvidenceSHA256               string
		SignatureIdentity, SignatureSHA256, AdmissionReceiptSHA256                  string
		AdmissionReceiptOCIManifestDigest, PromotionRequestReceiptSHA256            string
		PromotedReference, PolicySHA256, RoleRuntimeContractSHA256                  string
		ArtifactVersion, RecipeVersion, RecipeGeneration, BuildVersion              uint64
		PolicyRevision, AdmissionRevision, RoleRuntimeContractRevision              uint64
		BuildAttempt                                                                uint32
		Fence, AuthorityGeneration                                                  uint64
		ExpiresAtUnix                                                               int64
	}{
		Purpose: purpose, ArtifactRef: artifact.Ref, RecipeRef: artifact.RecipeRef,
		BuildRef: artifact.BuildRef, ManifestDigest: artifact.ManifestDigest,
		ProvenanceSHA256: artifact.ProvenanceSHA256, ImmutableBuildSHA256: artifact.ImmutableBuildSHA256,
		SBOMSHA256: artifact.SBOMSHA256, VulnerabilityEvidenceSHA256: artifact.VulnerabilityEvidenceSHA256,
		SignatureIdentity: artifact.SignatureIdentity, SignatureSHA256: artifact.SignatureSHA256,
		AdmissionReceiptSHA256:            artifact.AdmissionReceiptSHA256,
		AdmissionReceiptOCIManifestDigest: artifact.AdmissionReceiptOCIManifestDigest,
		PromotionRequestReceiptSHA256:     promotionRequestReceiptSHA256,
		PromotedReference:                 repository.roleImages.PromotedRepository + "@" + artifact.ManifestDigest,
		PolicySHA256:                      artifact.PolicySHA256, RoleRuntimeContractSHA256: artifact.RoleRuntimeContractSHA256,
		ArtifactVersion: artifact.Version, RecipeVersion: artifact.RecipeVersion,
		RecipeGeneration: artifact.RecipeGeneration, BuildVersion: artifact.BuildVersion,
		PolicyRevision: artifact.PolicyRevision, AdmissionRevision: artifact.AdmissionRevision,
		RoleRuntimeContractRevision: artifact.RoleRuntimeContractRevision,
		BuildAttempt:                artifact.BuildAttempt, Fence: fence, AuthorityGeneration: generation,
		ExpiresAtUnix: expiresAt.UTC().Unix(),
	})
	mac := hmac.New(sha256.New, repository.roleImages.LeaseSigningKey)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func tokenMatches(token, expectedDigest string) bool {
	actual := tokenDigest(token)
	return len(expectedDigest) == len(actual) && subtle.ConstantTimeCompare([]byte(actual), []byte(expectedDigest)) == 1
}

func exactSHA256(input string) bool {
	if len(input) != 64 {
		return false
	}
	for _, character := range input {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

func exactManifestDigest(input string) bool {
	return strings.HasPrefix(input, "sha256:") && exactSHA256(strings.TrimPrefix(input, "sha256:"))
}

func exactSignatureIdentity(input string) bool {
	if len(input) == 0 || len(input) > 256 {
		return false
	}
	for index, character := range input {
		if character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' || index > 0 && strings.ContainsRune("._:/@+-", character) {
			continue
		}
		return false
	}
	return true
}

func retryRoleImageTransaction[T any](ctx context.Context, operation func() (T, error)) (T, error) {
	var zero T
	for attempt := 0; attempt < roleImageTransactionAttempts; attempt++ {
		result, err := operation()
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, errRoleImageTransactionRetry) && !errors.Is(err, errs.ErrUnavailable) {
			return zero, err
		}
		if attempt+1 == roleImageTransactionAttempts || ctx.Err() != nil {
			if errors.Is(err, errs.ErrUnavailable) {
				return zero, errs.ErrUnavailable
			}
			return zero, errs.ErrConflict
		}
		delay := time.Duration(attempt+1) * 5 * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, errs.ErrUnavailable
		case <-timer.C:
		}
	}
	return zero, errs.ErrUnavailable
}

func (repository *Repository) lockRoleImageIdempotency(ctx context.Context, tx pgx.Tx, current scope,
	operation, key string) error {
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, current.organizationID,
		current.actorID, operation, key); err != nil {
		return errors.Join(errRoleImageTransactionRetry, errs.ErrUnavailable)
	}
	return nil
}

func (repository *Repository) loadRoleImageReceipt(ctx context.Context, tx pgx.Tx, current scope, operation, key, intent string, target any) (bool, error) {
	var storedIntent string
	var payload []byte
	err := tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation,
		current.organizationID, current.actorID, operation, key).Scan(&storedIntent, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, errs.ErrUnavailable
	}
	if storedIntent != intent {
		return false, errs.ErrIdempotencyReuse
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return false, errs.ErrConflict
	}
	return true, nil
}

func (repository *Repository) storeRoleImageReceipt(ctx context.Context, tx pgx.Tx, current scope, operation, key, intent, responseType string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest,
		current.organizationID, current.actorID, operation, key, intent, responseType, payload); err != nil {
		return mapRoleImageWriteError(err)
	}
	return nil
}

func (repository *Repository) auditRoleImage(ctx context.Context, tx pgx.Tx, current scope, projectID, action, resourceKind, resourceRef, summary string) error {
	ref, err := newRef("aud")
	if err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction,
		ref, current.organizationID, projectID, current.actorID, action, resourceKind,
		resourceRef, summary, current.correlationRef); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

func mapRoleImageWriteError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	}
	if roleImageTransactionConflict(err) {
		return errors.Join(errRoleImageTransactionRetry, errs.ErrConflict)
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case "23505":
			return errs.ErrConflict
		case "23503", "23514":
			return errs.ErrInvalid
		}
	}
	return mapWriteError(err)
}

func committed(tx pgx.Tx, ctx context.Context) error {
	if err := tx.Commit(ctx); err != nil {
		return roleImageCommitError(err)
	}
	return nil
}

func roleImageCommitError(err error) error {
	if roleImageTransactionConflict(err) || errors.Is(err, pgx.ErrTxCommitRollback) {
		return errors.Join(errRoleImageTransactionRetry, errs.ErrConflict)
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		return mapRoleImageWriteError(err)
	}
	return errors.Join(errRoleImageTransactionRetry, errs.ErrUnavailable)
}

func roleImageTransactionConflict(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && (pgError.Code == "40001" || pgError.Code == "40P01")
}

func newRoleImageBuildInput(recipe entity.RoleImageRecipe, immutableBuildSHA256 string) entity.RoleImageBuildInput {
	return entity.RoleImageBuildInput{
		RecipeRef: recipe.Ref, RecipeVersion: recipe.Version, RecipeGeneration: recipe.Generation,
		SpecSHA256: recipe.SpecSHA256, BaseImageReference: recipe.Input.BaseImageReference,
		BaseImageDigest: recipe.Input.BaseImageDigest, SourceRef: recipe.Input.SourceRef,
		SourceRevision: recipe.Input.SourceRevision, SourceSHA256: recipe.Input.SourceSHA256,
		ContextRef: recipe.Input.ContextRef, ContextSHA256: recipe.Input.ContextSHA256,
		BuilderSHA256: recipe.Input.BuilderSHA256, FrontendSHA256: recipe.Input.FrontendSHA256,
		InstallationBlock: recipe.Input.InstallationBlock, ToolchainSHA256: recipe.Input.ToolchainSHA256,
		Dockerfile:     recipe.Input.Dockerfile,
		PolicyRevision: recipe.PolicyRevision, PolicySHA256: recipe.PolicySHA256,
		ImmutableBuildSHA256:        immutableBuildSHA256,
		RoleRuntimeContractRevision: recipe.RoleRuntimeContractRevision,
		RoleRuntimeContractSHA256:   recipe.RoleRuntimeContractSHA256,
		Platforms:                   append([]entity.RoleImagePlatform(nil), recipe.Input.Platforms...),
		Packages:                    append([]entity.RoleImagePackage(nil), recipe.Input.Packages...),
		Tools:                       append([]entity.RoleImageTool(nil), recipe.Input.Tools...),
	}
}

func roleImageManageResult(recipe entity.RoleImageRecipe, build *entity.ImageBuild, artifact *entity.ImageArtifact, reused bool) roleimagerepo.ManageResult {
	return roleimagerepo.ManageResult{Recipe: recipe, Build: build, Artifact: artifact, Reused: reused}
}
