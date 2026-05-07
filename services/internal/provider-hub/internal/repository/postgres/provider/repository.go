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

	providerevents "github.com/codex-k8s/kodex/libs/go/platformevents/provider"
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
		projectionResult, err := applyProjectionUpdate(ctx, tx, operationStoreWebhookEvent, projectionUpdate)
		if err != nil {
			return err
		}
		filteredProviderEvents := filterProviderEvents(providerEvents, projectionUpdate, projectionResult)
		insertedProviderEvents, err := insertProviderEvents(ctx, tx, operationStoreWebhookEvent, filteredProviderEvents)
		if err != nil {
			return err
		}
		filteredOutboxEvents := filterOutboxEvents(outboxEvents, filteredProviderEvents, projectionResult)
		if err := insertOutboxEvents(ctx, tx, operationStoreWebhookEvent, filteredOutboxEvents); err != nil {
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
		projectionResult, err := applyProjectionUpdate(ctx, tx, operationProcessWebhookEvent, projectionUpdate)
		if err != nil {
			return err
		}
		filteredProviderEvents := filterProviderEvents(providerEvents, projectionUpdate, projectionResult)
		if _, err := insertProviderEvents(ctx, tx, operationProcessWebhookEvent, filteredProviderEvents); err != nil {
			return err
		}
		filteredOutboxEvents := filterOutboxEvents(outboxEvents, filteredProviderEvents, projectionResult)
		return insertOutboxEvents(ctx, tx, operationProcessWebhookEvent, filteredOutboxEvents)
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

// UpsertSyncCursor creates or updates one reconciliation cursor.
func (r *Repository) UpsertSyncCursor(ctx context.Context, cursor entity.SyncCursor) (entity.SyncCursor, error) {
	return queryOne(ctx, r.db, operationUpsertSyncCursor, querySyncCursorUpsert, syncCursorArgs(cursor), scanSyncCursor)
}

// GetSyncCursor returns one reconciliation cursor by id.
func (r *Repository) GetSyncCursor(ctx context.Context, id uuid.UUID) (entity.SyncCursor, error) {
	return queryOne(ctx, r.db, operationGetSyncCursor, querySyncCursorGet, pgx.NamedArgs{"id": id}, scanSyncCursor)
}

// ListSyncCursors returns reconciliation cursors by supported filters.
func (r *Repository) ListSyncCursors(ctx context.Context, filter query.SyncCursorFilter) ([]entity.SyncCursor, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListSyncCursors, querySyncCursorList, syncCursorFilterArgs(filter), scanSyncCursor)
}

// ClaimSyncCursor leases one due reconciliation cursor for a worker.
func (r *Repository) ClaimSyncCursor(ctx context.Context, claim providerrepo.SyncCursorClaim) (entity.SyncCursor, error) {
	return queryOne(ctx, r.db, operationClaimSyncCursor, querySyncCursorClaim, syncCursorClaimArgs(claim), scanSyncCursor)
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
	operationUpsertSyncCursor                 = "domain.Repository.UpsertSyncCursor"
	operationGetSyncCursor                    = "domain.Repository.GetSyncCursor"
	operationListSyncCursors                  = "domain.Repository.ListSyncCursors"
	operationClaimSyncCursor                  = "domain.Repository.ClaimSyncCursor"
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

type projectionUpdater interface {
	queryer
	execer
}

type projectionApplyResult struct {
	hasProjection               bool
	workItemApplied             bool
	workItemProjectionID        uuid.UUID
	appliedCommentProjectionIDs map[uuid.UUID]struct{}
	appliedCommentProviderIDs   map[string]struct{}
	appliedRelationshipIDs      map[uuid.UUID]struct{}
}

func applyProjectionUpdate(ctx context.Context, db projectionUpdater, operation string, update providerrepo.ProjectionUpdate) (projectionApplyResult, error) {
	result := projectionApplyResult{
		appliedCommentProjectionIDs: make(map[uuid.UUID]struct{}),
		appliedCommentProviderIDs:   make(map[string]struct{}),
		appliedRelationshipIDs:      make(map[uuid.UUID]struct{}),
	}
	if update.WorkItem == nil {
		return result, nil
	}
	result.hasProjection = true
	storedWorkItem, workItemApplied, err := upsertFreshWorkItemProjection(ctx, db, operation, *update.WorkItem)
	if err != nil {
		return result, err
	}
	result.workItemApplied = workItemApplied
	result.workItemProjectionID = storedWorkItem.ID
	for _, comment := range update.Comments {
		comment.WorkItemProjectionID = storedWorkItem.ID
		storedComment, commentApplied, err := upsertFreshCommentProjection(ctx, db, operation, comment)
		if err != nil {
			return result, err
		}
		if commentApplied {
			result.appliedCommentProjectionIDs[storedComment.ID] = struct{}{}
			result.appliedCommentProviderIDs[storedComment.ProviderCommentID] = struct{}{}
		}
	}
	if workItemApplied {
		if err := rebuildWatermarkRelationships(ctx, db, operation, storedWorkItem.ID, update.Relationships, result.appliedRelationshipIDs); err != nil {
			return result, err
		}
	}
	return result, nil
}

func upsertFreshWorkItemProjection(ctx context.Context, db queryer, operation string, incoming entity.ProviderWorkItemProjection) (entity.ProviderWorkItemProjection, bool, error) {
	existing, err := queryOne(ctx, db, operation, queryWorkItemProjectionGet, workItemProjectionLookupArgs(query.ProviderTargetLookup{
		ProviderSlug:     incoming.ProviderSlug,
		ProviderObjectID: incoming.ProviderWorkItemID,
	}), scanWorkItemProjection)
	if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return entity.ProviderWorkItemProjection{}, false, err
	}
	if err == nil && !isProviderUpdateFresh(incoming.ProviderUpdatedAt, existing.ProviderUpdatedAt) {
		return existing, false, nil
	}
	stored, err := queryOne(ctx, db, operation, queryWorkItemProjectionUpsert, workItemProjectionArgs(incoming), scanWorkItemProjection)
	if err != nil {
		return entity.ProviderWorkItemProjection{}, false, err
	}
	return stored, providerTimestampMatches(incoming.ProviderUpdatedAt, stored.ProviderUpdatedAt), nil
}

func upsertFreshCommentProjection(ctx context.Context, db queryer, operation string, incoming entity.ProviderCommentProjection) (entity.ProviderCommentProjection, bool, error) {
	existing, err := queryOne(ctx, db, operation, queryCommentProjectionGetByProviderID, commentProjectionLookupArgs(incoming.WorkItemProjectionID, incoming.ProviderCommentID), scanCommentProjection)
	if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return entity.ProviderCommentProjection{}, false, err
	}
	if err == nil && !isProviderUpdateFresh(incoming.ProviderUpdatedAt, existing.ProviderUpdatedAt) {
		return existing, false, nil
	}
	stored, err := queryOne(ctx, db, operation, queryCommentProjectionUpsert, commentProjectionArgs(incoming), scanCommentProjection)
	if err != nil {
		return entity.ProviderCommentProjection{}, false, err
	}
	return stored, providerTimestampMatches(incoming.ProviderUpdatedAt, stored.ProviderUpdatedAt), nil
}

func rebuildWatermarkRelationships(ctx context.Context, db projectionUpdater, operation string, sourceWorkItemID uuid.UUID, relationships []entity.ProviderRelationship, applied map[uuid.UUID]struct{}) error {
	currentRelationshipIDs := make([]uuid.UUID, 0, len(relationships))
	for _, relationship := range relationships {
		relationship.SourceWorkItemID = sourceWorkItemID
		stored, err := queryOne(ctx, db, operation, queryRelationshipUpsert, relationshipArgs(relationship), scanRelationship)
		if err != nil {
			return err
		}
		currentRelationshipIDs = append(currentRelationshipIDs, stored.ID)
		applied[stored.ID] = struct{}{}
	}
	_, err := db.Exec(ctx, queryRelationshipDeleteMissingWatermark, watermarkRelationshipCleanupArgs(sourceWorkItemID, currentRelationshipIDs))
	if err != nil {
		return wrapError(operation, err)
	}
	return nil
}

func isProviderUpdateFresh(incoming *time.Time, current *time.Time) bool {
	if current == nil {
		return true
	}
	if incoming == nil {
		return false
	}
	return !incoming.Before(*current)
}

func providerTimestampMatches(incoming *time.Time, stored *time.Time) bool {
	if incoming == nil {
		return stored == nil
	}
	if stored == nil {
		return false
	}
	return incoming.Equal(*stored)
}

func filterProviderEvents(events []entity.ProviderEvent, update providerrepo.ProjectionUpdate, result projectionApplyResult) []entity.ProviderEvent {
	if !result.hasProjection {
		return events
	}
	filtered := make([]entity.ProviderEvent, 0, len(events))
	for _, event := range events {
		switch event.AggregateType {
		case providerevents.AggregateWorkItem:
			if update.WorkItem != nil && result.workItemApplied && event.AggregateID == update.WorkItem.ProviderWorkItemID {
				filtered = append(filtered, event)
			}
		case providerevents.AggregateComment:
			if _, ok := result.appliedCommentProviderIDs[event.AggregateID]; ok {
				filtered = append(filtered, event)
			}
		default:
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func filterOutboxEvents(events []entity.OutboxEvent, providerEvents []entity.ProviderEvent, result projectionApplyResult) []entity.OutboxEvent {
	if !result.hasProjection {
		return events
	}
	providerEventIDs := make(map[uuid.UUID]struct{}, len(providerEvents))
	for _, event := range providerEvents {
		providerEventIDs[event.ID] = struct{}{}
	}
	filtered := make([]entity.OutboxEvent, 0, len(events))
	for _, event := range events {
		switch event.EventType {
		case providerevents.EventWebhookNormalized:
			if _, ok := providerEventIDs[event.AggregateID]; ok {
				filtered = append(filtered, event)
			}
		case providerevents.EventWorkItemSynced:
			if result.workItemApplied && event.AggregateID == result.workItemProjectionID {
				filtered = append(filtered, event)
			}
		case providerevents.EventCommentSynced:
			if _, ok := result.appliedCommentProjectionIDs[event.AggregateID]; ok {
				filtered = append(filtered, event)
			}
		case providerevents.EventRelationshipSynced:
			if _, ok := result.appliedRelationshipIDs[event.AggregateID]; ok {
				filtered = append(filtered, event)
			}
		default:
			filtered = append(filtered, event)
		}
	}
	return filtered
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
