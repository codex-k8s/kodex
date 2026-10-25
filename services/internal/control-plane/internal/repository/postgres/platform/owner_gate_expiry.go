package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// queryOwnerGateExpiryCandidates selects a bounded tenant-owned due set without granting authority.
// queryOwnerGateExpiryCandidates выбирает ограниченный набор просроченных Gate организации без выдачи полномочий.
//
//go:embed sql/owner_gate_expiry__candidates.sql
var queryOwnerGateExpiryCandidates string

// queryOwnerGateExpiryLock serializes the root before the exact gate and reads its current DB clock.
// queryOwnerGateExpiryLock блокирует корень до точного Gate и сверяет текущее время БД.
//
//go:embed sql/owner_gate_expiry__lock.sql
var queryOwnerGateExpiryLock string

// ownerGateExpiredSummary identifies a server expiry, never an owner's rejection or approval.
// ownerGateExpiredSummary обозначает истечение серверного срока, а не отказ или согласие владельца.
const ownerGateExpiredSummary = "i18n:OWNER_GATE_EXPIRED"

// expireWorkflowOwnerGate checks one immutable ordinary-workflow gate under owner locks. An earlier
// execution deadline retains its existing outcome; otherwise this gate expires and the whole root
// closes atomically. Missing, legacy, future and already-closed gates have no expiry effect.
// expireWorkflowOwnerGate проверяет срок обычного workflow Gate под блокировками владельца. Более
// ранний срок исполнения сохраняет прежний исход; иначе Gate истекает и весь корень закрывается
// атомарно. Отсутствующий, прежний, будущий либо закрытый Gate не вызывает нового истечения.
func (repository *Repository) expireWorkflowOwnerGate(ctx context.Context, tx pgx.Tx, current scope, input command.Command, gateRef string) (claimableExecution, string, bool, error) {
	var candidate claimableExecution
	var gateID string
	var expiresAt time.Time
	var due bool
	err := tx.QueryRow(ctx, queryOwnerGateExpiryLock, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "gate_ref": gateRef,
	}).Scan(&gateID, &candidate.nodeID, &candidate.nodeRef, &candidate.runID, &candidate.runRef,
		&candidate.rootRunID, &candidate.projectID, &candidate.projectRef, &candidate.sessionID,
		&candidate.sessionRef, &expiresAt, &due)
	if errors.Is(err, pgx.ErrNoRows) {
		return candidate, "", false, nil
	}
	if err != nil {
		return candidate, "", false, errs.ErrUnavailable
	}
	if !due {
		return candidate, "", false, nil
	}
	deadline, elapsed, err := readRuntimeExecutionDeadline(ctx, tx, current.organizationID, candidate.runID)
	if err != nil {
		return candidate, "", false, err
	}
	if elapsed && !deadline.EffectiveDeadlineAt.After(expiresAt) {
		err = repository.failRuntimeGraph(ctx, tx, current, input, candidate, "RUNTIME_TIMEOUT", runtimeTimeoutSummary)
		return candidate, runtimeTimeoutSummary, err == nil, err
	}
	err = repository.failRuntimeGraphWithGateExpiry(ctx, tx, current, input, candidate, "OWNER_GATE_EXPIRED", ownerGateExpiredSummary, gateID)
	return candidate, ownerGateExpiredSummary, err == nil, err
}

// reconcileOwnerGateExpiries uses the existing authorized runtime claim poll as a bounded tick;
// each candidate is re-read under locks and only committed graph changes count as work.
// reconcileOwnerGateExpiries использует прежний авторизованный claim poll как ограниченный цикл;
// каждый кандидат повторно читается под блокировкой, работой считаются только изменения графа.
func (repository *Repository) reconcileOwnerGateExpiries(ctx context.Context, tx pgx.Tx, current scope, input command.Command, limit int32) ([]claimableExecution, error) {
	rows, err := tx.Query(ctx, queryOwnerGateExpiryCandidates, pgx.StrictNamedArgs{"organization_id": current.organizationID, "limit": limit})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	var refs []string
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	var changed []claimableExecution
	for _, ref := range refs {
		candidate, _, expired, err := repository.expireWorkflowOwnerGate(ctx, tx, current, input, ref)
		if err != nil {
			return nil, err
		}
		if expired {
			changed = append(changed, candidate)
		}
	}
	return changed, nil
}
