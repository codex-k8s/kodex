package provider

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
)

// SQLFiles contains named SQL queries for the provider-hub PostgreSQL repository.
//
//go:embed sql/*.sql
var SQLFiles embed.FS

var _ providerrepo.Repository = (*Repository)(nil)

type database interface {
	execer
	queryer
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Repository stores provider-hub state in PostgreSQL.
type Repository struct {
	db database
}

// NewRepository creates a PostgreSQL repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	if pool == nil {
		panic("provider-hub postgres pool is required")
	}
	return &Repository{db: pool}
}

// Ping verifies connectivity to provider-hub storage.
func (r *Repository) Ping(ctx context.Context) error {
	pool, ok := r.db.(*pgxpool.Pool)
	if !ok {
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping provider-hub postgres: %w", err)
	}
	return nil
}

// StoreWebhookEvent stores a raw webhook, projections, normalized provider events and outbox events atomically.
func (r *Repository) StoreWebhookEvent(ctx context.Context, webhook entity.WebhookEvent, projectionUpdate providerrepo.ProjectionUpdate, providerEvents []entity.ProviderEvent, outboxEvents []entity.OutboxEvent) (entity.WebhookEvent, []entity.ProviderEvent, error) {
	var stored entity.WebhookEvent
	var storedProviderEvents []entity.ProviderEvent
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		var insertErr error
		stored, insertErr = queryOne(ctx, tx, operationStoreWebhookEvent, queryWebhookEventInsert, webhookEventArgs(webhook), scanWebhookEvent)
		if errors.Is(insertErr, errs.ErrNotFound) {
			replayed, replayErr := queryOne(ctx, tx, operationStoreWebhookEvent, queryWebhookEventGetByDelivery, webhookEventIdentityArgs(webhook), scanWebhookEvent)
			if replayErr != nil {
				return replayErr
			}
			if !sameWebhookEvent(webhook, replayed) {
				return errs.ErrConflict
			}
			stored = replayed
			events, _, eventErr := queryPage(ctx, tx, operationListProviderEvents, queryProviderEventList, providerEventFilterArgs(query.ProviderEventFilter{
				SourceWebhookEventID: &stored.ID,
			}), scanProviderEvent)
			storedProviderEvents = events
			return eventErr
		}
		if insertErr != nil {
			return insertErr
		}
		insertedProviderEvents, err := insertProviderEvents(ctx, tx, operationStoreWebhookEvent, providerEvents)
		if err != nil {
			return err
		}
		if err := applyProjectionUpdate(ctx, tx, operationStoreWebhookEvent, projectionUpdate); err != nil {
			return err
		}
		if err := insertOutboxEvents(ctx, tx, operationStoreWebhookEvent, outboxEvents); err != nil {
			return err
		}
		storedProviderEvents = insertedProviderEvents
		return nil
	})
	if err != nil {
		return entity.WebhookEvent{}, nil, wrapError(operationStoreWebhookEvent, err)
	}
	return stored, storedProviderEvents, nil
}

// ProcessWebhookEvent updates processing state and stores projection changes atomically.
func (r *Repository) ProcessWebhookEvent(ctx context.Context, webhook entity.WebhookEvent, projectionUpdate providerrepo.ProjectionUpdate, providerEvents []entity.ProviderEvent, outboxEvents []entity.OutboxEvent) (entity.WebhookEvent, error) {
	var stored entity.WebhookEvent
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		var updateErr error
		stored, updateErr = queryOne(ctx, tx, operationProcessWebhookEvent, queryWebhookEventUpdateProcessing, webhookEventProcessingArgs(webhook), scanWebhookEvent)
		if updateErr != nil {
			return updateErr
		}
		if _, err := insertProviderEvents(ctx, tx, operationProcessWebhookEvent, providerEvents); err != nil {
			return err
		}
		if err := applyProjectionUpdate(ctx, tx, operationProcessWebhookEvent, projectionUpdate); err != nil {
			return err
		}
		return insertOutboxEvents(ctx, tx, operationProcessWebhookEvent, outboxEvents)
	})
	if err != nil {
		return entity.WebhookEvent{}, wrapError(operationProcessWebhookEvent, err)
	}
	return stored, nil
}

// GetWorkItemProjection returns one Issue or PR/MR projection.
func (r *Repository) GetWorkItemProjection(ctx context.Context, lookup query.ProviderTargetLookup) (entity.ProviderWorkItemProjection, error) {
	return queryOne(ctx, r.db, operationGetWorkItemProjection, queryWorkItemProjectionGet, workItemProjectionLookupArgs(lookup), scanWorkItemProjection)
}

// ListWorkItemProjections returns Issue and PR/MR projections.
func (r *Repository) ListWorkItemProjections(ctx context.Context, filter query.WorkItemProjectionFilter) ([]entity.ProviderWorkItemProjection, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListWorkItemProjections, queryWorkItemProjectionList, workItemProjectionFilterArgs(filter), scanWorkItemProjection)
}

// ListComments returns comment projections for one work item.
func (r *Repository) ListComments(ctx context.Context, filter query.CommentProjectionFilter) ([]entity.ProviderCommentProjection, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListComments, queryCommentProjectionList, commentProjectionFilterArgs(filter), scanCommentProjection)
}

// ListRelationships returns normalized relationships.
func (r *Repository) ListRelationships(ctx context.Context, filter query.RelationshipFilter) ([]entity.ProviderRelationship, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListRelationships, queryRelationshipList, relationshipFilterArgs(filter), scanRelationship)
}

// GetWebhookEvent returns a stored raw webhook by id.
func (r *Repository) GetWebhookEvent(ctx context.Context, id uuid.UUID) (entity.WebhookEvent, error) {
	return queryOne(ctx, r.db, operationGetWebhookEvent, queryWebhookEventGet, pgx.NamedArgs{"id": id}, scanWebhookEvent)
}

// ListWebhookEvents returns raw webhook events.
func (r *Repository) ListWebhookEvents(ctx context.Context, filter query.WebhookEventFilter) ([]entity.WebhookEvent, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListWebhookEvents, queryWebhookEventList, webhookEventFilterArgs(filter), scanWebhookEvent)
}

// ListProviderEvents returns normalized provider events.
func (r *Repository) ListProviderEvents(ctx context.Context, filter query.ProviderEventFilter) ([]entity.ProviderEvent, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListProviderEvents, queryProviderEventList, providerEventFilterArgs(filter), scanProviderEvent)
}

// UpsertAccountRuntimeState creates or updates provider-side account state.
func (r *Repository) UpsertAccountRuntimeState(ctx context.Context, state entity.ProviderAccountRuntimeState) (entity.ProviderAccountRuntimeState, error) {
	return queryOne(ctx, r.db, operationUpsertAccountRuntimeState, queryAccountRuntimeStateUpsert, accountRuntimeStateArgs(state), scanAccountRuntimeState)
}

// GetAccountRuntimeState returns provider-side account state.
func (r *Repository) GetAccountRuntimeState(ctx context.Context, lookup query.AccountRuntimeStateLookup) (entity.ProviderAccountRuntimeState, error) {
	return queryOne(ctx, r.db, operationGetAccountRuntimeState, queryAccountRuntimeStateGet, accountRuntimeStateLookupArgs(lookup), scanAccountRuntimeState)
}

// ListAccountRuntimeStates returns provider-side account states.
func (r *Repository) ListAccountRuntimeStates(ctx context.Context, filter query.AccountRuntimeStateFilter) ([]entity.ProviderAccountRuntimeState, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListAccountRuntimeStates, queryAccountRuntimeStateList, accountRuntimeStateFilterArgs(filter), scanAccountRuntimeState)
}

// RecordLimitSnapshot stores a provider limit snapshot and updates account runtime state atomically.
func (r *Repository) RecordLimitSnapshot(ctx context.Context, snapshot entity.ProviderLimitSnapshot, state entity.ProviderAccountRuntimeState) (entity.ProviderLimitSnapshot, error) {
	var stored entity.ProviderLimitSnapshot
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		var recordErr error
		stored, recordErr = queryOne(ctx, tx, operationRecordLimitSnapshot, queryLimitSnapshotUpsert, limitSnapshotArgs(snapshot), scanLimitSnapshot)
		if errors.Is(recordErr, errs.ErrNotFound) {
			stored, recordErr = queryOne(ctx, tx, operationRecordLimitSnapshot, queryLimitSnapshotGetReplay, limitSnapshotArgs(snapshot), scanLimitSnapshot)
			if errors.Is(recordErr, errs.ErrNotFound) {
				return errs.ErrConflict
			}
			return recordErr
		}
		if recordErr != nil {
			return recordErr
		}
		_, err := queryOne(ctx, tx, operationUpsertAccountRuntimeState, queryAccountRuntimeStateUpsertFromSnapshot, accountRuntimeStateArgs(state), scanAccountRuntimeState)
		return err
	})
	if err != nil {
		return entity.ProviderLimitSnapshot{}, wrapError(operationRecordLimitSnapshot, err)
	}
	return stored, nil
}

// ListLimitSnapshots returns provider limit snapshots.
func (r *Repository) ListLimitSnapshots(ctx context.Context, filter query.LimitSnapshotFilter) ([]entity.ProviderLimitSnapshot, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListLimitSnapshots, queryLimitSnapshotList, limitSnapshotFilterArgs(filter), scanLimitSnapshot)
}

// RecordProviderOperation stores a provider operation audit record.
func (r *Repository) RecordProviderOperation(ctx context.Context, operation entity.ProviderOperation) (entity.ProviderOperation, error) {
	stored, err := queryOne(ctx, r.db, operationRecordProviderOperation, queryProviderOperationInsert, providerOperationArgs(operation), scanProviderOperation)
	if errors.Is(err, errs.ErrNotFound) {
		stored, err = queryOne(ctx, r.db, operationRecordProviderOperation, queryProviderOperationGetReplay, providerOperationArgs(operation), scanProviderOperation)
		if errors.Is(err, errs.ErrNotFound) {
			return entity.ProviderOperation{}, wrapError(operationRecordProviderOperation, errs.ErrConflict)
		}
	}
	return stored, err
}

// ListProviderOperations returns provider operation audit records.
func (r *Repository) ListProviderOperations(ctx context.Context, filter query.ProviderOperationFilter) ([]entity.ProviderOperation, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListProviderOperations, queryProviderOperationList, providerOperationFilterArgs(filter), scanProviderOperation)
}

// ClaimOutboxEvents leases unpublished outbox events for delivery.
func (r *Repository) ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error) {
	events, ok, err := postgreslib.ClaimOutboxRows(ctx, r.db, queryOutboxEventClaim, limit, now, lockedUntil, scanOutboxEvent)
	if !ok {
		return nil, wrapError(operationClaimOutboxEvents, errs.ErrInvalidArgument)
	}
	return events, wrapError(operationClaimOutboxEvents, err)
}

// MarkOutboxEventPublished marks a leased outbox event as published.
func (r *Repository) MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error {
	err := postgreslib.ApplyOutboxPublished(ctx, r.db, queryOutboxEventMarkPublished, errs.ErrInvalidArgument, id, attemptCount, publishedAt)
	return wrapError(operationMarkOutboxEventPublished, err)
}

// MarkOutboxEventFailed schedules a leased outbox event for retry.
func (r *Repository) MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error {
	return r.markOutboxEventDeliveryFailure(ctx, operationMarkOutboxEventFailed, queryOutboxEventMarkFailed, id, attemptCount, "next_attempt_at", nextAttemptAt, lastError)
}

// MarkOutboxEventPermanentlyFailed moves a leased outbox event to terminal failure.
func (r *Repository) MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error {
	return r.markOutboxEventDeliveryFailure(ctx, operationMarkOutboxEventPermanentlyFailed, queryOutboxEventMarkPermanentlyFailed, id, attemptCount, "failed_permanently_at", failedAt, lastError)
}

func (r *Repository) markOutboxEventDeliveryFailure(ctx context.Context, operation string, queryText string, id uuid.UUID, attemptCount int, timestampName string, timestampValue time.Time, lastError string) error {
	err := postgreslib.ApplyOutboxDeliveryFailure(ctx, r.db, queryText, errs.ErrInvalidArgument, id, attemptCount, timestampName, timestampValue, lastError)
	return wrapError(operation, err)
}

const (
	operationStoreWebhookEvent                = "domain.Repository.StoreWebhookEvent"
	operationProcessWebhookEvent              = "domain.Repository.ProcessWebhookEvent"
	operationGetWebhookEvent                  = "domain.Repository.GetWebhookEvent"
	operationListWebhookEvents                = "domain.Repository.ListWebhookEvents"
	operationListProviderEvents               = "domain.Repository.ListProviderEvents"
	operationGetWorkItemProjection            = "domain.Repository.GetWorkItemProjection"
	operationListWorkItemProjections          = "domain.Repository.ListWorkItemProjections"
	operationListComments                     = "domain.Repository.ListComments"
	operationListRelationships                = "domain.Repository.ListRelationships"
	operationGetAccountRuntimeState           = "domain.Repository.GetAccountRuntimeState"
	operationListAccountRuntimeStates         = "domain.Repository.ListAccountRuntimeStates"
	operationUpsertAccountRuntimeState        = "domain.Repository.UpsertAccountRuntimeState"
	operationRecordLimitSnapshot              = "domain.Repository.RecordLimitSnapshot"
	operationListLimitSnapshots               = "domain.Repository.ListLimitSnapshots"
	operationRecordProviderOperation          = "domain.Repository.RecordProviderOperation"
	operationListProviderOperations           = "domain.Repository.ListProviderOperations"
	operationClaimOutboxEvents                = "domain.Repository.ClaimOutboxEvents"
	operationMarkOutboxEventPublished         = "domain.Repository.MarkOutboxEventPublished"
	operationMarkOutboxEventFailed            = "domain.Repository.MarkOutboxEventFailed"
	operationMarkOutboxEventPermanentlyFailed = "domain.Repository.MarkOutboxEventPermanentlyFailed"
)

func queryOne[T any](ctx context.Context, db queryer, operation string, sql string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) (T, error) {
	result, err := scan(db.QueryRow(ctx, sql, args))
	if err != nil {
		return result, wrapError(operation, err)
	}
	return result, nil
}

func queryPage[T any](ctx context.Context, db queryer, operation string, sql string, paging pageQueryArgs, scan func(postgreslib.RowScanner) (T, error)) ([]T, query.PageResult, error) {
	rows, err := db.Query(ctx, sql, paging.args)
	if err != nil {
		return nil, query.PageResult{}, wrapError(operation, err)
	}
	items, err := postgreslib.ScanRows(rows, scan)
	if err != nil {
		return nil, query.PageResult{}, wrapError(operation, err)
	}
	pageItems, page := pageResult(items, paging.limit, paging.nextOffset)
	return pageItems, page, nil
}

func insertProviderEvents(ctx context.Context, db queryer, operation string, events []entity.ProviderEvent) ([]entity.ProviderEvent, error) {
	if len(events) == 0 {
		return nil, nil
	}
	result := make([]entity.ProviderEvent, 0, len(events))
	for _, event := range events {
		inserted, err := queryOne(ctx, db, operation, queryProviderEventInsert, providerEventArgs(event), scanProviderEvent)
		if err != nil {
			return nil, err
		}
		result = append(result, inserted)
	}
	return result, nil
}

func insertOutboxEvents(ctx context.Context, db execer, operation string, events []entity.OutboxEvent) error {
	for _, event := range events {
		if _, err := db.Exec(ctx, queryOutboxEventCreate, outboxEventArgs(event)); err != nil {
			return wrapError(operation, err)
		}
	}
	return nil
}

func applyProjectionUpdate(ctx context.Context, db queryer, operation string, update providerrepo.ProjectionUpdate) error {
	if update.WorkItem == nil {
		return nil
	}
	storedWorkItem, err := queryOne(ctx, db, operation, queryWorkItemProjectionUpsert, workItemProjectionArgs(*update.WorkItem), scanWorkItemProjection)
	if err != nil {
		return err
	}
	for _, comment := range update.Comments {
		comment.WorkItemProjectionID = storedWorkItem.ID
		if _, err := queryOne(ctx, db, operation, queryCommentProjectionUpsert, commentProjectionArgs(comment), scanCommentProjection); err != nil {
			return err
		}
	}
	for _, relationship := range update.Relationships {
		relationship.SourceWorkItemID = storedWorkItem.ID
		if _, err := queryOne(ctx, db, operation, queryRelationshipUpsert, relationshipArgs(relationship), scanRelationship); err != nil {
			return err
		}
	}
	return nil
}

func sameWebhookEvent(left entity.WebhookEvent, right entity.WebhookEvent) bool {
	return left.ProviderSlug == right.ProviderSlug &&
		left.DeliveryID == right.DeliveryID &&
		left.EventName == right.EventName &&
		left.RepositoryProviderID == right.RepositoryProviderID &&
		bytes.Equal(compactJSON(left.PayloadJSON), compactJSON(right.PayloadJSON))
}

func compactJSON(raw []byte) []byte {
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, raw); err != nil {
		return bytes.TrimSpace(raw)
	}
	return compacted.Bytes()
}
