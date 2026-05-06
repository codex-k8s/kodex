package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Dispatcher claims service outbox events and publishes them with retries.
type Dispatcher struct {
	store       Store
	publisher   Publisher
	cfg         Config
	logger      *slog.Logger
	serviceName string
}

// NewDispatcher creates a reusable outbox delivery worker.
func NewDispatcher(store Store, publisher Publisher, cfg Config, logger *slog.Logger, serviceName string) *Dispatcher {
	return &Dispatcher{store: store, publisher: publisher, cfg: cfg, logger: logger, serviceName: strings.TrimSpace(serviceName)}
}

// Run dispatches events until the context is cancelled.
func (d *Dispatcher) Run(ctx context.Context) error {
	if err := d.dispatchOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		d.logger.Error(d.logMessage("outbox dispatch failed"), "error", err)
	}

	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := d.dispatchOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				d.logger.Error(d.logMessage("outbox dispatch failed"), "error", err)
			}
		}
	}
}

func (d *Dispatcher) dispatchOnce(ctx context.Context) error {
	now := time.Now().UTC()
	events, err := d.store.ClaimOutboxEvents(ctx, d.cfg.BatchSize, now, now.Add(d.cfg.LockTTL))
	if err != nil {
		return fmt.Errorf("claim outbox events: %w", err)
	}
	for _, event := range events {
		if err := d.dispatchEvent(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (d *Dispatcher) dispatchEvent(ctx context.Context, event Event) error {
	publishCtx, cancel := context.WithTimeout(ctx, d.cfg.PublishTimeout)
	err := d.publisher.Publish(publishCtx, event)
	cancel()
	if err == nil {
		return d.store.MarkOutboxEventPublished(ctx, event.ID, event.AttemptCount, time.Now().UTC())
	}
	if errors.Is(err, context.Canceled) {
		return err
	}

	lastError := truncateFailure(err.Error(), d.cfg.FailureMessageLimit)
	if errors.Is(err, ErrPermanentPublish) {
		failedAt := time.Now().UTC()
		if markErr := d.store.MarkOutboxEventPermanentlyFailed(ctx, event.ID, event.AttemptCount, failedAt, lastError); markErr != nil {
			return fmt.Errorf("mark outbox event permanently failed: %w", markErr)
		}
		d.logger.Error(
			d.logMessage("outbox event publish failed permanently"),
			"event_id", event.ID.String(),
			"event_type", event.EventType,
			"attempt_count", event.AttemptCount,
			"failed_at", failedAt,
			"error", lastError,
		)
		return nil
	}

	nextAttemptAt := time.Now().UTC().Add(d.retryDelay(event.AttemptCount))
	if markErr := d.store.MarkOutboxEventFailed(ctx, event.ID, event.AttemptCount, nextAttemptAt, lastError); markErr != nil {
		return fmt.Errorf("mark outbox event failed: %w", markErr)
	}
	d.logger.Warn(
		d.logMessage("outbox event publish failed"),
		"event_id", event.ID.String(),
		"event_type", event.EventType,
		"attempt_count", event.AttemptCount,
		"next_attempt_at", nextAttemptAt,
		"error", lastError,
	)
	return nil
}

func (d *Dispatcher) retryDelay(attemptCount int) time.Duration {
	if attemptCount < 1 {
		attemptCount = 1
	}
	delay := d.cfg.RetryInitialDelay
	for i := 1; i < attemptCount && delay < d.cfg.RetryMaxDelay; i++ {
		if delay > d.cfg.RetryMaxDelay/2 {
			return d.cfg.RetryMaxDelay
		}
		delay *= 2
		if delay > d.cfg.RetryMaxDelay {
			return d.cfg.RetryMaxDelay
		}
	}
	return delay
}

func (d *Dispatcher) logMessage(text string) string {
	if d.serviceName == "" {
		return text
	}
	return d.serviceName + " " + text
}

func truncateFailure(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit < 1 || len(text) <= limit {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
