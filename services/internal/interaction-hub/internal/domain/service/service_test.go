package service

import (
	"context"
	"encoding/json"
	"errors"
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
	svc := NewWithConfig(repository, Config{Clock: fixedClock{now: now.Add(time.Minute)}, UUIDGenerator: &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}}})
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

func TestServiceBacklogOperationsReturnUnimplemented(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	svc := New(repository)

	err := svc.RequestFeedback(context.Background())
	if !errors.Is(err, errs.ErrNotImplemented) {
		t.Fatalf("RequestFeedback() err = %v, want ErrNotImplemented", err)
	}
	if len(repository.operations) != 1 || repository.operations[0] != enum.OperationRequestFeedback {
		t.Fatalf("operations = %v, want RequestFeedback", repository.operations)
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

	err := New(&fakeRepository{}).RequestApproval(context.Background())
	if !errors.Is(err, errs.ErrUnavailable) {
		t.Fatalf("RequestApproval() err = %v, want ErrUnavailable", err)
	}
}

type fakeRepository struct {
	ready      bool
	operations []enum.Operation
	threads    map[uuid.UUID]entity.ConversationThread
	messages   map[uuid.UUID]entity.ConversationMessage
	results    map[string]entity.CommandResult
	events     []entity.OutboxEvent
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		ready:    true,
		threads:  map[uuid.UUID]entity.ConversationThread{},
		messages: map[uuid.UUID]entity.ConversationMessage{},
		results:  map[string]entity.CommandResult{},
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
