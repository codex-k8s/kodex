package platform

import (
	"context"
	_ "embed"
	"errors"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryIntegrationApprovalLockCallScope linearizes fresh owner call admission with explicit revoke; a reservation alone does not admit an external call.
// queryIntegrationApprovalLockCallScope упорядочивает свежий owner call admission и explicit revoke; одна reservation не допускает внешний вызов.
//
//go:embed sql/integration_approval__lock_call_scope.sql
var queryIntegrationApprovalLockCallScope string

// queryIntegrationApprovalExpireQueued selects a bounded set of known unstarted mutations or read calls; RUNNING and uncertain effects are never declared absent.
// queryIntegrationApprovalExpireQueued выбирает ограниченные known unstarted mutations либо read calls; RUNNING и uncertain effects не объявляются отсутствующими.
//
//go:embed sql/integration_approval__expire_queued.sql
var queryIntegrationApprovalExpireQueued string

// lockIntegrationApprovalCallTx takes the same durable scope row as revoke, while allowing only server-recorded read-only reconciliation after consent expiry.
// lockIntegrationApprovalCallTx блокирует ту же durable scope row, что revoke; после expiry допускается только server-recorded read-only reconciliation.
func lockIntegrationApprovalCallTx(ctx context.Context, tx pgx.Tx, organizationID, invocationID string) error {
	var id string
	err := tx.QueryRow(ctx, queryIntegrationApprovalLockCallScope, invocationID, organizationID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	}
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	return nil
}

// expireQueuedIntegrationApprovalsTx atomically settles bounded non-started work, schedules existing continuation, and emits the canonical owner event and audit; no provider request or fake effect receipt is created.
// expireQueuedIntegrationApprovalsTx атомарно завершает bounded non-started work, назначает existing continuation и пишет canonical owner event и audit; provider request либо fake effect receipt не создаются.
func (repository *Repository) expireQueuedIntegrationApprovalsTx(ctx context.Context, tx pgx.Tx, current scope, workload, route string, sshLane bool) error {
	rows, err := tx.Query(ctx, queryIntegrationApprovalExpireQueued, current.organizationID, workload, route, sshLane)
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	type expiredApproval struct{ id, ref, rootID, projectID, nodeRef, reason string }
	expired := []expiredApproval{}
	for rows.Next() {
		var item expiredApproval
		if err := rows.Scan(&item.id, &item.ref, &item.rootID, &item.projectID, &item.nodeRef, &item.reason); err != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		expired = append(expired, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	for _, item := range expired {
		if _, err := repository.scheduleIntegrationContinuation(ctx, tx, current, item.id, item.projectID); err != nil {
			return err
		}
		if _, err := repository.emitIntegrationActionEvent(ctx, tx, current, item.projectID, item.rootID, item.ref, item.nodeRef, "FAILED"); err != nil {
			return err
		}
		ref, err := newRef("aud")
		if err != nil {
			return errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, ref, current.organizationID, nil, current.actorID, "platform.integration-approvals.expire", "INTEGRATION_INVOCATION", item.ref, "i18n:"+item.reason, current.correlationRef); err != nil {
			return errs.ErrUnavailable
		}
	}
	return nil
}
