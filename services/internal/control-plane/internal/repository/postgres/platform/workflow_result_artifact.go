package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryWorkflowResultArtifact resolves only captured immutable evidence, with current root-actor
// eligibility and exact final-catalog pins for earlier CONTINUES producers. It locks the mutable
// artifact HEAD while retaining the selected historical revision, never its current version.
// queryWorkflowResultArtifact разрешает только capture evidence с текущим доступом root actor
// и точными final-catalog pins для прежних производителей CONTINUES. Запрос блокирует изменяемый
// HEAD, сохраняя выбранную историческую ревизию независимо от текущей версии aggregate.
//
//go:embed sql/workflow_result__artifact.sql
var queryWorkflowResultArtifact string

// resolveWorkflowArtifactTx resolves a structurally validated selector against owner data. A missing
// or ambiguous match is a business rejection; a database error stops the enclosing transaction.
// The caller still owns pending-work barriers, the remaining predicates and journal/dispatch.
// resolveWorkflowArtifactTx разрешает структурно проверенный селектор по owner data. Отсутствие
// или неоднозначность — отказ бизнес-проверки; ошибка БД останавливает общую транзакцию. Вызывающий
// отвечает за незавершённую работу, остальные условия и запись журнала/запуск следующего шага.
func resolveWorkflowArtifactTx(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any,
	requirement entity.WorkflowResultRequirement, declaration entity.WorkflowDeclaredResult,
) (entity.WorkflowAcceptedResult, bool, error) {
	if requirement.Kind != "ARTIFACT" || declaration.Key != requirement.Key || declaration.Source == nil {
		return entity.WorkflowAcceptedResult{}, false, nil
	}
	source := declaration.Source
	if source.Kind != "CURRENT_COMPLETION" && source.Kind != "CAPTURED_ARTIFACT" ||
		source.Kind == "CURRENT_COMPLETION" && (source.ArtifactRef != "" || source.Revision != 0 || source.Digest != "") ||
		source.Kind == "CAPTURED_ARTIFACT" && (source.ArtifactRef == "" || source.Revision < 1 || source.Digest == "") {
		return entity.WorkflowAcceptedResult{}, false, nil
	}
	rows, err := tx.Query(ctx, queryWorkflowResultArtifact, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "node_id": stringMap(lease, "nodeID"),
		"root_run_id": stringMap(lease, "rootRunID"), "runtime_revision_id": stringMap(lease, "runtimeRevisionID"),
		"lease_id": stringMap(lease, "leaseID"), "source_kind": source.Kind,
		"artifact_ref": source.ArtifactRef, "artifact_revision": source.Revision, "artifact_digest": source.Digest,
		"file_name": requirement.FileName, "media_type": requirement.MediaType,
	})
	if err != nil {
		return entity.WorkflowAcceptedResult{}, false, errs.ErrUnavailable
	}
	defer rows.Close()
	var result entity.WorkflowAcceptedResult
	var count int
	for rows.Next() {
		var revision int64
		if err := rows.Scan(&result.ArtifactRef, &result.ArtifactRevision, &result.Digest, &revision); err != nil {
			return entity.WorkflowAcceptedResult{}, false, errs.ErrUnavailable
		}
		count++
	}
	if rows.Err() != nil {
		return entity.WorkflowAcceptedResult{}, false, errs.ErrUnavailable
	}
	if count != 1 {
		return entity.WorkflowAcceptedResult{}, false, nil
	}
	result.Key, result.Kind = requirement.Key, requirement.Kind
	return result, true, nil
}
