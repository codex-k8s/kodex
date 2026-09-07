package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	revisionservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/revision"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/cfg_copy_insert.sql
var queryCFGCopyInsert string

//go:embed sql/cfg_shipped_integration.sql
var queryCFGShippedIntegration string

//go:embed sql/cfg_archive.sql
var queryCFGArchive string

func cfgLifecycleCommand(kind command.Kind) bool {
	return kind == command.CopyRoleImageConfiguration || kind == command.CopyIntegrationDefinitionConfiguration || kind == command.ArchiveRoleImageConfiguration || kind == command.ArchiveIntegrationDefinitionConfiguration
}

func cfgNextActions(set entity.ManagedConfigurationSet, manage, sourceRead, build, shipped bool) []string {
	result := []string{}
	if set.Kind != "ROLE_IMAGE" && set.Kind != "INTEGRATION_DEFINITION" {
		return result
	}
	if manage && sourceRead && set.CurrentRevision != nil {
		result = append(result, "COPY")
	}
	if manage && build && !set.Archived && set.ManagedBy == "UI" && !shipped {
		result = append(result, "ARCHIVE")
	}
	return result
}

func (repository *Repository) projectCFGActions(ctx context.Context, tx pgx.Tx, current scope, set *entity.ManagedConfigurationSet) error {
	if set == nil || (set.Kind != "ROLE_IMAGE" && set.Kind != "INTEGRATION_DEFINITION") {
		return nil
	}
	resolved, err := repository.resolveManagedSet(ctx, tx, current, command.ManagedConfigurationInput{ConfigurationRef: set.Ref}, set.Kind, false)
	if err != nil {
		return err
	}
	managed := managedSet{ManagedConfigurationSet: *set}
	manageErr := repository.requireManagedSetAccess(ctx, tx, current, managed, "project.manage", "organization.manage")
	if manageErr != nil && !errors.Is(manageErr, errs.ErrForbidden) && !errors.Is(manageErr, errs.ErrNotFound) {
		return manageErr
	}
	sourceRead, build, shipped := true, true, false
	if set.Kind == "ROLE_IMAGE" {
		for permission, flag := range map[string]*bool{"image.source.view": &sourceRead, "image.build": &build} {
			err := repository.managedRoleImageSourceAccess(ctx, tx, current, set.Ref, set.ProjectRef, permission)
			if err != nil && !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
				return err
			}
			*flag = err == nil
		}
		if err := tx.QueryRow(ctx, queryRoleImageManagedShipped, resolved.id, current.organizationID, platformOwnedRoleImageSource).Scan(&shipped); err != nil {
			return errs.ErrUnavailable
		}
	}
	set.NextActions = cfgNextActions(resolved.ManagedConfigurationSet, manageErr == nil, sourceRead, build, shipped)
	return nil
}

// Receipt сохраняет исходную immutable revision, но не возвращает устаревшую доступность set.
func (repository *Repository) refreshCFGReceipt(ctx context.Context, tx pgx.Tx, current scope, result *command.Result) error {
	if result.ManagedConfiguration == nil || (result.ManagedConfiguration.Kind != "ROLE_IMAGE" && result.ManagedConfiguration.Kind != "INTEGRATION_DEFINITION") {
		return nil
	}
	set, err := repository.resolveManagedSet(ctx, tx, current, command.ManagedConfigurationInput{ConfigurationRef: result.ManagedConfiguration.Ref}, result.ManagedConfiguration.Kind, false)
	if err != nil {
		return err
	}
	result.ManagedConfiguration.Archived = set.Archived
	result.ManagedConfiguration.Version = set.Version
	result.ManagedConfiguration.UpdatedAt = set.UpdatedAt
	return nil
}

func (repository *Repository) authorizeCFGLifecycle(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	payload, ok := input.Payload.(command.ManagedConfigurationInput)
	if !ok {
		return errs.ErrInvalid
	}
	kind, action := managedCommand(input.Kind)
	if payload.ConfigurationRef != "" {
		set, err := repository.resolveManagedSet(ctx, tx, current, payload, kind, false)
		if err != nil {
			return err
		}
		if err := repository.requireManagedSetAccess(ctx, tx, current, set, "project.manage", "organization.manage"); err != nil {
			return err
		}
		if kind == "ROLE_IMAGE" {
			permission := "image.source.view"
			if action == "ARCHIVE" {
				permission = "image.build"
			}
			if err := repository.managedRoleImageSourceAccess(ctx, tx, current, set.Ref, set.ProjectRef, permission); err != nil {
				return err
			}
		}
	} else if kind == "ROLE_IMAGE" && action == "COPY_CFG" && payload.RecipeRef != "" {
		target, err := repository.resolveRoleImageAccessTarget(ctx, tx, current, payload.RecipeRef, "")
		if err != nil {
			return err
		}
		if err := repository.requireAccess(ctx, tx, current, "image.source.view", target); err != nil {
			return err
		}
	} else if kind == "INTEGRATION_DEFINITION" && action == "COPY_CFG" && payload.DefinitionKey != "" {
		if err := repository.requireAccess(ctx, tx, current, "organization.manage", organizationTarget(current.organizationRef)); err != nil {
			return err
		}
	} else {
		return errs.ErrInvalid
	}
	if kind == "ROLE_IMAGE" && action == "COPY_CFG" {
		target := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: payload.ProjectRef, ProjectRef: payload.ProjectRef}
		for _, permission := range []string{"project.manage", "image.source.manage", "image.source.view"} {
			if err := repository.requireAccess(ctx, tx, current, permission, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func (repository *Repository) copyCFG(ctx context.Context, tx pgx.Tx, current scope, input command.Command, payload command.ManagedConfigurationInput, kind string) (commandOutcome, error) {
	if input.Mutation.ExpectedVersion == nil || strings.TrimSpace(payload.Name) == "" || len(payload.Name) > 160 || !utf8.ValidString(payload.Name) || strings.ContainsRune(payload.Name, 0) {
		return commandOutcome{}, errs.ErrInvalid
	}
	var format, content, parentID string
	var provenance entity.ManagedConfigurationCopyProvenance
	if payload.ConfigurationRef != "" {
		if payload.RecipeRef != "" || payload.DefinitionKey != "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		source, err := repository.resolveManagedSet(ctx, tx, current, payload, kind, false)
		if err != nil {
			return commandOutcome{}, err
		}
		if source.Version != *input.Mutation.ExpectedVersion {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if source.CurrentRevision == nil {
			return commandOutcome{}, errs.ErrConflict
		}
		format, content, parentID = source.CurrentRevision.ContentFormat, source.CurrentRevision.Content, source.currentRevisionID
		provenance = entity.ManagedConfigurationCopyProvenance{Origin: source.ManagedBy, SourceRef: source.Ref, SourceRevision: source.CurrentRevision.Ref, SourceVersion: source.Version, SourceDigest: source.CurrentRevision.Digest}
		if kind == "ROLE_IMAGE" {
			var shipped bool
			if err := tx.QueryRow(ctx, queryRoleImageManagedShipped, source.id, current.organizationID, platformOwnedRoleImageSource).Scan(&shipped); err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
			if shipped {
				provenance.Origin = "SHIPPED"
				var recipeRef, roleRef string
				var version int64
				if err := tx.QueryRow(ctx, queryRoleImageManagedReadRecipe, source.id, current.organizationID).Scan(&recipeRef, &version, &roleRef); err != nil {
					return commandOutcome{}, errs.ErrUnavailable
				}
				locked, err := scanLockedRecipe(tx.QueryRow(ctx, queryRoleImagesLockRecipe, current.organizationID, recipeRef))
				if err != nil {
					return commandOutcome{}, errs.ErrUnavailable
				}
				content, err = repository.bootstrapRoleImageCopyContent(payload.Name, locked.Recipe)
				if err != nil {
					return commandOutcome{}, err
				}
				format = "JSON"
			} else {
				_, roleRef, selection, err := revisionservice.ParseRoleImage(format, content)
				if err != nil {
					return commandOutcome{}, errs.ErrConflict
				}
				content = roleImageCopyContent(payload.Name, entity.RoleImageRecipe{RoleDefinitionRef: roleRef, Input: entity.RoleImageRecipeInput{EnvironmentKey: selection.EnvironmentKey, PackageKeys: selection.PackageKeys, ToolKeys: selection.ToolKeys, Dockerfile: selection.Dockerfile, InstallationBlock: selection.InstallationBlock}})
				format = "JSON"
			}
		}
	} else if kind == "ROLE_IMAGE" && payload.RecipeRef != "" {
		locked, err := scanLockedRecipe(tx.QueryRow(ctx, queryRoleImagesLockRecipe, current.organizationID, payload.RecipeRef))
		if err != nil {
			return commandOutcome{}, errs.ErrNotFound
		}
		if int64(locked.Recipe.Version) != *input.Mutation.ExpectedVersion {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err := hydrateRoleImageManagedLineage(ctx, tx, current.organizationID, &locked.Recipe); err != nil {
			return commandOutcome{}, err
		}
		origin := "UI"
		if locked.Recipe.ManagedLineage != nil {
			origin = locked.Recipe.ManagedLineage.ManagedBy
		}
		provenance = entity.ManagedConfigurationCopyProvenance{Origin: origin, SourceRef: locked.Recipe.Ref, SourceRevision: strconv.FormatUint(locked.Recipe.Generation, 10), SourceVersion: int64(locked.Recipe.Version), SourceDigest: locked.Recipe.SpecSHA256}
		format = "JSON"
		content, err = repository.bootstrapRoleImageCopyContent(payload.Name, locked.Recipe)
		if err != nil {
			return commandOutcome{}, err
		}
	} else if kind == "INTEGRATION_DEFINITION" && payload.DefinitionKey != "" {
		var version int64
		var definitionVersion, digest, origin string
		if err := tx.QueryRow(ctx, queryCFGShippedIntegration, payload.DefinitionKey).Scan(&version, &definitionVersion, &digest, &origin); err != nil {
			return commandOutcome{}, errs.ErrNotFound
		}
		if version != *input.Mutation.ExpectedVersion || definitionVersion != payload.DefinitionVersion || digest != payload.DefinitionDigest {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		definition, ok := repository.integrationDefinitions[payload.DefinitionKey]
		if !ok || origin != "SHIPPED" || definition.Digest != digest || definition.Metadata.Version != definitionVersion {
			return commandOutcome{}, errs.ErrConflict
		}
		raw, err := json.Marshal(definition)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		format, content = "JSON", string(raw)
		provenance = entity.ManagedConfigurationCopyProvenance{Origin: "SHIPPED", SourceRef: payload.DefinitionKey, SourceRevision: definitionVersion, SourceVersion: version, SourceDigest: digest}
	} else {
		return commandOutcome{}, errs.ErrInvalid
	}
	if provenance.Origin != "SHIPPED" && provenance.Origin != "UI" && provenance.Origin != "GIT" {
		return commandOutcome{}, errs.ErrConflict
	}
	if kind == "INTEGRATION_DEFINITION" {
		format, content = repository.normalizeIntegrationDraft(format, content, "UI")
	}
	ref, err := newRef("mcfg")
	if err != nil {
		return commandOutcome{}, err
	}
	set, err := scanManagedSet(tx.QueryRow(ctx, queryCFGCopyInsert, pgx.StrictNamedArgs{"ref": ref, "organization_id": current.organizationID, "project_ref": payload.ProjectRef, "kind": kind, "name": strings.TrimSpace(payload.Name), "actor_id": current.actorID, "provenance": asJSON(provenance)}))
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	revisionRef, err := newRef("mrev")
	if err != nil {
		return commandOutcome{}, err
	}
	digest := sha256.Sum256([]byte(content))
	revision, err := scanManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationInsertRevision, pgx.StrictNamedArgs{"revision_ref": revisionRef, "organization_id": current.organizationID, "configuration_set_id": set.id, "content_format": format, "content": content, "digest": hex.EncodeToString(digest[:]), "parent_revision_id": parentID, "actor_id": current.actorID}))
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	return managedOutcome(set, &revision.ManagedConfigurationRevision), nil
}

func roleImageCopyContent(name string, recipe entity.RoleImageRecipe) string {
	return string(asJSON(map[string]any{"name": strings.TrimSpace(name), "roleImage": map[string]any{"roleDefinitionRef": recipe.RoleDefinitionRef, "environment": map[string]any{"environmentKey": recipe.Input.EnvironmentKey, "packageKeys": recipe.Input.PackageKeys, "toolKeys": recipe.Input.ToolKeys, "installationBlock": recipe.Input.InstallationBlock, "dockerfile": recipe.Input.Dockerfile}}}))
}

func (repository *Repository) bootstrapRoleImageCopyContent(name string, recipe entity.RoleImageRecipe) (string, error) {
	if shippedRoleImage(recipe) {
		if repository.roleImageBootstrapCopySelection == nil {
			return "", errs.ErrUnavailable
		}
		selection, err := repository.roleImageBootstrapCopySelection(recipe.Input)
		if err != nil {
			return "", err
		}
		recipe.Input.EnvironmentKey = selection.EnvironmentKey
	}
	return roleImageCopyContent(name, recipe), nil
}

func (repository *Repository) archiveCFG(ctx context.Context, tx pgx.Tx, current scope, set managedSet) (commandOutcome, error) {
	if set.ManagedBy != "UI" || set.Archived {
		return commandOutcome{}, errs.ErrConflict
	}
	if set.Kind == "ROLE_IMAGE" {
		var recipeRef, roleRef string
		var version int64
		err := tx.QueryRow(ctx, queryRoleImageManagedReadRecipe, set.id, current.organizationID).Scan(&recipeRef, &version, &roleRef)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if err == nil {
			result, _, _, applyErr := repository.applyRoleImageManage(ctx, tx, current, roleimagerepo.ManageInput{Action: "ARCHIVE", RecipeRef: recipeRef, ProjectRef: set.ProjectRef, Mutation: value.Mutation{ExpectedVersion: &version}})
			err = applyErr
			if err != nil {
				return commandOutcome{}, err
			}
			if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "ROLE_IMAGE_RECIPE_CHANGED", set.ProjectRef, result.Recipe.Ref, "i18n:ROLE_IMAGE_RECIPE_CHANGED", int64(result.Recipe.Version), result.Recipe.State); err != nil {
				return commandOutcome{}, err
			}
		}
	}
	if err := tx.QueryRow(ctx, queryCFGArchive, set.id, set.Version, true).Scan(&set.Version, &set.UpdatedAt); err != nil {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	set.Archived = true
	return managedOutcome(set, nil), nil
}
