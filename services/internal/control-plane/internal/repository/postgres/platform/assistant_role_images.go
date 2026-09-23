package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
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
