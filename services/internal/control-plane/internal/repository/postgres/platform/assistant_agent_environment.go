package platform

import (
	"context"
	"errors"
	"reflect"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) readAssistantAgentBindingSnapshot(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef, agentRef string,
) (map[string]any, int64, error) {
	projection, err := repository.resolveAssistantContext(ctx, tx, actorScope,
		entity.AssistantContextDescriptor{EntityKind: "AGENT", EntityRef: agentRef}, projectRef)
	if err != nil {
		return nil, 0, err
	}
	if !contains(projection.AllowedOperations, "BIND_AGENT_RUNTIME_ENVIRONMENT") {
		return nil, 0, errs.ErrForbidden
	}
	var name, purpose, roleDescription, avatarURL string
	var version int64
	if err := tx.QueryRow(ctx, queryConfigurationHydrateassistantoperationSelectAgent,
		actorScope.organizationID, projectRef, agentRef,
	).Scan(&name, &purpose, &roleDescription, &avatarURL, &version); errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, errs.ErrNotFound
	} else if err != nil {
		return nil, 0, errs.ErrUnavailable
	}
	view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, actorScope, agentRef)
	if err != nil {
		return nil, 0, err
	}
	if view.AgentVersion != version || view.EnvironmentBinding.AgentRef != agentRef {
		return nil, 0, errs.ErrConflict
	}
	return map[string]any{
		"agentRef": agentRef, "projectRef": projectRef, "agentName": name,
		"bindingRef":      view.EnvironmentBinding.Ref,
		"environmentRef":  view.EnvironmentBinding.EnvironmentRef,
		"environmentName": view.Environment.Name,
		"versionRef":      view.EnvironmentBinding.VersionRef,
	}, version, nil
}

func (repository *Repository) readAssistantBindingTarget(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef, environmentRef string,
) (map[string]any, error) {
	if _, err := repository.resolveAssistantContext(ctx, tx, actorScope,
		entity.AssistantContextDescriptor{EntityKind: "ENVIRONMENT", EntityRef: environmentRef}, projectRef); err != nil {
		return nil, err
	}
	environment, err := repository.getRuntimeEnvironmentTx(ctx, tx, actorScope, environmentRef)
	if err != nil {
		return nil, err
	}
	if environment.ProjectRef != projectRef || environment.State != "ACTIVE" || !environment.Ready ||
		environment.CurrentVersion.Ref == "" || environment.CurrentVersion.Digest == "" {
		return nil, errs.ErrConflict
	}
	return map[string]any{
		"environmentRef": environment.Ref, "environmentName": environment.Name,
		"versionRef":    environment.CurrentVersion.Ref,
		"versionDigest": environment.CurrentVersion.Digest,
	}, nil
}

func (repository *Repository) hydrateAssistantAgentEnvironmentBinding(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef string, operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if projectRef == "" || !onlyAssistantFields(operation.Parameters, "agentRef", "environmentRef") {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	agentRef, environmentRef := assistantString(operation.Parameters, "agentRef"), assistantString(operation.Parameters, "environmentRef")
	if agentRef == "" || environmentRef == "" {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	before, version, err := repository.readAssistantAgentBindingSnapshot(ctx, tx, actorScope, projectRef, agentRef)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	after, err := repository.readAssistantBindingTarget(ctx, tx, actorScope, projectRef, environmentRef)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	return hydrateAssistantBindingFields(before, after, version, operation)
}

func hydrateAssistantBindingFields(before, after map[string]any, version int64,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if assistantString(before, "environmentRef") == assistantString(after, "environmentRef") &&
		assistantString(before, "versionRef") == assistantString(after, "versionRef") {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "AGENT", Ref: assistantString(before, "agentRef"),
		Name: assistantString(before, "agentName"), Version: &version}
	operation.Before = cloneAssistantFields(before)
	operation.After = cloneAssistantFields(after)
	operation.Parameters = map[string]any{
		"agentRef": before["agentRef"], "environmentRef": after["environmentRef"], "versionRef": after["versionRef"],
	}
	operation.ExpectedVersion = &version
	operation.Selected = true
	operation.Input = nil
	return operation, nil
}

func (repository *Repository) rehydrateEditedAssistantBinding(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef string, original, edited entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if original.Type != "BIND_AGENT_RUNTIME_ENVIRONMENT" || original.Key != edited.Key ||
		original.Target.Kind != "AGENT" || original.Target.Ref == "" ||
		original.ExpectedVersion == nil || *original.ExpectedVersion < 1 || edited.Parameters == nil ||
		!onlyAssistantFields(edited.Parameters, "agentRef", "environmentRef", "versionRef") ||
		assistantString(edited.Parameters, "agentRef") != original.Target.Ref {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	before, version, err := repository.readAssistantAgentBindingSnapshot(ctx, tx, actorScope, projectRef, original.Target.Ref)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if version != *original.ExpectedVersion || !reflect.DeepEqual(before, original.Before) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	after, err := repository.readAssistantBindingTarget(ctx, tx, actorScope, projectRef,
		assistantString(edited.Parameters, "environmentRef"))
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	selected := edited.Selected
	hydrated, err := hydrateAssistantBindingFields(before, after, version, edited)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	hydrated.Selected = selected
	return hydrated, nil
}

func (repository *Repository) assistantAgentBindingSnapshotMatches(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef string, operation entity.AssistantPlanOperation,
) (bool, error) {
	before, version, err := repository.readAssistantAgentBindingSnapshot(ctx, tx, actorScope, projectRef, operation.Target.Ref)
	if err != nil {
		return false, err
	}
	after, err := repository.readAssistantBindingTarget(ctx, tx, actorScope, projectRef,
		assistantString(operation.Parameters, "environmentRef"))
	if err != nil {
		return false, err
	}
	return operation.ExpectedVersion != nil && *operation.ExpectedVersion == version &&
		operation.Target.Version != nil && *operation.Target.Version == version &&
		operation.Target.Name == assistantString(before, "agentName") &&
		reflect.DeepEqual(operation.Before, before) && reflect.DeepEqual(operation.After, after) &&
		assistantString(operation.Parameters, "agentRef") == operation.Target.Ref &&
		assistantString(operation.Parameters, "versionRef") == assistantString(after, "versionRef"), nil
}

func assistantAgentEnvironmentBindingCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	input := operation.Input
	if !onlyAssistantFields(input, "agentRef", "environmentRef", "versionRef", "expectedVersion") ||
		!hasAssistantFields(input, "agentRef", "environmentRef", "versionRef", "expectedVersion") ||
		assistantString(input, "agentRef") != operation.Target.Ref ||
		assistantString(input, "environmentRef") != assistantString(operation.After, "environmentRef") ||
		assistantString(input, "versionRef") != assistantString(operation.After, "versionRef") {
		return command.Command{}, errs.ErrInvalid
	}
	version, ok := assistantInt64(input, "expectedVersion")
	if !ok || version < 1 || assistantString(input, "environmentRef") == "" || assistantString(input, "versionRef") == "" {
		return command.Command{}, errs.ErrInvalid
	}
	return command.Command{Kind: command.BindAgentRuntimeEnvironment,
		Mutation: value.Mutation{ExpectedVersion: &version},
		Payload: command.RuntimeEnvironmentBindingInput{AgentRef: assistantString(input, "agentRef"),
			EnvironmentRef: assistantString(input, "environmentRef"), VersionRef: assistantString(input, "versionRef")}}, nil
}

func (repository *Repository) lockAssistantBindingEnvironment(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef, environmentRef string,
) error {
	var ref string
	err := tx.QueryRow(ctx, queryConfigurationLockAssistantBindingEnvironment,
		actorScope.organizationID, projectRef, environmentRef).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	}
	if err != nil || ref != environmentRef {
		return errs.ErrUnavailable
	}
	return nil
}
