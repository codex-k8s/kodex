// Package interaction implements the PostgreSQL repository for interaction-hub.
package interaction

import (
	"context"
	"embed"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/errs"
	interactionrepo "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/repository/interaction"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

// SQLFiles contains named SQL queries for interaction-hub repository.
//
//go:embed sql/*.sql
var SQLFiles embed.FS

var _ interactionrepo.Repository = (*Repository)(nil)

type database interface {
	execQuerier
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

type execQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct {
	db database
}

const (
	operationBacklog                   = "domain.Repository.RecordBacklogOperation"
	operationCreateConversationThread  = "domain.Repository.CreateConversationThreadWithResult"
	operationCreateConversationMessage = "domain.Repository.CreateConversationMessageWithResult"
	operationCreateInteractionRequest  = "domain.Repository.CreateInteractionRequestWithResult"
	operationUpdateInteractionRequest  = "domain.Repository.UpdateInteractionRequestWithResult"
	operationUpdateInteractionRequests = "domain.Repository.UpdateInteractionRequestsWithResult"
	operationCreateInteractionResponse = "domain.Repository.CreateInteractionResponseWithResult"
	operationCreateNotification        = "domain.Repository.CreateNotificationWithResult"
	operationCreateSubscription        = "domain.Repository.CreateSubscriptionWithResult"
	operationCreateDeliveryAttempt     = "domain.Repository.CreateDeliveryAttemptWithResult"
	operationUpdateDeliveryAttempt     = "domain.Repository.UpdateDeliveryAttemptWithResult"
	operationUpdateSubscription        = "domain.Repository.UpdateSubscriptionWithResult"
	operationGetCommandResult          = "domain.Repository.GetCommandResult"
	operationGetConversationMessage    = "domain.Repository.GetConversationMessage"
	operationGetConversationThread     = "domain.Repository.GetConversationThread"
	operationGetDeliveryAttempt        = "domain.Repository.GetDeliveryAttempt"
	operationGetDeliveryByDeliveryID   = "domain.Repository.GetDeliveryAttemptByDeliveryID"
	operationGetDeliveryRoute          = "domain.Repository.GetDeliveryRoute"
	operationGetInteractionRequest     = "domain.Repository.GetInteractionRequest"
	operationGetInteractionResponse    = "domain.Repository.GetInteractionResponse"
	operationGetNotification           = "domain.Repository.GetNotification"
	operationGetSubscription           = "domain.Repository.GetSubscription"
	operationFindActiveDeliveryRoute   = "domain.Repository.FindActiveDeliveryRoute"
	operationListDeliveryAttempts      = "domain.Repository.ListDeliveryAttempts"
	operationListConversationMessages  = "domain.Repository.ListConversationMessages"
	operationListInteractionRequests   = "domain.Repository.ListInteractionRequests"
	operationListExpirableRequests     = "domain.Repository.ListExpirableInteractionRequests"
	operationListSubscriptions         = "domain.Repository.ListSubscriptions"
	operationOutboxClaim               = "domain.Repository.ClaimOutboxEvents"
	operationOutboxMarkFailed          = "domain.Repository.MarkOutboxEventFailed"
	operationOutboxMarkPermanent       = "domain.Repository.MarkOutboxEventPermanentlyFailed"
	operationOutboxMarkPublished       = "domain.Repository.MarkOutboxEventPublished"
)

// NewRepository creates an interaction-hub PostgreSQL repository.
func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Ready() bool {
	return r != nil && r.db != nil
}

func (r *Repository) RecordBacklogOperation(_ context.Context, operation enum.Operation) error {
	if !operation.Valid() {
		return wrapError(operationBacklog, errs.ErrInvalidArgument)
	}
	return nil
}

func (r *Repository) CreateConversationThreadWithResult(ctx context.Context, thread entity.ConversationThread, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutate(ctx, operationCreateConversationThread,
		affectedMutation(queryThreadCreate, threadArgs(thread)),
		commandResultMutation(result),
		outboxEventMutation(event),
	)
}

func (r *Repository) GetConversationThread(ctx context.Context, id uuid.UUID) (entity.ConversationThread, error) {
	return queryOne(ctx, r.db, operationGetConversationThread, queryThreadGet, pgx.NamedArgs{"id": id}, scanThread)
}

func (r *Repository) CreateConversationMessageWithResult(ctx context.Context, message entity.ConversationMessage, thread entity.ConversationThread, previousThreadVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutate(ctx, operationCreateConversationMessage,
		affectedMutation(queryMessageCreate, messageArgs(message)),
		affectedMutation(queryThreadUpdateLatestMessage, threadLatestMessageArgs(thread, previousThreadVersion)),
		commandResultMutation(result),
		outboxEventMutation(event),
	)
}

func (r *Repository) GetConversationMessage(ctx context.Context, id uuid.UUID) (entity.ConversationMessage, error) {
	return queryOne(ctx, r.db, operationGetConversationMessage, queryMessageGet, pgx.NamedArgs{"id": id}, scanMessage)
}

func (r *Repository) ListConversationMessages(ctx context.Context, filter query.ConversationMessageFilter) ([]entity.ConversationMessage, value.PageResult, error) {
	args := messageFilterArgs(filter)
	items, err := queryAll(ctx, r.db, operationListConversationMessages, queryMessageList, args.NamedArgs, scanMessage)
	if err != nil {
		return nil, value.PageResult{}, err
	}
	pageItems, page := pageFromItems(items, args)
	return pageItems, page, nil
}

func (r *Repository) CreateInteractionRequestWithResult(ctx context.Context, request entity.InteractionRequest, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutate(ctx, operationCreateInteractionRequest,
		affectedMutation(queryRequestCreate, requestArgs(request)),
		commandResultMutation(result),
		outboxEventMutation(event),
	)
}

func (r *Repository) UpdateInteractionRequestWithResult(ctx context.Context, request entity.InteractionRequest, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutate(ctx, operationUpdateInteractionRequest,
		affectedMutation(queryRequestUpdateStatus, requestUpdateStatusArgs(request, previousVersion)),
		commandResultMutation(result),
		outboxEventMutation(event),
	)
}

func (r *Repository) UpdateInteractionRequestsWithResult(ctx context.Context, requests []entity.InteractionRequest, previousVersions map[uuid.UUID]int64, result entity.CommandResult, events []entity.OutboxEvent) error {
	if len(requests) != len(events) {
		return wrapError(operationUpdateInteractionRequests, errs.ErrInvalidArgument)
	}
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		if len(requests) > 0 {
			if err := runRequestStatusBatch(ctx, tx, requests, previousVersions); err != nil {
				return err
			}
			if err := runOutboxBatch(ctx, tx, events); err != nil {
				return err
			}
		}
		return postgreslib.RunMutation(ctx, tx, errs.ErrConflict, commandResultMutation(result))
	})
	return wrapError(operationUpdateInteractionRequests, err)
}

func (r *Repository) CreateInteractionResponseWithResult(ctx context.Context, response entity.InteractionResponse, request entity.InteractionRequest, previousRequestVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutate(ctx, operationCreateInteractionResponse,
		affectedMutation(queryResponseCreate, responseArgs(response)),
		affectedMutation(queryRequestUpdateStatus, requestUpdateStatusArgs(request, previousRequestVersion)),
		commandResultMutation(result),
		outboxEventMutation(event),
	)
}

func (r *Repository) GetInteractionRequest(ctx context.Context, id uuid.UUID) (entity.InteractionRequest, error) {
	return queryOne(ctx, r.db, operationGetInteractionRequest, queryRequestGet, pgx.NamedArgs{"id": id}, scanRequest)
}

func (r *Repository) GetInteractionResponse(ctx context.Context, id uuid.UUID) (entity.InteractionResponse, error) {
	return queryOne(ctx, r.db, operationGetInteractionResponse, queryResponseGet, pgx.NamedArgs{"id": id}, scanResponse)
}

func (r *Repository) ListInteractionRequests(ctx context.Context, filter query.InteractionRequestFilter) ([]entity.InteractionRequest, value.PageResult, error) {
	args := requestFilterArgs(filter)
	items, err := queryAll(ctx, r.db, operationListInteractionRequests, queryRequestList, args.NamedArgs, scanRequest)
	if err != nil {
		return nil, value.PageResult{}, err
	}
	pageItems, page := pageFromItems(items, args)
	return pageItems, page, nil
}

func (r *Repository) ListExpirableInteractionRequests(ctx context.Context, scope value.ScopeRef, deadlineBefore time.Time, limit int32) ([]entity.InteractionRequest, error) {
	return queryAll(ctx, r.db, operationListExpirableRequests, queryRequestListExpirable, expirableRequestArgs(scope, deadlineBefore, limit), scanRequest)
}

func (r *Repository) CreateNotificationWithResult(ctx context.Context, notification entity.Notification, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutate(ctx, operationCreateNotification,
		affectedMutation(queryNotificationCreate, notificationArgs(notification)),
		commandResultMutation(result),
		outboxEventMutation(event),
	)
}

func (r *Repository) GetNotification(ctx context.Context, id uuid.UUID) (entity.Notification, error) {
	return queryOne(ctx, r.db, operationGetNotification, queryNotificationGet, pgx.NamedArgs{"id": id}, scanNotification)
}

func (r *Repository) CreateSubscriptionWithResult(ctx context.Context, subscription entity.Subscription, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutate(ctx, operationCreateSubscription,
		affectedMutation(querySubscriptionCreate, subscriptionArgs(subscription)),
		commandResultMutation(result),
		outboxEventMutation(event),
	)
}

func (r *Repository) UpdateSubscriptionWithResult(ctx context.Context, subscription entity.Subscription, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutate(ctx, operationUpdateSubscription,
		affectedMutation(querySubscriptionUpdate, subscriptionUpdateArgs(subscription, previousVersion)),
		commandResultMutation(result),
		outboxEventMutation(event),
	)
}

func (r *Repository) GetSubscription(ctx context.Context, id uuid.UUID) (entity.Subscription, error) {
	return queryOne(ctx, r.db, operationGetSubscription, querySubscriptionGet, pgx.NamedArgs{"id": id}, scanSubscription)
}

func (r *Repository) ListSubscriptions(ctx context.Context, filter query.SubscriptionFilter) ([]entity.Subscription, value.PageResult, error) {
	args := subscriptionFilterArgs(filter)
	items, err := queryAll(ctx, r.db, operationListSubscriptions, querySubscriptionList, args.NamedArgs, scanSubscription)
	if err != nil {
		return nil, value.PageResult{}, err
	}
	pageItems, page := pageFromItems(items, args)
	return pageItems, page, nil
}

func (r *Repository) CreateDeliveryAttemptWithResult(ctx context.Context, attempt entity.DeliveryAttempt, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutate(ctx, operationCreateDeliveryAttempt,
		affectedMutation(queryDeliveryAttemptCreate, deliveryAttemptArgs(attempt)),
		commandResultMutation(result),
		outboxEventMutation(event),
	)
}

func (r *Repository) UpdateDeliveryAttemptWithResult(ctx context.Context, attempt entity.DeliveryAttempt, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutate(ctx, operationUpdateDeliveryAttempt,
		affectedMutation(queryDeliveryAttemptUpdate, deliveryAttemptArgs(attempt)),
		commandResultMutation(result),
		outboxEventMutation(event),
	)
}

func (r *Repository) GetDeliveryRoute(ctx context.Context, id uuid.UUID) (entity.DeliveryRoute, error) {
	return queryOne(ctx, r.db, operationGetDeliveryRoute, queryDeliveryRouteGet, pgx.NamedArgs{"id": id}, scanDeliveryRoute)
}

func (r *Repository) FindActiveDeliveryRoute(ctx context.Context, scope value.ScopeRef) (entity.DeliveryRoute, error) {
	return queryOne(ctx, r.db, operationFindActiveDeliveryRoute, queryDeliveryRouteFindActive, pgx.NamedArgs{
		"scope_type": string(scope.Type),
		"scope_ref":  scope.Ref,
	}, scanDeliveryRoute)
}

func (r *Repository) GetDeliveryAttempt(ctx context.Context, id uuid.UUID) (entity.DeliveryAttempt, error) {
	return queryOne(ctx, r.db, operationGetDeliveryAttempt, queryDeliveryAttemptGet, pgx.NamedArgs{"id": id}, scanDeliveryAttempt)
}

func (r *Repository) GetDeliveryAttemptByDeliveryID(ctx context.Context, deliveryID string) (entity.DeliveryAttempt, error) {
	return queryOne(ctx, r.db, operationGetDeliveryByDeliveryID, queryDeliveryAttemptGetByID, pgx.NamedArgs{"delivery_id": deliveryID}, scanDeliveryAttempt)
}

func (r *Repository) ListDeliveryAttempts(ctx context.Context, filter query.DeliveryAttemptFilter) ([]entity.DeliveryAttempt, error) {
	return queryAll(ctx, r.db, operationListDeliveryAttempts, queryDeliveryAttemptList, deliveryAttemptFilterArgs(filter), scanDeliveryAttempt)
}

func (r *Repository) GetCommandResult(ctx context.Context, identity query.CommandIdentity) (entity.CommandResult, error) {
	return queryOne(ctx, r.db, operationGetCommandResult, queryCommandResultGet, commandIdentityArgs(identity), scanCommandResult)
}

func (r *Repository) ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error) {
	events, ok, err := postgreslib.ClaimOutboxRows(ctx, r.db, queryOutboxEventClaim, limit, now, lockedUntil, scanOutboxEvent)
	if !ok {
		return nil, wrapError(operationOutboxClaim, errs.ErrInvalidArgument)
	}
	return events, wrapError(operationOutboxClaim, err)
}

func (r *Repository) MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error {
	err := postgreslib.ApplyOutboxPublished(ctx, r.db, queryOutboxEventMarkPublished, errs.ErrInvalidArgument, id, attemptCount, publishedAt)
	return wrapError(operationOutboxMarkPublished, err)
}

func (r *Repository) MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error {
	return r.markOutboxFailure(ctx, operationOutboxMarkFailed, queryOutboxEventMarkFailed, id, attemptCount, "next_attempt_at", nextAttemptAt, lastError)
}

func (r *Repository) MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error {
	return r.markOutboxFailure(ctx, operationOutboxMarkPermanent, queryOutboxEventMarkPermanent, id, attemptCount, "failed_permanently_at", failedAt, lastError)
}

func (r *Repository) markOutboxFailure(ctx context.Context, operation string, queryText string, id uuid.UUID, attemptCount int, timestampName string, timestamp time.Time, lastError string) error {
	err := postgreslib.ApplyOutboxDeliveryFailure(ctx, r.db, queryText, errs.ErrInvalidArgument, id, attemptCount, timestampName, timestamp, lastError)
	return wrapError(operation, err)
}

type mutation = postgreslib.Mutation

func (r *Repository) mutate(ctx context.Context, operation string, mutations ...mutation) error {
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		return postgreslib.RunDistinctMutations(ctx, tx, errs.ErrConflict, mutations...)
	})
	return wrapError(operation, err)
}

func affectedMutation(queryText string, args pgx.NamedArgs) mutation {
	return mutation{Query: queryText, Args: args, RequireAffected: true}
}

func commandResultMutation(result entity.CommandResult) mutation {
	return affectedMutation(queryCommandResultCreate, commandResultArgs(result))
}

func outboxEventMutation(event entity.OutboxEvent) mutation {
	return affectedMutation(queryOutboxEventCreate, outboxEventArgs(event))
}

func runRequestStatusBatch(ctx context.Context, tx pgx.Tx, requests []entity.InteractionRequest, previousVersions map[uuid.UUID]int64) error {
	batch := &pgx.Batch{}
	for _, request := range requests {
		previousVersion, ok := previousVersions[request.ID]
		if !ok {
			return errs.ErrInvalidArgument
		}
		batch.Queue(queryRequestUpdateStatus, requestUpdateStatusArgs(request, previousVersion))
	}
	results := tx.SendBatch(ctx, batch)
	for range requests {
		tag, err := results.Exec()
		if err != nil {
			_ = results.Close()
			return err
		}
		if tag.RowsAffected() == 0 {
			_ = results.Close()
			return errs.ErrConflict
		}
	}
	return results.Close()
}

func runOutboxBatch(ctx context.Context, tx pgx.Tx, events []entity.OutboxEvent) error {
	batch := &pgx.Batch{}
	for _, event := range events {
		batch.Queue(queryOutboxEventCreate, outboxEventArgs(event))
	}
	results := tx.SendBatch(ctx, batch)
	for range events {
		tag, err := results.Exec()
		if err != nil {
			_ = results.Close()
			return err
		}
		if tag.RowsAffected() == 0 {
			_ = results.Close()
			return errs.ErrConflict
		}
	}
	return results.Close()
}

func queryOne[T any](ctx context.Context, db execQuerier, operation string, queryText string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) (T, error) {
	value, err := scan(db.QueryRow(ctx, queryText, args))
	if err != nil {
		var zero T
		return zero, wrapError(operation, err)
	}
	return value, nil
}

func queryAll[T any](ctx context.Context, db execQuerier, operation string, queryText string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) ([]T, error) {
	rows, err := db.Query(ctx, queryText, args)
	if err != nil {
		return nil, wrapError(operation, err)
	}
	items, err := postgreslib.ScanRows(rows, scan)
	if err != nil {
		return nil, wrapError(operation, err)
	}
	return items, nil
}
