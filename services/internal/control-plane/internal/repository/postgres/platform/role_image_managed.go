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

//go:embed sql/role_image_managed__read_recipe.sql
var queryRoleImageManagedReadRecipe string

//go:embed sql/role_image_managed__record.sql
var queryRoleImageManagedRecord string

//go:embed sql/role_image_managed__configuration.sql
var queryRoleImageManagedConfiguration string

//go:embed sql/role_image_managed__build.sql
var queryRoleImageManagedBuild string

//go:embed sql/role_image_managed__draft_absent.sql
var queryRoleImageManagedDraftAbsent string

//go:embed sql/role_image_managed__count.sql
var queryRoleImageManagedCount string

//go:embed sql/role_image_managed__lineage.sql
var queryRoleImageManagedLineage string

//go:embed sql/role_image_managed__build_revision.sql
var queryRoleImageManagedBuildRevision string

//go:embed sql/role_image_managed__shipped.sql
var queryRoleImageManagedShipped string

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

const platformOwnedRoleImageSource = "platform-owned:default-role-image"

func shippedRoleImage(recipe entity.RoleImageRecipe) bool {
	return recipe.Input.SourceRef == platformOwnedRoleImageSource && recipe.Input.EnvironmentKey == "system-base"
}

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
	set, err := repository.resolveManagedSet(ctx, tx, current, command.ManagedConfigurationInput{ConfigurationRef: ref}, revisionservice.KindRoleImage, false)
	if err != nil {
		return nil, err
	}
	if set.ManagedBy != "UI" && input.Action != "REQUEST_BUILD" {
		return nil, errs.ErrConflict
	}
	return &set, nil
}

func (repository *Repository) recordManagedRoleImageCommand(ctx context.Context, tx pgx.Tx, current scope, input roleimagerepo.ManageInput, set *managedSet, result roleimagerepo.ManageResult) error {
	if input.Action != "CREATE" && input.Action != "UPDATE" {
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
		created, err := repository.resolveManagedSet(ctx, tx, current, command.ManagedConfigurationInput{ProjectRef: recipe.ProjectRef, Name: recipe.Name}, revisionservice.KindRoleImage, true)
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
