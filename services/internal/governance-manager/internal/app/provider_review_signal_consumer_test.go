package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	eventconsumer "github.com/codex-k8s/kodex/libs/go/eventconsumer"
	eventlog "github.com/codex-k8s/kodex/libs/go/eventlog"
	providerevents "github.com/codex-k8s/kodex/libs/go/platformevents/provider"
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governanceservice "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
)

func TestProviderReviewSignalEventHandlerRecordsApprovedReviewSignal(t *testing.T) {
	t.Parallel()

	recorder := &fakeReviewSignalRecorder{}
	handler := providerReviewSignalEventHandler{recorder: recorder}
	event := providerReviewSignalStoredEvent(t, providerevents.Payload{
		ProviderSlug:        "github",
		ProviderWorkItemID:  "provider:work_item/123",
		CommentProjectionID: "11111111-2222-3333-4444-555555555555",
		ProviderCommentID:   "9001",
		ReviewState:         "approved",
		Version:             3,
	})

	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: event})
	if result.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent() = %+v, want ack", result)
	}
	if len(recorder.inputs) != 1 {
		t.Fatalf("recorded inputs = %d, want 1", len(recorder.inputs))
	}
	input := recorder.inputs[0]
	if input.Target.Type != providerReviewSignalTargetType || input.Target.Ref != "provider:work_item/123" {
		t.Fatalf("target = %+v, want provider work item ref", input.Target)
	}
	if input.RoleKind != enum.ReviewRoleKindReviewer || input.Outcome != enum.ReviewSignalOutcomePass || input.Severity != enum.SignalSeverityInfo || input.Confidence != enum.ConfidenceHigh {
		t.Fatalf("review classification = %s/%s/%s/%s, want reviewer/pass/info/high", input.RoleKind, input.Outcome, input.Severity, input.Confidence)
	}
	if input.AuthorRef != "service:provider-hub" || input.Meta.Actor.Type != "service" || input.Meta.Actor.ID != "provider-hub" {
		t.Fatalf("actor refs = %q/%+v, want provider-hub service actor", input.AuthorRef, input.Meta.Actor)
	}
	if input.Meta.IdempotencyKey == "" || !strings.HasPrefix(input.Meta.IdempotencyKey, "provider_review_signal:") {
		t.Fatalf("idempotency key = %q, want provider review signal key", input.Meta.IdempotencyKey)
	}
	if input.Meta.RequestID != "provider_event:"+event.ID.String() {
		t.Fatalf("request id = %q, want provider event ref", input.Meta.RequestID)
	}
	if len(input.EvidenceRefs) != 1 {
		t.Fatalf("evidence refs = %d, want 1", len(input.EvidenceRefs))
	}
	evidence := input.EvidenceRefs[0]
	if evidence.Kind != providerReviewSignalEvidenceKind || evidence.Ref != "provider:comment_projection/11111111-2222-3333-4444-555555555555" || evidence.RetentionClass != providerReviewSignalRetention {
		t.Fatalf("evidence = %+v, want provider comment projection safe ref", evidence)
	}
	if input.Summary != "provider review approved" || strings.Contains(input.Summary, "payload") {
		t.Fatalf("summary = %q, want bounded safe summary", input.Summary)
	}
}

func TestProviderReviewSignalEventHandlerRecordsChangesRequestedReviewSignal(t *testing.T) {
	t.Parallel()

	recorder := &fakeReviewSignalRecorder{}
	handler := providerReviewSignalEventHandler{recorder: recorder}

	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: providerReviewSignalStoredEvent(t, providerevents.Payload{
		ProviderSlug:       "gitlab",
		ProviderWorkItemID: "provider:work_item/456",
		ProviderCommentID:  "review-7",
		ReviewState:        "changes_requested",
		Version:            4,
	})})
	if result.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent() = %+v, want ack", result)
	}
	if len(recorder.inputs) != 1 {
		t.Fatalf("recorded inputs = %d, want 1", len(recorder.inputs))
	}
	input := recorder.inputs[0]
	if input.Outcome != enum.ReviewSignalOutcomeRequestChanges || input.Severity != enum.SignalSeverityBlocking {
		t.Fatalf("classification = %s/%s, want request_changes/blocking", input.Outcome, input.Severity)
	}
	if got := input.EvidenceRefs[0].Ref; got != "provider:gitlab:comment/review-7" {
		t.Fatalf("fallback evidence ref = %q, want provider comment ref", got)
	}
	if input.Summary != "provider review requested changes" {
		t.Fatalf("summary = %q, want changes requested summary", input.Summary)
	}
}

func TestProviderReviewSignalEventHandlerIgnoresUnsupportedReviewStates(t *testing.T) {
	t.Parallel()

	for _, state := range []string{"", "commented", "pending", "dismissed"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()

			recorder := &fakeReviewSignalRecorder{}
			handler := providerReviewSignalEventHandler{recorder: recorder}
			result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: providerReviewSignalStoredEvent(t, providerevents.Payload{
				ProviderSlug:       "github",
				ProviderWorkItemID: "provider:work_item/123",
				ProviderCommentID:  "9001",
				ReviewState:        state,
				Version:            2,
			})})
			if result.Status != eventconsumer.ResultAck {
				t.Fatalf("HandleEvent() = %+v, want ack", result)
			}
			if len(recorder.inputs) != 0 {
				t.Fatalf("recorded inputs = %d, want 0", len(recorder.inputs))
			}
		})
	}
}

func TestProviderReviewSignalEventHandlerPoisonsInvalidEventShape(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		event  eventlog.StoredEvent
		code   string
		inputs int
	}{
		{
			name:  "wrong source",
			event: providerReviewSignalStoredEvent(t, providerevents.Payload{ProviderWorkItemID: "provider:work_item/123", ProviderCommentID: "9001", ReviewState: "approved"}),
			code:  "invalid_source_service",
		},
		{
			name:  "wrong aggregate",
			event: providerReviewSignalStoredEvent(t, providerevents.Payload{ProviderWorkItemID: "provider:work_item/123", ProviderCommentID: "9001", ReviewState: "approved"}),
			code:  "invalid_aggregate_type",
		},
		{
			name:  "missing work item",
			event: providerReviewSignalStoredEvent(t, providerevents.Payload{ProviderSlug: "github", ProviderCommentID: "9001", ReviewState: "approved"}),
			code:  "missing_provider_work_item_ref",
		},
		{
			name:  "missing review ref",
			event: providerReviewSignalStoredEvent(t, providerevents.Payload{ProviderWorkItemID: "provider:work_item/123", ReviewState: "approved"}),
			code:  "missing_provider_review_ref",
		},
	}
	cases[0].event.SourceService = "agent-manager"
	cases[1].event.AggregateType = providerevents.AggregateWorkItem

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := &fakeReviewSignalRecorder{}
			handler := providerReviewSignalEventHandler{recorder: recorder}
			result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: tc.event})
			if result.Status != eventconsumer.ResultPoison || result.Code != tc.code {
				t.Fatalf("HandleEvent() = %+v, want poison/%s", result, tc.code)
			}
			if len(recorder.inputs) != tc.inputs {
				t.Fatalf("recorded inputs = %d, want %d", len(recorder.inputs), tc.inputs)
			}
		})
	}
}

func TestProviderReviewSignalEventHandlerMapsDomainErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		err    error
		status eventconsumer.ResultStatus
		code   string
	}{
		{name: "invalid", err: errs.ErrInvalidArgument, status: eventconsumer.ResultPoison, code: "invalid_provider_review_signal"},
		{name: "conflict", err: errs.ErrConflict, status: eventconsumer.ResultPoison, code: "conflicting_provider_review_signal"},
		{name: "forbidden", err: errs.ErrForbidden, status: eventconsumer.ResultPoison, code: "forbidden_provider_review_signal"},
		{name: "temporary", err: errors.New("database unavailable"), status: eventconsumer.ResultRetry, code: "retryable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := &fakeReviewSignalRecorder{recordErr: tc.err}
			handler := providerReviewSignalEventHandler{recorder: recorder}
			result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: providerReviewSignalStoredEvent(t, providerevents.Payload{
				ProviderSlug:       "github",
				ProviderWorkItemID: "provider:work_item/123",
				ProviderCommentID:  "9001",
				ReviewState:        "approved",
				Version:            2,
			})})
			if result.Status != tc.status || result.Code != tc.code {
				t.Fatalf("HandleEvent() = %+v, want %s/%s", result, tc.status, tc.code)
			}
		})
	}
}

func providerReviewSignalStoredEvent(t *testing.T, payload providerevents.Payload) eventlog.StoredEvent {
	t.Helper()
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(): %v", err)
	}
	return eventlog.StoredEvent{
		SequenceID: 1,
		Event: eventlog.Event{
			ID:            uuid.New(),
			SourceService: providerReviewSignalSourceService,
			EventType:     providerevents.EventCommentSynced,
			SchemaVersion: providerevents.SchemaVersion,
			AggregateType: providerevents.AggregateComment,
			AggregateID:   uuid.New(),
			Payload:       payloadBytes,
			OccurredAt:    time.Now().UTC(),
		},
		RecordedAt: time.Now().UTC(),
	}
}

type fakeReviewSignalRecorder struct {
	inputs    []governanceservice.RecordReviewSignalInput
	recordErr error
}

func (r *fakeReviewSignalRecorder) RecordReviewSignal(_ context.Context, input governanceservice.RecordReviewSignalInput) (entity.ReviewSignal, error) {
	r.inputs = append(r.inputs, input)
	if r.recordErr != nil {
		return entity.ReviewSignal{}, r.recordErr
	}
	return entity.ReviewSignal{
		ID:       uuid.New(),
		Target:   input.Target,
		RoleKind: input.RoleKind,
		Outcome:  input.Outcome,
		Severity: input.Severity,
		Summary:  input.Summary,
	}, nil
}
