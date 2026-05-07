package outbox

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	// PublisherKindDisabled disables outbox delivery and is only valid when the dispatcher is off.
	PublisherKindDisabled = "disabled"
	// PublisherKindDiagnosticLogLossy writes events to the process log and then marks them as published.
	PublisherKindDiagnosticLogLossy = "diagnostic-log-lossy"
	// PublisherKindPostgresEventLog publishes events into the platform-event-log PostgreSQL contour.
	PublisherKindPostgresEventLog = "postgres-event-log"
)

// ErrPermanentPublish marks a non-retryable publish failure.
var ErrPermanentPublish = errors.New("permanent outbox publish failure")

// Event is the transport-neutral event shape claimed from a service outbox table.
type Event struct {
	ID            uuid.UUID
	EventType     string
	SchemaVersion int
	AggregateType string
	AggregateID   uuid.UUID
	Payload       []byte
	OccurredAt    time.Time
	AttemptCount  int
}

// EntityStore is a service-local outbox store with a service-specific event entity type.
type EntityStore[T any] interface {
	ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]T, error)
	MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error
	MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error
	MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error
}

// Store is the persistence contract required by Dispatcher.
type Store = EntityStore[Event]

// Publisher sends one claimed outbox event to a concrete delivery target.
type Publisher interface {
	Publish(ctx context.Context, event Event) error
}

// Config controls outbox dispatch pacing, leases and retry behavior.
type Config struct {
	BatchSize           int
	PollInterval        time.Duration
	LockTTL             time.Duration
	PublishTimeout      time.Duration
	RetryInitialDelay   time.Duration
	RetryMaxDelay       time.Duration
	FailureMessageLimit int
}

// ConfigFromRuntimeValues converts service env fields to dispatcher config.
func ConfigFromRuntimeValues(
	batchSize int,
	pollInterval time.Duration,
	lockTTL time.Duration,
	publishTimeout time.Duration,
	retryInitialDelay time.Duration,
	retryMaxDelay time.Duration,
	failureMessageLimit int,
) Config {
	return Config{
		BatchSize:           batchSize,
		PollInterval:        pollInterval,
		LockTTL:             lockTTL,
		PublishTimeout:      publishTimeout,
		RetryInitialDelay:   retryInitialDelay,
		RetryMaxDelay:       retryMaxDelay,
		FailureMessageLimit: failureMessageLimit,
	}
}
