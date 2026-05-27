package interaction

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

// Repository is the domain persistence port for interaction-hub.
type Repository interface {
	Ready() bool
	RecordBacklogOperation(context.Context, enum.Operation) error
	CreateConversationThreadWithResult(context.Context, entity.ConversationThread, entity.CommandResult, entity.OutboxEvent) error
	GetConversationThread(context.Context, uuid.UUID) (entity.ConversationThread, error)
	CreateConversationMessageWithResult(context.Context, entity.ConversationMessage, entity.ConversationThread, int64, entity.CommandResult, entity.OutboxEvent) error
	GetConversationMessage(context.Context, uuid.UUID) (entity.ConversationMessage, error)
	ListConversationMessages(context.Context, query.ConversationMessageFilter) ([]entity.ConversationMessage, value.PageResult, error)
	CreateInteractionRequestWithResult(context.Context, entity.InteractionRequest, entity.CommandResult, entity.OutboxEvent) error
	UpdateInteractionRequestWithResult(context.Context, entity.InteractionRequest, int64, entity.CommandResult, entity.OutboxEvent) error
	UpdateInteractionRequestsWithResult(context.Context, []entity.InteractionRequest, map[uuid.UUID]int64, entity.CommandResult, []entity.OutboxEvent) error
	CreateInteractionResponseWithResult(context.Context, entity.InteractionResponse, entity.InteractionRequest, int64, entity.CommandResult, entity.OutboxEvent) error
	CreateChannelCallbackResponseWithResult(context.Context, entity.ChannelCallback, entity.InteractionResponse, entity.InteractionRequest, int64, entity.CommandResult, []entity.OutboxEvent) error
	GetInteractionRequest(context.Context, uuid.UUID) (entity.InteractionRequest, error)
	GetInteractionResponse(context.Context, uuid.UUID) (entity.InteractionResponse, error)
	GetInteractionResponseBySource(context.Context, enum.InteractionResponseSourceKind, string) (entity.InteractionResponse, error)
	ListInteractionRequests(context.Context, query.InteractionRequestFilter) ([]entity.InteractionRequest, value.PageResult, error)
	ListExpirableInteractionRequests(context.Context, value.ScopeRef, time.Time, int32) ([]entity.InteractionRequest, error)
	CreateNotificationWithResult(context.Context, entity.Notification, entity.CommandResult, entity.OutboxEvent) error
	GetNotification(context.Context, uuid.UUID) (entity.Notification, error)
	CreateSubscriptionWithResult(context.Context, entity.Subscription, entity.CommandResult, entity.OutboxEvent) error
	UpdateSubscriptionWithResult(context.Context, entity.Subscription, int64, entity.CommandResult, entity.OutboxEvent) error
	GetSubscription(context.Context, uuid.UUID) (entity.Subscription, error)
	ListSubscriptions(context.Context, query.SubscriptionFilter) ([]entity.Subscription, value.PageResult, error)
	CreateDeliveryAttemptWithResult(context.Context, entity.DeliveryAttempt, entity.CommandResult, entity.OutboxEvent) error
	UpdateDeliveryAttemptWithResult(context.Context, entity.DeliveryAttempt, entity.CommandResult, entity.OutboxEvent) error
	GetDeliveryRoute(context.Context, uuid.UUID) (entity.DeliveryRoute, error)
	FindActiveDeliveryRoute(context.Context, value.ScopeRef) (entity.DeliveryRoute, error)
	GetDeliveryAttempt(context.Context, uuid.UUID) (entity.DeliveryAttempt, error)
	GetDeliveryAttemptByDeliveryID(context.Context, string) (entity.DeliveryAttempt, error)
	ListDeliveryAttempts(context.Context, query.DeliveryAttemptFilter) ([]entity.DeliveryAttempt, error)
	CreateChannelCallbackWithResult(context.Context, entity.ChannelCallback, entity.CommandResult, entity.OutboxEvent) error
	GetChannelCallback(context.Context, uuid.UUID) (entity.ChannelCallback, error)
	GetChannelCallbackByCallbackID(context.Context, string) (entity.ChannelCallback, error)
	GetLatestChannelCallback(context.Context, query.ChannelCallbackFilter) (entity.ChannelCallback, error)
	GetCommandResult(context.Context, query.CommandIdentity) (entity.CommandResult, error)
	ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error)
	MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error
	MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error
	MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error
}
