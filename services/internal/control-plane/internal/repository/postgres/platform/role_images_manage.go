package platform

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	accessservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/access"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

type roleImageListCursor struct {
	Filter    string
	Ref       string
	UpdatedAt time.Time
}

func (repository *Repository) List(ctx context.Context, principal value.Principal, filter roleimagerepo.Filter) ([]entity.RoleImageRecipe, string, int64, error) {
	if len(filter.Query) > 128 || !utf8.ValidString(filter.Query) || strings.ContainsRune(filter.Query, 0) || (filter.State != "" && filter.State != "ACTIVE" && filter.State != "ARCHIVED") {
		return nil, "", 0, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", 0, err
	}
	filterDigest := roleImageDigest([]string{current.organizationID, current.actorID, current.authorityProjectID, filter.ProjectRef, filter.RoleDefinitionRef, filter.Query, filter.State})
	cursor := roleImageListCursor{}
	if filter.Page.Token != "" {
		raw, err := base64.RawURLEncoding.DecodeString(filter.Page.Token)
		if err != nil || len(raw) > 2048 || json.Unmarshal(raw, &cursor) != nil || cursor.Filter != filterDigest || cursor.Ref == "" || cursor.UpdatedAt.IsZero() {
			return nil, "", 0, errs.ErrInvalid
		}
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, "", 0, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{ProjectRef: filter.ProjectRef, ResourceKind: "PROJECT", ResourceRef: filter.ProjectRef}); err != nil {
		return nil, "", 0, err
	}
	authorization, err := repository.loadRoleImageAccessContext(ctx, tx, current)
	if err != nil {
		return nil, "", 0, err
	}
	if err := tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&authorization.evaluatedAt); err != nil {
		return nil, "", 0, errs.ErrUnavailable
	}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project": current.authorityProjectID, "project_ref": filter.ProjectRef, "role_ref": filter.RoleDefinitionRef, "query": filter.Query, "state": filter.State}
	var total int64
	if err := tx.QueryRow(ctx, queryRoleImageManagedCount, args).Scan(&total); err != nil {
		return nil, "", 0, errs.ErrUnavailable
	}
	args["cursor_ref"], args["cursor_time"], args["page_limit"] = cursor.Ref, cursor.UpdatedAt, boundedPage(filter.Page)+1
	rows, err := tx.Query(ctx, queryRoleImagesListRecipes, args)
	if err != nil {
		return nil, "", 0, errs.ErrUnavailable
	}
	result := make([]entity.RoleImageRecipe, 0)
	targets := make([]resolvedAccessTarget, 0)
	for rows.Next() {
		item, ownerRef, err := scanRecipe(rows)
		if err != nil {
			rows.Close()
			return nil, "", 0, errs.ErrUnavailable
		}
		target := roleImageAccessTarget(item.Ref, item.ProjectRef, ownerRef)
		if !authorization.allowed("project.view", target) {
			rows.Close()
			return nil, "", 0, errs.ErrUnavailable
		}
		result, targets = append(result, item), append(targets, target)
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return nil, "", 0, errs.ErrUnavailable
	}
	next := ""
	if len(result) > int(boundedPage(filter.Page)) {
		result, targets = result[:len(result)-1], targets[:len(targets)-1]
		last := result[len(result)-1]
		raw, _ := json.Marshal(roleImageListCursor{Filter: filterDigest, Ref: last.Ref, UpdatedAt: last.UpdatedAt})
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	for index := range result {
		result[index].NextActions = roleImageActions(result[index], authorization.allowed("image.build", targets[index]))
		if err := hydrateRoleImageManagedLineage(ctx, tx, current.organizationID, &result[index]); err != nil {
			return nil, "", 0, err
		}
		projectRoleImageSource(&result[index], nil, authorization.allowed("image.source.view", targets[index]), authorization.allowed("image.source.manage", targets[index]))
	}
	if err := committed(tx, ctx); err != nil {
		return nil, "", 0, err
	}
	return result, next, total, nil
}

func (repository *Repository) Get(ctx context.Context, principal value.Principal, ref string) (roleimagerepo.Detail, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return roleimagerepo.Detail{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return roleimagerepo.Detail{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	target, err := repository.resolveRoleImageAccessTarget(ctx, tx, current, ref, "")
	if err != nil {
		return roleimagerepo.Detail{}, err
	}
	authorization, err := repository.loadRoleImageAccessContext(ctx, tx, current)
	if err != nil {
		return roleimagerepo.Detail{}, err
	}
	if !authorization.allowed("project.view", target) {
		return roleimagerepo.Detail{}, errs.ErrNotFound
	}
	canBuild := authorization.allowed("image.build", target)
	canPromote := authorization.allowed("image.promote", target)
	detail, err := repository.getRoleImageRecipe(ctx, tx, current, ref, canBuild, canPromote)
	if err != nil {
		return roleimagerepo.Detail{}, err
	}
	projectRoleImageSource(&detail.Recipe, detail.Builds, authorization.allowed("image.source.view", target), authorization.allowed("image.source.manage", target))
	if err := committed(tx, ctx); err != nil {
		return roleimagerepo.Detail{}, err
	}
	return detail, nil
}

type roleImageQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (repository *Repository) getRoleImageRecipe(ctx context.Context, querier roleImageQuerier, current scope, ref string, canBuild, canPromote bool) (roleimagerepo.Detail, error) {
	row := querier.QueryRow(ctx, queryRoleImagesGetRecipe, current.organizationID, ref)
	var internalID string
	var recipe entity.RoleImageRecipe
	var specification []byte
	err := row.Scan(&internalID, &recipe.Ref, &recipe.ProjectRef, &recipe.RoleDefinitionRef,
		&recipe.Name, &recipe.State, &specification, &recipe.Generation, &recipe.SpecSHA256,
		&recipe.PolicyRevision, &recipe.PolicySHA256, &recipe.RoleRuntimeContractRevision,
		&recipe.RoleRuntimeContractSHA256, &recipe.ActiveImageArtifactRef,
		&recipe.PromotedImageReference, &recipe.Version, &recipe.CreatedAt, &recipe.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return roleimagerepo.Detail{}, errs.ErrNotFound
	}
	if err != nil || decodeJSON(specification, &recipe.Input) != nil {
		return roleimagerepo.Detail{}, errs.ErrUnavailable
	}
	recipe.NextActions = roleImageActions(recipe, canBuild)
	rows, err := querier.Query(ctx, queryRoleImagesListBuilds, current.organizationID, internalID)
	if err != nil {
		return roleimagerepo.Detail{}, errs.ErrUnavailable
	}
	builds := make([]entity.ImageBuild, 0)
	for rows.Next() {
		build, scanErr := scanBuild(rows)
		if scanErr != nil {
			rows.Close()
			return roleimagerepo.Detail{}, errs.ErrUnavailable
		}
		builds = append(builds, build)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return roleimagerepo.Detail{}, errs.ErrUnavailable
	}
	var activeArtifact *entity.ImageArtifact
	if recipe.ActiveImageArtifactRef != "" {
		item, artifactErr := scanRoleImageArtifact(querier.QueryRow(ctx, queryRoleImagesGetActiveArtifact,
			current.organizationID, recipe.ActiveImageArtifactRef))
		if artifactErr != nil {
			return roleimagerepo.Detail{}, errs.ErrUnavailable
		}
		activeArtifact = &item
	}
	var promotionCandidate *entity.ImageArtifact
	var candidateCanBePromoted bool
	item, candidateErr := scanRoleImageArtifactWith(querier.QueryRow(ctx, queryRoleImagesGetPromotionCandidate,
		current.organizationID, internalID), &candidateCanBePromoted)
	if candidateErr == nil {
		promotionCandidate = &item
		if canPromote && candidateCanBePromoted {
			recipe.NextActions = append(recipe.NextActions, "PROMOTE")
		}
	} else if !errors.Is(candidateErr, pgx.ErrNoRows) {
		return roleimagerepo.Detail{}, errs.ErrUnavailable
	}
	if err := hydrateRoleImageManagedLineage(ctx, querier, current.organizationID, &recipe); err != nil {
		return roleimagerepo.Detail{}, err
	}
	for index := range builds {
		if err := hydrateRoleImageBuildRevision(ctx, querier, current.organizationID, &builds[index]); err != nil {
			return roleimagerepo.Detail{}, err
		}
	}
	return roleimagerepo.Detail{
		Recipe: recipe, Builds: builds, ActiveArtifact: activeArtifact,
		PromotionCandidate: promotionCandidate,
	}, nil
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
	if err := repository.authorizeRoleImageManage(ctx, tx, current, input); err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	var replay roleimagerepo.ManageResult
	if found, receiptErr := repository.loadRoleImageReceipt(ctx, tx, current,
		input.Mutation.Operation, input.Mutation.IdempotencyKey, input.Mutation.IntentDigest, &replay); receiptErr != nil {
		return roleimagerepo.ManageResult{}, receiptErr
	} else if found {
		if err := repository.projectRoleImageManageSource(ctx, tx, current, &replay); err != nil {
			return roleimagerepo.ManageResult{}, err
		}
		if err := committed(tx, ctx); err != nil {
			return roleimagerepo.ManageResult{}, err
		}
		return replay, nil
	}

	managed, err := repository.managedRoleImageTarget(ctx, tx, current, input)
	if err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	result, projectID, projectRef, err := repository.applyRoleImageManage(ctx, tx, current, input)
	if err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	if err := repository.auditRoleImage(ctx, tx, current, projectID, input.Mutation.Operation,
		"ROLE_IMAGE_RECIPE", result.Recipe.Ref, "i18n:ROLE_IMAGE_RECIPE_CHANGED"); err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	if err := repository.recordManagedRoleImageCommand(ctx, tx, current, input, managed, result); err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	if err := hydrateRoleImageManagedLineage(ctx, tx, current.organizationID, &result.Recipe); err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	if result.Build != nil {
		if err := hydrateRoleImageBuildRevision(ctx, tx, current.organizationID, result.Build); err != nil {
			return roleimagerepo.ManageResult{}, err
		}
	}
	if err := repository.emitPlatformEvent(ctx, tx, current, "ROLE_IMAGE_RECIPE_CHANGED",
		projectRef, result.Recipe.Ref, "i18n:ROLE_IMAGE_RECIPE_CHANGED"); err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	if err := repository.storeRoleImageReceipt(ctx, tx, current, input.Mutation.Operation,
		input.Mutation.IdempotencyKey, input.Mutation.IntentDigest, "ROLE_IMAGE_MANAGE", result); err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	if err := repository.projectRoleImageManageSource(ctx, tx, current, &result); err != nil {
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
			input.ProjectRef, input.RoleDefinitionRef).Scan(&projectID, &roleID); errors.Is(err, pgx.ErrNoRows) {
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
		detail, err := repository.getRoleImageRecipe(ctx, tx, current, ref, true, false)
		if err != nil {
			return roleimagerepo.ManageResult{}, "", "", err
		}
		recipe := detail.Recipe
		build, err := repository.insertRoleImageBuild(ctx, tx, current, recipeID, recipe)
		return roleImageManageResult(recipe, build, nil, false), projectID, input.ProjectRef, err
	case "UPDATE", "ARCHIVE", "RESTORE", "REQUEST_BUILD":
		locked, err := scanLockedRecipe(tx.QueryRow(ctx, queryRoleImagesLockRecipe,
			current.organizationID, input.RecipeRef))
		if errors.Is(err, pgx.ErrNoRows) {
			return roleimagerepo.ManageResult{}, "", "", errs.ErrNotFound
		}
		if err != nil {
			return roleimagerepo.ManageResult{}, "", "", errs.ErrUnavailable
		}
		if input.ProjectRef != locked.Recipe.ProjectRef {
			return roleimagerepo.ManageResult{}, "", "", errs.ErrNotFound
		}
		if shippedRoleImage(locked.Recipe) {
			return roleimagerepo.ManageResult{}, "", "", errs.ErrConflict
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
			if _, err := tx.Exec(ctx, queryRoleImagesCancelOpenPromotions, current.organizationID, locked.ID); err != nil {
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
			if input.Action == "ARCHIVE" {
				if _, err := tx.Exec(ctx, queryRoleImagesCancelOpenPromotions, current.organizationID, locked.ID); err != nil {
					return roleimagerepo.ManageResult{}, "", "", errs.ErrUnavailable
				}
			}
			state := "ARCHIVED"
			if input.Action == "RESTORE" {
				state = "ACTIVE"
			}
			if _, err := tx.Exec(ctx, queryRoleImagesChangeRecipeState, current.organizationID, locked.ID, state); err != nil {
				return roleimagerepo.ManageResult{}, "", "", mapRoleImageWriteError(err)
			}
		}
		detail, err := repository.getRoleImageRecipe(ctx, tx, current, input.RecipeRef, true, false)
		if err != nil {
			return roleimagerepo.ManageResult{}, "", "", err
		}
		recipe, activeArtifact := detail.Recipe, detail.ActiveArtifact
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

func (repository *Repository) authorizeRoleImageManage(ctx context.Context, tx pgx.Tx, current scope, input roleimagerepo.ManageInput) error {
	var target resolvedAccessTarget
	var err error
	if input.Action == "CREATE" {
		target, err = repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{
			ProjectRef: input.ProjectRef, ResourceKind: "PROJECT", ResourceRef: input.ProjectRef,
		})
	} else {
		target, err = repository.resolveRoleImageAccessTarget(ctx, tx, current, input.RecipeRef, input.ProjectRef)
	}
	if err != nil {
		return err
	}
	authorization, err := repository.loadRoleImageAccessContext(ctx, tx, current)
	if err != nil {
		return err
	}
	if !authorization.allowed("image.build", target) {
		return errs.ErrNotFound
	}
	if (input.Action == "CREATE" || input.Action == "UPDATE") &&
		(!authorization.allowed("image.source.view", target) || !authorization.allowed("image.source.manage", target)) {
		return errs.ErrNotFound
	}
	return nil
}

func (repository *Repository) resolveRoleImageAccessTarget(ctx context.Context, querier roleImageQuerier, current scope, ref, expectedProjectRef string) (resolvedAccessTarget, error) {
	var target resolvedAccessTarget
	if err := querier.QueryRow(ctx, queryRoleImagesResolveAccessTarget, current.organizationID, ref).Scan(
		&target.resourceID, &target.projectID, &target.scope.ProjectRef, &target.ownerSubjectRef,
	); errors.Is(err, pgx.ErrNoRows) {
		return resolvedAccessTarget{}, errs.ErrNotFound
	} else if err != nil {
		return resolvedAccessTarget{}, errs.ErrUnavailable
	}
	if expectedProjectRef != "" && expectedProjectRef != target.scope.ProjectRef {
		return resolvedAccessTarget{}, errs.ErrNotFound
	}
	target.scope = roleImageAccessTarget(ref, target.scope.ProjectRef, target.ownerSubjectRef).scope
	return target, nil
}

type roleImageAccessContext struct {
	subject            resolvedAccessSubject
	bindings           []entity.AccessBinding
	evaluatedAt        time.Time
	authorityProjectID string
}

func (repository *Repository) loadRoleImageAccessContext(ctx context.Context, tx pgx.Tx, current scope) (roleImageAccessContext, error) {
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef)
	if err != nil {
		return roleImageAccessContext{}, err
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return roleImageAccessContext{}, err
	}
	return roleImageAccessContext{subject: subject, bindings: bindings, evaluatedAt: time.Now().UTC(), authorityProjectID: current.authorityProjectID}, nil
}

func (authorization roleImageAccessContext) allowed(permission string, target resolvedAccessTarget) bool {
	if authorization.authorityProjectID != "" && target.projectID != "" && authorization.authorityProjectID != target.projectID {
		return false
	}
	return accessservice.Evaluate(authorization.subject.AccessSubject, permission, target.scope,
		target.ownerSubjectRef, authorization.bindings, authorization.evaluatedAt).Allowed
}

func roleImageAccessTarget(ref, projectRef, ownerSubjectRef string) resolvedAccessTarget {
	return resolvedAccessTarget{ownerSubjectRef: ownerSubjectRef, scope: entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ProjectRef: projectRef, ResourceKind: "ROLE_IMAGE", ResourceRef: ref,
		RelatedResourceRefs: map[string]string{"PROJECT": projectRef},
	}}
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
