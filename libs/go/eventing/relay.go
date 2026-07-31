package eventing

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Publisher отправляет неизменяемый envelope и проверяет broker contract.
type Publisher interface {
	Publish(context.Context, Envelope) error
	Check(context.Context) error
	Close() error
}

// ClaimedEvent связывает событие с server-owned lease token.
type ClaimedEvent struct {
	Envelope   Envelope
	LeaseToken string
}

// OutboxStore — узкий порт durable PostgreSQL outbox для relay.
type OutboxStore interface {
	Check(context.Context) error
	Claim(context.Context, string, int, time.Duration) ([]ClaimedEvent, error)
	MarkPublished(context.Context, string, string) error
	MarkFailed(context.Context, string, string, bool, time.Duration) error
}

// RelayConfig задаёт bounded delivery lifecycle.
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

// Check проверяет PostgreSQL outbox и exact broker contract.
func (relay *Relay) Check(ctx context.Context) error {
	if err := relay.store.Check(ctx); err != nil {
		return fmt.Errorf("check outbox: %w", err)
	}
	if err := relay.publisher.Check(ctx); err != nil {
		return fmt.Errorf("check publisher: %w", err)
	}
	return nil
}

// Run выполняет bounded claim/publish/finalize до отмены lifecycle.
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
	for _, item := range claimed {
		publishCtx, cancelPublish := context.WithTimeout(lifecycle, relay.config.PublishTimeout)
		publishErr := relay.publisher.Publish(publishCtx, item.Envelope)
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
			)
		} else {
			err = relay.store.MarkFailed(
				finalizeCtx,
				item.Envelope.EventID,
				item.LeaseToken,
				true,
				relay.config.InitialBackoff,
			)
		}
		cancelFinalize()
		if err != nil {
			return fmt.Errorf("finalize outbox: %w", err)
		}
	}
	return nil
}
