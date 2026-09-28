package platform

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// Черновик не публикуется планом помощника: публикация остается отдельным
// переходом штатного жизненного цикла инструкций сотрудника.
func (repository *Repository) hydrateAssistantInstructionDraft(
	ctx context.Context, tx pgx.Tx, actorScope scope, projectRef string,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if projectRef == "" || !onlyAssistantFields(operation.Parameters, "agentRef", "instructions") {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	ref, content := assistantString(operation.Parameters, "agentRef"), assistantString(operation.Parameters, "instructions")
	if ref == "" || len(strings.TrimSpace(content)) < 20 || len(content) > 65536 {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	var name, purpose, roleDescription, avatarURL string
	var version int64
	err := tx.QueryRow(ctx, queryConfigurationHydrateassistantoperationSelectAgent,
		actorScope.organizationID, projectRef, ref,
	).Scan(&name, &purpose, &roleDescription, &avatarURL, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AssistantPlanOperation{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.AssistantPlanOperation{}, errs.ErrUnavailable
	}
	return hydrateAssistantInstructionDraftFields(ref, name, version, operation)
}

func hydrateAssistantInstructionDraftFields(ref, name string, version int64,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	content, valid := operation.Parameters["instructions"].(string)
	if !valid || len(strings.TrimSpace(content)) < 20 || len(content) > 65536 || version < 1 {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	before := map[string]any{"agentRef": ref, "name": name}
	after := map[string]any{"agentRef": ref, "instructions": content}
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "AGENT", Ref: ref, Name: name, Version: &version}
	operation.Parameters = after
	operation.Before = before
	operation.After = cloneAssistantFields(after)
	operation.ExpectedVersion = &version
	operation.Selected = true
	return operation, nil
}

func rehydrateEditedAssistantInstructionDraft(original, edited entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if original.Type != "CREATE_INSTRUCTION_DRAFT" || original.Key != edited.Key ||
		original.Target.Kind != "AGENT" || original.Target.Ref == "" ||
		original.ExpectedVersion == nil || *original.ExpectedVersion < 1 ||
		edited.Parameters == nil || !onlyAssistantFields(edited.Parameters, "agentRef", "instructions") ||
		assistantString(edited.Parameters, "agentRef") != original.Target.Ref {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	selected := edited.Selected
	hydrated, err := hydrateAssistantInstructionDraftFields(original.Target.Ref,
		assistantString(original.Before, "name"), *original.ExpectedVersion, edited)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if !reflect.DeepEqual(hydrated.Before, original.Before) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	hydrated.Selected = selected
	return hydrated, nil
}

func (repository *Repository) assistantInstructionDraftSnapshotMatches(
	ctx context.Context, tx pgx.Tx, actorScope scope, projectRef string,
	operation entity.AssistantPlanOperation,
) (bool, error) {
	var name, purpose, roleDescription, avatarURL string
	var version int64
	err := tx.QueryRow(ctx, queryConfigurationHydrateassistantoperationSelectAgent,
		actorScope.organizationID, projectRef, operation.Target.Ref,
	).Scan(&name, &purpose, &roleDescription, &avatarURL, &version)
	if err != nil {
		return false, err
	}
	before := map[string]any{"agentRef": operation.Target.Ref, "name": name}
	return operation.ExpectedVersion != nil && *operation.ExpectedVersion == version &&
		operation.Target.Version != nil && *operation.Target.Version == version &&
		operation.Target.Name == name && reflect.DeepEqual(operation.Before, before) &&
		reflect.DeepEqual(operation.Parameters, operation.After) &&
		assistantString(operation.Parameters, "agentRef") == operation.Target.Ref, nil
}
