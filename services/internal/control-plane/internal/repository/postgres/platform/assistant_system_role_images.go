package platform

import (
	"context"
	"reflect"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// authorizeSystemAssistantImage resolves the authoritative system-assistant configuration target
// and requires ORGANIZATION scope plus the current organization reference before canonical
// organization CREATE/UPDATE image authorization; request references never establish ownership.
// authorizeSystemAssistantImage разрешает авторитетный system-assistant configuration target и
// требует ORGANIZATION scope с текущей organization reference до канонической проверки
// CREATE/UPDATE образа организации; ссылки запроса не устанавливают владение.
func (repository *Repository) authorizeSystemAssistantImage(ctx context.Context, tx pgx.Tx, current scope, payload command.SystemAssistantRoleImageInput, update bool) (assistantConfigurationTarget, error) {
	target, err := repository.assistantConfigurationTarget(ctx, tx, current, payload.SystemAssistantRef)
	if err != nil {
		return target, err
	}
	if target.scopeKind != "ORGANIZATION" || payload.OrganizationRef != current.organizationRef {
		return target, errs.ErrNotFound
	}
	input := roleimagerepo.ManageInput{ScopeKind: "ORGANIZATION", Action: "CREATE"}
	if update {
		input.Action, input.RecipeRef = "UPDATE", payload.RecipeRef
	}
	if err := repository.authorizeRoleImageManage(ctx, tx, current, input); err != nil {
		return target, err
	}
	return target, nil
}

// systemAssistantImageInput checks organization assistant authority and exact agent version before
// resolving catalog input. Update additionally requires active nonshipped visible organization
// source, exact recipe OCC, editable managed ownership and the saved valid recipe/digest,
// retaining same-environment options; a supplied specification digest must match the resolved
// recipe. It returns input/history without mutating state.
// systemAssistantImageInput проверяет полномочия organization assistant и точную agent version до
// разрешения catalog input. Update дополнительно требует active nonshipped видимый source
// организации, точный recipe OCC, editable managed ownership и сохранённые допустимые
// recipe/digest, сохраняя options того же окружения; переданный specification digest должен
// совпадать с разрешённым recipe. Возвращает input/history без изменения состояния.
func (repository *Repository) systemAssistantImageInput(ctx context.Context, tx pgx.Tx, current scope, payload command.SystemAssistantRoleImageInput, mutation value.Mutation, update bool) (roleimagerepo.ManageInput, *entity.RoleImageRecipe, error) {
	target, err := repository.authorizeSystemAssistantImage(ctx, tx, current, payload, update)
	if err != nil {
		return roleimagerepo.ManageInput{}, nil, err
	}
	if payload.AgentVersion != target.version {
		return roleimagerepo.ManageInput{}, nil, errs.ErrVersionMismatch
	}
	if repository.roleImageCatalogResolver == nil {
		return roleimagerepo.ManageInput{}, nil, errs.ErrUnavailable
	}
	input := roleimagerepo.ManageInput{ScopeKind: "ORGANIZATION", Action: "CREATE", Name: payload.Name, Environment: payload.Environment, Mutation: mutation}
	var previous *entity.RoleImageRecipe
	if update {
		input.Action, input.RecipeRef = "UPDATE", payload.RecipeRef
		detail, err := repository.getRoleImageRecipe(ctx, tx, current, payload.RecipeRef, true, false)
		if err != nil {
			return input, nil, err
		}
		previous = &detail.Recipe
		projectRoleImageSource(previous, nil, true, true)
		if previous.ScopeKind != "ORGANIZATION" || previous.OrganizationRef != current.organizationRef || previous.State != "ACTIVE" || shippedRoleImage(*previous) || !previous.SourceAvailable {
			return input, nil, errs.ErrNotFound
		}
		if mutation.ExpectedVersion == nil || *mutation.ExpectedVersion != int64(previous.Version) {
			return input, nil, errs.ErrVersionMismatch
		}
		if _, err := repository.managedRoleImageTarget(ctx, tx, current, input); err != nil {
			return input, nil, err
		}
		// Историческая спецификация проверяется как сохранённый immutable input,
		// а не пересобирается новым каталогом. Новый input закрепляется отдельно
		// в подтверждаемом плане; RequestBuild не обновляет эти зависимости.
		if roleimageservice.ValidateManagedOrganizationRecipe(previous.Name, previous.Input) != nil || previous.SpecSHA256 != roleImageDigest(previous.Input) {
			return input, nil, errs.ErrConflict
		}
		input.Environment = inheritAssistantImageOptions(input.Environment, previous.Input)
	}
	recipe, err := repository.roleImageCatalogResolver(input.Environment)
	if err != nil || roleimageservice.ValidateManagedOrganizationRecipe(payload.Name, recipe) != nil {
		return input, nil, errs.ErrInvalid
	}
	input.Recipe = recipe
	if payload.SpecSHA256 != "" && payload.SpecSHA256 != roleImageDigest(recipe) {
		return input, previous, errs.ErrConflict
	}
	return input, previous, nil
}

// applySystemAssistantImage requires typed payload and exact spec pin, resolves CREATE versus
// UPDATE organization input and managed ownership, then applies recipe/build with managed lineage
// in the caller transaction. It requires a build and returns the existing created-ref and
// organization/build runtime metadata plus event descriptor; it does not commit or bind the
// resulting artifact to an environment.
// applySystemAssistantImage требует typed payload и точный spec pin, разрешает CREATE либо UPDATE
// organization input и managed ownership, затем применяет recipe/build с managed lineage в
// транзакции вызывающего метода. Требует build и возвращает прежние created-ref и
// organization/build runtime metadata с event descriptor; commit и привязку артефакта к окружению
// не выполняет.
func (repository *Repository) applySystemAssistantImage(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.SystemAssistantRoleImageInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	if !exactSHA256(payload.SpecSHA256) {
		return commandOutcome{}, errs.ErrInvalid
	}
	managedInput, _, err := repository.systemAssistantImageInput(ctx, tx, current, payload, input.Mutation, input.Kind == command.UpdateSystemAssistantRoleImageRecipe)
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
	return commandOutcome{projectID: projectID, projectRef: projectRef, resourceKind: "ROLE_IMAGE_RECIPE", resourceRef: result.Recipe.Ref,
		summary: "i18n:ROLE_IMAGE_RECIPE_CHANGED", platformEvent: "ROLE_IMAGE_RECIPE_CHANGED",
		result: command.Result{CreatedRefs: []string{result.Recipe.Ref}, RuntimeItems: []map[string]any{{"scopeKind": "ORGANIZATION", "organizationRef": current.organizationRef, "imageBuildRef": result.Build.Ref, "imageBuildStage": result.Build.Stage}}}}, nil
}

// hydrateSystemAssistantImage accepts only closed fields and authoritative ORGANIZATION assistant
// scope, then prepares CREATE or UPDATE with server-owned agent version, target and before/after
// fields. Create may use the recommended catalog selection; update preserves omitted source
// choices and records recipe generation/spec pins. It validates the resolved input, rejects
// no-ops, selects the operation and clears materialized Input without writing.
// hydrateSystemAssistantImage принимает только закрытые fields и авторитетный ORGANIZATION
// assistant scope, затем готовит CREATE либо UPDATE с server-owned agent version, target и
// before/after fields. Create может выбрать рекомендованный catalog selection; update сохраняет
// пропущенные source choices и записывает recipe generation/spec pins. Проверяет разрешённый
// input, отклоняет no-op, выбирает operation и очищает материализованный Input без записи.
func (repository *Repository) hydrateSystemAssistantImage(ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	update := operation.Type == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE"
	if !onlyAssistantFields(operation.Parameters, "systemAssistantRef", "recipeRef", "name", "environmentKey", "dockerfile") {
		return operation, errs.ErrInvalid
	}
	ref := assistantString(operation.Parameters, "systemAssistantRef")
	target, err := repository.assistantConfigurationTarget(ctx, tx, current, ref)
	if err != nil {
		return operation, err
	}
	if target.scopeKind != "ORGANIZATION" {
		return operation, errs.ErrNotFound
	}
	payload := command.SystemAssistantRoleImageInput{SystemAssistantRef: ref, OrganizationRef: current.organizationRef, AgentVersion: target.version,
		RecipeRef: assistantString(operation.Parameters, "recipeRef"), Name: assistantString(operation.Parameters, "name"),
		Environment: entity.RoleEnvironmentSelection{EnvironmentKey: assistantString(operation.Parameters, "environmentKey"), Dockerfile: assistantImageDockerfile(operation.Parameters)}}
	var before map[string]any
	var version *int64
	if update {
		if _, err := repository.authorizeSystemAssistantImage(ctx, tx, current, payload, true); err != nil {
			return operation, err
		}
		detail, err := repository.getRoleImageRecipe(ctx, tx, current, payload.RecipeRef, true, false)
		if err != nil {
			return operation, err
		}
		previous := detail.Recipe
		if payload.Name == "" {
			payload.Name = previous.Name
		}
		payload.Environment, err = assistantImageUpdateSelection(operation.Parameters, previous.Input)
		if err != nil {
			return operation, err
		}
		v := int64(previous.Version)
		version = &v
		before = systemAssistantImageFields(payload, previous.Name, previous.Input.EnvironmentKey, previous.Input.Dockerfile)
		before["recipeGeneration"] = previous.Generation
		before["specSha256"] = previous.SpecSHA256
		operation.Target = entity.AssistantPlanTarget{Kind: "ROLE_IMAGE_RECIPE", Ref: previous.Ref, Name: previous.Name, Version: version}
		operation.Action = "UPDATE"
	} else {
		before = map[string]any{}
		if payload.RecipeRef != "" {
			return operation, errs.ErrInvalid
		}
		if payload.Environment.EnvironmentKey == "" {
			if repository.roleImageRecommendedSelection == nil {
				return operation, errs.ErrUnavailable
			}
			selection, err := repository.roleImageRecommendedSelection()
			if err != nil {
				return operation, err
			}
			payload.Environment = selection
			if dockerfile := assistantImageDockerfile(operation.Parameters); dockerfile != "" {
				payload.Environment.Dockerfile = dockerfile
			}
		}
		operation.Target = entity.AssistantPlanTarget{Kind: "ROLE_IMAGE_RECIPE", Name: payload.Name}
		operation.Action = "CREATE"
	}
	managedInput, _, err := repository.systemAssistantImageInput(ctx, tx, current, payload, value.Mutation{ExpectedVersion: version}, update)
	if err != nil {
		return operation, err
	}
	after := systemAssistantImageFields(payload, payload.Name, payload.Environment.EnvironmentKey, managedInput.Recipe.Dockerfile)
	after["specSha256"] = roleImageDigest(managedInput.Recipe)
	if update && assistantString(before, "name") == payload.Name && assistantString(before, "environmentKey") == payload.Environment.EnvironmentKey && assistantImageDockerfile(before) == managedInput.Recipe.Dockerfile && assistantString(before, "specSha256") == assistantString(after, "specSha256") {
		return operation, errs.ErrConflict
	}
	operation.Before, operation.Parameters, operation.After = before, after, cloneAssistantFields(after)
	operation.ExpectedVersion, operation.Selected, operation.Input = version, true, nil
	return operation, nil
}

// systemAssistantImageFields projects system-assistant identity, fixed organization scope/owner,
// agent version and exact name/environment/Dockerfile into a new field map, including recipeRef
// only when nonempty; it does not infer authority from these fields.
// systemAssistantImageFields проецирует system-assistant identity, фиксированные organization
// scope/owner, agent version и точные name/environment/Dockerfile в новую field map, добавляя
// recipeRef только при непустом значении; полномочия из этих полей не выводит.
func systemAssistantImageFields(payload command.SystemAssistantRoleImageInput, name, key, dockerfile string) map[string]any {
	fields := map[string]any{"systemAssistantRef": payload.SystemAssistantRef, "scopeKind": "ORGANIZATION", "organizationRef": payload.OrganizationRef, "agentVersion": payload.AgentVersion, "name": name, "environmentKey": key, "dockerfile": dockerfile}
	if payload.RecipeRef != "" {
		fields["recipeRef"] = payload.RecipeRef
	}
	return fields
}

// assistantImageDockerfile extracts only a string Dockerfile without trimming any bytes,
// preserving the immutable confirmed-input digest; missing or nonstring fields yield an empty
// string for separate validation.
// assistantImageDockerfile извлекает только строковый Dockerfile без обрезки байтов, сохраняя
// неизменяемый confirmed-input digest; отсутствующее либо нестроковое поле даёт пустую строку для
// отдельной валидации.
func assistantImageDockerfile(fields map[string]any) string {
	// Dockerfile является байтовой частью immutable спецификации: TrimSpace
	// изменяет digest между подтверждением и материализацией команды.
	content, _ := fields["dockerfile"].(string)
	return content
}

// systemAssistantImageCommand materializes a typed organization image command from the closed
// server-resolved Input fields, requiring positive agent version, exact spec digest and bounded
// nonempty Dockerfile. UPDATE requires recipeRef and positive expected version; CREATE rejects
// existing recipeRef or operation expected version. It constructs payload/OCC only, leaving
// principal and execution authority to the caller.
// systemAssistantImageCommand материализует typed organization image command из закрытых
// server-resolved Input fields, требуя положительную agent version, точный spec digest и
// ограниченный непустой Dockerfile. UPDATE требует recipeRef и положительную expected version;
// CREATE отклоняет существующие recipeRef либо operation expected version. Формирует только
// payload/OCC, оставляя principal и полномочия исполнения вызывающему методу.
func systemAssistantImageCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	update := operation.Type == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE"
	if !onlyAssistantFields(operation.Input, "systemAssistantRef", "scopeKind", "organizationRef", "agentVersion", "recipeRef", "name", "environmentKey", "dockerfile", "specSha256", "expectedVersion") || assistantString(operation.Input, "scopeKind") != "ORGANIZATION" {
		return command.Command{}, errs.ErrInvalid
	}
	version, ok := assistantInt64(operation.Input, "agentVersion")
	if !ok || version < 1 {
		return command.Command{}, errs.ErrInvalid
	}
	specSHA256, _ := operation.Input["specSha256"].(string)
	payload := command.SystemAssistantRoleImageInput{SystemAssistantRef: assistantString(operation.Input, "systemAssistantRef"), OrganizationRef: assistantString(operation.Input, "organizationRef"), AgentVersion: version,
		RecipeRef: assistantString(operation.Input, "recipeRef"), Name: assistantString(operation.Input, "name"), SpecSHA256: specSHA256, Environment: entity.RoleEnvironmentSelection{EnvironmentKey: assistantString(operation.Input, "environmentKey"), Dockerfile: assistantImageDockerfile(operation.Input)}}
	if payload.SystemAssistantRef == "" || payload.OrganizationRef == "" || payload.Name == "" || !exactSHA256(payload.SpecSHA256) || payload.Environment.EnvironmentKey == "" || payload.Environment.Dockerfile == "" || len(payload.Environment.Dockerfile) > 64<<10 {
		return command.Command{}, errs.ErrInvalid
	}
	result := command.Command{Kind: command.CreateSystemAssistantRoleImageRecipe, Payload: payload}
	if update {
		expected, ok := assistantInt64(operation.Input, "expectedVersion")
		if !ok || expected < 1 || payload.RecipeRef == "" {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind = command.UpdateSystemAssistantRoleImageRecipe
		result.Mutation.ExpectedVersion = &expected
	} else if payload.RecipeRef != "" || operation.ExpectedVersion != nil {
		return command.Command{}, errs.ErrInvalid
	}
	return result, nil
}

// systemAssistantImageSnapshotMatches materializes and revalidates current authoritative
// system-image input before comparing plan snapshots. CREATE requires an empty before map; UPDATE
// includes exact recipe generation and saved spec digest using JSON-equivalent before comparison,
// while both paths require parameters/after equality without applying a mutation.
// systemAssistantImageSnapshotMatches материализует и заново проверяет текущий авторитетный
// system-image input до сравнения snapshots плана. CREATE требует пустую before map; UPDATE
// включает точные recipe generation и сохранённый spec digest с JSON-equivalent before comparison,
// оба пути требуют равенства parameters/after без изменения состояния.
func (repository *Repository) systemAssistantImageSnapshotMatches(ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation) (bool, error) {
	planned, err := systemAssistantImageCommand(operation)
	if err != nil {
		return false, err
	}
	payload := planned.Payload.(command.SystemAssistantRoleImageInput)
	_, previous, err := repository.systemAssistantImageInput(ctx, tx, current, payload, planned.Mutation, planned.Kind == command.UpdateSystemAssistantRoleImageRecipe)
	if err != nil {
		return false, err
	}
	if previous == nil {
		return len(operation.Before) == 0 && reflect.DeepEqual(operation.Parameters, operation.After), nil
	}
	before := systemAssistantImageFields(payload, previous.Name, previous.Input.EnvironmentKey, previous.Input.Dockerfile)
	before["recipeGeneration"] = previous.Generation
	before["specSha256"] = previous.SpecSHA256
	return assistantJSONEqual(before, operation.Before) && reflect.DeepEqual(operation.Parameters, operation.After), nil
}
