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

// queryCFGCopyInsert inserts a new UI-owned managed set in the target project or organization with exact copy
// provenance and current actor, returning the canonical set projection without starting a build.
// queryCFGCopyInsert вставляет новый UI-owned managed set целевых project либо organization с точными copy
// provenance и текущим actor, возвращая каноническое set projection без запуска build.
//
//go:embed sql/cfg_copy_insert.sql
var queryCFGCopyInsert string

// queryCFGShippedIntegration reads the active shipped integration definition version/digest/origin
// needed to bind an exact bootstrap copy, rather than trusting a client package.
// queryCFGShippedIntegration читает active shipped integration definition version/digest/origin
// для точной bootstrap copy вместо доверия клиентскому package.
//
//go:embed sql/cfg_shipped_integration.sql
var queryCFGShippedIntegration string

// queryCFGArchive changes the exact set archive flag under version OCC and returns new
// version/update metadata without deleting revision history.
// queryCFGArchive изменяет archive flag точного set с version OCC и возвращает новые
// version/update metadata без удаления revision history.
//
//go:embed sql/cfg_archive.sql
var queryCFGArchive string

// cfgLifecycleCommand recognizes only the four specialized role-image/integration copy/archive
// commands, leaving other managed actions to their own registry.
// cfgLifecycleCommand распознаёт только четыре специализированные role-image/integration
// copy/archive commands, оставляя остальные managed actions собственному registry.
func cfgLifecycleCommand(kind command.Kind) bool {
	return kind == command.CopyRoleImageConfiguration || kind == command.CopyIntegrationDefinitionConfiguration || kind == command.ArchiveRoleImageConfiguration || kind == command.ArchiveIntegrationDefinitionConfiguration
}

// cfgNextActions returns a fresh ordered COPY/ARCHIVE hint list only for role-image/integration
// sets: COPY requires manage/source/current revision; ARCHIVE independently requires manage/build,
// UI ownership and nonarchive/nonshipped state. Hints do not grant mutation authority.
// cfgNextActions возвращает новый упорядоченный COPY/ARCHIVE hint list только для
// role-image/integration sets: COPY требует manage/source/current revision; ARCHIVE независимо
// требует manage/build, UI ownership и nonarchive/nonshipped state. Hints не выдают mutation authority.
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

// projectCFGActions projects current generic lifecycle hints after resolving set/source/build
// permissions and shipped status, propagating infrastructure failures instead of converting them
// to denial. Organization role-image source exposes an explicit empty generic-action list because
// its mutation path is specialized.
// projectCFGActions формирует текущие generic lifecycle hints после разрешения set/source/build
// permissions и shipped status, передавая инфраструктурные ошибки вместо преобразования в denial.
// Organization role-image source получает явно пустой generic-action list, поскольку изменяется
// специализированным путём.
func (repository *Repository) projectCFGActions(ctx context.Context, tx pgx.Tx, current scope, set *entity.ManagedConfigurationSet) error {
	if set == nil || (set.Kind != "ROLE_IMAGE" && set.Kind != "INTEGRATION_DEFINITION") {
		return nil
	}
	var resolved managedSet
	var err error
	if set.Kind == "ROLE_IMAGE" {
		_, scopeKind, _, ownerErr := repository.roleImageConfigurationOwner(ctx, tx, current, set.Ref)
		if ownerErr != nil && !errors.Is(ownerErr, errs.ErrNotFound) {
			return ownerErr
		}
		if ownerErr == nil && scopeKind == "ORGANIZATION" {
			// ORG source изменяется только специализированными recipe-командами.
			set.NextActions = []string{}
			return nil
		}
	}
	resolved, err = repository.resolveManagedSet(ctx, tx, current, command.ManagedConfigurationInput{ConfigurationRef: set.Ref}, set.Kind, false)
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

// refreshCFGReceipt refreshes only current archive/version/update metadata in
// role-image/integration command results, using the specialized role-image impact resolver when
// needed; the original immutable revision in the receipt is not replaced.
// refreshCFGReceipt обновляет только текущие archive/version/update metadata в
// role-image/integration command results, при необходимости используя специализированный
// role-image impact resolver; исходная immutable revision квитанции не заменяется.
func (repository *Repository) refreshCFGReceipt(ctx context.Context, tx pgx.Tx, current scope, result *command.Result) error {
	if result.ManagedConfiguration == nil || (result.ManagedConfiguration.Kind != "ROLE_IMAGE" && result.ManagedConfiguration.Kind != "INTEGRATION_DEFINITION") {
		return nil
	}
	var set managedSet
	var err error
	if result.ManagedConfiguration.Kind == "ROLE_IMAGE" && result.RoleImageImpactPlan != nil {
		set, err = repository.resolveRoleImageImpactSet(ctx, tx, current, command.ManagedConfigurationInput{ConfigurationRef: result.ManagedConfiguration.Ref})
	} else {
		set, err = repository.resolveManagedSet(ctx, tx, current, command.ManagedConfigurationInput{ConfigurationRef: result.ManagedConfiguration.Ref}, result.ManagedConfiguration.Kind, false)
	}
	if err != nil {
		return err
	}
	result.ManagedConfiguration.Archived = set.Archived
	result.ManagedConfiguration.Version = set.Version
	result.ManagedConfiguration.UpdatedAt = set.UpdatedAt
	return nil
}

// authorizeCFGLifecycle authorizes exact source set/recipe or shipped integration before lifecycle
// execution, using source.view for copy and image.build for archive. Role-image copies also
// require current target-project manage and source.manage/view, keeping source readability
// distinct from permission to create a target copy.
// authorizeCFGLifecycle авторизует точный source set/recipe либо shipped integration до lifecycle
// execution, используя source.view для copy и image.build для archive. Role-image copies
// дополнительно требуют текущие target-project manage и source.manage/view, сохраняя различие
// source readability и права создать целевую копию.
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

// copyCFG checks copy name and expected version, resolves an exact managed revision, recipe or
// shipped integration provenance and rejects mixed source locators. It preserves or canonicalizes
// source content by kind, validates allowed origin and shipped catalog pins, then inserts a new UI
// set and immutable DRAFT with parent/digest/provenance in the caller transaction; no build or
// publication is implied.
// copyCFG проверяет имя копии и expected version, разрешает точные managed revision, recipe либо
// shipped integration provenance и отклоняет смешанные source locators. Сохраняет либо
// канонизирует source content по kind, проверяет допустимые origin и shipped catalog pins, затем
// вставляет новый UI set и immutable DRAFT с parent/digest/provenance в транзакции вызывающего
// метода; build либо publication этим не подразумеваются.
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

// roleImageCopyContent serializes a new trimmed copy name with exact
// role/environment/package/tool/install/Dockerfile fields from the supplied recipe using the
// existing JSON encoder, without building an image or authorizing its source.
// roleImageCopyContent сериализует новое очищенное имя копии с точными
// role/environment/package/tool/install/Dockerfile fields переданного recipe прежним JSON encoder
// без сборки образа либо авторизации source.
func roleImageCopyContent(name string, recipe entity.RoleImageRecipe) string {
	return string(asJSON(map[string]any{"name": strings.TrimSpace(name), "roleImage": map[string]any{"roleDefinitionRef": recipe.RoleDefinitionRef, "environment": map[string]any{"environmentKey": recipe.Input.EnvironmentKey, "packageKeys": recipe.Input.PackageKeys, "toolKeys": recipe.Input.ToolKeys, "installationBlock": recipe.Input.InstallationBlock, "dockerfile": recipe.Input.Dockerfile}}}))
}

// bootstrapRoleImageCopyContent for a shipped recipe requires the bootstrap-copy selector and
// replaces only its environment key with the selected UI-capable key before canonical copy
// serialization; other recipe input bytes remain unchanged.
// bootstrapRoleImageCopyContent для shipped recipe требует bootstrap-copy selector и заменяет
// только environment key выбранным UI-capable key до канонической сериализации копии; остальные
// recipe input bytes не изменяются.
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

// archiveCFG permits only a currently unarchived UI set, optionally applies the mapped project
// role-image archive and its existing recipe-change event, then CAS-archives the managed set and
// returns its outcome. It preserves missing-mapping tolerance and caller transaction ownership
// without deleting content.
// archiveCFG допускает только текущий unarchived UI set, при наличии mapping применяет project
// role-image archive и прежний recipe-change event, затем CAS-архивирует managed set и возвращает
// outcome. Сохраняет допуск отсутствия mapping и владение транзакцией вызывающим методом без
// удаления content.
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
			result, _, _, applyErr := repository.applyRoleImageManage(ctx, tx, current, roleimagerepo.ManageInput{ScopeKind: "PROJECT", Action: "ARCHIVE", RecipeRef: recipeRef, ProjectRef: set.ProjectRef, Mutation: value.Mutation{ExpectedVersion: &version}})
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
