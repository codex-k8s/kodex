package eventing

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

// Publisher отправляет неизменяемый конверт и проверяет контракт брокера.
type Publisher interface {
	Publish(context.Context, Envelope) (PublishReceipt, error)
	Check(context.Context) error
	Close() error
}

// PublishReceipt хранит устойчивое подтверждение брокером точного события.
type PublishReceipt struct {
	Stream    string
	Sequence  uint64
	Duplicate bool
}

// ClaimedEvent связывает событие с назначенным сервером токеном аренды.
type ClaimedEvent struct {
	Envelope   Envelope
	LeaseToken string
	Attempts   uint32
}

// OutboxStore — узкий порт устойчивого журнала исходящих событий PostgreSQL.
type OutboxStore interface {
	Check(context.Context) error
	Claim(context.Context, string, int, time.Duration) ([]ClaimedEvent, error)
	MarkPublished(context.Context, string, string, PublishReceipt) error
	MarkFailed(context.Context, string, string, bool, time.Duration) error
}

// RelayConfig задаёт ограниченный жизненный цикл доставки.
type RelayConfig struct {
	InstanceID      string
	BatchSize       int
	PollInterval    time.Duration
	LeaseDuration   time.Duration
	PublishTimeout  time.Duration
	FinalizeTimeout time.Duration
	MaxAttempts     uint32
	InitialBackoff  time.Duration
	MaximumBackoff  time.Duration
}

// Relay доставляет outbox как минимум один раз.
type Relay struct {
	config    RelayConfig
	store     OutboxStore
	publisher Publisher
	claimed   atomic.Uint64
	published atomic.Uint64
	failed    atomic.Uint64
}

type RelayStats struct {
	Claimed   uint64
	Published uint64
	Failed    uint64
}

// NewRelay проверяет конфигурацию и зависимости.
func NewRelay(config RelayConfig, store OutboxStore, publisher Publisher) (*Relay, error) {
	if config.InstanceID == "" || len(config.InstanceID) > 128 ||
		config.BatchSize < 1 || config.BatchSize > 128 ||
		config.PollInterval < 50*time.Millisecond ||
		config.LeaseDuration < time.Second ||
		config.PublishTimeout <= 0 || config.FinalizeTimeout <= 0 ||
		config.PublishTimeout+config.FinalizeTimeout >= config.LeaseDuration ||
		config.MaxAttempts < 1 || config.MaxAttempts > 100 ||
		config.InitialBackoff <= 0 || config.MaximumBackoff < config.InitialBackoff ||
		store == nil || publisher == nil {
		return nil, errors.New("event relay configuration is invalid")
	}
	return &Relay{config: config, store: store, publisher: publisher}, nil
}

// Check проверяет журнал исходящих событий PostgreSQL и точный контракт брокера.
func (relay *Relay) Check(ctx context.Context) error {
	if err := relay.store.Check(ctx); err != nil {
		return fmt.Errorf("check outbox: %w", err)
	}
	if err := relay.publisher.Check(ctx); err != nil {
		return fmt.Errorf("check publisher: %w", err)
	}
	return nil
}

// Run ограниченно получает, публикует и завершает события до отмены жизненного цикла.
func (relay *Relay) Run(lifecycle, finalizeParent context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-lifecycle.Done():
			return lifecycle.Err()
		case <-timer.C:
			if err := relay.cycle(lifecycle, finalizeParent); err != nil {
				return err
			}
			timer.Reset(relay.config.PollInterval)
		}
	}
}

func (relay *Relay) cycle(lifecycle, finalizeParent context.Context) error {
	claimed, err := relay.store.Claim(
		lifecycle,
		relay.config.InstanceID,
		relay.config.BatchSize,
		relay.config.LeaseDuration,
	)
	if err != nil {
		return fmt.Errorf("claim outbox: %w", err)
	}
	relay.claimed.Add(uint64(len(claimed)))
	for _, item := range claimed {
		publishCtx, cancelPublish := context.WithTimeout(lifecycle, relay.config.PublishTimeout)
		publishReceipt, publishErr := relay.publisher.Publish(
			publishCtx,
			item.Envelope,
		)
		cancelPublish()

		finalizeCtx, cancelFinalize := context.WithTimeout(
			finalizeParent,
			relay.config.FinalizeTimeout,
		)
		if publishErr == nil {
			err = relay.store.MarkPublished(
				finalizeCtx,
				item.Envelope.EventID,
				item.LeaseToken,
				publishReceipt,
			)
			if err == nil {
				relay.published.Add(1)
			}
		} else {
			err = relay.store.MarkFailed(
				finalizeCtx,
				item.Envelope.EventID,
				item.LeaseToken,
				true,
				relay.backoff(item.Attempts),
			)
			if err == nil {
				relay.failed.Add(1)
			}
		}
		cancelFinalize()
		if err != nil {
			return fmt.Errorf("finalize outbox: %w", err)
		}
	}
	return nil
}

func (relay *Relay) Stats() RelayStats {
	return RelayStats{
		Claimed:   relay.claimed.Load(),
		Published: relay.published.Load(),
		Failed:    relay.failed.Load(),
	}
}

func (relay *Relay) backoff(attempts uint32) time.Duration {
	backoff := relay.config.InitialBackoff
	for current := uint32(0); current < attempts; current++ {
		if backoff >= relay.config.MaximumBackoff/2 {
			return relay.config.MaximumBackoff
		}
		backoff *= 2
	}
	if backoff > relay.config.MaximumBackoff {
		return relay.config.MaximumBackoff
	}
	return backoff
}
