package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// assistantProjectConfigurationOwnerFields lists the server-owned project-assistant
// identity/scope/profile fields preserved across plan snapshots and removed before ordinary
// command conversion.
// assistantProjectConfigurationOwnerFields перечисляет серверные project-assistant
// identity/scope/profile fields, сохраняемые в plan snapshots и удаляемые до обычного command
// conversion.
var assistantProjectConfigurationOwnerFields = []string{"projectAssistantRef", "assistantScope", "scopeKind", "organizationRef", "projectRef", "assistantProfileRef"}

// assistantProjectConfigurationDependencyFields lists agent/environment binding/version/digest
// pins that must agree across frozen command input, before and after snapshots.
// assistantProjectConfigurationDependencyFields перечисляет agent/environment
// binding/version/digest pins, которые должны совпасть в frozen command input, before и after
// snapshots.
var assistantProjectConfigurationDependencyFields = []string{"agentVersion", "runtimeEnvironmentBindingRef", "runtimeEnvironmentVersionRef", "runtimeEnvironmentDigest"}

// assistantProjectConfigurationOperation recognizes only a nonempty projectAssistantRef paired
// with environment preparation, instruction draft creation or environment binding; the locator
// itself carries no authority.
// assistantProjectConfigurationOperation распознаёт только непустой projectAssistantRef вместе с
// environment preparation, instruction draft creation либо environment binding; сам locator не
// несёт authority.
func assistantProjectConfigurationOperation(operation entity.AssistantPlanOperation) bool {
	return assistantString(operation.Parameters, "projectAssistantRef") != "" && (operation.Type == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" || operation.Type == "CREATE_INSTRUCTION_DRAFT" || operation.Type == "BIND_AGENT_RUNTIME_ENVIRONMENT")
}

// authorizeProjectAssistantConfiguration resolves mutation authority from the canonical assistant
// target and requires a project/profile. Frozen mode additionally compares server-owned identity
// fields in parameters/before/after, rejecting payload owner substitution.
// authorizeProjectAssistantConfiguration разрешает mutation authority по каноническому assistant
// target и требует project/profile. Frozen mode дополнительно сравнивает серверные identity fields
// в parameters/before/after, отклоняя подстановку owner в payload.
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

// hydrateProjectAssistantConfiguration resolves the assistant independently of screen context,
// rejects an injected agentRef/system locator, and binds environment changes to the current
// project environment. Dedicated ordinary hydrators prepare the three supported operations, then
// server-owned identity and runtime dependency pins are attached to parameters/before/after
// without applying changes.
// hydrateProjectAssistantConfiguration разрешает assistant независимо от screen context, отклоняет
// подставленный agentRef/system locator и связывает environment changes с текущим project
// environment. Отдельные обычные hydrators готовят три поддерживаемые операции, затем серверные
// identity и runtime dependency pins добавляются в parameters/before/after без применения
// изменений.
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

// assistantProjectConfigurationEditable returns the specific editable field set for environment
// preparation, instruction draft or environment binding; unsupported kinds have no editable
// fields.
// assistantProjectConfigurationEditable возвращает отдельный набор редактируемых полей для
// environment preparation, instruction draft либо environment binding; неподдерживаемые kinds не
// имеют редактируемых полей.
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

// refreshProjectAssistantConfiguration rejects operation identity changes, altered immutable
// parameters and added fields, revalidates the frozen owner and rehydrates only allowed edits.
// Normal edits require the original before/version/target, while explicit stale refresh permits
// dependency changes but retains target identity and selected state.
// refreshProjectAssistantConfiguration отклоняет изменение operation identity, immutable
// parameters и добавленные поля, повторно проверяет frozen owner и заново готовит лишь допустимые
// правки. Обычные правки требуют прежние before/version/target, а явный stale refresh допускает
// dependency changes, сохраняя target identity и selected state.
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

// assistantMapKeys returns a newly allocated unsorted key list for an allowed-field membership
// check; map iteration order has no authority or serialization meaning.
// assistantMapKeys возвращает новый неупорядоченный список ключей для allowed-field membership
// check; порядок обхода map не задаёт authority или serialization.
func assistantMapKeys(fields map[string]any) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	return keys
}

// projectAssistantConfigurationSnapshotMatches revalidates frozen owner pins, explicitly
// rehydrates current dependencies and compares exact before/after/expected-version/target
// snapshots without applying the operation.
// projectAssistantConfigurationSnapshotMatches повторно проверяет frozen owner pins, явно
// восстанавливает текущие dependencies и сравнивает точные before/after/expected-version/target
// snapshots без применения операции.
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

// projectAssistantConfigurationCommand requires owner/dependency fields to agree in
// input/before/after and nonempty project identity, clones input and strips assistant-only
// metadata before ordinary command conversion. Environment preparation alone retains projectRef;
// no principal authority is inferred from these fields.
// projectAssistantConfigurationCommand требует совпадение owner/dependency fields в
// input/before/after и непустую project identity, копирует input и удаляет assistant-only metadata
// перед обычным command conversion. Только environment preparation сохраняет projectRef; principal
// authority из этих полей не выводится.
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
