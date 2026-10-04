package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

var assistantProjectConfigurationOwnerFields = []string{"projectAssistantRef", "assistantScope", "scopeKind", "organizationRef", "projectRef", "assistantProfileRef"}

var assistantProjectConfigurationDependencyFields = []string{"agentVersion", "runtimeEnvironmentBindingRef", "runtimeEnvironmentVersionRef", "runtimeEnvironmentDigest"}

func assistantProjectConfigurationOperation(operation entity.AssistantPlanOperation) bool {
	return assistantString(operation.Parameters, "projectAssistantRef") != "" && (operation.Type == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" || operation.Type == "CREATE_INSTRUCTION_DRAFT" || operation.Type == "BIND_AGENT_RUNTIME_ENVIRONMENT")
}

func (repository *Repository) authorizeProjectAssistantConfiguration(ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation, frozen bool) (assistantConfigurationTarget, error) {
	target, err := repository.assistantConfigurationTarget(ctx, tx, current, assistantString(operation.Parameters, "projectAssistantRef"))
	if err != nil {
		return target, err
	}
	if target.scopeKind != "PROJECT" || target.profileRef == "" || target.projectRef == "" {
		return target, errs.ErrNotFound
	}
	if frozen {
		fields := map[string]any{"projectAssistantRef": assistantString(operation.Parameters, "projectAssistantRef"), "assistantScope": "PROJECT", "scopeKind": "PROJECT", "organizationRef": current.organizationRef, "projectRef": target.projectRef, "assistantProfileRef": target.profileRef}
		for key, expected := range fields {
			if !assistantJSONEqual(operation.Parameters[key], expected) || !assistantJSONEqual(operation.Before[key], expected) || !assistantJSONEqual(operation.After[key], expected) {
				return target, errs.ErrForbidden
			}
		}
	}
	return target, nil
}

// Locator помощника разрешается отдельно от контекста экрана. Обычные операции
// сотрудников по-прежнему требуют свой экран и проектную boundary.
func (repository *Repository) hydrateProjectAssistantConfiguration(ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	target, err := repository.authorizeProjectAssistantConfiguration(ctx, tx, current, operation, false)
	if err != nil {
		return operation, err
	}
	ref := assistantString(operation.Parameters, "projectAssistantRef")
	view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, current, ref)
	if err != nil {
		return operation, err
	}
	parameters := cloneAssistantFields(operation.Parameters)
	delete(parameters, "projectAssistantRef")
	if _, supplied := parameters["agentRef"]; supplied {
		return operation, errs.ErrInvalid
	}
	if operation.Type == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" {
		_, hasSystemLocator := parameters["systemAssistantRef"]
		if !onlyAssistantFields(parameters, append([]string{"environmentRef"}, assistantEnvironmentProposalFields...)...) || hasSystemLocator {
			return operation, errs.ErrInvalid
		}
		if view.Environment.ScopeKind != "PROJECT" || view.Environment.ProjectRef != target.projectRef || view.Environment.Ref == "" {
			return operation, errs.ErrNotFound
		}
		if supplied, exists := parameters["environmentRef"]; exists {
			requested, valid := supplied.(string)
			if !valid || requested == "" {
				return operation, errs.ErrInvalid
			}
			if requested != view.Environment.Ref {
				return operation, errs.ErrForbidden
			}
		}
		parameters["environmentRef"] = view.Environment.Ref
	} else {
		parameters["agentRef"] = ref
	}
	operation.Parameters = parameters
	switch operation.Type {
	case "PREPARE_RUNTIME_ENVIRONMENT_REVISION":
		operation, err = repository.hydrateAssistantEnvironmentOperation(ctx, tx, current, target.projectRef, operation)
	case "CREATE_INSTRUCTION_DRAFT":
		operation, err = repository.hydrateAssistantInstructionDraft(ctx, tx, current, target.projectRef, operation)
	case "BIND_AGENT_RUNTIME_ENVIRONMENT":
		operation, err = repository.hydrateAssistantAgentEnvironmentBinding(ctx, tx, current, target.projectRef, operation)
	default:
		return operation, errs.ErrInvalid
	}
	if err != nil {
		return operation, err
	}
	owner := map[string]any{"projectAssistantRef": ref, "assistantScope": "PROJECT", "scopeKind": "PROJECT", "organizationRef": current.organizationRef, "projectRef": target.projectRef, "assistantProfileRef": target.profileRef,
		"agentVersion": target.version, "runtimeEnvironmentBindingRef": view.EnvironmentBinding.Ref, "runtimeEnvironmentVersionRef": view.EnvironmentBinding.VersionRef, "runtimeEnvironmentDigest": view.Environment.CurrentVersion.Digest}
	for _, fields := range []map[string]any{operation.Parameters, operation.Before, operation.After} {
		for key, value := range owner {
			fields[key] = value
		}
	}
	return operation, nil
}

func assistantProjectConfigurationEditable(operation entity.AssistantPlanOperation) []string {
	switch operation.Type {
	case "PREPARE_RUNTIME_ENVIRONMENT_REVISION":
		return append([]string{"environmentRef"}, assistantEnvironmentEditableFields...)
	case "CREATE_INSTRUCTION_DRAFT":
		return []string{"instructions"}
	case "BIND_AGENT_RUNTIME_ENVIRONMENT":
		return []string{"environmentRef"}
	}
	return nil
}

func (repository *Repository) refreshProjectAssistantConfiguration(ctx context.Context, tx pgx.Tx, current scope, original, edited entity.AssistantPlanOperation, refreshStale bool) (entity.AssistantPlanOperation, error) {
	if original.Type != edited.Type || original.Key != edited.Key {
		return edited, errs.ErrForbidden
	}
	for key, value := range original.Parameters {
		if !contains(assistantProjectConfigurationEditable(original), key) && !assistantJSONEqual(value, edited.Parameters[key]) {
			return edited, errs.ErrForbidden
		}
	}
	if !onlyAssistantFields(edited.Parameters, assistantMapKeys(original.Parameters)...) {
		return edited, errs.ErrInvalid
	}
	if _, err := repository.authorizeProjectAssistantConfiguration(ctx, tx, current, original, true); err != nil {
		return edited, err
	}
	request := map[string]any{"projectAssistantRef": original.Parameters["projectAssistantRef"]}
	for _, key := range assistantProjectConfigurationEditable(original) {
		if value, exists := edited.Parameters[key]; exists {
			request[key] = value
		}
	}
	edited.Parameters = request
	refreshed, err := repository.hydrateProjectAssistantConfiguration(ctx, tx, current, edited)
	if err != nil {
		return edited, err
	}
	if !refreshStale && (!assistantJSONEqual(refreshed.Before, original.Before) || !assistantJSONEqual(refreshed.ExpectedVersion, original.ExpectedVersion) || !assistantJSONEqual(refreshed.Target, original.Target)) {
		return edited, errs.ErrConflict
	}
	if refreshed.Target.Kind != original.Target.Kind || refreshed.Target.Ref != original.Target.Ref {
		return edited, errs.ErrForbidden
	}
	refreshed.Selected = edited.Selected
	return refreshed, nil
}

func assistantMapKeys(fields map[string]any) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	return keys
}

func (repository *Repository) projectAssistantConfigurationSnapshotMatches(ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation) (bool, error) {
	if _, err := repository.authorizeProjectAssistantConfiguration(ctx, tx, current, operation, true); err != nil {
		return false, err
	}
	refreshed, err := repository.refreshProjectAssistantConfiguration(ctx, tx, current, operation, operation, true)
	if err != nil {
		return false, err
	}
	return assistantJSONEqual(refreshed.Before, operation.Before) && assistantJSONEqual(refreshed.After, operation.After) && assistantJSONEqual(refreshed.ExpectedVersion, operation.ExpectedVersion) && assistantJSONEqual(refreshed.Target, operation.Target), nil
}

func projectAssistantConfigurationCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	for _, key := range append(append([]string(nil), assistantProjectConfigurationOwnerFields...), assistantProjectConfigurationDependencyFields...) {
		if !assistantJSONEqual(operation.Input[key], operation.Before[key]) || !assistantJSONEqual(operation.Input[key], operation.After[key]) {
			return command.Command{}, errs.ErrInvalid
		}
	}
	if assistantString(operation.Input, "assistantScope") != "PROJECT" || assistantString(operation.Input, "scopeKind") != "PROJECT" || assistantString(operation.Input, "organizationRef") == "" || assistantString(operation.Input, "projectRef") == "" || assistantString(operation.Input, "assistantProfileRef") == "" {
		return command.Command{}, errs.ErrInvalid
	}
	operation.Input = cloneAssistantFields(operation.Input)
	for _, key := range append(append([]string(nil), assistantProjectConfigurationOwnerFields...), assistantProjectConfigurationDependencyFields...) {
		if key != "projectRef" || operation.Type != "PREPARE_RUNTIME_ENVIRONMENT_REVISION" {
			delete(operation.Input, key)
		}
	}
	return assistantOperationCommand(operation)
}
