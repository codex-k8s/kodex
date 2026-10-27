package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// sshComposeAdmissionScanLimit bounds one durable progress batch without limiting queue length.
	// sshComposeAdmissionScanLimit ограничивает одну durable progress batch, но не длину очереди.
	sshComposeAdmissionScanLimit = 32
	// sshComposeAdmissionBlockSummary names the closed user-facing progress reason.
	// sshComposeAdmissionBlockSummary задаёт closed пользовательскую причину progress.
	sshComposeAdmissionBlockSummary = "i18n:INTEGRATION_ADMISSION_BLOCKED"
	// sshComposeAdmissionBlockAudit identifies the owner audit action without provider details.
	// sshComposeAdmissionBlockAudit обозначает owner audit action без provider details.
	sshComposeAdmissionBlockAudit = "integration.ssh-compose.admission.block"
)

// querySSHComposeBlockAdmission pins the original invocation after candidate rollback, takes
// the root lock without waiting, and marks only admission without changing the external outcome.
// querySSHComposeBlockAdmission закрепляет исходный invocation после candidate rollback,
// берёт root lock без ожидания и блокирует только admission, не меняя внешний outcome.
//
//go:embed sql/ssh_compose__block_admission.sql
var querySSHComposeBlockAdmission string

// permanentSSHComposeAdmissionDenial recognizes authoritative current eligibility refusals only.
// Infrastructure, context, serialization and lock failures must never become durable decisions.
// permanentSSHComposeAdmissionDenial распознаёт только авторитетные current eligibility отказы.
// Infrastructure, context, serialization и lock failures не становятся durable decisions.
func permanentSSHComposeAdmissionDenial(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, errs.ErrUnavailable) || errors.Is(err, errs.ErrConflict) || serializableTransactionConflict(err) {
		return false
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		return false
	}
	return errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound)
}

// rollbackSSHComposeAdmissionCandidate releases partial candidate writes under independent bounded
// cleanup; caller cancellation never permits claim processing to continue after this rollback.
// rollbackSSHComposeAdmissionCandidate откатывает partial candidate writes с отдельным bounded
// cleanup; отмена caller не разрешает продолжить обработку claim после rollback.
func rollbackSSHComposeAdmissionCandidate(ctx context.Context, candidate pgx.Tx) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := candidate.Rollback(cleanup); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// blockSSHComposeAdmissionTx persists monotonic admission denial and its audit/event atomically.
// State, result, safe error, receipt, snapshot and execution generation stay unchanged. An inverse
// root-to-invocation writer is never waited on; transient lock failure rolls back the owner batch.
// blockSSHComposeAdmissionTx атомарно сохраняет monotonic admission denial, audit и event.
// State, result, safe error, receipt, snapshot и execution generation не меняются. Функция не ждёт
// inverse root-to-invocation writer; transient lock failure откатывает owner batch.
func (repository *Repository) blockSSHComposeAdmissionTx(ctx context.Context, tx pgx.Tx, current scope, item integrationInvocationCandidate) error {
	if ctx.Err() != nil {
		return errs.ErrUnavailable
	}
	var invocationRef, nodeRef, projectID, rootID string
	err := tx.QueryRow(ctx, querySSHComposeBlockAdmission, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "invocation_id": item.id,
		"expected_state": item.state, "expected_generation": item.generation,
		"expected_input_digest": item.inputDigest, "expected_snapshot": item.sshSnapshot,
	}).Scan(&invocationRef, &nodeRef, &projectID, &rootID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	}
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	auditRef, _ := newRef("aud")
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef, current.organizationID, projectID, current.actorID,
		sshComposeAdmissionBlockAudit, "INTEGRATION_INVOCATION", invocationRef, sshComposeAdmissionBlockSummary, current.correlationRef); err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	_, err = repository.emitRunEventWithBinding(ctx, tx, current, projectID, rootID, invocationRef, "TURN_PROGRESS", nodeRef, "", "", "", nil, nil,
		sshComposeAdmissionBlockSummary, "", "", invocationRef)
	return err
}
