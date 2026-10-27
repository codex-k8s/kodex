package platform

import (
	"context"
	"errors"
	"reflect"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// assistantRoleImageManageInput validates project/agent/version/environment input, resolves exact
// agent.view authority and the active agent role/version, then resolves and validates a catalog
// recipe. It returns a PROJECT/CREATE input without performing a write; the caller separately
// authorizes image management.
// assistantRoleImageManageInput проверяет project/agent/version/environment input, разрешает
// точные agent.view полномочия и роль/версию активного агента, затем разрешает и проверяет recipe
// каталога. Возвращает PROJECT/CREATE input без записи; вызывающий метод отдельно проверяет
// полномочия управления образом.
func (repository *Repository) assistantRoleImageManageInput(
	ctx context.Context, tx pgx.Tx, current scope, payload command.AssistantRoleImageRecipeInput,
) (roleimagerepo.ManageInput, error) {
	if payload.ProjectRef == "" || payload.AgentRef == "" || payload.AgentVersion < 1 ||
		payload.Environment.EnvironmentKey == "" || repository.roleImageCatalogResolver == nil {
		return roleimagerepo.ManageInput{}, errs.ErrInvalid
	}
	_, target, err := repository.resolveCommandTarget(ctx, tx, current, "agent.view", "AGENT", payload.AgentRef, payload.ProjectRef)
	if err != nil {
		return roleimagerepo.ManageInput{}, err
	}
	if err := repository.requireAccess(ctx, tx, current, "agent.view", target); err != nil {
		return roleimagerepo.ManageInput{}, err
	}
	var roleRef string
	var version int64
	if err := tx.QueryRow(ctx, queryConfigurationAssistantRoleImageAgent, current.organizationID,
		payload.ProjectRef, payload.AgentRef).Scan(&roleRef, &version); errors.Is(err, pgx.ErrNoRows) {
		return roleimagerepo.ManageInput{}, errs.ErrNotFound
	} else if err != nil {
		return roleimagerepo.ManageInput{}, errs.ErrUnavailable
	}
	if version != payload.AgentVersion {
		return roleimagerepo.ManageInput{}, errs.ErrVersionMismatch
	}
	recipe, err := repository.roleImageCatalogResolver(payload.Environment)
	if err != nil || roleimageservice.ValidateManagedRecipe(payload.ProjectRef, roleRef, payload.Name, recipe) != nil {
		return roleimagerepo.ManageInput{}, errs.ErrInvalid
	}
	return roleimagerepo.ManageInput{ScopeKind: "PROJECT", Action: "CREATE", ProjectRef: payload.ProjectRef,
		RoleDefinitionRef: roleRef, Name: payload.Name, Environment: payload.Environment, Recipe: recipe}, nil
}

// assistantImageUpdateSelection preserves the previous environment and exact Dockerfile bytes when
// omitted, resets Dockerfile to the server template on an explicit nonempty environmentKey, and
// accepts a separately supplied nonempty Dockerfile up to64KiB. Wrong types and explicit empty
// values fail instead of being treated as omission.
// assistantImageUpdateSelection сохраняет прежнее окружение и точные Dockerfile bytes при
// отсутствии полей, сбрасывает Dockerfile к server template при явном непустом environmentKey и
// принимает отдельно заданный непустой Dockerfile до64KiB. Неверные типы и явно пустые значения
// отклоняются, а не считаются отсутствующими.
func assistantImageUpdateSelection(parameters map[string]any, previous entity.RoleImageRecipeInput) (entity.RoleEnvironmentSelection, error) {
	selection := entity.RoleEnvironmentSelection{EnvironmentKey: previous.EnvironmentKey, Dockerfile: previous.Dockerfile}
	if supplied, exists := parameters["environmentKey"]; exists {
		key, valid := supplied.(string)
		if !valid || key == "" {
			return entity.RoleEnvironmentSelection{}, errs.ErrInvalid
		}
		selection.EnvironmentKey, selection.Dockerfile = key, ""
	}
	if supplied, exists := parameters["dockerfile"]; exists {
		dockerfile, valid := supplied.(string)
		if !valid || dockerfile == "" || len(dockerfile) > 64<<10 {
			return entity.RoleEnvironmentSelection{}, errs.ErrInvalid
		}
		selection.Dockerfile = dockerfile
	}
	return selection, nil
}

// authorizeAssistantRoleImage resolves the project agent/version and validated catalog input
// before applying the canonical role-image management permission check; it neither creates a
// recipe nor commits.
// authorizeAssistantRoleImage разрешает project agent/version и проверенный catalog input до
// канонической проверки полномочий управления образом; рецепт не создаёт и commit не выполняет.
func (repository *Repository) authorizeAssistantRoleImage(
	ctx context.Context, tx pgx.Tx, current scope, payload command.AssistantRoleImageRecipeInput,
) error {
	input, err := repository.assistantRoleImageManageInput(ctx, tx, current, payload)
	if err != nil {
		return err
	}
	return repository.authorizeRoleImageManage(ctx, tx, current, input)
}

// createAssistantRoleImage validates and authorizes a project assistant image, applies
// recipe/build creation with managed lineage in the caller transaction, and requires concrete
// recipe/build references. It returns created recipe and build runtime metadata with a
// recipe-change event descriptor; outer command handling owns audit/event/receipt and commit.
// createAssistantRoleImage проверяет и авторизует образ project assistant, создаёт recipe/build с
// managed lineage в транзакции вызывающего метода и требует конкретные recipe/build references.
// Возвращает созданный recipe и build runtime metadata с descriptor события изменения рецепта;
// внешняя обработка команды владеет audit/event/receipt и commit.
func (repository *Repository) createAssistantRoleImage(
	ctx context.Context, tx pgx.Tx, current scope, payload command.AssistantRoleImageRecipeInput,
) (commandOutcome, error) {
	input, err := repository.assistantRoleImageManageInput(ctx, tx, current, payload)
	if err != nil {
		return commandOutcome{}, err
	}
	if err := repository.authorizeRoleImageManage(ctx, tx, current, input); err != nil {
		return commandOutcome{}, err
	}
	result, projectID, projectRef, err := repository.applyAssistantImageManage(ctx, tx, current, input, nil)
	if err != nil {
		return commandOutcome{}, err
	}
	if result.Build == nil || result.Recipe.Ref == "" {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return commandOutcome{projectID: projectID, projectRef: projectRef,
		resourceKind: "ROLE_IMAGE_RECIPE", resourceRef: result.Recipe.Ref,
		summary: "i18n:ROLE_IMAGE_RECIPE_CHANGED", platformEvent: "ROLE_IMAGE_RECIPE_CHANGED",
		result: command.Result{CreatedRefs: []string{result.Recipe.Ref}, RuntimeItems: []map[string]any{{"imageBuildRef": result.Build.Ref,
			"imageBuildStage": result.Build.Stage}}}}, nil
}

// assistantRoleImageUpdateInput requires project/recipe/name/environment and positive expected
// version, authorizes management before reading source, then rejects foreign, inactive, shipped or
// hidden source and noneditable managed ownership. It verifies the saved recipe/digest without
// rebuilding history, retains same-environment package/tool/install options, resolves the new
// recipe and checks an optional exact specification commitment; caller execution applies OCC.
// assistantRoleImageUpdateInput требует project/recipe/name/environment и положительную expected
// version, проверяет management authority до чтения source, затем отклоняет foreign, inactive,
// shipped либо скрытый source и недоступную для правки managed ownership. Проверяет сохранённые
// recipe/digest без пересборки истории, сохраняет package/tool/install options того же окружения,
// разрешает новый рецепт и проверяет необязательный точный specification commitment; OCC исполняет
// вызывающая операция.
func (repository *Repository) assistantRoleImageUpdateInput(
	ctx context.Context, tx pgx.Tx, current scope, mutation value.Mutation, payload command.AssistantRoleImageUpdateInput,
) (roleimagerepo.ManageInput, entity.RoleImageRecipe, error) {
	if payload.ProjectRef == "" || payload.RecipeRef == "" || payload.Name == "" ||
		payload.Environment.EnvironmentKey == "" || mutation.ExpectedVersion == nil || *mutation.ExpectedVersion < 1 ||
		repository.roleImageCatalogResolver == nil {
		return roleimagerepo.ManageInput{}, entity.RoleImageRecipe{}, errs.ErrInvalid
	}
	input := roleimagerepo.ManageInput{ScopeKind: "PROJECT", Action: "UPDATE", RecipeRef: payload.RecipeRef,
		ProjectRef: payload.ProjectRef, Name: payload.Name, Environment: payload.Environment, Mutation: mutation}
	if err := repository.authorizeRoleImageManage(ctx, tx, current, input); err != nil {
		return roleimagerepo.ManageInput{}, entity.RoleImageRecipe{}, err
	}
	detail, err := repository.getRoleImageRecipe(ctx, tx, current, payload.RecipeRef, true, false)
	if err != nil {
		return roleimagerepo.ManageInput{}, entity.RoleImageRecipe{}, err
	}
	previous := detail.Recipe
	projectRoleImageSource(&previous, nil, true, true)
	if previous.ProjectRef != payload.ProjectRef || previous.State != "ACTIVE" || shippedRoleImage(previous) || !previous.SourceAvailable {
		return roleimagerepo.ManageInput{}, entity.RoleImageRecipe{}, errs.ErrConflict
	}
	if _, err := repository.managedRoleImageTarget(ctx, tx, current, input); err != nil {
		return roleimagerepo.ManageInput{}, entity.RoleImageRecipe{}, err
	}
	// Проверяем сохранённую спецификацию, не пересобирая её новым каталогом.
	// Новая спецификация отдельно закрепляется в подтверждаемом плане.
	if roleimageservice.ValidateManagedRecipe(previous.ProjectRef, previous.RoleDefinitionRef, previous.Name, previous.Input) != nil ||
		previous.SpecSHA256 != roleImageDigest(previous.Input) {
		return roleimagerepo.ManageInput{}, entity.RoleImageRecipe{}, errs.ErrConflict
	}
	input.Environment = inheritAssistantImageOptions(input.Environment, previous.Input)
	input.Recipe, err = repository.roleImageCatalogResolver(input.Environment)
	if err != nil || roleimageservice.ValidateManagedRecipe(payload.ProjectRef, previous.RoleDefinitionRef, payload.Name, input.Recipe) != nil {
		return roleimagerepo.ManageInput{}, entity.RoleImageRecipe{}, errs.ErrInvalid
	}
	if payload.SpecSHA256 != "" && (!exactSHA256(payload.SpecSHA256) || payload.SpecSHA256 != roleImageDigest(input.Recipe)) {
		return roleimagerepo.ManageInput{}, entity.RoleImageRecipe{}, errs.ErrConflict
	}
	return input, previous, nil
}

// updateAssistantRoleImage requires a typed update payload and exact confirmed specification
// digest, resolves and reauthorizes current input/managed ownership, and applies recipe/build plus
// lineage in the caller transaction. It requires a build and returns runtime build metadata with
// the existing event descriptor, without adding a created-recipe reference or committing.
// updateAssistantRoleImage требует typed update payload и точный подтверждённый specification
// digest, разрешает и повторно проверяет текущие input/managed ownership и применяет recipe/build
// с lineage в транзакции вызывающего метода. Требует build и возвращает runtime build metadata с
// прежним event descriptor без добавления created-recipe reference и без commit.
func (repository *Repository) updateAssistantRoleImage(
	ctx context.Context, tx pgx.Tx, current scope, input command.Command,
) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantRoleImageUpdateInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	if !exactSHA256(payload.SpecSHA256) {
		return commandOutcome{}, errs.ErrInvalid
	}
	managedInput, _, err := repository.assistantRoleImageUpdateInput(ctx, tx, current, input.Mutation, payload)
	if err != nil {
		return commandOutcome{}, err
	}
	managed, err := repository.managedRoleImageTarget(ctx, tx, current, managedInput)
	if err != nil {
		return commandOutcome{}, err
	}
	result, projectID, projectRef, err := repository.applyAssistantImageManage(ctx, tx, current, managedInput, managed)
	if err != nil {
		return commandOutcome{}, err
	}
	if result.Build == nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return commandOutcome{projectID: projectID, projectRef: projectRef,
		resourceKind: "ROLE_IMAGE_RECIPE", resourceRef: result.Recipe.Ref,
		summary: "i18n:ROLE_IMAGE_RECIPE_CHANGED", platformEvent: "ROLE_IMAGE_RECIPE_CHANGED",
		result: command.Result{RuntimeItems: []map[string]any{{"imageBuildRef": result.Build.Ref,
			"imageBuildStage": result.Build.Stage}}}}, nil
}

// hydrateAssistantRoleImageUpdate accepts only closed editable fields for the current project,
// authorizes exact source and reads an active nonshipped recipe, then validates the selected
// catalog update. It builds server-owned before/after/parameters, target and version pins with
// exact Dockerfile bytes and new spec digest, rejects a no-op and marks the plan operation
// selected without writing the recipe.
// hydrateAssistantRoleImageUpdate принимает только закрытый набор editable fields текущего
// проекта, проверяет точный source и читает active nonshipped recipe, затем проверяет выбранное
// обновление каталога. Формирует server-owned before/after/parameters, target и version pins с
// точными Dockerfile bytes и новым spec digest, отклоняет отсутствие изменений и отмечает
// operation выбранной без записи рецепта.
func (repository *Repository) hydrateAssistantRoleImageUpdate(
	ctx context.Context, tx pgx.Tx, current scope, projectRef string, operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if projectRef == "" || !onlyAssistantFields(operation.Parameters, "projectRef", "recipeRef", "name", "environmentKey", "dockerfile") {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	requestedProject := assistantString(operation.Parameters, "projectRef")
	if requestedProject != "" && requestedProject != "current" && requestedProject != projectRef {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	ref := assistantString(operation.Parameters, "recipeRef")
	if ref == "" {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	if err := repository.authorizeRoleImageManage(ctx, tx, current, roleimagerepo.ManageInput{
		ScopeKind: "PROJECT", Action: "UPDATE", ProjectRef: projectRef, RecipeRef: ref,
	}); err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	detail, err := repository.getRoleImageRecipe(ctx, tx, current, ref, true, false)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	previous := detail.Recipe
	projectRoleImageSource(&previous, nil, true, true)
	if previous.ProjectRef != projectRef || previous.State != "ACTIVE" || shippedRoleImage(previous) || !previous.SourceAvailable {
		return entity.AssistantPlanOperation{}, errs.ErrNotFound
	}
	name := previous.Name
	if supplied, exists := operation.Parameters["name"]; exists {
		var valid bool
		name, valid = supplied.(string)
		if !valid {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
	}
	selection, err := assistantImageUpdateSelection(operation.Parameters, previous.Input)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	version := int64(previous.Version)
	mutation := value.Mutation{ExpectedVersion: &version}
	managedInput, _, err := repository.assistantRoleImageUpdateInput(ctx, tx, current, mutation,
		command.AssistantRoleImageUpdateInput{ProjectRef: projectRef, RecipeRef: ref, Name: name,
			Environment: selection})
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	before := map[string]any{"projectRef": projectRef, "recipeRef": ref,
		"name": previous.Name, "environmentKey": previous.Input.EnvironmentKey,
		"dockerfile": previous.Input.Dockerfile, "specSha256": previous.SpecSHA256}
	after := cloneAssistantFields(before)
	after["name"], after["environmentKey"], after["dockerfile"] = name, selection.EnvironmentKey, managedInput.Recipe.Dockerfile
	after["specSha256"] = roleImageDigest(managedInput.Recipe)
	if reflect.DeepEqual(before, after) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "ROLE_IMAGE_RECIPE", Ref: ref, Name: previous.Name, Version: &version}
	operation.Parameters, operation.Before, operation.After = after, before, cloneAssistantFields(after)
	operation.ExpectedVersion = &version
	operation.Selected = true
	return operation, nil
}

// assistantRoleImageUpdateSnapshotMatches returns false for absent expected version, rejects
// malformed confirmed spec digest, and reuses current input/authority resolution before comparing
// exact recipe version, target version/name, original before fields and parameters/after equality.
// It is a read-side stale-plan check and does not apply changes.
// assistantRoleImageUpdateSnapshotMatches возвращает false без expected version, отклоняет
// неверный подтверждённый spec digest и повторно разрешает текущие input/authority до сравнения
// точных recipe version, target version/name, исходных before fields и равенства parameters/after.
// Это read-side проверка устаревшего плана без применения изменений.
func (repository *Repository) assistantRoleImageUpdateSnapshotMatches(
	ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation,
) (bool, error) {
	if operation.ExpectedVersion == nil {
		return false, nil
	}
	specSHA256, _ := operation.Parameters["specSha256"].(string)
	if !exactSHA256(specSHA256) {
		return false, errs.ErrInvalid
	}
	_, previous, err := repository.assistantRoleImageUpdateInput(ctx, tx, current,
		value.Mutation{ExpectedVersion: operation.ExpectedVersion}, command.AssistantRoleImageUpdateInput{
			ProjectRef: assistantString(operation.Parameters, "projectRef"),
			RecipeRef:  assistantString(operation.Parameters, "recipeRef"),
			Name:       assistantString(operation.Parameters, "name"),
			SpecSHA256: specSHA256,
			Environment: entity.RoleEnvironmentSelection{EnvironmentKey: assistantString(operation.Parameters, "environmentKey"),
				Dockerfile: assistantRoleImageDockerfile(operation.Parameters)},
		})
	if err != nil {
		return false, err
	}
	before := map[string]any{"projectRef": previous.ProjectRef, "recipeRef": previous.Ref,
		"name": previous.Name, "environmentKey": previous.Input.EnvironmentKey,
		"dockerfile": previous.Input.Dockerfile, "specSha256": previous.SpecSHA256}
	return *operation.ExpectedVersion == int64(previous.Version) &&
		operation.Target.Version != nil && *operation.Target.Version == int64(previous.Version) &&
		operation.Target.Name == previous.Name && reflect.DeepEqual(operation.Before, before) &&
		reflect.DeepEqual(operation.Parameters, operation.After), nil
}

// assistantRoleImageDockerfile returns only a string-valued Dockerfile exactly as stored in the
// parameter map, with empty output for missing or nonstring values; it performs no trimming or
// validation.
// assistantRoleImageDockerfile возвращает только строковый Dockerfile ровно из parameter map,
// давая пустую строку при отсутствии либо неверном типе; обрезку пробелов и валидацию не
// выполняет.
func assistantRoleImageDockerfile(parameters map[string]any) string {
	dockerfile, _ := parameters["dockerfile"].(string)
	return dockerfile
}

// rehydrateEditedAssistantRoleImageUpdate validates the original operation/target and matching edit
// key, immutable
// project/recipe/spec pins and the closed editable field set before accepting bounded nonempty
// name/environment/Dockerfile edits. It rebuilds action/target/before/after/version from the
// original operation, rejects forbidden changes and no-ops, and leaves the new specification
// digest for authoritative refresh.
// rehydrateEditedAssistantRoleImageUpdate проверяет исходные operation/target и совпадение ключа
// правки, неизменяемые
// project/recipe/spec pins и закрытый набор editable fields до приёма ограниченных непустых
// name/environment/Dockerfile правок. Восстанавливает action/target/before/after/version из
// исходной operation, отклоняет запрещённые изменения и no-op, оставляя новый specification digest
// авторитетному refresh.
func rehydrateEditedAssistantRoleImageUpdate(original, edited entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	specSHA256, _ := original.Parameters["specSha256"].(string)
	if original.Type != "UPDATE_ROLE_IMAGE_RECIPE" || original.Key != edited.Key ||
		original.Target.Kind != "ROLE_IMAGE_RECIPE" || original.Target.Ref == "" ||
		original.ExpectedVersion == nil || edited.Parameters == nil ||
		!onlyAssistantFields(edited.Parameters, "projectRef", "recipeRef", "name", "environmentKey", "dockerfile", "specSha256") ||
		!exactSHA256(specSHA256) ||
		!assistantJSONEqual(edited.Parameters["specSha256"], original.Parameters["specSha256"]) ||
		assistantString(edited.Parameters, "projectRef") != assistantString(original.Parameters, "projectRef") ||
		assistantString(edited.Parameters, "recipeRef") != original.Target.Ref {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	name, key := assistantString(edited.Parameters, "name"), assistantString(edited.Parameters, "environmentKey")
	dockerfile, dockerfileOK := edited.Parameters["dockerfile"].(string)
	if name == "" || key == "" || !dockerfileOK || dockerfile == "" || len(dockerfile) > 64<<10 {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	parameters := cloneAssistantFields(original.Parameters)
	parameters["name"], parameters["environmentKey"], parameters["dockerfile"] = name, key, dockerfile
	if reflect.DeepEqual(parameters, original.Before) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	edited.Action = "UPDATE"
	edited.Target = original.Target
	edited.Before = cloneAssistantFields(original.Before)
	edited.After = cloneAssistantFields(parameters)
	edited.Parameters = parameters
	edited.ExpectedVersion = original.ExpectedVersion
	return edited, nil
}

// refreshEditedAssistantRoleImageUpdate validates edited immutable pins and, unless explicitly
// refreshing stale input, normalizes and rechecks the frozen original snapshot. It rehydrates only
// editable fields through current owner/catalog checks, preserves recipe/project identity and the
// required original before/target/version pins, restores the user selection flag, and returns the
// supplied edited value alongside errors without persisting.
// refreshEditedAssistantRoleImageUpdate проверяет immutable pins правки и, если stale refresh явно
// не выбран, нормализует и заново проверяет исходный frozen snapshot. Повторно разрешает только
// editable fields через текущие owner/catalog проверки, сохраняет recipe/project identity и
// требуемые исходные before/target/version pins, восстанавливает selected flag и при ошибке
// возвращает переданную edited operation без сохранения.
func (repository *Repository) refreshEditedAssistantRoleImageUpdate(ctx context.Context, tx pgx.Tx, current scope, projectRef string, original, edited entity.AssistantPlanOperation, refreshStale bool) (entity.AssistantPlanOperation, error) {
	checked, err := rehydrateEditedAssistantRoleImageUpdate(original, edited)
	if err != nil {
		return edited, err
	}
	if !refreshStale {
		frozen, err := normalizeAssistantOperation(original)
		if err != nil {
			return edited, err
		}
		matching, err := repository.assistantRoleImageUpdateSnapshotMatches(ctx, tx, current, frozen)
		if err != nil {
			return edited, err
		}
		if !matching {
			return edited, errs.ErrConflict
		}
	}
	checked.Parameters = map[string]any{}
	for _, field := range []string{"projectRef", "recipeRef", "name", "environmentKey", "dockerfile"} {
		checked.Parameters[field] = edited.Parameters[field]
	}
	refreshed, err := repository.hydrateAssistantRoleImageUpdate(ctx, tx, current, projectRef, checked)
	if err != nil {
		return edited, err
	}
	if refreshed.Target.Ref != original.Target.Ref || assistantString(refreshed.Parameters, "projectRef") != assistantString(original.Parameters, "projectRef") ||
		!refreshStale && (!assistantJSONEqual(refreshed.Before, original.Before) || !assistantJSONEqual(refreshed.Target, original.Target) || !assistantJSONEqual(refreshed.ExpectedVersion, original.ExpectedVersion)) {
		return edited, errs.ErrConflict
	}
	refreshed.Selected = edited.Selected
	return refreshed, nil
}

// inheritAssistantImageOptions preserves saved package/tool/install options only when the selected
// environment key matches the previous recipe, copying populated slices and retaining the original
// empty-to-nil shape. Dockerfile and environment key stay untouched; a different
// environment retains the caller's supplied options. This pure helper grants no authority.
// inheritAssistantImageOptions сохраняет прежние package/tool/install options только при совпадении
// выбранного environment key с прежним recipe, копируя заполненные slices и сохраняя исходную
// empty-to-nil форму. Dockerfile и environment key не изменяются; другое окружение
// сохраняет переданные options. Чистый helper не выдаёт полномочий.
func inheritAssistantImageOptions(selection entity.RoleEnvironmentSelection, previous entity.RoleImageRecipeInput) entity.RoleEnvironmentSelection {
	if selection.EnvironmentKey == previous.EnvironmentKey {
		selection.PackageKeys = append([]string(nil), previous.PackageKeys...)
		selection.ToolKeys = append([]string(nil), previous.ToolKeys...)
		selection.InstallationBlock = previous.InstallationBlock
	}
	return selection
}

// applyAssistantImageManage applies the already authorized recipe command and records its managed
// source/build lineage in the same caller transaction, returning zero projections on either error.
// It preserves the optional existing managed-set pointer and does not own authority, result-shape
// checks, audit/event/receipt projection, rollback or commit; those remain with the command path.
// applyAssistantImageManage применяет уже авторизованную recipe command и сохраняет её managed
// source/build lineage в той же транзакции вызывающего метода, возвращая нулевые представления при
// любой ошибке. Сохраняет необязательный pointer прежнего managed set; не владеет authority,
// result-shape checks, audit/event/receipt projection, rollback либо commit: они остаются у command path.
func (repository *Repository) applyAssistantImageManage(ctx context.Context, tx pgx.Tx, current scope, input roleimagerepo.ManageInput, managed *managedSet) (roleimagerepo.ManageResult, string, string, error) {
	result, projectID, projectRef, err := repository.applyRoleImageManage(ctx, tx, current, input)
	if err != nil {
		return roleimagerepo.ManageResult{}, "", "", err
	}
	if err := repository.recordManagedRoleImageCommand(ctx, tx, current, input, managed, result); err != nil {
		return roleimagerepo.ManageResult{}, "", "", err
	}
	return result, projectID, projectRef, nil
}
