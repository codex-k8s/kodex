package platform

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// OutboxItem carries a claimed event ID/subject/payload, lease-owner token and incremented attempt
// count for the relay. It is internal delivery state; payload and token must not become arbitrary
// metric labels or public diagnostics.
// OutboxItem хранит claimed event ID/subject/payload, lease-owner token и увеличенный attempt
// count для relay. Это внутреннее состояние доставки; payload и token не становятся произвольными
// metric labels и публичной диагностикой.
type OutboxItem struct {
	EventID, Subject, LeaseToken string
	Payload                      []byte
	Attempts                     uint32
}

// CheckOutbox checks only that the outbox table resolves, returning a fixed English error
// otherwise. It neither validates broker retention/durability, ordering progress or publisher
// identity nor performs a publish.
// CheckOutbox проверяет лишь наличие разрешаемой outbox table, иначе возвращает фиксированную
// английскую ошибку. Не проверяет broker retention/durability, ordering progress и publisher
// identity и не выполняет publish.
func (repository *Repository) CheckOutbox(ctx context.Context) error {
	var table string
	if err := repository.pool.QueryRow(ctx, queryOutboxCheckOutboxTable).Scan(&table); err != nil || table == "" {
		return errors.New("control-plane outbox is unavailable")
	}
	return nil
}

// ClaimOutbox validates nonempty instance, batch1..128 and a minimum one-second lease, then
// atomically claims due or expired-claim rows with a fresh owner token. SQL blocks any unpublished
// predecessor of the same ordering key. Scan/iteration failures discard returned items although
// the pool query may already have claimed rows; lease expiry provides later recovery.
// ClaimOutbox проверяет непустой instance, batch1..128 и lease минимум одну секунду, затем
// атомарно захватывает due либо expired-claim rows со свежим owner token. SQL блокирует любого
// unpublished predecessor того же ordering key. Scan/iteration errors отбрасывают items, хотя pool
// query уже мог захватить строки; последующее восстановление использует lease expiry.
func (repository *Repository) ClaimOutbox(ctx context.Context, instance string, limit int, leaseDuration time.Duration) ([]OutboxItem, error) {
	if instance == "" || limit < 1 || limit > 128 || leaseDuration < time.Second {
		return nil, errs.ErrInvalid
	}
	leaseToken, err := newRef("obl")
	if err != nil {
		return nil, err
	}
	rows, err := repository.pool.Query(ctx, queryOutboxClaimPublishableEvents, limit, instance+":"+leaseToken, leaseDuration.String())
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]OutboxItem, 0, limit)
	for rows.Next() {
		var item OutboxItem
		if err := rows.Scan(&item.EventID, &item.Subject, &item.Payload, &item.Attempts); err != nil {
			return nil, errs.ErrUnavailable
		}
		item.LeaseToken = instance + ":" + leaseToken
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.ErrUnavailable
	}
	return items, nil
}

// MarkOutboxPublished formats the supplied broker stream/sequence/duplicate receipt and marks
// exactly one CLAIMED event for its lease-owner token as PUBLISHED, clearing lease fields. Exec or
// affected-row mismatch is Conflict. The relay must supply a verified broker receipt; this method
// does not independently validate it or assert fresh lease time.
// MarkOutboxPublished форматирует переданную broker stream/sequence/duplicate receipt и отмечает
// ровно один CLAIMED event с его lease-owner token как PUBLISHED, очищая lease fields. Ошибка Exec
// либо affected-row mismatch даёт Conflict. Relay передаёт проверенную broker receipt; метод
// самостоятельно её не проверяет и не доказывает свежесть lease time.
func (repository *Repository) MarkOutboxPublished(ctx context.Context, item OutboxItem, receipt eventing.PublishReceipt) error {
	value := fmt.Sprintf("%s:%d:%t", receipt.Stream, receipt.Sequence, receipt.Duplicate)
	tag, err := repository.pool.Exec(ctx, queryOutboxMarkoutboxpublishedUpdateOutboxEventsStateBrokerReceiptPublishedAt, item.EventID, item.LeaseToken, value)
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}

// MarkOutboxFailed returns the owned CLAIMED event to PENDING at the supplied retry interval, or
// DEAD_LETTER from attempt100 onward, clearing the lease. Exactly one row is required or Conflict
// returns. Retry interval is not bounded here and dead-letter remains an ordering predecessor;
// this method does not skip or republish it.
// MarkOutboxFailed возвращает принадлежащий claimant event из CLAIMED в PENDING с переданным retry
// interval либо DEAD_LETTER начиная с attempt100, очищая lease. Требуется ровно одна строка, иначе
// Conflict. Retry interval здесь не ограничен, dead-letter остаётся ordering predecessor; метод не
// пропускает и не публикует его повторно.
func (repository *Repository) MarkOutboxFailed(ctx context.Context, item OutboxItem, retryAfter time.Duration) error {
	state := "PENDING"
	if item.Attempts >= 100 {
		state = "DEAD_LETTER"
	}
	tag, err := repository.pool.Exec(ctx, queryOutboxMarkoutboxfailedUpdateOutboxEventsStateAvailableAtLeaseOwner, item.EventID, item.LeaseToken, state, retryAfter.String())
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}
