package platform

import (
	"context"
	_ "embed"
	"errors"
	"slices"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	revisionservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/revision"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryRoleImageSourceConfigurationTarget reads the exact tenant-owned ROLE_IMAGE configuration
// and optional mapped recipe/project references before selecting the authoritative source-access
// target.
// queryRoleImageSourceConfigurationTarget читает точную ROLE_IMAGE configuration нужного tenant и
// необязательные mapped recipe/project references до выбора авторитетной source-access target.
//
//go:embed sql/role_image_source__configuration_target.sql
var queryRoleImageSourceConfigurationTarget string

// queryRoleImageConfigurationOwner resolves owner scope from an immutable recipe/configuration
// mapping, requiring matching project identity and an active project for PROJECT scope.
// queryRoleImageConfigurationOwner разрешает owner scope по неизменяемой recipe/configuration
// mapping, требуя совпадение project identity и активный проект для PROJECT scope.
//
//go:embed sql/role_image_configuration_owner.sql
var queryRoleImageConfigurationOwner string

// roleImageConfigurationOwner reads and validates explicit recipe/organization/project scope pins,
// requiring organization-role access for ORGANIZATION; an empty project locator alone never
// determines scope.
// roleImageConfigurationOwner читает и проверяет явные recipe/organization/project scope pins,
// требуя organization-role access для ORGANIZATION; одна пустая ссылка проекта не определяет
// область.
func (repository *Repository) roleImageConfigurationOwner(ctx context.Context, tx pgx.Tx, current scope, ref string) (string, string, string, error) {
	var recipeRef, scopeKind, organizationRef, projectRef string
	err := tx.QueryRow(ctx, queryRoleImageConfigurationOwner, current.organizationID, ref).Scan(&recipeRef, &scopeKind, &organizationRef, &projectRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", errs.ErrNotFound
	}
	if err != nil || !validRuntimeOwnerSnapshot(scopeKind, organizationRef, projectRef) || organizationRef != current.organizationRef {
		return "", "", "", errs.ErrUnavailable
	}
	if scopeKind == "ORGANIZATION" {
		if err := repository.requireOrganizationRoleImageAccess(ctx, tx, current); err != nil {
			return "", "", "", err
		}
	}
	return recipeRef, scopeKind, projectRef, nil
}

// resolveRoleImageImpactSet derives exact recipe owner scope before resolving the managed
// ROLE_IMAGE set in that scope without creating a configuration.
// resolveRoleImageImpactSet выводит точную recipe owner scope до разрешения managed ROLE_IMAGE set
// в этой области без создания конфигурации.
func (repository *Repository) resolveRoleImageImpactSet(ctx context.Context, tx pgx.Tx, current scope, payload command.ManagedConfigurationInput) (managedSet, error) {
	_, scopeKind, _, err := repository.roleImageConfigurationOwner(ctx, tx, current, payload.ConfigurationRef)
	if err != nil {
		return managedSet{}, err
	}
	return repository.resolveManagedSetScope(ctx, tx, current, payload, revisionservice.KindRoleImage, false, scopeKind)
}

// projectRoleImageSource sets source visibility on the recipe and supplied build slice, removes
// private source text when unreadable and permits COPY only for readable PROJECT recipes; UPDATE
// additionally requires source-edit permission.
// projectRoleImageSource задаёт видимость source рецепта и переданного build slice, убирает
// внутренний source text без права чтения и допускает COPY только у читаемого PROJECT recipe;
// UPDATE дополнительно требует права редактирования source.
func projectRoleImageSource(recipe *entity.RoleImageRecipe, builds []entity.ImageBuild, canRead, canEdit bool) {
	recipe.SourceAvailable = canRead
	if canRead && recipe.ScopeKind == "PROJECT" && !slices.Contains(recipe.NextActions, "COPY") {
		recipe.NextActions = append(recipe.NextActions, "COPY")
	}
	if !canRead || recipe.ScopeKind != "PROJECT" {
		recipe.NextActions = slices.DeleteFunc(recipe.NextActions, func(action string) bool { return action == "COPY" })
	}
	for index := range builds {
		builds[index].SourceAvailable = canRead
	}
	if !canRead {
		recipe.Input.Dockerfile = ""
		recipe.Input.InstallationBlock = ""
		for index := range builds {
			builds[index].Dockerfile = ""
		}
	}
	if !canRead || !canEdit {
		recipe.NextActions = slices.DeleteFunc(recipe.NextActions, func(action string) bool { return action == "UPDATE" })
	}
}

// managedRoleImageSourceAccess resolves actual configuration/project ownership and mapped recipe
// scope before requiring the requested source permission, rejecting a conflicting supplied project
// instead of trusting it.
// managedRoleImageSourceAccess разрешает фактическое configuration/project ownership и mapped
// recipe scope до проверки заданного source permission, отклоняя конфликтующий переданный проект
// вместо доверия ему.
func (repository *Repository) managedRoleImageSourceAccess(ctx context.Context, tx pgx.Tx, current scope, ref, projectRef, permission string) error {
	if ref != "" {
		var recipeRef, actualProjectRef string
		if err := tx.QueryRow(ctx, queryRoleImageSourceConfigurationTarget, pgx.StrictNamedArgs{"organization_id": current.organizationID, "configuration_ref": ref}).Scan(&recipeRef, &actualProjectRef); errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrNotFound
		} else if err != nil {
			return errs.ErrUnavailable
		}
		if projectRef != "" && projectRef != actualProjectRef {
			return errs.ErrNotFound
		}
		projectRef = actualProjectRef
		if recipeRef != "" {
			recipeRef, scopeKind, actualProject, err := repository.roleImageConfigurationOwner(ctx, tx, current, ref)
			if err != nil {
				return err
			}
			target, err := repository.resolveScopedRoleImageAccessTarget(ctx, tx, current, recipeRef, actualProject, scopeKind)
			if err != nil {
				return err
			}
			return repository.requireAccess(ctx, tx, current, permission, target)
		}
	}
	if projectRef == "" {
		return repository.requireAccess(ctx, tx, current, permission, organizationTarget(current.organizationRef))
	}
	return repository.requireAccess(ctx, tx, current, permission, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: projectRef, ResourceKind: "PROJECT", ResourceRef: projectRef})
}

// authorizeManagedRoleImageSource ignores other payload/kind combinations and requires both source
// view and source manage for the seven mutating or validating ROLE_IMAGE actions after
// authoritative configuration lookup.
// authorizeManagedRoleImageSource пропускает другие payload/kind combinations и требует source
// view и source manage для семи изменяющих либо проверяющих ROLE_IMAGE actions после авторитетного
// чтения конфигурации.
func (repository *Repository) authorizeManagedRoleImageSource(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	payload, ok := input.Payload.(command.ManagedConfigurationInput)
	if !ok {
		return nil
	}
	kind, action := managedCommand(input.Kind)
	projectRef := payload.ProjectRef
	if payload.ConfigurationRef != "" {
		if err := tx.QueryRow(ctx, queryManagedConfigurationAccessTarget, pgx.StrictNamedArgs{"organization_id": current.organizationID, "configuration_ref": payload.ConfigurationRef}).Scan(&projectRef, &kind); errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrNotFound
		} else if err != nil {
			return errs.ErrUnavailable
		}
	}
	if kind != "ROLE_IMAGE" {
		return nil
	}
	if action == "CREATE" || action == "SAVE" || action == "COPY" || action == "DETACH" || action == "DISCARD" || action == "VALIDATE" || action == "PUBLISH" {
		for _, permission := range []string{"image.source.view", "image.source.manage"} {
			if err := repository.managedRoleImageSourceAccess(ctx, tx, current, payload.ConfigurationRef, projectRef, permission); err != nil {
				return err
			}
		}
	}
	return nil
}

// projectManagedRoleImageSource projects source-editability and source visibility for both current
// and returned managed revisions, propagating unexpected access failures instead of silently
// revealing content.
// projectManagedRoleImageSource формирует source-editability и source visibility для текущей и
// возвращаемой managed revisions, передавая неожиданные отказы доступа вместо молчаливой выдачи
// содержимого.
func (repository *Repository) projectManagedRoleImageSource(ctx context.Context, tx pgx.Tx, current scope, result *command.Result) error {
	if result.ManagedConfiguration == nil || result.ManagedConfiguration.Kind != "ROLE_IMAGE" {
		return nil
	}
	set := result.ManagedConfiguration
	canRead, canEdit, err := repository.managedRoleImageSourceProjection(ctx, tx, current, set.Ref, set.ProjectRef)
	if err != nil {
		return err
	}
	set.SourceEditable = &canEdit
	projectManagedRevisionSource(set.CurrentRevision, canRead)
	projectManagedRevisionSource(result.ManagedRevision, canRead)
	return nil
}

// managedRoleImageSourceProjection treats forbidden/not-found as absent view/manage hints but
// propagates other errors, and allows editing only when both permissions succeeded.
// managedRoleImageSourceProjection считает forbidden/not-found отсутствием view/manage hints,
// передаёт другие ошибки и разрешает редактирование только при успехе обеих проверок.
func (repository *Repository) managedRoleImageSourceProjection(ctx context.Context, tx pgx.Tx, current scope, ref, projectRef string) (bool, bool, error) {
	readErr := repository.managedRoleImageSourceAccess(ctx, tx, current, ref, projectRef, "image.source.view")
	if readErr != nil && !errors.Is(readErr, errs.ErrForbidden) && !errors.Is(readErr, errs.ErrNotFound) {
		return false, false, readErr
	}
	editErr := repository.managedRoleImageSourceAccess(ctx, tx, current, ref, projectRef, "image.source.manage")
	if editErr != nil && !errors.Is(editErr, errs.ErrForbidden) && !errors.Is(editErr, errs.ErrNotFound) {
		return false, false, editErr
	}
	return readErr == nil, readErr == nil && editErr == nil, nil
}

// projectManagedRevisionSource ignores an absent revision, otherwise sets SourceAvailable and
// clears content plus validation diagnostics when source read is denied.
// projectManagedRevisionSource пропускает отсутствующую ревизию, иначе задаёт SourceAvailable и
// очищает content и validation diagnostics при отказе чтения source.
func projectManagedRevisionSource(revision *entity.ManagedConfigurationRevision, canRead bool) {
	if revision == nil {
		return
	}
	revision.SourceAvailable = &canRead
	if !canRead {
		revision.Content = ""
		revision.ValidationDiagnostics = nil
	}
}

// projectRoleImageManageSource re-resolves the exact scoped recipe and current access context
// before redacting the manage result recipe and optional build; worker source materialization
// remains a separate grant path.
// projectRoleImageManageSource повторно разрешает точный scoped recipe и текущий access context до
// скрытия source в manage result recipe и необязательном build; worker materialization остаётся
// отдельным grant path.
func (repository *Repository) projectRoleImageManageSource(ctx context.Context, tx pgx.Tx, current scope, result *roleimagerepo.ManageResult, scopeKind string) error {
	target, err := repository.resolveScopedRoleImageAccessTarget(ctx, tx, current, result.Recipe.Ref, result.Recipe.ProjectRef, scopeKind)
	if err != nil {
		return err
	}
	authorization, err := repository.loadRoleImageAccessContext(ctx, tx, current)
	if err != nil {
		return err
	}
	canRead := authorization.allowed("image.source.view", target)
	projectRoleImageSource(&result.Recipe, nil, canRead, authorization.allowed("image.source.manage", target))
	if result.Build != nil {
		result.Build.SourceAvailable = canRead
		if !canRead {
			result.Build.Dockerfile = ""
		}
	}
	return nil
}
