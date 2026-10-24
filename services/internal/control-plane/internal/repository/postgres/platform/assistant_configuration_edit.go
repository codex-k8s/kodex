package platform

import (
	"context"
	"fmt"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// assistantConfigurationOperationType recognizes only the six supported assistant
// configuration/image/integration operation kinds; it does not validate payloads or grant
// authority.
// assistantConfigurationOperationType распознаёт только шесть поддерживаемых assistant
// configuration/image/integration operation kinds; не проверяет payloads и не выдаёт authority.
func assistantConfigurationOperationType(kind string) bool {
	return kind == "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" || kind == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" || kind == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" || kind == "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" || kind == prepareProjectAssistantConnection || kind == changeProjectAssistantIntegrationGrant
}

// rehydrateEditedAssistantConfiguration requires unchanged type/key and typed parameters, routes
// grant/connection edits to dedicated handlers and rehydrates runtime/image proposals from
// editable fields. Normal edits preserve frozen before/version/target pins; explicit stale refresh
// permits dependency revisions but preserves resource identity/owner/action and selected state,
// never applying the proposal here.
// rehydrateEditedAssistantConfiguration требует неизменные type/key и параметры, направляет
// grant/connection edits отдельным обработчикам и заново готовит runtime/image proposals из
// редактируемых полей. Обычная правка сохраняет frozen before/version/target pins; явный stale
// refresh допускает новые dependency revisions, но сохраняет resource identity/owner/action и
// selected state, не применяя предложение здесь.
func (repository *Repository) rehydrateEditedAssistantConfiguration(ctx context.Context, tx pgx.Tx, current scope, original, edited entity.AssistantPlanOperation, refreshStale bool) (entity.AssistantPlanOperation, error) {
	if original.Type != edited.Type || original.Key != edited.Key || edited.Parameters == nil {
		return edited, errs.ErrForbidden
	}
	if original.Type == changeProjectAssistantIntegrationGrant {
		return repository.rehydrateEditedProjectAssistantGrant(ctx, tx, current, original, edited, refreshStale)
	}
	if original.Type == "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" {
		return repository.rehydrateEditedSystemAssistantGrant(ctx, tx, current, original, edited, refreshStale)
	}
	if original.Type == prepareProjectAssistantConnection {
		return repository.rehydrateEditedProjectAssistantConnection(ctx, tx, current, original, edited, refreshStale)
	}
	request := cloneAssistantFields(edited.Parameters)
	var refreshed entity.AssistantPlanOperation
	var err error
	if original.Type == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" {
		if !refreshStale {
			frozen, normalizeErr := normalizeAssistantOperation(original)
			if normalizeErr != nil {
				return edited, normalizeErr
			}
			matching, snapshotErr := repository.assistantRuntimeConfigurationSnapshotMatches(ctx, tx, current, frozen)
			if snapshotErr != nil || !matching {
				return edited, errs.ErrConflict
			}
		}
		allowed := append(append([]string(nil), assistantRuntimeEditable...), assistantRuntimeOwnerFields...)
		allowed = append(allowed, "providerCatalogPins", "runtimeProfilePin")
		if !onlyAssistantFields(request, allowed...) {
			return edited, errs.ErrInvalid
		}
		for _, field := range append(append([]string(nil), assistantRuntimeOwnerFields...), "providerCatalogPins", "runtimeProfilePin") {
			if !assistantJSONEqual(request[field], original.Parameters[field]) {
				return edited, fmt.Errorf("assistant configuration immutable field %s differs: %w", field, errs.ErrForbidden)
			}
		}
		parameters := map[string]any{}
		for _, field := range assistantRuntimeEditable {
			if value, exists := request[field]; exists {
				parameters[field] = value
			}
		}
		edited.Parameters = parameters
		refreshed, err = repository.hydrateAssistantRuntimeConfiguration(ctx, tx, current, edited)
	} else {
		if !refreshStale {
			frozen, normalizeErr := normalizeAssistantOperation(original)
			if normalizeErr != nil {
				return edited, normalizeErr
			}
			matching, snapshotErr := repository.systemAssistantImageSnapshotMatches(ctx, tx, current, frozen)
			if snapshotErr != nil || !matching {
				return edited, errs.ErrConflict
			}
		}
		if !onlyAssistantFields(request, "systemAssistantRef", "scopeKind", "organizationRef", "agentVersion", "recipeRef", "name", "environmentKey", "dockerfile", "specSha256") {
			return edited, errs.ErrInvalid
		}
		for _, field := range []string{"systemAssistantRef", "scopeKind", "organizationRef", "agentVersion", "recipeRef", "specSha256"} {
			if !assistantJSONEqual(request[field], original.Parameters[field]) {
				return edited, errs.ErrForbidden
			}
		}
		parameters := map[string]any{}
		for _, field := range []string{"systemAssistantRef", "recipeRef", "name", "environmentKey", "dockerfile"} {
			if value, exists := request[field]; exists {
				parameters[field] = value
			}
		}
		edited.Parameters = parameters
		refreshed, err = repository.hydrateSystemAssistantImage(ctx, tx, current, edited)
		if err == nil && !refreshStale && !assistantJSONEqual(refreshed.Parameters["agentVersion"], original.Parameters["agentVersion"]) {
			return edited, errs.ErrConflict
		}
	}
	if err != nil {
		return edited, err
	}
	if refreshStale {
		// После явного STALE меняются только версии зависимостей нового
		// snapshot. Идентичность ресурса и его владелец остаются прежними.
		fields := []string{"systemAssistantRef", "scopeKind", "organizationRef", "recipeRef"}
		if original.Type == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" {
			fields = []string{"agentRef", "assistantScope", "scopeKind", "organizationRef", "projectRef", "assistantProfileRef"}
		}
		for _, field := range fields {
			if !assistantJSONEqual(refreshed.Parameters[field], original.Parameters[field]) {
				return edited, errs.ErrForbidden
			}
		}
		if refreshed.Target.Kind != original.Target.Kind || refreshed.Target.Ref != original.Target.Ref || refreshed.Action != original.Action {
			return edited, errs.ErrForbidden
		}
		refreshed.Selected = edited.Selected
		return refreshed, nil
	}
	comparableTarget := refreshed.Target
	if original.Action == "CREATE" {
		comparableTarget.Name = original.Target.Name
	}
	if !assistantJSONEqual(refreshed.Before, original.Before) || !assistantJSONEqual(refreshed.ExpectedVersion, original.ExpectedVersion) || !assistantJSONEqual(comparableTarget, original.Target) {
		return edited, errs.ErrConflict
	}
	refreshed.Selected = edited.Selected
	return refreshed, nil
}
