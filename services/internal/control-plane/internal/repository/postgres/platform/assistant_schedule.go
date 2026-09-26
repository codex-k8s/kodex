package platform

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	scheduleservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/schedule"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

var assistantScheduleEditableFields = []string{
	"name", "targetType", "targetRef", "preset", "cronExpression", "timeOfDay", "dayOfWeek",
	"timezone", "input", "automationText", "sessionPolicy", "notificationPolicy",
}

func (repository *Repository) readAssistantScheduleSnapshot(ctx context.Context, tx pgx.Tx, actorScope scope, projectRef, scheduleRef string) (map[string]any, int64, error) {
	projection, err := repository.resolveAssistantContext(ctx, tx, actorScope,
		entity.AssistantContextDescriptor{EntityKind: "SCHEDULE", EntityRef: scheduleRef}, projectRef)
	if err != nil {
		return nil, 0, err
	}
	if !contains(projection.AllowedOperations, "UPDATE_SCHEDULE") {
		return nil, 0, errs.ErrForbidden
	}
	var version int64
	var state, name, targetType, targetRef, preset, cron, timezone, automationText string
	var sessionPolicy, notificationPolicy, dstGapPolicy, dstFoldPolicy, misfirePolicy, overlapPolicy string
	var inputJSON, promptInputsJSON []byte
	err = tx.QueryRow(ctx, queryConfigurationAssistantScheduleSnapshot, pgx.StrictNamedArgs{
		"organization_id": actorScope.organizationID, "schedule_ref": scheduleRef, "project_ref": projectRef,
	}).Scan(&version, &state, &name, &targetType, &targetRef, &preset, &cron, &timezone,
		&inputJSON, &automationText, &sessionPolicy, &notificationPolicy, &dstGapPolicy,
		&dstFoldPolicy, &misfirePolicy, &overlapPolicy, &promptInputsJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, errs.ErrNotFound
	}
	if err != nil {
		return nil, 0, errs.ErrUnavailable
	}
	if state == "ARCHIVED" {
		return nil, 0, errs.ErrConflict
	}
	var input, promptInputs map[string]any
	if json.Unmarshal(inputJSON, &input) != nil || json.Unmarshal(promptInputsJSON, &promptInputs) != nil || input == nil || promptInputs == nil {
		return nil, 0, errs.ErrUnavailable
	}
	timeOfDay, dayOfWeek, err := scheduleservice.Display(preset, cron)
	if err != nil {
		return nil, 0, errs.ErrUnavailable
	}
	return map[string]any{
		"scheduleRef": scheduleRef, "projectRef": projectRef, "name": name,
		"targetType": targetType, "targetRef": targetRef, "preset": preset,
		"cronExpression": cron, "timeOfDay": timeOfDay, "dayOfWeek": dayOfWeek,
		"timezone": timezone, "input": input, "automationText": automationText,
		"sessionPolicy": sessionPolicy, "notificationPolicy": notificationPolicy,
		"dstGapPolicy": dstGapPolicy, "dstFoldPolicy": dstFoldPolicy,
		"misfirePolicy": misfirePolicy, "overlapPolicy": overlapPolicy,
		"promptInputs": promptInputs,
	}, version, nil
}

func (repository *Repository) hydrateAssistantScheduleOperation(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef string, operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if projectRef == "" || !onlyAssistantFields(operation.Parameters, append([]string{"scheduleRef"}, assistantScheduleEditableFields...)...) {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	ref := assistantString(operation.Parameters, "scheduleRef")
	if ref == "" {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	before, version, err := repository.readAssistantScheduleSnapshot(ctx, tx, actorScope, projectRef, ref)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	return hydrateAssistantScheduleFields(before, version, operation)
}

func hydrateAssistantScheduleFields(before map[string]any, version int64,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	after := cloneAssistantFields(before)
	for _, key := range assistantScheduleEditableFields {
		if value, supplied := operation.Parameters[key]; supplied {
			if text, ok := value.(string); ok {
				value = strings.TrimSpace(text)
			}
			after[key] = value
		}
	}
	if reflect.DeepEqual(before, after) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	if _, _, err := assistantUpdateSchedule(withAssistantExpectedVersion(after, version)); err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "SCHEDULE", Ref: assistantString(before, "scheduleRef"),
		Name: assistantString(before, "name"), Version: &version}
	operation.Parameters = after
	operation.Before = cloneAssistantFields(before)
	operation.After = cloneAssistantFields(after)
	operation.ExpectedVersion = &version
	operation.Selected = true
	return operation, nil
}

func withAssistantExpectedVersion(fields map[string]any, version int64) map[string]any {
	result := cloneAssistantFields(fields)
	result["expectedVersion"] = version
	return result
}

func assistantUpdateSchedule(input map[string]any) (command.ScheduleInput, int64, error) {
	if !onlyAssistantFields(input, "scheduleRef", "projectRef", "name", "targetType", "targetRef", "preset",
		"cronExpression", "timeOfDay", "dayOfWeek", "timezone", "input", "automationText",
		"sessionPolicy", "notificationPolicy", "dstGapPolicy", "dstFoldPolicy", "misfirePolicy",
		"overlapPolicy", "promptInputs", "expectedVersion") {
		return command.ScheduleInput{}, 0, errs.ErrInvalid
	}
	version, valid := assistantInt64(input, "expectedVersion")
	if !valid || version < 1 || assistantString(input, "scheduleRef") == "" {
		return command.ScheduleInput{}, 0, errs.ErrInvalid
	}
	createFields := make(map[string]any, len(assistantScheduleEditableFields)+1)
	for _, key := range append([]string{"projectRef"}, assistantScheduleEditableFields...) {
		if value, exists := input[key]; exists {
			createFields[key] = value
		}
	}
	payload, err := assistantSchedule(createFields)
	if err != nil {
		return command.ScheduleInput{}, 0, err
	}
	promptInputs, ok := assistantObjectValue(input, "promptInputs")
	if !ok || !validBoundedRunInput(promptInputs) {
		return command.ScheduleInput{}, 0, errs.ErrInvalid
	}
	payload.Ref = assistantString(input, "scheduleRef")
	payload.PromptInputs = promptInputs
	payload.DSTGapPolicy = assistantString(input, "dstGapPolicy")
	payload.DSTFoldPolicy = assistantString(input, "dstFoldPolicy")
	payload.MisfirePolicy = assistantString(input, "misfirePolicy")
	payload.OverlapPolicy = assistantString(input, "overlapPolicy")
	if _, err := normalizeScheduleInput(payload, time.Now().UTC()); err != nil {
		return command.ScheduleInput{}, 0, err
	}
	return payload, version, nil
}

func rehydrateEditedAssistantSchedule(original, edited entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if original.Type != "UPDATE_SCHEDULE" || original.Key != edited.Key ||
		original.Target.Kind != "SCHEDULE" || original.Target.Ref == "" ||
		original.ExpectedVersion == nil || *original.ExpectedVersion < 1 || edited.Parameters == nil ||
		!onlyAssistantFields(edited.Parameters, "scheduleRef", "projectRef", "name", "targetType", "targetRef",
			"preset", "cronExpression", "timeOfDay", "dayOfWeek", "timezone", "input", "automationText",
			"sessionPolicy", "notificationPolicy", "dstGapPolicy", "dstFoldPolicy", "misfirePolicy",
			"overlapPolicy", "promptInputs") ||
		assistantString(edited.Parameters, "scheduleRef") != original.Target.Ref {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	for _, key := range []string{"projectRef", "dstGapPolicy", "dstFoldPolicy", "misfirePolicy", "overlapPolicy", "promptInputs"} {
		if value, supplied := edited.Parameters[key]; supplied && !reflect.DeepEqual(value, original.Before[key]) {
			return entity.AssistantPlanOperation{}, errs.ErrForbidden
		}
	}
	parameters := map[string]any{"scheduleRef": original.Target.Ref}
	for _, key := range assistantScheduleEditableFields {
		if value, supplied := edited.Parameters[key]; supplied {
			parameters[key] = value
		}
	}
	edited.Parameters = parameters
	selected := edited.Selected
	hydrated, err := hydrateAssistantScheduleFields(original.Before, *original.ExpectedVersion, edited)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if !reflect.DeepEqual(hydrated.Before, original.Before) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	hydrated.Selected = selected
	return hydrated, nil
}

func (repository *Repository) assistantScheduleUpdateSnapshotMatches(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef string, operation entity.AssistantPlanOperation,
) (bool, error) {
	before, version, err := repository.readAssistantScheduleSnapshot(ctx, tx, actorScope, projectRef, operation.Target.Ref)
	if err != nil {
		return false, err
	}
	return operation.ExpectedVersion != nil && *operation.ExpectedVersion == version &&
		operation.Target.Version != nil && *operation.Target.Version == version &&
		operation.Target.Name == assistantString(before, "name") &&
		reflect.DeepEqual(operation.Before, before) &&
		reflect.DeepEqual(operation.Parameters, operation.After) &&
		assistantString(operation.Parameters, "scheduleRef") == operation.Target.Ref &&
		assistantString(operation.Parameters, "projectRef") == projectRef, nil
}
