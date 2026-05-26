package interaction

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	migrationtest "github.com/codex-k8s/kodex/libs/go/migrationtest"
	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	interactionevents "github.com/codex-k8s/kodex/libs/go/platformevents/interaction"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

var sqlHeaderPattern = regexp.MustCompile(`^-- name: ([a-z0-9_]+__[a-z0-9_]+) :(one|many|exec)$`)

func TestSQLFilesHaveNamedHeaders(t *testing.T) {
	t.Parallel()

	files, err := fs.Glob(SQLFiles, "sql/*.sql")
	if err != nil {
		t.Fatalf("glob sql files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected embedded SQL files")
	}
	for _, file := range files {
		contentBytes, err := SQLFiles.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		firstLine, _, _ := strings.Cut(string(contentBytes), "\n")
		match := sqlHeaderPattern.FindStringSubmatch(firstLine)
		if match == nil {
			t.Fatalf("%s has invalid named query header: %q", file, firstLine)
		}
		queryName := strings.TrimSuffix(filepath.Base(file), ".sql")
		if match[1] != queryName {
			t.Fatalf("%s header query name = %s, want %s", file, match[1], queryName)
		}
	}
}

func TestRepositoryLoadsEverySQLFile(t *testing.T) {
	t.Parallel()

	files, err := fs.Glob(SQLFiles, "sql/*.sql")
	if err != nil {
		t.Fatalf("glob sql files: %v", err)
	}
	for _, file := range files {
		queryName := strings.TrimSuffix(filepath.Base(file), ".sql")
		query, err := loadQuery(queryName)
		if err != nil {
			t.Fatalf("load query %s: %v", queryName, err)
		}
		if strings.TrimSpace(query) == "" {
			t.Fatalf("query %s is empty", queryName)
		}
	}
}

func TestWrapErrorMapsPostgresErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want error
	}{
		{name: "not found", err: pgx.ErrNoRows, want: errs.ErrNotFound},
		{name: "unique", err: &pgconn.PgError{Code: "23505"}, want: errs.ErrAlreadyExists},
		{name: "check", err: &pgconn.PgError{Code: "23514"}, want: errs.ErrInvalidArgument},
		{name: "serialization", err: &pgconn.PgError{Code: "40001"}, want: errs.ErrConflict},
		{name: "deadlock", err: &pgconn.PgError{Code: "40P01"}, want: errs.ErrConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := wrapError("test operation", tc.err); !errors.Is(got, tc.want) {
				t.Fatalf("wrapError() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRepositoryIntegrationThreadMessageAndOutbox(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)

	thread := testThread(now)
	createCommandID := uuid.New()
	createResult := testCommandResult(createCommandID, "thread-create", enum.OperationCreateConversationThread, interactionevents.AggregateThread, thread.ID, "create-fingerprint", now)
	createEvent := testOutboxEvent(interactionevents.EventThreadCreated, interactionevents.AggregateThread, thread.ID, now)
	if err := repository.CreateConversationThreadWithResult(ctx, thread, createResult, createEvent); err != nil {
		t.Fatalf("create thread with result: %v", err)
	}
	storedThread, err := repository.GetConversationThread(ctx, thread.ID)
	if err != nil {
		t.Fatalf("get thread: %v", err)
	}
	if storedThread.Scope.Type != enum.ScopeTypeService || storedThread.Version != 1 {
		t.Fatalf("stored thread = %+v, want service scope v1", storedThread)
	}
	replayCommandID := uuid.New()
	storedResult, err := repository.GetCommandResult(ctx, query.CommandIdentity{
		CommandID:      replayCommandID,
		IdempotencyKey: createResult.IdempotencyKey,
		ActorRef:       createResult.ActorRef,
		Operation:      createResult.Operation,
	})
	if err != nil {
		t.Fatalf("get command result by idempotency: %v", err)
	}
	if storedResult.CommandID != createCommandID || storedResult.RequestFingerprint != createResult.RequestFingerprint {
		t.Fatalf("stored result = %+v, want command %s", storedResult, createCommandID)
	}

	message := testMessage(thread.ID, now.Add(time.Minute))
	thread.LatestMessageID = &message.ID
	thread.Version = 2
	thread.UpdatedAt = message.CreatedAt
	messageResult := testCommandResult(uuid.New(), "message-create", enum.OperationRecordConversationMessage, interactionevents.AggregateMessage, message.ID, "message-fingerprint", message.CreatedAt)
	messageEvent := testOutboxEvent(interactionevents.EventMessageRecorded, interactionevents.AggregateMessage, message.ID, message.CreatedAt)
	if err := repository.CreateConversationMessageWithResult(ctx, message, thread, 99, messageResult, messageEvent); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("stale message create err = %v, want %v", err, errs.ErrConflict)
	}
	if err := repository.CreateConversationMessageWithResult(ctx, message, thread, 1, messageResult, messageEvent); err != nil {
		t.Fatalf("create message with result: %v", err)
	}
	storedMessage, err := repository.GetConversationMessage(ctx, message.ID)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if storedMessage.BodyObject.SizeBytes == nil || *storedMessage.BodyObject.SizeBytes != 512 || storedMessage.SafeMetadata["surface"] != "mcp" {
		t.Fatalf("stored message = %+v, want object ref and safe metadata", storedMessage)
	}
	updatedThread, err := repository.GetConversationThread(ctx, thread.ID)
	if err != nil {
		t.Fatalf("get updated thread: %v", err)
	}
	if updatedThread.LatestMessageID == nil || *updatedThread.LatestMessageID != message.ID || updatedThread.Version != 2 {
		t.Fatalf("updated thread = %+v, want latest message %s v2", updatedThread, message.ID)
	}
	messages, page, err := repository.ListConversationMessages(ctx, query.ConversationMessageFilter{ThreadID: thread.ID, Page: value.PageRequest{PageSize: 1}})
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 1 || page.NextPageToken != "" || messages[0].ID != message.ID {
		t.Fatalf("messages = %+v page = %+v, want single message", messages, page)
	}

	claimedEvents, err := repository.ClaimOutboxEvents(ctx, 10, now.Add(2*time.Minute), now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("claim outbox events: %v", err)
	}
	if len(claimedEvents) != 2 || claimedEvents[0].AttemptCount != 1 {
		t.Fatalf("claimed events = %+v, want two leased events", claimedEvents)
	}
	if err := repository.MarkOutboxEventPublished(ctx, claimedEvents[0].ID, claimedEvents[0].AttemptCount, now.Add(4*time.Minute)); err != nil {
		t.Fatalf("mark first event published: %v", err)
	}
	if err := repository.MarkOutboxEventFailed(ctx, claimedEvents[1].ID, claimedEvents[1].AttemptCount, now.Add(5*time.Minute), "temporary"); err != nil {
		t.Fatalf("mark second event failed: %v", err)
	}
	reclaimedEvents, err := repository.ClaimOutboxEvents(ctx, 10, now.Add(6*time.Minute), now.Add(7*time.Minute))
	if err != nil {
		t.Fatalf("reclaim outbox events: %v", err)
	}
	if len(reclaimedEvents) != 1 || reclaimedEvents[0].ID != claimedEvents[1].ID || reclaimedEvents[0].AttemptCount != 2 {
		t.Fatalf("reclaimed events = %+v, want retry event attempt 2", reclaimedEvents)
	}
	if err := repository.MarkOutboxEventPermanentlyFailed(ctx, reclaimedEvents[0].ID, reclaimedEvents[0].AttemptCount, now.Add(8*time.Minute), "permanent"); err != nil {
		t.Fatalf("mark retry event permanently failed: %v", err)
	}
}

func TestRepositoryIntegrationInteractionRequestResponseLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)

	request := testInteractionRequest(now)
	createResult := testCommandResult(uuid.New(), "request-create", enum.OperationRequestApproval, interactionevents.AggregateRequest, request.ID, "request-fingerprint", now)
	createEvent := testOutboxEvent(interactionevents.EventApprovalRequested, interactionevents.AggregateRequest, request.ID, now)
	if err := repository.CreateInteractionRequestWithResult(ctx, request, createResult, createEvent); err != nil {
		t.Fatalf("create request with result: %v", err)
	}
	storedRequest, err := repository.GetInteractionRequest(ctx, request.ID)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if storedRequest.Scope.Type != enum.ScopeTypeService || storedRequest.ReminderPolicyRef != "policy:standard" || len(storedRequest.TargetRefs) != 1 {
		t.Fatalf("stored request = %+v, want service request with refs", storedRequest)
	}
	listed, _, err := repository.ListInteractionRequests(ctx, query.InteractionRequestFilter{Scope: request.Scope, Status: enum.InteractionRequestStatusWaiting, Page: value.PageRequest{PageSize: 10}})
	if err != nil {
		t.Fatalf("list requests: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != request.ID {
		t.Fatalf("listed requests = %+v, want created request", listed)
	}

	response := testInteractionResponse(request.ID, now.Add(time.Minute))
	answered := storedRequest
	answered.Status = enum.InteractionRequestStatusAnswered
	answered.Version = 2
	answered.UpdatedAt = response.CreatedAt
	answered.ResolvedAt = &response.CreatedAt
	responseResult := testCommandResult(uuid.New(), "response-create", enum.OperationRecordInteractionResponse, "response", response.ID, "response-fingerprint", response.CreatedAt)
	responseEvent := testOutboxEvent(interactionevents.EventRequestResponseRecorded, interactionevents.AggregateRequest, request.ID, response.CreatedAt)
	if err := repository.CreateInteractionResponseWithResult(ctx, response, answered, 99, responseResult, responseEvent); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("stale response create err = %v, want ErrConflict", err)
	}
	if err := repository.CreateInteractionResponseWithResult(ctx, response, answered, 1, responseResult, responseEvent); err != nil {
		t.Fatalf("create response with result: %v", err)
	}
	storedResponse, err := repository.GetInteractionResponse(ctx, response.ID)
	if err != nil {
		t.Fatalf("get response: %v", err)
	}
	if storedResponse.ResponseObject.SizeBytes == nil || *storedResponse.ResponseObject.SizeBytes != 256 || storedResponse.OwnerDecisionRef != "decision:1" {
		t.Fatalf("stored response = %+v, want object ref and decision ref", storedResponse)
	}
	updatedRequest, err := repository.GetInteractionRequest(ctx, request.ID)
	if err != nil {
		t.Fatalf("get answered request: %v", err)
	}
	if updatedRequest.Status != enum.InteractionRequestStatusAnswered || updatedRequest.Version != 2 || updatedRequest.ResolvedAt == nil {
		t.Fatalf("updated request = %+v, want answered v2", updatedRequest)
	}

	expiringRequest := testInteractionRequest(now.Add(2 * time.Minute))
	expiringRequest.ID = uuid.New()
	expireDeadline := now.Add(-time.Minute)
	expiringRequest.DeadlineAt = &expireDeadline
	expireCreateResult := testCommandResult(uuid.New(), "request-expiring", enum.OperationRequestApproval, interactionevents.AggregateRequest, expiringRequest.ID, "request-expiring-fingerprint", now)
	expireCreateEvent := testOutboxEvent(interactionevents.EventApprovalRequested, interactionevents.AggregateRequest, expiringRequest.ID, now)
	if err := repository.CreateInteractionRequestWithResult(ctx, expiringRequest, expireCreateResult, expireCreateEvent); err != nil {
		t.Fatalf("create expiring request: %v", err)
	}
	candidates, err := repository.ListExpirableInteractionRequests(ctx, expiringRequest.Scope, now, 10)
	if err != nil {
		t.Fatalf("list expirable requests: %v", err)
	}
	if len(candidates) != 1 || candidates[0].ID != expiringRequest.ID {
		t.Fatalf("candidates = %+v, want expiring request", candidates)
	}
	expired := candidates[0]
	previousVersions := map[uuid.UUID]int64{expired.ID: expired.Version}
	expired.Status = enum.InteractionRequestStatusExpired
	expired.Version = 2
	expired.UpdatedAt = now.Add(3 * time.Minute)
	expired.ResolvedAt = &expired.UpdatedAt
	expireResult := testCommandResult(uuid.New(), "expire", enum.OperationExpireInteractionRequests, interactionevents.AggregateRequest, uuid.Nil, "expire-fingerprint", expired.UpdatedAt)
	expireEvent := testOutboxEvent(interactionevents.EventRequestExpired, interactionevents.AggregateRequest, expired.ID, expired.UpdatedAt)
	if err := repository.UpdateInteractionRequestsWithResult(ctx, []entity.InteractionRequest{expired}, previousVersions, expireResult, []entity.OutboxEvent{expireEvent}); err != nil {
		t.Fatalf("expire request with result: %v", err)
	}
	storedExpired, err := repository.GetInteractionRequest(ctx, expired.ID)
	if err != nil {
		t.Fatalf("get expired request: %v", err)
	}
	if storedExpired.Status != enum.InteractionRequestStatusExpired || storedExpired.Version != 2 {
		t.Fatalf("stored expired = %+v, want expired v2", storedExpired)
	}
}

func TestRepositoryIntegrationNotificationSubscriptionLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)

	subscription := testSubscription(now)
	createResult := testCommandResult(uuid.New(), "subscription-create", enum.OperationUpsertSubscription, interactionevents.AggregateSubscription, subscription.ID, "subscription-create-fingerprint", now)
	createEvent := testOutboxEvent(interactionevents.EventSubscriptionUpdated, interactionevents.AggregateSubscription, subscription.ID, now)
	if err := repository.CreateSubscriptionWithResult(ctx, subscription, createResult, createEvent); err != nil {
		t.Fatalf("create subscription with result: %v", err)
	}
	storedSubscription, err := repository.GetSubscription(ctx, subscription.ID)
	if err != nil {
		t.Fatalf("get subscription: %v", err)
	}
	if storedSubscription.SourceOwner.Kind != enum.SourceOwnerKindAgentManager || storedSubscription.SubscriptionPolicyRef != "policy:ops-notifications" || len(storedSubscription.ChannelHintRefs) != 1 {
		t.Fatalf("stored subscription = %+v, want source owner, channel hints and policy ref", storedSubscription)
	}
	listedSubscriptions, _, err := repository.ListSubscriptions(ctx, query.SubscriptionFilter{
		Scope:         subscription.Scope,
		SubscriberRef: subscription.SubscriberRef.String(),
		Status:        enum.SubscriptionStatusActive,
		Page:          value.PageRequest{PageSize: 10},
	})
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	if len(listedSubscriptions) != 1 || listedSubscriptions[0].ID != subscription.ID {
		t.Fatalf("listed subscriptions = %+v, want created subscription", listedSubscriptions)
	}

	updatedSubscription := storedSubscription
	updatedSubscription.Status = enum.SubscriptionStatusPaused
	updatedSubscription.Version = 2
	updatedSubscription.UpdatedAt = now.Add(time.Minute)
	updateResult := testCommandResult(uuid.New(), "subscription-update", enum.OperationUpsertSubscription, interactionevents.AggregateSubscription, subscription.ID, "subscription-update-fingerprint", updatedSubscription.UpdatedAt)
	updateEvent := testOutboxEvent(interactionevents.EventSubscriptionUpdated, interactionevents.AggregateSubscription, subscription.ID, updatedSubscription.UpdatedAt)
	if err := repository.UpdateSubscriptionWithResult(ctx, updatedSubscription, 99, updateResult, updateEvent); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("stale subscription update err = %v, want ErrConflict", err)
	}
	if err := repository.UpdateSubscriptionWithResult(ctx, updatedSubscription, 1, updateResult, updateEvent); err != nil {
		t.Fatalf("update subscription with result: %v", err)
	}
	storedUpdatedSubscription, err := repository.GetSubscription(ctx, subscription.ID)
	if err != nil {
		t.Fatalf("get updated subscription: %v", err)
	}
	if storedUpdatedSubscription.Status != enum.SubscriptionStatusPaused || storedUpdatedSubscription.Version != 2 {
		t.Fatalf("updated subscription = %+v, want paused v2", storedUpdatedSubscription)
	}

	notification := testNotification(now.Add(2 * time.Minute))
	notification.SubscriptionID = &subscription.ID
	notificationResult := testCommandResult(uuid.New(), "notification-create", enum.OperationRequestNotification, interactionevents.AggregateNotification, notification.ID, "notification-create-fingerprint", notification.CreatedAt)
	notificationEvent := testOutboxEvent(interactionevents.EventNotificationRequested, interactionevents.AggregateNotification, notification.ID, notification.CreatedAt)
	if err := repository.CreateNotificationWithResult(ctx, notification, notificationResult, notificationEvent); err != nil {
		t.Fatalf("create notification with result: %v", err)
	}
	storedNotification, err := repository.GetNotification(ctx, notification.ID)
	if err != nil {
		t.Fatalf("get notification: %v", err)
	}
	if storedNotification.SubscriptionID == nil || *storedNotification.SubscriptionID != subscription.ID || storedNotification.MessageTitle != "Safe title" || storedNotification.NotificationPolicyRef != "policy:notify-standard" {
		t.Fatalf("stored notification = %+v, want subscription ref and safe policy fields", storedNotification)
	}
}

func testThread(now time.Time) entity.ConversationThread {
	return entity.ConversationThread{
		ID:              uuid.New(),
		Scope:           value.ScopeRef{Type: enum.ScopeTypeService, Ref: "agent-manager"},
		ThreadKind:      enum.ConversationThreadKindUserDialog,
		PrimaryActorRef: "service:agent-manager",
		SourceKind:      enum.ConversationSourceKindService,
		SourceRef:       "run:123",
		Status:          enum.ConversationThreadStatusOpen,
		CorrelationID:   "trace-123",
		RetentionClass:  "standard",
		Version:         1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func testMessage(threadID uuid.UUID, now time.Time) entity.ConversationMessage {
	size := int64(512)
	return entity.ConversationMessage{
		ID:          uuid.New(),
		ThreadID:    threadID,
		MessageKind: enum.ConversationMessageKindAgentText,
		AuthorRef:   "agent:codex",
		BodySummary: "safe summary",
		BodyObject: value.ObjectRef{
			URI:       "s3://kodex-interactions/messages/1",
			Digest:    "sha256:" + strings.Repeat("a", 64),
			SizeBytes: &size,
		},
		BodyDigest:   "sha256:" + strings.Repeat("b", 64),
		Locale:       "ru",
		SafeMetadata: map[string]string{"surface": "mcp"},
		CreatedAt:    now,
	}
}

func testInteractionRequest(now time.Time) entity.InteractionRequest {
	deadline := now.Add(time.Hour)
	size := int64(1024)
	return entity.InteractionRequest{
		ID:            uuid.New(),
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
			{Kind: "provider_operation", Ref: "provider:op-1"},
		},
		PromptSummary: "safe prompt summary",
		PromptObject: value.ObjectRef{
			URI:       "s3://kodex-interactions/prompts/1",
			Digest:    "sha256:" + strings.Repeat("c", 64),
			SizeBytes: &size,
		},
		AllowedActions: []value.InteractionAction{
			{ActionKey: string(enum.InteractionResponseActionApprove), LabelTemplateRef: "interaction.actions.approve", Terminal: true},
		},
		RiskClass:         enum.InteractionRiskClassHigh,
		Status:            enum.InteractionRequestStatusWaiting,
		DeadlineAt:        &deadline,
		ReminderPolicyRef: "policy:standard",
		Version:           1,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

func testInteractionResponse(requestID uuid.UUID, now time.Time) entity.InteractionResponse {
	size := int64(256)
	return entity.InteractionResponse{
		ID:                  uuid.New(),
		RequestID:           requestID,
		ResponseAction:      enum.InteractionResponseActionApprove,
		RespondedByActorRef: "user:approver-1",
		ResponseSummary:     "approved",
		ResponseObject: value.ObjectRef{
			URI:       "s3://kodex-interactions/responses/1",
			Digest:    "sha256:" + strings.Repeat("d", 64),
			SizeBytes: &size,
		},
		SourceKind:       enum.InteractionResponseSourceKindMCP,
		SourceRef:        "mcp:command-1",
		OwnerDecisionRef: "decision:1",
		CreatedAt:        now,
	}
}

func testNotification(now time.Time) entity.Notification {
	expiresAt := now.Add(time.Hour)
	return entity.Notification{
		ID:                 uuid.New(),
		Scope:              value.ScopeRef{Type: enum.ScopeTypeService, Ref: "agent-manager"},
		NotificationKind:   enum.NotificationKindAttention,
		RecipientRefs:      []value.ActorRef{{Kind: "user", Ref: "owner-1"}},
		MessageTemplateRef: "interaction.notification.attention",
		MessageTitle:       "Safe title",
		MessageSummary:     "safe bounded summary",
		BodyPreview:        "safe bounded preview",
		Priority:           enum.NotificationPriorityHigh,
		Status:             enum.NotificationStatusCreated,
		SourceOwner:        value.SourceOwnerRef{Kind: enum.SourceOwnerKindAgentManager, Ref: "run:123"},
		Ingress:            value.IngressRef{Kind: enum.IngressKindDirectGRPC, Ref: "grpc:notify-1"},
		ContextRefs: []value.ExternalRef{
			{Kind: "agent_run", Ref: "run:123"},
		},
		ChannelHintRefs: []value.ExternalRef{
			{Kind: "surface", Ref: "web_console"},
		},
		NotificationPolicyRef: "policy:notify-standard",
		CreatedAt:             now,
		UpdatedAt:             now,
		ExpiresAt:             &expiresAt,
	}
}

func testSubscription(now time.Time) entity.Subscription {
	return entity.Subscription{
		ID:                      uuid.New(),
		Scope:                   value.ScopeRef{Type: enum.ScopeTypeService, Ref: "agent-manager"},
		SubscriberRef:           value.ActorRef{Kind: "user", Ref: "owner-1"},
		EventFilterJSON:         `{"event_kind":["run_waiting"],"severity":["high"]}`,
		DeliveryPreferencesJSON: `{"surfaces":["web_console"],"fallback_policy_ref":"policy:fallback"}`,
		Status:                  enum.SubscriptionStatusActive,
		Version:                 1,
		SourceOwner:             value.SourceOwnerRef{Kind: enum.SourceOwnerKindAgentManager, Ref: "run:123"},
		ChannelHintRefs: []value.ExternalRef{
			{Kind: "surface", Ref: "web_console"},
		},
		SubscriptionPolicyRef: "policy:ops-notifications",
		CreatedAt:             now,
		UpdatedAt:             now,
	}
}

func testCommandResult(commandID uuid.UUID, idempotencyKey string, operation enum.Operation, aggregateType string, aggregateID uuid.UUID, fingerprint string, now time.Time) entity.CommandResult {
	return entity.CommandResult{
		Key:                "command:" + commandID.String(),
		CommandID:          commandID,
		IdempotencyKey:     idempotencyKey,
		ActorRef:           "service:interaction-test",
		Operation:          operation,
		AggregateType:      aggregateType,
		AggregateID:        aggregateID,
		RequestFingerprint: fingerprint,
		ResultPayload:      []byte(`{}`),
		CreatedAt:          now,
	}
}

func testOutboxEvent(eventType string, aggregateType string, aggregateID uuid.UUID, now time.Time) entity.OutboxEvent {
	return entity.OutboxEvent{
		Event: outboxlib.NewEvent(uuid.New(), eventType, interactionevents.SchemaVersion, aggregateType, aggregateID, []byte(`{"version":1}`), now, 0),
	}
}

func openIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("KODEX_INTERACTION_HUB_TEST_DATABASE_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("set KODEX_INTERACTION_HUB_TEST_DATABASE_DSN to run PostgreSQL repository integration tests")
	}
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := "interaction_repo_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.WithoutCancel(ctx), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE")
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	applyMigrations(t, ctx, pool)
	return pool
}

func applyMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	statements := migrationtest.GooseUpStatements(t, "../../../../cmd/cli/migrations")
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("apply migration statement %q: %v", statement, err)
		}
	}
}
