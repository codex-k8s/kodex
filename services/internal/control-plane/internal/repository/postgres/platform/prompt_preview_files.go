package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryPromptPreviewKnowledge embeds exact agent-bound CLEAN/ACTIVE historical knowledge revisions
// with matching immutable content digest/size, ordered and bounded to 2049 rows. Actor artifact
// permission is checked separately before issuance.
// queryPromptPreviewKnowledge встраивает точные исторические knowledge-ревизии агента со статусами
// CLEAN/ACTIVE и совпадающими неизменяемыми дайджестом и размером, упорядоченные и ограниченные
// 2049 строками. Права субъекта на артефакты проверяются отдельно до выдачи.
//
//go:embed sql/prompt_preview_knowledge.sql
var queryPromptPreviewKnowledge string

// queryPromptPreviewSessionSets embeds finalized attachment sets from prior session turns in the
// exact organization/project, excluding the current set and ordering by first use. Limit513 lets
// the caller reject more than512 historical sets.
// queryPromptPreviewSessionSets встраивает финализированные наборы вложений прежних turns сессии
// точных организации и проекта, исключая текущий набор и упорядочивая по первому использованию.
// Лимит513 позволяет отклонить более512 исторических наборов.
//
//go:embed sql/prompt_preview_session_sets.sql
var queryPromptPreviewSessionSets string

// promptPreviewSessionFilesTx reads and closes historical attachment-set rows before processing at
// most512 sets through the existing typed attachment preview path. It then refreshes file scopes;
// a later error may follow snapshot mutation, so callers must discard the incomplete preview. It
// neither reads object bytes nor commits.
// promptPreviewSessionFilesTx читает и закрывает строки исторических наборов перед обработкой
// максимум512 наборов через прежний типизированный путь preview вложений. Затем обновляет файловые
// области; поздняя ошибка возможна после изменения снимка, поэтому неполный preview отбрасывается.
// Не читает содержимое объектов и не выполняет Commit.
func (repository *Repository) promptPreviewSessionFilesTx(ctx context.Context, tx pgx.Tx, current scope, sessionID, projectID, currentSetRef string, snapshot *entity.PromptMaterializationSnapshot) error {
	rows, err := tx.Query(ctx, queryPromptPreviewSessionSets, pgx.StrictNamedArgs{"organization_id": current.organizationID, "session_id": sessionID, "project_id": projectID, "current_set_ref": currentSetRef})
	if err != nil {
		return errs.ErrUnavailable
	}
	var sets []sealedAttachmentSet
	for rows.Next() {
		var set sealedAttachmentSet
		if rows.Scan(&set.ID, &set.Ref, &set.ManifestDigest, &set.Purpose, &set.ItemCount, &set.TotalSizeBytes) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		sets = append(sets, set)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return errs.ErrUnavailable
	}
	if len(sets) > 512 {
		return errs.ErrConflict
	}
	for _, set := range sets {
		if err := repository.promptPreviewAttachmentSetTx(ctx, tx, current, projectID, snapshot, set, true); err != nil {
			return err
		}
	}
	return refreshPromptFileScopes(snapshot)
}

// promptPreviewKnowledgeTx loads exact knowledge descriptors only when the artifact capability is
// enabled, appends them and authorizes their refs for the current actor. NotFound disables file
// scopes as PERMISSION_REQUIRED; other failures propagate. It refreshes structured file metadata
// without granting content access or committing.
// promptPreviewKnowledgeTx загружает точные knowledge-дескрипторы только при включённой artifact
// capability, добавляет их и проверяет доступ текущего субъекта к ссылкам. NotFound отключает
// файловые области с PERMISSION_REQUIRED; другие ошибки передаются выше. Обновляет структурные
// метаданные без выдачи доступа к содержимому и Commit.
func (repository *Repository) promptPreviewKnowledgeTx(ctx context.Context, tx pgx.Tx, current scope, snapshot *entity.PromptMaterializationSnapshot) error {
	if !capabilityEnabled(snapshot.AgentCapabilities, runtimecontract.ArtifactCapability) {
		return refreshPromptFileScopes(snapshot)
	}
	rows, err := tx.Query(ctx, queryPromptPreviewKnowledge, pgx.StrictNamedArgs{"organization_id": current.organizationID, "agent_ref": snapshot.ContextPin.AgentRef})
	if err != nil {
		return errs.ErrUnavailable
	}
	refs := []string{}
	for rows.Next() {
		item := runtimecontract.RunnerInputArtifact{Scope: runtimecontract.AttachmentScopeKnowledge, AttachmentPurpose: "PROJECT_KNOWLEDGE", Provenance: "PROJECT_BINDING"}
		if err := rows.Scan(&item.Ref, &item.FileName, &item.MediaType, &item.Digest, &item.SizeBytes, &item.Revision, &item.Version, &item.Source, &item.Position); err != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		snapshot.Artifacts = append(snapshot.Artifacts, item)
		refs = append(refs, item.Ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return errs.ErrUnavailable
	}
	if err := repository.authorizePromptArtifactsTx(ctx, tx, current, snapshot.ProjectRef, refs); err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			disablePromptFileScopes(snapshot, "PERMISSION_REQUIRED")
			return refreshPromptFileScopes(snapshot)
		}
		return err
	}
	return refreshPromptFileScopes(snapshot)
}

// refreshPromptFileScopes clears artifact descriptors when capability/permission requires it,
// rejects numeric metadata outside the nonnegative JSON-safe integer range and rebuilds six
// structured file scopes. Existing workspace/other fields remain; the caller supplies an
// initialized structured map. Mutations are not rolled back on a later descriptor error.
// refreshPromptFileScopes очищает дескрипторы артефактов при требуемом отказе capability или
// permission, отклоняет числовые поля вне неотрицательного JSON-safe диапазона целых и
// пересобирает шесть файловых областей. Прежние workspace и прочие поля сохраняются; вызывающий
// код передаёт инициализированную структурную карту. Поздняя ошибка дескриптора не откатывает
// изменения.
func refreshPromptFileScopes(snapshot *entity.PromptMaterializationSnapshot) error {
	if !capabilityEnabled(snapshot.AgentCapabilities, runtimecontract.ArtifactCapability) {
		disablePromptFileScopes(snapshot, "CAPABILITY_REQUIRED")
	} else if snapshot.UnavailableVariables["input.files"] == "PERMISSION_REQUIRED" {
		snapshot.Artifacts = nil
	}
	items := make([]map[string]any, 0, len(snapshot.Artifacts))
	for _, item := range snapshot.Artifacts {
		for _, number := range []int64{item.SizeBytes, item.Revision, item.Version, item.Position} {
			if number < 0 || number > 9007199254740991 {
				return errs.ErrConflict
			}
		}
		items = append(items, map[string]any{"ref": item.Ref, "fileName": item.FileName, "mediaType": item.MediaType, "digest": item.Digest, "sizeBytes": float64(item.SizeBytes),
			"revision": float64(item.Revision), "version": float64(item.Version), "source": item.Source, "position": float64(item.Position), "scope": item.Scope, "attachmentSetRef": item.AttachmentSetRef, "attachmentPurpose": item.AttachmentPurpose, "provenance": item.Provenance})
	}
	structured, err := promptStructuredVariables(items, nil, runtimecontract.RuntimeEnvironmentImage{}, snapshot.ContextPin.EnvironmentVersionRef, snapshot.ContextPin.AttachmentSetRef, snapshot.ContextPin.WorkflowRef)
	if err != nil {
		return errs.ErrConflict
	}
	for _, scope := range []string{"input", "run", "session", "workflow", "gate", "project"} {
		current, ok := snapshot.StructuredVariables[scope].(map[string]any)
		if !ok {
			current = map[string]any{}
			snapshot.StructuredVariables[scope] = current
		}
		for key, value := range structured[scope].(map[string]any) {
			current[key] = value
		}
	}
	return nil
}

// disablePromptFileScopes marks files/count/directory/manifest variables unavailable across
// input/run/session/workflow/gate/project and clears artifact descriptors. It leaves unrelated
// variables and structured maps intact for subsequent refresh, performing no storage or
// authorization operation.
// disablePromptFileScopes помечает недоступными files/count/directory/manifest переменные
// input/run/session/workflow/gate/project и очищает дескрипторы артефактов. Сохраняет прочие
// переменные и структурные карты для последующего обновления, не обращаясь к хранилищу и не
// проверяя полномочия.
func disablePromptFileScopes(snapshot *entity.PromptMaterializationSnapshot, reason string) {
	if snapshot.UnavailableVariables == nil {
		snapshot.UnavailableVariables = map[string]string{}
	}
	for _, scope := range []string{"input", "run", "session", "workflow", "gate", "project"} {
		for _, field := range []string{"files", "files_count", "files_dir", "manifest_path"} {
			snapshot.UnavailableVariables[scope+"."+field] = reason
		}
	}
	snapshot.Artifacts = nil
}
