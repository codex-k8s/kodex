package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	revisionservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/revision"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryRoleImageManagedReadRecipe locks the recipe mapped to an exact tenant/configuration and
// returns its current reference, version and role for managed-source publication.
// queryRoleImageManagedReadRecipe блокирует рецепт точных tenant/configuration и возвращает его
// текущие reference, version и role для публикации managed source.
//
//go:embed sql/role_image_managed__read_recipe.sql
var queryRoleImageManagedReadRecipe string

// queryRoleImageManagedRecord links managed configuration, exact published revision, recipe
// generation/version and build in the caller transaction, preserving a previously created owner
// mapping.
// queryRoleImageManagedRecord связывает managed configuration, точные published revision, recipe
// generation/version и build в транзакции caller, сохраняя ранее созданную owner mapping.
//
//go:embed sql/role_image_managed__record.sql
var queryRoleImageManagedRecord string

// queryRoleImageManagedConfiguration resolves the managed configuration reference only through
// matching organization ownership of the recipe and mapping.
// queryRoleImageManagedConfiguration разрешает ссылку managed configuration только через
// совпадающее организационное владение рецептом и mapping.
//
//go:embed sql/role_image_managed__configuration.sql
var queryRoleImageManagedConfiguration string

// queryRoleImageManagedBuild links an existing build to the managed revision for its exact recipe
// generation and ignores an already recorded build mapping.
// queryRoleImageManagedBuild связывает существующий build с managed revision его точной recipe
// generation и игнорирует уже записанную build mapping.
//
//go:embed sql/role_image_managed__build.sql
var queryRoleImageManagedBuild string

// queryRoleImageManagedDraftAbsent checks that no DRAFT, VALID or INVALID managed revision exists
// before recipe commands create and publish a new immutable source revision.
// queryRoleImageManagedDraftAbsent проверяет отсутствие DRAFT, VALID либо INVALID managed revision
// до создания и публикации новой неизменяемой source revision командами рецепта.
//
//go:embed sql/role_image_managed__draft_absent.sql
var queryRoleImageManagedDraftAbsent string

// queryRoleImageManagedCount counts scoped recipes using closed filters and authoritative
// organization-role or project catalog eligibility, including the caller project authority bound.
// queryRoleImageManagedCount считает scoped recipes по закрытым фильтрам и авторитетной
// organization-role либо project catalog eligibility с ограничением project authority caller.
//
//go:embed sql/role_image_managed__count.sql
var queryRoleImageManagedCount string

// queryRoleImageManagedLineage reads configuration/revision/source ownership for the exact recipe
// generation rather than silently using the latest configuration revision.
// queryRoleImageManagedLineage читает configuration/revision/source ownership точной recipe
// generation, не подставляя молча последнюю ревизию конфигурации.
//
//go:embed sql/role_image_managed__lineage.sql
var queryRoleImageManagedLineage string

// queryRoleImageManagedBuildRevision resolves the exact managed configuration revision recorded
// for a tenant-owned build, leaving absent mappings optional to the caller.
// queryRoleImageManagedBuildRevision разрешает точную managed configuration revision, записанную
// для build нужного tenant; отсутствие mapping остаётся необязательным для caller.
//
//go:embed sql/role_image_managed__build_revision.sql
var queryRoleImageManagedBuildRevision string

// queryRoleImageManagedShipped detects the platform-owned system-base recipe through exact
// tenant/configuration mapping and the immutable source marker.
// queryRoleImageManagedShipped определяет platform-owned system-base рецепт через точные
// tenant/configuration mapping и неизменяемый source marker.
//
//go:embed sql/role_image_managed__shipped.sql
var queryRoleImageManagedShipped string

// rejectShippedRoleImageMutation ignores other configuration kinds but rejects mutation of a
// mapped platform-owned role image, mapping lookup failure to unavailable.
// rejectShippedRoleImageMutation пропускает другие configuration kinds, но отклоняет изменение
// mapped platform-owned role image; отказ чтения даёт unavailable.
func rejectShippedRoleImageMutation(ctx context.Context, tx pgx.Tx, organizationID string, set managedSet) error {
	if set.Kind != revisionservice.KindRoleImage {
		return nil
	}
	var shipped bool
	if err := tx.QueryRow(ctx, queryRoleImageManagedShipped, set.id, organizationID, platformOwnedRoleImageSource).Scan(&shipped); err != nil {
		return errs.ErrUnavailable
	}
	if shipped {
		return errs.ErrConflict
	}
	return nil
}

// platformOwnedRoleImageSource identifies the shipped default role-image source only together with
// the system-base environment key; the marker does not authorize callers.
// platformOwnedRoleImageSource идентифицирует shipped default role-image source только вместе с
// environment key system-base; marker не авторизует caller.
const platformOwnedRoleImageSource = "platform-owned:default-role-image"

// shippedRoleImage recognizes only the exact platform source marker and system-base environment
// pair in the saved recipe input.
// shippedRoleImage распознаёт только точную пару platform source marker и environment system-base
// в сохранённом recipe input.
func shippedRoleImage(recipe entity.RoleImageRecipe) bool {
	return recipe.Input.SourceRef == platformOwnedRoleImageSource && recipe.Input.EnvironmentKey == "system-base"
}

// hydrateRoleImageManagedLineage loads exact generation lineage, synthesizes SHIPPED/BASELINE for
// platform recipes and removes UI mutation hints for Git-owned recipes without altering other
// actions.
// hydrateRoleImageManagedLineage загружает lineage точного поколения, формирует SHIPPED/BASELINE
// для platform recipes и убирает UI mutation hints у Git-owned рецептов без изменения прочих
// действий.
func hydrateRoleImageManagedLineage(ctx context.Context, reader roleImageQuerier, organizationID string, recipe *entity.RoleImageRecipe) error {
	var lineage entity.RoleImageManagedLineage
	err := reader.QueryRow(ctx, queryRoleImageManagedLineage, organizationID, recipe.Ref).Scan(&lineage.ConfigurationRef, &lineage.RevisionRef, &lineage.Revision, &lineage.ManagedBy, &lineage.SourceRef, &lineage.SourceRevision, &lineage.Origin)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrUnavailable
	}
	if shippedRoleImage(*recipe) {
		lineage.ManagedBy, lineage.Origin = "SHIPPED", "BASELINE"
		lineage.SourceRef, lineage.SourceRevision = recipe.Input.SourceRef, recipe.Input.SourceRevision
	} else if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	recipe.ManagedLineage = &lineage
	if lineage.ManagedBy == "GIT" {
		actions := make([]string, 0, len(recipe.NextActions))
		for _, action := range recipe.NextActions {
			if action != "UPDATE" && action != "ARCHIVE" && action != "RESTORE" {
				actions = append(actions, action)
			}
		}
		recipe.NextActions = actions
	}
	return nil
}

// hydrateRoleImageBuildRevision adds the optional recorded configuration revision to a build,
// accepting an absent mapping but rejecting other read failures.
// hydrateRoleImageBuildRevision добавляет необязательную записанную configuration revision в
// build, принимая отсутствие mapping и отклоняя другие ошибки чтения.
func hydrateRoleImageBuildRevision(ctx context.Context, reader roleImageQuerier, organizationID string, build *entity.ImageBuild) error {
	err := reader.QueryRow(ctx, queryRoleImageManagedBuildRevision, organizationID, build.Ref).Scan(&build.ConfigurationRevisionRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// managedRoleImageTarget resolves an optional mapped managed set in the requested scope; non-UI
// ownership permits only build/cancel, and archived sets permit only restore.
// managedRoleImageTarget разрешает необязательный mapped managed set в запрошенной области; non-UI
// ownership допускает только build/cancel, архивный set — только restore.
func (repository *Repository) managedRoleImageTarget(ctx context.Context, tx pgx.Tx, current scope, input roleimagerepo.ManageInput) (*managedSet, error) {
	if input.RecipeRef == "" {
		return nil, nil
	}
	var ref string
	err := tx.QueryRow(ctx, queryRoleImageManagedConfiguration, current.organizationID, input.RecipeRef).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	set, err := repository.resolveManagedSetScope(ctx, tx, current, command.ManagedConfigurationInput{ConfigurationRef: ref}, revisionservice.KindRoleImage, false, input.ScopeKind)
	if err != nil {
		return nil, err
	}
	if set.ManagedBy != "UI" && input.Action != "REQUEST_BUILD" && input.Action != "CANCEL_BUILD" {
		return nil, errs.ErrConflict
	}
	if set.Archived && input.Action != "RESTORE" {
		return nil, errs.ErrConflict
	}
	return &set, nil
}

// recordManagedRoleImageCommand mirrors archive/restore state and records build lineage;
// create/update/restore require no parallel draft and publish a fresh immutable managed revision
// in the caller transaction.
// recordManagedRoleImageCommand отражает archive/restore state и записывает build lineage;
// create/update/restore требуют отсутствия параллельного draft и публикуют свежую неизменяемую
// managed revision в транзакции caller.
func (repository *Repository) recordManagedRoleImageCommand(ctx context.Context, tx pgx.Tx, current scope, input roleimagerepo.ManageInput, set *managedSet, result roleimagerepo.ManageResult) error {
	if set != nil && (input.Action == "ARCHIVE" || input.Action == "RESTORE") {
		if err := tx.QueryRow(ctx, queryCFGArchive, set.id, set.Version, input.Action == "ARCHIVE").Scan(&set.Version, &set.UpdatedAt); err != nil {
			return errs.ErrVersionMismatch
		}
		set.Archived = input.Action == "ARCHIVE"
	}
	// RESTORE создаёт новую generation рецепта, поэтому для неё требуется
	// новая immutable source revision, а не ссылка на прежнюю generation.
	if input.Action != "CREATE" && input.Action != "UPDATE" && input.Action != "RESTORE" {
		if result.Build != nil && set != nil {
			_, err := tx.Exec(ctx, queryRoleImageManagedBuild, current.organizationID, result.Build.Ref)
			if err != nil {
				return errs.ErrUnavailable
			}
		}
		return nil
	}
	if result.Build == nil {
		return errs.ErrUnavailable
	}
	recipe := result.Recipe
	if set == nil {
		var created managedSet
		var err error
		if recipe.ScopeKind == "ORGANIZATION" {
			created, err = repository.createOrganizationRoleImageConfiguration(ctx, tx, current, recipe.Name)
		} else {
			created, err = repository.resolveManagedSet(ctx, tx, current, command.ManagedConfigurationInput{ProjectRef: recipe.ProjectRef, Name: recipe.Name}, revisionservice.KindRoleImage, true)
		}
		if err != nil {
			return err
		}
		set = &created
	}
	// Recipe OCC не даёт права заменить параллельный managed draft без его version.
	var draftAbsent bool
	if err := tx.QueryRow(ctx, queryRoleImageManagedDraftAbsent, set.id).Scan(&draftAbsent); err != nil {
		return errs.ErrUnavailable
	}
	if !draftAbsent {
		return errs.ErrConflict
	}
	selection := entity.RoleEnvironmentSelection{EnvironmentKey: recipe.Input.EnvironmentKey, PackageKeys: recipe.Input.PackageKeys, ToolKeys: recipe.Input.ToolKeys, InstallationBlock: recipe.Input.InstallationBlock, Dockerfile: recipe.Input.Dockerfile}
	content := string(asJSON(map[string]any{"name": recipe.Name, "roleImage": map[string]any{
		"roleDefinitionRef": recipe.RoleDefinitionRef, "environment": map[string]any{
			"environmentKey": selection.EnvironmentKey, "packageKeys": selection.PackageKeys, "toolKeys": selection.ToolKeys,
			"installationBlock": selection.InstallationBlock, "dockerfile": selection.Dockerfile}}}))
	digest := sha256.Sum256([]byte(content))
	ref, err := newRef("mrev")
	if err != nil {
		return errs.ErrUnavailable
	}
	revision, err := scanManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationInsertRevision, pgx.StrictNamedArgs{
		"revision_ref": ref, "organization_id": current.organizationID, "configuration_set_id": set.id,
		"content_format": "JSON", "content": content, "digest": hex.EncodeToString(digest[:]),
		"parent_revision_id": set.currentRevisionID, "actor_id": current.actorID}))
	if err != nil {
		return mapWriteError(err)
	}
	_, err = scanManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationValidateRevision, pgx.StrictNamedArgs{
		"revision_id": revision.internalID, "state": "VALID", "diagnostics": "[]"}))
	if err != nil {
		return mapWriteError(err)
	}
	published, _, _, err := scanPublishedManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationPublishRevision, pgx.StrictNamedArgs{
		"configuration_set_id": set.id, "revision_id": revision.internalID, "expected_version": set.Version}))
	if err != nil {
		return mapWriteError(err)
	}
	tag, err := tx.Exec(ctx, queryRoleImageManagedRecord, set.id, current.organizationID, recipe.Ref, published.Ref, recipe.Generation, recipe.Version, result.Build.Ref)
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrUnavailable
	}
	return nil
}

// ConfigureRoleImageCatalog installs catalog resolution, recommendation, listing and
// bootstrap-copy callbacks on the repository without querying storage or granting access.
// ConfigureRoleImageCatalog устанавливает catalog callbacks разрешения, рекомендаций, списка и
// bootstrap copy в repository без чтения хранилища или выдачи доступа.
func (repository *Repository) ConfigureRoleImageCatalog(catalog *roleimageservice.Catalog) {
	repository.roleImageCatalogResolver = catalog.Resolve
	repository.roleImageRecommendedSelection = catalog.RecommendedSelection
	repository.roleImageCatalogEntries = catalog.List
	repository.roleImageBootstrapCopySelection = catalog.CopyBootstrapSelection
}

// validateSourceRoleImage requires an installed catalog, strict structured role-image parsing and
// resolved managed recipe validation; metadata validity does not establish image-build permission.
// validateSourceRoleImage требует установленный каталог, строгий разбор структурированного role
// image и проверку разрешённого managed recipe; допустимость метаданных не доказывает image-build
// permission.
func (repository *Repository) validateSourceRoleImage(set managedSet, format, content string) error {
	_, err := repository.sourceRoleImageInput(set, format, content)
	return err
}

// publishSourceRoleImage validates the source and requires image.build on the exact project,
// creates or updates the mapped recipe with OCC and immutable role identity, then records build
// lineage, audit and event in the caller transaction.
// publishSourceRoleImage проверяет source и требует image.build точного проекта, создаёт либо
// обновляет mapped recipe с OCC и неизменяемой role identity, затем записывает build lineage,
// audit и event в транзакции caller.
func (repository *Repository) publishSourceRoleImage(ctx context.Context, tx pgx.Tx, current scope, set managedSet, revision entity.ManagedConfigurationRevision) error {
	input, err := repository.sourceRoleImageInput(set, revision.ContentFormat, revision.Content)
	if err != nil {
		return err
	}
	if repository.requireAccess(ctx, tx, current, "image.build", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: set.ProjectRef, ResourceKind: "PROJECT", ResourceRef: set.ProjectRef}) != nil {
		return errs.ErrForbidden
	}
	var recipeRef, currentRole string
	var recipeVersion int64
	err = tx.QueryRow(ctx, queryRoleImageManagedReadRecipe, set.id, current.organizationID).Scan(&recipeRef, &recipeVersion, &currentRole)
	if err == nil {
		if input.RoleDefinitionRef != currentRole {
			return errs.ErrConflict
		}
		input.Action, input.RecipeRef, input.RoleDefinitionRef = "UPDATE", recipeRef, ""
		input.Mutation.ExpectedVersion = &recipeVersion
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrUnavailable
	}
	result, projectID, projectRef, err := repository.applyRoleImageManage(ctx, tx, current, input)
	if err != nil {
		return err
	}
	if result.Build == nil {
		return errs.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, queryRoleImageManagedRecord, set.id, current.organizationID, result.Recipe.Ref, revision.Ref, result.Recipe.Generation, result.Recipe.Version, result.Build.Ref)
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrUnavailable
	}
	if err := repository.auditRoleImage(ctx, tx, current, projectID, "managed-role-image.publish", "ROLE_IMAGE_RECIPE", result.Recipe.Ref, "i18n:ROLE_IMAGE_RECIPE_CHANGED"); err != nil {
		return err
	}
	return repository.emitPlatformEventSnapshot(ctx, tx, current, "ROLE_IMAGE_RECIPE_CHANGED", projectRef, result.Recipe.Ref, "i18n:ROLE_IMAGE_RECIPE_CHANGED", int64(result.Recipe.Version), result.Recipe.State)
}

// sourceRoleImageInput preserves the shared catalog/parse/resolve/recipe-validation order and
// maps source failures to the existing domain errors, returning the project CREATE input without
// resolving resources or granting image.build authority.
// sourceRoleImageInput сохраняет общий порядок catalog/parse/resolve/recipe validation и прежнее
// преобразование отказов source в доменные ошибки, возвращая project CREATE input без разрешения
// ресурсов либо выдачи image.build authority.
func (repository *Repository) sourceRoleImageInput(set managedSet, format, content string) (roleimagerepo.ManageInput, error) {
	if repository.roleImageCatalogResolver == nil {
		return roleimagerepo.ManageInput{}, errs.ErrUnavailable
	}
	name, roleRef, selection, err := revisionservice.ParseRoleImage(format, content)
	if err != nil {
		return roleimagerepo.ManageInput{}, errs.ErrInvalid
	}
	resolved, err := repository.roleImageCatalogResolver(selection)
	if err != nil || roleimageservice.ValidateManagedRecipe(set.ProjectRef, roleRef, name, resolved) != nil {
		return roleimagerepo.ManageInput{}, errs.ErrInvalid
	}
	input := roleimagerepo.ManageInput{ScopeKind: "PROJECT", Action: "CREATE", ProjectRef: set.ProjectRef, RoleDefinitionRef: roleRef, Name: name, Recipe: resolved, Environment: selection}
	return input, nil
}
