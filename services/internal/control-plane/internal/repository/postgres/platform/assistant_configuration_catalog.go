package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryAssistantConfigurationCatalogSource embeds the lease-owned conversation scope and source
// agent lookup; lease liveness/fence checks are performed by the preceding resolver.
// queryAssistantConfigurationCatalogSource встраивает поиск принадлежащих lease conversation scope
// и source agent; liveness/fence проверяются предшествующим resolver.
//
//go:embed sql/assistant_configuration_catalog__source.sql
var queryAssistantConfigurationCatalogSource string

// queryAssistantConfigurationCatalogAssistants embeds the scope/actor/project-filtered active
// assistant page, including runtime/profile/environment locators and an eleventh pagination row;
// it does not grant mutation authority.
// queryAssistantConfigurationCatalogAssistants встраивает страницу активных assistants с фильтрами
// scope/actor/project, включая runtime/profile/environment locators и одиннадцатую строку для
// пагинации; не выдаёт mutation authority.
//
//go:embed sql/assistant_configuration_catalog__assistants.sql
var queryAssistantConfigurationCatalogAssistants string

// queryAssistantConfigurationCatalogProfiles embeds enabled stable-key runtime profile search with
// version and default provider/model, ordered and limited to eleven rows.
// queryAssistantConfigurationCatalogProfiles встраивает поиск включённых stable-key runtime
// profiles с version и default provider/model, упорядоченный и ограниченный одиннадцатью строками.
//
//go:embed sql/assistant_configuration_catalog__profiles.sql
var queryAssistantConfigurationCatalogProfiles string

// queryAssistantConfigurationCatalogProfile embeds enabled profile-to-provider lookup for
// account/model catalog filtering, returning no credential material.
// queryAssistantConfigurationCatalogProfile встраивает поиск provider включённого profile для
// фильтрации account/model catalog, без credential material.
//
//go:embed sql/assistant_configuration_catalog__profile.sql
var queryAssistantConfigurationCatalogProfile string

// queryAssistantConfigurationCatalogAccounts embeds tenant/provider-authorized account search
// requiring enabled definitions, current credentials and actor visibility; only public
// locators/versions are returned.
// queryAssistantConfigurationCatalogAccounts встраивает tenant/provider поиск авторизованных
// accounts с enabled definitions, current credentials и actor visibility; возвращаются только
// публичные locators/versions.
//
//go:embed sql/assistant_configuration_catalog__accounts.sql
var queryAssistantConfigurationCatalogAccounts string

// queryAssistantConfigurationCatalogImages embeds active scoped recipe/artifact search with source
// visibility, current recipe generation/spec and accepted/promoted/readback evidence gates for
// artifacts; decoder verification still follows.
// queryAssistantConfigurationCatalogImages встраивает поиск активных scoped recipe/artifact с
// source visibility, текущими recipe generation/spec и accepted/promoted/readback evidence gates
// для artifacts; далее выполняется проверка decoder.
//
//go:embed sql/assistant_configuration_catalog__images.sql
var queryAssistantConfigurationCatalogImages string

// ListAssistantConfigurationCatalog runs the whole owner/lease-locked catalog read through the
// existing bounded retry wrapper; each retry starts a new transaction and preserves caller
// selectors.
// ListAssistantConfigurationCatalog выполняет полное owner/lease-locked чтение каталога через
// существующий ограниченный retry wrapper; каждая попытка начинает новую транзакцию и сохраняет
// selectors caller.
func (repository *Repository) ListAssistantConfigurationCatalog(ctx context.Context, principal value.Principal, leaseRef, fence string, generation int64, input entity.AssistantConfigurationCatalogRequest) (entity.AssistantConfigurationCatalogResponse, error) {
	return retryAssistantLockedRead(ctx, func(attemptCtx context.Context) (entity.AssistantConfigurationCatalogResponse, error) {
		return repository.listAssistantConfigurationCatalogOnce(attemptCtx, principal, leaseRef, fence, generation, input)
	})
}

// listAssistantConfigurationCatalogOnce opens RepeatableRead, resolves the exact lease
// fence/generation and fresh root membership, then dispatches only permitted source/helper/target
// catalog paths. It preserves distinct context-bound instructions and view-only metadata, closes
// rows before evidence reads, caps pages at ten plus one and commits before returning; deferred
// bounded rollback may override an error.
// listAssistantConfigurationCatalogOnce открывает RepeatableRead, разрешает точные lease
// fence/generation и свежее root membership, затем направляет только допустимые
// source/helper/target catalog paths. Сохраняет раздельные context-bound instructions и view-only
// metadata, закрывает rows до evidence reads, ограничивает страницу десятью плюс одной и фиксирует
// транзакцию перед возвратом; deferred bounded rollback может заменить ошибку.
func (repository *Repository) listAssistantConfigurationCatalogOnce(ctx context.Context, principal value.Principal, leaseRef, fence string, generation int64, input entity.AssistantConfigurationCatalogRequest) (_ entity.AssistantConfigurationCatalogResponse, resultError error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.AssistantConfigurationCatalogResponse{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return entity.AssistantConfigurationCatalogResponse{}, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultError)
	fenceDigest := sha256.Sum256([]byte(fence))
	var sourceProjectRef, sourceScope, sourceRef string
	err = tx.QueryRow(ctx, queryAssistantSearchResolveLease, pgx.StrictNamedArgs{"organization_id": current.organizationID, "lease_ref": leaseRef, "fence_digest": hex.EncodeToString(fenceDigest[:]), "generation": generation}).Scan(&current.actorRef, &current.actorID, &current.authorityProjectID, &sourceProjectRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AssistantConfigurationCatalogResponse{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.AssistantConfigurationCatalogResponse{}, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	if _, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef); err != nil {
		return entity.AssistantConfigurationCatalogResponse{}, err
	}
	if err := tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, current.actorRef, current.organizationRef).Scan(&current.organizationID, &current.organizationRef, &current.actorID, &current.actorRef, &current.actorName, &current.role); err != nil {
		return entity.AssistantConfigurationCatalogResponse{}, errs.ErrForbidden
	}
	if err := tx.QueryRow(ctx, queryAssistantConfigurationCatalogSource, pgx.StrictNamedArgs{"organization_id": current.organizationID, "lease_ref": leaseRef}).Scan(&sourceScope, &sourceRef); err != nil {
		return entity.AssistantConfigurationCatalogResponse{}, assistantLockedReadError(err, errs.ErrNotFound)
	}
	if sourceScope != "SYSTEM" && sourceScope != "PROJECT" || (sourceScope == "PROJECT" || input.Kind == "CURRENT_CONFIGURATION" || input.Kind == "RECIPIENT_INTEGRATION_GRANTS" || input.Kind == "WORKFLOW_CONFIGURATION" || input.Kind == "WORKFLOW_METADATA" || input.Kind == "AGENT_CONFIGURATION" || input.Kind == "AGENT_RUNTIME_CONFIGURATION") && input.AssistantRef != sourceRef {
		return entity.AssistantConfigurationCatalogResponse{}, errs.ErrForbidden
	}
	if input.Kind == "WORKFLOW_METADATA" {
		result, readErr := repository.assistantResolvedWorkflowMetadataTx(ctx, tx, current, sourceScope, sourceProjectRef, input)
		return finishAssistantConfigurationCatalog(ctx, tx, result, readErr)
	}
	if input.Kind == "AGENT_RUNTIME_CONFIGURATION" {
		result, readErr := repository.assistantResolvedAgentRuntimeConfigurationTx(ctx, tx, current, sourceScope, sourceProjectRef, input)
		return finishAssistantConfigurationCatalog(ctx, tx, result, readErr)
	}
	if input.Kind == "AGENT_CONFIGURATION" {
		result, readErr := repository.assistantAgentConfigurationCatalogTx(ctx, tx, current, leaseRef, input)
		return finishAssistantConfigurationCatalog(ctx, tx, result, readErr)
	}
	if input.Kind == "RECIPIENT_INTEGRATION_GRANTS" {
		result, readErr := repository.assistantRecipientIntegrationCatalogTx(ctx, tx, current, leaseRef, input)
		return finishAssistantConfigurationCatalog(ctx, tx, result, readErr)
	}
	if input.Kind == "WORKFLOW_CONFIGURATION" {
		result, readErr := repository.assistantWorkflowConfigurationCatalogTx(ctx, tx, current, leaseRef, input)
		return finishAssistantConfigurationCatalog(ctx, tx, result, readErr)
	}
	target, err := repository.assistantConfigurationTargetForRead(ctx, tx, current, input.AssistantRef, input.Kind == "CURRENT_CONFIGURATION")
	if err != nil {
		return entity.AssistantConfigurationCatalogResponse{}, err
	}
	result := entity.AssistantConfigurationCatalogResponse{Kind: input.Kind, AssistantRef: input.AssistantRef, ScopeKind: target.scopeKind, OrganizationRef: current.organizationRef, ProjectRef: target.projectRef, AssistantProfileRef: target.profileRef, Entries: []entity.AssistantConfigurationCatalogEntry{}}
	base := entity.AssistantConfigurationCatalogEntry{ScopeKind: target.scopeKind, OrganizationRef: current.organizationRef, ProjectRef: target.projectRef, AssistantProfileRef: target.profileRef}
	var rows pgx.Rows
	switch input.Kind {
	case "PROJECT_INTEGRATION_GRANTS":
		if sourceScope != "PROJECT" || input.AssistantRef != sourceRef || target.scopeKind != "PROJECT" {
			return result, errs.ErrForbidden
		}
		result.ProjectIntegrationGrants, result.NextOffset, err = repository.projectAssistantIntegrationCatalogTx(ctx, tx, current, input)
	case "CURRENT_CONFIGURATION":
		configuration, readErr := repository.assistantCurrentConfigurationTx(ctx, tx, current, input.AssistantRef, sourceScope)
		if readErr != nil {
			return entity.AssistantConfigurationCatalogResponse{}, readErr
		}
		result.CurrentConfiguration = &configuration
	case "ASSISTANTS":
		rows, err = tx.Query(ctx, queryAssistantConfigurationCatalogAssistants, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project": current.authorityProjectID, "source_scope": sourceScope, "source_ref": sourceRef, "query": input.Query, "offset": input.Offset})
	case "RUNTIME_PROFILES":
		rows, err = tx.Query(ctx, queryAssistantConfigurationCatalogProfiles, pgx.StrictNamedArgs{"query": input.Query, "offset": input.Offset})
	case "PROVIDER_ACCOUNTS", "MODELS":
		profileRef := input.RuntimeProfileRef
		if profileRef == "" {
			view, viewErr := repository.getRuntimeConfigurationViewTx(ctx, tx, current, input.AssistantRef)
			if viewErr != nil {
				return result, viewErr
			}
			profileRef = view.Configuration.RuntimeProfileRef
		}
		var provider string
		if err := tx.QueryRow(ctx, queryAssistantConfigurationCatalogProfile, pgx.StrictNamedArgs{"profile_ref": profileRef}).Scan(&provider); errors.Is(err, pgx.ErrNoRows) {
			return result, errs.ErrNotFound
		} else if err != nil {
			return result, errs.ErrUnavailable
		}
		if input.Kind == "PROVIDER_ACCOUNTS" {
			rows, err = tx.Query(ctx, queryAssistantConfigurationCatalogAccounts, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "provider": provider, "query": input.Query, "offset": input.Offset})
			break
		}
		var accountRef string
		if err := repository.requireAccess(ctx, tx, current, "organization.view", organizationTarget(current.organizationRef)); err != nil {
			return result, errs.ErrNotFound
		}
		if err := tx.QueryRow(ctx, queryRuntimeCatalogLockAccounts, current.organizationID, input.AccountRef, provider).Scan(&accountRef); errors.Is(err, pgx.ErrNoRows) {
			return result, errs.ErrNotFound
		} else if err != nil {
			return result, errs.ErrUnavailable
		}
		catalog, catalogErr := readModelCatalogTx(ctx, tx, current, provider, accountRef)
		if catalogErr != nil {
			return result, catalogErr
		}
		var matched int32
		for _, model := range catalog.Models {
			if !model.Available || input.Query != "" && !strings.Contains(strings.ToLower(model.ID), strings.ToLower(input.Query)) {
				continue
			}
			if matched < input.Offset {
				matched++
				continue
			}
			matched++
			entry := base
			entry.Ref, entry.Name, entry.Model, entry.Provider = model.ID, model.ID, model.ID, provider
			entry.CatalogRevision, entry.CatalogDigest = catalog.Revision, catalog.Digest
			entry.ReasoningEfforts, entry.DefaultReasoningEffort = append([]string{}, model.ReasoningEfforts...), model.DefaultReasoningEffort
			result.Entries = append(result.Entries, entry)
			if len(result.Entries) == 11 {
				break
			}
		}
	case "ROLE_IMAGE_RECIPES", "IMAGE_ARTIFACTS":
		if target.scopeKind == "ORGANIZATION" {
			if err := repository.requireOrganizationRuntimeResourceAccess(ctx, tx, current, "image.source.view"); err != nil {
				return result, err
			}
		}
		rows, err = tx.Query(ctx, queryAssistantConfigurationCatalogImages, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project": current.authorityProjectID, "scope_kind": target.scopeKind, "project_ref": target.projectRef, "artifacts": input.Kind == "IMAGE_ARTIFACTS", "query": input.Query, "offset": input.Offset})
	case "ROLE_ENVIRONMENTS":
		if repository.roleImageCatalogEntries == nil {
			return result, errs.ErrUnavailable
		}
		var matched int32
		for _, environment := range repository.roleImageCatalogEntries() {
			if !environment.Available || input.Query != "" && !strings.Contains(strings.ToLower(environment.Key), strings.ToLower(input.Query)) && !strings.Contains(strings.ToLower(environment.NameMessageKey), strings.ToLower(input.Query)) {
				continue
			}
			if matched < input.Offset {
				matched++
				continue
			}
			matched++
			entry := base
			entry.Ref, entry.Name = environment.Key, environment.NameMessageKey
			result.Entries = append(result.Entries, entry)
			if len(result.Entries) == 11 {
				break
			}
		}
	default:
		return result, errs.ErrInvalid
	}
	if err != nil {
		return result, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	if rows != nil {
		for rows.Next() {
			entry := base
			var scanErr error
			switch input.Kind {
			case "ASSISTANTS":
				scanErr = rows.Scan(&entry.Ref, &entry.Name, &entry.Version, &entry.ScopeKind, &entry.ProjectRef, &entry.AssistantProfileRef, &entry.RuntimeEnvironmentRef)
			case "RUNTIME_PROFILES":
				scanErr = rows.Scan(&entry.Ref, &entry.Name, &entry.Provider, &entry.Model, &entry.Version)
			case "PROVIDER_ACCOUNTS":
				scanErr = rows.Scan(&entry.Ref, &entry.Name, &entry.Provider, &entry.Version)
			case "ROLE_IMAGE_RECIPES", "IMAGE_ARTIFACTS":
				scanErr = rows.Scan(&entry.Ref, &entry.Name, &entry.Version, &entry.RecipeGeneration, &entry.Reference, &entry.ManifestDigest, &entry.EnvironmentKey)
			}
			if scanErr != nil {
				rows.Close()
				return result, assistantLockedReadError(scanErr, errs.ErrUnavailable)
			}
			result.Entries = append(result.Entries, entry)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, assistantLockedReadError(err, errs.ErrUnavailable)
		}
	}
	if len(result.Entries) > 10 {
		result.Entries = result.Entries[:10]
		result.NextOffset = input.Offset + 10
	}
	if input.Kind == "IMAGE_ARTIFACTS" {
		// Rows закрыты: evidence читается в том же owner/lease RR snapshot.
		for index := range result.Entries {
			entry := &result.Entries[index]
			artifact, err := scanRoleImageArtifact(tx.QueryRow(ctx, queryRoleImagesGetActiveArtifact, current.organizationID, entry.Ref))
			if err != nil {
				return entity.AssistantConfigurationCatalogResponse{}, assistantLockedReadError(err, errs.ErrUnavailable)
			}
			if err := projectAssistantCatalogArtifact(entry, artifact); err != nil {
				return entity.AssistantConfigurationCatalogResponse{}, err
			}
		}
	}
	return finishAssistantConfigurationCatalog(ctx, tx, result, nil)
}

// projectAssistantCatalogArtifact requires exact catalog/artifact reference, version, recipe
// generation, owner/scope/project, digest/reference and accepted/promoted readback evidence before
// attaching tool inventory metadata; failure leaves the entry unmodified.
// projectAssistantCatalogArtifact требует точные catalog/artifact reference, version, recipe
// generation, owner/scope/project, digest/reference и accepted/promoted readback evidence до
// присоединения tool inventory metadata; ошибка оставляет entry неизменённым.
func projectAssistantCatalogArtifact(entry *entity.AssistantConfigurationCatalogEntry, artifact entity.ImageArtifact) error {
	if entry.Version < 1 || entry.RecipeGeneration < 1 || artifact.Ref != entry.Ref || artifact.Version != uint64(entry.Version) || artifact.RecipeGeneration != uint64(entry.RecipeGeneration) ||
		artifact.OrganizationRef != entry.OrganizationRef || artifact.ScopeKind != entry.ScopeKind || artifact.ProjectRef != entry.ProjectRef ||
		artifact.ManifestDigest != entry.ManifestDigest || artifact.PromotedReference != entry.Reference ||
		artifact.AdmissionVerdict != "ACCEPTED" || artifact.PromotionState != "PROMOTED" || artifact.PromotionReadbackSHA256 == "" {
		return errs.ErrUnavailable
	}
	entry.AdmissionVerdict, entry.PromotionState = artifact.AdmissionVerdict, artifact.PromotionState
	entry.ToolInventory, entry.ToolInventorySHA256 = artifact.ToolInventory, artifact.ToolInventorySHA256
	return nil
}

// finishAssistantConfigurationCatalog preserves a completed reader result and its error without
// committing on read failure; otherwise it commits once using the caller context and maps commit
// failure through the existing locked-read classifier. It owns no rollback, retry or authority
// checks, and a populated result may accompany an error exactly as in the original catalog paths.
// finishAssistantConfigurationCatalog сохраняет результат завершённого reader и его ошибку без
// commit при ошибке чтения; иначе один раз фиксирует транзакцию с caller context и отображает
// commit failure через прежний locked-read classifier. Не владеет rollback, retry или authority
// checks; заполненный результат может сопровождать ошибку, как в исходных catalog paths.
func finishAssistantConfigurationCatalog(ctx context.Context, tx pgx.Tx, result entity.AssistantConfigurationCatalogResponse, readErr error) (entity.AssistantConfigurationCatalogResponse, error) {
	if readErr != nil {
		return result, readErr
	}
	if err := tx.Commit(ctx); err != nil {
		return result, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	return result, nil
}

// assistantConfigurationSHA256 returns the lowercase SHA-256 of already selected projection
// bytes. It preserves exact JSON or nil bytes without re-encoding, validation or authority checks;
// callers enforce publication, snapshot and size constraints before attaching this integrity hash.
// assistantConfigurationSHA256 возвращает строчный SHA-256 уже выбранных projection bytes.
// Сохраняет точные JSON либо nil bytes без повторного кодирования, validation или authority checks;
// callers проверяют publication, snapshot и size constraints до добавления integrity hash.
func assistantConfigurationSHA256(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
