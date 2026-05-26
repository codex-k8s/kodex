package interaction

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/errs"
	interactionrepo "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/repository/interaction"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

var _ interactionrepo.Repository = (*Repository)(nil)

// Repository is an IH-2 persistence stub that records no domain state.
type Repository struct{}

// NewRepository creates the scaffold repository used until IH-3 adds PostgreSQL.
func NewRepository() *Repository {
	return &Repository{}
}

// Ready reports that the scaffold repository is composed.
func (r *Repository) Ready() bool {
	return r != nil
}

// RecordBacklogOperation accepts a stable operation without persisting state.
func (r *Repository) RecordBacklogOperation(context.Context, enum.Operation) error {
	return nil
}

func (r *Repository) CreateConversationThreadWithResult(context.Context, entity.ConversationThread, entity.CommandResult, entity.OutboxEvent) error {
	return errs.ErrNotImplemented
}

func (r *Repository) GetConversationThread(context.Context, uuid.UUID) (entity.ConversationThread, error) {
	return entity.ConversationThread{}, errs.ErrNotFound
}

func (r *Repository) CreateConversationMessageWithResult(context.Context, entity.ConversationMessage, entity.ConversationThread, int64, entity.CommandResult, entity.OutboxEvent) error {
	return errs.ErrNotImplemented
}

func (r *Repository) GetConversationMessage(context.Context, uuid.UUID) (entity.ConversationMessage, error) {
	return entity.ConversationMessage{}, errs.ErrNotFound
}

func (r *Repository) ListConversationMessages(context.Context, query.ConversationMessageFilter) ([]entity.ConversationMessage, value.PageResult, error) {
	return nil, value.PageResult{}, errs.ErrNotImplemented
}

func (r *Repository) CreateInteractionRequestWithResult(context.Context, entity.InteractionRequest, entity.CommandResult, entity.OutboxEvent) error {
	return errs.ErrNotImplemented
}

func (r *Repository) UpdateInteractionRequestWithResult(context.Context, entity.InteractionRequest, int64, entity.CommandResult, entity.OutboxEvent) error {
	return errs.ErrNotImplemented
}

func (r *Repository) UpdateInteractionRequestsWithResult(context.Context, []entity.InteractionRequest, map[uuid.UUID]int64, entity.CommandResult, []entity.OutboxEvent) error {
	return errs.ErrNotImplemented
}

func (r *Repository) CreateInteractionResponseWithResult(context.Context, entity.InteractionResponse, entity.InteractionRequest, int64, entity.CommandResult, entity.OutboxEvent) error {
	return errs.ErrNotImplemented
}

func (r *Repository) GetInteractionRequest(context.Context, uuid.UUID) (entity.InteractionRequest, error) {
	return entity.InteractionRequest{}, errs.ErrNotFound
}

func (r *Repository) GetInteractionResponse(context.Context, uuid.UUID) (entity.InteractionResponse, error) {
	return entity.InteractionResponse{}, errs.ErrNotFound
}

func (r *Repository) ListInteractionRequests(context.Context, query.InteractionRequestFilter) ([]entity.InteractionRequest, value.PageResult, error) {
	return nil, value.PageResult{}, errs.ErrNotImplemented
}

func (r *Repository) ListExpirableInteractionRequests(context.Context, value.ScopeRef, time.Time, int32) ([]entity.InteractionRequest, error) {
	return nil, errs.ErrNotImplemented
}

func (r *Repository) CreateNotificationWithResult(context.Context, entity.Notification, entity.CommandResult, entity.OutboxEvent) error {
	return errs.ErrNotImplemented
}

func (r *Repository) GetNotification(context.Context, uuid.UUID) (entity.Notification, error) {
	return entity.Notification{}, errs.ErrNotFound
}

func (r *Repository) CreateSubscriptionWithResult(context.Context, entity.Subscription, entity.CommandResult, entity.OutboxEvent) error {
	return errs.ErrNotImplemented
}

func (r *Repository) UpdateSubscriptionWithResult(context.Context, entity.Subscription, int64, entity.CommandResult, entity.OutboxEvent) error {
	return errs.ErrNotImplemented
}

func (r *Repository) GetSubscription(context.Context, uuid.UUID) (entity.Subscription, error) {
	return entity.Subscription{}, errs.ErrNotFound
}

func (r *Repository) ListSubscriptions(context.Context, query.SubscriptionFilter) ([]entity.Subscription, value.PageResult, error) {
	return nil, value.PageResult{}, errs.ErrNotImplemented
}

func (r *Repository) CreateDeliveryAttemptWithResult(context.Context, entity.DeliveryAttempt, entity.CommandResult, entity.OutboxEvent) error {
	return errs.ErrNotImplemented
}

func (r *Repository) UpdateDeliveryAttemptWithResult(context.Context, entity.DeliveryAttempt, entity.CommandResult, entity.OutboxEvent) error {
	return errs.ErrNotImplemented
}

func (r *Repository) GetDeliveryRoute(context.Context, uuid.UUID) (entity.DeliveryRoute, error) {
	return entity.DeliveryRoute{}, errs.ErrNotFound
}

func (r *Repository) FindActiveDeliveryRoute(context.Context, value.ScopeRef) (entity.DeliveryRoute, error) {
	return entity.DeliveryRoute{}, errs.ErrNotFound
}

func (r *Repository) GetDeliveryAttempt(context.Context, uuid.UUID) (entity.DeliveryAttempt, error) {
	return entity.DeliveryAttempt{}, errs.ErrNotFound
}

func (r *Repository) GetDeliveryAttemptByDeliveryID(context.Context, string) (entity.DeliveryAttempt, error) {
	return entity.DeliveryAttempt{}, errs.ErrNotFound
}

func (r *Repository) ListDeliveryAttempts(context.Context, query.DeliveryAttemptFilter) ([]entity.DeliveryAttempt, error) {
	return nil, errs.ErrNotImplemented
}

func (r *Repository) GetCommandResult(context.Context, query.CommandIdentity) (entity.CommandResult, error) {
	return entity.CommandResult{}, errs.ErrNotFound
}

func (r *Repository) ClaimOutboxEvents(context.Context, int, time.Time, time.Time) ([]entity.OutboxEvent, error) {
	return nil, errs.ErrNotImplemented
}

func (r *Repository) MarkOutboxEventPublished(context.Context, uuid.UUID, int, time.Time) error {
	return errs.ErrNotImplemented
}

func (r *Repository) MarkOutboxEventFailed(context.Context, uuid.UUID, int, time.Time, string) error {
	return errs.ErrNotImplemented
}

func (r *Repository) MarkOutboxEventPermanentlyFailed(context.Context, uuid.UUID, int, time.Time, string) error {
	return errs.ErrNotImplemented
}
