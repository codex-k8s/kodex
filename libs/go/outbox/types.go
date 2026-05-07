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

// NewEvent creates the shared dispatcher event shape from a service-local outbox row.
func NewEvent(
	id uuid.UUID,
	eventType string,
	schemaVersion int,
	aggregateType string,
	aggregateID uuid.UUID,
	payload []byte,
	occurredAt time.Time,
	attemptCount int,
) Event {
	return Event{
		ID:            id,
		EventType:     eventType,
		SchemaVersion: schemaVersion,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		Payload:       payload,
		OccurredAt:    occurredAt,
		AttemptCount:  attemptCount,
	}
}

// Record is the shared flat shape for service-local outbox rows.
type Record struct {
	Event
	PublishedAt         *time.Time
	NextAttemptAt       time.Time
	LockedUntil         *time.Time
	FailedPermanentlyAt *time.Time
	FailureKind         string
	LastError           string
}

// RecordDelivery stores retry and publication fields for a service-local outbox row.
type RecordDelivery struct {
	PublishedAt   *time.Time
	AttemptCount  int
	NextAttemptAt time.Time
	LockedUntil   *time.Time
}

// RecordFailure stores terminal failure diagnostics for a service-local outbox row.
type RecordFailure struct {
	FailedPermanentlyAt *time.Time
	FailureKind         string
	LastError           string
}

// EventFromRecord builds the dispatch event shape from a service-local outbox record.
func EventFromRecord(record Record) Event {
	return record.Event
}

// RecordFromParts builds the shared flat record from grouped outbox fields.
func RecordFromParts(event Event, delivery RecordDelivery, failure RecordFailure) Record {
	record := Record{Event: event}
	record.PublishedAt = delivery.PublishedAt
	record.AttemptCount = delivery.AttemptCount
	record.NextAttemptAt = delivery.NextAttemptAt
	record.LockedUntil = delivery.LockedUntil
	record.FailedPermanentlyAt = failure.FailedPermanentlyAt
	record.FailureKind = failure.FailureKind
	record.LastError = failure.LastError
	return record
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
