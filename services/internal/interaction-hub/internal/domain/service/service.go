package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	interactionevents "github.com/codex-k8s/kodex/libs/go/platformevents/interaction"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/errs"
	interactionrepo "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/repository/interaction"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

const (
	maxMessageBodySummaryRunes = 2000
	maxSafeMetadataEntries     = 20
	maxSafeMetadataKeyBytes    = 64
	maxSafeMetadataValueBytes  = 256
	maxSafeMetadataTotalBytes  = 2048
	maxInteractionRefs         = 50
	maxInteractionRefBytes     = 256
	maxPolicyJSONBytes         = 8192
	maxPolicyJSONDepth         = 8
	defaultExpireLimit         = int32(100)
	maxExpireLimit             = int32(500)
	maxDeliveryStatusAttempts  = int32(100)
	aggregateResponse          = "response"

	callbackErrorRejected          = "CALLBACK_REJECTED"
	callbackErrorRequestResolved   = "REQUEST_ALREADY_RESOLVED"
	callbackErrorActionNotAllowed  = "CALLBACK_ACTION_NOT_ALLOWED"
	callbackErrorActionUnsupported = "CALLBACK_ACTION_UNSUPPORTED"
	callbackErrorActionNotTerminal = "CALLBACK_ACTION_NOT_TERMINAL"
	callbackErrorActorRequired     = "CALLBACK_ACTOR_REQUIRED"
	callbackErrorResponseRequired  = "CALLBACK_RESPONSE_REQUIRED"
)

// Service coordinates interaction-hub domain use cases.
type Service struct {
	repository interactionrepo.Repository
	clock      value.Clock
	ids        value.UUIDGenerator
}

// New creates a domain service with injected persistence.
func New(repository interactionrepo.Repository) *Service {
	return NewWithConfig(repository, Config{Clock: systemClock{}, UUIDGenerator: uuidGenerator{}})
}

func NewWithConfig(repository interactionrepo.Repository, cfg Config) *Service {
	if cfg.Clock == nil {
		cfg.Clock = systemClock{}
	}
	if cfg.UUIDGenerator == nil {
		cfg.UUIDGenerator = uuidGenerator{}
	}
	return &Service{repository: repository, clock: cfg.Clock, ids: cfg.UUIDGenerator}
}

// Ready reports whether the composed domain dependencies are available.
func (s *Service) Ready() bool {
	return s != nil && s.repository != nil && s.repository.Ready()
}

func (s *Service) CreateConversationThread(ctx context.Context, input CreateConversationThreadInput) (entity.ConversationThread, error) {
	if err := s.ensureReady(); err != nil {
		return entity.ConversationThread{}, err
	}
	if err := validateCommandMeta(input.Meta); err != nil {
		return entity.ConversationThread{}, err
	}
	if err := validateScope(input.Scope); err != nil {
		return entity.ConversationThread{}, err
	}
	if !input.ThreadKind.Valid() || !input.SourceKind.Valid() || blank(input.CorrelationID) || blank(input.RetentionClass) {
		return entity.ConversationThread{}, errs.ErrInvalidArgument
	}
	fingerprint, err := fingerprintInput(input)
	if err != nil {
		return entity.ConversationThread{}, err
	}
	if thread, ok, err := s.replayThreadCommand(ctx, input.Meta, enum.OperationCreateConversationThread, fingerprint); err != nil || ok {
		return thread, err
	}

	now := s.clock.Now()
	thread := entity.ConversationThread{
		ID:              s.ids.New(),
		Scope:           input.Scope,
		ThreadKind:      input.ThreadKind,
		PrimaryActorRef: strings.TrimSpace(input.PrimaryActorRef),
		SourceKind:      input.SourceKind,
		SourceRef:       strings.TrimSpace(input.SourceRef),
		Status:          enum.ConversationThreadStatusOpen,
		CorrelationID:   strings.TrimSpace(input.CorrelationID),
		RetentionClass:  strings.TrimSpace(input.RetentionClass),
		Version:         1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	result := commandResult(input.Meta, enum.OperationCreateConversationThread, interactionevents.AggregateThread, thread.ID, fingerprint, now)
	event, err := s.outboxEvent(interactionevents.EventThreadCreated, interactionevents.AggregateThread, thread.ID, interactionevents.Payload{
		ThreadID:      thread.ID.String(),
		ScopeType:     string(thread.Scope.Type),
		ScopeRef:      thread.Scope.Ref,
		SourceKind:    string(thread.SourceKind),
		CorrelationID: thread.CorrelationID,
		Version:       thread.Version,
	}, now)
	if err != nil {
		return entity.ConversationThread{}, err
	}
	if err := s.repository.CreateConversationThreadWithResult(ctx, thread, result, event); err != nil {
		return entity.ConversationThread{}, err
	}
	return thread, nil
}

func (s *Service) RecordConversationMessage(ctx context.Context, input RecordConversationMessageInput) (entity.ConversationMessage, error) {
	if err := s.ensureReady(); err != nil {
		return entity.ConversationMessage{}, err
	}
	if err := validateCommandMeta(input.Meta); err != nil {
		return entity.ConversationMessage{}, err
	}
	input, err := normalizeRecordConversationMessageInput(input)
	if err != nil {
		return entity.ConversationMessage{}, err
	}
	fingerprint, err := fingerprintInput(input)
	if err != nil {
		return entity.ConversationMessage{}, err
	}
	if message, ok, err := s.replayMessageCommand(ctx, input.Meta, enum.OperationRecordConversationMessage, fingerprint); err != nil || ok {
		return message, err
	}
	thread, err := s.repository.GetConversationThread(ctx, input.ThreadID)
	if err != nil {
		return entity.ConversationMessage{}, err
	}

	now := s.clock.Now()
	message := entity.ConversationMessage{
		ID:           s.ids.New(),
		ThreadID:     input.ThreadID,
		MessageKind:  input.MessageKind,
		AuthorRef:    input.AuthorRef,
		BodySummary:  input.BodySummary,
		BodyObject:   input.BodyObject,
		BodyDigest:   input.BodyDigest,
		Locale:       input.Locale,
		SafeMetadata: input.SafeMetadata,
		CreatedAt:    now,
	}
	thread.LatestMessageID = &message.ID
	thread.Version++
	thread.UpdatedAt = now
	result := commandResult(input.Meta, enum.OperationRecordConversationMessage, interactionevents.AggregateMessage, message.ID, fingerprint, now)
	event, err := s.outboxEvent(interactionevents.EventMessageRecorded, interactionevents.AggregateMessage, message.ID, interactionevents.Payload{
		MessageID: message.ID.String(),
		ThreadID:  thread.ID.String(),
		ActorRef:  message.AuthorRef,
		Status:    string(thread.Status),
		Version:   thread.Version,
	}, now)
	if err != nil {
		return entity.ConversationMessage{}, err
	}
	if err := s.repository.CreateConversationMessageWithResult(ctx, message, thread, thread.Version-1, result, event); err != nil {
		return entity.ConversationMessage{}, err
	}
	return message, nil
}

func (s *Service) GetConversationThread(ctx context.Context, input GetConversationThreadInput) (entity.ConversationThread, error) {
	if err := s.ensureReady(); err != nil {
		return entity.ConversationThread{}, err
	}
	if input.ThreadID == uuid.Nil {
		return entity.ConversationThread{}, errs.ErrInvalidArgument
	}
	return s.repository.GetConversationThread(ctx, input.ThreadID)
}

func (s *Service) ListConversationMessages(ctx context.Context, input ListConversationMessagesInput) ([]entity.ConversationMessage, value.PageResult, error) {
	if err := s.ensureReady(); err != nil {
		return nil, value.PageResult{}, err
	}
	if input.ThreadID == uuid.Nil {
		return nil, value.PageResult{}, errs.ErrInvalidArgument
	}
	return s.repository.ListConversationMessages(ctx, query.ConversationMessageFilter{ThreadID: input.ThreadID, Page: input.Page})
}

func (s *Service) RequestFeedback(ctx context.Context, input RequestFeedbackInput) (entity.InteractionRequest, error) {
	return s.createInteractionRequest(ctx, enum.InteractionRequestKindFeedback, enum.OperationRequestFeedback, interactionevents.EventFeedbackRequested, input.Meta, input.Request)
}

func (s *Service) RequestApproval(ctx context.Context, input RequestApprovalInput) (entity.InteractionRequest, error) {
	return s.createInteractionRequest(ctx, enum.InteractionRequestKindApproval, enum.OperationRequestApproval, interactionevents.EventApprovalRequested, input.Meta, input.Request)
}

func (s *Service) RequestHumanGate(ctx context.Context, input RequestHumanGateInput) (entity.InteractionRequest, error) {
	return s.createInteractionRequest(ctx, enum.InteractionRequestKindHumanGate, enum.OperationRequestHumanGate, interactionevents.EventHumanGateRequested, input.Meta, input.Request)
}

func (s *Service) RecordInteractionResponse(ctx context.Context, input RecordInteractionResponseInput) (entity.InteractionRequest, entity.InteractionResponse, error) {
	if err := s.ensureReady(); err != nil {
		return entity.InteractionRequest{}, entity.InteractionResponse{}, err
	}
	if err := validateCommandMeta(input.Meta); err != nil {
		return entity.InteractionRequest{}, entity.InteractionResponse{}, err
	}
	input, err := normalizeRecordInteractionResponseInput(input)
	if err != nil {
		return entity.InteractionRequest{}, entity.InteractionResponse{}, err
	}
	fingerprint, err := fingerprintInput(input)
	if err != nil {
		return entity.InteractionRequest{}, entity.InteractionResponse{}, err
	}
	if request, response, ok, err := s.replayResponseCommand(ctx, input.Meta, fingerprint); err != nil || ok {
		return request, response, err
	}

	request, err := s.repository.GetInteractionRequest(ctx, input.RequestID)
	if err != nil {
		return entity.InteractionRequest{}, entity.InteractionResponse{}, err
	}
	if err := validateExpectedVersion(input.Meta, request.Version); err != nil {
		return entity.InteractionRequest{}, entity.InteractionResponse{}, err
	}
	if request.Status.Terminal() {
		return entity.InteractionRequest{}, entity.InteractionResponse{}, errs.ErrConflict
	}
	if !terminalAllowedAction(request.AllowedActions, input.ResponseAction) {
		return entity.InteractionRequest{}, entity.InteractionResponse{}, errs.ErrConflict
	}

	now := s.clock.Now()
	response := entity.InteractionResponse{
		ID:                  s.ids.New(),
		RequestID:           request.ID,
		ResponseAction:      input.ResponseAction,
		RespondedByActorRef: input.RespondedByActorRef,
		ResponseSummary:     input.ResponseSummary,
		ResponseObject:      input.ResponseObject,
		SourceKind:          input.SourceKind,
		SourceRef:           input.SourceRef,
		OwnerDecisionRef:    input.OwnerDecisionRef,
		CreatedAt:           now,
	}
	previousVersion := request.Version
	request.Status = enum.InteractionRequestStatusAnswered
	request.Version++
	request.UpdatedAt = now
	request.ResolvedAt = &now

	result := commandResult(input.Meta, enum.OperationRecordInteractionResponse, aggregateResponse, response.ID, fingerprint, now)
	event, err := s.outboxEvent(interactionevents.EventRequestResponseRecorded, interactionevents.AggregateRequest, request.ID, requestResponseRecordedPayload(request, response), now)
	if err != nil {
		return entity.InteractionRequest{}, entity.InteractionResponse{}, err
	}
	if err := s.repository.CreateInteractionResponseWithResult(ctx, response, request, previousVersion, result, event); err != nil {
		return entity.InteractionRequest{}, entity.InteractionResponse{}, err
	}
	return request, response, nil
}

func (s *Service) CancelInteractionRequest(ctx context.Context, input CancelInteractionRequestInput) (entity.InteractionRequest, error) {
	if err := s.ensureReady(); err != nil {
		return entity.InteractionRequest{}, err
	}
	if err := validateCommandMeta(input.Meta); err != nil {
		return entity.InteractionRequest{}, err
	}
	if input.RequestID == uuid.Nil {
		return entity.InteractionRequest{}, errs.ErrInvalidArgument
	}
	fingerprint, err := fingerprintInput(input)
	if err != nil {
		return entity.InteractionRequest{}, err
	}
	if request, ok, err := s.replayRequestCommand(ctx, input.Meta, enum.OperationCancelInteractionRequest, fingerprint); err != nil || ok {
		return request, err
	}
	request, err := s.repository.GetInteractionRequest(ctx, input.RequestID)
	if err != nil {
		return entity.InteractionRequest{}, err
	}
	if err := validateExpectedVersion(input.Meta, request.Version); err != nil {
		return entity.InteractionRequest{}, err
	}
	if request.Status.Terminal() {
		return entity.InteractionRequest{}, errs.ErrConflict
	}

	now := s.clock.Now()
	previousVersion := request.Version
	request.Status = enum.InteractionRequestStatusCancelled
	request.Version++
	request.UpdatedAt = now
	request.ResolvedAt = &now
	result := commandResult(input.Meta, enum.OperationCancelInteractionRequest, interactionevents.AggregateRequest, request.ID, fingerprint, now)
	event, err := s.outboxEvent(interactionevents.EventRequestCancelled, interactionevents.AggregateRequest, request.ID, interactionevents.Payload{
		RequestID:      request.ID.String(),
		RequestKind:    string(request.RequestKind),
		CancelledByRef: input.Meta.Actor.Ref(),
		Status:         string(request.Status),
		Version:        request.Version,
	}, now)
	if err != nil {
		return entity.InteractionRequest{}, err
	}
	if err := s.repository.UpdateInteractionRequestWithResult(ctx, request, previousVersion, result, event); err != nil {
		return entity.InteractionRequest{}, err
	}
	return request, nil
}

func (s *Service) ExpireInteractionRequests(ctx context.Context, input ExpireInteractionRequestsInput) (ExpireInteractionRequestsResult, error) {
	if err := s.ensureReady(); err != nil {
		return ExpireInteractionRequestsResult{}, err
	}
	if err := validateCommandMeta(input.Meta); err != nil {
		return ExpireInteractionRequestsResult{}, err
	}
	input, err := normalizeExpireInteractionRequestsInput(input)
	if err != nil {
		return ExpireInteractionRequestsResult{}, err
	}
	fingerprint, err := fingerprintInput(input)
	if err != nil {
		return ExpireInteractionRequestsResult{}, err
	}
	if result, ok, err := s.replayExpireCommand(ctx, input.Meta, fingerprint); err != nil || ok {
		return result, err
	}

	deadlineBefore := s.clock.Now().UTC()
	if input.DeadlineBefore != nil {
		deadlineBefore = *input.DeadlineBefore
	}
	candidates, err := s.repository.ListExpirableInteractionRequests(ctx, input.Scope, deadlineBefore, input.Limit)
	if err != nil {
		return ExpireInteractionRequestsResult{}, err
	}
	now := s.clock.Now()
	previousVersions := make(map[uuid.UUID]int64, len(candidates))
	events := make([]entity.OutboxEvent, 0, len(candidates))
	expiredIDs := make([]uuid.UUID, 0, len(candidates))
	for index := range candidates {
		request := &candidates[index]
		previousVersions[request.ID] = request.Version
		request.Status = enum.InteractionRequestStatusExpired
		request.Version++
		request.UpdatedAt = now
		request.ResolvedAt = &now
		expiredIDs = append(expiredIDs, request.ID)
		event, err := s.outboxEvent(interactionevents.EventRequestExpired, interactionevents.AggregateRequest, request.ID, interactionevents.Payload{
			RequestID:   request.ID.String(),
			RequestKind: string(request.RequestKind),
			DeadlineAt:  timeProto(request.DeadlineAt),
			Status:      string(request.Status),
			Version:     request.Version,
		}, now)
		if err != nil {
			return ExpireInteractionRequestsResult{}, err
		}
		events = append(events, event)
	}
	payload, err := expireResultPayload(expiredIDs)
	if err != nil {
		return ExpireInteractionRequestsResult{}, err
	}
	result := commandResultWithPayload(input.Meta, enum.OperationExpireInteractionRequests, interactionevents.AggregateRequest, uuid.Nil, fingerprint, payload, now)
	if err := s.repository.UpdateInteractionRequestsWithResult(ctx, candidates, previousVersions, result, events); err != nil {
		return ExpireInteractionRequestsResult{}, err
	}
	return ExpireInteractionRequestsResult{ExpiredRequestIDs: expiredIDs}, nil
}

func (s *Service) GetInteractionRequest(ctx context.Context, input GetInteractionRequestInput) (entity.InteractionRequest, error) {
	if err := s.ensureReady(); err != nil {
		return entity.InteractionRequest{}, err
	}
	if input.RequestID == uuid.Nil {
		return entity.InteractionRequest{}, errs.ErrInvalidArgument
	}
	return s.repository.GetInteractionRequest(ctx, input.RequestID)
}

func (s *Service) ListInteractionRequests(ctx context.Context, input ListInteractionRequestsInput) ([]entity.InteractionRequest, value.PageResult, error) {
	if err := s.ensureReady(); err != nil {
		return nil, value.PageResult{}, err
	}
	if err := validateScope(input.Scope); err != nil {
		return nil, value.PageResult{}, err
	}
	if input.RequestKind != "" && !input.RequestKind.Valid() {
		return nil, value.PageResult{}, errs.ErrInvalidArgument
	}
	if input.Status != "" && !input.Status.Valid() {
		return nil, value.PageResult{}, errs.ErrInvalidArgument
	}
	if input.SourceOwnerKind != "" && !input.SourceOwnerKind.Valid() {
		return nil, value.PageResult{}, errs.ErrInvalidArgument
	}
	return s.repository.ListInteractionRequests(ctx, query.InteractionRequestFilter{
		Scope:           input.Scope,
		RequestKind:     input.RequestKind,
		Status:          input.Status,
		SourceOwnerKind: input.SourceOwnerKind,
		SourceOwnerRef:  strings.TrimSpace(input.SourceOwnerRef),
		DeadlineBefore:  input.DeadlineBefore,
		Page:            input.Page,
	})
}

func (s *Service) RequestNotification(ctx context.Context, input RequestNotificationInput) (entity.Notification, error) {
	if err := s.ensureReady(); err != nil {
		return entity.Notification{}, err
	}
	if err := validateCommandMeta(input.Meta); err != nil {
		return entity.Notification{}, err
	}
	input, err := normalizeRequestNotificationInput(input, s.clock.Now())
	if err != nil {
		return entity.Notification{}, err
	}
	fingerprint, err := fingerprintInput(input)
	if err != nil {
		return entity.Notification{}, err
	}
	if notification, ok, err := s.replayNotificationCommand(ctx, input.Meta, enum.OperationRequestNotification, fingerprint); err != nil || ok {
		return notification, err
	}
	if input.RequestID != uuid.Nil {
		if _, err := s.repository.GetInteractionRequest(ctx, input.RequestID); err != nil {
			return entity.Notification{}, err
		}
	}
	if input.SubscriptionID != uuid.Nil {
		if _, err := s.repository.GetSubscription(ctx, input.SubscriptionID); err != nil {
			return entity.Notification{}, err
		}
	}

	now := s.clock.Now()
	notification := entity.Notification{
		ID:                    s.ids.New(),
		Scope:                 input.Scope,
		NotificationKind:      input.NotificationKind,
		RequestID:             optionalUUID(input.RequestID),
		SubscriptionID:        optionalUUID(input.SubscriptionID),
		RecipientRefs:         input.RecipientRefs,
		MessageTemplateRef:    input.MessageTemplateRef,
		MessageTitle:          input.MessageTitle,
		MessageSummary:        input.MessageSummary,
		BodyPreview:           input.BodyPreview,
		Priority:              input.Priority,
		Status:                enum.NotificationStatusCreated,
		SourceOwner:           input.SourceOwner,
		Ingress:               input.Ingress,
		ContextRefs:           input.ContextRefs,
		ChannelHintRefs:       input.ChannelHintRefs,
		NotificationPolicyRef: input.NotificationPolicyRef,
		CreatedAt:             now,
		UpdatedAt:             now,
		ExpiresAt:             input.ExpiresAt,
	}
	result := commandResult(input.Meta, enum.OperationRequestNotification, interactionevents.AggregateNotification, notification.ID, fingerprint, now)
	event, err := s.outboxEvent(interactionevents.EventNotificationRequested, interactionevents.AggregateNotification, notification.ID, notificationEventPayload(notification), now)
	if err != nil {
		return entity.Notification{}, err
	}
	if err := s.repository.CreateNotificationWithResult(ctx, notification, result, event); err != nil {
		return entity.Notification{}, err
	}
	return notification, nil
}

func (s *Service) UpsertSubscription(ctx context.Context, input UpsertSubscriptionInput) (entity.Subscription, error) {
	if err := s.ensureReady(); err != nil {
		return entity.Subscription{}, err
	}
	if err := validateCommandMeta(input.Meta); err != nil {
		return entity.Subscription{}, err
	}
	input, err := normalizeUpsertSubscriptionInput(input)
	if err != nil {
		return entity.Subscription{}, err
	}
	fingerprint, err := fingerprintInput(input)
	if err != nil {
		return entity.Subscription{}, err
	}
	if subscription, ok, err := s.replaySubscriptionCommand(ctx, input.Meta, enum.OperationUpsertSubscription, fingerprint); err != nil || ok {
		return subscription, err
	}

	now := s.clock.Now()
	subscription := entity.Subscription{
		ID:                      input.SubscriptionID,
		Scope:                   input.Scope,
		SubscriberRef:           input.SubscriberRef,
		EventFilterJSON:         input.EventFilterJSON,
		DeliveryPreferencesJSON: input.DeliveryPreferencesJSON,
		Status:                  input.Status,
		SourceOwner:             input.SourceOwner,
		ChannelHintRefs:         input.ChannelHintRefs,
		SubscriptionPolicyRef:   input.SubscriptionPolicyRef,
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	resultAggregateID := subscription.ID
	if subscription.ID == uuid.Nil {
		subscription.ID = s.ids.New()
		resultAggregateID = subscription.ID
		subscription.Version = 1
		result := commandResult(input.Meta, enum.OperationUpsertSubscription, interactionevents.AggregateSubscription, resultAggregateID, fingerprint, now)
		event, err := s.outboxEvent(interactionevents.EventSubscriptionUpdated, interactionevents.AggregateSubscription, subscription.ID, subscriptionEventPayload(subscription), now)
		if err != nil {
			return entity.Subscription{}, err
		}
		if err := s.repository.CreateSubscriptionWithResult(ctx, subscription, result, event); err != nil {
			return entity.Subscription{}, err
		}
		return subscription, nil
	}

	current, err := s.repository.GetSubscription(ctx, subscription.ID)
	if err != nil {
		return entity.Subscription{}, err
	}
	if err := validateExpectedVersion(input.Meta, current.Version); err != nil {
		return entity.Subscription{}, err
	}
	previousVersion := current.Version
	subscription.Version = current.Version + 1
	subscription.CreatedAt = current.CreatedAt
	subscription.UpdatedAt = now
	result := commandResult(input.Meta, enum.OperationUpsertSubscription, interactionevents.AggregateSubscription, resultAggregateID, fingerprint, now)
	event, err := s.outboxEvent(interactionevents.EventSubscriptionUpdated, interactionevents.AggregateSubscription, subscription.ID, subscriptionEventPayload(subscription), now)
	if err != nil {
		return entity.Subscription{}, err
	}
	if err := s.repository.UpdateSubscriptionWithResult(ctx, subscription, previousVersion, result, event); err != nil {
		return entity.Subscription{}, err
	}
	return subscription, nil
}

func (s *Service) DisableSubscription(ctx context.Context, input DisableSubscriptionInput) (entity.Subscription, error) {
	if err := s.ensureReady(); err != nil {
		return entity.Subscription{}, err
	}
	if err := validateCommandMeta(input.Meta); err != nil {
		return entity.Subscription{}, err
	}
	if input.SubscriptionID == uuid.Nil {
		return entity.Subscription{}, errs.ErrInvalidArgument
	}
	fingerprint, err := fingerprintInput(input)
	if err != nil {
		return entity.Subscription{}, err
	}
	if subscription, ok, err := s.replaySubscriptionCommand(ctx, input.Meta, enum.OperationDisableSubscription, fingerprint); err != nil || ok {
		return subscription, err
	}
	subscription, err := s.repository.GetSubscription(ctx, input.SubscriptionID)
	if err != nil {
		return entity.Subscription{}, err
	}
	if err := validateExpectedVersion(input.Meta, subscription.Version); err != nil {
		return entity.Subscription{}, err
	}
	if subscription.Status == enum.SubscriptionStatusDisabled {
		return entity.Subscription{}, errs.ErrConflict
	}

	now := s.clock.Now()
	previousVersion := subscription.Version
	subscription.Status = enum.SubscriptionStatusDisabled
	subscription.Version++
	subscription.UpdatedAt = now
	result := commandResult(input.Meta, enum.OperationDisableSubscription, interactionevents.AggregateSubscription, subscription.ID, fingerprint, now)
	event, err := s.outboxEvent(interactionevents.EventSubscriptionUpdated, interactionevents.AggregateSubscription, subscription.ID, subscriptionEventPayload(subscription), now)
	if err != nil {
		return entity.Subscription{}, err
	}
	if err := s.repository.UpdateSubscriptionWithResult(ctx, subscription, previousVersion, result, event); err != nil {
		return entity.Subscription{}, err
	}
	return subscription, nil
}

func (s *Service) ListSubscriptions(ctx context.Context, input ListSubscriptionsInput) ([]entity.Subscription, value.PageResult, error) {
	if err := s.ensureReady(); err != nil {
		return nil, value.PageResult{}, err
	}
	if err := validateScope(input.Scope); err != nil {
		return nil, value.PageResult{}, err
	}
	input.SubscriberRef = strings.TrimSpace(input.SubscriberRef)
	if input.Status != "" && !input.Status.Valid() {
		return nil, value.PageResult{}, errs.ErrInvalidArgument
	}
	return s.repository.ListSubscriptions(ctx, query.SubscriptionFilter{
		Scope:         input.Scope,
		SubscriberRef: input.SubscriberRef,
		Status:        input.Status,
		Page:          input.Page,
	})
}

func (s *Service) PlanDelivery(ctx context.Context, input PlanDeliveryInput) (entity.DeliveryAttempt, error) {
	if err := s.ensureReady(); err != nil {
		return entity.DeliveryAttempt{}, err
	}
	if err := validateCommandMeta(input.Meta); err != nil {
		return entity.DeliveryAttempt{}, err
	}
	input, targetContext, err := s.normalizePlanDeliveryInput(ctx, input)
	if err != nil {
		return entity.DeliveryAttempt{}, err
	}
	fingerprint, err := fingerprintInput(input)
	if err != nil {
		return entity.DeliveryAttempt{}, err
	}
	if attempt, ok, err := replayAggregate(ctx, s, input.Meta, enum.OperationPlanDelivery, fingerprint, s.repository.GetDeliveryAttempt); err != nil || ok {
		return attempt, err
	}

	route, err := s.deliveryRoute(ctx, input.RouteID, targetContext.Scope)
	if err != nil {
		return entity.DeliveryAttempt{}, err
	}
	existing, err := s.repository.ListDeliveryAttempts(ctx, query.DeliveryAttemptFilter{Target: input.Target, Limit: maxDeliveryStatusAttempts})
	if err != nil {
		return entity.DeliveryAttempt{}, err
	}

	now := s.clock.Now()
	attemptID := s.ids.New()
	attemptNumber := nextAttemptNumber(existing)
	deliveryID := attemptID.String()
	deliveryCommandRef := "interaction.delivery_command:" + deliveryID
	callbackRef := "interaction.callback:" + deliveryID
	attempt := entity.DeliveryAttempt{
		ID:                     attemptID,
		Target:                 input.Target,
		RouteID:                route.ID,
		DeliveryID:             deliveryID,
		DeliveryKind:           targetContext.DeliveryKind,
		Status:                 enum.DeliveryAttemptStatusQueued,
		AttemptNumber:          attemptNumber,
		ChannelCapabilityRef:   route.ChannelCapabilityRef,
		PackageInstallationRef: route.PackageInstallationRef,
		PackageVersionRef:      route.PackageVersionRef,
		DeliveryCommandRef:     deliveryCommandRef,
		CallbackRef:            callbackRef,
		CallbackRouteRef:       route.CallbackRouteRef,
		RuntimeRef:             route.RuntimeRef,
		RoutingPolicyRef:       route.RoutingPolicyRef,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	attempt.PayloadDigest = deliveryPayloadDigest(targetContext, route, attempt, input.CorrelationID)
	result := commandResult(input.Meta, enum.OperationPlanDelivery, interactionevents.AggregateDelivery, attempt.ID, fingerprint, now)
	event, err := s.outboxEvent(interactionevents.EventDeliveryRequested, interactionevents.AggregateDelivery, attempt.ID, deliveryRequestedPayload(attempt, targetContext, input.CorrelationID), now)
	if err != nil {
		return entity.DeliveryAttempt{}, err
	}
	if err := s.repository.CreateDeliveryAttemptWithResult(ctx, attempt, result, event); err != nil {
		return entity.DeliveryAttempt{}, err
	}
	return attempt, nil
}

func (s *Service) RecordDeliveryResult(ctx context.Context, input RecordDeliveryResultInput) (entity.DeliveryAttempt, error) {
	if err := s.ensureReady(); err != nil {
		return entity.DeliveryAttempt{}, err
	}
	if err := validateCommandMeta(input.Meta); err != nil {
		return entity.DeliveryAttempt{}, err
	}
	input, err := normalizeRecordDeliveryResultInput(input)
	if err != nil {
		return entity.DeliveryAttempt{}, err
	}
	fingerprint, err := fingerprintInput(input)
	if err != nil {
		return entity.DeliveryAttempt{}, err
	}
	if attempt, ok, err := replayAggregate(ctx, s, input.Meta, enum.OperationRecordDeliveryResult, fingerprint, s.repository.GetDeliveryAttempt); err != nil || ok {
		return attempt, err
	}
	resultFingerprint, err := deliveryResultFingerprint(input.Result)
	if err != nil {
		return entity.DeliveryAttempt{}, err
	}

	attempt, err := s.repository.GetDeliveryAttemptByDeliveryID(ctx, input.Result.DeliveryID)
	if err != nil {
		return entity.DeliveryAttempt{}, err
	}
	if input.Result.DeliveryCommandRef != "" && input.Result.DeliveryCommandRef != attempt.DeliveryCommandRef {
		return entity.DeliveryAttempt{}, errs.ErrConflict
	}
	if input.Result.RuntimeRef != "" && attempt.RuntimeRef != "" && input.Result.RuntimeRef != attempt.RuntimeRef {
		return entity.DeliveryAttempt{}, errs.ErrConflict
	}
	if replayed, ok, err := replayDeliveryResultByFingerprint(attempt, resultFingerprint); err != nil || ok {
		return replayed, err
	}
	if attempt.Status.Terminal() {
		return entity.DeliveryAttempt{}, errs.ErrConflict
	}
	now := s.clock.Now()
	updated, eventType, err := deliveryAttemptWithResult(attempt, input.Result, now)
	if err != nil {
		return entity.DeliveryAttempt{}, err
	}
	updated.ResultFingerprint = resultFingerprint
	result := commandResult(input.Meta, enum.OperationRecordDeliveryResult, interactionevents.AggregateDelivery, updated.ID, fingerprint, now)
	event, err := s.outboxEvent(eventType, interactionevents.AggregateDelivery, updated.ID, deliveryResultPayload(updated), now)
	if err != nil {
		return entity.DeliveryAttempt{}, err
	}
	if err := s.repository.UpdateDeliveryAttemptWithResult(ctx, updated, result, event); err != nil {
		if errors.Is(err, errs.ErrConflict) {
			return s.replayDeliveryResultConflict(ctx, input.Result.DeliveryID, resultFingerprint)
		}
		return entity.DeliveryAttempt{}, err
	}
	return updated, nil
}

func (s *Service) RecordChannelCallback(ctx context.Context, input RecordChannelCallbackInput) (ChannelCallbackResult, error) {
	if err := s.ensureReady(); err != nil {
		return ChannelCallbackResult{}, err
	}
	if err := validateCommandMeta(input.Meta); err != nil {
		return ChannelCallbackResult{}, err
	}
	input, err := normalizeRecordChannelCallbackInput(input)
	if err != nil {
		return ChannelCallbackResult{}, err
	}
	fingerprint, err := channelCallbackRequestFingerprint(input)
	if err != nil {
		return ChannelCallbackResult{}, err
	}
	if callback, ok, err := replayAggregate(ctx, s, input.Meta, enum.OperationRecordChannelCallback, fingerprint, s.repository.GetChannelCallback); err != nil || ok {
		if err != nil {
			return ChannelCallbackResult{}, err
		}
		return s.channelCallbackResult(ctx, callback)
	}
	callbackFingerprint, err := callbackEnvelopeFingerprint(input.Callback)
	if err != nil {
		return ChannelCallbackResult{}, err
	}
	if existing, err := s.repository.GetChannelCallbackByCallbackID(ctx, input.Callback.CallbackID); err == nil {
		if existing.CallbackFingerprint == callbackFingerprint {
			return s.channelCallbackResult(ctx, existing)
		}
		return ChannelCallbackResult{}, errs.ErrConflict
	} else if !errors.Is(err, errs.ErrNotFound) {
		return ChannelCallbackResult{}, err
	}

	resolved, err := s.resolveChannelCallback(ctx, input.Callback)
	if err != nil {
		return ChannelCallbackResult{}, err
	}
	now := s.clock.Now()
	callback := entity.ChannelCallback{
		ID:                  s.ids.New(),
		CallbackID:          input.Callback.CallbackID,
		DeliveryID:          input.Callback.DeliveryID,
		DeliveryAttemptID:   resolved.deliveryAttemptID,
		RequestID:           resolved.requestID,
		SourceRouteID:       resolved.sourceRouteID,
		ActorRef:            input.Callback.ActorRef,
		Action:              input.Callback.Action,
		CallbackSummary:     input.Callback.AnswerSummary,
		CallbackObject:      input.Callback.AnswerObject,
		SignatureStatus:     input.Callback.SignatureStatus,
		ProcessingStatus:    enum.CallbackProcessingStatusAccepted,
		ReceivedAt:          input.Callback.ReceivedAt,
		CreatedAt:           now,
		CallbackRouteRef:    resolved.callbackRouteRef,
		GatewayRef:          input.Callback.GatewayRef,
		CorrelationID:       input.Callback.CorrelationID,
		CallbackFingerprint: callbackFingerprint,
	}
	if !input.Callback.SignatureStatus.Accepted() {
		callback.ProcessingStatus = enum.CallbackProcessingStatusRejected
		callback.ErrorCode = callbackErrorRejected
	}
	var response *entity.InteractionResponse
	var updatedRequest entity.InteractionRequest
	var previousRequestVersion int64
	if callback.ProcessingStatus == enum.CallbackProcessingStatusAccepted && resolved.request != nil {
		var rejectionCode string
		response, updatedRequest, previousRequestVersion, rejectionCode = s.channelCallbackResponse(callback, *resolved.request, now)
		if rejectionCode != "" {
			callback.ProcessingStatus = enum.CallbackProcessingStatusRejected
			callback.ErrorCode = rejectionCode
			response = nil
		}
	}
	result := commandResult(input.Meta, enum.OperationRecordChannelCallback, interactionevents.AggregateCallback, callback.ID, fingerprint, now)
	event, err := s.outboxEvent(interactionevents.EventCallbackReceived, interactionevents.AggregateCallback, callback.ID, callbackReceivedPayload(callback), now)
	if err != nil {
		return ChannelCallbackResult{}, err
	}
	if response != nil {
		responseEvent, err := s.outboxEvent(interactionevents.EventRequestResponseRecorded, interactionevents.AggregateRequest, updatedRequest.ID, requestResponseRecordedPayload(updatedRequest, *response), now)
		if err != nil {
			return ChannelCallbackResult{}, err
		}
		if err := s.repository.CreateChannelCallbackResponseWithResult(ctx, callback, *response, updatedRequest, previousRequestVersion, result, []entity.OutboxEvent{event, responseEvent}); err != nil {
			if errors.Is(err, errs.ErrAlreadyExists) {
				return s.replayExistingChannelCallback(ctx, input.Callback.CallbackID, callbackFingerprint)
			}
			return ChannelCallbackResult{}, err
		}
		return ChannelCallbackResult{Callback: callback, Response: response}, nil
	}
	if err := s.repository.CreateChannelCallbackWithResult(ctx, callback, result, event); err != nil {
		if errors.Is(err, errs.ErrAlreadyExists) {
			return s.replayExistingChannelCallback(ctx, input.Callback.CallbackID, callbackFingerprint)
		}
		return ChannelCallbackResult{}, err
	}
	return ChannelCallbackResult{Callback: callback}, nil
}

func (s *Service) GetDeliveryStatus(ctx context.Context, input GetDeliveryStatusInput) (DeliveryStatusResult, error) {
	if err := s.ensureReady(); err != nil {
		return DeliveryStatusResult{}, err
	}
	input.DeliveryID = strings.TrimSpace(input.DeliveryID)
	if input.Target.ID == uuid.Nil && input.DeliveryID == "" {
		return DeliveryStatusResult{}, errs.ErrInvalidArgument
	}
	var attempts []entity.DeliveryAttempt
	if input.DeliveryID != "" {
		attempt, err := s.repository.GetDeliveryAttemptByDeliveryID(ctx, input.DeliveryID)
		if err != nil {
			return DeliveryStatusResult{}, err
		}
		if input.Target.Valid() && input.Target != attempt.Target {
			return DeliveryStatusResult{}, errs.ErrConflict
		}
		input.Target = attempt.Target
		attempts = []entity.DeliveryAttempt{attempt}
	} else {
		if !input.Target.Valid() {
			return DeliveryStatusResult{}, errs.ErrInvalidArgument
		}
		var err error
		attempts, err = s.repository.ListDeliveryAttempts(ctx, query.DeliveryAttemptFilter{Target: input.Target, Limit: maxDeliveryStatusAttempts})
		if err != nil {
			return DeliveryStatusResult{}, err
		}
	}

	result := DeliveryStatusResult{DeliveryAttempts: attempts}
	if latest, err := s.repository.GetLatestChannelCallback(ctx, query.ChannelCallbackFilter{
		DeliveryAttemptIDs: deliveryAttemptIDs(attempts),
		DeliveryID:         input.DeliveryID,
		RequestID:          deliveryStatusRequestID(input.Target),
	}); err == nil {
		result.LatestCallback = &latest
	} else if !errors.Is(err, errs.ErrNotFound) {
		return DeliveryStatusResult{}, err
	}
	switch input.Target.Kind {
	case value.DeliveryTargetKindRequest:
		request, err := s.repository.GetInteractionRequest(ctx, input.Target.ID)
		if err != nil {
			return DeliveryStatusResult{}, err
		}
		result.Request = &request
	case value.DeliveryTargetKindNotification:
		notification, err := s.repository.GetNotification(ctx, input.Target.ID)
		if err != nil {
			return DeliveryStatusResult{}, err
		}
		result.Notification = &notification
	default:
		return DeliveryStatusResult{}, errs.ErrInvalidArgument
	}
	return result, nil
}

func (s *Service) createInteractionRequest(ctx context.Context, kind enum.InteractionRequestKind, operation enum.Operation, eventType string, meta value.CommandMeta, draft InteractionRequestDraftInput) (entity.InteractionRequest, error) {
	if err := s.ensureReady(); err != nil {
		return entity.InteractionRequest{}, err
	}
	if err := validateCommandMeta(meta); err != nil {
		return entity.InteractionRequest{}, err
	}
	draft, err := normalizeInteractionRequestDraft(kind, draft, s.clock.Now())
	if err != nil {
		return entity.InteractionRequest{}, err
	}
	input := createInteractionRequestFingerprint{Meta: meta, Kind: kind, Request: draft}
	fingerprint, err := fingerprintInput(input)
	if err != nil {
		return entity.InteractionRequest{}, err
	}
	if request, ok, err := s.replayRequestCommand(ctx, meta, operation, fingerprint); err != nil || ok {
		return request, err
	}

	now := s.clock.Now()
	request := entity.InteractionRequest{
		ID:                s.ids.New(),
		RequestKind:       kind,
		Scope:             draft.Scope,
		ThreadID:          optionalUUID(draft.ThreadID),
		SourceOwner:       draft.SourceOwner,
		Ingress:           draft.Ingress,
		DecisionOwner:     draft.DecisionOwner,
		TargetRefs:        draft.TargetRefs,
		ContextRefs:       draft.ContextRefs,
		PromptSummary:     draft.PromptSummary,
		PromptObject:      draft.PromptObject,
		AllowedActions:    draft.AllowedActions,
		RiskClass:         draft.RiskClass,
		Status:            enum.InteractionRequestStatusWaiting,
		DeadlineAt:        draft.DeadlineAt,
		ReminderPolicyRef: draft.ReminderPolicyRef,
		Version:           1,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	result := commandResult(meta, operation, interactionevents.AggregateRequest, request.ID, fingerprint, now)
	event, err := s.outboxEvent(eventType, interactionevents.AggregateRequest, request.ID, requestEventPayload(request), now)
	if err != nil {
		return entity.InteractionRequest{}, err
	}
	if err := s.repository.CreateInteractionRequestWithResult(ctx, request, result, event); err != nil {
		return entity.InteractionRequest{}, err
	}
	return request, nil
}

func (s *Service) ensureReady() error {
	if s == nil || s.repository == nil || !s.repository.Ready() {
		return errs.ErrUnavailable
	}
	return nil
}

func (s *Service) replayThreadCommand(ctx context.Context, meta value.CommandMeta, operation enum.Operation, fingerprint string) (entity.ConversationThread, bool, error) {
	return replayAggregate(ctx, s, meta, operation, fingerprint, s.repository.GetConversationThread)
}

func (s *Service) replayMessageCommand(ctx context.Context, meta value.CommandMeta, operation enum.Operation, fingerprint string) (entity.ConversationMessage, bool, error) {
	return replayAggregate(ctx, s, meta, operation, fingerprint, s.repository.GetConversationMessage)
}

func (s *Service) replayRequestCommand(ctx context.Context, meta value.CommandMeta, operation enum.Operation, fingerprint string) (entity.InteractionRequest, bool, error) {
	return replayAggregate(ctx, s, meta, operation, fingerprint, s.repository.GetInteractionRequest)
}

func (s *Service) replayNotificationCommand(ctx context.Context, meta value.CommandMeta, operation enum.Operation, fingerprint string) (entity.Notification, bool, error) {
	return replayAggregate(ctx, s, meta, operation, fingerprint, s.repository.GetNotification)
}

func (s *Service) replaySubscriptionCommand(ctx context.Context, meta value.CommandMeta, operation enum.Operation, fingerprint string) (entity.Subscription, bool, error) {
	return replayAggregate(ctx, s, meta, operation, fingerprint, s.repository.GetSubscription)
}

func (s *Service) replayResponseCommand(ctx context.Context, meta value.CommandMeta, fingerprint string) (entity.InteractionRequest, entity.InteractionResponse, bool, error) {
	result, ok, err := s.replayCommand(ctx, meta, enum.OperationRecordInteractionResponse, fingerprint)
	if err != nil || !ok {
		return entity.InteractionRequest{}, entity.InteractionResponse{}, ok, err
	}
	response, err := s.repository.GetInteractionResponse(ctx, result.AggregateID)
	if err != nil {
		return entity.InteractionRequest{}, entity.InteractionResponse{}, true, err
	}
	request, err := s.repository.GetInteractionRequest(ctx, response.RequestID)
	return request, response, true, err
}

func (s *Service) replayExpireCommand(ctx context.Context, meta value.CommandMeta, fingerprint string) (ExpireInteractionRequestsResult, bool, error) {
	result, ok, err := s.replayCommand(ctx, meta, enum.OperationExpireInteractionRequests, fingerprint)
	if err != nil || !ok {
		return ExpireInteractionRequestsResult{}, ok, err
	}
	ids, err := parseExpireResultPayload(result.ResultPayload)
	return ExpireInteractionRequestsResult{ExpiredRequestIDs: ids}, true, err
}

func replayAggregate[T any](ctx context.Context, service *Service, meta value.CommandMeta, operation enum.Operation, fingerprint string, fetch func(context.Context, uuid.UUID) (T, error)) (T, bool, error) {
	result, ok, err := service.replayCommand(ctx, meta, operation, fingerprint)
	if err != nil || !ok {
		var zero T
		return zero, ok, err
	}
	value, err := fetch(ctx, result.AggregateID)
	return value, true, err
}

func (s *Service) replayCommand(ctx context.Context, meta value.CommandMeta, operation enum.Operation, fingerprint string) (entity.CommandResult, bool, error) {
	identity := query.CommandIdentity{
		CommandID:      meta.CommandID,
		IdempotencyKey: strings.TrimSpace(meta.IdempotencyKey),
		ActorRef:       meta.Actor.Ref(),
		Operation:      operation,
	}
	result, err := s.repository.GetCommandResult(ctx, identity)
	if errors.Is(err, errs.ErrNotFound) {
		return entity.CommandResult{}, false, nil
	}
	if err != nil {
		return entity.CommandResult{}, false, err
	}
	if result.RequestFingerprint != fingerprint {
		return entity.CommandResult{}, true, errs.ErrConflict
	}
	return result, true, nil
}

func validateCommandMeta(meta value.CommandMeta) error {
	if meta.CommandID == uuid.Nil && blank(meta.IdempotencyKey) {
		return errs.ErrInvalidArgument
	}
	if blank(meta.Actor.Type) || blank(meta.Actor.ID) {
		return errs.ErrInvalidArgument
	}
	return nil
}

func validateScope(scope value.ScopeRef) error {
	if !scope.Type.Valid() || blank(scope.Ref) {
		return errs.ErrInvalidArgument
	}
	return nil
}

func normalizeInteractionRequestDraft(kind enum.InteractionRequestKind, input InteractionRequestDraftInput, now time.Time) (InteractionRequestDraftInput, error) {
	if !kind.Valid() {
		return InteractionRequestDraftInput{}, errs.ErrInvalidArgument
	}
	if err := validateScope(input.Scope); err != nil {
		return InteractionRequestDraftInput{}, err
	}
	input.SourceOwner.Ref = strings.TrimSpace(input.SourceOwner.Ref)
	if !input.SourceOwner.Kind.Valid() {
		return InteractionRequestDraftInput{}, errs.ErrInvalidArgument
	}
	input.Ingress.Ref = strings.TrimSpace(input.Ingress.Ref)
	if !input.Ingress.Kind.Valid() {
		return InteractionRequestDraftInput{}, errs.ErrInvalidArgument
	}
	input.DecisionOwner.OwnerRequestRef = strings.TrimSpace(input.DecisionOwner.OwnerRequestRef)
	input.DecisionOwner.OwnerDecisionRef = strings.TrimSpace(input.DecisionOwner.OwnerDecisionRef)
	if kind != enum.InteractionRequestKindFeedback {
		if !input.DecisionOwner.Kind.Valid() || blank(input.DecisionOwner.OwnerRequestRef) {
			return InteractionRequestDraftInput{}, errs.ErrInvalidArgument
		}
	} else if input.DecisionOwner.Kind != "" && !input.DecisionOwner.Kind.Valid() {
		return InteractionRequestDraftInput{}, errs.ErrInvalidArgument
	}
	if input.RiskClass != "" && !input.RiskClass.Valid() {
		return InteractionRequestDraftInput{}, errs.ErrInvalidArgument
	}

	targets, err := normalizeActorRefs(input.TargetRefs)
	if err != nil {
		return InteractionRequestDraftInput{}, err
	}
	input.TargetRefs = targets
	contextRefs, err := normalizeExternalRefs(input.ContextRefs)
	if err != nil {
		return InteractionRequestDraftInput{}, err
	}
	input.ContextRefs = contextRefs
	input.PromptSummary = strings.TrimSpace(input.PromptSummary)
	input.PromptObject = normalizeObjectRef(input.PromptObject)
	input.ReminderPolicyRef = strings.TrimSpace(input.ReminderPolicyRef)
	if err := validateRequestPrompt(input); err != nil {
		return InteractionRequestDraftInput{}, err
	}
	actions, err := normalizeInteractionActions(input.AllowedActions)
	if err != nil {
		return InteractionRequestDraftInput{}, err
	}
	input.AllowedActions = actions
	if input.DeadlineAt != nil {
		deadline := input.DeadlineAt.UTC()
		if !deadline.After(now) {
			return InteractionRequestDraftInput{}, errs.ErrInvalidArgument
		}
		input.DeadlineAt = &deadline
	}
	return input, nil
}

func validateRequestPrompt(input InteractionRequestDraftInput) error {
	if blank(input.PromptSummary) || utf8.RuneCountInString(input.PromptSummary) > maxMessageBodySummaryRunes {
		return errs.ErrInvalidArgument
	}
	return validateObjectRef(input.PromptObject)
}

func normalizeRecordInteractionResponseInput(input RecordInteractionResponseInput) (RecordInteractionResponseInput, error) {
	input.RespondedByActorRef = strings.TrimSpace(input.RespondedByActorRef)
	input.ResponseSummary = strings.TrimSpace(input.ResponseSummary)
	input.ResponseObject = normalizeObjectRef(input.ResponseObject)
	input.SourceRef = strings.TrimSpace(input.SourceRef)
	input.OwnerDecisionRef = strings.TrimSpace(input.OwnerDecisionRef)
	if input.RequestID == uuid.Nil || !input.ResponseAction.Valid() || blank(input.RespondedByActorRef) || !input.SourceKind.Valid() {
		return RecordInteractionResponseInput{}, errs.ErrInvalidArgument
	}
	if input.ResponseAction == enum.InteractionResponseActionAnswer || input.ResponseAction == enum.InteractionResponseActionCustom {
		if blank(input.ResponseSummary) && !hasObjectRef(input.ResponseObject) {
			return RecordInteractionResponseInput{}, errs.ErrInvalidArgument
		}
	}
	if utf8.RuneCountInString(input.ResponseSummary) > maxMessageBodySummaryRunes {
		return RecordInteractionResponseInput{}, errs.ErrInvalidArgument
	}
	if err := validateObjectRef(input.ResponseObject); err != nil {
		return RecordInteractionResponseInput{}, err
	}
	return input, nil
}

func normalizeExpireInteractionRequestsInput(input ExpireInteractionRequestsInput) (ExpireInteractionRequestsInput, error) {
	if err := validateScope(input.Scope); err != nil {
		return ExpireInteractionRequestsInput{}, err
	}
	if input.DeadlineBefore != nil {
		deadline := input.DeadlineBefore.UTC()
		input.DeadlineBefore = &deadline
	}
	if input.Limit <= 0 {
		input.Limit = defaultExpireLimit
	}
	if input.Limit > maxExpireLimit {
		return ExpireInteractionRequestsInput{}, errs.ErrInvalidArgument
	}
	return input, nil
}

func normalizeRequestNotificationInput(input RequestNotificationInput, now time.Time) (RequestNotificationInput, error) {
	if err := validateScope(input.Scope); err != nil {
		return RequestNotificationInput{}, err
	}
	if !input.NotificationKind.Valid() {
		return RequestNotificationInput{}, errs.ErrInvalidArgument
	}
	input.SourceOwner.Ref = strings.TrimSpace(input.SourceOwner.Ref)
	if !input.SourceOwner.Kind.Valid() || len(input.SourceOwner.Ref) > maxInteractionRefBytes {
		return RequestNotificationInput{}, errs.ErrInvalidArgument
	}
	input.Ingress.Ref = strings.TrimSpace(input.Ingress.Ref)
	if !input.Ingress.Kind.Valid() || len(input.Ingress.Ref) > maxInteractionRefBytes {
		return RequestNotificationInput{}, errs.ErrInvalidArgument
	}
	recipients, err := normalizeActorRefs(input.RecipientRefs)
	if err != nil {
		return RequestNotificationInput{}, err
	}
	input.RecipientRefs = recipients
	contextRefs, err := normalizeExternalRefs(input.ContextRefs)
	if err != nil {
		return RequestNotificationInput{}, err
	}
	input.ContextRefs = contextRefs
	channelHints, err := normalizeExternalRefs(input.ChannelHintRefs)
	if err != nil {
		return RequestNotificationInput{}, err
	}
	input.ChannelHintRefs = channelHints
	input.MessageTemplateRef = strings.TrimSpace(input.MessageTemplateRef)
	input.MessageTitle = strings.TrimSpace(input.MessageTitle)
	input.MessageSummary = strings.TrimSpace(input.MessageSummary)
	input.BodyPreview = strings.TrimSpace(input.BodyPreview)
	input.NotificationPolicyRef = strings.TrimSpace(input.NotificationPolicyRef)
	if blank(input.MessageTemplateRef) ||
		blank(input.MessageSummary) ||
		len(input.MessageTemplateRef) > maxInteractionRefBytes ||
		len(input.NotificationPolicyRef) > maxInteractionRefBytes ||
		utf8.RuneCountInString(input.MessageTitle) > maxMessageBodySummaryRunes ||
		utf8.RuneCountInString(input.MessageSummary) > maxMessageBodySummaryRunes ||
		utf8.RuneCountInString(input.BodyPreview) > maxMessageBodySummaryRunes {
		return RequestNotificationInput{}, errs.ErrInvalidArgument
	}
	if input.Priority == "" {
		input.Priority = enum.NotificationPriorityNormal
	}
	if !input.Priority.Valid() {
		return RequestNotificationInput{}, errs.ErrInvalidArgument
	}
	if input.ExpiresAt != nil {
		expiresAt := input.ExpiresAt.UTC()
		if !expiresAt.After(now) {
			return RequestNotificationInput{}, errs.ErrInvalidArgument
		}
		input.ExpiresAt = &expiresAt
	}
	return input, nil
}

func normalizeUpsertSubscriptionInput(input UpsertSubscriptionInput) (UpsertSubscriptionInput, error) {
	if err := validateScope(input.Scope); err != nil {
		return UpsertSubscriptionInput{}, err
	}
	subscriber, err := normalizeActorRef(input.SubscriberRef)
	if err != nil {
		return UpsertSubscriptionInput{}, err
	}
	input.SubscriberRef = subscriber
	input.SourceOwner.Ref = strings.TrimSpace(input.SourceOwner.Ref)
	if !input.SourceOwner.Kind.Valid() || len(input.SourceOwner.Ref) > maxInteractionRefBytes {
		return UpsertSubscriptionInput{}, errs.ErrInvalidArgument
	}
	channelHints, err := normalizeExternalRefs(input.ChannelHintRefs)
	if err != nil {
		return UpsertSubscriptionInput{}, err
	}
	input.ChannelHintRefs = channelHints
	input.EventFilterJSON, err = normalizePolicyObjectJSON(input.EventFilterJSON)
	if err != nil {
		return UpsertSubscriptionInput{}, err
	}
	input.DeliveryPreferencesJSON, err = normalizePolicyObjectJSON(input.DeliveryPreferencesJSON)
	if err != nil {
		return UpsertSubscriptionInput{}, err
	}
	input.SubscriptionPolicyRef = strings.TrimSpace(input.SubscriptionPolicyRef)
	if len(input.SubscriptionPolicyRef) > maxInteractionRefBytes {
		return UpsertSubscriptionInput{}, errs.ErrInvalidArgument
	}
	if input.Status == "" {
		input.Status = enum.SubscriptionStatusActive
	}
	if !input.Status.Valid() {
		return UpsertSubscriptionInput{}, errs.ErrInvalidArgument
	}
	return input, nil
}

type deliveryTargetContext struct {
	Target                value.DeliveryTarget
	Scope                 value.ScopeRef
	DeliveryKind          enum.DeliveryKind
	RequestID             string
	NotificationID        string
	SubscriptionID        string
	DeadlineAt            *time.Time
	ReminderPolicyRef     string
	NotificationPolicyRef string
}

func (s *Service) normalizePlanDeliveryInput(ctx context.Context, input PlanDeliveryInput) (PlanDeliveryInput, deliveryTargetContext, error) {
	if !input.Target.Valid() {
		return PlanDeliveryInput{}, deliveryTargetContext{}, errs.ErrInvalidArgument
	}
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	if len(input.CorrelationID) > maxInteractionRefBytes {
		return PlanDeliveryInput{}, deliveryTargetContext{}, errs.ErrInvalidArgument
	}
	switch input.Target.Kind {
	case value.DeliveryTargetKindRequest:
		request, err := s.repository.GetInteractionRequest(ctx, input.Target.ID)
		if err != nil {
			return PlanDeliveryInput{}, deliveryTargetContext{}, err
		}
		if request.Status.Terminal() {
			return PlanDeliveryInput{}, deliveryTargetContext{}, errs.ErrConflict
		}
		return input, deliveryTargetContext{
			Target:            input.Target,
			Scope:             request.Scope,
			DeliveryKind:      deliveryKindForRequest(request.RequestKind),
			RequestID:         request.ID.String(),
			DeadlineAt:        request.DeadlineAt,
			ReminderPolicyRef: request.ReminderPolicyRef,
		}, nil
	case value.DeliveryTargetKindNotification:
		notification, err := s.repository.GetNotification(ctx, input.Target.ID)
		if err != nil {
			return PlanDeliveryInput{}, deliveryTargetContext{}, err
		}
		if notification.Status == enum.NotificationStatusExpired || notification.Status == enum.NotificationStatusFailed {
			return PlanDeliveryInput{}, deliveryTargetContext{}, errs.ErrConflict
		}
		return input, deliveryTargetContext{
			Target:                input.Target,
			Scope:                 notification.Scope,
			DeliveryKind:          enum.DeliveryKindNotification,
			RequestID:             uuidProto(notification.RequestID),
			NotificationID:        notification.ID.String(),
			SubscriptionID:        uuidProto(notification.SubscriptionID),
			DeadlineAt:            notification.ExpiresAt,
			NotificationPolicyRef: notification.NotificationPolicyRef,
		}, nil
	default:
		return PlanDeliveryInput{}, deliveryTargetContext{}, errs.ErrInvalidArgument
	}
}

func deliveryKindForRequest(kind enum.InteractionRequestKind) enum.DeliveryKind {
	switch kind {
	case enum.InteractionRequestKindFeedback:
		return enum.DeliveryKindFeedback
	case enum.InteractionRequestKindApproval:
		return enum.DeliveryKindApproval
	case enum.InteractionRequestKindHumanGate:
		return enum.DeliveryKindHumanGate
	default:
		return ""
	}
}

func (s *Service) deliveryRoute(ctx context.Context, routeID uuid.UUID, scope value.ScopeRef) (entity.DeliveryRoute, error) {
	var route entity.DeliveryRoute
	var err error
	if routeID != uuid.Nil {
		route, err = s.repository.GetDeliveryRoute(ctx, routeID)
	} else {
		route, err = s.repository.FindActiveDeliveryRoute(ctx, scope)
	}
	if err != nil {
		return entity.DeliveryRoute{}, err
	}
	if route.Status != enum.DeliveryRouteStatusActive || route.Scope != scope {
		return entity.DeliveryRoute{}, errs.ErrConflict
	}
	if route.SurfaceKind == enum.DeliverySurfaceKindChannelPackage && (blank(route.ChannelCapabilityRef) || blank(route.PackageInstallationRef)) {
		return entity.DeliveryRoute{}, errs.ErrConflict
	}
	return route, nil
}

type callbackResolution struct {
	deliveryAttemptID *uuid.UUID
	requestID         *uuid.UUID
	sourceRouteID     *uuid.UUID
	callbackRouteRef  string
	request           *entity.InteractionRequest
}

func (s *Service) resolveChannelCallback(ctx context.Context, envelope value.ChannelCallbackEnvelope) (callbackResolution, error) {
	var resolved callbackResolution
	if envelope.DeliveryID != "" {
		attempt, err := s.repository.GetDeliveryAttemptByDeliveryID(ctx, envelope.DeliveryID)
		if err != nil {
			return callbackResolution{}, err
		}
		resolved.deliveryAttemptID = uuidPtr(attempt.ID)
		resolved.sourceRouteID = uuidPtr(attempt.RouteID)
		resolved.callbackRouteRef = attempt.CallbackRouteRef
		if attempt.Target.Kind == value.DeliveryTargetKindRequest {
			resolved.requestID = uuidPtr(attempt.Target.ID)
		}
		if envelope.RequestRef != "" && resolved.requestID != nil && envelope.RequestRef != resolved.requestID.String() {
			return callbackResolution{}, errs.ErrConflict
		}
	}
	if envelope.RequestRef != "" {
		requestID, err := uuid.Parse(envelope.RequestRef)
		if err != nil {
			return callbackResolution{}, errs.ErrInvalidArgument
		}
		if resolved.requestID != nil && *resolved.requestID != requestID {
			return callbackResolution{}, errs.ErrConflict
		}
		resolved.requestID = uuidPtr(requestID)
	}
	if resolved.requestID != nil {
		request, err := s.repository.GetInteractionRequest(ctx, *resolved.requestID)
		if err != nil {
			return callbackResolution{}, err
		}
		resolved.request = &request
	}
	if resolved.requestID == nil && resolved.deliveryAttemptID == nil {
		return callbackResolution{}, errs.ErrInvalidArgument
	}
	return resolved, nil
}

func (s *Service) channelCallbackResult(ctx context.Context, callback entity.ChannelCallback) (ChannelCallbackResult, error) {
	result := ChannelCallbackResult{Callback: callback}
	if callback.ProcessingStatus != enum.CallbackProcessingStatusAccepted || callback.RequestID == nil {
		return result, nil
	}
	response, err := s.repository.GetInteractionResponseBySource(ctx, enum.InteractionResponseSourceKindChannelCallback, callbackResponseSourceRef(callback))
	if errors.Is(err, errs.ErrNotFound) {
		return result, nil
	}
	if err != nil {
		return ChannelCallbackResult{}, err
	}
	result.Response = &response
	return result, nil
}

func (s *Service) replayExistingChannelCallback(ctx context.Context, callbackID string, fingerprint string) (ChannelCallbackResult, error) {
	existing, getErr := s.repository.GetChannelCallbackByCallbackID(ctx, callbackID)
	if errors.Is(getErr, errs.ErrNotFound) {
		return ChannelCallbackResult{}, errs.ErrConflict
	}
	if getErr != nil {
		return ChannelCallbackResult{}, getErr
	}
	if existing.CallbackFingerprint != fingerprint {
		return ChannelCallbackResult{}, errs.ErrConflict
	}
	return s.channelCallbackResult(ctx, existing)
}

func (s *Service) channelCallbackResponse(callback entity.ChannelCallback, request entity.InteractionRequest, now time.Time) (*entity.InteractionResponse, entity.InteractionRequest, int64, string) {
	if request.Status.Terminal() {
		return nil, entity.InteractionRequest{}, 0, callbackErrorRequestResolved
	}
	if !callbackActionAllowed(request.AllowedActions, callback.Action) {
		return nil, entity.InteractionRequest{}, 0, callbackErrorActionNotAllowed
	}
	responseAction := enum.InteractionResponseAction(callback.Action)
	if !responseAction.Valid() {
		return nil, entity.InteractionRequest{}, 0, callbackErrorActionUnsupported
	}
	if !terminalAllowedAction(request.AllowedActions, responseAction) {
		return nil, entity.InteractionRequest{}, 0, callbackErrorActionNotTerminal
	}
	if blank(callback.ActorRef) {
		return nil, entity.InteractionRequest{}, 0, callbackErrorActorRequired
	}
	if responseAction == enum.InteractionResponseActionAnswer || responseAction == enum.InteractionResponseActionCustom {
		if blank(callback.CallbackSummary) && !hasObjectRef(callback.CallbackObject) {
			return nil, entity.InteractionRequest{}, 0, callbackErrorResponseRequired
		}
	}

	response := entity.InteractionResponse{
		ID:                  s.ids.New(),
		RequestID:           request.ID,
		ResponseAction:      responseAction,
		RespondedByActorRef: callback.ActorRef,
		ResponseSummary:     callback.CallbackSummary,
		ResponseObject:      callback.CallbackObject,
		SourceKind:          enum.InteractionResponseSourceKindChannelCallback,
		SourceRef:           callbackResponseSourceRef(callback),
		CreatedAt:           now,
	}
	previousVersion := request.Version
	request.Status = enum.InteractionRequestStatusAnswered
	request.Version++
	request.UpdatedAt = now
	request.ResolvedAt = &now
	return &response, request, previousVersion, ""
}

func callbackActionAllowed(actions []value.InteractionAction, action string) bool {
	for _, item := range actions {
		if item.ActionKey == action {
			return true
		}
	}
	return len(actions) == 0
}

func callbackResponseSourceRef(callback entity.ChannelCallback) string {
	return callback.ID.String()
}

func uuidPtr(id uuid.UUID) *uuid.UUID {
	value := id
	return &value
}

func normalizeRecordDeliveryResultInput(input RecordDeliveryResultInput) (RecordDeliveryResultInput, error) {
	result := input.Result
	result.ContractVersion = strings.TrimSpace(result.ContractVersion)
	result.DeliveryID = strings.TrimSpace(result.DeliveryID)
	result.ChannelMessageRef = strings.TrimSpace(result.ChannelMessageRef)
	result.ErrorCode = strings.TrimSpace(result.ErrorCode)
	result.DeliveryCommandRef = strings.TrimSpace(result.DeliveryCommandRef)
	result.RuntimeRef = strings.TrimSpace(result.RuntimeRef)
	result.RuntimeJobRef = strings.TrimSpace(result.RuntimeJobRef)
	if blank(result.ContractVersion) ||
		blank(result.DeliveryID) ||
		len(result.ContractVersion) > maxInteractionRefBytes ||
		len(result.DeliveryID) > maxInteractionRefBytes ||
		len(result.ChannelMessageRef) > maxInteractionRefBytes ||
		len(result.ErrorCode) > maxInteractionRefBytes ||
		len(result.DeliveryCommandRef) > maxInteractionRefBytes ||
		len(result.RuntimeRef) > maxInteractionRefBytes ||
		len(result.RuntimeJobRef) > maxInteractionRefBytes ||
		!result.ResultStatus.Valid() ||
		result.OccurredAt.IsZero() {
		return RecordDeliveryResultInput{}, errs.ErrInvalidArgument
	}
	result.OccurredAt = result.OccurredAt.UTC()
	if result.RetryAfter != nil {
		retryAfter := result.RetryAfter.UTC()
		if retryAfter.Before(result.OccurredAt) {
			return RecordDeliveryResultInput{}, errs.ErrInvalidArgument
		}
		result.RetryAfter = &retryAfter
	}
	if result.ErrorCode != "" && sensitiveMetadataKey(result.ErrorCode) {
		return RecordDeliveryResultInput{}, errs.ErrInvalidArgument
	}
	if result.DeliveryCommandRef != "" && sensitiveMetadataKey(result.DeliveryCommandRef) {
		return RecordDeliveryResultInput{}, errs.ErrInvalidArgument
	}
	if result.ErrorClass != "" && !result.ErrorClass.Valid() {
		return RecordDeliveryResultInput{}, errs.ErrInvalidArgument
	}
	input.Result = result
	return input, nil
}

func normalizeRecordChannelCallbackInput(input RecordChannelCallbackInput) (RecordChannelCallbackInput, error) {
	callback := input.Callback
	callback.ContractVersion = strings.TrimSpace(callback.ContractVersion)
	callback.CallbackID = strings.TrimSpace(callback.CallbackID)
	callback.DeliveryID = strings.TrimSpace(callback.DeliveryID)
	callback.RequestRef = strings.TrimSpace(callback.RequestRef)
	callback.ActorRef = strings.TrimSpace(callback.ActorRef)
	callback.Action = strings.TrimSpace(callback.Action)
	callback.AnswerSummary = strings.TrimSpace(callback.AnswerSummary)
	callback.AnswerObject = normalizeObjectRef(callback.AnswerObject)
	callback.GatewayRef = strings.TrimSpace(callback.GatewayRef)
	callback.CorrelationID = strings.TrimSpace(callback.CorrelationID)
	if blank(callback.ContractVersion) ||
		blank(callback.CallbackID) ||
		blank(callback.Action) ||
		(callback.DeliveryID == "" && callback.RequestRef == "") ||
		!callback.SignatureStatus.Valid() ||
		callback.ReceivedAt.IsZero() ||
		len(callback.ContractVersion) > maxInteractionRefBytes ||
		len(callback.CallbackID) > maxInteractionRefBytes ||
		len(callback.DeliveryID) > maxInteractionRefBytes ||
		len(callback.RequestRef) > maxInteractionRefBytes ||
		len(callback.ActorRef) > maxInteractionRefBytes ||
		len(callback.Action) > maxInteractionRefBytes ||
		len(callback.GatewayRef) > maxInteractionRefBytes ||
		len(callback.CorrelationID) > maxInteractionRefBytes ||
		utf8.RuneCountInString(callback.AnswerSummary) > maxMessageBodySummaryRunes {
		return RecordChannelCallbackInput{}, errs.ErrInvalidArgument
	}
	if sensitiveMetadataKey(callback.CallbackID) || sensitiveMetadataKey(callback.Action) || sensitiveMetadataKey(callback.GatewayRef) {
		return RecordChannelCallbackInput{}, errs.ErrInvalidArgument
	}
	if hasUnsafePayloadMarker(callback.AnswerSummary) {
		return RecordChannelCallbackInput{}, errs.ErrInvalidArgument
	}
	if err := validateObjectRef(callback.AnswerObject); err != nil {
		return RecordChannelCallbackInput{}, err
	}
	callback.ReceivedAt = callback.ReceivedAt.UTC()
	input.Callback = callback
	return input, nil
}

func deliveryAttemptWithResult(attempt entity.DeliveryAttempt, result value.ChannelDeliveryResult, now time.Time) (entity.DeliveryAttempt, string, error) {
	attempt.UpdatedAt = now
	occurredAt := result.OccurredAt
	attempt.SentAt = &occurredAt
	attempt.ChannelMessageRef = result.ChannelMessageRef
	if result.RuntimeRef != "" {
		attempt.RuntimeRef = result.RuntimeRef
	}
	if result.RuntimeJobRef != "" {
		attempt.RuntimeJobRef = result.RuntimeJobRef
	}

	switch result.ResultStatus {
	case enum.ChannelDeliveryResultStatusAccepted:
		return deliveryAttemptSuccess(attempt, enum.DeliveryAttemptStatusAccepted), interactionevents.EventDeliveryAccepted, nil
	case enum.ChannelDeliveryResultStatusDelivered:
		return deliveryAttemptSuccess(attempt, enum.DeliveryAttemptStatusDelivered), interactionevents.EventDeliveryDelivered, nil
	case enum.ChannelDeliveryResultStatusExpired:
		attempt.Status = enum.DeliveryAttemptStatusExpired
		applyDeliveryResultError(&attempt, result, nil, enum.DeliveryErrorClassTemporary, "DELIVERY_EXPIRED")
		return attempt, interactionevents.EventDeliveryExpired, nil
	case enum.ChannelDeliveryResultStatusDeferred, enum.ChannelDeliveryResultStatusRejected:
		return entity.DeliveryAttempt{}, "", errs.ErrInvalidArgument
	case enum.ChannelDeliveryResultStatusFailed:
		if result.ErrorClass == "" || result.ErrorCode == "" {
			return entity.DeliveryAttempt{}, "", errs.ErrInvalidArgument
		}
		attempt.Status = enum.DeliveryAttemptStatusFailed
		attempt.NextRetryAt = result.RetryAfter
		attempt.ErrorClass = result.ErrorClass
		attempt.ErrorCode = result.ErrorCode
		return attempt, interactionevents.EventDeliveryFailed, nil
	default:
		return entity.DeliveryAttempt{}, "", errs.ErrInvalidArgument
	}
}

func deliveryAttemptSuccess(attempt entity.DeliveryAttempt, status enum.DeliveryAttemptStatus) entity.DeliveryAttempt {
	attempt.Status = status
	attempt.NextRetryAt = nil
	attempt.ErrorCode = ""
	attempt.ErrorClass = ""
	return attempt
}

func applyDeliveryResultError(attempt *entity.DeliveryAttempt, result value.ChannelDeliveryResult, nextRetryAt *time.Time, defaultClass enum.DeliveryErrorClass, defaultCode string) {
	attempt.NextRetryAt = nextRetryAt
	attempt.ErrorClass = result.ErrorClass
	if attempt.ErrorClass == "" {
		attempt.ErrorClass = defaultClass
	}
	attempt.ErrorCode = result.ErrorCode
	if attempt.ErrorCode == "" {
		attempt.ErrorCode = defaultCode
	}
}

func replayDeliveryResultByFingerprint(attempt entity.DeliveryAttempt, resultFingerprint string) (entity.DeliveryAttempt, bool, error) {
	if resultFingerprint == "" {
		return entity.DeliveryAttempt{}, false, errs.ErrInvalidArgument
	}
	if attempt.ResultFingerprint == "" {
		return entity.DeliveryAttempt{}, false, nil
	}
	if attempt.ResultFingerprint != resultFingerprint {
		return entity.DeliveryAttempt{}, true, errs.ErrConflict
	}
	return attempt, true, nil
}

func (s *Service) replayDeliveryResultConflict(ctx context.Context, deliveryID string, resultFingerprint string) (entity.DeliveryAttempt, error) {
	current, err := s.repository.GetDeliveryAttemptByDeliveryID(ctx, deliveryID)
	if err != nil {
		return entity.DeliveryAttempt{}, err
	}
	replayed, ok, err := replayDeliveryResultByFingerprint(current, resultFingerprint)
	if err != nil {
		return entity.DeliveryAttempt{}, err
	}
	if !ok {
		return entity.DeliveryAttempt{}, errs.ErrConflict
	}
	return replayed, nil
}

type deliveryResultFingerprintInput struct {
	ContractVersion    string                           `json:"contract_version"`
	DeliveryID         string                           `json:"delivery_id"`
	ResultStatus       enum.ChannelDeliveryResultStatus `json:"result_status"`
	ChannelMessageRef  string                           `json:"channel_message_ref,omitempty"`
	ErrorCode          string                           `json:"error_code,omitempty"`
	ErrorClass         enum.DeliveryErrorClass          `json:"error_class,omitempty"`
	RetryAfter         string                           `json:"retry_after,omitempty"`
	OccurredAt         string                           `json:"occurred_at"`
	DeliveryCommandRef string                           `json:"delivery_command_ref,omitempty"`
	RuntimeRef         string                           `json:"runtime_ref,omitempty"`
	RuntimeJobRef      string                           `json:"runtime_job_ref,omitempty"`
}

func deliveryResultFingerprint(result value.ChannelDeliveryResult) (string, error) {
	fingerprint, err := fingerprintInput(deliveryResultFingerprintInput{
		ContractVersion:    result.ContractVersion,
		DeliveryID:         result.DeliveryID,
		ResultStatus:       result.ResultStatus,
		ChannelMessageRef:  result.ChannelMessageRef,
		ErrorCode:          result.ErrorCode,
		ErrorClass:         result.ErrorClass,
		RetryAfter:         timeProto(result.RetryAfter),
		OccurredAt:         timeProto(&result.OccurredAt),
		DeliveryCommandRef: result.DeliveryCommandRef,
		RuntimeRef:         result.RuntimeRef,
		RuntimeJobRef:      result.RuntimeJobRef,
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + fingerprint, nil
}

type callbackEnvelopeFingerprintInput struct {
	ContractVersion string                       `json:"contract_version"`
	CallbackID      string                       `json:"callback_id"`
	DeliveryID      string                       `json:"delivery_id,omitempty"`
	RequestRef      string                       `json:"request_ref,omitempty"`
	ActorRef        string                       `json:"actor_ref,omitempty"`
	Action          string                       `json:"action"`
	AnswerSummary   string                       `json:"answer_summary,omitempty"`
	AnswerObject    value.ObjectRef              `json:"answer_object,omitempty"`
	SignatureStatus enum.CallbackSignatureStatus `json:"signature_status"`
	GatewayRef      string                       `json:"gateway_ref,omitempty"`
	CorrelationID   string                       `json:"correlation_id,omitempty"`
}

type callbackCommandFingerprintInput struct {
	Meta     value.CommandMeta                `json:"meta"`
	Callback callbackEnvelopeFingerprintInput `json:"callback"`
}

func channelCallbackRequestFingerprint(input RecordChannelCallbackInput) (string, error) {
	return fingerprintInput(callbackCommandFingerprintInput{
		Meta:     input.Meta,
		Callback: callbackEnvelopeSemanticFingerprintInput(input.Callback),
	})
}

func callbackEnvelopeFingerprint(callback value.ChannelCallbackEnvelope) (string, error) {
	fingerprint, err := fingerprintInput(callbackEnvelopeSemanticFingerprintInput(callback))
	if err != nil {
		return "", err
	}
	return "sha256:" + fingerprint, nil
}

func callbackEnvelopeSemanticFingerprintInput(callback value.ChannelCallbackEnvelope) callbackEnvelopeFingerprintInput {
	return callbackEnvelopeFingerprintInput{
		ContractVersion: callback.ContractVersion,
		CallbackID:      callback.CallbackID,
		DeliveryID:      callback.DeliveryID,
		RequestRef:      callback.RequestRef,
		ActorRef:        callback.ActorRef,
		Action:          callback.Action,
		AnswerSummary:   callback.AnswerSummary,
		AnswerObject:    callback.AnswerObject,
		SignatureStatus: callback.SignatureStatus,
		GatewayRef:      callback.GatewayRef,
		CorrelationID:   callback.CorrelationID,
	}
}

func nextAttemptNumber(attempts []entity.DeliveryAttempt) int32 {
	var max int32
	for _, attempt := range attempts {
		if attempt.AttemptNumber > max {
			max = attempt.AttemptNumber
		}
	}
	return max + 1
}

func normalizeActorRefs(input []value.ActorRef) ([]value.ActorRef, error) {
	if len(input) == 0 || len(input) > maxInteractionRefs {
		return nil, errs.ErrInvalidArgument
	}
	result := make([]value.ActorRef, 0, len(input))
	seen := map[string]struct{}{}
	for _, ref := range input {
		item, err := normalizeActorRef(ref)
		if err != nil {
			return nil, err
		}
		key := item.String()
		if _, exists := seen[key]; exists {
			return nil, errs.ErrInvalidArgument
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result, nil
}

func normalizeActorRef(ref value.ActorRef) (value.ActorRef, error) {
	item := value.ActorRef{Kind: strings.TrimSpace(ref.Kind), Ref: strings.TrimSpace(ref.Ref)}
	if blank(item.Kind) || blank(item.Ref) || len(item.Kind) > maxInteractionRefBytes || len(item.Ref) > maxInteractionRefBytes {
		return value.ActorRef{}, errs.ErrInvalidArgument
	}
	return item, nil
}

func normalizeExternalRefs(input []value.ExternalRef) ([]value.ExternalRef, error) {
	if len(input) > maxInteractionRefs {
		return nil, errs.ErrInvalidArgument
	}
	result := make([]value.ExternalRef, 0, len(input))
	for _, ref := range input {
		item := value.ExternalRef{Kind: strings.TrimSpace(ref.Kind), Ref: strings.TrimSpace(ref.Ref)}
		if blank(item.Kind) || blank(item.Ref) || len(item.Kind) > maxInteractionRefBytes || len(item.Ref) > maxInteractionRefBytes {
			return nil, errs.ErrInvalidArgument
		}
		result = append(result, item)
	}
	return result, nil
}

func normalizePolicyObjectJSON(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		value = "{}"
	}
	if len(value) > maxPolicyJSONBytes || !json.Valid([]byte(value)) {
		return "", errs.ErrInvalidArgument
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(value), &object); err != nil || object == nil {
		return "", errs.ErrInvalidArgument
	}
	if err := validateSafePolicyObject(object, 0); err != nil {
		return "", err
	}
	return value, nil
}

func validateSafePolicyObject(object map[string]any, depth int) error {
	if depth > maxPolicyJSONDepth {
		return errs.ErrInvalidArgument
	}
	for rawKey, value := range object {
		key := strings.TrimSpace(rawKey)
		if key == "" || len(key) > maxSafeMetadataKeyBytes || sensitiveMetadataKey(key) {
			return errs.ErrInvalidArgument
		}
		switch typed := value.(type) {
		case map[string]any:
			if err := validateSafePolicyObject(typed, depth+1); err != nil {
				return err
			}
		case []any:
			for _, item := range typed {
				nested, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if err := validateSafePolicyObject(nested, depth+1); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func normalizeInteractionActions(input []value.InteractionAction) ([]value.InteractionAction, error) {
	if len(input) == 0 || len(input) > maxInteractionRefs {
		return nil, errs.ErrInvalidArgument
	}
	result := make([]value.InteractionAction, 0, len(input))
	seen := map[string]struct{}{}
	hasTerminal := false
	for _, action := range input {
		item := value.InteractionAction{
			ActionKey:        strings.TrimSpace(action.ActionKey),
			LabelTemplateRef: strings.TrimSpace(action.LabelTemplateRef),
			Terminal:         action.Terminal,
		}
		if !enum.InteractionResponseAction(item.ActionKey).Valid() || len(item.LabelTemplateRef) > maxInteractionRefBytes {
			return nil, errs.ErrInvalidArgument
		}
		if _, exists := seen[item.ActionKey]; exists {
			return nil, errs.ErrInvalidArgument
		}
		seen[item.ActionKey] = struct{}{}
		hasTerminal = hasTerminal || item.Terminal
		result = append(result, item)
	}
	if !hasTerminal {
		return nil, errs.ErrInvalidArgument
	}
	return result, nil
}

func validateObjectRef(input value.ObjectRef) error {
	if input.SizeBytes != nil && *input.SizeBytes < 0 {
		return errs.ErrInvalidArgument
	}
	if hasObjectRef(input) && (blank(input.URI) || blank(input.Digest)) {
		return errs.ErrInvalidArgument
	}
	return nil
}

func validateExpectedVersion(meta value.CommandMeta, current int64) error {
	if meta.ExpectedVersion == nil || *meta.ExpectedVersion <= 0 {
		return errs.ErrInvalidArgument
	}
	if *meta.ExpectedVersion != current {
		return errs.ErrConflict
	}
	return nil
}

func terminalAllowedAction(actions []value.InteractionAction, action enum.InteractionResponseAction) bool {
	for _, item := range actions {
		if item.ActionKey == string(action) {
			return item.Terminal
		}
	}
	return false
}

func normalizeRecordConversationMessageInput(input RecordConversationMessageInput) (RecordConversationMessageInput, error) {
	input.AuthorRef = strings.TrimSpace(input.AuthorRef)
	input.BodySummary = strings.TrimSpace(input.BodySummary)
	input.BodyObject = normalizeObjectRef(input.BodyObject)
	input.BodyDigest = strings.TrimSpace(input.BodyDigest)
	input.Locale = strings.TrimSpace(input.Locale)

	metadata, err := normalizeSafeMetadata(input.SafeMetadata)
	if err != nil {
		return RecordConversationMessageInput{}, err
	}
	input.SafeMetadata = metadata

	if input.ThreadID == uuid.Nil || !input.MessageKind.Valid() || blank(input.AuthorRef) {
		return RecordConversationMessageInput{}, errs.ErrInvalidArgument
	}
	if err := validateMessageBodyStorage(input); err != nil {
		return RecordConversationMessageInput{}, err
	}
	return input, nil
}

func normalizeObjectRef(input value.ObjectRef) value.ObjectRef {
	result := value.ObjectRef{
		URI:    strings.TrimSpace(input.URI),
		Digest: strings.TrimSpace(input.Digest),
	}
	if input.SizeBytes != nil {
		size := *input.SizeBytes
		result.SizeBytes = &size
	}
	return result
}

func validateMessageBodyStorage(input RecordConversationMessageInput) error {
	if utf8.RuneCountInString(input.BodySummary) > maxMessageBodySummaryRunes {
		return errs.ErrInvalidArgument
	}
	if err := validateObjectRef(input.BodyObject); err != nil {
		return errs.ErrInvalidArgument
	}

	hasObject := hasObjectRef(input.BodyObject)
	hasDigest := !blank(input.BodyDigest)
	if hasObject || hasDigest {
		if !hasObject || !hasDigest {
			return errs.ErrInvalidArgument
		}
	}
	if blank(input.BodySummary) && (!hasObject || !hasDigest) {
		return errs.ErrInvalidArgument
	}
	return nil
}

func hasObjectRef(input value.ObjectRef) bool {
	return !blank(input.URI) || !blank(input.Digest) || input.SizeBytes != nil
}

func normalizeSafeMetadata(input map[string]string) (map[string]string, error) {
	if len(input) == 0 {
		return map[string]string{}, nil
	}
	if len(input) > maxSafeMetadataEntries {
		return nil, errs.ErrInvalidArgument
	}

	result := make(map[string]string, len(input))
	totalBytes := 0
	for rawKey, rawValue := range input {
		key := strings.TrimSpace(rawKey)
		value := strings.TrimSpace(rawValue)
		if key == "" || len(key) > maxSafeMetadataKeyBytes || len(value) > maxSafeMetadataValueBytes {
			return nil, errs.ErrInvalidArgument
		}
		if sensitiveMetadataKey(key) {
			return nil, errs.ErrInvalidArgument
		}
		if _, exists := result[key]; exists {
			return nil, errs.ErrInvalidArgument
		}
		totalBytes += len(key) + len(value)
		if totalBytes > maxSafeMetadataTotalBytes {
			return nil, errs.ErrInvalidArgument
		}
		result[key] = value
	}
	return result, nil
}

func sensitiveMetadataKey(key string) bool {
	normalized := strings.ToLower(key)
	normalized = strings.NewReplacer("-", "_", ".", "_", " ", "_", "/", "_").Replace(normalized)
	compact := strings.ReplaceAll(normalized, "_", "")
	return strings.Contains(compact, "secret") ||
		strings.Contains(compact, "token") ||
		strings.Contains(compact, "password") ||
		strings.Contains(compact, "passwd") ||
		strings.Contains(compact, "pwd") ||
		strings.Contains(compact, "credential") ||
		strings.Contains(compact, "authorization") ||
		strings.Contains(compact, "bearer") ||
		strings.Contains(compact, "cookie") ||
		strings.Contains(compact, "session") ||
		strings.Contains(compact, "apikey") ||
		strings.Contains(compact, "privatekey") ||
		strings.Contains(compact, "accesskey")
}

func hasUnsafePayloadMarker(value string) bool {
	if value == "" {
		return false
	}
	normalized := strings.ToLower(value)
	return strings.Contains(normalized, "authorization:") ||
		strings.Contains(normalized, "bearer ") ||
		strings.Contains(normalized, "token=") ||
		strings.Contains(normalized, "secret=") ||
		strings.Contains(normalized, "password=") ||
		strings.Contains(normalized, "raw_payload")
}

func requestEventPayload(request entity.InteractionRequest) interactionevents.Payload {
	return interactionevents.Payload{
		RequestID:            request.ID.String(),
		RequestKind:          string(request.RequestKind),
		ScopeType:            string(request.Scope.Type),
		ScopeRef:             request.Scope.Ref,
		SourceOwnerKind:      string(request.SourceOwner.Kind),
		SourceOwnerRef:       request.SourceOwner.Ref,
		IngressKind:          string(request.Ingress.Kind),
		RiskClass:            string(request.RiskClass),
		ProviderOperationRef: contextRef(request.ContextRefs, "provider_operation"),
		AgentRunRef:          contextRef(request.ContextRefs, "agent_run"),
		OwnerService:         string(request.DecisionOwner.Kind),
		OwnerRequestRef:      request.DecisionOwner.OwnerRequestRef,
		OwnerDecisionRef:     request.DecisionOwner.OwnerDecisionRef,
		Status:               string(request.Status),
		DeadlineAt:           timeProto(request.DeadlineAt),
		Version:              request.Version,
	}
}

func requestResponseRecordedPayload(request entity.InteractionRequest, response entity.InteractionResponse) interactionevents.Payload {
	return interactionevents.Payload{
		RequestID:        request.ID.String(),
		ResponseID:       response.ID.String(),
		ResponseAction:   string(response.ResponseAction),
		ActorRef:         response.RespondedByActorRef,
		OwnerService:     string(request.DecisionOwner.Kind),
		OwnerRequestRef:  request.DecisionOwner.OwnerRequestRef,
		OwnerDecisionRef: response.OwnerDecisionRef,
		Status:           string(request.Status),
		Version:          request.Version,
	}
}

func notificationEventPayload(notification entity.Notification) interactionevents.Payload {
	return interactionevents.Payload{
		NotificationID:   notification.ID.String(),
		RequestID:        uuidProto(notification.RequestID),
		SubscriptionID:   uuidProto(notification.SubscriptionID),
		NotificationKind: string(notification.NotificationKind),
		ScopeType:        string(notification.Scope.Type),
		ScopeRef:         notification.Scope.Ref,
		SourceOwnerKind:  string(notification.SourceOwner.Kind),
		SourceOwnerRef:   notification.SourceOwner.Ref,
		IngressKind:      string(notification.Ingress.Kind),
		Priority:         string(notification.Priority),
		Status:           string(notification.Status),
	}
}

func subscriptionEventPayload(subscription entity.Subscription) interactionevents.Payload {
	return interactionevents.Payload{
		SubscriptionID:  subscription.ID.String(),
		ScopeType:       string(subscription.Scope.Type),
		ScopeRef:        subscription.Scope.Ref,
		SourceOwnerKind: string(subscription.SourceOwner.Kind),
		SourceOwnerRef:  subscription.SourceOwner.Ref,
		SubscriberRef:   subscription.SubscriberRef.String(),
		Status:          string(subscription.Status),
		Version:         subscription.Version,
	}
}

func deliveryRequestedPayload(attempt entity.DeliveryAttempt, target deliveryTargetContext, correlationID string) interactionevents.Payload {
	return interactionevents.Payload{
		DeliveryAttemptID:      attempt.ID.String(),
		DeliveryID:             attempt.DeliveryID,
		RequestID:              target.RequestID,
		NotificationID:         target.NotificationID,
		SubscriptionID:         target.SubscriptionID,
		RouteID:                attempt.RouteID.String(),
		AttemptNumber:          int64(attempt.AttemptNumber),
		ScopeType:              string(target.Scope.Type),
		ScopeRef:               target.Scope.Ref,
		RequestKind:            string(kindFromDeliveryKind(target.DeliveryKind)),
		Status:                 string(attempt.Status),
		CorrelationID:          correlationID,
		DeadlineAt:             timeProto(target.DeadlineAt),
		ChannelCapabilityRef:   attempt.ChannelCapabilityRef,
		PackageInstallationRef: attempt.PackageInstallationRef,
		PackageVersionRef:      attempt.PackageVersionRef,
		DeliveryCommandRef:     attempt.DeliveryCommandRef,
		CallbackRef:            attempt.CallbackRef,
		CallbackRouteRef:       attempt.CallbackRouteRef,
		RuntimeRef:             attempt.RuntimeRef,
		RoutingPolicyRef:       attempt.RoutingPolicyRef,
	}
}

func deliveryResultPayload(attempt entity.DeliveryAttempt) interactionevents.Payload {
	return interactionevents.Payload{
		DeliveryAttemptID:  attempt.ID.String(),
		DeliveryID:         attempt.DeliveryID,
		RequestID:          deliveryTargetRequestID(attempt.Target),
		NotificationID:     deliveryTargetNotificationID(attempt.Target),
		RouteID:            attempt.RouteID.String(),
		AttemptNumber:      int64(attempt.AttemptNumber),
		ChannelMessageRef:  attempt.ChannelMessageRef,
		ErrorCode:          attempt.ErrorCode,
		ErrorClass:         string(attempt.ErrorClass),
		NextRetryAt:        timeProto(attempt.NextRetryAt),
		Status:             string(attempt.Status),
		DeliveryCommandRef: attempt.DeliveryCommandRef,
		RuntimeRef:         attempt.RuntimeRef,
		RuntimeJobRef:      attempt.RuntimeJobRef,
	}
}

func callbackReceivedPayload(callback entity.ChannelCallback) interactionevents.Payload {
	return interactionevents.Payload{
		CallbackID:        callback.CallbackID,
		DeliveryAttemptID: uuidValue(callback.DeliveryAttemptID),
		DeliveryID:        callback.DeliveryID,
		RequestID:         uuidValue(callback.RequestID),
		RouteID:           uuidValue(callback.SourceRouteID),
		ActorRef:          callback.ActorRef,
		ResponseAction:    callback.Action,
		ProcessingStatus:  string(callback.ProcessingStatus),
		Status:            string(callback.ProcessingStatus),
		CallbackRouteRef:  callback.CallbackRouteRef,
		GatewayRef:        callback.GatewayRef,
		CorrelationID:     callback.CorrelationID,
	}
}

func uuidValue(input *uuid.UUID) string {
	if input == nil {
		return ""
	}
	return input.String()
}

func deliveryAttemptIDs(attempts []entity.DeliveryAttempt) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(attempts))
	for _, attempt := range attempts {
		ids = append(ids, attempt.ID)
	}
	return ids
}

func deliveryStatusRequestID(target value.DeliveryTarget) uuid.UUID {
	if target.Kind != value.DeliveryTargetKindRequest {
		return uuid.Nil
	}
	return target.ID
}

func deliveryTargetRequestID(target value.DeliveryTarget) string {
	if target.Kind != value.DeliveryTargetKindRequest {
		return ""
	}
	return target.ID.String()
}

func deliveryTargetNotificationID(target value.DeliveryTarget) string {
	if target.Kind != value.DeliveryTargetKindNotification {
		return ""
	}
	return target.ID.String()
}

func kindFromDeliveryKind(kind enum.DeliveryKind) enum.InteractionRequestKind {
	switch kind {
	case enum.DeliveryKindFeedback:
		return enum.InteractionRequestKindFeedback
	case enum.DeliveryKindApproval:
		return enum.InteractionRequestKindApproval
	case enum.DeliveryKindHumanGate:
		return enum.InteractionRequestKindHumanGate
	default:
		return ""
	}
}

type deliveryDigestInput struct {
	TargetKind             value.DeliveryTargetKind `json:"target_kind"`
	TargetID               string                   `json:"target_id"`
	DeliveryKind           enum.DeliveryKind        `json:"delivery_kind"`
	RouteID                string                   `json:"route_id"`
	ScopeType              enum.ScopeType           `json:"scope_type"`
	ScopeRef               string                   `json:"scope_ref"`
	CorrelationID          string                   `json:"correlation_id,omitempty"`
	DeadlineAt             string                   `json:"deadline_at,omitempty"`
	ReminderPolicyRef      string                   `json:"reminder_policy_ref,omitempty"`
	NotificationPolicyRef  string                   `json:"notification_policy_ref,omitempty"`
	RoutingPolicyRef       string                   `json:"routing_policy_ref,omitempty"`
	ChannelCapabilityRef   string                   `json:"channel_capability_ref,omitempty"`
	PackageInstallationRef string                   `json:"package_installation_ref,omitempty"`
	PackageVersionRef      string                   `json:"package_version_ref,omitempty"`
	DeliveryCommandRef     string                   `json:"delivery_command_ref,omitempty"`
	CallbackRef            string                   `json:"callback_ref,omitempty"`
	CallbackRouteRef       string                   `json:"callback_route_ref,omitempty"`
	RuntimeRef             string                   `json:"runtime_ref,omitempty"`
	AttemptNumber          int32                    `json:"attempt_number"`
}

func deliveryPayloadDigest(target deliveryTargetContext, route entity.DeliveryRoute, attempt entity.DeliveryAttempt, correlationID string) string {
	fingerprint, err := fingerprintInput(deliveryDigestInput{
		TargetKind:             target.Target.Kind,
		TargetID:               target.Target.ID.String(),
		DeliveryKind:           target.DeliveryKind,
		RouteID:                route.ID.String(),
		ScopeType:              target.Scope.Type,
		ScopeRef:               target.Scope.Ref,
		CorrelationID:          correlationID,
		DeadlineAt:             timeProto(target.DeadlineAt),
		ReminderPolicyRef:      target.ReminderPolicyRef,
		NotificationPolicyRef:  target.NotificationPolicyRef,
		RoutingPolicyRef:       route.RoutingPolicyRef,
		ChannelCapabilityRef:   attempt.ChannelCapabilityRef,
		PackageInstallationRef: attempt.PackageInstallationRef,
		PackageVersionRef:      attempt.PackageVersionRef,
		DeliveryCommandRef:     attempt.DeliveryCommandRef,
		CallbackRef:            attempt.CallbackRef,
		CallbackRouteRef:       attempt.CallbackRouteRef,
		RuntimeRef:             attempt.RuntimeRef,
		AttemptNumber:          attempt.AttemptNumber,
	})
	if err != nil {
		return ""
	}
	return "sha256:" + fingerprint
}

func contextRef(refs []value.ExternalRef, kind string) string {
	for _, ref := range refs {
		if ref.Kind == kind {
			return ref.Ref
		}
	}
	return ""
}

func uuidProto(input *uuid.UUID) string {
	if input == nil || *input == uuid.Nil {
		return ""
	}
	return input.String()
}

func timeProto(input *time.Time) string {
	if input == nil || input.IsZero() {
		return ""
	}
	return input.UTC().Format(time.RFC3339Nano)
}

func optionalUUID(input uuid.UUID) *uuid.UUID {
	if input == uuid.Nil {
		return nil
	}
	value := input
	return &value
}

type expireCommandResultPayload struct {
	ExpiredRequestIDs []string `json:"expired_request_ids"`
}

type createInteractionRequestFingerprint struct {
	Meta    value.CommandMeta
	Kind    enum.InteractionRequestKind
	Request InteractionRequestDraftInput
}

func expireResultPayload(ids []uuid.UUID) ([]byte, error) {
	payload := expireCommandResultPayload{ExpiredRequestIDs: make([]string, 0, len(ids))}
	for _, id := range ids {
		payload.ExpiredRequestIDs = append(payload.ExpiredRequestIDs, id.String())
	}
	return json.Marshal(payload)
}

func parseExpireResultPayload(input []byte) ([]uuid.UUID, error) {
	if len(input) == 0 {
		return nil, nil
	}
	var payload expireCommandResultPayload
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	result := make([]uuid.UUID, 0, len(payload.ExpiredRequestIDs))
	for _, raw := range payload.ExpiredRequestIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, nil
}

func commandResult(meta value.CommandMeta, operation enum.Operation, aggregateType string, aggregateID uuid.UUID, fingerprint string, now time.Time) entity.CommandResult {
	return commandResultWithPayload(meta, operation, aggregateType, aggregateID, fingerprint, []byte(`{}`), now)
}

func commandResultWithPayload(meta value.CommandMeta, operation enum.Operation, aggregateType string, aggregateID uuid.UUID, fingerprint string, payload []byte, now time.Time) entity.CommandResult {
	actorRef := meta.Actor.Ref()
	key := "idempotency:" + string(operation) + ":" + actorRef + ":" + strings.TrimSpace(meta.IdempotencyKey)
	if meta.CommandID != uuid.Nil {
		key = "command:" + meta.CommandID.String()
	}
	return entity.CommandResult{
		Key:                key,
		CommandID:          meta.CommandID,
		IdempotencyKey:     strings.TrimSpace(meta.IdempotencyKey),
		ActorRef:           actorRef,
		Operation:          operation,
		AggregateType:      aggregateType,
		AggregateID:        aggregateID,
		RequestFingerprint: fingerprint,
		ResultPayload:      append([]byte(nil), payload...),
		CreatedAt:          now,
	}
}

func (s *Service) outboxEvent(eventType string, aggregateType string, aggregateID uuid.UUID, payload interactionevents.Payload, now time.Time) (entity.OutboxEvent, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return entity.OutboxEvent{
		Event:         outboxlib.NewEvent(s.ids.New(), eventType, interactionevents.SchemaVersion, aggregateType, aggregateID, body, now, 0),
		NextAttemptAt: time.Unix(0, 0).UTC(),
	}, nil
}

func fingerprintInput(input any) (string, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func blank(value string) bool {
	return strings.TrimSpace(value) == ""
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID {
	return uuid.New()
}
