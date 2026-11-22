package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

var (
	// queryEmailPendingClose settles at most one hundred unstarted or running READ obsolete mailbox calls;
	// started mutations and UNKNOWN_OUTCOME are retained without retry or a fabricated receipt.
	// queryEmailPendingClose завершает до ста устаревших неначатых либо running READ mailbox calls;
	// начатые mutations и UNKNOWN_OUTCOME сохраняются без retry и выдуманной квитанции.
	//go:embed sql/email_pending__close.sql
	queryEmailPendingClose string
	// queryEmailPendingCloseTests cancels obsolete read-only health authority and resets TESTING
	// only for the exact connections whose tests were closed in this transaction.
	// queryEmailPendingCloseTests отменяет устаревшие read-only health authority и сбрасывает TESTING
	// лишь у точных подключений, проверки которых закрыты этой транзакцией.
	//go:embed sql/email_pending__close_tests.sql
	queryEmailPendingCloseTests string
)

// settleEmailPendingTx fences stale health and queued call metadata in bounded owner batches.
// Eligibility was already revoked by its owner; this maintenance does not claim a remote mutation
// stopped. Gate cancellation emits its canonical event; failed approved calls emit the existing
// integration event. Both schedule the existing exact continuation and retain consumed counters.
// settleEmailPendingTx ограждает устаревшие health и queued call metadata ограниченными owner batches.
// Eligibility уже отозвана владельцем; maintenance не утверждает остановку remote mutation.
// Отмена Gate публикует его каноническое событие; отказ approved call — существующее integration
// event. Оба назначают прежнее точное continuation и сохраняют использованные counters.
func (repository *Repository) settleEmailPendingTx(ctx context.Context, tx pgx.Tx, current scope, connectionRef string) error {
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "connection_ref": connectionRef}
	rows, err := tx.Query(ctx, queryEmailPendingCloseTests, args)
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	// healthReset records safe aggregate coordinates until rows close before emitting outbox events.
	// healthReset сохраняет безопасные aggregate coordinates до закрытия rows и записи outbox events.
	type healthReset struct {
		ref, state string
		version    int64
	}
	var resets []healthReset
	for rows.Next() {
		var reset healthReset
		if rows.Scan(&reset.ref, &reset.version, &reset.state) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		resets = append(resets, reset)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	for _, reset := range resets {
		if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "INTEGRATION_CONNECTION_CHANGED", "", reset.ref, "i18n:INTEGRATION_CONNECTION_UPDATED", reset.version, reset.state); err != nil {
			return err
		}
	}
	args["actor_id"] = current.actorID
	rows, err = tx.Query(ctx, queryEmailPendingClose, args)
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	// pendingFact carries only safe immutable coordinates for canonical event and continuation writers.
	// pendingFact передаёт только безопасные неизменные coordinates каноническим event/continuation writers.
	type pendingFact struct{ kind, ref, id, node, project, root, state, rootState string }
	var facts []pendingFact
	for rows.Next() {
		var fact pendingFact
		if rows.Scan(&fact.kind, &fact.ref, &fact.id, &fact.node, &fact.project, &fact.root, &fact.state, &fact.rootState) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		facts = append(facts, fact)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	for _, fact := range facts {
		if fact.kind == "GATE" {
			if _, err := repository.emitRunEvent(ctx, tx, current, fact.project, fact.root, fact.ref, "OWNER_GATE_RESOLVED", fact.node, "", fact.ref, "", "i18n:OWNER_GATE_CANCELLED", fact.rootState, "CANCELLED"); err != nil {
				return err
			}
			continue
		}
		if _, err := repository.scheduleIntegrationContinuation(ctx, tx, current, fact.id, fact.project); err != nil {
			return err
		}
		if fact.state == "FAILED" {
			if _, err := repository.emitIntegrationActionEvent(ctx, tx, current, fact.project, fact.root, fact.ref, fact.node, fact.state); err != nil {
				return err
			}
		}
		ref, err := newRef("aud")
		if err != nil {
			return errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, ref, current.organizationID, nil, current.actorID, "platform.email-mailboxes.settle", "INTEGRATION_INVOCATION", fact.ref, "i18n:INTEGRATION_ACTION_FAILED", current.correlationRef); err != nil {
			return errs.ErrUnavailable
		}
	}
	return nil
}
