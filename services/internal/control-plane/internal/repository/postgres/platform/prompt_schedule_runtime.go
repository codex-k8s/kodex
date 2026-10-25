package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryPromptScheduleMaterializeTask embeds exact organization/node replacement of the root
// scheduled run turn content. The caller requires exactly one affected row after authorized
// snapshot materialization; the statement does not create a new turn.
// queryPromptScheduleMaterializeTask встраивает замену содержимого turn корневого запуска
// расписания по точным организации и узлу. Вызывающий код требует ровно одну изменённую строку
// после материализации проверенного снимка; запрос не создаёт новый turn.
//
//go:embed sql/prompt_schedule_materialize_task.sql
var queryPromptScheduleMaterializeTask string

// unwrapSchedulePromptInput requires automation metadata, supported format and exact stored
// raw-input digest, then decodes capture values and removes promptInputsRaw. It adds captured
// template pins when present and returns that template; mutations affect the shared input map and
// do not establish execution authority.
// unwrapSchedulePromptInput требует automation-метаданные, поддерживаемый формат и точный дайджест
// сырых inputs, затем декодирует values и удаляет promptInputsRaw. При наличии добавляет версии
// захваченного шаблона и возвращает его; изменения затрагивают общую входную карту и не
// устанавливают полномочия исполнения.
func unwrapSchedulePromptInput(input map[string]any) (*schedulePromptTemplate, error) {
	automation, ok := input["automation"].(map[string]any)
	if !ok {
		return nil, errs.ErrConflict
	}
	format := runtimeRevisionMapInt64(automation, "promptInputFormat")
	raw := []byte(stringMap(automation, "promptInputsRaw"))
	if (format != 0 && format != 1) || schedulePromptDigest(int(format), raw) != stringMap(automation, "promptInputsDigest") {
		return nil, errs.ErrConflict
	}
	capture, err := decodeSchedulePromptCapture(int(format), raw)
	if err != nil {
		return nil, err
	}
	automation["promptInputs"] = capture.Values
	delete(automation, "promptInputsRaw")
	if capture.Template != nil {
		automation["templateRef"], automation["templateDigest"] = capture.Template.Ref, capture.Template.Digest
	}
	return capture.Template, nil
}

// materializeSchedulePromptTaskTx renders the captured schedule task and returns directly for an
// absent template. Otherwise it updates exactly one root scheduled turn in the caller transaction;
// zero/multiple rows or SQL failure is Conflict. Snapshot changes made by rendering can precede a
// failed update and require caller rollback/discard.
// materializeSchedulePromptTaskTx материализует захваченную задачу расписания и сразу возвращает
// результат при отсутствии шаблона. Иначе обновляет ровно один turn корневого запуска в переданной
// транзакции; иное число строк либо ошибка SQL даёт Conflict. Изменения снимка при рендеринге
// могут предшествовать неудачной записи и требуют rollback либо отбрасывания результата.
func (repository *Repository) materializeSchedulePromptTaskTx(ctx context.Context, tx pgx.Tx, current scope, nodeRef string, template *schedulePromptTemplate, snapshot *entity.PromptMaterializationSnapshot) (string, error) {
	rendered, err := renderSchedulePromptTask(template, snapshot)
	if err != nil || template == nil {
		return rendered, err
	}
	tag, err := tx.Exec(ctx, queryPromptScheduleMaterializeTask, pgx.StrictNamedArgs{"organization_id": current.organizationID, "node_ref": nodeRef, "task": rendered})
	if err != nil || tag.RowsAffected() != 1 {
		return "", errs.ErrConflict
	}
	return rendered, nil
}

// renderSchedulePromptTask returns the current task unchanged when no captured template exists.
// Otherwise it renders only the captured automation template from the original occurrence task,
// requires completeness and a nonblank result up to65536 bytes, and retains user-template sections
// plus referenced semantic slots. It appends rendered provenance/digest and replaces
// task/Automation without database I/O or reinterpolating a prior rendered turn.
// renderSchedulePromptTask возвращает текущую задачу без изменения при отсутствии захваченного
// шаблона. Иначе материализует этот automation-шаблон из исходной задачи occurrence, требует
// полноту и непустой результат до65536 байт и оставляет секции пользовательского шаблона и
// использованные semantic slots. Добавляет происхождение и дайджест результата и заменяет
// task/Automation без обращения к БД и повторной интерполяции ранее отрендеренного turn.
func renderSchedulePromptTask(template *schedulePromptTemplate, snapshot *entity.PromptMaterializationSnapshot) (string, error) {
	if template == nil {
		return snapshot.Variables["task"], nil
	}
	// Исходная .task принадлежит occurrence; retry не читает ранее
	// отрендеренное значение turn и не интерполирует его повторно.
	taskSnapshot := promptservice.FromSnapshot(*snapshot)
	taskSnapshot.ExtraTemplates = nil
	taskSnapshot.TemplateRef, taskSnapshot.TemplateDigest = template.Ref, template.Digest
	taskSnapshot.TargetKind = promptservice.TargetAutomation
	taskSnapshot.StagePurposeTemplate, taskSnapshot.StageExpectedResultTemplate = "", ""
	materialized, err := promptservice.Materialize(template.Content, taskSnapshot)
	if err != nil || !materialized.Complete {
		return "", errs.ErrConflict
	}
	used := map[promptservice.SemanticSlot]bool{}
	for _, slot := range materialized.Slots {
		if slot.Source == "USER_TEMPLATE" {
			used[slot.Slot] = true
		}
	}
	var task strings.Builder
	for _, section := range materialized.FullSections {
		if section.Source == "USER_TEMPLATE" || used[section.Slot] {
			task.WriteString(section.Content)
		}
	}
	if strings.TrimSpace(task.String()) == "" || task.Len() > 65536 {
		return "", errs.ErrConflict
	}
	rendered := task.String()
	digest := sha256.Sum256([]byte(rendered))
	snapshot.ExtraTemplates = append(snapshot.ExtraTemplates, entity.PromptUserTemplate{Kind: "AUTOMATION_TASK", Ref: template.Ref, Digest: template.Digest, Content: template.Content,
		Rendered: &entity.PromptRenderedUserTask{Content: rendered, Digest: hex.EncodeToString(digest[:])}})
	snapshot.Variables["task"] = rendered
	snapshot.Automation = rendered
	return rendered, nil
}
