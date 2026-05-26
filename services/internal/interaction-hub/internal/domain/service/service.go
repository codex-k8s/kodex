package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

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
	if input.ThreadID == uuid.Nil || !input.MessageKind.Valid() || blank(input.AuthorRef) {
		return entity.ConversationMessage{}, errs.ErrInvalidArgument
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
		AuthorRef:    strings.TrimSpace(input.AuthorRef),
		BodySummary:  strings.TrimSpace(input.BodySummary),
		BodyObject:   input.BodyObject,
		BodyDigest:   strings.TrimSpace(input.BodyDigest),
		Locale:       strings.TrimSpace(input.Locale),
		SafeMetadata: copyMetadata(input.SafeMetadata),
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

func (s *Service) RequestFeedback(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationRequestFeedback)
}

func (s *Service) RequestApproval(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationRequestApproval)
}

func (s *Service) RequestHumanGate(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationRequestHumanGate)
}

func (s *Service) RecordInteractionResponse(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationRecordInteractionResponse)
}

func (s *Service) CancelInteractionRequest(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationCancelInteractionRequest)
}

func (s *Service) ExpireInteractionRequests(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationExpireInteractionRequests)
}

func (s *Service) GetInteractionRequest(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationGetInteractionRequest)
}

func (s *Service) ListInteractionRequests(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationListInteractionRequests)
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

func commandResult(meta value.CommandMeta, operation enum.Operation, aggregateType string, aggregateID uuid.UUID, fingerprint string, now time.Time) entity.CommandResult {
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
		ResultPayload:      []byte(`{}`),
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

func copyMetadata(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
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
