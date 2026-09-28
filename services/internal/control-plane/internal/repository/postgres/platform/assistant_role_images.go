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
	return roleimagerepo.ManageInput{Action: "CREATE", ProjectRef: payload.ProjectRef,
		RoleDefinitionRef: roleRef, Name: payload.Name, Environment: payload.Environment, Recipe: recipe}, nil
}

func (repository *Repository) authorizeAssistantRoleImage(
	ctx context.Context, tx pgx.Tx, current scope, payload command.AssistantRoleImageRecipeInput,
) error {
	input, err := repository.assistantRoleImageManageInput(ctx, tx, current, payload)
	if err != nil {
		return err
	}
	return repository.authorizeRoleImageManage(ctx, tx, current, input)
}

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
	result, projectID, projectRef, err := repository.applyRoleImageManage(ctx, tx, current, input)
	if err != nil {
		return commandOutcome{}, err
	}
	if err := repository.recordManagedRoleImageCommand(ctx, tx, current, input, nil, result); err != nil {
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

func (repository *Repository) assistantRoleImageUpdateInput(
	ctx context.Context, tx pgx.Tx, current scope, mutation value.Mutation, payload command.AssistantRoleImageUpdateInput,
) (roleimagerepo.ManageInput, entity.RoleImageRecipe, error) {
	if payload.ProjectRef == "" || payload.RecipeRef == "" || payload.Name == "" ||
		payload.Environment.EnvironmentKey == "" || mutation.ExpectedVersion == nil || *mutation.ExpectedVersion < 1 ||
		repository.roleImageCatalogResolver == nil {
		return roleimagerepo.ManageInput{}, entity.RoleImageRecipe{}, errs.ErrInvalid
	}
	input := roleimagerepo.ManageInput{Action: "UPDATE", RecipeRef: payload.RecipeRef,
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
	baseline, err := repository.roleImageCatalogResolver(entity.RoleEnvironmentSelection{
		EnvironmentKey: previous.Input.EnvironmentKey, PackageKeys: previous.Input.PackageKeys,
		ToolKeys: previous.Input.ToolKeys, InstallationBlock: previous.Input.InstallationBlock,
		Dockerfile: previous.Input.Dockerfile,
	})
	if err != nil || roleImageDigest(baseline) != roleImageDigest(previous.Input) {
		return roleimagerepo.ManageInput{}, entity.RoleImageRecipe{}, errs.ErrConflict
	}
	if input.Environment.EnvironmentKey == previous.Input.EnvironmentKey {
		input.Environment.PackageKeys = append([]string(nil), previous.Input.PackageKeys...)
		input.Environment.ToolKeys = append([]string(nil), previous.Input.ToolKeys...)
		input.Environment.InstallationBlock = previous.Input.InstallationBlock
		if input.Environment.Dockerfile == "" {
			input.Environment.Dockerfile = previous.Input.Dockerfile
		}
	}
	input.Recipe, err = repository.roleImageCatalogResolver(input.Environment)
	if err != nil || roleimageservice.ValidateManagedRecipe(payload.ProjectRef, previous.RoleDefinitionRef, payload.Name, input.Recipe) != nil {
		return roleimagerepo.ManageInput{}, entity.RoleImageRecipe{}, errs.ErrInvalid
	}
	return input, previous, nil
}

func (repository *Repository) updateAssistantRoleImage(
	ctx context.Context, tx pgx.Tx, current scope, input command.Command,
) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantRoleImageUpdateInput)
	if !ok {
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
	result, projectID, projectRef, err := repository.applyRoleImageManage(ctx, tx, current, managedInput)
	if err != nil {
		return commandOutcome{}, err
	}
	if err := repository.recordManagedRoleImageCommand(ctx, tx, current, managedInput, managed, result); err != nil {
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
		Action: "UPDATE", ProjectRef: projectRef, RecipeRef: ref,
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
	key := previous.Input.EnvironmentKey
	if supplied, exists := operation.Parameters["environmentKey"]; exists {
		var valid bool
		key, valid = supplied.(string)
		if !valid {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
	}
	dockerfile := previous.Input.Dockerfile
	if key != previous.Input.EnvironmentKey {
		dockerfile = ""
	}
	if supplied, exists := operation.Parameters["dockerfile"]; exists {
		var valid bool
		dockerfile, valid = supplied.(string)
		if !valid || len(dockerfile) > 64<<10 {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
	}
	version := int64(previous.Version)
	mutation := value.Mutation{ExpectedVersion: &version}
	managedInput, _, err := repository.assistantRoleImageUpdateInput(ctx, tx, current, mutation,
		command.AssistantRoleImageUpdateInput{ProjectRef: projectRef, RecipeRef: ref, Name: name,
			Environment: entity.RoleEnvironmentSelection{EnvironmentKey: key, Dockerfile: dockerfile}})
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	before := map[string]any{"projectRef": projectRef, "recipeRef": ref,
		"name": previous.Name, "environmentKey": previous.Input.EnvironmentKey,
		"dockerfile": previous.Input.Dockerfile}
	after := cloneAssistantFields(before)
	after["name"], after["environmentKey"], after["dockerfile"] = name, key, managedInput.Recipe.Dockerfile
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

func (repository *Repository) assistantRoleImageUpdateSnapshotMatches(
	ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation,
) (bool, error) {
	if operation.ExpectedVersion == nil {
		return false, nil
	}
	_, previous, err := repository.assistantRoleImageUpdateInput(ctx, tx, current,
		value.Mutation{ExpectedVersion: operation.ExpectedVersion}, command.AssistantRoleImageUpdateInput{
			ProjectRef: assistantString(operation.Parameters, "projectRef"),
			RecipeRef:  assistantString(operation.Parameters, "recipeRef"),
			Name:       assistantString(operation.Parameters, "name"),
			Environment: entity.RoleEnvironmentSelection{EnvironmentKey: assistantString(operation.Parameters, "environmentKey"),
				Dockerfile: assistantRoleImageDockerfile(operation.Parameters)},
		})
	if err != nil {
		return false, err
	}
	before := map[string]any{"projectRef": previous.ProjectRef, "recipeRef": previous.Ref,
		"name": previous.Name, "environmentKey": previous.Input.EnvironmentKey,
		"dockerfile": previous.Input.Dockerfile}
	return *operation.ExpectedVersion == int64(previous.Version) &&
		operation.Target.Version != nil && *operation.Target.Version == int64(previous.Version) &&
		operation.Target.Name == previous.Name && reflect.DeepEqual(operation.Before, before) &&
		reflect.DeepEqual(operation.Parameters, operation.After), nil
}

func assistantRoleImageDockerfile(parameters map[string]any) string {
	dockerfile, _ := parameters["dockerfile"].(string)
	return dockerfile
}

func rehydrateEditedAssistantRoleImageUpdate(original, edited entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if original.Type != "UPDATE_ROLE_IMAGE_RECIPE" || original.Key != edited.Key ||
		original.Target.Kind != "ROLE_IMAGE_RECIPE" || original.Target.Ref == "" ||
		original.ExpectedVersion == nil || edited.Parameters == nil ||
		!onlyAssistantFields(edited.Parameters, "projectRef", "recipeRef", "name", "environmentKey", "dockerfile") ||
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
