package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// assistantLeaseContext holds the conversation and actor fields read under the lease-owned
// transaction lock. It is intermediate data and does not itself authorize a plan or update.
// assistantLeaseContext содержит поля диалога и субъекта, прочитанные под transaction lock,
// принадлежащим lease. Это промежуточные данные, не разрешающие план или изменение сами по себе.
type assistantLeaseContext struct {
	conversationID, conversationRef, projectID, projectRef, assistantRef, contextKind, contextRef string
	conversationVersion                                                                           int64
	allowedOperations                                                                             []string
	actorScope                                                                                    scope
}

// readAssistantLeaseContext executes the existing organization/run-scoped locking query after
// the caller validates its lease, preserving column order and mapping any read error to Forbidden.
// It carries correlation only; callers assign additional authority and perform their own checks.
// readAssistantLeaseContext исполняет прежний locking query по organization/run после проверки
// lease вызывающим, сохраняя порядок столбцов и отображая любую ошибку чтения в Forbidden.
// Переносит только correlation; дополнительные полномочия и проверки остаются у вызывающих.
func readAssistantLeaseContext(ctx context.Context, tx pgx.Tx, machineScope scope, runID any) (assistantLeaseContext, error) {
	result := assistantLeaseContext{actorScope: scope{correlationRef: machineScope.correlationRef}}
	if err := tx.QueryRow(ctx, queryRuntimeProposeassistantplanSelectContext, machineScope.organizationID, runID).Scan(
		&result.conversationID, &result.conversationRef, &result.conversationVersion, &result.projectID, &result.projectRef,
		&result.allowedOperations, &result.contextKind, &result.contextRef, &result.assistantRef,
		&result.actorScope.actorID, &result.actorScope.actorRef, &result.actorScope.actorName, &result.actorScope.role,
		&result.actorScope.organizationRef,
	); err != nil {
		return assistantLeaseContext{}, errs.ErrForbidden
	}
	return result, nil
}
