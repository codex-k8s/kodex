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

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// roleImageTransactionAttempts bounds the shared role-image transaction retry loop at three
// operation attempts.
// roleImageTransactionAttempts ограничивает общий цикл повторов role-image transaction тремя
// попытками операции.
const roleImageTransactionAttempts = 3

// errRoleImageTransactionRetry marks failures eligible for the existing bounded role-image retry
// path without carrying driver text or query arguments.
// errRoleImageTransactionRetry помечает отказы для существующего ограниченного role-image retry
// path без driver text либо аргументов query.
var errRoleImageTransactionRetry = errors.New("retry role image transaction")

// roleImageRowScanner abstracts the ordered Scan contract shared by pgx rows and row fixtures
// without starting a query or acquiring locks.
// roleImageRowScanner задаёт упорядоченный Scan-контракт общих pgx rows и row fixtures без запуска
// query либо получения блокировок.
type roleImageRowScanner interface {
	// Scan assigns ordered row columns to destinations and returns the row/decoding error.
	// Scan присваивает упорядоченные колонки строки destinations и возвращает row/decoding error.
	Scan(...any) error
}

// lockedRecipe carries database identifiers and the recipe projection returned by the caller's
// locked recipe query; the value itself owns no lock.
// lockedRecipe содержит идентификаторы БД и представление рецепта из locked query caller; само
// значение не владеет блокировкой.
type lockedRecipe struct {
	ID, ProjectID, RoleDefinitionID, ActiveArtifactID string
	Recipe                                            entity.RoleImageRecipe
}

// lockedArtifact carries artifact data plus internal admission/promotion/authorization tokens,
// fences, generations and expiries from a locked owner row.
// lockedArtifact содержит данные артефакта и внутренние admission/promotion/authorization tokens,
// fences, поколения и сроки из locked owner row.
type lockedArtifact struct {
	ID, RecipeID, AdmissionState, AdmissionTokenSHA256, PromotionState string
	PromotionTokenSHA256, AuthorizationTokenSHA256                     string
	PromotionRequestID                                                 string
	AdmissionFence, AdmissionAuthorityGeneration                       uint64
	PromotionFence, PromotionAuthorityGeneration                       uint64
	AdmissionExpiresAt, PromotionExpiresAt, AuthorizationExpiresAt     *time.Time
	Artifact                                                           entity.ImageArtifact
}

// lockedPromotionRequest holds the locked promotion request identity, immutable evidence pins,
// requester and lifecycle state for owner checks.
// lockedPromotionRequest хранит identity locked promotion request, immutable evidence pins,
// инициатора и lifecycle state для проверок владельца.
type lockedPromotionRequest struct {
	ID, Ref, ExpectedProvenanceSHA256, ManifestDigest, ReceiptSHA256 string
	State, RequestedBy                                               string
	CreatedAt                                                        time.Time
}

// lockedBuild pairs locked build identifiers and projection with the saved recipe specification
// and exact policy/runtime-contract pins.
// lockedBuild объединяет locked build identifiers и представление с сохранённой спецификацией
// рецепта и точными policy/runtime-contract pins.
type lockedBuild struct {
	ID, RecipeID, ProjectID string
	Specification           entity.RoleImageRecipeInput
	PolicyRevision          uint64
	PolicySHA256            string
	ContractRevision        uint64
	ContractSHA256          string
	Build                   entity.ImageBuild
}

// scanRecipe scans the public recipe column order and separate owner reference, decoding its JSON
// specification with a fixed decode error and no semantic admission check.
// scanRecipe сканирует порядок публичных колонок рецепта и отдельную ссылку владельца, декодируя
// JSON specification с фиксированной ошибкой без semantic admission check.
func scanRecipe(row roleImageRowScanner) (entity.RoleImageRecipe, string, error) {
	var recipe entity.RoleImageRecipe
	var specification []byte
	var ownerSubjectRef string
	err := row.Scan(&recipe.Ref, &recipe.ProjectRef, &recipe.RoleDefinitionRef, &recipe.Name,
		&recipe.State, &specification, &recipe.Generation, &recipe.SpecSHA256,
		&recipe.PolicyRevision, &recipe.PolicySHA256, &recipe.RoleRuntimeContractRevision,
		&recipe.RoleRuntimeContractSHA256, &recipe.ActiveImageArtifactRef,
		&recipe.PromotedImageReference, &recipe.Version, &recipe.CreatedAt, &recipe.UpdatedAt,
		&ownerSubjectRef, &recipe.ScopeKind, &recipe.OrganizationRef)
	if err != nil {
		return entity.RoleImageRecipe{}, "", err
	}
	if err := json.Unmarshal(specification, &recipe.Input); err != nil {
		return entity.RoleImageRecipe{}, "", errors.New("decode role image recipe specification")
	}
	return recipe, ownerSubjectRef, nil
}

// scanLockedRecipe scans internal recipe identifiers and owner scope, decodes the saved
// specification and assigns management action hints after a successful row scan.
// scanLockedRecipe сканирует внутренние recipe identifiers и owner scope, декодирует сохранённую
// спецификацию и назначает management action hints после успешного row scan.
func scanLockedRecipe(row roleImageRowScanner) (lockedRecipe, error) {
	var result lockedRecipe
	var specification []byte
	err := row.Scan(&result.ID, &result.ProjectID, &result.Recipe.ProjectRef,
		&result.RoleDefinitionID, &result.Recipe.RoleDefinitionRef, &result.Recipe.Name,
		&result.Recipe.State, &specification, &result.Recipe.Generation,
		&result.Recipe.SpecSHA256, &result.Recipe.PolicyRevision, &result.Recipe.PolicySHA256,
		&result.Recipe.RoleRuntimeContractRevision, &result.Recipe.RoleRuntimeContractSHA256,
		&result.ActiveArtifactID, &result.Recipe.Version, &result.Recipe.CreatedAt,
		&result.Recipe.UpdatedAt, &result.Recipe.ScopeKind, &result.Recipe.OrganizationRef)
	if err != nil {
		return lockedRecipe{}, err
	}
	if err := json.Unmarshal(specification, &result.Recipe.Input); err != nil {
		return lockedRecipe{}, errors.New("decode locked role image recipe specification")
	}
	result.Recipe.NextActions = roleImageActions(result.Recipe, true)
	return result, nil
}

// scanBuild scans the public build tuple, decodes saved recipe JSON and copies Dockerfile with
// SourceAvailable true, propagating row failure before projection.
// scanBuild сканирует публичный build tuple, декодирует сохранённый recipe JSON и копирует
// Dockerfile с SourceAvailable=true, передавая row failure до преобразования.
func scanBuild(row roleImageRowScanner) (entity.ImageBuild, error) {
	var result entity.ImageBuild
	var specification []byte
	err := row.Scan(&result.Ref, &result.RecipeRef, &result.SpecSHA256, &result.Stage,
		&result.StagingReference, &result.ManifestDigest, &result.ProvenanceSHA256,
		&result.ImmutableBuildSHA256, &result.SafeErrorCode, &result.DiagnosticCode,
		&result.DiagnosticSummary, &result.LeaseTokenSHA256, &result.ClaimantWorkload,
		&result.Version, &result.RecipeVersion, &result.RecipeGeneration, &result.Fence,
		&result.AuthorityGeneration, &result.Attempt, &result.ProgressPercent,
		&result.LeaseExpiresAt, &result.CreatedAt, &result.UpdatedAt, &specification,
		&result.ScopeKind, &result.OrganizationRef, &result.ProjectRef)
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

// scanLockedBuild scans the locked build and internal recipe/project identifiers with policy and
// contract pins, then decodes its saved specification without modifying transaction ownership.
// scanLockedBuild сканирует locked build и внутренние recipe/project identifiers с policy и
// contract pins, затем декодирует сохранённую спецификацию без изменения владения транзакцией.
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
		&result.ContractRevision, &result.ContractSHA256,
		&result.Build.ScopeKind, &result.Build.OrganizationRef, &result.Build.ProjectRef)
	if err != nil {
		return lockedBuild{}, err
	}
	if err := json.Unmarshal(specification, &result.Specification); err != nil {
		return lockedBuild{}, errors.New("decode image build specification")
	}
	return result, nil
}

// scanRoleImageArtifact uses the common artifact scanner with no trailing destinations, preserving
// the ordinary public artifact row shape.
// scanRoleImageArtifact использует общий artifact scanner без дополнительных destinations,
// сохраняя обычную форму публичной строки артефакта.
func scanRoleImageArtifact(row roleImageRowScanner) (entity.ImageArtifact, error) {
	return scanRoleImageArtifactWith(row)
}

// scanRoleImageArtifactWith scans the exact artifact columns plus optional trailing destinations,
// decodes recipe provenance and copies platform/tool slices before validating tool inventory.
// scanRoleImageArtifactWith сканирует точные колонки артефакта и необязательные дополнительные
// destinations, декодирует provenance рецепта и копирует platform/tool slices до проверки tool
// inventory.
func scanRoleImageArtifactWith(row roleImageRowScanner, additionalDestinations ...any) (entity.ImageArtifact, error) {
	var result entity.ImageArtifact
	var specification []byte
	var inventoryJSON string
	destinations := []any{&result.Ref, &result.RecipeRef, &result.SpecSHA256, &result.BuildRef,
		&result.StagingReference, &result.ManifestDigest, &result.ImmutableBuildSHA256,
		&result.ProvenanceSHA256, &specification, &inventoryJSON, &result.ToolInventorySHA256, &result.PolicySHA256,
		&result.SBOMSHA256, &result.VulnerabilityEvidenceSHA256,
		&result.AdmissionVerdict, &result.SignatureIdentity, &result.SignatureSHA256,
		&result.AdmissionReceiptSHA256, &result.AdmissionReceiptOCIManifestDigest,
		&result.PromotedReference, &result.PromotionReadbackSHA256,
		&result.RoleRuntimeContractSHA256, &result.Version, &result.RecipeVersion,
		&result.RecipeGeneration, &result.BuildVersion, &result.PolicyRevision,
		&result.AdmissionRevision, &result.RoleRuntimeContractRevision,
		&result.BuildAttempt, &result.PromotedAt, &result.CreatedAt, &result.UpdatedAt,
		&result.PromotionState, &result.PromotionRequested,
		&result.ScopeKind, &result.OrganizationRef, &result.ProjectRef}
	destinations = append(destinations, additionalDestinations...)
	err := row.Scan(destinations...)
	if err != nil {
		return entity.ImageArtifact{}, err
	}
	var recipe entity.RoleImageRecipeInput
	if err := json.Unmarshal(specification, &recipe); err != nil {
		return entity.ImageArtifact{}, errors.New("decode image artifact specification")
	}
	copyRoleImageArtifactRecipe(&result, recipe)
	if err := hydrateArtifactToolInventory(&result, inventoryJSON); err != nil {
		return entity.ImageArtifact{}, err
	}
	return result, nil
}

// scanLockedArtifact scans internal claim and artifact pins, restores recipe provenance and
// inventory and derives public promotion state/request flags from the locked row.
// scanLockedArtifact сканирует внутренние claim и artifact pins, восстанавливает provenance
// рецепта и inventory и выводит публичные promotion state/request flags из locked row.
func scanLockedArtifact(row roleImageRowScanner) (lockedArtifact, error) {
	var result lockedArtifact
	var specification []byte
	var inventoryJSON string
	err := row.Scan(&result.ID, &result.Artifact.Ref, &result.Artifact.RecipeRef,
		&result.Artifact.SpecSHA256, &result.Artifact.BuildRef,
		&result.Artifact.StagingReference, &result.Artifact.ManifestDigest,
		&result.Artifact.ImmutableBuildSHA256, &result.Artifact.ProvenanceSHA256,
		&specification, &inventoryJSON, &result.Artifact.ToolInventorySHA256, &result.Artifact.PolicySHA256, &result.Artifact.SBOMSHA256,
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
		&result.RecipeID, &result.PromotionRequestID,
		&result.Artifact.ScopeKind, &result.Artifact.OrganizationRef, &result.Artifact.ProjectRef)
	if err != nil {
		return lockedArtifact{}, err
	}
	var recipe entity.RoleImageRecipeInput
	if err := json.Unmarshal(specification, &recipe); err != nil {
		return lockedArtifact{}, errors.New("decode locked image artifact specification")
	}
	copyRoleImageArtifactRecipe(&result.Artifact, recipe)
	if err := hydrateArtifactToolInventory(&result.Artifact, inventoryJSON); err != nil {
		return lockedArtifact{}, err
	}
	result.Artifact.PromotionState = result.PromotionState
	result.Artifact.PromotionRequested = result.PromotionRequestID != ""
	return result, nil
}

// scanLockedPromotionRequest scans the exact promotion request identity, evidence, receipt, state,
// requester and creation columns, returning a zero value on scan failure.
// scanLockedPromotionRequest сканирует точные колонки identity, evidence, receipt, state,
// инициатора и времени promotion request, возвращая нулевое значение при scan failure.
func scanLockedPromotionRequest(row roleImageRowScanner) (lockedPromotionRequest, error) {
	var result lockedPromotionRequest
	if err := row.Scan(&result.ID, &result.Ref, &result.ExpectedProvenanceSHA256,
		&result.ManifestDigest, &result.ReceiptSHA256, &result.State,
		&result.RequestedBy, &result.CreatedAt); err != nil {
		return lockedPromotionRequest{}, err
	}
	return result, nil
}

// roleImageActions returns OPEN and management hints based on ACTIVE state, omitting mutation
// hints for shipped recipes or callers without management access; hints do not authorize commands.
// roleImageActions возвращает OPEN и management hints по ACTIVE state, исключая mutation hints
// shipped recipes либо callers без management access; подсказки не разрешают команды.
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

// roleImageDigest hashes the existing JSON encoding as lowercase SHA-256 while preserving ignored
// marshal errors and their nil-byte hash fallback.
// roleImageDigest хеширует существующее JSON encoding строчным SHA-256, сохраняя игнорирование
// marshal errors и их nil-byte hash fallback.
func roleImageDigest(value any) string {
	return jsonValueDigest(value)
}

// roleImageToken HMAC-signs the NUL-delimited purpose, reference, attempt, fence, generation and
// UTC expiry seconds with the configured lease key and returns unpadded URL-safe base64.
// roleImageToken подписывает HMAC разделённые NUL purpose, reference, attempt, fence, generation и
// UTC expiry seconds настроенным lease key, возвращая unpadded URL-safe base64.
func (repository *Repository) roleImageToken(purpose, ref string, attempt uint32, fence, generation uint64, expiresAt time.Time) string {
	payload := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d\x00%d", purpose, ref, attempt, fence, generation, expiresAt.UTC().Unix())
	mac := hmac.New(sha256.New, repository.roleImages.LeaseSigningKey)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// roleImagePromotionToken HMAC-signs the fixed ordered JSON promotion snapshot, including exact
// evidence, request receipt, versions, destination, fence/generation and expiry, without
// publishing an image.
// roleImagePromotionToken подписывает HMAC фиксированный упорядоченный JSON promotion snapshot с
// точными evidence, request receipt, версиями, destination, fence/generation и сроком без
// публикации образа.
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

// tokenDigest returns lowercase SHA-256 text for the supplied token bytes without decoding or
// authenticating token claims.
// tokenDigest возвращает строчный SHA-256 text переданных байтов token без декодирования либо
// аутентификации token claims.
func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// tokenMatches hashes token bytes and compares equal-length digest text in constant time,
// rejecting any different expected digest length.
// tokenMatches хеширует байты token и сравнивает digest text одинаковой длины за constant time,
// отклоняя другую длину ожидаемого digest.
func tokenMatches(token, expectedDigest string) bool {
	actual := tokenDigest(token)
	return len(expectedDigest) == len(actual) && subtle.ConstantTimeCompare([]byte(actual), []byte(expectedDigest)) == 1
}

// exactSHA256 requires exactly sixty-four unprefixed lowercase hexadecimal bytes using the shared
// lexical validator without hashing content or granting authority.
// exactSHA256 требует ровно шестьдесят четыре строчных hexadecimal bytes без префикса через общий
// lexical validator без хеширования содержимого либо выдачи полномочий.
func exactSHA256(input string) bool {
	return runtimecontract.ValidSHA256Text(input)
}

// exactManifestDigest requires the literal sha256 prefix and the exact unprefixed lowercase digest
// grammar for its remaining suffix.
// exactManifestDigest требует буквальный префикс sha256 и точную грамматику строчного digest без
// префикса для оставшегося суффикса.
func exactManifestDigest(input string) bool {
	return strings.HasPrefix(input, "sha256:") && exactSHA256(strings.TrimPrefix(input, "sha256:"))
}

// exactSignatureIdentity requires one to two hundred fifty-six bytes of the configured ASCII
// identity alphabet with punctuation allowed only after the first character.
// exactSignatureIdentity требует от одного до двухсот пятидесяти шести байтов заданного
// ASCII-алфавита identity с punctuation только после первого символа.
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

// retryRoleImageTransaction retries only the existing retry sentinel or unavailable failures
// within three attempts and bounded linear waits, discarding partial results; operation callbacks
// retain transaction and rollback responsibility.
// retryRoleImageTransaction повторяет только прежний retry sentinel либо unavailable в пределах
// трёх попыток с ограниченными линейными ожиданиями, отбрасывая частичный результат; callbacks
// сохраняют ответственность за transaction и rollback.
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

// lockRoleImageIdempotency locks the exact organization/actor/operation/key scope through the
// caller transaction and maps lock failure to the existing retry/unavailable markers.
// lockRoleImageIdempotency блокирует точную область organization/actor/operation/key через
// транзакцию caller и отображает отказ в прежние retry/unavailable markers.
func (repository *Repository) lockRoleImageIdempotency(ctx context.Context, tx pgx.Tx, current scope,
	operation, key string) error {
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, current.organizationID,
		current.actorID, operation, key); err != nil {
		return errors.Join(errRoleImageTransactionRetry, errs.ErrUnavailable)
	}
	return nil
}

// loadRoleImageReceipt reads the scoped receipt, distinguishes absence, rejects changed intent and
// decodes the stored response into the supplied target without committing.
// loadRoleImageReceipt читает scoped receipt, отличает отсутствие, отклоняет изменённый intent и
// декодирует сохранённый ответ в переданный target без commit.
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

// storeRoleImageReceipt encodes the response and stores its scoped operation/key/intent/type
// receipt through the supplied transaction, preserving write-error mapping.
// storeRoleImageReceipt кодирует ответ и сохраняет scoped operation/key/intent/type receipt через
// переданную транзакцию, сохраняя write-error mapping.
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

// auditRoleImage allocates an audit reference and inserts the scoped
// actor/resource/action/correlation record with optional project inside the caller transaction.
// auditRoleImage создаёт audit reference и вставляет scoped actor/resource/action/correlation
// record с необязательным проектом внутри транзакции caller.
func (repository *Repository) auditRoleImage(ctx context.Context, tx pgx.Tx, current scope, projectID, action, resourceKind, resourceRef, summary string) error {
	var auditProjectID any
	if projectID != "" {
		auditProjectID = projectID
	}
	ref, err := newRef("aud")
	if err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction,
		ref, current.organizationID, auditProjectID, current.actorID, action, resourceKind,
		resourceRef, summary, current.correlationRef); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// mapRoleImageWriteError maps no-row, serialization/deadlock and selected constraint failures to
// stable domain/retry errors before falling back to the common write mapper.
// mapRoleImageWriteError переводит no-row, serialization/deadlock и выбранные constraint failures
// в устойчивые domain/retry errors до общего write mapper.
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

// committed commits the supplied transaction with caller context and applies the role-image commit
// classifier only when Commit fails.
// committed выполняет commit переданной транзакции с caller context и применяет role-image commit
// classifier только при отказе Commit.
func committed(tx pgx.Tx, ctx context.Context) error {
	if err := tx.Commit(ctx); err != nil {
		return roleImageCommitError(err)
	}
	return nil
}

// roleImageCommitError marks known aborts as retry/conflict, maps typed PostgreSQL failures and
// preserves the existing retry/unavailable result for other commit errors without proving whether
// commit occurred.
// roleImageCommitError помечает известные aborts как retry/conflict, преобразует типизированные
// PostgreSQL failures и сохраняет прежний retry/unavailable для прочих commit errors без
// доказательства факта commit.
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

// roleImageTransactionConflict recognizes only PostgreSQL serialization failure and deadlock
// SQLSTATE values through wrapped typed driver errors.
// roleImageTransactionConflict распознаёт только PostgreSQL serialization failure и deadlock
// SQLSTATE через обёрнутые типизированные driver errors.
func roleImageTransactionConflict(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && (pgError.Code == "40001" || pgError.Code == "40P01")
}

// newRoleImageBuildInput copies exact recipe/owner/version/policy/contract and immutable build
// pins into the worker input with independent platform, package and tool slices.
// newRoleImageBuildInput копирует точные recipe/owner/version/policy/contract и immutable build
// pins во вход worker с независимыми platform, package и tool slices.
func newRoleImageBuildInput(recipe entity.RoleImageRecipe, immutableBuildSHA256 string) entity.RoleImageBuildInput {
	return entity.RoleImageBuildInput{
		ScopeKind: recipe.ScopeKind, OrganizationRef: recipe.OrganizationRef, ProjectRef: recipe.ProjectRef,
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

// roleImageManageResult packages the supplied recipe, optional build/artifact and reuse flag into
// the domain manage result without performing persistence.
// roleImageManageResult объединяет переданные recipe, необязательные build/artifact и reuse flag в
// доменный manage result без операций хранения.
func roleImageManageResult(recipe entity.RoleImageRecipe, build *entity.ImageBuild, artifact *entity.ImageArtifact, reused bool) roleimagerepo.ManageResult {
	return roleimagerepo.ManageResult{Recipe: recipe, Build: build, Artifact: artifact, Reused: reused}
}

// copyRoleImageArtifactRecipe copies only the saved recipe provenance fields into an artifact and
// clones platform/tool slices with the existing empty-to-nil behavior; other artifact fields stay intact.
// copyRoleImageArtifactRecipe копирует только сохранённые provenance fields рецепта в артефакт и
// клонирует platform/tool slices с прежним empty-to-nil поведением; остальные поля артефакта сохраняются.
func copyRoleImageArtifactRecipe(artifact *entity.ImageArtifact, recipe entity.RoleImageRecipeInput) {
	artifact.BaseImageDigest, artifact.SourceSHA256 = recipe.BaseImageDigest, recipe.SourceSHA256
	artifact.ContextSHA256, artifact.BuilderSHA256 = recipe.ContextSHA256, recipe.BuilderSHA256
	artifact.FrontendSHA256, artifact.ToolchainSHA256 = recipe.FrontendSHA256, recipe.ToolchainSHA256
	artifact.Platforms = append([]entity.RoleImagePlatform(nil), recipe.Platforms...)
	artifact.DeclaredTools = append([]entity.RoleImageTool(nil), recipe.Tools...)
}
