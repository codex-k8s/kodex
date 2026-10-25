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

// hydrateAssistantInstructionDraft requires a project and only agentRef/instructions, bounding
// trimmed instruction length to at least 20 and raw length to 65536 bytes. It reads the
// non-system, nonarchived agent in an active tenant/project in the caller transaction and
// delegates pure draft hydration. No rows is NotFound, other SQL failure is Unavailable. It
// prepares a draft operation; publication remains a separate instruction lifecycle command.
// hydrateAssistantInstructionDraft требует project и только agentRef/instructions, ограничивая
// длину instructions минимумом 20 после обрезки пробелов и максимумом 65536 исходных байтов.
// Читает несистемного неархивного агента активного tenant/project в транзакции вызывающего и
// делегирует чистую подготовку draft. Отсутствие строки даёт NotFound, прочий SQL-отказ —
// Unavailable. Готовит операцию черновика; публикация остаётся отдельной командой жизненного цикла
// инструкций.
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

// hydrateAssistantInstructionDraftFields validates instruction text and a positive captured agent
// version, then replaces action, target, parameters, Before, After and version pins and selects
// the operation. Before contains only agent ref/name, After contains ref/text and is
// shallow-copied from parameters. Input and unrelated operation fields are retained; this
// transformation neither persists nor publishes instructions.
// hydrateAssistantInstructionDraftFields проверяет текст инструкций и положительную captured agent
// version, затем заменяет action, target, parameters, Before, After и version pins и выбирает
// operation. Before содержит только ref/name агента, After — ref/text с поверхностной копией
// parameters. Input и остальные поля operation сохраняются; преобразование не сохраняет и не
// публикует инструкции.
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

// rehydrateEditedAssistantInstructionDraft requires the original draft type/key, agent target/ref
// and positive version plus the closed edited parameters with the same agent ref. It hydrates from
// original target/name/version, rejects changed Before as Conflict and restores edited selection.
// It retains the specialized instruction hydrator signature rather than adapting it to
// workflow/schedule/environment editable-field reconstruction. This pure path creates no
// authorization or publication.
// rehydrateEditedAssistantInstructionDraft требует исходные draft type/key, agent target/ref и
// положительную version, а также закрытые edited parameters с прежним agent ref. Выполняет
// hydration из исходных target/name/version, отклоняет изменённый Before как Conflict и
// восстанавливает edited selection. Сохраняет специализированную сигнатуру instruction hydrator,
// не подгоняя её под реконструкцию editable fields workflow/schedule/environment. Чистый путь не
// создаёт полномочий или публикации.
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

// assistantInstructionDraftSnapshotMatches delegates project-managed configuration operations to
// their specialized snapshot check. Otherwise it rereads the same tenant/project agent projection
// and requires both version pins, name, exact ref/name Before, Parameters/After deep equality and
// matching agent ref. SQL errors propagate unchanged here; equality is an optimistic snapshot
// check, not authorization, instruction publication or a content-length validator.
// assistantInstructionDraftSnapshotMatches передаёт project-managed configuration operations
// специализированной проверке snapshot. Иначе повторно читает ту же tenant/project agent
// projection и требует обе version pins, name, точный ref/name Before, глубокое равенство
// Parameters/After и совпадающий agent ref. SQL-ошибки здесь передаются без изменения; равенство
// является optimistic snapshot check, а не авторизацией, публикацией инструкций или проверкой
// длины текста.
func (repository *Repository) assistantInstructionDraftSnapshotMatches(
	ctx context.Context, tx pgx.Tx, actorScope scope, projectRef string,
	operation entity.AssistantPlanOperation,
) (bool, error) {
	if assistantProjectConfigurationOperation(operation) {
		return repository.projectAssistantConfigurationSnapshotMatches(ctx, tx, actorScope, operation)
	}
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
