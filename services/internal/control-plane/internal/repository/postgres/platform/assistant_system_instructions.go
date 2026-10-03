package platform

import (
	"context"
	"reflect"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) hydrateAssistantSystemInstructions(
	ctx context.Context,
	tx pgx.Tx,
	actorScope scope,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if !onlyAssistantFields(operation.Parameters, "systemAssistantRef", "instructions") {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	assistant, err := repository.getAssistantTx(ctx, tx, actorScope)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	requestedRef := assistantString(operation.Parameters, "systemAssistantRef")
	if assistant.Ref == "" || requestedRef != assistant.Ref && requestedRef != assistant.StableKey {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	return hydrateAssistantSystemInstructionFields(assistant, operation)
}

func hydrateAssistantSystemInstructionFields(
	assistant entity.SystemAssistant,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	instructions, ok := operation.Parameters["instructions"].(string)
	if !ok || len(instructions) > 20000 || assistant.Version < 1 {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	instructions = strings.TrimSpace(instructions)
	if instructions == strings.TrimSpace(assistant.OwnerInstructions) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	before := map[string]any{
		"systemAssistantRef": assistant.Ref,
		"name":               assistant.Name,
		"instructions":       assistant.OwnerInstructions,
	}
	after := map[string]any{
		"systemAssistantRef": assistant.Ref,
		"instructions":       instructions,
	}
	version := assistant.Version
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{
		Kind: "SYSTEM_ASSISTANT", Ref: assistant.Ref, Name: assistant.Name, Version: &version,
	}
	operation.Parameters = after
	operation.Before = before
	operation.After = cloneAssistantFields(after)
	operation.ExpectedVersion = &version
	operation.Selected = true
	operation.Input = nil
	return operation, nil
}

func rehydrateEditedAssistantSystemInstructions(
	original entity.AssistantPlanOperation,
	edited entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if original.Type != "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS" || original.Key != edited.Key ||
		original.Target.Kind != "SYSTEM_ASSISTANT" || original.Target.Ref == "" ||
		original.ExpectedVersion == nil || *original.ExpectedVersion < 1 || edited.Parameters == nil ||
		!onlyAssistantFields(edited.Parameters, "systemAssistantRef", "instructions") ||
		assistantString(edited.Parameters, "systemAssistantRef") != original.Target.Ref {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	selected := edited.Selected
	assistant := entity.SystemAssistant{
		Ref:               original.Target.Ref,
		Name:              assistantString(original.Before, "name"),
		OwnerInstructions: assistantString(original.Before, "instructions"),
		Version:           *original.ExpectedVersion,
	}
	hydrated, err := hydrateAssistantSystemInstructionFields(assistant, edited)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if !reflect.DeepEqual(hydrated.Before, original.Before) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	hydrated.Selected = selected
	return hydrated, nil
}

func (repository *Repository) assistantSystemInstructionsSnapshotMatches(
	ctx context.Context,
	tx pgx.Tx,
	actorScope scope,
	operation entity.AssistantPlanOperation,
) (bool, error) {
	assistant, err := repository.getAssistantTx(ctx, tx, actorScope)
	if err != nil {
		return false, err
	}
	before := map[string]any{
		"systemAssistantRef": assistant.Ref,
		"name":               assistant.Name,
		"instructions":       assistant.OwnerInstructions,
	}
	return operation.ExpectedVersion != nil && *operation.ExpectedVersion == assistant.Version &&
		operation.Target.Version != nil && *operation.Target.Version == assistant.Version &&
		operation.Target.Ref == assistant.Ref && operation.Target.Name == assistant.Name &&
		reflect.DeepEqual(operation.Before, before) && reflect.DeepEqual(operation.Parameters, operation.After), nil
}

func assistantSystemInstructionsCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	if !onlyAssistantFields(operation.Input, "systemAssistantRef", "instructions", "expectedVersion") ||
		!hasAssistantFields(operation.Input, "systemAssistantRef", "instructions", "expectedVersion") ||
		assistantString(operation.Input, "systemAssistantRef") != operation.Target.Ref {
		return command.Command{}, errs.ErrInvalid
	}
	version, ok := assistantInt64(operation.Input, "expectedVersion")
	instructions, instructionsOK := operation.Input["instructions"].(string)
	if !ok || version < 1 || !instructionsOK || len(instructions) > 20000 {
		return command.Command{}, errs.ErrInvalid
	}
	return command.Command{
		Kind:    command.UpdateAssistantInstructions,
		Payload: command.AssistantInstructionsInput{Instructions: instructions},
		Mutation: value.Mutation{
			ExpectedVersion: &version,
		},
	}, nil
}
