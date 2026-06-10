package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	eventconsumer "github.com/codex-k8s/kodex/libs/go/eventconsumer"
	eventlog "github.com/codex-k8s/kodex/libs/go/eventlog"
	governanceevents "github.com/codex-k8s/kodex/libs/go/platformevents/governance"
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

func TestSelfDeployGateDecisionEventHandlerRecordsApprovedDecision(t *testing.T) {
	t.Parallel()

	planID := uuid.MustParse("aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa")
	gateRequestID := uuid.MustParse("bbbbbbbb-2222-4222-8222-bbbbbbbbbbbb")
	gateDecisionID := uuid.MustParse("cccccccc-3333-4333-8333-cccccccccccc")
	recorder := &fakeSelfDeployGateDecisionRecorder{plans: map[uuid.UUID]entity.SelfDeployPlan{
		planID: selfDeployGateDecisionPlan(planID, gateRequestID),
	}}
	handler := selfDeployGateDecisionEventHandler{recorder: recorder}

	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: selfDeployGateDecisionStoredEvent(t, governanceevents.Payload{
		GateRequestID:  gateRequestID.String(),
		GateDecisionID: gateDecisionID.String(),
		Outcome:        "approve",
		Status:         "resolved",
		TargetType:     "self_deploy_plan",
		TargetRef:      "agent:self-deploy-plan:" + planID.String(),
		SafeSummary:    "owner approved self-deploy build",
		Version:        4,
	})})
	if result.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent() = %+v, want ack", result)
	}
	if len(recorder.inputs) != 1 {
		t.Fatalf("recorded inputs = %d, want 1", len(recorder.inputs))
	}
	input := recorder.inputs[0]
	if input.SelfDeployPlanID != planID || input.Outcome != agentservice.SelfDeployPlanGateDecisionOutcomeApprove {
		t.Fatalf("input = %+v, want approved plan decision", input)
	}
	if input.Meta.ExpectedVersion == nil || *input.Meta.ExpectedVersion != 7 {
		t.Fatalf("expected version = %+v, want 7", input.Meta.ExpectedVersion)
	}
	if input.Meta.IdempotencyKey != "governance_gate_resolved:"+gateDecisionID.String() {
		t.Fatalf("idempotency key = %q", input.Meta.IdempotencyKey)
	}
	if input.GateRequestRef != "governance:gate_request/"+gateRequestID.String() ||
		input.GateDecisionRef != "governance:gate_decision/"+gateDecisionID.String() {
		t.Fatalf("governance refs = %q/%q", input.GateRequestRef, input.GateDecisionRef)
	}
}

func TestSelfDeployGateDecisionEventHandlerMapsNonApprovedOutcomes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		outcome string
		want    agentservice.SelfDeployPlanGateDecisionOutcome
	}{
		{name: "reject", outcome: "reject", want: agentservice.SelfDeployPlanGateDecisionOutcomeReject},
		{name: "request_changes", outcome: "request_changes", want: agentservice.SelfDeployPlanGateDecisionOutcomeRequestChanges},
		{name: "revise", outcome: "revise", want: agentservice.SelfDeployPlanGateDecisionOutcomeRevise},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			planID := uuid.New()
			gateRequestID := uuid.New()
			recorder := &fakeSelfDeployGateDecisionRecorder{plans: map[uuid.UUID]entity.SelfDeployPlan{
				planID: selfDeployGateDecisionPlan(planID, gateRequestID),
			}}
			handler := selfDeployGateDecisionEventHandler{recorder: recorder}

			result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: selfDeployGateDecisionStoredEvent(t, governanceevents.Payload{
				GateRequestID:  gateRequestID.String(),
				GateDecisionID: uuid.NewString(),
				Outcome:        tc.outcome,
				Status:         "resolved",
				TargetType:     "self_deploy_plan",
				TargetRef:      "agent:self-deploy-plan:" + planID.String(),
				SafeSummary:    "owner decision recorded",
				Version:        2,
			})})
			if result.Status != eventconsumer.ResultAck {
				t.Fatalf("HandleEvent() = %+v, want ack", result)
			}
			if len(recorder.inputs) != 1 || recorder.inputs[0].Outcome != tc.want {
				t.Fatalf("recorded inputs = %+v, want outcome %s", recorder.inputs, tc.want)
			}
		})
	}
}

func TestSelfDeployGateDecisionEventHandlerIgnoresOtherTargets(t *testing.T) {
	t.Parallel()

	recorder := &fakeSelfDeployGateDecisionRecorder{}
	handler := selfDeployGateDecisionEventHandler{recorder: recorder}

	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: selfDeployGateDecisionStoredEvent(t, governanceevents.Payload{
		GateRequestID:  uuid.NewString(),
		GateDecisionID: uuid.NewString(),
		Outcome:        "approve",
		Status:         "resolved",
		TargetType:     "release_package",
		TargetRef:      "governance:release/package-1",
		Version:        1,
	})})
	if result.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent() = %+v, want ack", result)
	}
	if len(recorder.inputs) != 0 {
		t.Fatalf("recorded inputs = %d, want 0", len(recorder.inputs))
	}
}

func TestSelfDeployGateDecisionEventHandlerPoisonsInvalidTargetAndOutcome(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload governanceevents.Payload
		code    string
	}{
		{
			name: "bad target",
			payload: governanceevents.Payload{
				GateRequestID:  uuid.NewString(),
				GateDecisionID: uuid.NewString(),
				Outcome:        "approve",
				Status:         "resolved",
				TargetType:     "self_deploy_plan",
				TargetRef:      "agent:self-deploy-plan:not-a-uuid",
				Version:        1,
			},
			code: "invalid_self_deploy_gate_target",
		},
		{
			name: "unknown outcome",
			payload: governanceevents.Payload{
				GateRequestID:  uuid.NewString(),
				GateDecisionID: uuid.NewString(),
				Outcome:        "unknown",
				Status:         "resolved",
				TargetType:     "self_deploy_plan",
				TargetRef:      "agent:self-deploy-plan:" + uuid.NewString(),
				Version:        1,
			},
			code: "unsupported_self_deploy_gate_outcome",
		},
		{
			name: "stale status",
			payload: governanceevents.Payload{
				GateRequestID:  uuid.NewString(),
				GateDecisionID: uuid.NewString(),
				Outcome:        "approve",
				Status:         "requested",
				TargetType:     "self_deploy_plan",
				TargetRef:      "agent:self-deploy-plan:" + uuid.NewString(),
				Version:        1,
			},
			code: "stale_self_deploy_gate_status",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := selfDeployGateDecisionEventHandler{recorder: &fakeSelfDeployGateDecisionRecorder{}}
			result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: selfDeployGateDecisionStoredEvent(t, tc.payload)})
			if result.Status != eventconsumer.ResultPoison || result.Code != tc.code {
				t.Fatalf("HandleEvent() = %+v, want poison %s", result, tc.code)
			}
		})
	}
}

func TestSelfDeployGateDecisionEventHandlerMapsReplayMismatchAndUnknownPlan(t *testing.T) {
	t.Parallel()

	planID := uuid.New()
	gateRequestID := uuid.New()
	cases := []struct {
		name   string
		err    error
		status eventconsumer.ResultStatus
		code   string
	}{
		{name: "replay", err: nil, status: eventconsumer.ResultAck, code: ""},
		{name: "mismatch", err: errs.ErrConflict, status: eventconsumer.ResultPoison, code: "conflicting_self_deploy_gate_decision"},
		{name: "stale", err: errs.ErrPreconditionFailed, status: eventconsumer.ResultPoison, code: "stale_self_deploy_gate_decision"},
		{name: "temporary", err: errors.New("database unavailable"), status: eventconsumer.ResultRetry, code: "retryable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := &fakeSelfDeployGateDecisionRecorder{
				plans:     map[uuid.UUID]entity.SelfDeployPlan{planID: selfDeployGateDecisionPlan(planID, gateRequestID)},
				recordErr: tc.err,
			}
			handler := selfDeployGateDecisionEventHandler{recorder: recorder}
			result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: selfDeployGateDecisionStoredEvent(t, governanceevents.Payload{
				GateRequestID:  gateRequestID.String(),
				GateDecisionID: uuid.NewString(),
				Outcome:        "approve",
				Status:         "resolved",
				TargetType:     "self_deploy_plan",
				TargetRef:      "agent:self-deploy-plan:" + planID.String(),
				SafeSummary:    "owner approved self-deploy build",
				Version:        3,
			})})
			if result.Status != tc.status || result.Code != tc.code {
				t.Fatalf("HandleEvent() = %+v, want %s/%s", result, tc.status, tc.code)
			}
		})
	}

	handler := selfDeployGateDecisionEventHandler{recorder: &fakeSelfDeployGateDecisionRecorder{}}
	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: selfDeployGateDecisionStoredEvent(t, governanceevents.Payload{
		GateRequestID:  gateRequestID.String(),
		GateDecisionID: uuid.NewString(),
		Outcome:        "approve",
		Status:         "resolved",
		TargetType:     "self_deploy_plan",
		TargetRef:      "agent:self-deploy-plan:" + planID.String(),
		Version:        3,
	})})
	if result.Status != eventconsumer.ResultPoison || result.Code != "unknown_self_deploy_plan" {
		t.Fatalf("HandleEvent() = %+v, want unknown plan poison", result)
	}
}

func selfDeployGateDecisionPlan(planID uuid.UUID, gateRequestID uuid.UUID) entity.SelfDeployPlan {
	return entity.SelfDeployPlan{
		VersionedBase: entity.VersionedBase{ID: planID, Version: 7},
		GovernanceContext: entityGovernanceContext(
			"governance:risk_assessment/"+uuid.NewString(),
			"governance:gate_request/"+gateRequestID.String(),
			"",
		),
		Status: enum.SelfDeployPlanStatusPendingApproval,
	}
}

func entityGovernanceContext(riskAssessmentRef string, gateRequestRef string, gateDecisionRef string) value.GovernanceContextRef {
	return value.GovernanceContextRef{
		RiskAssessmentRef: riskAssessmentRef,
		GateRequestRef:    gateRequestRef,
		GateDecisionRef:   gateDecisionRef,
	}
}

func selfDeployGateDecisionStoredEvent(t *testing.T, payload governanceevents.Payload) eventlog.StoredEvent {
	t.Helper()

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return eventlog.StoredEvent{
		SequenceID: 1,
		Event: eventlog.Event{
			ID:            uuid.New(),
			EventType:     governanceevents.EventGateResolved,
			SourceService: selfDeployGateDecisionSourceService,
			AggregateType: governanceevents.AggregateGate,
			AggregateID:   uuid.New(),
			SchemaVersion: governanceevents.SchemaVersion,
			Payload:       encoded,
			OccurredAt:    time.Now().UTC(),
		},
		RecordedAt: time.Now().UTC(),
	}
}

type fakeSelfDeployGateDecisionRecorder struct {
	plans     map[uuid.UUID]entity.SelfDeployPlan
	inputs    []agentservice.RecordSelfDeployPlanGateDecisionInput
	loadErr   error
	recordErr error
}

func (r *fakeSelfDeployGateDecisionRecorder) GetSelfDeployPlan(_ context.Context, id uuid.UUID) (entity.SelfDeployPlan, error) {
	if r.loadErr != nil {
		return entity.SelfDeployPlan{}, r.loadErr
	}
	plan, ok := r.plans[id]
	if !ok {
		return entity.SelfDeployPlan{}, errs.ErrNotFound
	}
	return plan, nil
}

func (r *fakeSelfDeployGateDecisionRecorder) RecordSelfDeployPlanGateDecision(_ context.Context, input agentservice.RecordSelfDeployPlanGateDecisionInput) (entity.SelfDeployPlan, error) {
	r.inputs = append(r.inputs, input)
	if r.recordErr != nil {
		return entity.SelfDeployPlan{}, r.recordErr
	}
	return entity.SelfDeployPlan{
		VersionedBase: entity.VersionedBase{ID: input.SelfDeployPlanID},
		Status:        enum.SelfDeployPlanStatusApproved,
	}, nil
}
