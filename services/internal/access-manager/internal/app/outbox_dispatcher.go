package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
)

const (
	outboxPublisherKindDisabled           = "disabled"
	outboxPublisherKindDiagnosticLogLossy = "diagnostic-log-lossy"
	outboxPublisherKindPostgresEventLog   = "postgres-event-log"
)

var errOutboxPermanentPublish = errors.New("permanent outbox publish failure")

type outboxStore interface {
	ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error)
	MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error
	MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error
	MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error
}

type outboxPublisher interface {
	Publish(ctx context.Context, event entity.OutboxEvent) error
}

type outboxDispatcherConfig struct {
	BatchSize           int
	PollInterval        time.Duration
	LockTTL             time.Duration
	PublishTimeout      time.Duration
	RetryInitialDelay   time.Duration
	RetryMaxDelay       time.Duration
	FailureMessageLimit int
}

type outboxDispatcher struct {
	store     outboxStore
	publisher outboxPublisher
	cfg       outboxDispatcherConfig
	logger    *slog.Logger
}

func newOutboxDispatcher(
	store outboxStore,
	publisher outboxPublisher,
	cfg outboxDispatcherConfig,
	logger *slog.Logger,
) *outboxDispatcher {
	return &outboxDispatcher{store: store, publisher: publisher, cfg: cfg, logger: logger}
}

func (d *outboxDispatcher) Run(ctx context.Context) error {
	if err := d.dispatchOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		d.logger.Error("access-manager outbox dispatch failed", "error", err)
	}

	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := d.dispatchOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				d.logger.Error("access-manager outbox dispatch failed", "error", err)
			}
		}
	}
}

func (d *outboxDispatcher) dispatchOnce(ctx context.Context) error {
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

func (d *outboxDispatcher) dispatchEvent(ctx context.Context, event entity.OutboxEvent) error {
	publishCtx, cancel := context.WithTimeout(ctx, d.cfg.PublishTimeout)
	err := d.publisher.Publish(publishCtx, event)
	cancel()
	if err == nil {
		return d.store.MarkOutboxEventPublished(ctx, event.ID, event.AttemptCount, time.Now().UTC())
	}
	if errors.Is(err, context.Canceled) {
		return err
	}

	lastError := truncateOutboxFailure(err.Error(), d.cfg.FailureMessageLimit)
	if errors.Is(err, errOutboxPermanentPublish) {
		failedAt := time.Now().UTC()
		if markErr := d.store.MarkOutboxEventPermanentlyFailed(ctx, event.ID, event.AttemptCount, failedAt, lastError); markErr != nil {
			return fmt.Errorf("mark outbox event permanently failed: %w", markErr)
		}
		d.logger.Error(
			"access-manager outbox event publish failed permanently",
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
		"access-manager outbox event publish failed",
		"event_id", event.ID.String(),
		"event_type", event.EventType,
		"attempt_count", event.AttemptCount,
		"next_attempt_at", nextAttemptAt,
		"error", lastError,
	)
	return nil
}

func (d *outboxDispatcher) retryDelay(attemptCount int) time.Duration {
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

type loggingOutboxPublisher struct {
	logger *slog.Logger
}

func (p loggingOutboxPublisher) Publish(_ context.Context, event entity.OutboxEvent) error {
	p.logger.Info(
		"access-manager outbox event delivered to diagnostic log sink",
		"event_id", event.ID.String(),
		"event_type", event.EventType,
		"aggregate_type", event.AggregateType,
		"aggregate_id", event.AggregateID.String(),
	)
	return nil
}

func truncateOutboxFailure(text string, limit int) string {
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

func newOutboxPublisher(cfg Config, eventLog eventLogAppender, logger *slog.Logger) (outboxPublisher, error) {
	switch strings.TrimSpace(cfg.OutboxPublisherKind) {
	case outboxPublisherKindPostgresEventLog:
		return postgresEventLogPublisher{
			sourceService: strings.TrimSpace(cfg.OutboxEventLogSource),
			eventLog:      eventLog,
		}, nil
	case outboxPublisherKindDiagnosticLogLossy:
		if !cfg.OutboxAllowLossyPublisher {
			return nil, fmt.Errorf("lossy diagnostic outbox publisher is not explicitly allowed")
		}
		return loggingOutboxPublisher{logger: logger}, nil
	case outboxPublisherKindDisabled:
		return nil, fmt.Errorf("outbox publisher is disabled")
	default:
		return nil, fmt.Errorf("unsupported outbox publisher kind %q", cfg.OutboxPublisherKind)
	}
}
