package receipt

import (
	"context"
	_ "embed"
	"time"

	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	"github.com/jackc/pgx/v5"
)

// rememberSQL stores an exact correlated owner receipt and permits only the defined monotone-version/idempotent updates without changing lineage.
// rememberSQL сохраняет точную связанную квитанцию владельца и допускает только заданные монотонные либо идемпотентные обновления без смены происхождения.
//
//go:embed sql/owner_receipt__remember.sql
var rememberSQL string

// pendingSQL selects a bounded ordered batch of due unknown receipts without a conclusive decision or unlocked source.
// pendingSQL выбирает ограниченную упорядоченную пачку доступных unknown-квитанций без определённого решения или разблокированного источника.
//
//go:embed sql/owner_receipt__pending.sql
var pendingSQL string

// claimReconciliationSQL conditionally advances the next-check time for the exact owner reference/version/digest that is still due and unresolved.
// claimReconciliationSQL условно повышает время следующей проверки точных owner reference/version/digest, ещё доступных и неразрешённых.
//
//go:embed sql/owner_receipt__claim.sql
var claimReconciliationSQL string

// commitReconciliationSQL locks the exact local/owner pair, atomically audits a fresh decision and unlocks its source, or recognizes an identical replay.
// commitReconciliationSQL блокирует точную пару local/owner, атомарно записывает audit свежего решения и разблокирует источник либо распознаёт идентичный повтор.
//
//go:embed sql/owner_receipt__commit.sql
var commitReconciliationSQL string

var _ port.ReconciliationRepository = (*Repository)(nil)

// Remember checks owner/local scope, digest, input and outcome correlation before a monotone guarded upsert and requires exactly one affected row.
// A conflicting lineage or rejected version update returns conflict without replacing the stored observation.
// Remember проверяет связь owner/local по области, дайджесту, входу и исходу до монотонного защищённого upsert и требует ровно одну изменённую строку.
// Конфликт происхождения либо отвергнутое обновление версии даёт conflict без подмены сохранённого наблюдения.
func (r *Repository) Remember(ctx context.Context, scope port.Scope, record port.Record, owner port.OwnerReceipt, after time.Time) error {
	if owner.Ref == "" || owner.Version < 1 || owner.Invocation == "" || owner.Connection == "" || owner.Mailbox != scope.Mailbox || owner.ExternalRef != record.ID || owner.ExternalDigest != record.ExternalDigest(scope) || owner.InputDigest != record.Digest || owner.EffectKey != record.Key || owner.ConfigurationRevision != record.Audit.ConfigurationRevision || owner.Outcome != record.Outcome() || after.IsZero() {
		return errs.Invalid
	}
	tag, err := r.Pool.Exec(ctx, rememberSQL, pgx.StrictNamedArgs{"tenant": scope.Tenant, "mailbox": scope.Mailbox, "id": record.ID, "input": record.Digest, "owner_ref": owner.Ref, "version": owner.Version, "invocation": owner.Invocation, "connection": owner.Connection, "external_digest": owner.ExternalDigest, "outcome": owner.Outcome, "after": after})
	if err != nil {
		return errs.Unavailable
	}
	if tag.RowsAffected() != 1 {
		return errs.Conflict
	}
	return nil
}

// Pending reads at most 1..64 due unresolved observations with explicit columns, closes rows and discards partial results on scan/iteration failure.
// It materializes matching owner correlations; the domain validates each Pending before requesting a decision.
// Pending читает до 1..64 доступных неразрешённых наблюдений с явными колонками, закрывает rows и отбрасывает частичный результат при отказе scan/iteration.
// Материализуются соответствующие owner-связи; каждый Pending проверяет домен до запроса решения.
func (r *Repository) Pending(ctx context.Context, batch int) ([]port.Pending, error) {
	if batch < 1 || batch > 64 {
		return nil, errs.Invalid
	}
	rows, err := r.Pool.Query(ctx, pendingSQL, pgx.StrictNamedArgs{"batch": batch})
	if err != nil {
		return nil, errs.Unavailable
	}
	defer rows.Close()
	result := make([]port.Pending, 0, batch)
	for rows.Next() {
		var p port.Pending
		x, o := &p.Record, &p.Owner
		if err := rows.Scan(&p.Scope.Tenant, &p.Scope.Mailbox, &x.ID, &x.Key, &x.Digest, &x.Status, &x.Resource, &x.Audit.Actor, &x.Audit.Agent, &x.Audit.Grant, &x.Audit.Operation, &x.Audit.ConfigurationRevision, &x.Audit.CredentialGeneration, &x.Audit.GateApproved, &o.Ref, &o.Version, &o.Invocation, &o.Connection, &o.ExternalDigest); err != nil {
			return nil, errs.Unavailable
		}
		o.ExternalRef, o.InputDigest, o.EffectKey, o.Mailbox, o.ConfigurationRevision, o.Outcome = x.ID, x.Digest, x.Key, p.Scope.Mailbox, x.Audit.ConfigurationRevision, port.Unknown
		result = append(result, p)
	}
	if rows.Err() != nil {
		return nil, errs.Unavailable
	}
	return result, nil
}

// ClaimReconciliation accepts a 5-second..5-minute interval and conditionally reschedules the exact still-due owner snapshot.
// Zero affected rows mean no claim; this method does not execute the decision or resend the original mailbox operation.
// ClaimReconciliation допускает интервал 5 секунд..5 минут и условно переносит проверку точного ещё доступного owner-снимка.
// Ноль изменённых строк означает отсутствие claim; метод не исполняет решение и не повторяет исходную операцию ящика.
func (r *Repository) ClaimReconciliation(ctx context.Context, p port.Pending, interval time.Duration) (bool, error) {
	if interval < 5*time.Second || interval > 5*time.Minute {
		return false, errs.Invalid
	}
	tag, err := r.Pool.Exec(ctx, claimReconciliationSQL, pgx.StrictNamedArgs{"tenant": p.Scope.Tenant, "mailbox": p.Scope.Mailbox, "id": p.Record.ID, "owner": p.Owner.Ref, "version": p.Owner.Version, "digest": p.Owner.ExternalDigest, "seconds": interval.Seconds()})
	if err != nil {
		return false, errs.Unavailable
	}
	return tag.RowsAffected() == 1, nil
}

// CommitReconciliation requires an exact fresh domain decision and bounds the atomic SQL commit by one second and expiry minus 100 ms.
// It distinguishes an actual source unlock from an identical replay and rejects neither-changed-nor-replayed as conflict, without repeating protocol I/O.
// CommitReconciliation требует точное свежее доменное решение и ограничивает атомарный SQL-commit секундой и сроком минус 100 мс.
// Реальная разблокировка источника отличается от идентичного повтора; отсутствие обоих даёт conflict без повторного протокольного I/O.
func (r *Repository) CommitReconciliation(ctx context.Context, p port.Pending, d port.Decision) (bool, error) {
	if !d.ValidFor(p, time.Now()) {
		return false, errs.Denied
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	ctx, expires := context.WithDeadline(ctx, d.ExpiresAt.Add(-100*time.Millisecond))
	defer expires()
	var changed, replay bool
	err := r.Pool.QueryRow(ctx, commitReconciliationSQL, pgx.StrictNamedArgs{"tenant": p.Scope.Tenant, "mailbox": p.Scope.Mailbox, "id": p.Record.ID, "input": p.Record.Digest, "owner": p.Owner.Ref, "owner_version": p.Owner.Version, "invocation": p.Owner.Invocation, "connection": p.Owner.Connection, "digest": p.Owner.ExternalDigest, "decision": d.Ref, "decision_version": d.Version, "outcome": d.Outcome, "actor": d.Actor, "grant": d.Grant, "expires": d.ExpiresAt}).Scan(&changed, &replay)
	if err != nil {
		return false, errs.Unavailable
	}
	if !changed && !replay {
		return false, errs.Conflict
	}
	return changed, nil
}
