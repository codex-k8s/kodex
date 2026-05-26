package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

func TestServiceCreatesThreadWithOutboxAndIdempotentReplay(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}}})
	input := CreateConversationThreadInput{
		Meta: value.CommandMeta{
			CommandID: uuid.New(),
			Actor:     value.Actor{Type: "service", ID: "agent-manager"},
			Reason:    "test",
			RequestID: "request-1",
		},
		Scope:           value.ScopeRef{Type: enum.ScopeTypeService, Ref: "agent-manager"},
		ThreadKind:      enum.ConversationThreadKindUserDialog,
		PrimaryActorRef: "service:agent-manager",
		SourceKind:      enum.ConversationSourceKindService,
		SourceRef:       "run:123",
		CorrelationID:   "trace-123",
		RetentionClass:  "standard",
	}

	thread, err := svc.CreateConversationThread(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateConversationThread(): %v", err)
	}
	if thread.Scope.Type != enum.ScopeTypeService || thread.Version != 1 {
		t.Fatalf("thread = %+v, want service scope v1", thread)
	}
	if len(repository.events) != 1 {
		t.Fatalf("events = %d, want 1", len(repository.events))
	}
	var payload map[string]any
	if err := json.Unmarshal(repository.events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal outbox payload: %v", err)
	}
	if payload["scope_type"] != "service" || payload["thread_id"] != thread.ID.String() {
		t.Fatalf("payload = %+v, want service thread payload", payload)
	}

	replayed, err := svc.CreateConversationThread(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateConversationThread() replay: %v", err)
	}
	if replayed.ID != thread.ID || len(repository.events) != 1 {
		t.Fatalf("replay thread = %+v events=%d, want original thread and no extra event", replayed, len(repository.events))
	}
}

func TestServiceRecordsMessageWithoutRawBodyOutbox(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	threadID := uuid.New()
	seedConversationThread(repository, threadID, now)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}}})
	objectSize := int64(128)
	input := RecordConversationMessageInput{
		Meta: value.CommandMeta{
			CommandID: uuid.New(),
			Actor:     value.Actor{Type: "agent", ID: "codex"},
			Reason:    "test",
			RequestID: "request-2",
		},
		ThreadID:     threadID,
		MessageKind:  enum.ConversationMessageKindAgentText,
		AuthorRef:    "agent:codex",
		BodySummary:  "summary that must stay outside the outbox",
		BodyObject:   value.ObjectRef{URI: "s3://kodex-interactions/messages/1", Digest: "sha256:object", SizeBytes: &objectSize},
		BodyDigest:   "sha256:body",
		Locale:       "ru",
		SafeMetadata: map[string]string{"surface": "mcp"},
	}

	message, err := svc.RecordConversationMessage(context.Background(), input)
	if err != nil {
		t.Fatalf("RecordConversationMessage(): %v", err)
	}
	if message.BodySummary != input.BodySummary {
		t.Fatalf("message summary = %q, want persisted summary", message.BodySummary)
	}
	if len(repository.events) != 1 {
		t.Fatalf("events = %d, want 1", len(repository.events))
	}
	var payload map[string]any
	if err := json.Unmarshal(repository.events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal outbox payload: %v", err)
	}
	if _, ok := payload["body_summary"]; ok {
		t.Fatalf("outbox payload contains body_summary: %+v", payload)
	}
	if payload["message_id"] != message.ID.String() || payload["thread_id"] != threadID.String() {
		t.Fatalf("payload = %+v, want message/thread refs", payload)
	}
}

func TestServiceRejectsUnsafeConversationMessages(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*RecordConversationMessageInput)
	}{
		{
			name: "body summary exceeds safe bound",
			mutate: func(input *RecordConversationMessageInput) {
				input.BodySummary = strings.Repeat("a", maxMessageBodySummaryRunes+1)
			},
		},
		{
			name: "blank body has no object ref and digest",
			mutate: func(input *RecordConversationMessageInput) {
				input.BodySummary = " "
				input.BodyObject = value.ObjectRef{}
				input.BodyDigest = ""
			},
		},
		{
			name: "body digest without object ref",
			mutate: func(input *RecordConversationMessageInput) {
				input.BodyObject = value.ObjectRef{}
			},
		},
		{
			name: "object ref without body digest",
			mutate: func(input *RecordConversationMessageInput) {
				input.BodyDigest = ""
			},
		},
		{
			name: "negative object size",
			mutate: func(input *RecordConversationMessageInput) {
				size := int64(-1)
				input.BodyObject.SizeBytes = &size
			},
		},
		{
			name: "sensitive metadata key",
			mutate: func(input *RecordConversationMessageInput) {
				input.SafeMetadata = map[string]string{"github_token": "redacted"}
			},
		},
		{
			name: "metadata value exceeds safe bound",
			mutate: func(input *RecordConversationMessageInput) {
				input.SafeMetadata = map[string]string{"surface": strings.Repeat("x", maxSafeMetadataValueBytes+1)}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repository := newFakeRepository()
			now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
			threadID := uuid.New()
			seedConversationThread(repository, threadID, now)
			svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}}})
			input := validRecordConversationMessageInput(threadID)
			tc.mutate(&input)

			_, err := svc.RecordConversationMessage(context.Background(), input)
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Fatalf("RecordConversationMessage() err = %v, want ErrInvalidArgument", err)
			}
			if len(repository.messages) != 0 || len(repository.events) != 0 {
				t.Fatalf("messages=%d events=%d, want no writes", len(repository.messages), len(repository.events))
			}
		})
	}
}

func TestServiceCreatesInteractionRequestWithOutboxAndIdempotentReplay(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}}})
	input := RequestApprovalInput{
		Meta:    validCommandMeta(),
		Request: validInteractionRequestDraft(now.Add(time.Hour)),
	}

	request, err := svc.RequestApproval(context.Background(), input)
	if err != nil {
		t.Fatalf("RequestApproval(): %v", err)
	}
	if request.RequestKind != enum.InteractionRequestKindApproval || request.Status != enum.InteractionRequestStatusWaiting || request.Version != 1 {
		t.Fatalf("request = %+v, want approval waiting v1", request)
	}
	if len(repository.events) != 1 {
		t.Fatalf("events = %d, want 1", len(repository.events))
	}
	var payload map[string]any
	if err := json.Unmarshal(repository.events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal outbox payload: %v", err)
	}
	if payload["request_id"] != request.ID.String() || payload["owner_request_ref"] != "gate:req-1" {
		t.Fatalf("payload = %+v, want safe request refs", payload)
	}
	if _, ok := payload["prompt_summary"]; ok {
		t.Fatalf("outbox payload contains prompt_summary: %+v", payload)
	}

	replayed, err := svc.RequestApproval(context.Background(), input)
	if err != nil {
		t.Fatalf("RequestApproval() replay: %v", err)
	}
	if replayed.ID != request.ID || len(repository.events) != 1 {
		t.Fatalf("replay request = %+v events=%d, want original request and no extra event", replayed, len(repository.events))
	}
}

func TestServiceRecordsInteractionResponseWithTerminalStatusAndIdempotency(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	seedInteractionRequest(repository, requestID, now, enum.InteractionRequestStatusWaiting)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}}})
	input := RecordInteractionResponseInput{
		Meta:                validVersionedCommandMeta(1),
		RequestID:           requestID,
		ResponseAction:      enum.InteractionResponseActionApprove,
		RespondedByActorRef: "user:approver-1",
		ResponseSummary:     "approved summary that must stay outside outbox",
		SourceKind:          enum.InteractionResponseSourceKindMCP,
		SourceRef:           "mcp:command-1",
		OwnerDecisionRef:    "decision:1",
	}

	request, response, err := svc.RecordInteractionResponse(context.Background(), input)
	if err != nil {
		t.Fatalf("RecordInteractionResponse(): %v", err)
	}
	if request.Status != enum.InteractionRequestStatusAnswered || request.Version != 2 || request.ResolvedAt == nil {
		t.Fatalf("request = %+v, want answered v2", request)
	}
	if response.ResponseAction != enum.InteractionResponseActionApprove || response.ResponseSummary != input.ResponseSummary {
		t.Fatalf("response = %+v, want persisted safe summary", response)
	}
	var payload map[string]any
	if err := json.Unmarshal(repository.events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal outbox payload: %v", err)
	}
	if payload["response_action"] != "approve" || payload["owner_decision_ref"] != "decision:1" {
		t.Fatalf("payload = %+v, want response refs", payload)
	}
	if _, ok := payload["response_summary"]; ok {
		t.Fatalf("outbox payload contains response_summary: %+v", payload)
	}

	replayedRequest, replayedResponse, err := svc.RecordInteractionResponse(context.Background(), input)
	if err != nil {
		t.Fatalf("RecordInteractionResponse() replay: %v", err)
	}
	if replayedRequest.ID != request.ID || replayedResponse.ID != response.ID || len(repository.events) != 1 {
		t.Fatalf("replay request=%+v response=%+v events=%d, want original response", replayedRequest, replayedResponse, len(repository.events))
	}

	conflictingInput := input
	conflictingInput.Meta = validVersionedCommandMeta(2)
	conflictingInput.Meta.CommandID = uuid.New()
	_, _, err = svc.RecordInteractionResponse(context.Background(), conflictingInput)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("second response err = %v, want ErrConflict", err)
	}
}

func TestServiceCancelsAndExpiresInteractionRequests(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	cancelID := uuid.New()
	expireID := uuid.New()
	futureID := uuid.New()
	seedInteractionRequest(repository, cancelID, now, enum.InteractionRequestStatusWaiting)
	seedInteractionRequest(repository, expireID, now, enum.InteractionRequestStatusWaiting)
	seedInteractionRequest(repository, futureID, now, enum.InteractionRequestStatusWaiting)
	pastDeadline := now.Add(-time.Minute)
	futureDeadline := now.Add(time.Hour)
	expiring := repository.requests[expireID]
	expiring.DeadlineAt = &pastDeadline
	repository.requests[expireID] = expiring
	future := repository.requests[futureID]
	future.DeadlineAt = &futureDeadline
	repository.requests[futureID] = future
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}}})

	cancelled, err := svc.CancelInteractionRequest(context.Background(), CancelInteractionRequestInput{Meta: validVersionedCommandMeta(1), RequestID: cancelID})
	if err != nil {
		t.Fatalf("CancelInteractionRequest(): %v", err)
	}
	if cancelled.Status != enum.InteractionRequestStatusCancelled || cancelled.Version != 2 {
		t.Fatalf("cancelled = %+v, want cancelled v2", cancelled)
	}

	expireInput := ExpireInteractionRequestsInput{
		Meta:           validCommandMeta(),
		Scope:          value.ScopeRef{Type: enum.ScopeTypeService, Ref: "agent-manager"},
		DeadlineBefore: &now,
		Limit:          10,
	}
	expired, err := svc.ExpireInteractionRequests(context.Background(), expireInput)
	if err != nil {
		t.Fatalf("ExpireInteractionRequests(): %v", err)
	}
	if len(expired.ExpiredRequestIDs) != 1 || expired.ExpiredRequestIDs[0] != expireID {
		t.Fatalf("expired = %+v, want only %s", expired, expireID)
	}
	stored := repository.requests[expireID]
	if stored.Status != enum.InteractionRequestStatusExpired || stored.Version != 2 {
		t.Fatalf("stored expired = %+v, want expired v2", stored)
	}
	replayed, err := svc.ExpireInteractionRequests(context.Background(), expireInput)
	if err != nil {
		t.Fatalf("ExpireInteractionRequests() replay: %v", err)
	}
	if len(replayed.ExpiredRequestIDs) != 1 || replayed.ExpiredRequestIDs[0] != expireID {
		t.Fatalf("replayed expired = %+v, want same request id", replayed)
	}
}

func TestServiceReplaysExpireInteractionRequestsWithoutDeadline(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	expireID := uuid.New()
	seedInteractionRequest(repository, expireID, now, enum.InteractionRequestStatusWaiting)
	pastDeadline := now.Add(-time.Minute)
	expiring := repository.requests[expireID]
	expiring.DeadlineAt = &pastDeadline
	repository.requests[expireID] = expiring
	input := ExpireInteractionRequestsInput{
		Meta:  validCommandMeta(),
		Scope: value.ScopeRef{Type: enum.ScopeTypeService, Ref: "agent-manager"},
		Limit: 10,
	}
	firstService := NewWithConfig(repository, Config{Clock: fixedClock{now: now}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New()}}})
	first, err := firstService.ExpireInteractionRequests(context.Background(), input)
	if err != nil {
		t.Fatalf("ExpireInteractionRequests(): %v", err)
	}
	if len(first.ExpiredRequestIDs) != 1 || first.ExpiredRequestIDs[0] != expireID {
		t.Fatalf("first expired = %+v, want %s", first, expireID)
	}

	replayService := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Hour)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New()}}})
	replayed, err := replayService.ExpireInteractionRequests(context.Background(), input)
	if err != nil {
		t.Fatalf("ExpireInteractionRequests() replay: %v", err)
	}
	if len(replayed.ExpiredRequestIDs) != 1 || replayed.ExpiredRequestIDs[0] != expireID {
		t.Fatalf("replayed expired = %+v, want same request id", replayed)
	}
	if len(repository.results) != 1 || len(repository.events) != 1 {
		t.Fatalf("results=%d events=%d, want replay without additional writes", len(repository.results), len(repository.events))
	}
}

func TestServiceRejectsUnsafeInteractionLifecycleInput(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}}})
	requestInput := RequestHumanGateInput{
		Meta:    validCommandMeta(),
		Request: validInteractionRequestDraft(now.Add(time.Hour)),
	}
	requestInput.Request.PromptSummary = strings.Repeat("a", maxMessageBodySummaryRunes+1)
	_, err := svc.RequestHumanGate(context.Background(), requestInput)
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RequestHumanGate() err = %v, want ErrInvalidArgument", err)
	}
	if len(repository.requests) != 0 || len(repository.events) != 0 {
		t.Fatalf("requests=%d events=%d, want no writes", len(repository.requests), len(repository.events))
	}

	requestID := uuid.New()
	seedInteractionRequest(repository, requestID, now, enum.InteractionRequestStatusWaiting)
	_, _, err = svc.RecordInteractionResponse(context.Background(), RecordInteractionResponseInput{
		Meta:                validVersionedCommandMeta(1),
		RequestID:           requestID,
		ResponseAction:      enum.InteractionResponseActionAnswer,
		RespondedByActorRef: "user:approver-1",
		SourceKind:          enum.InteractionResponseSourceKindMCP,
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RecordInteractionResponse() err = %v, want ErrInvalidArgument", err)
	}
	if len(repository.responses) != 0 {
		t.Fatalf("responses=%d, want no response writes", len(repository.responses))
	}
}

func TestServiceBacklogOperationsReturnUnimplemented(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	svc := New(repository)

	err := svc.RequestNotification(context.Background())
	if !errors.Is(err, errs.ErrNotImplemented) {
		t.Fatalf("RequestNotification() err = %v, want ErrNotImplemented", err)
	}
	if len(repository.operations) != 1 || repository.operations[0] != enum.OperationRequestNotification {
		t.Fatalf("operations = %v, want RequestNotification", repository.operations)
	}
}

func TestServiceReadinessDependsOnRepository(t *testing.T) {
	t.Parallel()

	if New(newFakeRepository()).Ready() != true {
		t.Fatal("Ready() = false, want true")
	}
	if New(&fakeRepository{}).Ready() != false {
		t.Fatal("Ready() = true, want false")
	}
}

func TestServiceBacklogRequiresReadyRepository(t *testing.T) {
	t.Parallel()

	err := New(&fakeRepository{}).RequestNotification(context.Background())
	if !errors.Is(err, errs.ErrUnavailable) {
		t.Fatalf("RequestNotification() err = %v, want ErrUnavailable", err)
	}
}

type fakeRepository struct {
	ready      bool
	operations []enum.Operation
	threads    map[uuid.UUID]entity.ConversationThread
	messages   map[uuid.UUID]entity.ConversationMessage
	requests   map[uuid.UUID]entity.InteractionRequest
	responses  map[uuid.UUID]entity.InteractionResponse
	results    map[string]entity.CommandResult
	events     []entity.OutboxEvent
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		ready:     true,
		threads:   map[uuid.UUID]entity.ConversationThread{},
		messages:  map[uuid.UUID]entity.ConversationMessage{},
		requests:  map[uuid.UUID]entity.InteractionRequest{},
		responses: map[uuid.UUID]entity.InteractionResponse{},
		results:   map[string]entity.CommandResult{},
	}
}

func seedConversationThread(repository *fakeRepository, threadID uuid.UUID, now time.Time) {
	repository.threads[threadID] = entity.ConversationThread{
		ID:             threadID,
		Scope:          value.ScopeRef{Type: enum.ScopeTypeRepository, Ref: "repo"},
		ThreadKind:     enum.ConversationThreadKindUserDialog,
		SourceKind:     enum.ConversationSourceKindMCP,
		Status:         enum.ConversationThreadStatusOpen,
		CorrelationID:  "trace",
		RetentionClass: "standard",
		Version:        1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func validRecordConversationMessageInput(threadID uuid.UUID) RecordConversationMessageInput {
	objectSize := int64(128)
	return RecordConversationMessageInput{
		Meta: value.CommandMeta{
			CommandID: uuid.New(),
			Actor:     value.Actor{Type: "agent", ID: "codex"},
			Reason:    "test",
			RequestID: "request-2",
		},
		ThreadID:     threadID,
		MessageKind:  enum.ConversationMessageKindAgentText,
		AuthorRef:    "agent:codex",
		BodySummary:  "safe summary",
		BodyObject:   value.ObjectRef{URI: "s3://kodex-interactions/messages/1", Digest: "sha256:object", SizeBytes: &objectSize},
		BodyDigest:   "sha256:body",
		Locale:       "ru",
		SafeMetadata: map[string]string{"surface": "mcp"},
	}
}

func validCommandMeta() value.CommandMeta {
	return value.CommandMeta{
		CommandID: uuid.New(),
		Actor:     value.Actor{Type: "service", ID: "agent-manager"},
		Reason:    "test",
		RequestID: "request-ih",
	}
}

func validVersionedCommandMeta(version int64) value.CommandMeta {
	meta := validCommandMeta()
	meta.ExpectedVersion = &version
	return meta
}

func validInteractionRequestDraft(deadline time.Time) InteractionRequestDraftInput {
	return InteractionRequestDraftInput{
		Scope:         value.ScopeRef{Type: enum.ScopeTypeService, Ref: "agent-manager"},
		SourceOwner:   value.SourceOwnerRef{Kind: enum.SourceOwnerKindAgentManager, Ref: "run:123"},
		Ingress:       value.IngressRef{Kind: enum.IngressKindDirectGRPC, Ref: "grpc:command-1"},
		DecisionOwner: value.DecisionOwnerRef{Kind: enum.DecisionOwnerKindGovernanceManager, OwnerRequestRef: "gate:req-1"},
		TargetRefs: []value.ActorRef{
			{Kind: "user", Ref: "approver-1"},
		},
		ContextRefs: []value.ExternalRef{
			{Kind: "agent_run", Ref: "run:123"},
			{Kind: "provider_operation", Ref: "provider:op-1"},
		},
		PromptSummary: "safe prompt summary",
		AllowedActions: []value.InteractionAction{
			{ActionKey: string(enum.InteractionResponseActionApprove), LabelTemplateRef: "interaction.actions.approve", Terminal: true},
			{ActionKey: string(enum.InteractionResponseActionDefer), LabelTemplateRef: "interaction.actions.defer", Terminal: false},
		},
		RiskClass:         enum.InteractionRiskClassHigh,
		DeadlineAt:        &deadline,
		ReminderPolicyRef: "policy:standard",
	}
}

func seedInteractionRequest(repository *fakeRepository, requestID uuid.UUID, now time.Time, status enum.InteractionRequestStatus) {
	deadline := now.Add(time.Hour)
	repository.requests[requestID] = entity.InteractionRequest{
		ID:            requestID,
		RequestKind:   enum.InteractionRequestKindApproval,
		Scope:         value.ScopeRef{Type: enum.ScopeTypeService, Ref: "agent-manager"},
		SourceOwner:   value.SourceOwnerRef{Kind: enum.SourceOwnerKindAgentManager, Ref: "run:123"},
		Ingress:       value.IngressRef{Kind: enum.IngressKindDirectGRPC, Ref: "grpc:command-1"},
		DecisionOwner: value.DecisionOwnerRef{Kind: enum.DecisionOwnerKindGovernanceManager, OwnerRequestRef: "gate:req-1"},
		TargetRefs: []value.ActorRef{
			{Kind: "user", Ref: "approver-1"},
		},
		ContextRefs: []value.ExternalRef{
			{Kind: "agent_run", Ref: "run:123"},
		},
		PromptSummary: "safe prompt summary",
		AllowedActions: []value.InteractionAction{
			{ActionKey: string(enum.InteractionResponseActionApprove), LabelTemplateRef: "interaction.actions.approve", Terminal: true},
		},
		RiskClass:  enum.InteractionRiskClassHigh,
		Status:     status,
		DeadlineAt: &deadline,
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func (r *fakeRepository) Ready() bool {
	return r.ready
}

func (r *fakeRepository) RecordBacklogOperation(_ context.Context, operation enum.Operation) error {
	r.operations = append(r.operations, operation)
	return nil
}

func (r *fakeRepository) CreateConversationThreadWithResult(_ context.Context, thread entity.ConversationThread, result entity.CommandResult, event entity.OutboxEvent) error {
	r.threads[thread.ID] = thread
	r.results[result.Key] = result
	r.events = append(r.events, event)
	return nil
}

func (r *fakeRepository) GetConversationThread(_ context.Context, id uuid.UUID) (entity.ConversationThread, error) {
	thread, ok := r.threads[id]
	if !ok {
		return entity.ConversationThread{}, errs.ErrNotFound
	}
	return thread, nil
}

func (r *fakeRepository) CreateConversationMessageWithResult(_ context.Context, message entity.ConversationMessage, thread entity.ConversationThread, previousThreadVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	stored, ok := r.threads[thread.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if stored.Version != previousThreadVersion {
		return errs.ErrConflict
	}
	r.messages[message.ID] = message
	r.threads[thread.ID] = thread
	r.results[result.Key] = result
	r.events = append(r.events, event)
	return nil
}

func (r *fakeRepository) GetConversationMessage(_ context.Context, id uuid.UUID) (entity.ConversationMessage, error) {
	message, ok := r.messages[id]
	if !ok {
		return entity.ConversationMessage{}, errs.ErrNotFound
	}
	return message, nil
}

func (r *fakeRepository) ListConversationMessages(_ context.Context, filter query.ConversationMessageFilter) ([]entity.ConversationMessage, value.PageResult, error) {
	messages := make([]entity.ConversationMessage, 0, len(r.messages))
	for _, message := range r.messages {
		if message.ThreadID == filter.ThreadID {
			messages = append(messages, message)
		}
	}
	return messages, value.PageResult{}, nil
}

func (r *fakeRepository) CreateInteractionRequestWithResult(_ context.Context, request entity.InteractionRequest, result entity.CommandResult, event entity.OutboxEvent) error {
	r.requests[request.ID] = request
	r.results[result.Key] = result
	r.events = append(r.events, event)
	return nil
}

func (r *fakeRepository) UpdateInteractionRequestWithResult(_ context.Context, request entity.InteractionRequest, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	stored, ok := r.requests[request.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if stored.Version != previousVersion {
		return errs.ErrConflict
	}
	r.requests[request.ID] = request
	r.results[result.Key] = result
	r.events = append(r.events, event)
	return nil
}

func (r *fakeRepository) UpdateInteractionRequestsWithResult(_ context.Context, requests []entity.InteractionRequest, previousVersions map[uuid.UUID]int64, result entity.CommandResult, events []entity.OutboxEvent) error {
	if len(requests) != len(events) {
		return errs.ErrInvalidArgument
	}
	for _, request := range requests {
		stored, ok := r.requests[request.ID]
		if !ok {
			return errs.ErrNotFound
		}
		if stored.Version != previousVersions[request.ID] {
			return errs.ErrConflict
		}
	}
	for _, request := range requests {
		r.requests[request.ID] = request
	}
	r.results[result.Key] = result
	r.events = append(r.events, events...)
	return nil
}

func (r *fakeRepository) CreateInteractionResponseWithResult(_ context.Context, response entity.InteractionResponse, request entity.InteractionRequest, previousRequestVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	stored, ok := r.requests[request.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if stored.Version != previousRequestVersion {
		return errs.ErrConflict
	}
	for _, existing := range r.responses {
		if existing.RequestID == response.RequestID {
			return errs.ErrAlreadyExists
		}
	}
	r.responses[response.ID] = response
	r.requests[request.ID] = request
	r.results[result.Key] = result
	r.events = append(r.events, event)
	return nil
}

func (r *fakeRepository) GetInteractionRequest(_ context.Context, id uuid.UUID) (entity.InteractionRequest, error) {
	request, ok := r.requests[id]
	if !ok {
		return entity.InteractionRequest{}, errs.ErrNotFound
	}
	return request, nil
}

func (r *fakeRepository) GetInteractionResponse(_ context.Context, id uuid.UUID) (entity.InteractionResponse, error) {
	response, ok := r.responses[id]
	if !ok {
		return entity.InteractionResponse{}, errs.ErrNotFound
	}
	return response, nil
}

func (r *fakeRepository) ListInteractionRequests(_ context.Context, filter query.InteractionRequestFilter) ([]entity.InteractionRequest, value.PageResult, error) {
	requests := make([]entity.InteractionRequest, 0, len(r.requests))
	for _, request := range r.requests {
		if request.Scope != filter.Scope {
			continue
		}
		if filter.RequestKind != "" && request.RequestKind != filter.RequestKind {
			continue
		}
		if filter.Status != "" && request.Status != filter.Status {
			continue
		}
		if filter.SourceOwnerKind != "" && request.SourceOwner.Kind != filter.SourceOwnerKind {
			continue
		}
		if filter.SourceOwnerRef != "" && request.SourceOwner.Ref != filter.SourceOwnerRef {
			continue
		}
		if filter.DeadlineBefore != nil && (request.DeadlineAt == nil || request.DeadlineAt.After(*filter.DeadlineBefore)) {
			continue
		}
		requests = append(requests, request)
	}
	return requests, value.PageResult{}, nil
}

func (r *fakeRepository) ListExpirableInteractionRequests(_ context.Context, scope value.ScopeRef, deadlineBefore time.Time, limit int32) ([]entity.InteractionRequest, error) {
	requests := make([]entity.InteractionRequest, 0, len(r.requests))
	for _, request := range r.requests {
		if request.Scope != scope || request.DeadlineAt == nil || request.DeadlineAt.After(deadlineBefore) || request.Status.Terminal() {
			continue
		}
		requests = append(requests, request)
		if int32(len(requests)) >= limit {
			break
		}
	}
	return requests, nil
}

func (r *fakeRepository) GetCommandResult(_ context.Context, identity query.CommandIdentity) (entity.CommandResult, error) {
	for _, result := range r.results {
		if identity.CommandID != uuid.Nil && result.CommandID == identity.CommandID {
			return result, nil
		}
		if identity.IdempotencyKey != "" &&
			result.IdempotencyKey == identity.IdempotencyKey &&
			result.ActorRef == identity.ActorRef &&
			result.Operation == identity.Operation {
			return result, nil
		}
	}
	return entity.CommandResult{}, errs.ErrNotFound
}

func (r *fakeRepository) ClaimOutboxEvents(context.Context, int, time.Time, time.Time) ([]entity.OutboxEvent, error) {
	return nil, errs.ErrNotImplemented
}

func (r *fakeRepository) MarkOutboxEventPublished(context.Context, uuid.UUID, int, time.Time) error {
	return errs.ErrNotImplemented
}

func (r *fakeRepository) MarkOutboxEventFailed(context.Context, uuid.UUID, int, time.Time, string) error {
	return errs.ErrNotImplemented
}

func (r *fakeRepository) MarkOutboxEventPermanentlyFailed(context.Context, uuid.UUID, int, time.Time, string) error {
	return errs.ErrNotImplemented
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type sequenceIDs struct {
	ids []uuid.UUID
}

func (g *sequenceIDs) New() uuid.UUID {
	if len(g.ids) == 0 {
		return uuid.New()
	}
	id := g.ids[0]
	g.ids = g.ids[1:]
	return id
}
