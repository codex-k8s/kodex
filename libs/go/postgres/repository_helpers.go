package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	postgresUniqueViolation     = "23505"
	postgresForeignKeyViolation = "23503"
	postgresCheckViolation      = "23514"
	postgresSerialization       = "40001"
	postgresDeadlock            = "40P01"
)

// ErrorSentinels maps PostgreSQL error classes to service-level domain errors.
type ErrorSentinels struct {
	AlreadyExists      error
	Conflict           error
	InvalidArgument    error
	NotFound           error
	PreconditionFailed error
}

// CRUDSentinels builds the common create/read/update/delete repository error mapping.
func CRUDSentinels(alreadyExists error, conflict error, invalidArgument error, notFound error, preconditionFailed error) ErrorSentinels {
	return ErrorSentinels{
		AlreadyExists:      alreadyExists,
		Conflict:           conflict,
		InvalidArgument:    invalidArgument,
		NotFound:           notFound,
		PreconditionFailed: preconditionFailed,
	}
}

// RowScanner is the minimal pgx row/rows scanning contract used by repository code.
type RowScanner interface {
	Scan(dest ...any) error
}

// ExecQuerier is the minimal write contract shared by pgx pools and transactions.
type ExecQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// RowQuerier is the minimal read contract shared by pgx pools and transactions.
type RowQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// TxBeginner is the minimal transaction opener shared by pgx pools and test doubles.
type TxBeginner interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

// PoolRuntimeSettings is a semantic alias for service-neutral pgxpool settings.
type PoolRuntimeSettings = PoolSettings

// PoolSettingsFromRuntime converts service env config to the shared pgxpool contract.
func PoolSettingsFromRuntime(settings PoolRuntimeSettings) PoolSettings {
	return settings
}

// PoolRuntimeSettingsFromValues builds pool settings from service-owned env fields.
func PoolRuntimeSettingsFromValues(
	dsn string,
	maxConns int32,
	minConns int32,
	maxConnLifetime time.Duration,
	maxConnIdleTime time.Duration,
	healthCheckPeriod time.Duration,
	pingTimeout time.Duration,
	connectRetryMaxAttempts int,
	connectRetryInitialDelay time.Duration,
	connectRetryMaxDelay time.Duration,
	connectRetryJitterRatio float64,
) PoolRuntimeSettings {
	return PoolRuntimeSettings{
		DSN:                      dsn,
		MaxConns:                 maxConns,
		MinConns:                 minConns,
		MaxConnLifetime:          maxConnLifetime,
		MaxConnIdleTime:          maxConnIdleTime,
		HealthCheckPeriod:        healthCheckPeriod,
		PingTimeout:              pingTimeout,
		ConnectRetryMaxAttempts:  connectRetryMaxAttempts,
		ConnectRetryInitialDelay: connectRetryInitialDelay,
		ConnectRetryMaxDelay:     connectRetryMaxDelay,
		ConnectRetryJitterRatio:  connectRetryJitterRatio,
	}
}

// Mutation describes one SQL write inside repository transactions.
type Mutation struct {
	Query           string
	Args            pgx.NamedArgs
	RequireAffected bool
}

// RunMutation executes one write operation and enforces optimistic affected-row checks.
func RunMutation(ctx context.Context, db ExecQuerier, conflict error, mutation Mutation) error {
	tag, err := db.Exec(ctx, mutation.Query, mutation.Args)
	if err != nil {
		return err
	}
	if mutation.RequireAffected && tag.RowsAffected() == 0 {
		return conflict
	}
	return nil
}

// RunDistinctMutations executes a fixed set of different write operations.
//
// Do not use it for per-item collection writes. Repeated query text is rejected
// so callers cannot hide N+1 write loops behind a shared helper.
func RunDistinctMutations(ctx context.Context, db ExecQuerier, conflict error, mutations ...Mutation) error {
	seenQueries := make(map[string]struct{}, len(mutations))
	for _, mutation := range mutations {
		if _, exists := seenQueries[mutation.Query]; exists {
			return errors.New("postgres: duplicate mutation query in fixed mutation set")
		}
		seenQueries[mutation.Query] = struct{}{}
	}
	for _, mutation := range mutations {
		if err := RunMutation(ctx, db, conflict, mutation); err != nil {
			return err
		}
	}
	return nil
}

// WrapError converts pgx/PostgreSQL errors into service-level domain errors while preserving causes.
func WrapError(operation string, err error, sentinels ErrorSentinels) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", operation, sentinels.NotFound)
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case postgresUniqueViolation:
			return fmt.Errorf("%s: %w", operation, errors.Join(sentinels.AlreadyExists, err))
		case postgresForeignKeyViolation:
			return fmt.Errorf("%s: %w", operation, errors.Join(sentinels.PreconditionFailed, err))
		case postgresCheckViolation:
			return fmt.Errorf("%s: %w", operation, errors.Join(sentinels.InvalidArgument, err))
		case postgresSerialization, postgresDeadlock:
			return fmt.Errorf("%s: %w", operation, errors.Join(sentinels.Conflict, err))
		}
	}

	return fmt.Errorf("%s: %w", operation, err)
}

// ScanRows scans all pgx rows with a caller-supplied row caster and closes rows through pgx.CollectRows.
func ScanRows[T any](rows pgx.Rows, scan func(RowScanner) (T, error)) ([]T, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (T, error) {
		return scan(row)
	})
}

// QueryRows runs a read query and scans all rows with the supplied caster.
func QueryRows[T any](ctx context.Context, db RowQuerier, sqlText string, args pgx.NamedArgs, scan func(RowScanner) (T, error)) ([]T, error) {
	rows, err := db.Query(ctx, sqlText, args)
	if err != nil {
		return nil, err
	}
	return ScanRows(rows, scan)
}

// WithTx executes fn in a PostgreSQL transaction and rolls it back unless commit succeeds.
func WithTx(ctx context.Context, db TxBeginner, fn func(tx pgx.Tx) error) error {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

// NullableUUID converts an optional domain UUID to a pgx argument.
func NullableUUID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return *id
}

// NullableTime converts an optional domain timestamp to a pgx argument.
func NullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

// NullableCommandID stores zero UUID command ids as SQL NULL.
func NullableCommandID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

// IdempotencyLookupKey suppresses idempotency-key lookup when command id is present.
func IdempotencyLookupKey(commandID uuid.UUID, idempotencyKey string) string {
	if commandID != uuid.Nil {
		return ""
	}
	return idempotencyKey
}

// OutboxDeliveryFailureArgs builds validated arguments for retry/final failure updates.
func OutboxDeliveryFailureArgs(id uuid.UUID, attemptCount int, timestampName string, timestampValue time.Time, lastError string) (pgx.NamedArgs, bool) {
	if id == uuid.Nil || attemptCount < 1 || timestampValue.IsZero() {
		return nil, false
	}
	args := pgx.NamedArgs{
		"id":            id,
		"attempt_count": attemptCount,
		"last_error":    lastError,
	}
	args[timestampName] = timestampValue
	return args, true
}

// ExecOutboxDeliveryFailure validates and applies a retry/final failure update.
func ExecOutboxDeliveryFailure(ctx context.Context, db ExecQuerier, queryText string, id uuid.UUID, attemptCount int, timestampName string, timestampValue time.Time, lastError string) (bool, error) {
	args, ok := OutboxDeliveryFailureArgs(id, attemptCount, timestampName, timestampValue, lastError)
	if !ok {
		return false, nil
	}
	_, err := db.Exec(ctx, queryText, args)
	return true, err
}

// OutboxClaimArgs builds validated arguments for leasing unpublished outbox events.
func OutboxClaimArgs(limit int, now time.Time, lockedUntil time.Time) (pgx.NamedArgs, bool) {
	if limit < 1 || !lockedUntil.After(now) {
		return nil, false
	}
	return pgx.NamedArgs{
		"limit":        limit,
		"now":          now,
		"locked_until": lockedUntil,
	}, true
}

// ClaimOutboxRows validates claim bounds, executes the claim query and scans rows.
func ClaimOutboxRows[T any](ctx context.Context, db RowQuerier, queryText string, limit int, now time.Time, lockedUntil time.Time, scan func(RowScanner) (T, error)) ([]T, bool, error) {
	args, ok := OutboxClaimArgs(limit, now, lockedUntil)
	if !ok {
		return nil, false, nil
	}
	rows, err := db.Query(ctx, queryText, args)
	if err != nil {
		return nil, true, err
	}
	items, err := ScanRows(rows, scan)
	return items, true, err
}

// OutboxPublishedArgs builds validated arguments for marking an event as published.
func OutboxPublishedArgs(id uuid.UUID, attemptCount int, publishedAt time.Time) (pgx.NamedArgs, bool) {
	if id == uuid.Nil || attemptCount < 1 || publishedAt.IsZero() {
		return nil, false
	}
	return pgx.NamedArgs{
		"id":            id,
		"attempt_count": attemptCount,
		"published_at":  publishedAt,
	}, true
}

// OutboxCreateArgs builds common named arguments for service-local outbox inserts.
func OutboxCreateArgs(
	id uuid.UUID,
	eventType string,
	schemaVersion int,
	aggregateType string,
	aggregateID uuid.UUID,
	payload []byte,
	occurredAt time.Time,
	publishedAt *time.Time,
) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":             id,
		"event_type":     eventType,
		"schema_version": schemaVersion,
		"aggregate_type": aggregateType,
		"aggregate_id":   aggregateID,
		"payload":        JSONPayload(payload),
		"occurred_at":    occurredAt,
		"published_at":   NullableTime(publishedAt),
	}
}

// ExecOutboxPublished validates and applies a successful outbox publication update.
func ExecOutboxPublished(ctx context.Context, db ExecQuerier, queryText string, id uuid.UUID, attemptCount int, publishedAt time.Time) (bool, error) {
	args, ok := OutboxPublishedArgs(id, attemptCount, publishedAt)
	if !ok {
		return false, nil
	}
	_, err := db.Exec(ctx, queryText, args)
	return true, err
}

// ApplyOutboxPublished validates and persists successful publication.
func ApplyOutboxPublished(ctx context.Context, db ExecQuerier, queryText string, invalid error, id uuid.UUID, attemptCount int, publishedAt time.Time) error {
	ok, err := ExecOutboxPublished(ctx, db, queryText, id, attemptCount, publishedAt)
	if !ok {
		return invalid
	}
	return err
}

// ApplyOutboxDeliveryFailure validates and persists a retry/final failure marker.
func ApplyOutboxDeliveryFailure(ctx context.Context, db ExecQuerier, queryText string, invalid error, id uuid.UUID, attemptCount int, timestampName string, timestampValue time.Time, lastError string) error {
	ok, err := ExecOutboxDeliveryFailure(ctx, db, queryText, id, attemptCount, timestampName, timestampValue, lastError)
	if !ok {
		return invalid
	}
	return err
}

// UUIDPtrFromPG converts a nullable pgtype UUID to a domain pointer.
func UUIDPtrFromPG(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	id := uuid.UUID(value.Bytes)
	return &id
}

// TimePtrFromPG converts a nullable pgtype timestamp to a domain pointer.
func TimePtrFromPG(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

// StringValues converts typed string enums to plain text slices for SQL ANY filters.
func StringValues[T ~string](values []T) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

// UUIDValues returns a non-nil UUID slice for SQL ANY filters.
func UUIDValues(values []uuid.UUID) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(values))
	result = append(result, values...)
	return result
}

// JSONPayload returns an empty JSON object for absent repository payloads.
func JSONPayload(payload []byte) string {
	if len(payload) == 0 {
		return "{}"
	}
	return string(payload)
}

// AddBaseArgs appends common aggregate metadata to a named-argument set.
func AddBaseArgs(args pgx.NamedArgs, id uuid.UUID, version int64, createdAt time.Time, updatedAt time.Time) pgx.NamedArgs {
	args["id"] = id
	args["version"] = version
	args["created_at"] = createdAt
	args["updated_at"] = updatedAt
	return args
}

// OffsetPageBounds converts an API page request into LIMIT/OFFSET values.
func OffsetPageBounds(pageSize int32, pageToken string, defaultPageSize int32, maxPageSize int32) (limit int32, offset int32, nextOffset int32) {
	limit = pageSize
	if limit <= 0 {
		limit = defaultPageSize
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}
	parsedOffset, err := strconv.ParseInt(pageToken, 10, 32)
	if err == nil && parsedOffset > 0 {
		offset = int32(parsedOffset)
	}
	return limit, offset, offset + limit
}

// AddOffsetPageArgs appends LIMIT/OFFSET named args and returns the computed page bounds.
func AddOffsetPageArgs(args pgx.NamedArgs, pageSize int32, pageToken string, defaultPageSize int32, maxPageSize int32) (limit int32, offset int32, nextOffset int32) {
	limit, offset, nextOffset = OffsetPageBounds(pageSize, pageToken, defaultPageSize, maxPageSize)
	args["limit"] = limit + 1
	args["offset"] = offset
	return limit, offset, nextOffset
}

// TrimOffsetPage keeps one extra queried row only as a continuation signal.
func TrimOffsetPage[T any](items []T, limit int32, nextOffset int32) ([]T, string) {
	if int32(len(items)) <= limit {
		return items, ""
	}
	return items[:len(items)-1], strconv.FormatInt(int64(nextOffset), 10)
}

// CommandResultRow stores common idempotency result columns shared by service repositories.
type CommandResultRow struct {
	Key            string
	CommandID      *uuid.UUID
	IdempotencyKey string
	ActorType      string
	ActorID        string
	Operation      string
	AggregateType  string
	AggregateID    uuid.UUID
	ResultPayload  []byte
	CreatedAt      time.Time
}

// ScanCommandResultRow scans the common command-results row shape used by idempotent writes.
func ScanCommandResultRow(row RowScanner) (CommandResultRow, error) {
	var result CommandResultRow
	var commandID pgtype.UUID
	err := row.Scan(
		&result.Key,
		&commandID,
		&result.IdempotencyKey,
		&result.ActorType,
		&result.ActorID,
		&result.Operation,
		&result.AggregateType,
		&result.AggregateID,
		&result.ResultPayload,
		&result.CreatedAt,
	)
	result.CommandID = UUIDPtrFromPG(commandID)
	return result, err
}

// OutboxEventRow stores transport-neutral outbox columns scanned from service databases.
type OutboxEventRow struct {
	Identity OutboxEventIdentity
	Delivery OutboxEventDelivery
	Failure  OutboxEventFailure
	Body     []byte
}

// OutboxEventIdentity stores immutable event identity fields.
type OutboxEventIdentity struct {
	RowID           uuid.UUID
	TypeName        string
	ContractVersion int
	SubjectKind     string
	SubjectID       uuid.UUID
	CreatedAt       time.Time
}

// OutboxEventDelivery stores retry and publication state.
type OutboxEventDelivery struct {
	SentAt     *time.Time
	Attempts   int
	RetryAt    time.Time
	LeaseUntil *time.Time
}

// OutboxEventFailure stores final failure diagnostics.
type OutboxEventFailure struct {
	DeadAt      *time.Time
	FailureCode string
	ErrorText   string
}

// ScanOutboxEventRow scans the common outbox row shape shared by service databases.
func ScanOutboxEventRow(row RowScanner) (OutboxEventRow, error) {
	var event OutboxEventRow
	var payload []byte
	var publishedAt, lockedUntil, failedPermanentlyAt pgtype.Timestamptz
	err := row.Scan(
		&event.Identity.RowID,
		&event.Identity.TypeName,
		&event.Identity.ContractVersion,
		&event.Identity.SubjectKind,
		&event.Identity.SubjectID,
		&payload,
		&event.Identity.CreatedAt,
		&publishedAt,
		&event.Delivery.Attempts,
		&event.Delivery.RetryAt,
		&lockedUntil,
		&failedPermanentlyAt,
		&event.Failure.FailureCode,
		&event.Failure.ErrorText,
	)
	event.Body = append(event.Body[:0], payload...)
	event.Delivery.SentAt = TimePtrFromPG(publishedAt)
	event.Delivery.LeaseUntil = TimePtrFromPG(lockedUntil)
	event.Failure.DeadAt = TimePtrFromPG(failedPermanentlyAt)
	return event, err
}
