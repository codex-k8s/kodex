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

//go:embed sql/prompt_schedule_materialize_task.sql
var queryPromptScheduleMaterializeTask string

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
