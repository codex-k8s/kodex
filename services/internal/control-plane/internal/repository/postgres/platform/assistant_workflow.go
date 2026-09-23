package platform

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

var assistantWorkflowEditableFields = []string{
	"name", "purpose", "instructions", "completionCriteria", "maxConcurrency", "timeoutSeconds",
}

func (repository *Repository) readAssistantWorkflowSnapshot(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef, workflowRef string,
) (map[string]any, int64, error) {
	projection, err := repository.resolveAssistantContext(ctx, tx, actorScope,
		entity.AssistantContextDescriptor{EntityKind: "WORKFLOW", EntityRef: workflowRef}, projectRef)
	if err != nil {
		return nil, 0, err
	}
	if !contains(projection.AllowedOperations, "UPDATE_WORKFLOW") {
		return nil, 0, errs.ErrForbidden
	}
	workflow, err := scanWorkflow(tx.QueryRow(ctx, queryCommandsChangeworkflowSelectAuthoritativeReadback,
		actorScope.organizationID, workflowRef), false)
	if err != nil {
		return nil, 0, err
	}
	if workflow.ProjectRef != projectRef || workflow.State == "ARCHIVED" || workflow.Draft == nil ||
		!contains(workflow.NextActions, "EDIT") {
		return nil, 0, errs.ErrNotFound
	}
	raw, err := json.Marshal(workflow.Draft)
	if err != nil {
		return nil, 0, errs.ErrUnavailable
	}
	var draft map[string]any
	if json.Unmarshal(raw, &draft) != nil || draft == nil {
		return nil, 0, errs.ErrUnavailable
	}
	return map[string]any{
		"workflowRef": workflow.Ref, "projectRef": workflow.ProjectRef,
		"name": workflow.Draft.Name, "purpose": workflow.Draft.Purpose,
		"instructions": workflow.Draft.Instructions, "completionCriteria": workflow.Draft.CompletionCriteria,
		"maxConcurrency": float64(workflow.Draft.Concurrency), "timeoutSeconds": float64(workflow.Draft.TimeoutSeconds),
		"draft": draft,
	}, workflow.Version, nil
}

func (repository *Repository) hydrateAssistantWorkflowOperation(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef string, operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if projectRef == "" || !onlyAssistantFields(operation.Parameters, append([]string{"workflowRef"}, assistantWorkflowEditableFields...)...) {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	ref := assistantString(operation.Parameters, "workflowRef")
	if ref == "" {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	before, version, err := repository.readAssistantWorkflowSnapshot(ctx, tx, actorScope, projectRef, ref)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	return hydrateAssistantWorkflowFields(before, version, operation)
}

func hydrateAssistantWorkflowFields(before map[string]any, version int64,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	after := make(map[string]any, len(assistantWorkflowEditableFields)+2)
	for _, key := range append([]string{"workflowRef", "projectRef"}, assistantWorkflowEditableFields...) {
		after[key] = before[key]
	}
	changed := false
	for _, key := range assistantWorkflowEditableFields {
		value, supplied := operation.Parameters[key]
		if !supplied {
			continue
		}
		switch key {
		case "maxConcurrency", "timeoutSeconds":
			integer, valid := assistantInt64(operation.Parameters, key)
			if !valid || integer < 1 || (key == "maxConcurrency" && integer > 100) ||
				(key == "timeoutSeconds" && integer > 7*24*60*60) {
				return entity.AssistantPlanOperation{}, errs.ErrInvalid
			}
			value = float64(integer)
		default:
			text, valid := value.(string)
			if !valid {
				return entity.AssistantPlanOperation{}, errs.ErrInvalid
			}
			value = strings.TrimSpace(text)
		}
		changed = changed || !reflect.DeepEqual(after[key], value)
		after[key] = value
	}
	if !changed {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	operation.Before = cloneAssistantFields(before)
	operation.After = cloneAssistantFields(after)
	operation.Parameters = after
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "WORKFLOW", Ref: assistantString(before, "workflowRef"),
		Name: assistantString(before, "name"), Version: &version}
	operation.ExpectedVersion = &version
	operation.Selected = true
	operation.Input = nil
	if _, _, err := assistantUpdateWorkflow(operation); err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	return operation, nil
}

func assistantUpdateWorkflow(operation entity.AssistantPlanOperation) (command.WorkflowInput, int64, error) {
	input := operation.Input
	if input == nil {
		input = withAssistantExpectedVersion(operation.Parameters, valueOrZero(operation.ExpectedVersion))
	}
	if !onlyAssistantFields(input, "workflowRef", "projectRef", "name", "purpose", "instructions",
		"completionCriteria", "maxConcurrency", "timeoutSeconds", "expectedVersion") ||
		!hasAssistantFields(input, "workflowRef", "projectRef", "name", "purpose", "instructions",
			"completionCriteria", "maxConcurrency", "timeoutSeconds", "expectedVersion") {
		return command.WorkflowInput{}, 0, errs.ErrInvalid
	}
	version, valid := assistantInt64(input, "expectedVersion")
	if !valid || version < 1 || assistantString(input, "workflowRef") == "" ||
		assistantString(input, "projectRef") == "" || assistantString(input, "workflowRef") != assistantString(operation.Before, "workflowRef") ||
		assistantString(input, "projectRef") != assistantString(operation.Before, "projectRef") {
		return command.WorkflowInput{}, 0, errs.ErrInvalid
	}
	concurrency, validConcurrency := assistantInt64(input, "maxConcurrency")
	timeout, validTimeout := assistantInt64(input, "timeoutSeconds")
	if !validConcurrency || !validTimeout || concurrency < 1 || concurrency > 100 || timeout < 1 || timeout > 7*24*60*60 {
		return command.WorkflowInput{}, 0, errs.ErrInvalid
	}
	raw, err := json.Marshal(operation.Before["draft"])
	if err != nil {
		return command.WorkflowInput{}, 0, errs.ErrInvalid
	}
	var draft entity.WorkflowVersion
	if json.Unmarshal(raw, &draft) != nil || !validWorkflowVersion(draft) {
		return command.WorkflowInput{}, 0, errs.ErrInvalid
	}
	for _, key := range []string{"name", "purpose", "instructions", "completionCriteria"} {
		value, ok := input[key].(string)
		if !ok {
			return command.WorkflowInput{}, 0, errs.ErrInvalid
		}
		switch key {
		case "name":
			draft.Name = value
		case "purpose":
			draft.Purpose = value
		case "instructions":
			draft.Instructions = value
		case "completionCriteria":
			draft.CompletionCriteria = value
		}
	}
	draft.Concurrency, draft.TimeoutSeconds = int32(concurrency), timeout
	if !validWorkflowVersion(draft) {
		return command.WorkflowInput{}, 0, errs.ErrInvalid
	}
	return command.WorkflowInput{Ref: assistantString(input, "workflowRef"), ProjectRef: assistantString(input, "projectRef"),
		Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: draft.CoordinatorAgentRef, Draft: &draft}, version, nil
}

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func rehydrateEditedAssistantWorkflow(original, edited entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if original.Type != "UPDATE_WORKFLOW" || original.Key != edited.Key || original.Target.Kind != "WORKFLOW" ||
		original.Target.Ref == "" || original.ExpectedVersion == nil || *original.ExpectedVersion < 1 ||
		edited.Parameters == nil || !onlyAssistantFields(edited.Parameters,
		append([]string{"workflowRef", "projectRef"}, assistantWorkflowEditableFields...)...) ||
		assistantString(edited.Parameters, "workflowRef") != original.Target.Ref ||
		assistantString(edited.Parameters, "projectRef") != assistantString(original.Before, "projectRef") {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	parameters := map[string]any{"workflowRef": original.Target.Ref}
	for _, field := range assistantWorkflowEditableFields {
		if value, supplied := edited.Parameters[field]; supplied {
			parameters[field] = value
		}
	}
	edited.Parameters = parameters
	selected := edited.Selected
	hydrated, err := hydrateAssistantWorkflowFields(original.Before, *original.ExpectedVersion, edited)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if !reflect.DeepEqual(hydrated.Before, original.Before) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	hydrated.Selected = selected
	return hydrated, nil
}

func (repository *Repository) assistantWorkflowUpdateSnapshotMatches(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef string, operation entity.AssistantPlanOperation,
) (bool, error) {
	before, version, err := repository.readAssistantWorkflowSnapshot(ctx, tx, actorScope, projectRef, operation.Target.Ref)
	if err != nil {
		return false, err
	}
	return operation.ExpectedVersion != nil && *operation.ExpectedVersion == version &&
		operation.Target.Version != nil && *operation.Target.Version == version &&
		operation.Target.Name == assistantString(before, "name") &&
		reflect.DeepEqual(operation.Before, before) &&
		reflect.DeepEqual(operation.Parameters, operation.After) &&
		assistantString(operation.Parameters, "workflowRef") == operation.Target.Ref &&
		assistantString(operation.Parameters, "projectRef") == projectRef, nil
}
