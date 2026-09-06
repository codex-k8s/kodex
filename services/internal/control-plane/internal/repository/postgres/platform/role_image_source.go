package platform

import (
	"context"
	_ "embed"
	"errors"
	"slices"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/role_image_source__configuration_target.sql
var queryRoleImageSourceConfigurationTarget string

// Проекция выполняется только на пользовательской границе. Неизменяемый
// source для build worker остаётся частью его отдельного exact grant.
func projectRoleImageSource(recipe *entity.RoleImageRecipe, builds []entity.ImageBuild, canRead, canEdit bool) {
	recipe.SourceAvailable = canRead
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
			target, err := repository.resolveRoleImageAccessTarget(ctx, tx, current, recipeRef, projectRef)
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

func (repository *Repository) projectRoleImageManageSource(ctx context.Context, tx pgx.Tx, current scope, result *roleimagerepo.ManageResult) error {
	target, err := repository.resolveRoleImageAccessTarget(ctx, tx, current, result.Recipe.Ref, result.Recipe.ProjectRef)
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
