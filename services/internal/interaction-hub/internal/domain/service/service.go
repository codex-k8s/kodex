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
	defaultExpireLimit         = int32(100)
	maxExpireLimit             = int32(500)
	aggregateResponse          = "response"
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
	event, err := s.outboxEvent(interactionevents.EventRequestResponseRecorded, interactionevents.AggregateRequest, request.ID, interactionevents.Payload{
		RequestID:        request.ID.String(),
		ResponseID:       response.ID.String(),
		ResponseAction:   string(response.ResponseAction),
		ActorRef:         response.RespondedByActorRef,
		OwnerService:     string(request.DecisionOwner.Kind),
		OwnerRequestRef:  request.DecisionOwner.OwnerRequestRef,
		OwnerDecisionRef: response.OwnerDecisionRef,
		Status:           string(request.Status),
		Version:          request.Version,
	}, now)
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

func (s *Service) RequestNotification(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationRequestNotification)
}

func (s *Service) UpsertSubscription(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationUpsertSubscription)
}

func (s *Service) DisableSubscription(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationDisableSubscription)
}

func (s *Service) ListSubscriptions(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationListSubscriptions)
}

func (s *Service) PlanDelivery(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationPlanDelivery)
}

func (s *Service) RecordDeliveryResult(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationRecordDeliveryResult)
}

func (s *Service) RecordChannelCallback(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationRecordChannelCallback)
}

func (s *Service) GetDeliveryStatus(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationGetDeliveryStatus)
}

func (s *Service) backlog(ctx context.Context, operation enum.Operation) error {
	if err := s.ensureReady(); err != nil {
		return err
	}
	if !operation.Valid() {
		return errs.ErrInvalidArgument
	}
	if err := s.repository.RecordBacklogOperation(ctx, operation); err != nil {
		return err
	}
	return errs.ErrNotImplemented
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

func normalizeActorRefs(input []value.ActorRef) ([]value.ActorRef, error) {
	if len(input) == 0 || len(input) > maxInteractionRefs {
		return nil, errs.ErrInvalidArgument
	}
	result := make([]value.ActorRef, 0, len(input))
	seen := map[string]struct{}{}
	for _, ref := range input {
		item := value.ActorRef{Kind: strings.TrimSpace(ref.Kind), Ref: strings.TrimSpace(ref.Ref)}
		if blank(item.Kind) || blank(item.Ref) || len(item.Kind) > maxInteractionRefBytes || len(item.Ref) > maxInteractionRefBytes {
			return nil, errs.ErrInvalidArgument
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

func contextRef(refs []value.ExternalRef, kind string) string {
	for _, ref := range refs {
		if ref.Kind == kind {
			return ref.Ref
		}
	}
	return ""
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
