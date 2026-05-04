package eventlog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type database interface {
	execer
	queryer
}

// Store appends platform events and leases them to independent consumers.
type Store struct {
	db database
}

// NewStore creates a PostgreSQL-backed event log store.
func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// Append writes an event once. Repeating the same event id is treated as already delivered.
func (s *Store) Append(ctx context.Context, params AppendParams) error {
	if err := validateAppend(params); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx, queryEventLogAppend, appendArgs(params))
	return err
}

// Claim leases the next event batch for a consumer. Different consumers see the same event stream independently.
func (s *Store) Claim(ctx context.Context, params ClaimParams) (ClaimedBatch, error) {
	if err := validateClaim(params); err != nil {
		return ClaimedBatch{}, err
	}
	if err := s.ensureCheckpoint(ctx, params.ConsumerName, params.Now); err != nil {
		return ClaimedBatch{}, err
	}
	rows, err := s.db.Query(ctx, queryEventLogClaim, claimArgs(params))
	if err != nil {
		return ClaimedBatch{}, err
	}
	events, err := scanStoredEvents(rows)
	if err != nil {
		return ClaimedBatch{}, err
	}
	return ClaimedBatch{
		ConsumerName: params.ConsumerName,
		LeaseOwner:   params.LeaseOwner,
		LockedUntil:  params.LockedUntil,
		Events:       events,
	}, nil
}

// Advance moves the consumer checkpoint after the leased events are processed.
func (s *Store) Advance(ctx context.Context, params AdvanceParams) error {
	if err := validateAdvance(params); err != nil {
		return err
	}
	return s.execOwnedCheckpoint(ctx, queryEventLogAdvanceCheckpoint, advanceArgs(params))
}

// Release clears a consumer lease without advancing the checkpoint.
func (s *Store) Release(ctx context.Context, params ReleaseParams) error {
	if err := validateRelease(params); err != nil {
		return err
	}
	return s.execOwnedCheckpoint(ctx, queryEventLogReleaseCheckpoint, releaseArgs(params))
}

func (s *Store) execOwnedCheckpoint(ctx context.Context, query string, args pgx.NamedArgs) error {
	tag, err := s.db.Exec(ctx, query, args)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCheckpointNotOwned
	}
	return nil
}

// GetStoredEvent returns a stored event by event id.
func (s *Store) GetStoredEvent(ctx context.Context, id uuid.UUID) (StoredEvent, error) {
	if id == uuid.Nil {
		return StoredEvent{}, fmt.Errorf("%w: event id is required", ErrInvalidEvent)
	}
	return scanStoredEvent(s.db.QueryRow(ctx, queryEventLogGetStoredEventByID, pgx.NamedArgs{"event_id": id}))
}

// GetCheckpointState returns a consumer checkpoint by name.
func (s *Store) GetCheckpointState(ctx context.Context, consumerName string) (CheckpointState, error) {
	consumerName = strings.TrimSpace(consumerName)
	if consumerName == "" {
		return CheckpointState{}, fmt.Errorf("%w: consumer name is required", ErrInvalidClaim)
	}
	return scanCheckpointState(s.db.QueryRow(ctx, queryEventLogGetCheckpointState, pgx.NamedArgs{"consumer_name": consumerName}))
}

func (s *Store) ensureCheckpoint(ctx context.Context, consumerName string, updatedAt time.Time) error {
	_, err := s.db.Exec(ctx, queryEventLogEnsureCheckpoint, pgx.NamedArgs{
		"consumer_name": consumerName,
		"updated_at":    updatedAt,
	})
	return err
}

func validateAppend(params AppendParams) error {
	event := params.Event
	switch {
	case event.ID == uuid.Nil:
		return fmt.Errorf("%w: event id is required", ErrInvalidEvent)
	case strings.TrimSpace(event.SourceService) == "":
		return fmt.Errorf("%w: source service is required", ErrInvalidEvent)
	case strings.TrimSpace(event.EventType) == "":
		return fmt.Errorf("%w: event type is required", ErrInvalidEvent)
	case event.SchemaVersion < 1:
		return fmt.Errorf("%w: schema version must be positive", ErrInvalidEvent)
	case strings.TrimSpace(event.AggregateType) == "":
		return fmt.Errorf("%w: aggregate type is required", ErrInvalidEvent)
	case event.AggregateID == uuid.Nil:
		return fmt.Errorf("%w: aggregate id is required", ErrInvalidEvent)
	case event.OccurredAt.IsZero():
		return fmt.Errorf("%w: occurred_at is required", ErrInvalidEvent)
	case params.RecordedAt.IsZero():
		return fmt.Errorf("%w: recorded_at is required", ErrInvalidEvent)
	}
	payload := bytes.TrimSpace(event.Payload)
	if len(payload) == 0 || !json.Valid(payload) {
		return fmt.Errorf("%w: payload must be valid json", ErrInvalidEvent)
	}
	return nil
}

func validateClaim(params ClaimParams) error {
	switch {
	case strings.TrimSpace(params.ConsumerName) == "":
		return fmt.Errorf("%w: consumer name is required", ErrInvalidClaim)
	case strings.TrimSpace(params.LeaseOwner) == "":
		return fmt.Errorf("%w: lease owner is required", ErrInvalidClaim)
	case params.Limit < 1:
		return fmt.Errorf("%w: limit must be positive", ErrInvalidClaim)
	case params.Now.IsZero():
		return fmt.Errorf("%w: now is required", ErrInvalidClaim)
	case !params.LockedUntil.After(params.Now):
		return fmt.Errorf("%w: locked_until must be after now", ErrInvalidClaim)
	default:
		return nil
	}
}

func validateAdvance(params AdvanceParams) error {
	switch {
	case strings.TrimSpace(params.ConsumerName) == "":
		return fmt.Errorf("%w: consumer name is required", ErrInvalidClaim)
	case strings.TrimSpace(params.LeaseOwner) == "":
		return fmt.Errorf("%w: lease owner is required", ErrInvalidClaim)
	case params.LastSequenceID < 1:
		return fmt.Errorf("%w: last sequence id must be positive", ErrInvalidClaim)
	case params.Now.IsZero():
		return fmt.Errorf("%w: now is required", ErrInvalidClaim)
	default:
		return nil
	}
}

func validateRelease(params ReleaseParams) error {
	switch {
	case strings.TrimSpace(params.ConsumerName) == "":
		return fmt.Errorf("%w: consumer name is required", ErrInvalidClaim)
	case strings.TrimSpace(params.LeaseOwner) == "":
		return fmt.Errorf("%w: lease owner is required", ErrInvalidClaim)
	case params.Now.IsZero():
		return fmt.Errorf("%w: now is required", ErrInvalidClaim)
	default:
		return nil
	}
}

func appendArgs(params AppendParams) pgx.NamedArgs {
	event := params.Event
	return pgx.NamedArgs{
		"event_id":       event.ID,
		"source_service": strings.TrimSpace(event.SourceService),
		"event_type":     strings.TrimSpace(event.EventType),
		"schema_version": event.SchemaVersion,
		"aggregate_type": strings.TrimSpace(event.AggregateType),
		"aggregate_id":   event.AggregateID,
		"payload":        string(bytes.TrimSpace(event.Payload)),
		"occurred_at":    event.OccurredAt,
		"recorded_at":    params.RecordedAt,
	}
}

func claimArgs(params ClaimParams) pgx.NamedArgs {
	return pgx.NamedArgs{
		"consumer_name": strings.TrimSpace(params.ConsumerName),
		"lease_owner":   strings.TrimSpace(params.LeaseOwner),
		"limit":         params.Limit,
		"now":           params.Now,
		"locked_until":  params.LockedUntil,
	}
}

func advanceArgs(params AdvanceParams) pgx.NamedArgs {
	args := ownedCheckpointArgs(params.ConsumerName, params.LeaseOwner, params.Now)
	args["last_sequence_id"] = params.LastSequenceID
	return args
}

func releaseArgs(params ReleaseParams) pgx.NamedArgs {
	return ownedCheckpointArgs(params.ConsumerName, params.LeaseOwner, params.Now)
}

func ownedCheckpointArgs(consumerName string, leaseOwner string, now time.Time) pgx.NamedArgs {
	return pgx.NamedArgs{
		"consumer_name": strings.TrimSpace(consumerName),
		"lease_owner":   strings.TrimSpace(leaseOwner),
		"now":           now,
		"updated_at":    now,
	}
}

func scanStoredEvents(rows pgx.Rows) ([]StoredEvent, error) {
	defer rows.Close()
	events := make([]StoredEvent, 0)
	for rows.Next() {
		event, err := scanStoredEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanStoredEvent(row rowScanner) (StoredEvent, error) {
	var event StoredEvent
	var payload []byte
	err := row.Scan(
		&event.SequenceID,
		&event.ID,
		&event.SourceService,
		&event.EventType,
		&event.SchemaVersion,
		&event.AggregateType,
		&event.AggregateID,
		&payload,
		&event.OccurredAt,
		&event.RecordedAt,
	)
	event.Payload = append(event.Payload[:0], payload...)
	return event, err
}

func scanCheckpointState(row rowScanner) (CheckpointState, error) {
	var checkpoint CheckpointState
	var lockedUntil pgtype.Timestamptz
	err := row.Scan(
		&checkpoint.ConsumerName,
		&checkpoint.LastSequenceID,
		&checkpoint.LeaseOwner,
		&lockedUntil,
		&checkpoint.UpdatedAt,
	)
	checkpoint.LockedUntil = timePtrFromPG(lockedUntil)
	return checkpoint, err
}

func timePtrFromPG(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}
