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
	interactionevents "github.com/codex-k8s/kodex/libs/go/platformevents/interaction"
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governanceservice "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
)

func TestInteractionGateDecisionEventHandlerRecordsApprovedGateDecision(t *testing.T) {
	t.Parallel()

	gateRequestID := uuid.New()
	recorder := &fakeGateDecisionRecorder{gateRequest: entity.GateRequest{
		VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 7},
		Status:        enum.GateRequestStatusAwaitingDecision,
	}}
	handler := interactionGateDecisionEventHandler{recorder: recorder}
	event := interactionGateDecisionStoredEvent(t, interactionevents.Payload{
		RequestID:                uuid.NewString(),
		InteractionRequestRef:    "interaction:request/req-1",
		RequestKind:              interactionGateDecisionRequestKind,
		ResponseID:               uuid.NewString(),
		InteractionResponseRef:   "interaction:response/resp-1",
		ResponseAction:           "approve",
		ResponseOutcome:          "approve",
		ResponseSourceRef:        "mcp:command-1",
		ResponseSummaryDigest:    "sha256:summary",
		ActorRef:                 "user:owner-1",
		GovernanceGateRequestRef: "governance:gate_request/" + gateRequestID.String(),
		OwnerService:             interactionGateDecisionOwnerService,
		Status:                   interactionGateDecisionAnswered,
		Version:                  3,
	})

	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: event})
	if result.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent() = %+v, want ack", result)
	}
	if recorder.getInputs != 1 || len(recorder.submitInputs) != 1 {
		t.Fatalf("calls get=%d submit=%d, want 1/1", recorder.getInputs, len(recorder.submitInputs))
	}
	input := recorder.submitInputs[0]
	if input.GateRequestID != gateRequestID || input.Outcome != enum.GateOutcomeApprove {
		t.Fatalf("decision input = %+v, want approve for gate", input)
	}
	if input.DecisionActorRef != "user:owner-1" || input.SourceRef != "mcp:command-1" {
		t.Fatalf("actor/source = %q/%q, want safe interaction refs", input.DecisionActorRef, input.SourceRef)
	}
	if input.Meta.Actor.Type != "service" || input.Meta.Actor.ID != interactionGateDecisionConsumerActor {
		t.Fatalf("meta actor = %+v, want interaction-hub service", input.Meta.Actor)
	}
	if input.Meta.ExpectedVersion == nil || *input.Meta.ExpectedVersion != 7 {
		t.Fatalf("expected version = %+v, want 7", input.Meta.ExpectedVersion)
	}
	if input.Meta.RequestID != "interaction_event:"+event.ID.String() {
		t.Fatalf("request id = %q, want interaction event ref", input.Meta.RequestID)
	}
	if !strings.HasPrefix(input.Meta.IdempotencyKey, "interaction_gate_decision:") {
		t.Fatalf("idempotency key = %q, want interaction gate decision key", input.Meta.IdempotencyKey)
	}
	if input.InteractionDeliveryRef.RequestRef != "interaction:request/req-1" || input.InteractionDeliveryRef.DecisionRef != "interaction:response/resp-1" {
		t.Fatalf("interaction refs = %+v, want request and response refs", input.InteractionDeliveryRef)
	}
	if input.Reason != "owner approved governance gate via interaction-hub" || input.ConditionsSummary != "interaction response digest sha256:summary" {
		t.Fatalf("summary fields = %q/%q, want bounded safe summaries", input.Reason, input.ConditionsSummary)
	}
}

func TestInteractionGateDecisionEventHandlerRecordsRejectedGateDecisionFromOwnerRequestRef(t *testing.T) {
	t.Parallel()

	gateRequestID := uuid.New()
	recorder := &fakeGateDecisionRecorder{gateRequest: entity.GateRequest{
		VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 4},
		Status:        enum.GateRequestStatusAwaitingDecision,
	}}
	handler := interactionGateDecisionEventHandler{recorder: recorder}
	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: interactionGateDecisionStoredEvent(t, interactionevents.Payload{
		RequestID:       "request-1",
		RequestKind:     interactionGateDecisionRequestKind,
		ResponseID:      "response-1",
		ResponseAction:  "reject",
		ActorRef:        "user:owner-2",
		OwnerRequestRef: "governance:gate/" + gateRequestID.String(),
		OwnerService:    interactionGateDecisionOwnerService,
		Status:          interactionGateDecisionAnswered,
		Version:         2,
	})})
	if result.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent() = %+v, want ack", result)
	}
	input := recorder.submitInputs[0]
	if input.Outcome != enum.GateOutcomeReject || input.Reason != "owner rejected governance gate via interaction-hub" {
		t.Fatalf("decision = %s/%q, want reject", input.Outcome, input.Reason)
	}
	if input.SourceRef != "interaction:response/response-1" {
		t.Fatalf("source ref = %q, want interaction response ref", input.SourceRef)
	}
}

func TestInteractionGateDecisionEventHandlerIgnoresOtherOwners(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload interactionevents.Payload
	}{
		{
			name: "agent owner",
			payload: interactionevents.Payload{
				RequestID:       "request-1",
				RequestKind:     interactionGateDecisionRequestKind,
				ResponseID:      "response-1",
				ResponseAction:  "approve",
				ActorRef:        "user:owner",
				OwnerRequestRef: "agent:human_gate/11111111-1111-4111-8111-111111111111",
				OwnerService:    "agent_manager",
				Status:          interactionGateDecisionAnswered,
				Version:         2,
			},
		},
		{
			name: "missing explicit owner_service",
			payload: interactionevents.Payload{
				RequestID:         "request-1",
				RequestKind:       interactionGateDecisionRequestKind,
				ResponseID:        "response-1",
				ResponseAction:    "approve",
				ActorRef:          "user:owner",
				OwnerRequestRef:   "governance:gate/11111111-1111-4111-8111-111111111111",
				DecisionOwnerKind: interactionGateDecisionOwnerService,
				Status:            interactionGateDecisionAnswered,
				Version:           2,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := &fakeGateDecisionRecorder{}
			handler := interactionGateDecisionEventHandler{recorder: recorder}
			result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: interactionGateDecisionStoredEvent(t, tc.payload)})
			if result.Status != eventconsumer.ResultAck {
				t.Fatalf("HandleEvent() = %+v, want ack", result)
			}
			if recorder.getInputs != 0 || len(recorder.submitInputs) != 0 {
				t.Fatalf("calls get=%d submit=%d, want no governance mutation", recorder.getInputs, len(recorder.submitInputs))
			}
		})
	}
}

func TestInteractionGateDecisionEventHandlerPoisonsInvalidEventShape(t *testing.T) {
	t.Parallel()

	gateRequestID := uuid.New()
	basePayload := interactionevents.Payload{
		RequestID:                "request-1",
		RequestKind:              interactionGateDecisionRequestKind,
		ResponseID:               "response-1",
		ResponseAction:           "approve",
		ActorRef:                 "user:owner",
		GovernanceGateRequestRef: gateRequestID.String(),
		OwnerService:             interactionGateDecisionOwnerService,
		Status:                   interactionGateDecisionAnswered,
		Version:                  1,
	}
	cases := []struct {
		name  string
		event eventlog.StoredEvent
		code  string
	}{
		{
			name:  "wrong source",
			event: interactionGateDecisionStoredEvent(t, basePayload),
			code:  "invalid_source_service",
		},
		{
			name:  "wrong aggregate",
			event: interactionGateDecisionStoredEvent(t, basePayload),
			code:  "invalid_aggregate_type",
		},
		{
			name: "not answered",
			event: interactionGateDecisionStoredEvent(t, interactionevents.Payload{
				RequestID:                "request-1",
				RequestKind:              interactionGateDecisionRequestKind,
				ResponseID:               "response-1",
				ResponseAction:           "approve",
				ActorRef:                 "user:owner",
				GovernanceGateRequestRef: gateRequestID.String(),
				OwnerService:             interactionGateDecisionOwnerService,
				Status:                   "waiting",
			}),
			code: "invalid_response_status",
		},
		{
			name: "missing response refs",
			event: interactionGateDecisionStoredEvent(t, interactionevents.Payload{
				RequestKind:              interactionGateDecisionRequestKind,
				ResponseAction:           "approve",
				ActorRef:                 "user:owner",
				GovernanceGateRequestRef: gateRequestID.String(),
				OwnerService:             interactionGateDecisionOwnerService,
				Status:                   interactionGateDecisionAnswered,
			}),
			code: "missing_response_refs",
		},
		{
			name: "missing actor",
			event: interactionGateDecisionStoredEvent(t, interactionevents.Payload{
				RequestID:                "request-1",
				RequestKind:              interactionGateDecisionRequestKind,
				ResponseID:               "response-1",
				ResponseAction:           "approve",
				GovernanceGateRequestRef: gateRequestID.String(),
				OwnerService:             interactionGateDecisionOwnerService,
				Status:                   interactionGateDecisionAnswered,
			}),
			code: "missing_actor_ref",
		},
		{
			name: "invalid gate ref",
			event: interactionGateDecisionStoredEvent(t, interactionevents.Payload{
				RequestID:       "request-1",
				RequestKind:     interactionGateDecisionRequestKind,
				ResponseID:      "response-1",
				ResponseAction:  "approve",
				ActorRef:        "user:owner",
				OwnerRequestRef: "governance:gate/not-a-uuid",
				OwnerService:    interactionGateDecisionOwnerService,
				Status:          interactionGateDecisionAnswered,
			}),
			code: "invalid_gate_request_ref",
		},
		{
			name: "unsupported action",
			event: interactionGateDecisionStoredEvent(t, interactionevents.Payload{
				RequestID:                "request-1",
				RequestKind:              interactionGateDecisionRequestKind,
				ResponseID:               "response-1",
				ResponseAction:           "defer",
				ActorRef:                 "user:owner",
				GovernanceGateRequestRef: gateRequestID.String(),
				OwnerService:             interactionGateDecisionOwnerService,
				Status:                   interactionGateDecisionAnswered,
			}),
			code: "unsupported_response_action",
		},
		{
			name: "missing action with approve outcome",
			event: interactionGateDecisionStoredEvent(t, interactionevents.Payload{
				RequestID:                "request-1",
				RequestKind:              interactionGateDecisionRequestKind,
				ResponseID:               "response-1",
				ResponseOutcome:          "approve",
				ActorRef:                 "user:owner",
				GovernanceGateRequestRef: gateRequestID.String(),
				OwnerService:             interactionGateDecisionOwnerService,
				Status:                   interactionGateDecisionAnswered,
			}),
			code: "unsupported_response_action",
		},
		{
			name: "mismatched action and outcome",
			event: interactionGateDecisionStoredEvent(t, interactionevents.Payload{
				RequestID:                "request-1",
				RequestKind:              interactionGateDecisionRequestKind,
				ResponseID:               "response-1",
				ResponseAction:           "approve",
				ResponseOutcome:          "reject",
				ActorRef:                 "user:owner",
				GovernanceGateRequestRef: gateRequestID.String(),
				OwnerService:             interactionGateDecisionOwnerService,
				Status:                   interactionGateDecisionAnswered,
			}),
			code: "unsupported_response_action",
		},
	}
	cases[0].event.SourceService = "agent-manager"
	cases[1].event.AggregateType = "delivery"

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := &fakeGateDecisionRecorder{gateRequest: entity.GateRequest{VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 1}}}
			handler := interactionGateDecisionEventHandler{recorder: recorder}
			result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: tc.event})
			if result.Status != eventconsumer.ResultPoison || result.Code != tc.code {
				t.Fatalf("HandleEvent() = %+v, want poison/%s", result, tc.code)
			}
			if len(recorder.submitInputs) != 0 {
				t.Fatalf("submit calls = %d, want no mutation", len(recorder.submitInputs))
			}
		})
	}
}

func TestInteractionGateDecisionEventHandlerMapsDomainErrors(t *testing.T) {
	t.Parallel()

	gateRequestID := uuid.New()
	cases := []struct {
		name   string
		getErr error
		err    error
		status eventconsumer.ResultStatus
		code   string
	}{
		{name: "unknown gate", getErr: errs.ErrNotFound, status: eventconsumer.ResultPoison, code: "unknown_gate_request"},
		{name: "invalid", err: errs.ErrInvalidArgument, status: eventconsumer.ResultPoison, code: "invalid_gate_decision_response"},
		{name: "conflict", err: errs.ErrConflict, status: eventconsumer.ResultPoison, code: "conflicting_gate_decision_response"},
		{name: "forbidden", err: errs.ErrForbidden, status: eventconsumer.ResultPoison, code: "forbidden_gate_decision_response"},
		{name: "stale", err: errs.ErrPreconditionFailed, status: eventconsumer.ResultPoison, code: "stale_gate_decision_response"},
		{name: "temporary", err: errors.New("database unavailable"), status: eventconsumer.ResultRetry, code: "retryable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := &fakeGateDecisionRecorder{
				gateRequest: entity.GateRequest{VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 1}},
				getErr:      tc.getErr,
				submitErr:   tc.err,
			}
			handler := interactionGateDecisionEventHandler{recorder: recorder}
			result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: interactionGateDecisionStoredEvent(t, interactionevents.Payload{
				RequestID:                "request-1",
				RequestKind:              interactionGateDecisionRequestKind,
				ResponseID:               "response-1",
				ResponseAction:           "approve",
				ActorRef:                 "user:owner",
				GovernanceGateRequestRef: gateRequestID.String(),
				OwnerService:             interactionGateDecisionOwnerService,
				Status:                   interactionGateDecisionAnswered,
			})})
			if result.Status != tc.status || result.Code != tc.code {
				t.Fatalf("HandleEvent() = %+v, want %s/%s", result, tc.status, tc.code)
			}
		})
	}
}

func TestInteractionGateDecisionIdempotencyKeyIncludesSafeOutcomeFingerprint(t *testing.T) {
	t.Parallel()

	payload := interactionevents.Payload{
		RequestID:                "request-1",
		RequestKind:              interactionGateDecisionRequestKind,
		ResponseID:               "response-1",
		ResponseAction:           "approve",
		GovernanceGateRequestRef: uuid.NewString(),
		OwnerService:             interactionGateDecisionOwnerService,
		Status:                   interactionGateDecisionAnswered,
		Version:                  2,
	}
	first := interactionGateDecisionIdempotencyKey(payload)
	replayed := interactionGateDecisionIdempotencyKey(payload)
	payload.ResponseAction = "reject"
	conflicting := interactionGateDecisionIdempotencyKey(payload)
	if first == "" || first != replayed || first == conflicting {
		t.Fatalf("idempotency keys first=%q replayed=%q conflicting=%q", first, replayed, conflicting)
	}
}

func interactionGateDecisionStoredEvent(t *testing.T, payload interactionevents.Payload) eventlog.StoredEvent {
	t.Helper()
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(): %v", err)
	}
	return eventlog.StoredEvent{
		SequenceID: 1,
		Event: eventlog.Event{
			ID:            uuid.New(),
			SourceService: interactionGateDecisionSourceService,
			EventType:     interactionevents.EventRequestResponseRecorded,
			SchemaVersion: interactionevents.SchemaVersion,
			AggregateType: interactionevents.AggregateRequest,
			AggregateID:   uuid.New(),
			Payload:       payloadBytes,
			OccurredAt:    time.Now().UTC(),
		},
		RecordedAt: time.Now().UTC(),
	}
}

type fakeGateDecisionRecorder struct {
	gateRequest  entity.GateRequest
	getErr       error
	submitErr    error
	getInputs    int
	submitInputs []governanceservice.SubmitGateDecisionInput
}

func (r *fakeGateDecisionRecorder) GetGateRequest(_ context.Context, input governanceservice.GetGateRequestInput) (entity.GateRequest, error) {
	r.getInputs++
	if r.getErr != nil {
		return entity.GateRequest{}, r.getErr
	}
	request := r.gateRequest
	request.ID = input.GateRequestID
	return request, nil
}

func (r *fakeGateDecisionRecorder) SubmitGateDecision(_ context.Context, input governanceservice.SubmitGateDecisionInput) (entity.GateDecision, entity.GateRequest, error) {
	r.submitInputs = append(r.submitInputs, input)
	if r.submitErr != nil {
		return entity.GateDecision{}, entity.GateRequest{}, r.submitErr
	}
	return entity.GateDecision{
		ID:               uuid.New(),
		GateRequestID:    input.GateRequestID,
		DecisionActorRef: input.DecisionActorRef,
		Outcome:          input.Outcome,
		Reason:           input.Reason,
		SourceRef:        input.SourceRef,
	}, r.gateRequest, nil
}
