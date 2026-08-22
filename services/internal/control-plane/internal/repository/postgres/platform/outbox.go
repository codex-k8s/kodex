package platform

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/eventing"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
)

type OutboxItem struct {
	EventID, Subject, LeaseToken string
	Payload                      []byte
	Attempts                     uint32
}

func (repository *Repository) CheckOutbox(ctx context.Context) error {
	var table string
	if err := repository.pool.QueryRow(ctx, queryOutboxCheckOutboxTable).Scan(&table); err != nil || table == "" {
		return errors.New("control-plane outbox is unavailable")
	}
	return nil
}

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

func (repository *Repository) MarkOutboxPublished(ctx context.Context, item OutboxItem, receipt eventing.PublishReceipt) error {
	value := fmt.Sprintf("%s:%d:%t", receipt.Stream, receipt.Sequence, receipt.Duplicate)
	tag, err := repository.pool.Exec(ctx, queryOutboxMarkoutboxpublishedUpdateOutboxEventsStateBrokerReceiptPublishedAt, item.EventID, item.LeaseToken, value)
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}

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
