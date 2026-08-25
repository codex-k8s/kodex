package platform

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) List(ctx context.Context, principal value.Principal, filter roleimagerepo.Filter) ([]entity.RoleImageRecipe, string, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, queryRoleImagesListRecipes, current.organizationID,
		filter.ProjectRef, filter.RoleDefinitionRef, current.role, current.actorID, boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	result := make([]entity.RoleImageRecipe, 0)
	for rows.Next() {
		item, scanErr := scanRecipe(rows)
		if scanErr != nil {
			return nil, "", errs.ErrUnavailable
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", errs.ErrUnavailable
	}
	return result, "", nil
}

func (repository *Repository) Get(ctx context.Context, principal value.Principal, ref string) (entity.RoleImageRecipe, []entity.ImageBuild, *entity.ImageArtifact, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.RoleImageRecipe{}, nil, nil, err
	}
	return repository.getRoleImageRecipe(ctx, repository.pool, current, ref)
}

type roleImageQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (repository *Repository) getRoleImageRecipe(ctx context.Context, querier roleImageQuerier, current scope, ref string) (entity.RoleImageRecipe, []entity.ImageBuild, *entity.ImageArtifact, error) {
	row := querier.QueryRow(ctx, queryRoleImagesGetRecipe, current.organizationID, ref, current.role, current.actorID)
	var internalID string
	var recipe entity.RoleImageRecipe
	var specification []byte
	var canManage bool
	err := row.Scan(&internalID, &recipe.Ref, &recipe.ProjectRef, &recipe.RoleDefinitionRef,
		&recipe.Name, &recipe.State, &specification, &recipe.Generation, &recipe.SpecSHA256,
		&recipe.PolicyRevision, &recipe.PolicySHA256, &recipe.RoleRuntimeContractRevision,
		&recipe.RoleRuntimeContractSHA256, &recipe.ActiveImageArtifactRef,
		&recipe.PromotedImageReference, &recipe.Version, &recipe.CreatedAt, &recipe.UpdatedAt,
		&canManage)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.RoleImageRecipe{}, nil, nil, errs.ErrNotFound
	}
	if err != nil || decodeJSON(specification, &recipe.Input) != nil {
		return entity.RoleImageRecipe{}, nil, nil, errs.ErrUnavailable
	}
	recipe.NextActions = roleImageActions(recipe, canManage)
	rows, err := querier.Query(ctx, queryRoleImagesListBuilds, current.organizationID, internalID)
	if err != nil {
		return entity.RoleImageRecipe{}, nil, nil, errs.ErrUnavailable
	}
	builds := make([]entity.ImageBuild, 0)
	for rows.Next() {
		build, scanErr := scanBuild(rows)
		if scanErr != nil {
			rows.Close()
			return entity.RoleImageRecipe{}, nil, nil, errs.ErrUnavailable
		}
		builds = append(builds, build)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return entity.RoleImageRecipe{}, nil, nil, errs.ErrUnavailable
	}
	var artifact *entity.ImageArtifact
	if recipe.ActiveImageArtifactRef != "" {
		item, artifactErr := scanRoleImageArtifact(querier.QueryRow(ctx, queryRoleImagesGetActiveArtifact,
			current.organizationID, recipe.ActiveImageArtifactRef))
		if artifactErr != nil {
			return entity.RoleImageRecipe{}, nil, nil, errs.ErrUnavailable
		}
		artifact = &item
	}
	return recipe, builds, artifact, nil
}

func (repository *Repository) Manage(ctx context.Context, input roleimagerepo.ManageInput) (roleimagerepo.ManageResult, error) {
	current, err := repository.resolveScope(ctx, input.Principal)
	if err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return roleimagerepo.ManageResult{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var replay roleimagerepo.ManageResult
	if found, receiptErr := repository.loadRoleImageReceipt(ctx, tx, current,
		input.Mutation.Operation, input.Mutation.IdempotencyKey, input.Mutation.IntentDigest, &replay); receiptErr != nil {
		return roleimagerepo.ManageResult{}, receiptErr
	} else if found {
		if err := committed(tx, ctx); err != nil {
			return roleimagerepo.ManageResult{}, err
		}
		return replay, nil
	}

	result, projectID, projectRef, err := repository.applyRoleImageManage(ctx, tx, current, input)
	if err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	if err := repository.auditRoleImage(ctx, tx, current, projectID, input.Mutation.Operation,
		"ROLE_IMAGE_RECIPE", result.Recipe.Ref, "i18n:ROLE_IMAGE_RECIPE_CHANGED"); err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	if err := repository.emitPlatformEvent(ctx, tx, current, "ROLE_IMAGE_RECIPE_CHANGED",
		projectRef, result.Recipe.Ref, "i18n:ROLE_IMAGE_RECIPE_CHANGED"); err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	if err := repository.storeRoleImageReceipt(ctx, tx, current, input.Mutation.Operation,
		input.Mutation.IdempotencyKey, input.Mutation.IntentDigest, "ROLE_IMAGE_MANAGE", result); err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	return result, nil
}

func (repository *Repository) applyRoleImageManage(ctx context.Context, tx pgx.Tx, current scope, input roleimagerepo.ManageInput) (roleimagerepo.ManageResult, string, string, error) {
	specification := asJSON(input.Recipe)
	specSHA256 := roleImageDigest(input.Recipe)
	switch input.Action {
	case "CREATE":
		var projectID, roleID string
		if err := tx.QueryRow(ctx, queryRoleImagesResolveProjectRole, current.organizationID,
			input.ProjectRef, input.RoleDefinitionRef, current.role, current.actorID).Scan(&projectID, &roleID); errors.Is(err, pgx.ErrNoRows) {
			return roleimagerepo.ManageResult{}, "", "", errs.ErrNotFound
		} else if err != nil {
			return roleimagerepo.ManageResult{}, "", "", errs.ErrUnavailable
		}
		ref, _ := newRef("imgrec")
		var recipeID string
		if err := tx.QueryRow(ctx, queryRoleImagesInsertRecipe, ref, current.organizationID,
			projectID, roleID, input.Name, specification, specSHA256,
			repository.roleImages.PolicyRevision, repository.roleImages.PolicySHA256,
			repository.roleImages.RoleRuntimeContractRevision,
			repository.roleImages.RoleRuntimeContractSHA256, current.actorID).Scan(&recipeID); err != nil {
			return roleimagerepo.ManageResult{}, "", "", mapRoleImageWriteError(err)
		}
		recipe, _, _, err := repository.getRoleImageRecipe(ctx, tx, current, ref)
		if err != nil {
			return roleimagerepo.ManageResult{}, "", "", err
		}
		build, err := repository.insertRoleImageBuild(ctx, tx, current, recipeID, recipe)
		return roleImageManageResult(recipe, build, nil, false), projectID, input.ProjectRef, err
	case "UPDATE", "ARCHIVE", "RESTORE", "REQUEST_BUILD":
		locked, err := scanLockedRecipe(tx.QueryRow(ctx, queryRoleImagesLockRecipe,
			current.organizationID, input.RecipeRef, current.role, current.actorID))
		if errors.Is(err, pgx.ErrNoRows) {
			return roleimagerepo.ManageResult{}, "", "", errs.ErrNotFound
		}
		if err != nil {
			return roleimagerepo.ManageResult{}, "", "", errs.ErrUnavailable
		}
		if input.ProjectRef != locked.Recipe.ProjectRef {
			return roleimagerepo.ManageResult{}, "", "", errs.ErrNotFound
		}
		if input.Mutation.ExpectedVersion == nil || uint64(*input.Mutation.ExpectedVersion) != locked.Recipe.Version {
			return roleimagerepo.ManageResult{}, "", "", errs.ErrVersionMismatch
		}
		if input.Action != "RESTORE" && locked.Recipe.State != "ACTIVE" || input.Action == "RESTORE" && locked.Recipe.State != "ARCHIVED" {
			return roleimagerepo.ManageResult{}, "", "", errs.ErrConflict
		}
		if input.Action == "UPDATE" {
			if _, err := tx.Exec(ctx, queryRoleImagesCancelOpenBuilds, current.organizationID, locked.ID); err != nil {
				return roleimagerepo.ManageResult{}, "", "", errs.ErrUnavailable
			}
			if _, err := tx.Exec(ctx, queryRoleImagesUpdateRecipe, current.organizationID, locked.ID,
				input.Name, specification, specSHA256, repository.roleImages.PolicyRevision,
				repository.roleImages.PolicySHA256, repository.roleImages.RoleRuntimeContractRevision,
				repository.roleImages.RoleRuntimeContractSHA256); err != nil {
				return roleimagerepo.ManageResult{}, "", "", mapRoleImageWriteError(err)
			}
		} else if input.Action == "ARCHIVE" || input.Action == "RESTORE" {
			if _, err := tx.Exec(ctx, queryRoleImagesCancelOpenBuilds, current.organizationID, locked.ID); err != nil {
				return roleimagerepo.ManageResult{}, "", "", errs.ErrUnavailable
			}
			state := "ARCHIVED"
			if input.Action == "RESTORE" {
				state = "ACTIVE"
			}
			if _, err := tx.Exec(ctx, queryRoleImagesChangeRecipeState, current.organizationID, locked.ID, state); err != nil {
				return roleimagerepo.ManageResult{}, "", "", mapRoleImageWriteError(err)
			}
		}
		recipe, _, activeArtifact, err := repository.getRoleImageRecipe(ctx, tx, current, input.RecipeRef)
		if err != nil {
			return roleimagerepo.ManageResult{}, "", "", err
		}
		if input.Action == "ARCHIVE" {
			return roleImageManageResult(recipe, nil, activeArtifact, false), locked.ProjectID, locked.Recipe.ProjectRef, nil
		}
		if input.Action == "REQUEST_BUILD" && activeArtifact != nil && activeArtifact.SpecSHA256 == recipe.SpecSHA256 && activeArtifact.PromotedReference != "" {
			return roleImageManageResult(recipe, nil, activeArtifact, true), locked.ProjectID, locked.Recipe.ProjectRef, nil
		}
		build, err := repository.insertRoleImageBuild(ctx, tx, current, locked.ID, recipe)
		return roleImageManageResult(recipe, build, activeArtifact, false), locked.ProjectID, locked.Recipe.ProjectRef, err
	default:
		return roleimagerepo.ManageResult{}, "", "", errs.ErrInvalid
	}
}

func (repository *Repository) insertRoleImageBuild(ctx context.Context, tx pgx.Tx, current scope, recipeID string, recipe entity.RoleImageRecipe) (*entity.ImageBuild, error) {
	immutable := roleImageDigest(struct {
		Input                    entity.RoleImageRecipeInput
		SpecSHA256, PolicySHA256 string
		PolicyRevision           uint64
		ContractRevision         uint64
		ContractSHA256           string
	}{recipe.Input, recipe.SpecSHA256, recipe.PolicySHA256, recipe.PolicyRevision,
		recipe.RoleRuntimeContractRevision, recipe.RoleRuntimeContractSHA256})
	ref, _ := newRef("imgbld")
	var buildID string
	if err := tx.QueryRow(ctx, queryRoleImagesInsertBuild, ref, current.organizationID,
		immutable, repository.roleImages.MaximumAttempts, recipeID).Scan(&buildID); err != nil {
		return nil, mapRoleImageWriteError(err)
	}
	rows, err := tx.Query(ctx, queryRoleImagesListBuilds, current.organizationID, recipeID)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		build, scanErr := scanBuild(rows)
		if scanErr != nil {
			return nil, errs.ErrUnavailable
		}
		if build.Ref == ref {
			return &build, nil
		}
	}
	return nil, errs.ErrUnavailable
}

func decodeJSON(raw []byte, target any) error {
	return json.Unmarshal(raw, target)
}
