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

func TestServiceRequestsNotificationWithOutboxAndIdempotentReplay(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}}})
	input := validNotificationInput(now.Add(time.Hour))

	notification, err := svc.RequestNotification(context.Background(), input)
	if err != nil {
		t.Fatalf("RequestNotification(): %v", err)
	}
	if notification.NotificationKind != enum.NotificationKindAttention || notification.Status != enum.NotificationStatusCreated {
		t.Fatalf("notification = %+v, want attention created", notification)
	}
	if notification.MessageTitle != "Safe title" || notification.BodyPreview != "Safe bounded preview" || notification.SourceOwner.Kind != enum.SourceOwnerKindAgentManager {
		t.Fatalf("notification = %+v, want persisted safe title/body/source owner", notification)
	}
	if len(repository.events) != 1 {
		t.Fatalf("events = %d, want 1", len(repository.events))
	}
	var payload map[string]any
	if err := json.Unmarshal(repository.events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal outbox payload: %v", err)
	}
	if payload["notification_id"] != notification.ID.String() || payload["source_owner_kind"] != "agent_manager" || payload["priority"] != "high" {
		t.Fatalf("payload = %+v, want notification safe refs", payload)
	}
	for _, unsafeField := range []string{"message_summary", "message_title", "body_preview"} {
		if _, ok := payload[unsafeField]; ok {
			t.Fatalf("outbox payload contains %s: %+v", unsafeField, payload)
		}
	}

	replayed, err := svc.RequestNotification(context.Background(), input)
	if err != nil {
		t.Fatalf("RequestNotification() replay: %v", err)
	}
	if replayed.ID != notification.ID || len(repository.events) != 1 {
		t.Fatalf("replay notification = %+v events=%d, want original notification and no extra event", replayed, len(repository.events))
	}
}

func TestServiceUpsertsListsAndDisablesSubscription(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}}})
	createInput := validSubscriptionInput()

	subscription, err := svc.UpsertSubscription(context.Background(), createInput)
	if err != nil {
		t.Fatalf("UpsertSubscription() create: %v", err)
	}
	if subscription.Status != enum.SubscriptionStatusActive || subscription.Version != 1 || subscription.SubscriptionPolicyRef != "policy:ops-notifications" {
		t.Fatalf("subscription = %+v, want active v1 with policy ref", subscription)
	}
	replayed, err := svc.UpsertSubscription(context.Background(), createInput)
	if err != nil {
		t.Fatalf("UpsertSubscription() replay: %v", err)
	}
	if replayed.ID != subscription.ID || len(repository.events) != 1 {
		t.Fatalf("replay subscription = %+v events=%d, want original subscription", replayed, len(repository.events))
	}

	updateInput := createInput
	updateInput.Meta = validVersionedCommandMeta(1)
	updateInput.SubscriptionID = subscription.ID
	updateInput.Status = enum.SubscriptionStatusPaused
	updateInput.DeliveryPreferencesJSON = `{"surfaces":["web_console"],"quiet_hours_policy_ref":"policy:quiet"}`
	updated, err := svc.UpsertSubscription(context.Background(), updateInput)
	if err != nil {
		t.Fatalf("UpsertSubscription() update: %v", err)
	}
	if updated.Status != enum.SubscriptionStatusPaused || updated.Version != 2 {
		t.Fatalf("updated subscription = %+v, want paused v2", updated)
	}
	listed, _, err := svc.ListSubscriptions(context.Background(), ListSubscriptionsInput{
		Scope:         createInput.Scope,
		SubscriberRef: createInput.SubscriberRef.String(),
		Status:        enum.SubscriptionStatusPaused,
	})
	if err != nil {
		t.Fatalf("ListSubscriptions(): %v", err)
	}
	if len(listed) != 1 || listed[0].ID != subscription.ID {
		t.Fatalf("listed = %+v, want updated subscription", listed)
	}

	staleInput := updateInput
	staleInput.Meta = validVersionedCommandMeta(1)
	staleInput.Meta.CommandID = uuid.New()
	staleInput.Status = enum.SubscriptionStatusActive
	if _, err := svc.UpsertSubscription(context.Background(), staleInput); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("UpsertSubscription() stale err = %v, want ErrConflict", err)
	}

	disabled, err := svc.DisableSubscription(context.Background(), DisableSubscriptionInput{Meta: validVersionedCommandMeta(2), SubscriptionID: subscription.ID})
	if err != nil {
		t.Fatalf("DisableSubscription(): %v", err)
	}
	if disabled.Status != enum.SubscriptionStatusDisabled || disabled.Version != 3 {
		t.Fatalf("disabled subscription = %+v, want disabled v3", disabled)
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

	notificationInput := validNotificationInput(now.Add(time.Hour))
	notificationInput.MessageSummary = strings.Repeat("n", maxMessageBodySummaryRunes+1)
	if _, err := svc.RequestNotification(context.Background(), notificationInput); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RequestNotification() err = %v, want ErrInvalidArgument", err)
	}
	if len(repository.notifications) != 0 {
		t.Fatalf("notifications=%d, want no notification writes", len(repository.notifications))
	}

	notificationInput = validNotificationInput(now.Add(time.Hour))
	notificationInput.SourceOwner.Ref = strings.Repeat("x", maxInteractionRefBytes+1)
	if _, err := svc.RequestNotification(context.Background(), notificationInput); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RequestNotification() oversized source owner err = %v, want ErrInvalidArgument", err)
	}
	if len(repository.notifications) != 0 {
		t.Fatalf("notifications=%d, want no notification writes", len(repository.notifications))
	}

	subscriptionInput := validSubscriptionInput()
	subscriptionInput.EventFilterJSON = `["not-an-object"]`
	if _, err := svc.UpsertSubscription(context.Background(), subscriptionInput); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("UpsertSubscription() err = %v, want ErrInvalidArgument", err)
	}
	if len(repository.subscriptions) != 0 {
		t.Fatalf("subscriptions=%d, want no subscription writes", len(repository.subscriptions))
	}

	subscriptionInput = validSubscriptionInput()
	subscriptionInput.DeliveryPreferencesJSON = `{"bot_token":"secret"}`
	if _, err := svc.UpsertSubscription(context.Background(), subscriptionInput); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("UpsertSubscription() secret policy err = %v, want ErrInvalidArgument", err)
	}
	if len(repository.subscriptions) != 0 {
		t.Fatalf("subscriptions=%d, want no subscription writes", len(repository.subscriptions))
	}
}

func TestServicePlansDeliveryWithSafeOutboxAndIdempotentReplay(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	routeID := uuid.New()
	seedInteractionRequest(repository, requestID, now, enum.InteractionRequestStatusWaiting)
	seedDeliveryRoute(repository, routeID, now)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}}})

	input := validPlanDeliveryInput(requestID, routeID)
	attempt, err := svc.PlanDelivery(context.Background(), input)
	if err != nil {
		t.Fatalf("PlanDelivery(): %v", err)
	}
	if attempt.Target.ID != requestID || attempt.RouteID != routeID || attempt.Status != enum.DeliveryAttemptStatusQueued || attempt.AttemptNumber != 1 {
		t.Fatalf("attempt = %+v, want queued delivery for request", attempt)
	}
	if !strings.HasPrefix(attempt.PayloadDigest, "sha256:") {
		t.Fatalf("payload digest = %q, want sha256 digest", attempt.PayloadDigest)
	}
	if attempt.ChannelCapabilityRef != "capability:channel" ||
		attempt.PackageInstallationRef != "package-installation:channel-core" ||
		attempt.PackageVersionRef != "package-version:channel-core:v1" ||
		attempt.DeliveryCommandRef == "" ||
		attempt.CallbackRef == "" ||
		attempt.CallbackRouteRef != "callback-route:interaction-channel" ||
		attempt.RuntimeRef != "runtime:channel-core" {
		t.Fatalf("attempt refs = %+v, want channel package refs and command envelope refs", attempt)
	}
	if len(repository.events) != 1 {
		t.Fatalf("events = %d, want 1", len(repository.events))
	}
	var payload map[string]any
	if err := json.Unmarshal(repository.events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal delivery event: %v", err)
	}
	if payload["delivery_attempt_id"] != attempt.ID.String() || payload["route_id"] != routeID.String() || payload["status"] != "queued" {
		t.Fatalf("payload = %+v, want safe delivery refs", payload)
	}
	if payload["delivery_command_ref"] != attempt.DeliveryCommandRef || payload["channel_capability_ref"] != attempt.ChannelCapabilityRef {
		t.Fatalf("payload = %+v, want safe channel contract refs", payload)
	}
	if _, ok := payload["prompt_summary"]; ok {
		t.Fatalf("outbox payload contains prompt_summary: %+v", payload)
	}

	replayed, err := svc.PlanDelivery(context.Background(), input)
	if err != nil {
		t.Fatalf("PlanDelivery() replay: %v", err)
	}
	if replayed.ID != attempt.ID || len(repository.events) != 1 {
		t.Fatalf("replay attempt = %+v events=%d, want original attempt and no extra event", replayed, len(repository.events))
	}
}

func TestServiceRecordsDeliveryResultAndBlocksTerminalRollback(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	routeID := uuid.New()
	seedInteractionRequest(repository, requestID, now, enum.InteractionRequestStatusWaiting)
	seedDeliveryRoute(repository, routeID, now)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}}})
	planned, err := svc.PlanDelivery(context.Background(), validPlanDeliveryInput(requestID, routeID))
	if err != nil {
		t.Fatalf("PlanDelivery(): %v", err)
	}
	input := RecordDeliveryResultInput{
		Meta: validCommandMeta(),
		Result: value.ChannelDeliveryResult{
			ContractVersion:    "interaction.channel.v1",
			DeliveryID:         planned.DeliveryID,
			ResultStatus:       enum.ChannelDeliveryResultStatusAccepted,
			ChannelMessageRef:  "channel:message-1",
			OccurredAt:         now.Add(2 * time.Minute),
			DeliveryCommandRef: planned.DeliveryCommandRef,
			RuntimeRef:         planned.RuntimeRef,
			RuntimeJobRef:      "runtime-job:delivery-1",
		},
	}
	accepted, err := svc.RecordDeliveryResult(context.Background(), input)
	if err != nil {
		t.Fatalf("RecordDeliveryResult(): %v", err)
	}
	if accepted.Status != enum.DeliveryAttemptStatusAccepted || accepted.ChannelMessageRef != "channel:message-1" || accepted.SentAt == nil || accepted.RuntimeJobRef != "runtime-job:delivery-1" {
		t.Fatalf("accepted = %+v, want accepted with channel ref", accepted)
	}
	if len(repository.events) != 2 {
		t.Fatalf("events = %d, want requested and accepted", len(repository.events))
	}
	var payload map[string]any
	if err := json.Unmarshal(repository.events[1].Payload, &payload); err != nil {
		t.Fatalf("unmarshal accepted event: %v", err)
	}
	if payload["channel_message_ref"] != "channel:message-1" || payload["delivery_id"] != planned.DeliveryID {
		t.Fatalf("accepted payload = %+v, want safe delivery result refs", payload)
	}

	replayed, err := svc.RecordDeliveryResult(context.Background(), input)
	if err != nil {
		t.Fatalf("RecordDeliveryResult() replay: %v", err)
	}
	if replayed.ID != planned.ID || len(repository.events) != 2 {
		t.Fatalf("replayed = %+v events=%d, want original accepted attempt and no extra event", replayed, len(repository.events))
	}

	retryWithNewMeta := input
	retryWithNewMeta.Meta = validCommandMeta()
	replayed, err = svc.RecordDeliveryResult(context.Background(), retryWithNewMeta)
	if err != nil {
		t.Fatalf("RecordDeliveryResult() delivery_id replay: %v", err)
	}
	if replayed.ID != planned.ID || len(repository.events) != 2 {
		t.Fatalf("delivery_id replay = %+v events=%d, want original accepted attempt and no extra event", replayed, len(repository.events))
	}

	stored := repository.deliveries[planned.ID]
	stored.Status = enum.DeliveryAttemptStatusFailed
	repository.deliveries[planned.ID] = stored
	retry := input
	retry.Meta = validCommandMeta()
	retry.Result.ResultStatus = enum.ChannelDeliveryResultStatusAccepted
	retry.Result.ChannelMessageRef = "channel:message-2"
	if _, err := svc.RecordDeliveryResult(context.Background(), retry); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("RecordDeliveryResult() terminal err = %v, want ErrConflict", err)
	}
}

func TestServiceRecordsTerminalDeliveryResultIdempotentlyByDeliveryID(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	routeID := uuid.New()
	seedInteractionRequest(repository, requestID, now, enum.InteractionRequestStatusWaiting)
	seedDeliveryRoute(repository, routeID, now)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}}})
	planned, err := svc.PlanDelivery(context.Background(), validPlanDeliveryInput(requestID, routeID))
	if err != nil {
		t.Fatalf("PlanDelivery(): %v", err)
	}
	input := RecordDeliveryResultInput{
		Meta: validCommandMeta(),
		Result: value.ChannelDeliveryResult{
			ContractVersion: "interaction.channel.v1",
			DeliveryID:      planned.DeliveryID,
			ResultStatus:    enum.ChannelDeliveryResultStatusFailed,
			ErrorCode:       "CHANNEL_UNAVAILABLE",
			ErrorClass:      enum.DeliveryErrorClassTemporary,
			RetryAfter:      ptrTime(now.Add(5 * time.Minute)),
			OccurredAt:      now.Add(2 * time.Minute),
		},
	}
	failed, err := svc.RecordDeliveryResult(context.Background(), input)
	if err != nil {
		t.Fatalf("RecordDeliveryResult(): %v", err)
	}
	if failed.Status != enum.DeliveryAttemptStatusFailed || failed.ResultFingerprint == "" {
		t.Fatalf("failed = %+v, want terminal failed with result fingerprint", failed)
	}

	retry := input
	retry.Meta = validCommandMeta()
	replayed, err := svc.RecordDeliveryResult(context.Background(), retry)
	if err != nil {
		t.Fatalf("RecordDeliveryResult() terminal replay: %v", err)
	}
	if replayed.ID != planned.ID || len(repository.events) != 2 {
		t.Fatalf("terminal replay = %+v events=%d, want stored result without extra event", replayed, len(repository.events))
	}

	changed := input
	changed.Meta = validCommandMeta()
	changed.Result.ErrorCode = "DIFFERENT"
	if _, err := svc.RecordDeliveryResult(context.Background(), changed); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("RecordDeliveryResult() changed terminal result err = %v, want ErrConflict", err)
	}
}

func TestServiceRecordsDeliveredAndExpiredDeliveryResults(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		status     enum.ChannelDeliveryResultStatus
		wantStatus enum.DeliveryAttemptStatus
		wantEvent  string
	}{
		{name: "delivered", status: enum.ChannelDeliveryResultStatusDelivered, wantStatus: enum.DeliveryAttemptStatusDelivered, wantEvent: "interaction.delivery.delivered"},
		{name: "expired", status: enum.ChannelDeliveryResultStatusExpired, wantStatus: enum.DeliveryAttemptStatusExpired, wantEvent: "interaction.delivery.expired"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repository := newFakeRepository()
			now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
			requestID := uuid.New()
			routeID := uuid.New()
			seedInteractionRequest(repository, requestID, now, enum.InteractionRequestStatusWaiting)
			seedDeliveryRoute(repository, routeID, now)
			svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}}})
			planned, err := svc.PlanDelivery(context.Background(), validPlanDeliveryInput(requestID, routeID))
			if err != nil {
				t.Fatalf("PlanDelivery(): %v", err)
			}
			updated, err := svc.RecordDeliveryResult(context.Background(), RecordDeliveryResultInput{
				Meta: validCommandMeta(),
				Result: value.ChannelDeliveryResult{
					ContractVersion:    "interaction.channel.v1",
					DeliveryID:         planned.DeliveryID,
					ResultStatus:       tc.status,
					ChannelMessageRef:  "channel:message-final",
					OccurredAt:         now.Add(2 * time.Minute),
					DeliveryCommandRef: planned.DeliveryCommandRef,
				},
			})
			if err != nil {
				t.Fatalf("RecordDeliveryResult(): %v", err)
			}
			if updated.Status != tc.wantStatus {
				t.Fatalf("status = %s, want %s", updated.Status, tc.wantStatus)
			}
			if len(repository.events) != 2 || repository.events[1].EventType != tc.wantEvent {
				t.Fatalf("events = %+v, want %s after requested", repository.events, tc.wantEvent)
			}
		})
	}
}

func TestServiceRejectsDeferredAndRejectedDeliveryResultsInIH6(t *testing.T) {
	t.Parallel()

	cases := []enum.ChannelDeliveryResultStatus{
		enum.ChannelDeliveryResultStatusDeferred,
		enum.ChannelDeliveryResultStatusRejected,
	}
	for _, status := range cases {
		status := status
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()

			repository := newFakeRepository()
			now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
			requestID := uuid.New()
			routeID := uuid.New()
			seedInteractionRequest(repository, requestID, now, enum.InteractionRequestStatusWaiting)
			seedDeliveryRoute(repository, routeID, now)
			svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}}})
			planned, err := svc.PlanDelivery(context.Background(), validPlanDeliveryInput(requestID, routeID))
			if err != nil {
				t.Fatalf("PlanDelivery(): %v", err)
			}
			_, err = svc.RecordDeliveryResult(context.Background(), RecordDeliveryResultInput{
				Meta: validCommandMeta(),
				Result: value.ChannelDeliveryResult{
					ContractVersion: "interaction.channel.v1",
					DeliveryID:      planned.DeliveryID,
					ResultStatus:    status,
					OccurredAt:      now.Add(2 * time.Minute),
					RetryAfter:      ptrTime(now.Add(5 * time.Minute)),
					ErrorClass:      enum.DeliveryErrorClassTemporary,
					ErrorCode:       "OUT_OF_SCOPE",
				},
			})
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Fatalf("RecordDeliveryResult() err = %v, want ErrInvalidArgument", err)
			}
			if repository.deliveries[planned.ID].Status != enum.DeliveryAttemptStatusQueued || len(repository.events) != 1 {
				t.Fatalf("delivery=%+v events=%d, want unchanged queued attempt", repository.deliveries[planned.ID], len(repository.events))
			}
		})
	}
}

func TestServiceReplaysDeliveryResultAfterAtomicClaimConflict(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	routeID := uuid.New()
	seedInteractionRequest(repository, requestID, now, enum.InteractionRequestStatusWaiting)
	seedDeliveryRoute(repository, routeID, now)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}}})
	planned, err := svc.PlanDelivery(context.Background(), validPlanDeliveryInput(requestID, routeID))
	if err != nil {
		t.Fatalf("PlanDelivery(): %v", err)
	}
	input := RecordDeliveryResultInput{
		Meta: validCommandMeta(),
		Result: value.ChannelDeliveryResult{
			ContractVersion:   "interaction.channel.v1",
			DeliveryID:        planned.DeliveryID,
			ResultStatus:      enum.ChannelDeliveryResultStatusAccepted,
			ChannelMessageRef: "channel:message-race",
			OccurredAt:        now.Add(2 * time.Minute),
		},
	}
	resultFingerprint, err := deliveryResultFingerprint(input.Result)
	if err != nil {
		t.Fatalf("deliveryResultFingerprint(): %v", err)
	}
	repository.beforeDeliveryUpdate = func(r *fakeRepository) {
		claimed := r.deliveries[planned.ID]
		claimed.Status = enum.DeliveryAttemptStatusAccepted
		claimed.ChannelMessageRef = input.Result.ChannelMessageRef
		claimed.ResultFingerprint = resultFingerprint
		claimed.SentAt = &input.Result.OccurredAt
		claimed.UpdatedAt = now.Add(2 * time.Minute)
		r.deliveries[planned.ID] = claimed
	}

	replayed, err := svc.RecordDeliveryResult(context.Background(), input)
	if err != nil {
		t.Fatalf("RecordDeliveryResult() concurrent replay: %v", err)
	}
	if replayed.ID != planned.ID || replayed.ResultFingerprint != resultFingerprint || len(repository.events) != 1 {
		t.Fatalf("concurrent replay = %+v events=%d, want claimed attempt without extra event", replayed, len(repository.events))
	}
}

func TestServiceConflictsDeliveryResultAfterAtomicClaimConflict(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	routeID := uuid.New()
	seedInteractionRequest(repository, requestID, now, enum.InteractionRequestStatusWaiting)
	seedDeliveryRoute(repository, routeID, now)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}}})
	planned, err := svc.PlanDelivery(context.Background(), validPlanDeliveryInput(requestID, routeID))
	if err != nil {
		t.Fatalf("PlanDelivery(): %v", err)
	}
	input := RecordDeliveryResultInput{
		Meta: validCommandMeta(),
		Result: value.ChannelDeliveryResult{
			ContractVersion:   "interaction.channel.v1",
			DeliveryID:        planned.DeliveryID,
			ResultStatus:      enum.ChannelDeliveryResultStatusAccepted,
			ChannelMessageRef: "channel:message-loser",
			OccurredAt:        now.Add(2 * time.Minute),
		},
	}
	winning := input.Result
	winning.ChannelMessageRef = "channel:message-winner"
	winningFingerprint, err := deliveryResultFingerprint(winning)
	if err != nil {
		t.Fatalf("deliveryResultFingerprint(): %v", err)
	}
	repository.beforeDeliveryUpdate = func(r *fakeRepository) {
		claimed := r.deliveries[planned.ID]
		claimed.Status = enum.DeliveryAttemptStatusAccepted
		claimed.ChannelMessageRef = winning.ChannelMessageRef
		claimed.ResultFingerprint = winningFingerprint
		claimed.SentAt = &winning.OccurredAt
		claimed.UpdatedAt = now.Add(2 * time.Minute)
		r.deliveries[planned.ID] = claimed
	}

	if _, err := svc.RecordDeliveryResult(context.Background(), input); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("RecordDeliveryResult() concurrent conflict err = %v, want ErrConflict", err)
	}
	if len(repository.events) != 1 {
		t.Fatalf("events=%d, want no extra event after conflict", len(repository.events))
	}
}

func TestServiceGetsDeliveryStatusByDeliveryID(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	notificationID := uuid.New()
	routeID := uuid.New()
	notification := validNotificationInput(now.Add(time.Hour))
	notification.Meta = validCommandMeta()
	createdNotification, err := NewWithConfig(repository, Config{Clock: fixedClock{now: now}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{notificationID, uuid.New()}}}).RequestNotification(context.Background(), notification)
	if err != nil {
		t.Fatalf("RequestNotification(): %v", err)
	}
	seedDeliveryRoute(repository, routeID, now)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}}})
	planned, err := svc.PlanDelivery(context.Background(), PlanDeliveryInput{
		Meta:    validCommandMeta(),
		Target:  value.DeliveryTarget{Kind: value.DeliveryTargetKindNotification, ID: createdNotification.ID},
		RouteID: routeID,
	})
	if err != nil {
		t.Fatalf("PlanDelivery(): %v", err)
	}
	status, err := svc.GetDeliveryStatus(context.Background(), GetDeliveryStatusInput{DeliveryID: planned.DeliveryID})
	if err != nil {
		t.Fatalf("GetDeliveryStatus(): %v", err)
	}
	if status.Notification == nil || status.Notification.ID != createdNotification.ID || len(status.DeliveryAttempts) != 1 || status.DeliveryAttempts[0].ID != planned.ID {
		t.Fatalf("status = %+v, want notification and planned attempt", status)
	}
}

func TestServiceRecordsChannelCallbackWithSafeOutboxAndReplay(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	routeID := uuid.New()
	seedInteractionRequest(repository, requestID, now, enum.InteractionRequestStatusWaiting)
	seedDeliveryRoute(repository, routeID, now)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}}})
	planned, err := svc.PlanDelivery(context.Background(), validPlanDeliveryInput(requestID, routeID))
	if err != nil {
		t.Fatalf("PlanDelivery(): %v", err)
	}
	input := validRecordChannelCallbackInput(planned.DeliveryID, requestID)
	result, err := svc.RecordChannelCallback(context.Background(), input)
	if err != nil {
		t.Fatalf("RecordChannelCallback(): %v", err)
	}
	callback := result.Callback
	if callback.CallbackID != input.Callback.CallbackID || callback.DeliveryAttemptID == nil || *callback.DeliveryAttemptID != planned.ID || callback.RequestID == nil || *callback.RequestID != requestID {
		t.Fatalf("callback = %+v, want linked callback", callback)
	}
	if callback.CallbackRouteRef != "callback-route:interaction-channel" || callback.ProcessingStatus != enum.CallbackProcessingStatusAccepted {
		t.Fatalf("callback = %+v, want accepted callback with route ref", callback)
	}
	if result.Response == nil {
		t.Fatal("response is nil, want callback to resolve request")
	}
	if result.Response.RequestID != requestID ||
		result.Response.ResponseAction != enum.InteractionResponseActionApprove ||
		result.Response.SourceKind != enum.InteractionResponseSourceKindChannelCallback ||
		result.Response.SourceRef != callback.ID.String() {
		t.Fatalf("response = %+v, want channel callback response", result.Response)
	}
	if storedRequest := repository.requests[requestID]; storedRequest.Status != enum.InteractionRequestStatusAnswered || storedRequest.Version != 2 || storedRequest.ResolvedAt == nil {
		t.Fatalf("request = %+v, want answered v2", storedRequest)
	}
	if len(repository.responses) != 1 {
		t.Fatalf("responses=%d, want one response", len(repository.responses))
	}
	if len(repository.events) != 3 {
		t.Fatalf("events=%d, want requested, callback and response", len(repository.events))
	}
	var payload map[string]any
	if err := json.Unmarshal(repository.events[1].Payload, &payload); err != nil {
		t.Fatalf("unmarshal callback event: %v", err)
	}
	if payload["callback_id"] != callback.CallbackID || payload["delivery_id"] != planned.DeliveryID || payload["callback_route_ref"] != callback.CallbackRouteRef {
		t.Fatalf("payload = %+v, want safe callback refs", payload)
	}
	if _, ok := payload["answer_summary"]; ok {
		t.Fatalf("outbox payload contains answer_summary: %+v", payload)
	}
	var responsePayload map[string]any
	if err := json.Unmarshal(repository.events[2].Payload, &responsePayload); err != nil {
		t.Fatalf("unmarshal response event: %v", err)
	}
	if responsePayload["response_id"] != result.Response.ID.String() || responsePayload["status"] != string(enum.InteractionRequestStatusAnswered) {
		t.Fatalf("response payload = %+v, want safe response refs", responsePayload)
	}
	if _, ok := responsePayload["response_summary"]; ok {
		t.Fatalf("response payload contains response_summary: %+v", responsePayload)
	}

	replayed, err := svc.RecordChannelCallback(context.Background(), input)
	if err != nil {
		t.Fatalf("RecordChannelCallback() replay: %v", err)
	}
	if replayed.Callback.ID != callback.ID || replayed.Response == nil || replayed.Response.ID != result.Response.ID || len(repository.events) != 3 {
		t.Fatalf("replay callback = %+v response=%+v events=%d, want original callback/response and no extra event", replayed.Callback, replayed.Response, len(repository.events))
	}

	retryWithNewReceivedAt := input
	retryWithNewReceivedAt.Meta = validCommandMeta()
	retryWithNewReceivedAt.Callback.ReceivedAt = input.Callback.ReceivedAt.Add(time.Minute)
	replayed, err = svc.RecordChannelCallback(context.Background(), retryWithNewReceivedAt)
	if err != nil {
		t.Fatalf("RecordChannelCallback() callback_id replay with new received_at: %v", err)
	}
	if replayed.Callback.ID != callback.ID || replayed.Response == nil || replayed.Response.ID != result.Response.ID || !replayed.Callback.ReceivedAt.Equal(input.Callback.ReceivedAt) || len(repository.events) != 3 {
		t.Fatalf("received_at replay callback = %+v response=%+v events=%d, want original callback/response and no extra event", replayed.Callback, replayed.Response, len(repository.events))
	}

	changed := input
	changed.Meta = validCommandMeta()
	changed.Callback.AnswerSummary = "different safe callback summary"
	if _, err := svc.RecordChannelCallback(context.Background(), changed); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("RecordChannelCallback() changed replay err = %v, want ErrConflict", err)
	}
}

func TestServiceValidatesDeliveryIDOnlyChannelCallbackAgainstRequest(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	routeID := uuid.New()
	seedInteractionRequest(repository, requestID, now, enum.InteractionRequestStatusWaiting)
	seedDeliveryRoute(repository, routeID, now)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}}})
	planned, err := svc.PlanDelivery(context.Background(), validPlanDeliveryInput(requestID, routeID))
	if err != nil {
		t.Fatalf("PlanDelivery(): %v", err)
	}
	input := validRecordChannelCallbackInput(planned.DeliveryID, requestID)
	input.Callback.RequestRef = ""

	result, err := svc.RecordChannelCallback(context.Background(), input)
	if err != nil {
		t.Fatalf("RecordChannelCallback() delivery-id only: %v", err)
	}
	if result.Response == nil || result.Response.RequestID != requestID {
		t.Fatalf("result = %+v, want response resolved through delivery id", result)
	}
	if storedRequest := repository.requests[requestID]; storedRequest.Status != enum.InteractionRequestStatusAnswered {
		t.Fatalf("request = %+v, want answered", storedRequest)
	}
}

func TestServiceRejectsDeliveryIDOnlyChannelCallbackWithDiagnostic(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	routeID := uuid.New()
	seedInteractionRequest(repository, requestID, now, enum.InteractionRequestStatusWaiting)
	seedDeliveryRoute(repository, routeID, now)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}}})
	planned, err := svc.PlanDelivery(context.Background(), validPlanDeliveryInput(requestID, routeID))
	if err != nil {
		t.Fatalf("PlanDelivery(): %v", err)
	}
	input := validRecordChannelCallbackInput(planned.DeliveryID, requestID)
	input.Callback.RequestRef = ""
	input.Callback.Action = "unexpected_action"

	result, err := svc.RecordChannelCallback(context.Background(), input)
	if err != nil {
		t.Fatalf("RecordChannelCallback() invalid action: %v", err)
	}
	if result.Response != nil || result.Callback.ProcessingStatus != enum.CallbackProcessingStatusRejected || result.Callback.ErrorCode != callbackErrorActionNotAllowed {
		t.Fatalf("result = %+v, want rejected diagnostic callback without response", result)
	}
	if storedRequest := repository.requests[requestID]; storedRequest.Status != enum.InteractionRequestStatusWaiting || storedRequest.Version != 1 {
		t.Fatalf("request = %+v, want unchanged waiting request", storedRequest)
	}
	if len(repository.callbacks) != 1 || len(repository.events) != 2 {
		t.Fatalf("callbacks=%d events=%d, want diagnostic callback write", len(repository.callbacks), len(repository.events))
	}
}

func TestServiceRecordsRejectedSignatureCallbackWithoutResponse(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	routeID := uuid.New()
	seedInteractionRequest(repository, requestID, now, enum.InteractionRequestStatusWaiting)
	seedDeliveryRoute(repository, routeID, now)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}}})
	planned, err := svc.PlanDelivery(context.Background(), validPlanDeliveryInput(requestID, routeID))
	if err != nil {
		t.Fatalf("PlanDelivery(): %v", err)
	}
	input := validRecordChannelCallbackInput(planned.DeliveryID, requestID)
	input.Callback.SignatureStatus = enum.CallbackSignatureStatusRejectedBeforeDomain

	result, err := svc.RecordChannelCallback(context.Background(), input)
	if err != nil {
		t.Fatalf("RecordChannelCallback() rejected signature: %v", err)
	}
	if result.Response != nil || result.Callback.ProcessingStatus != enum.CallbackProcessingStatusRejected || result.Callback.ErrorCode != callbackErrorRejected {
		t.Fatalf("result = %+v, want rejected callback without response", result)
	}
	if storedRequest := repository.requests[requestID]; storedRequest.Status != enum.InteractionRequestStatusWaiting || storedRequest.Version != 1 {
		t.Fatalf("request = %+v, want unchanged waiting request", storedRequest)
	}
}

func TestServiceRejectsDeliveryIDOnlyChannelCallbackForTerminalRequest(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	routeID := uuid.New()
	seedInteractionRequest(repository, requestID, now, enum.InteractionRequestStatusWaiting)
	seedDeliveryRoute(repository, routeID, now)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}}})
	planned, err := svc.PlanDelivery(context.Background(), validPlanDeliveryInput(requestID, routeID))
	if err != nil {
		t.Fatalf("PlanDelivery(): %v", err)
	}
	request := repository.requests[requestID]
	request.Status = enum.InteractionRequestStatusAnswered
	repository.requests[requestID] = request
	input := validRecordChannelCallbackInput(planned.DeliveryID, requestID)
	input.Callback.RequestRef = ""

	result, err := svc.RecordChannelCallback(context.Background(), input)
	if err != nil {
		t.Fatalf("RecordChannelCallback() terminal request: %v", err)
	}
	if result.Response != nil || result.Callback.ProcessingStatus != enum.CallbackProcessingStatusRejected || result.Callback.ErrorCode != callbackErrorRequestResolved {
		t.Fatalf("result = %+v, want request-resolved diagnostic callback", result)
	}
	if len(repository.callbacks) != 1 || len(repository.events) != 2 {
		t.Fatalf("callbacks=%d events=%d, want diagnostic callback write", len(repository.callbacks), len(repository.events))
	}
}

func TestServiceRejectsUnsafeChannelCallbackEnvelope(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	routeID := uuid.New()
	seedInteractionRequest(repository, requestID, now, enum.InteractionRequestStatusWaiting)
	seedDeliveryRoute(repository, routeID, now)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}}})
	planned, err := svc.PlanDelivery(context.Background(), validPlanDeliveryInput(requestID, routeID))
	if err != nil {
		t.Fatalf("PlanDelivery(): %v", err)
	}
	input := validRecordChannelCallbackInput(planned.DeliveryID, requestID)
	input.Callback.AnswerSummary = "Authorization: bearer secret-token"
	if _, err := svc.RecordChannelCallback(context.Background(), input); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RecordChannelCallback() err = %v, want ErrInvalidArgument", err)
	}
	if len(repository.callbacks) != 0 {
		t.Fatalf("callbacks=%d, want no callback writes", len(repository.callbacks))
	}
}

func TestServiceGetsLatestCallbackInDeliveryStatus(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	routeID := uuid.New()
	seedInteractionRequest(repository, requestID, now, enum.InteractionRequestStatusWaiting)
	seedDeliveryRoute(repository, routeID, now)
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}}})
	planned, err := svc.PlanDelivery(context.Background(), validPlanDeliveryInput(requestID, routeID))
	if err != nil {
		t.Fatalf("PlanDelivery(): %v", err)
	}
	callback, err := svc.RecordChannelCallback(context.Background(), validRecordChannelCallbackInput(planned.DeliveryID, requestID))
	if err != nil {
		t.Fatalf("RecordChannelCallback(): %v", err)
	}
	status, err := svc.GetDeliveryStatus(context.Background(), GetDeliveryStatusInput{DeliveryID: planned.DeliveryID})
	if err != nil {
		t.Fatalf("GetDeliveryStatus(): %v", err)
	}
	if status.LatestCallback == nil || status.LatestCallback.ID != callback.Callback.ID {
		t.Fatalf("status latest callback = %+v, want callback %s", status.LatestCallback, callback.Callback.ID)
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

func TestServiceRecordChannelCallbackRequiresReadyRepository(t *testing.T) {
	t.Parallel()

	_, err := New(&fakeRepository{}).RecordChannelCallback(context.Background(), validRecordChannelCallbackInput("delivery-1", uuid.New()))
	if !errors.Is(err, errs.ErrUnavailable) {
		t.Fatalf("RecordChannelCallback() err = %v, want ErrUnavailable", err)
	}
}

type fakeRepository struct {
	ready                bool
	operations           []enum.Operation
	threads              map[uuid.UUID]entity.ConversationThread
	messages             map[uuid.UUID]entity.ConversationMessage
	requests             map[uuid.UUID]entity.InteractionRequest
	responses            map[uuid.UUID]entity.InteractionResponse
	notifications        map[uuid.UUID]entity.Notification
	subscriptions        map[uuid.UUID]entity.Subscription
	routes               map[uuid.UUID]entity.DeliveryRoute
	deliveries           map[uuid.UUID]entity.DeliveryAttempt
	callbacks            map[uuid.UUID]entity.ChannelCallback
	results              map[string]entity.CommandResult
	events               []entity.OutboxEvent
	beforeDeliveryUpdate func(*fakeRepository)
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		ready:         true,
		threads:       map[uuid.UUID]entity.ConversationThread{},
		messages:      map[uuid.UUID]entity.ConversationMessage{},
		requests:      map[uuid.UUID]entity.InteractionRequest{},
		responses:     map[uuid.UUID]entity.InteractionResponse{},
		notifications: map[uuid.UUID]entity.Notification{},
		subscriptions: map[uuid.UUID]entity.Subscription{},
		routes:        map[uuid.UUID]entity.DeliveryRoute{},
		deliveries:    map[uuid.UUID]entity.DeliveryAttempt{},
		callbacks:     map[uuid.UUID]entity.ChannelCallback{},
		results:       map[string]entity.CommandResult{},
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

func validNotificationInput(expiresAt time.Time) RequestNotificationInput {
	return RequestNotificationInput{
		Meta:             validCommandMeta(),
		Scope:            value.ScopeRef{Type: enum.ScopeTypeService, Ref: "agent-manager"},
		NotificationKind: enum.NotificationKindAttention,
		RecipientRefs: []value.ActorRef{
			{Kind: "user", Ref: "owner-1"},
		},
		MessageTemplateRef: "interaction.notification.attention",
		MessageTitle:       "Safe title",
		MessageSummary:     "Safe bounded summary",
		BodyPreview:        "Safe bounded preview",
		Priority:           enum.NotificationPriorityHigh,
		ExpiresAt:          &expiresAt,
		SourceOwner:        value.SourceOwnerRef{Kind: enum.SourceOwnerKindAgentManager, Ref: "run:123"},
		Ingress:            value.IngressRef{Kind: enum.IngressKindDirectGRPC, Ref: "grpc:notify-1"},
		ContextRefs: []value.ExternalRef{
			{Kind: "agent_run", Ref: "run:123"},
		},
		ChannelHintRefs: []value.ExternalRef{
			{Kind: "surface", Ref: "web_console"},
		},
		NotificationPolicyRef: "policy:notify-standard",
	}
}

func validSubscriptionInput() UpsertSubscriptionInput {
	return UpsertSubscriptionInput{
		Meta:                    validCommandMeta(),
		Scope:                   value.ScopeRef{Type: enum.ScopeTypeService, Ref: "agent-manager"},
		SubscriberRef:           value.ActorRef{Kind: "user", Ref: "owner-1"},
		EventFilterJSON:         `{"event_kind":["run_waiting"],"severity":["high"]}`,
		DeliveryPreferencesJSON: `{"surfaces":["web_console"],"fallback_policy_ref":"policy:fallback"}`,
		Status:                  enum.SubscriptionStatusActive,
		SourceOwner:             value.SourceOwnerRef{Kind: enum.SourceOwnerKindAgentManager, Ref: "run:123"},
		ChannelHintRefs: []value.ExternalRef{
			{Kind: "surface", Ref: "web_console"},
		},
		SubscriptionPolicyRef: "policy:ops-notifications",
	}
}

func validPlanDeliveryInput(requestID uuid.UUID, routeID uuid.UUID) PlanDeliveryInput {
	return PlanDeliveryInput{
		Meta:          validCommandMeta(),
		Target:        value.DeliveryTarget{Kind: value.DeliveryTargetKindRequest, ID: requestID},
		RouteID:       routeID,
		CorrelationID: "trace-delivery",
	}
}

func validRecordChannelCallbackInput(deliveryID string, requestID uuid.UUID) RecordChannelCallbackInput {
	return RecordChannelCallbackInput{
		Meta: validCommandMeta(),
		Callback: value.ChannelCallbackEnvelope{
			ContractVersion: "interaction.channel.v1",
			CallbackID:      uuid.NewString(),
			DeliveryID:      deliveryID,
			RequestRef:      requestID.String(),
			ActorRef:        "user:approver-1",
			Action:          string(enum.InteractionResponseActionApprove),
			AnswerSummary:   "safe callback summary",
			SignatureStatus: enum.CallbackSignatureStatusVerified,
			GatewayRef:      "gateway:request-1",
			ReceivedAt:      time.Date(2026, 5, 26, 12, 10, 0, 0, time.UTC),
			CorrelationID:   "trace-callback",
		},
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
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

func seedDeliveryRoute(repository *fakeRepository, routeID uuid.UUID, now time.Time) {
	repository.routes[routeID] = entity.DeliveryRoute{
		ID:                     routeID,
		Scope:                  value.ScopeRef{Type: enum.ScopeTypeService, Ref: "agent-manager"},
		SurfaceKind:            enum.DeliverySurfaceKindChannelPackage,
		ChannelCapabilityRef:   "capability:channel",
		PackageInstallationRef: "package-installation:channel-core",
		PackageVersionRef:      "package-version:channel-core:v1",
		RoutingPolicyRef:       "policy:route-standard",
		CallbackRouteRef:       "callback-route:interaction-channel",
		RuntimeRef:             "runtime:channel-core",
		Status:                 enum.DeliveryRouteStatusActive,
		CreatedAt:              now,
		UpdatedAt:              now,
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

func (r *fakeRepository) CreateChannelCallbackResponseWithResult(_ context.Context, callback entity.ChannelCallback, response entity.InteractionResponse, request entity.InteractionRequest, previousRequestVersion int64, result entity.CommandResult, events []entity.OutboxEvent) error {
	stored, ok := r.requests[request.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if stored.Version != previousRequestVersion {
		return errs.ErrConflict
	}
	if _, ok := r.callbacks[callback.ID]; ok {
		return errs.ErrAlreadyExists
	}
	for _, existing := range r.callbacks {
		if existing.CallbackID == callback.CallbackID {
			return errs.ErrAlreadyExists
		}
	}
	for _, existing := range r.responses {
		if existing.RequestID == response.RequestID {
			return errs.ErrAlreadyExists
		}
		if existing.SourceKind == response.SourceKind && existing.SourceRef == response.SourceRef && response.SourceRef != "" {
			return errs.ErrAlreadyExists
		}
	}
	r.callbacks[callback.ID] = callback
	r.responses[response.ID] = response
	r.requests[request.ID] = request
	r.results[result.Key] = result
	r.events = append(r.events, events...)
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

func (r *fakeRepository) GetInteractionResponseBySource(_ context.Context, sourceKind enum.InteractionResponseSourceKind, sourceRef string) (entity.InteractionResponse, error) {
	for _, response := range r.responses {
		if response.SourceKind == sourceKind && response.SourceRef == sourceRef {
			return response, nil
		}
	}
	return entity.InteractionResponse{}, errs.ErrNotFound
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

func (r *fakeRepository) CreateNotificationWithResult(_ context.Context, notification entity.Notification, result entity.CommandResult, event entity.OutboxEvent) error {
	r.notifications[notification.ID] = notification
	r.results[result.Key] = result
	r.events = append(r.events, event)
	return nil
}

func (r *fakeRepository) GetNotification(_ context.Context, id uuid.UUID) (entity.Notification, error) {
	notification, ok := r.notifications[id]
	if !ok {
		return entity.Notification{}, errs.ErrNotFound
	}
	return notification, nil
}

func (r *fakeRepository) CreateSubscriptionWithResult(_ context.Context, subscription entity.Subscription, result entity.CommandResult, event entity.OutboxEvent) error {
	r.subscriptions[subscription.ID] = subscription
	r.results[result.Key] = result
	r.events = append(r.events, event)
	return nil
}

func (r *fakeRepository) UpdateSubscriptionWithResult(_ context.Context, subscription entity.Subscription, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	stored, ok := r.subscriptions[subscription.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if stored.Version != previousVersion {
		return errs.ErrConflict
	}
	r.subscriptions[subscription.ID] = subscription
	r.results[result.Key] = result
	r.events = append(r.events, event)
	return nil
}

func (r *fakeRepository) GetSubscription(_ context.Context, id uuid.UUID) (entity.Subscription, error) {
	subscription, ok := r.subscriptions[id]
	if !ok {
		return entity.Subscription{}, errs.ErrNotFound
	}
	return subscription, nil
}

func (r *fakeRepository) ListSubscriptions(_ context.Context, filter query.SubscriptionFilter) ([]entity.Subscription, value.PageResult, error) {
	subscriptions := make([]entity.Subscription, 0, len(r.subscriptions))
	for _, subscription := range r.subscriptions {
		if subscription.Scope != filter.Scope {
			continue
		}
		if filter.SubscriberRef != "" && subscription.SubscriberRef.String() != filter.SubscriberRef {
			continue
		}
		if filter.Status != "" && subscription.Status != filter.Status {
			continue
		}
		subscriptions = append(subscriptions, subscription)
	}
	return subscriptions, value.PageResult{}, nil
}

func (r *fakeRepository) CreateDeliveryAttemptWithResult(_ context.Context, attempt entity.DeliveryAttempt, result entity.CommandResult, event entity.OutboxEvent) error {
	if _, ok := r.deliveries[attempt.ID]; ok {
		return errs.ErrAlreadyExists
	}
	for _, existing := range r.deliveries {
		if existing.DeliveryID == attempt.DeliveryID {
			return errs.ErrAlreadyExists
		}
	}
	r.deliveries[attempt.ID] = attempt
	r.results[result.Key] = result
	r.events = append(r.events, event)
	return nil
}

func (r *fakeRepository) UpdateDeliveryAttemptWithResult(_ context.Context, attempt entity.DeliveryAttempt, result entity.CommandResult, event entity.OutboxEvent) error {
	if r.beforeDeliveryUpdate != nil {
		r.beforeDeliveryUpdate(r)
		r.beforeDeliveryUpdate = nil
	}
	stored, ok := r.deliveries[attempt.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if stored.Status.Terminal() || stored.ResultFingerprint != "" {
		return errs.ErrConflict
	}
	r.deliveries[attempt.ID] = attempt
	r.results[result.Key] = result
	r.events = append(r.events, event)
	return nil
}

func (r *fakeRepository) GetDeliveryRoute(_ context.Context, id uuid.UUID) (entity.DeliveryRoute, error) {
	route, ok := r.routes[id]
	if !ok {
		return entity.DeliveryRoute{}, errs.ErrNotFound
	}
	return route, nil
}

func (r *fakeRepository) FindActiveDeliveryRoute(_ context.Context, scope value.ScopeRef) (entity.DeliveryRoute, error) {
	for _, route := range r.routes {
		if route.Scope == scope && route.Status == enum.DeliveryRouteStatusActive {
			return route, nil
		}
	}
	return entity.DeliveryRoute{}, errs.ErrNotFound
}

func (r *fakeRepository) GetDeliveryAttempt(_ context.Context, id uuid.UUID) (entity.DeliveryAttempt, error) {
	attempt, ok := r.deliveries[id]
	if !ok {
		return entity.DeliveryAttempt{}, errs.ErrNotFound
	}
	return attempt, nil
}

func (r *fakeRepository) GetDeliveryAttemptByDeliveryID(_ context.Context, deliveryID string) (entity.DeliveryAttempt, error) {
	for _, attempt := range r.deliveries {
		if attempt.DeliveryID == deliveryID {
			return attempt, nil
		}
	}
	return entity.DeliveryAttempt{}, errs.ErrNotFound
}

func (r *fakeRepository) ListDeliveryAttempts(_ context.Context, filter query.DeliveryAttemptFilter) ([]entity.DeliveryAttempt, error) {
	deliveries := make([]entity.DeliveryAttempt, 0, len(r.deliveries))
	for _, attempt := range r.deliveries {
		if filter.Target.Valid() && attempt.Target != filter.Target {
			continue
		}
		if filter.DeliveryID != "" && attempt.DeliveryID != filter.DeliveryID {
			continue
		}
		deliveries = append(deliveries, attempt)
		if filter.Limit > 0 && int32(len(deliveries)) >= filter.Limit {
			break
		}
	}
	return deliveries, nil
}

func (r *fakeRepository) CreateChannelCallbackWithResult(_ context.Context, callback entity.ChannelCallback, result entity.CommandResult, event entity.OutboxEvent) error {
	if _, ok := r.callbacks[callback.ID]; ok {
		return errs.ErrAlreadyExists
	}
	for _, existing := range r.callbacks {
		if existing.CallbackID == callback.CallbackID {
			return errs.ErrAlreadyExists
		}
	}
	r.callbacks[callback.ID] = callback
	r.results[result.Key] = result
	r.events = append(r.events, event)
	return nil
}

func (r *fakeRepository) GetChannelCallback(_ context.Context, id uuid.UUID) (entity.ChannelCallback, error) {
	callback, ok := r.callbacks[id]
	if !ok {
		return entity.ChannelCallback{}, errs.ErrNotFound
	}
	return callback, nil
}

func (r *fakeRepository) GetChannelCallbackByCallbackID(_ context.Context, callbackID string) (entity.ChannelCallback, error) {
	for _, callback := range r.callbacks {
		if callback.CallbackID == callbackID {
			return callback, nil
		}
	}
	return entity.ChannelCallback{}, errs.ErrNotFound
}

func (r *fakeRepository) GetLatestChannelCallback(_ context.Context, filter query.ChannelCallbackFilter) (entity.ChannelCallback, error) {
	var latest entity.ChannelCallback
	for _, callback := range r.callbacks {
		if !callbackMatchesFilter(callback, filter) {
			continue
		}
		if latest.ID == uuid.Nil || callback.CreatedAt.After(latest.CreatedAt) {
			latest = callback
		}
	}
	if latest.ID == uuid.Nil {
		return entity.ChannelCallback{}, errs.ErrNotFound
	}
	return latest, nil
}

func callbackMatchesFilter(callback entity.ChannelCallback, filter query.ChannelCallbackFilter) bool {
	if filter.DeliveryID != "" && callback.DeliveryID == filter.DeliveryID {
		return true
	}
	if filter.RequestID != uuid.Nil && callback.RequestID != nil && *callback.RequestID == filter.RequestID {
		return true
	}
	for _, id := range filter.DeliveryAttemptIDs {
		if callback.DeliveryAttemptID != nil && *callback.DeliveryAttemptID == id {
			return true
		}
	}
	return false
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
