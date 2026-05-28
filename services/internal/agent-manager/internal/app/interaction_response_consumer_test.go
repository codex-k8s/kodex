package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	eventconsumer "github.com/codex-k8s/kodex/libs/go/eventconsumer"
	eventlog "github.com/codex-k8s/kodex/libs/go/eventlog"
	interactionevents "github.com/codex-k8s/kodex/libs/go/platformevents/interaction"
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
)

func TestInteractionResponseEventHandlerRecordsHumanGateDecision(t *testing.T) {
	t.Parallel()

	gateID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	requestID := uuid.MustParse("22222222-3333-4444-5555-666666666666")
	responseID := uuid.MustParse("33333333-4444-5555-6666-777777777777")
	recorder := &fakeHumanGateResponseRecorder{
		gates: map[uuid.UUID]entity.HumanGateRequest{
			gateID: {
				VersionedBase:         entity.VersionedBase{ID: gateID, Version: 3},
				InteractionRequestRef: "interaction:request/" + requestID.String(),
				SafeSummary:           "Review stage needs owner decision",
				Status:                enum.HumanGateStatusWaiting,
				Outcome:               enum.HumanGateOutcomeNone,
			},
		},
	}
	handler := interactionResponseEventHandler{recorder: recorder}

	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: interactionResponseStoredEvent(t, interactionevents.Payload{
		RequestID:       requestID.String(),
		RequestKind:     "human_gate",
		ResponseID:      responseID.String(),
		ResponseAction:  "approve",
		OwnerService:    "agent_manager",
		OwnerRequestRef: "human_gate:" + gateID.String(),
		SourceKind:      "mcp",
		Status:          "answered",
		Version:         2,
	})})
	if result.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent() = %+v, want ack", result)
	}
	if len(recorder.inputs) != 1 {
		t.Fatalf("recorded inputs = %d, want 1", len(recorder.inputs))
	}
	input := recorder.inputs[0]
	if input.HumanGateRequestID != gateID || input.Outcome != enum.HumanGateOutcomeApprove {
		t.Fatalf("input = %+v, want approve for gate", input)
	}
	if input.Meta.ExpectedVersion == nil || *input.Meta.ExpectedVersion != 3 {
		t.Fatalf("expected version = %+v, want 3", input.Meta.ExpectedVersion)
	}
	if input.Meta.IdempotencyKey != "interaction_response:"+responseID.String() {
		t.Fatalf("idempotency key = %q", input.Meta.IdempotencyKey)
	}
	if input.InteractionRequestRef != "interaction:request/"+requestID.String() || input.InteractionResponseRef != "interaction:response/"+responseID.String() {
		t.Fatalf("interaction refs = %q/%q", input.InteractionRequestRef, input.InteractionResponseRef)
	}
	if input.InteractionResponseFingerprint == "" || input.InteractionRequestVersion != 2 {
		t.Fatalf("fingerprint/version = %q/%d", input.InteractionResponseFingerprint, input.InteractionRequestVersion)
	}
	if input.SafeSummary != "Review stage needs owner decision" {
		t.Fatalf("safe summary = %q", input.SafeSummary)
	}
}

func TestInteractionResponseEventHandlerMapsAdditionalHumanGateOutcomes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		action  string
		outcome enum.HumanGateOutcome
	}{
		{name: "reject", action: "reject", outcome: enum.HumanGateOutcomeReject},
		{name: "request_changes", action: "request_changes", outcome: enum.HumanGateOutcomeRequestChanges},
		{name: "answer", action: "answer", outcome: enum.HumanGateOutcomeAnswer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gateID := uuid.New()
			recorder := &fakeHumanGateResponseRecorder{
				gates: map[uuid.UUID]entity.HumanGateRequest{
					gateID: {
						VersionedBase: entity.VersionedBase{ID: gateID, Version: 3},
						Status:        enum.HumanGateStatusWaiting,
						Outcome:       enum.HumanGateOutcomeNone,
					},
				},
			}
			handler := interactionResponseEventHandler{recorder: recorder}

			result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: interactionResponseStoredEvent(t, interactionevents.Payload{
				RequestID:       uuid.NewString(),
				RequestKind:     "human_gate",
				ResponseID:      uuid.NewString(),
				ResponseAction:  tc.action,
				OwnerService:    "agent_manager",
				OwnerRequestRef: gateID.String(),
				Status:          "answered",
				Version:         2,
			})})
			if result.Status != eventconsumer.ResultAck {
				t.Fatalf("HandleEvent() = %+v, want ack", result)
			}
			if len(recorder.inputs) != 1 || recorder.inputs[0].Outcome != tc.outcome {
				t.Fatalf("recorded inputs = %+v, want outcome %s", recorder.inputs, tc.outcome)
			}
		})
	}
}

func TestInteractionResponseEventHandlerIgnoresOtherOwners(t *testing.T) {
	t.Parallel()

	recorder := &fakeHumanGateResponseRecorder{}
	handler := interactionResponseEventHandler{recorder: recorder}

	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: interactionResponseStoredEvent(t, interactionevents.Payload{
		RequestID:       uuid.NewString(),
		RequestKind:     "approval",
		ResponseID:      uuid.NewString(),
		ResponseAction:  "approve",
		OwnerService:    "governance_manager",
		OwnerRequestRef: "gate:req-1",
		Status:          "answered",
		Version:         2,
	})})
	if result.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent() = %+v, want ack", result)
	}
	if len(recorder.inputs) != 0 {
		t.Fatalf("recorded inputs = %d, want 0", len(recorder.inputs))
	}
}

func TestInteractionResponseEventHandlerPoisonsUnsafeResponse(t *testing.T) {
	t.Parallel()

	handler := interactionResponseEventHandler{recorder: &fakeHumanGateResponseRecorder{}}
	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: interactionResponseStoredEvent(t, interactionevents.Payload{
		RequestID:       uuid.NewString(),
		RequestKind:     "human_gate",
		ResponseID:      uuid.NewString(),
		ResponseAction:  "defer",
		OwnerService:    "agent_manager",
		OwnerRequestRef: "human_gate:not-a-uuid",
		Status:          "answered",
		Version:         2,
	})})
	if result.Status != eventconsumer.ResultPoison || result.Code != "invalid_owner_request_ref" {
		t.Fatalf("HandleEvent() = %+v, want invalid owner request poison", result)
	}
}

func TestInteractionResponseEventHandlerPoisonsUnknownAction(t *testing.T) {
	t.Parallel()

	gateID := uuid.New()
	recorder := &fakeHumanGateResponseRecorder{}
	handler := interactionResponseEventHandler{recorder: recorder}
	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: interactionResponseStoredEvent(t, interactionevents.Payload{
		RequestID:       uuid.NewString(),
		RequestKind:     "human_gate",
		ResponseID:      uuid.NewString(),
		ResponseAction:  "defer",
		OwnerService:    "agent_manager",
		OwnerRequestRef: "human_gate:" + gateID.String(),
		Status:          "answered",
		Version:         2,
	})})
	if result.Status != eventconsumer.ResultPoison || result.Code != "unsupported_response_action" {
		t.Fatalf("HandleEvent() = %+v, want unsupported action poison", result)
	}
	if len(recorder.inputs) != 0 {
		t.Fatalf("recorded inputs = %d, want 0", len(recorder.inputs))
	}
}

func TestInteractionResponseEventHandlerPoisonsStaleStatus(t *testing.T) {
	t.Parallel()

	gateID := uuid.New()
	recorder := &fakeHumanGateResponseRecorder{}
	handler := interactionResponseEventHandler{recorder: recorder}
	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: interactionResponseStoredEvent(t, interactionevents.Payload{
		RequestID:       uuid.NewString(),
		RequestKind:     "human_gate",
		ResponseID:      uuid.NewString(),
		ResponseAction:  "approve",
		OwnerService:    "agent_manager",
		OwnerRequestRef: "human_gate:" + gateID.String(),
		Status:          "waiting",
		Version:         2,
	})})
	if result.Status != eventconsumer.ResultPoison || result.Code != "invalid_response_status" {
		t.Fatalf("HandleEvent() = %+v, want invalid status poison", result)
	}
	if len(recorder.inputs) != 0 {
		t.Fatalf("recorded inputs = %d, want 0", len(recorder.inputs))
	}
}

func TestInteractionResponseEventHandlerMapsDomainErrors(t *testing.T) {
	t.Parallel()

	gateID := uuid.MustParse("44444444-5555-6666-7777-888888888888")
	cases := []struct {
		name   string
		err    error
		status eventconsumer.ResultStatus
		code   string
	}{
		{name: "conflict", err: errs.ErrConflict, status: eventconsumer.ResultPoison, code: "conflicting_human_gate_response"},
		{name: "stale", err: errs.ErrPreconditionFailed, status: eventconsumer.ResultPoison, code: "stale_human_gate_response"},
		{name: "temporary", err: errors.New("database unavailable"), status: eventconsumer.ResultRetry, code: "retryable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := &fakeHumanGateResponseRecorder{
				gates: map[uuid.UUID]entity.HumanGateRequest{
					gateID: {VersionedBase: entity.VersionedBase{ID: gateID, Version: 1}, Status: enum.HumanGateStatusWaiting},
				},
				recordErr: tc.err,
			}
			handler := interactionResponseEventHandler{recorder: recorder}
			result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: interactionResponseStoredEvent(t, interactionevents.Payload{
				RequestID:       uuid.NewString(),
				RequestKind:     "human_gate",
				ResponseID:      uuid.NewString(),
				ResponseAction:  "reject",
				OwnerService:    "agent_manager",
				OwnerRequestRef: gateID.String(),
				Status:          "answered",
				Version:         2,
			})})
			if result.Status != tc.status || result.Code != tc.code {
				t.Fatalf("HandleEvent() = %+v, want %s/%s", result, tc.status, tc.code)
			}
		})
	}
}

func interactionResponseStoredEvent(t *testing.T, payload interactionevents.Payload) eventlog.StoredEvent {
	t.Helper()
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(): %v", err)
	}
	return eventlog.StoredEvent{
		SequenceID: 1,
		Event: eventlog.Event{
			ID:            uuid.New(),
			SourceService: interactionResponseSourceService,
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

type fakeHumanGateResponseRecorder struct {
	gates     map[uuid.UUID]entity.HumanGateRequest
	inputs    []agentservice.RecordHumanGateDecisionInput
	loadErr   error
	recordErr error
}

func (r *fakeHumanGateResponseRecorder) GetHumanGateRequest(_ context.Context, id uuid.UUID) (entity.HumanGateRequest, error) {
	if r.loadErr != nil {
		return entity.HumanGateRequest{}, r.loadErr
	}
	gate, ok := r.gates[id]
	if !ok {
		return entity.HumanGateRequest{}, errs.ErrNotFound
	}
	return gate, nil
}

func (r *fakeHumanGateResponseRecorder) RecordHumanGateDecision(_ context.Context, input agentservice.RecordHumanGateDecisionInput) (entity.HumanGateRequest, error) {
	r.inputs = append(r.inputs, input)
	if r.recordErr != nil {
		return entity.HumanGateRequest{}, r.recordErr
	}
	return entity.HumanGateRequest{
		VersionedBase: entity.VersionedBase{ID: input.HumanGateRequestID},
		Status:        enum.HumanGateStatusResolved,
		Outcome:       input.Outcome,
	}, nil
}
