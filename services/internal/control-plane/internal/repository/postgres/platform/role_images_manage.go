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

// roleImageListCursor pins the list filter commitment, last recipe reference and update time for
// stable keyset pagination; the cursor itself does not grant access.
// roleImageListCursor закрепляет list filter commitment, последнюю recipe reference и update time
// для устойчивой keyset pagination; сам cursor не выдаёт доступ.
type roleImageListCursor struct {
	Filter    string
	Ref       string
	UpdatedAt time.Time
}

// List forces PROJECT scope before invoking the shared role-image list so this entrypoint cannot
// infer organization scope from an empty project.
// List принудительно задаёт PROJECT scope перед общим role-image list, исключая вывод organization
// scope по пустому проекту в этом entrypoint.
func (repository *Repository) List(ctx context.Context, principal value.Principal, filter roleimagerepo.Filter) ([]entity.RoleImageRecipe, string, int64, error) {
	filter.ScopeKind = "PROJECT"
	return repository.listRoleImages(ctx, principal, filter)
}

// listRoleImages validates query/state/cursor and uses one read-only repeatable-read snapshot for
// count, scoped rows, access hints and source/managed-lineage projection; commits before returning
// the bounded page.
// listRoleImages проверяет query/state/cursor и использует один read-only repeatable-read snapshot
// для count, scoped rows, access hints и source/managed-lineage projection; выполняет commit до
// выдачи ограниченной страницы.
func (repository *Repository) listRoleImages(ctx context.Context, principal value.Principal, filter roleimagerepo.Filter) ([]entity.RoleImageRecipe, string, int64, error) {
	if len(filter.Query) > 128 || !utf8.ValidString(filter.Query) || strings.ContainsRune(filter.Query, 0) || (filter.State != "" && filter.State != "ACTIVE" && filter.State != "ARCHIVED") {
		return nil, "", 0, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", 0, err
	}
	filterDigest := roleImageDigest([]string{current.organizationID, current.actorID, current.authorityProjectID, filter.ScopeKind, filter.ProjectRef, filter.RoleDefinitionRef, filter.Query, filter.State})
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
	if filter.ScopeKind == "ORGANIZATION" {
		if err := repository.requireOrganizationRoleImageAccess(ctx, tx, current); err != nil {
			return nil, "", 0, err
		}
	} else {
		if _, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{ProjectRef: filter.ProjectRef, ResourceKind: "PROJECT", ResourceRef: filter.ProjectRef}); err != nil {
			return nil, "", 0, err
		}
	}
	authorization, err := repository.loadRoleImageAccessContext(ctx, tx, current)
	if err != nil {
		return nil, "", 0, err
	}
	if err := tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&authorization.evaluatedAt); err != nil {
		return nil, "", 0, errs.ErrUnavailable
	}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project": current.authorityProjectID, "project_ref": filter.ProjectRef, "role_ref": filter.RoleDefinitionRef, "query": filter.Query, "state": filter.State, "scope_kind": filter.ScopeKind}
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
		if item.ScopeKind == "PROJECT" && !authorization.allowed("project.view", target) {
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

// Get selects the PROJECT detail path with fresh owner access and source-redaction checks
// performed by the scoped reader.
// Get выбирает PROJECT detail path со свежими owner access и source-redaction checks внутри scoped
// reader.
func (repository *Repository) Get(ctx context.Context, principal value.Principal, ref string) (roleimagerepo.Detail, error) {
	return repository.getScopedRoleImage(ctx, principal, ref, "PROJECT")
}

// getScopedRoleImage resolves exact scope and current permissions in a read-only repeatable-read
// transaction, reads the recipe detail and redacts source independently of build/promote hints
// before commit.
// getScopedRoleImage разрешает точную область и текущие permissions в read-only repeatable-read
// транзакции, читает recipe detail и скрывает source независимо от build/promote hints до commit.
func (repository *Repository) getScopedRoleImage(ctx context.Context, principal value.Principal, ref, scopeKind string) (roleimagerepo.Detail, error) {
	current, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return roleimagerepo.Detail{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if scopeKind == "ORGANIZATION" {
		if err := repository.requireOrganizationRoleImageAccess(ctx, tx, current); err != nil {
			return roleimagerepo.Detail{}, err
		}
	}
	target, err := repository.resolveScopedRoleImageAccessTarget(ctx, tx, current, ref, "", scopeKind)
	if err != nil {
		return roleimagerepo.Detail{}, err
	}
	authorization, err := repository.loadRoleImageAccessContext(ctx, tx, current)
	if err != nil {
		return roleimagerepo.Detail{}, err
	}
	if scopeKind == "PROJECT" && !authorization.allowed("project.view", target) {
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

// roleImageQuerier abstracts only row and rows queries so recipe projections can share a pool or
// caller-owned transaction without owning its lifetime.
// roleImageQuerier обобщает только row и rows queries для общих recipe projections через pool либо
// транзакцию caller без владения её жизненным циклом.
type roleImageQuerier interface {
	// QueryRow runs the exact SQL and arguments as a lazy pgx.Row; the projection caller handles Scan
	// failure and ownership validation.
	// QueryRow выполняет точные SQL и аргументы как отложенный pgx.Row; projection caller обрабатывает
	// Scan failure и проверку владения.
	QueryRow(context.Context, string, ...any) pgx.Row
	// Query returns iterable rows for the exact SQL and arguments; the consuming projection closes
	// rows and checks scan/iteration errors.
	// Query возвращает iterable rows точных SQL и аргументов; projection consumer закрывает rows и
	// проверяет scan/iteration errors.
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// getRoleImageRecipe builds exact recipe/build/artifact/risk/managed-lineage detail with existing
// action hints, treating absent optional candidate/failure as normal and other projection errors
// as failures.
// getRoleImageRecipe собирает точные recipe/build/artifact/risk/managed-lineage detail с прежними
// action hints, считая отсутствие необязательных candidate/failure нормальным, а прочие ошибки
// projection — отказом.
func (repository *Repository) getRoleImageRecipe(ctx context.Context, querier roleImageQuerier, current scope, ref string, canBuild, canPromote bool) (roleimagerepo.Detail, error) {
	row := querier.QueryRow(ctx, queryRoleImagesGetRecipe, current.organizationID, ref)
	var internalID string
	var recipe entity.RoleImageRecipe
	var specification []byte
	err := row.Scan(&internalID, &recipe.Ref, &recipe.ProjectRef, &recipe.RoleDefinitionRef,
		&recipe.Name, &recipe.State, &specification, &recipe.Generation, &recipe.SpecSHA256,
		&recipe.PolicyRevision, &recipe.PolicySHA256, &recipe.RoleRuntimeContractRevision,
		&recipe.RoleRuntimeContractSHA256, &recipe.ActiveImageArtifactRef,
		&recipe.PromotedImageReference, &recipe.Version, &recipe.CreatedAt, &recipe.UpdatedAt,
		&recipe.ScopeKind, &recipe.OrganizationRef)
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
	if canBuild && recipe.State == "ACTIVE" {
		for _, build := range builds {
			if build.Stage != "COMPLETED" && build.Stage != "CANCELLED" && build.Stage != "DEAD_LETTER" {
				recipe.NextActions = append(recipe.NextActions, "CANCEL_BUILD")
				break
			}
		}
	}
	var activeArtifact *entity.ImageArtifact
	if recipe.ActiveImageArtifactRef != "" {
		item, artifactErr := scanRoleImageArtifact(querier.QueryRow(ctx, queryRoleImagesGetActiveArtifact,
			current.organizationID, recipe.ActiveImageArtifactRef))
		if artifactErr != nil {
			return roleimagerepo.Detail{}, errs.ErrUnavailable
		}
		activeArtifact = &item
		if err := repository.hydrateImageRiskHistory(ctx, querier, current, activeArtifact); err != nil {
			return roleimagerepo.Detail{}, err
		}
	}
	var promotionCandidate *entity.ImageArtifact
	var candidateCanBePromoted bool
	item, candidateErr := scanRoleImageArtifactWith(querier.QueryRow(ctx, queryRoleImagesGetPromotionCandidate,
		current.organizationID, internalID), &candidateCanBePromoted)
	if candidateErr == nil {
		promotionCandidate = &item
		if err := repository.hydrateImageRiskHistory(ctx, querier, current, promotionCandidate); err != nil {
			return roleimagerepo.Detail{}, err
		}
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
	var failure entity.RoleImageAdmissionFailure
	err = querier.QueryRow(ctx, queryRoleImagesGetAdmissionFailure, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "recipe_id": internalID,
	}).Scan(&failure.ImageArtifactRef, &failure.Version, &failure.RecipeRef, &failure.RecipeGeneration,
		&failure.BuildRef, &failure.BuildAttempt, &failure.ScopeKind, &failure.OrganizationRef, &failure.ProjectRef,
		&failure.State, &failure.ErrorCode)
	var admissionFailure *entity.RoleImageAdmissionFailure
	if err == nil {
		admissionFailure = &failure
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return roleimagerepo.Detail{}, errs.ErrUnavailable
	}
	return roleimagerepo.Detail{
		Recipe: recipe, Builds: builds, ActiveArtifact: activeArtifact,
		PromotionCandidate: promotionCandidate, AdmissionFailure: admissionFailure,
	}, nil
}

// Manage forces PROJECT scope and delegates the complete owner-managed recipe command, preserving
// its authorization and transaction path.
// Manage принудительно задаёт PROJECT scope и передаёт полную owner-managed recipe command с
// сохранением authorization и transaction path.
func (repository *Repository) Manage(ctx context.Context, input roleimagerepo.ManageInput) (roleimagerepo.ManageResult, error) {
	input.ScopeKind = "PROJECT"
	return repository.manageScopedRoleImage(ctx, input)
}

// manageScopedRoleImage authorizes the scoped command before receipt replay, otherwise applies the
// recipe transition, audit, managed source lineage, event and receipt in one serializable
// transaction; source visibility is reprojected before return.
// manageScopedRoleImage авторизует scoped command до receipt replay, иначе применяет recipe
// transition, audit, managed source lineage, event и receipt в одной serializable транзакции;
// source visibility вычисляется повторно перед выдачей.
func (repository *Repository) manageScopedRoleImage(ctx context.Context, input roleimagerepo.ManageInput) (roleimagerepo.ManageResult, error) {
	current, err := repository.resolveScope(ctx, input.Principal)
	if err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return roleimagerepo.ManageResult{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if input.ScopeKind == "ORGANIZATION" {
		if err := repository.requireOrganizationRoleImageAccess(ctx, tx, current); err != nil {
			return roleimagerepo.ManageResult{}, err
		}
	}
	if err := repository.authorizeRoleImageManage(ctx, tx, current, input); err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	var replay roleimagerepo.ManageResult
	if found, receiptErr := repository.loadRoleImageReceipt(ctx, tx, current,
		input.Mutation.Operation, input.Mutation.IdempotencyKey, input.Mutation.IntentDigest, &replay); receiptErr != nil {
		return roleimagerepo.ManageResult{}, receiptErr
	} else if found {
		if err := repository.projectRoleImageManageSource(ctx, tx, current, &replay, input.ScopeKind); err != nil {
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
	if err := repository.projectRoleImageManageSource(ctx, tx, current, &result, input.ScopeKind); err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return roleimagerepo.ManageResult{}, err
	}
	return result, nil
}

// applyRoleImageManage applies the closed create/update/archive/restore/request-build/cancel-build
// actions with exact scope and recipe OCC; cancellation invalidates open work, policy refresh
// preserves server pins and matching promoted artifacts can satisfy request-build without a new
// build.
// applyRoleImageManage применяет закрытые create/update/archive/restore/request-build/cancel-build
// actions с точными scope и recipe OCC; отмена закрывает open work, policy refresh сохраняет
// server pins, а совпадающий promoted artifact может исполнить request-build без новой сборки.
func (repository *Repository) applyRoleImageManage(ctx context.Context, tx pgx.Tx, current scope, input roleimagerepo.ManageInput) (roleimagerepo.ManageResult, string, string, error) {
	specification := asJSON(input.Recipe)
	specSHA256 := roleImageDigest(input.Recipe)
	switch input.Action {
	case "CREATE":
		var projectID, roleID string
		var ownerErr error
		if input.ScopeKind == "ORGANIZATION" {
			ownerErr = tx.QueryRow(ctx, queryOrganizationRoleImageResolveRole, current.organizationID).Scan(&roleID)
		} else {
			ownerErr = tx.QueryRow(ctx, queryRoleImagesResolveProjectRole, current.organizationID,
				input.ProjectRef, input.RoleDefinitionRef).Scan(&projectID, &roleID)
		}
		if errors.Is(ownerErr, pgx.ErrNoRows) {
			return roleimagerepo.ManageResult{}, "", "", errs.ErrNotFound
		} else if ownerErr != nil {
			return roleimagerepo.ManageResult{}, "", "", errs.ErrUnavailable
		}
		ref, _ := newRef("imgrec")
		var recipeID string
		if err := tx.QueryRow(ctx, queryRoleImagesInsertRecipe, ref, current.organizationID,
			projectID, roleID, input.Name, specification, specSHA256,
			repository.roleImages.PolicyRevision, repository.roleImages.PolicySHA256,
			repository.roleImages.RoleRuntimeContractRevision,
			repository.roleImages.RoleRuntimeContractSHA256, current.actorID, input.ScopeKind).Scan(&recipeID); err != nil {
			return roleimagerepo.ManageResult{}, "", "", mapRoleImageWriteError(err)
		}
		detail, err := repository.getRoleImageRecipe(ctx, tx, current, ref, true, false)
		if err != nil {
			return roleimagerepo.ManageResult{}, "", "", err
		}
		recipe := detail.Recipe
		build, err := repository.insertRoleImageBuild(ctx, tx, current, recipeID, recipe)
		return roleImageManageResult(recipe, build, nil, false), projectID, input.ProjectRef, err
	case "UPDATE", "ARCHIVE", "RESTORE", "REQUEST_BUILD", "CANCEL_BUILD":
		locked, err := scanLockedRecipe(tx.QueryRow(ctx, queryRoleImagesLockRecipe,
			current.organizationID, input.RecipeRef))
		if errors.Is(err, pgx.ErrNoRows) {
			return roleimagerepo.ManageResult{}, "", "", errs.ErrNotFound
		}
		if err != nil {
			return roleimagerepo.ManageResult{}, "", "", errs.ErrUnavailable
		}
		if input.ProjectRef != locked.Recipe.ProjectRef || input.ScopeKind != locked.Recipe.ScopeKind {
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
		if input.Action == "CANCEL_BUILD" {
			build, err := scanBuild(tx.QueryRow(ctx, queryRoleImagesCancelExactBuild,
				current.organizationID, locked.ID, input.BuildRef))
			if errors.Is(err, pgx.ErrNoRows) {
				return roleimagerepo.ManageResult{}, "", "", errs.ErrNotFound
			}
			if err != nil {
				return roleimagerepo.ManageResult{}, "", "", errs.ErrUnavailable
			}
			detail, err := repository.getRoleImageRecipe(ctx, tx, current, input.RecipeRef, true, false)
			if err != nil {
				return roleimagerepo.ManageResult{}, "", "", err
			}
			return roleImageManageResult(detail.Recipe, &build, detail.ActiveArtifact, false), locked.ProjectID, locked.Recipe.ProjectRef, nil
		}
		if _, err := tx.Exec(ctx, queryRoleImagesCancelOpenBuilds, current.organizationID, locked.ID); err != nil {
			return roleimagerepo.ManageResult{}, "", "", errs.ErrUnavailable
		}
		if input.Action == "UPDATE" {
			if _, err := tx.Exec(ctx, queryRoleImagesCancelOpenPromotions, current.organizationID, locked.ID); err != nil {
				return roleimagerepo.ManageResult{}, "", "", errs.ErrUnavailable
			}
			if _, err := tx.Exec(ctx, queryRoleImagesUpdateRecipe, current.organizationID, locked.ID,
				input.Name, specification, specSHA256, repository.roleImages.PolicyRevision,
				repository.roleImages.PolicySHA256, repository.roleImages.RoleRuntimeContractRevision,
				repository.roleImages.RoleRuntimeContractSHA256); err != nil {
				return roleimagerepo.ManageResult{}, "", "", mapRoleImageWriteError(err)
			}
		} else if input.Action == "REQUEST_BUILD" {
			// Повторный запрос всегда заменяет незавершённую сборку. Если платформа
			// обновила policy или runtime contract, одновременно выпускаем новую
			// серверную ревизию рецепта: worker не должен бесконечно забирать
			// заведомо устаревшую задачу и блокировать всю очередь.
			if !repository.roleImageRecipePolicyCurrent(locked.Recipe) {
				if _, err := tx.Exec(ctx, queryRoleImagesCancelOpenPromotions, current.organizationID, locked.ID); err != nil {
					return roleimagerepo.ManageResult{}, "", "", errs.ErrUnavailable
				}
				if _, err := tx.Exec(ctx, queryRoleImagesRefreshRecipePolicy,
					current.organizationID, locked.ID,
					repository.roleImages.PolicyRevision, repository.roleImages.PolicySHA256,
					repository.roleImages.RoleRuntimeContractRevision,
					repository.roleImages.RoleRuntimeContractSHA256); err != nil {
					return roleimagerepo.ManageResult{}, "", "", mapRoleImageWriteError(err)
				}
			}
		} else if input.Action == "ARCHIVE" || input.Action == "RESTORE" {
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

// roleImageRecipePolicyCurrent compares all four recipe admission-policy and role-runtime-contract
// revision/digest pins against the currently configured server policy.
// roleImageRecipePolicyCurrent сравнивает все четыре recipe admission-policy и
// role-runtime-contract revision/digest pins с текущей настроенной policy сервера.
func (repository *Repository) roleImageRecipePolicyCurrent(recipe entity.RoleImageRecipe) bool {
	return recipe.PolicyRevision == repository.roleImages.PolicyRevision &&
		recipe.PolicySHA256 == repository.roleImages.PolicySHA256 &&
		recipe.RoleRuntimeContractRevision == repository.roleImages.RoleRuntimeContractRevision &&
		recipe.RoleRuntimeContractSHA256 == repository.roleImages.RoleRuntimeContractSHA256
}

// authorizeRoleImageManage resolves a create target or existing exact recipe target, requires
// image.build and additionally source view/manage for create/update; denial maps to not found to
// preserve resource privacy.
// authorizeRoleImageManage разрешает create target либо существующую точную recipe target, требует
// image.build и дополнительно source view/manage для create/update; отказ даёт not found для
// сохранения закрытости ресурса.
func (repository *Repository) authorizeRoleImageManage(ctx context.Context, tx pgx.Tx, current scope, input roleimagerepo.ManageInput) error {
	var target resolvedAccessTarget
	var err error
	if input.Action == "CREATE" {
		if input.ScopeKind == "ORGANIZATION" {
			target = resolvedAccessTarget{scope: entity.AccessScope{Kind: "RESOURCE_KIND", ResourceKind: "ROLE_IMAGE"}}
		} else {
			target, err = repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{
				ProjectRef: input.ProjectRef, ResourceKind: "PROJECT", ResourceRef: input.ProjectRef,
			})
		}
	} else {
		target, err = repository.resolveScopedRoleImageAccessTarget(ctx, tx, current, input.RecipeRef, input.ProjectRef, input.ScopeKind)
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

// resolveRoleImageAccessTarget selects the PROJECT target resolver, retaining the optional
// expected-project check rather than treating caller locators as authority.
// resolveRoleImageAccessTarget выбирает PROJECT target resolver с сохранением необязательной
// проверки expected project вместо доверия caller locators как полномочиям.
func (repository *Repository) resolveRoleImageAccessTarget(ctx context.Context, querier roleImageQuerier, current scope, ref, expectedProjectRef string) (resolvedAccessTarget, error) {
	return repository.resolveScopedRoleImageAccessTarget(ctx, querier, current, ref, expectedProjectRef, "PROJECT")
}

// resolveScopedRoleImageAccessTarget reads exact tenant/reference/scope owner and project IDs,
// rejects a conflicting expected project and constructs the role-image access scope from
// authoritative row values.
// resolveScopedRoleImageAccessTarget читает точные tenant/reference/scope owner и project IDs,
// отклоняет конфликтующий expected project и строит role-image access scope по авторитетной
// строке.
func (repository *Repository) resolveScopedRoleImageAccessTarget(ctx context.Context, querier roleImageQuerier, current scope, ref, expectedProjectRef, scopeKind string) (resolvedAccessTarget, error) {
	var target resolvedAccessTarget
	if err := querier.QueryRow(ctx, queryRoleImagesResolveAccessTarget, current.organizationID, ref, scopeKind).Scan(
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

// roleImageAccessContext holds the resolved subject, current bindings, one evaluation time and
// optional authority project for repeated permission checks within a reader or command.
// roleImageAccessContext хранит разрешённый subject, текущие bindings, единое evaluation time и
// необязательный authority project для повторных permission checks внутри reader либо command.
type roleImageAccessContext struct {
	subject            resolvedAccessSubject
	bindings           []entity.AccessBinding
	evaluatedAt        time.Time
	authorityProjectID string
}

// loadRoleImageAccessContext loads the current subject and access bindings under the caller
// transaction and captures UTC evaluation time plus the trusted project authority bound.
// loadRoleImageAccessContext загружает текущие subject и access bindings в транзакции caller и
// фиксирует UTC evaluation time и доверенную границу project authority.
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

// allowed rejects a conflicting nonempty target project before delegating
// subject/owner/scope/bindings to the shared access evaluator at the captured time.
// allowed отклоняет конфликтующий непустой target project до передачи subject/owner/scope/bindings
// общему access evaluator на зафиксированное время.
func (authorization roleImageAccessContext) allowed(permission string, target resolvedAccessTarget) bool {
	if authorization.authorityProjectID != "" && target.projectID != "" && authorization.authorityProjectID != target.projectID {
		return false
	}
	return accessservice.Evaluate(authorization.subject.AccessSubject, permission, target.scope,
		target.ownerSubjectRef, authorization.bindings, authorization.evaluatedAt).Allowed
}

// roleImageAccessTarget constructs a ROLE_IMAGE resource-instance scope with an optional related
// PROJECT locator and owner subject hint, without resolving database identity or granting
// permission.
// roleImageAccessTarget строит ROLE_IMAGE resource-instance scope с необязательным связанным
// PROJECT locator и owner subject hint без разрешения database identity либо выдачи permission.
func roleImageAccessTarget(ref, projectRef, ownerSubjectRef string) resolvedAccessTarget {
	var related map[string]string
	if projectRef != "" {
		related = map[string]string{"PROJECT": projectRef}
	}
	return resolvedAccessTarget{ownerSubjectRef: ownerSubjectRef, scope: entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ProjectRef: projectRef, ResourceKind: "ROLE_IMAGE", ResourceRef: ref,
		RelatedResourceRefs: related,
	}}
}

// insertRoleImageBuild commits recipe/owner/policy/runtime input into an immutable build digest,
// inserts the build and reads its exact projection from the caller transaction; absent readback
// yields unavailable.
// insertRoleImageBuild закрепляет recipe/owner/policy/runtime input в immutable build digest,
// добавляет build и читает его точную projection в транзакции caller; отсутствие readback даёт
// unavailable.
func (repository *Repository) insertRoleImageBuild(ctx context.Context, tx pgx.Tx, current scope, recipeID string, recipe entity.RoleImageRecipe) (*entity.ImageBuild, error) {
	immutable := roleImageDigest(struct {
		ScopeKind, OrganizationRef, ProjectRef string
		Input                                  entity.RoleImageRecipeInput
		SpecSHA256, PolicySHA256               string
		PolicyRevision                         uint64
		ContractRevision                       uint64
		ContractSHA256                         string
	}{recipe.ScopeKind, recipe.OrganizationRef, recipe.ProjectRef, recipe.Input, recipe.SpecSHA256, recipe.PolicySHA256, recipe.PolicyRevision,
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

// decodeJSON delegates to encoding/json.Unmarshal without additional unknown-field rejection,
// canonicalization or owner validation.
// decodeJSON передаёт декодирование encoding/json.Unmarshal без дополнительного запрета unknown
// fields, канонизации либо проверки владельца.
func decodeJSON(raw []byte, target any) error {
	return json.Unmarshal(raw, target)
}
