package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	revisionservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/revision"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/role_image_managed__read_recipe.sql
var queryRoleImageManagedReadRecipe string

//go:embed sql/role_image_managed__record.sql
var queryRoleImageManagedRecord string

func (repository *Repository) ConfigureRoleImageCatalog(resolve func(entity.RoleEnvironmentSelection) (entity.RoleImageRecipeInput, error)) {
	repository.roleImageCatalogResolver = resolve
}

func (repository *Repository) validateSourceRoleImage(set managedSet, format, content string) error {
	if repository.roleImageCatalogResolver == nil {
		return errs.ErrUnavailable
	}
	name, roleRef, selection, err := revisionservice.ParseRoleImage(format, content)
	if err != nil {
		return errs.ErrInvalid
	}
	resolved, err := repository.roleImageCatalogResolver(selection)
	if err != nil || roleimageservice.ValidateManagedRecipe(set.ProjectRef, roleRef, name, resolved) != nil {
		return errs.ErrInvalid
	}
	return nil
}

func (repository *Repository) publishSourceRoleImage(ctx context.Context, tx pgx.Tx, current scope, set managedSet, revision entity.ManagedConfigurationRevision) error {
	if repository.roleImageCatalogResolver == nil {
		return errs.ErrUnavailable
	}
	name, roleRef, selection, err := revisionservice.ParseRoleImage(revision.ContentFormat, revision.Content)
	if err != nil {
		return errs.ErrInvalid
	}
	resolved, err := repository.roleImageCatalogResolver(selection)
	if err != nil || roleimageservice.ValidateManagedRecipe(set.ProjectRef, roleRef, name, resolved) != nil {
		return errs.ErrInvalid
	}
	if repository.requireAccess(ctx, tx, current, "image.build", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: set.ProjectRef, ResourceKind: "PROJECT", ResourceRef: set.ProjectRef}) != nil {
		return errs.ErrForbidden
	}
	input := roleimagerepo.ManageInput{Action: "CREATE", ProjectRef: set.ProjectRef, RoleDefinitionRef: roleRef, Name: name, Recipe: resolved, Environment: selection}
	var recipeRef, currentRole string
	var recipeVersion int64
	err = tx.QueryRow(ctx, queryRoleImageManagedReadRecipe, set.id, current.organizationID).Scan(&recipeRef, &recipeVersion, &currentRole)
	if err == nil {
		if roleRef != currentRole {
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
