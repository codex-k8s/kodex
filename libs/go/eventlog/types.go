package eventlog

import (
	"time"

	"github.com/google/uuid"
)

// Event is the stable event payload written by service outbox publishers.
type Event struct {
	// ID is the producer event id and the deduplication key in the shared log.
	ID uuid.UUID
	// SourceService identifies the service that owns the original outbox.
	SourceService string
	// EventType is the AsyncAPI event name, for example access.user.created.
	EventType string
	// SchemaVersion is the payload contract version for this event type.
	SchemaVersion int
	// AggregateType identifies the producer aggregate kind without requiring a foreign key.
	AggregateType string
	// AggregateID identifies the producer aggregate instance.
	AggregateID uuid.UUID
	// Payload is the already-rendered JSON event payload.
	Payload []byte
	// OccurredAt is the producer-side domain event time.
	OccurredAt time.Time
}

// StoredEvent is an Event with the monotonic sequence assigned by the shared log.
type StoredEvent struct {
	// SequenceID is a monotonic cursor assigned by the shared PostgreSQL log.
	SequenceID int64
	Event
	// RecordedAt is the time when the event reached the shared log.
	RecordedAt time.Time
}

// AppendParams describes one append-only event-log write.
type AppendParams struct {
	// Event is the producer event to append.
	Event Event
	// RecordedAt is the append time chosen by the publisher.
	RecordedAt time.Time
}

// ClaimParams asks the log to lease the next contiguous batch for one consumer.
type ClaimParams struct {
	// ConsumerName is the stable logical subscriber name.
	ConsumerName string
	// LeaseOwner identifies one worker instance of the consumer.
	LeaseOwner string
	// Limit caps the leased batch size.
	Limit int
	// Now is supplied by the caller to keep lease math deterministic in tests.
	Now time.Time
	// LockedUntil is the end of the short consumer lease.
	LockedUntil time.Time
}

// ClaimedBatch contains events leased to one consumer worker.
type ClaimedBatch struct {
	// ConsumerName is the stable logical subscriber name.
	ConsumerName string
	// LeaseOwner identifies the worker that owns this batch.
	LeaseOwner string
	// LockedUntil is the lease expiry time.
	LockedUntil time.Time
	// Events is the ordered leased event batch.
	Events []StoredEvent
}

// CheckpointState is the persisted cursor and lease state for one consumer.
type CheckpointState struct {
	// ConsumerName is the stable logical subscriber name.
	ConsumerName string
	// LastSequenceID is the highest fully processed event sequence.
	LastSequenceID int64
	// LeaseOwner is set while a worker leases the consumer checkpoint.
	LeaseOwner string
	// LockedUntil is nil when no worker owns the checkpoint.
	LockedUntil *time.Time
	// UpdatedAt is the last checkpoint write time.
	UpdatedAt time.Time
}

// AdvanceParams moves the consumer checkpoint after successful idempotent processing.
type AdvanceParams struct {
	// ConsumerName is the stable logical subscriber name.
	ConsumerName string
	// LeaseOwner must match the worker that claimed the batch.
	LeaseOwner string
	// LastSequenceID is the highest successfully processed event sequence.
	LastSequenceID int64
	// Now is used to reject stale workers after lease expiry.
	Now time.Time
}

// ReleaseParams releases a consumer lease without advancing the checkpoint.
type ReleaseParams struct {
	// ConsumerName is the stable logical subscriber name.
	ConsumerName string
	// LeaseOwner must match the worker that claimed the batch.
	LeaseOwner string
	// Now is used to reject stale workers after lease expiry.
	Now time.Time
}
