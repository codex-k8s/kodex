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
		baseline, err := repository.roleImageCatalogResolver(entity.RoleEnvironmentSelection{EnvironmentKey: previous.Input.EnvironmentKey, PackageKeys: previous.Input.PackageKeys, ToolKeys: previous.Input.ToolKeys, InstallationBlock: previous.Input.InstallationBlock, Dockerfile: previous.Input.Dockerfile})
		if err != nil || roleImageDigest(baseline) != roleImageDigest(previous.Input) {
			return input, nil, errs.ErrConflict
		}
		if input.Environment.EnvironmentKey == previous.Input.EnvironmentKey {
			input.Environment.PackageKeys = append([]string(nil), previous.Input.PackageKeys...)
			input.Environment.ToolKeys = append([]string(nil), previous.Input.ToolKeys...)
			input.Environment.InstallationBlock = previous.Input.InstallationBlock
		}
	}
	recipe, err := repository.roleImageCatalogResolver(input.Environment)
	if err != nil || roleimageservice.ValidateManagedOrganizationRecipe(payload.Name, recipe) != nil {
		return input, nil, errs.ErrInvalid
	}
	input.Recipe = recipe
	return input, previous, nil
}

func (repository *Repository) applySystemAssistantImage(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.SystemAssistantRoleImageInput)
	if !ok {
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
	return commandOutcome{projectID: projectID, projectRef: projectRef, resourceKind: "ROLE_IMAGE_RECIPE", resourceRef: result.Recipe.Ref,
		summary: "i18n:ROLE_IMAGE_RECIPE_CHANGED", platformEvent: "ROLE_IMAGE_RECIPE_CHANGED",
		result: command.Result{CreatedRefs: []string{result.Recipe.Ref}, RuntimeItems: []map[string]any{{"scopeKind": "ORGANIZATION", "organizationRef": current.organizationRef, "imageBuildRef": result.Build.Ref, "imageBuildStage": result.Build.Stage}}}}, nil
}

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
		Environment: entity.RoleEnvironmentSelection{EnvironmentKey: assistantString(operation.Parameters, "environmentKey"), Dockerfile: assistantString(operation.Parameters, "dockerfile")}}
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
		if payload.Environment.EnvironmentKey == "" {
			payload.Environment.EnvironmentKey = previous.Input.EnvironmentKey
		}
		if payload.Environment.Dockerfile == "" && payload.Environment.EnvironmentKey == previous.Input.EnvironmentKey {
			payload.Environment.Dockerfile = previous.Input.Dockerfile
		}
		v := int64(previous.Version)
		version = &v
		before = systemAssistantImageFields(payload, previous.Name, previous.Input.EnvironmentKey, previous.Input.Dockerfile)
		before["recipeGeneration"] = previous.Generation
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
			if dockerfile := assistantString(operation.Parameters, "dockerfile"); dockerfile != "" {
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
	if update && assistantString(before, "name") == payload.Name && assistantString(before, "environmentKey") == payload.Environment.EnvironmentKey && assistantString(before, "dockerfile") == managedInput.Recipe.Dockerfile {
		return operation, errs.ErrConflict
	}
	operation.Before, operation.Parameters, operation.After = before, after, cloneAssistantFields(after)
	operation.ExpectedVersion, operation.Selected, operation.Input = version, true, nil
	return operation, nil
}

func systemAssistantImageFields(payload command.SystemAssistantRoleImageInput, name, key, dockerfile string) map[string]any {
	fields := map[string]any{"systemAssistantRef": payload.SystemAssistantRef, "scopeKind": "ORGANIZATION", "organizationRef": payload.OrganizationRef, "agentVersion": payload.AgentVersion, "name": name, "environmentKey": key, "dockerfile": dockerfile}
	if payload.RecipeRef != "" {
		fields["recipeRef"] = payload.RecipeRef
	}
	return fields
}

func systemAssistantImageCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	update := operation.Type == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE"
	if !onlyAssistantFields(operation.Input, "systemAssistantRef", "scopeKind", "organizationRef", "agentVersion", "recipeRef", "name", "environmentKey", "dockerfile", "expectedVersion") || assistantString(operation.Input, "scopeKind") != "ORGANIZATION" {
		return command.Command{}, errs.ErrInvalid
	}
	version, ok := assistantInt64(operation.Input, "agentVersion")
	if !ok || version < 1 {
		return command.Command{}, errs.ErrInvalid
	}
	payload := command.SystemAssistantRoleImageInput{SystemAssistantRef: assistantString(operation.Input, "systemAssistantRef"), OrganizationRef: assistantString(operation.Input, "organizationRef"), AgentVersion: version,
		RecipeRef: assistantString(operation.Input, "recipeRef"), Name: assistantString(operation.Input, "name"), Environment: entity.RoleEnvironmentSelection{EnvironmentKey: assistantString(operation.Input, "environmentKey"), Dockerfile: assistantString(operation.Input, "dockerfile")}}
	if payload.SystemAssistantRef == "" || payload.OrganizationRef == "" || payload.Name == "" || payload.Environment.EnvironmentKey == "" || payload.Environment.Dockerfile == "" || len(payload.Environment.Dockerfile) > 64<<10 {
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
	return assistantJSONEqual(before, operation.Before) && reflect.DeepEqual(operation.Parameters, operation.After), nil
}
