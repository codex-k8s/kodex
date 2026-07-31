package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/eventing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OutboxStore использует отдельный пул ретранслятора с минимальными полномочиями.
type OutboxStore struct {
	pool        *pgxpool.Pool
	maxAttempts uint32
	terminal    atomic.Uint64
}

var _ eventing.OutboxStore = (*OutboxStore)(nil)

// NewOutboxStore создаёт независимое от брокера хранилище ретранслятора.
func NewOutboxStore(pool *pgxpool.Pool, maxAttempts uint32) (*OutboxStore, error) {
	if pool == nil || maxAttempts < 1 || maxAttempts > 100 {
		return nil, errors.New("control-plane outbox configuration is invalid")
	}
	return &OutboxStore{pool: pool, maxAttempts: maxAttempts}, nil
}

func (store *OutboxStore) Check(ctx context.Context) error {
	var version uint64
	var member, nonSuperuser, noBypassRLS bool
	var terminalEvents uint64
	if err := store.pool.QueryRow(ctx, sqlOutboxCheck).Scan(
		&version,
		&member,
		&nonSuperuser,
		&noBypassRLS,
		&terminalEvents,
	); err != nil {
		return mapError(err)
	}
	store.terminal.Store(terminalEvents)
	if version != 20260731000500 || !member || !nonSuperuser || !noBypassRLS {
		return errors.New("control-plane outbox role is not ready")
	}
	return nil
}

// TerminalEvents возвращает последний bounded readiness readback без payload.
func (store *OutboxStore) TerminalEvents() uint64 {
	return store.terminal.Load()
}

func (store *OutboxStore) Claim(
	ctx context.Context,
	instanceID string,
	limit int,
	leaseDuration time.Duration,
) ([]eventing.ClaimedEvent, error) {
	rows, err := store.pool.Query(
		ctx,
		sqlOutboxClaim,
		pgx.StrictNamedArgs{
			"lease_owner":    instanceID,
			"limit":          limit,
			"lease_duration": postgresInterval(leaseDuration),
		},
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	claimed := make([]eventing.ClaimedEvent, 0, limit)
	for rows.Next() {
		var raw []byte
		var item eventing.ClaimedEvent
		if err := rows.Scan(&raw, &item.LeaseToken, &item.Attempts); err != nil {
			return nil, mapError(err)
		}
		if err := json.Unmarshal(raw, &item.Envelope); err != nil ||
			item.Envelope.Validate() != nil {
			return nil, errors.New("decode canonical outbox envelope")
		}
		claimed = append(claimed, item)
	}
	return claimed, mapError(rows.Err())
}

func (store *OutboxStore) MarkPublished(
	ctx context.Context,
	eventID, leaseToken string,
	receipt eventing.PublishReceipt,
) error {
	if receipt.Stream == "" || receipt.Sequence == 0 {
		return errors.New("outbox broker receipt is invalid")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	tag, err := store.pool.Exec(
		ctx,
		sqlOutboxMarkPublished,
		pgx.StrictNamedArgs{
			"event_id":         eventID,
			"lease_token":      leaseToken,
			"broker_stream":    receipt.Stream,
			"broker_sequence":  receipt.Sequence,
			"broker_duplicate": receipt.Duplicate,
			"published_at":     now,
			"cleanup_after":    now.Add(31 * 24 * time.Hour),
		},
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("outbox publish lease is stale")
	}
	return nil
}

func (store *OutboxStore) MarkFailed(
	ctx context.Context,
	eventID, leaseToken string,
	retryable bool,
	backoff time.Duration,
) error {
	tag, err := store.pool.Exec(
		ctx,
		sqlOutboxMarkFailed,
		pgx.StrictNamedArgs{
			"event_id":     eventID,
			"lease_token":  leaseToken,
			"retryable":    retryable,
			"max_attempts": store.maxAttempts,
			"available_at": time.Now().UTC().Add(backoff),
		},
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("outbox failure lease is stale")
	}
	return nil
}

func postgresInterval(duration time.Duration) string {
	return fmt.Sprintf("%d microseconds", duration.Microseconds())
}
