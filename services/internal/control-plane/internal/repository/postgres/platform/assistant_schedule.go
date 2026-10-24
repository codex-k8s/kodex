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

// assistantScheduleEditableFields lists the schedule form values that may overlay a captured
// snapshot, including target, timing, input, session and notification choices. Project identity
// and DST/misfire/overlap/prompt pins remain outside this list and are checked/preserved
// separately; the list does not authorize a target or validate a value.
// assistantScheduleEditableFields перечисляет значения формы schedule для наложения на captured
// snapshot, включая target, timing, input, session и notification choices. Project identity и
// DST/misfire/overlap/prompt pins находятся вне списка и проверяются/сохраняются отдельно; список
// не авторизует target и не валидирует значение.
var assistantScheduleEditableFields = []string{
	"name", "targetType", "targetRef", "preset", "cronExpression", "timeOfDay", "dayOfWeek",
	"timezone", "input", "automationText", "sessionPolicy", "notificationPolicy",
}

// readAssistantScheduleSnapshot requires UPDATE_SCHEDULE from the exact assistant context, then
// reads the current revision under tenant/project/active-project SQL predicates in the caller
// transaction. No rows is NotFound, other read/JSON/display errors are Unavailable and ARCHIVED is
// Conflict. It returns nonnil input/prompt maps, display timing and retained policy pins with
// schedule version, without acquiring a new grant or writing state.
// readAssistantScheduleSnapshot требует UPDATE_SCHEDULE из точного assistant context, затем читает
// current revision с SQL-ограничениями tenant/project/active-project в транзакции вызывающего.
// Отсутствие строки даёт NotFound, прочие read/JSON/display errors дают Unavailable, ARCHIVED даёт
// Conflict. Возвращает ненулевые input/prompt maps, display timing и сохранённые policy pins с
// schedule version без нового grant или записи состояния.
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

// hydrateAssistantScheduleOperation requires a nonempty project and scheduleRef with only the
// closed editable parameters, then resolves the authoritative schedule snapshot/version in the
// supplied transaction. It returns the schedule-specific pure hydration result unchanged;
// malformed shape/ref is Invalid and current read authority is never derived from edited values.
// hydrateAssistantScheduleOperation требует непустые project и scheduleRef только с закрытыми
// editable parameters, затем разрешает авторитетные schedule snapshot/version в переданной
// транзакции. Возвращает неизменённый результат чистой schedule hydration; неверные shape/ref дают
// Invalid, текущие полномочия чтения не выводятся из edited values.
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

// hydrateAssistantScheduleFields shallow-copies Before, overlays only supplied editable values and
// trims strings, rejecting an unchanged snapshot. It validates the schedule command with the
// captured version, then restores target/Before/After/version and selects the operation. Unlike
// workflow hydration it retains the existing Input field and stores the merged map as After rather
// than a canonical typed-draft projection; no state or authority is created here.
// hydrateAssistantScheduleFields поверхностно копирует Before, накладывает только заданные
// editable values и обрезает пробелы строк, отклоняя неизменённый snapshot. Проверяет schedule
// command с captured version, затем восстанавливает target/Before/After/version и выбирает
// operation. В отличие от workflow hydration сохраняет существующее поле Input и хранит merged map
// в After вместо canonical typed-draft projection; состояние или полномочия здесь не создаются.
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

// rehydrateAssistantEditableFields fills a fresh nonnil caller-owned seed map for the prevalidated
// workflow/schedule/environment callers with only supplied closed editable keys, preserving
// explicit nils and nested aliases. Callers retain reference and SYSTEM seed construction. The
// selected pure hydrator receives original Before/version; its error propagates and a changed
// Before is Conflict, then edited Selected is restored. Callers must validate kind/key/ref and a
// nonnil positive version first. The helper mutates only that seed/envelope and neither authorizes
// nor reads, persists, retries or chooses a command from user input.
// rehydrateAssistantEditableFields дополняет новую ненулевую seed map вызывающего для
// предварительно проверенных workflow/schedule/environment callers только заданными закрытыми
// editable keys с сохранением явных nil и вложенных aliases. Формирование reference и SYSTEM seed
// остаётся у callers. Выбранный pure hydrator получает исходные Before/version; ошибка передаётся
// выше, изменённый Before даёт Conflict, затем восстанавливается edited Selected. Callers обязаны
// заранее проверить kind/key/ref и ненулевой указатель на положительную version. Helper меняет
// только эту seed/envelope и не авторизует, не читает, не сохраняет, не повторяет и не выбирает
// команду из пользовательского input.
func rehydrateAssistantEditableFields(original, edited entity.AssistantPlanOperation,
	parameters map[string]any, editableFields []string,
	hydrate func(map[string]any, int64, entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error),
) (entity.AssistantPlanOperation, error) {
	for _, key := range editableFields {
		if value, supplied := edited.Parameters[key]; supplied {
			parameters[key] = value
		}
	}
	edited.Parameters = parameters
	selected := edited.Selected
	hydrated, err := hydrate(original.Before, *original.ExpectedVersion, edited)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if !reflect.DeepEqual(hydrated.Before, original.Before) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	hydrated.Selected = selected
	return hydrated, nil
}

// withAssistantExpectedVersion makes a nonnil shallow copy of the fields and overwrites
// expectedVersion with the supplied int64. Nested values remain shared and the original map is not
// changed; the helper does not validate positivity or perform an optimistic-lock check.
// withAssistantExpectedVersion создаёт ненулевую поверхностную копию fields и заменяет
// expectedVersion переданным int64. Вложенные значения остаются общими, исходная map не меняется;
// helper не проверяет положительность и не выполняет optimistic-lock check.
func withAssistantExpectedVersion(fields map[string]any, version int64) map[string]any {
	result := cloneAssistantFields(fields)
	result["expectedVersion"] = version
	return result
}

// assistantUpdateSchedule accepts only the update field set and a positive
// expectedVersion/nonempty schedule ref, then parses create-shaped editable fields through the
// existing schedule parser. It requires bounded prompt inputs, restores DST/misfire/overlap pins
// and validates against the current UTC clock. The returned value remains the parsed payload, not
// the normalizer return value; owner/version authority and persistence belong to later command
// processing.
// assistantUpdateSchedule принимает только update field set с положительной expectedVersion и
// непустым schedule ref, затем разбирает create-shaped editable fields существующим schedule
// parser. Требует bounded prompt inputs, восстанавливает DST/misfire/overlap pins и валидирует по
// текущим UTC-часам. Возвращает разобранный payload, а не результат normalizer; owner/version
// authority и сохранение принадлежат последующей обработке команды.
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

// rehydrateEditedAssistantSchedule checks original UPDATE_SCHEDULE key/kind/ref/version and the
// closed edited shape before pure reconstruction. A supplied project or DST/misfire/overlap/prompt
// pin must deeply equal original Before; omitted protected fields are retained by hydration. The
// shared helper forwards only supplied editable keys and restores selection without replacing
// original snapshot/version or adding authorization.
// rehydrateEditedAssistantSchedule проверяет исходные UPDATE_SCHEDULE key/kind/ref/version и
// закрытую edited shape до чистой реконструкции. Заданный project либо DST/misfire/overlap/prompt
// pin должен глубоко совпадать с original Before; пропущенные защищённые fields сохраняются
// hydration. Общий helper передаёт только заданные editable keys и восстанавливает selection без
// замены исходных snapshot/version или добавления полномочий.
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
	return rehydrateAssistantEditableFields(original, edited, map[string]any{"scheduleRef": original.Target.Ref}, assistantScheduleEditableFields, hydrateAssistantScheduleFields)
}

// assistantScheduleUpdateSnapshotMatches re-reads the authoritative schedule snapshot in the
// supplied transaction and requires both version pins, target name, complete Before,
// Parameters/After deep equality and exact schedule/project refs. Read errors propagate and
// mismatch returns false. It neither canonicalizes After as workflow does nor commits a schedule
// update or grants execution authority.
// assistantScheduleUpdateSnapshotMatches повторно читает авторитетный schedule snapshot в
// переданной транзакции и требует обе version pins, target name, полный Before, глубокое равенство
// Parameters/After и точные schedule/project refs. Ошибки чтения передаются выше, несовпадение
// даёт false. Не канонизирует After как workflow, не фиксирует update schedule и не выдаёт
// полномочия исполнения.
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
