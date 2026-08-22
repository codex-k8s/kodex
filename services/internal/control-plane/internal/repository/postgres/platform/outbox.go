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
	if err := repository.pool.QueryRow(ctx, `SELECT to_regclass('control_plane.outbox_events')::text`).Scan(&table); err != nil || table == "" {
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
	rows, err := repository.pool.Query(ctx, `WITH candidates AS (
		SELECT e.id FROM control_plane.outbox_events e
		WHERE ((e.state='PENDING' AND e.available_at<=clock_timestamp()) OR (e.state='CLAIMED' AND e.lease_expires_at<clock_timestamp()))
		AND NOT EXISTS(SELECT 1 FROM control_plane.outbox_events predecessor WHERE predecessor.ordering_key=e.ordering_key AND predecessor.sequence<e.sequence AND predecessor.state<>'PUBLISHED')
		ORDER BY e.created_at FOR UPDATE SKIP LOCKED LIMIT $1
	), claimed AS (
		UPDATE control_plane.outbox_events e SET state='CLAIMED',lease_owner=$2,lease_expires_at=clock_timestamp()+$3::interval,attempts=attempts+1
		FROM candidates c WHERE e.id=c.id RETURNING e.event_id::text,e.subject,e.payload,e.attempts
	) SELECT event_id,subject,payload,attempts FROM claimed`, limit, instance+":"+leaseToken, leaseDuration.String())
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
	tag, err := repository.pool.Exec(ctx, `UPDATE control_plane.outbox_events SET state='PUBLISHED',broker_receipt=$3,published_at=clock_timestamp(),lease_owner=NULL,lease_expires_at=NULL WHERE event_id=$1::uuid AND state='CLAIMED' AND lease_owner=$2`, item.EventID, item.LeaseToken, value)
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
	tag, err := repository.pool.Exec(ctx, `UPDATE control_plane.outbox_events SET state=$3,available_at=clock_timestamp()+$4::interval,lease_owner=NULL,lease_expires_at=NULL WHERE event_id=$1::uuid AND state='CLAIMED' AND lease_owner=$2`, item.EventID, item.LeaseToken, state, retryAfter.String())
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}
