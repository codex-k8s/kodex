package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/assistant_configuration_catalog__source.sql
var queryAssistantConfigurationCatalogSource string

//go:embed sql/assistant_configuration_catalog__assistants.sql
var queryAssistantConfigurationCatalogAssistants string

//go:embed sql/assistant_configuration_catalog__profiles.sql
var queryAssistantConfigurationCatalogProfiles string

//go:embed sql/assistant_configuration_catalog__profile.sql
var queryAssistantConfigurationCatalogProfile string

//go:embed sql/assistant_configuration_catalog__accounts.sql
var queryAssistantConfigurationCatalogAccounts string

//go:embed sql/assistant_configuration_catalog__images.sql
var queryAssistantConfigurationCatalogImages string

func (repository *Repository) ListAssistantConfigurationCatalog(ctx context.Context, principal value.Principal, leaseRef, fence string, generation int64, input entity.AssistantConfigurationCatalogRequest) (entity.AssistantConfigurationCatalogResponse, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.AssistantConfigurationCatalogResponse{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return entity.AssistantConfigurationCatalogResponse{}, errs.ErrUnavailable
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
	fenceDigest := sha256.Sum256([]byte(fence))
	var sourceProjectRef, sourceScope, sourceRef string
	err = tx.QueryRow(ctx, queryAssistantSearchResolveLease, pgx.StrictNamedArgs{"organization_id": current.organizationID, "lease_ref": leaseRef, "fence_digest": hex.EncodeToString(fenceDigest[:]), "generation": generation}).Scan(&current.actorRef, &current.actorID, &current.authorityProjectID, &sourceProjectRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AssistantConfigurationCatalogResponse{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.AssistantConfigurationCatalogResponse{}, errs.ErrUnavailable
	}
	if _, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef); err != nil {
		return entity.AssistantConfigurationCatalogResponse{}, err
	}
	if err := tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, current.actorRef, current.organizationRef).Scan(&current.organizationID, &current.organizationRef, &current.actorID, &current.actorRef, &current.actorName, &current.role); err != nil {
		return entity.AssistantConfigurationCatalogResponse{}, errs.ErrForbidden
	}
	if err := tx.QueryRow(ctx, queryAssistantConfigurationCatalogSource, pgx.StrictNamedArgs{"organization_id": current.organizationID, "lease_ref": leaseRef}).Scan(&sourceScope, &sourceRef); err != nil {
		return entity.AssistantConfigurationCatalogResponse{}, errs.ErrNotFound
	}
	if sourceScope != "SYSTEM" && sourceScope != "PROJECT" || (sourceScope == "PROJECT" || input.Kind == "CURRENT_CONFIGURATION") && input.AssistantRef != sourceRef {
		return entity.AssistantConfigurationCatalogResponse{}, errs.ErrForbidden
	}
	target, err := repository.assistantConfigurationTargetForRead(ctx, tx, current, input.AssistantRef, input.Kind == "CURRENT_CONFIGURATION")
	if err != nil {
		return entity.AssistantConfigurationCatalogResponse{}, err
	}
	result := entity.AssistantConfigurationCatalogResponse{Kind: input.Kind, AssistantRef: input.AssistantRef, ScopeKind: target.scopeKind, OrganizationRef: current.organizationRef, ProjectRef: target.projectRef, AssistantProfileRef: target.profileRef, Entries: []entity.AssistantConfigurationCatalogEntry{}}
	base := entity.AssistantConfigurationCatalogEntry{ScopeKind: target.scopeKind, OrganizationRef: current.organizationRef, ProjectRef: target.projectRef, AssistantProfileRef: target.profileRef}
	var rows pgx.Rows
	switch input.Kind {
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
		return result, errs.ErrUnavailable
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
				scanErr = rows.Scan(&entry.Ref, &entry.Name, &entry.Version, &entry.RecipeGeneration, &entry.Reference, &entry.ManifestDigest)
			}
			if scanErr != nil {
				rows.Close()
				return result, errs.ErrUnavailable
			}
			result.Entries = append(result.Entries, entry)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, errs.ErrUnavailable
		}
	}
	if len(result.Entries) > 10 {
		result.Entries = result.Entries[:10]
		result.NextOffset = input.Offset + 10
	}
	if err := tx.Commit(ctx); err != nil {
		return result, errs.ErrUnavailable
	}
	return result, nil
}
