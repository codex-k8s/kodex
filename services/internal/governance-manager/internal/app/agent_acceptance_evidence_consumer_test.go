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
	agentevents "github.com/codex-k8s/kodex/libs/go/platformevents/agent"
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governanceservice "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

func TestAgentAcceptanceEvidenceEventHandlerRecordsCompletedAcceptanceEvidence(t *testing.T) {
	t.Parallel()

	packageID := uuid.New()
	recorder := &fakeReleaseAgentEvidenceRecorder{pkg: entity.ReleaseDecisionPackage{
		VersionedBase: entity.VersionedBase{ID: packageID, Version: 6},
	}}
	handler := agentAcceptanceEvidenceEventHandler{recorder: recorder}
	event := agentAcceptanceEvidenceStoredEvent(t, agentevents.EventAcceptanceCompleted, agentevents.Payload{
		AcceptanceResultID:                  "acceptance-1",
		SessionID:                           "session-1",
		RunID:                               "run-1",
		StageID:                             "stage-1",
		RuntimeJobRef:                       "runtime:job/job-1",
		GovernanceReleaseDecisionPackageRef: "governance:release-package/" + packageID.String(),
		Status:                              "passed",
		ReasonCode:                          "acceptance_passed",
		Version:                             4,
	})

	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: event})
	if result.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent() = %+v, want ack", result)
	}
	if recorder.getInputs != 1 || len(recorder.recordInputs) != 1 {
		t.Fatalf("calls get=%d record=%d, want 1/1", recorder.getInputs, len(recorder.recordInputs))
	}
	input := recorder.recordInputs[0]
	if input.ReleaseDecisionPackageID != packageID {
		t.Fatalf("package id = %s, want %s", input.ReleaseDecisionPackageID, packageID)
	}
	if input.Meta.Actor.Type != "service" || input.Meta.Actor.ID != agentAcceptanceEvidenceConsumerActor {
		t.Fatalf("actor = %+v, want agent-manager service", input.Meta.Actor)
	}
	if input.Meta.ExpectedVersion == nil || *input.Meta.ExpectedVersion != 6 {
		t.Fatalf("expected version = %+v, want 6", input.Meta.ExpectedVersion)
	}
	if input.Meta.RequestID != "agent_event:"+event.ID.String() {
		t.Fatalf("request id = %q, want agent event ref", input.Meta.RequestID)
	}
	if !strings.HasPrefix(input.Meta.IdempotencyKey, "agent_acceptance_evidence:") {
		t.Fatalf("idempotency key = %q, want agent acceptance evidence key", input.Meta.IdempotencyKey)
	}
	if len(input.EvidenceRefs) != 1 {
		t.Fatalf("evidence refs = %d, want 1", len(input.EvidenceRefs))
	}
	evidence := input.EvidenceRefs[0]
	if evidence.Kind != agentAcceptanceEvidenceKind || evidence.Ref != "agent:acceptance/acceptance-1" || evidence.RetentionClass != agentAcceptanceEvidenceRetention {
		t.Fatalf("evidence = %+v, want safe agent acceptance ref", evidence)
	}
	if evidence.Summary != "agent acceptance passed: acceptance_passed" || evidence.Digest == "" {
		t.Fatalf("evidence summary/digest = %q/%q, want bounded summary and digest", evidence.Summary, evidence.Digest)
	}
	if !releaseIntegrationRefPresent(input.IntegrationRefs, "agent", "acceptance", "agent:acceptance/acceptance-1", "passed") {
		t.Fatalf("integration refs = %+v, want agent acceptance ref", input.IntegrationRefs)
	}
	if !releaseIntegrationRefPresent(input.IntegrationRefs, "runtime", "job", "runtime:job/job-1", "") {
		t.Fatalf("integration refs = %+v, want runtime job ref", input.IntegrationRefs)
	}
	if string(input.AgentContext) == "" || strings.Contains(string(input.AgentContext), "prompt") || strings.Contains(string(input.AgentContext), "transcript") {
		t.Fatalf("agent context = %s, want safe refs only", input.AgentContext)
	}
}

func TestAgentAcceptanceEvidenceEventHandlerRecordsFailedAcceptanceEvidence(t *testing.T) {
	t.Parallel()

	packageID := uuid.New()
	recorder := &fakeReleaseAgentEvidenceRecorder{pkg: entity.ReleaseDecisionPackage{VersionedBase: entity.VersionedBase{ID: packageID, Version: 2}}}
	handler := agentAcceptanceEvidenceEventHandler{recorder: recorder}
	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: agentAcceptanceEvidenceStoredEvent(t, agentevents.EventAcceptanceFailed, agentevents.Payload{
		AcceptanceResultID:                  "acceptance-2",
		SessionID:                           "session-2",
		GovernanceReleaseDecisionPackageRef: packageID.String(),
		Status:                              "failed",
		ReasonCode:                          "tests_failed",
		Version:                             5,
	})})
	if result.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent() = %+v, want ack", result)
	}
	input := recorder.recordInputs[0]
	if input.EvidenceRefs[0].Summary != "agent acceptance failed: tests_failed" {
		t.Fatalf("summary = %q, want failed reason code", input.EvidenceRefs[0].Summary)
	}
	if got := input.IntegrationRefs[0].ErrorCode; got != "tests_failed" {
		t.Fatalf("error code = %q, want tests_failed", got)
	}
}

func TestAgentAcceptanceEvidenceEventHandlerIgnoresEventsWithoutPackageRef(t *testing.T) {
	t.Parallel()

	recorder := &fakeReleaseAgentEvidenceRecorder{}
	handler := agentAcceptanceEvidenceEventHandler{recorder: recorder}
	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: agentAcceptanceEvidenceStoredEvent(t, agentevents.EventAcceptanceCompleted, agentevents.Payload{
		AcceptanceResultID: "acceptance-1",
		SessionID:          "session-1",
		Status:             "passed",
	})})
	if result.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent() = %+v, want ack", result)
	}
	if recorder.getInputs != 0 || len(recorder.recordInputs) != 0 {
		t.Fatalf("calls get=%d record=%d, want no governance mutation", recorder.getInputs, len(recorder.recordInputs))
	}
}

func TestAgentAcceptanceEvidenceEventHandlerPoisonsInvalidEventShape(t *testing.T) {
	t.Parallel()

	packageID := uuid.New()
	basePayload := agentevents.Payload{
		AcceptanceResultID:                  "acceptance-1",
		SessionID:                           "session-1",
		GovernanceReleaseDecisionPackageRef: packageID.String(),
		Status:                              "passed",
		Version:                             1,
	}
	cases := []struct {
		name  string
		event eventlog.StoredEvent
		code  string
	}{
		{
			name:  "wrong source",
			event: agentAcceptanceEvidenceStoredEvent(t, agentevents.EventAcceptanceCompleted, basePayload),
			code:  "invalid_source_service",
		},
		{
			name:  "wrong aggregate",
			event: agentAcceptanceEvidenceStoredEvent(t, agentevents.EventAcceptanceCompleted, basePayload),
			code:  "invalid_aggregate_type",
		},
		{
			name: "invalid release package ref",
			event: agentAcceptanceEvidenceStoredEvent(t, agentevents.EventAcceptanceCompleted, agentevents.Payload{
				AcceptanceResultID:                  "acceptance-1",
				SessionID:                           "session-1",
				GovernanceReleaseDecisionPackageRef: "governance:release-package/not-a-uuid",
				Status:                              "passed",
			}),
			code: "invalid_release_package_ref",
		},
		{
			name: "missing acceptance refs",
			event: agentAcceptanceEvidenceStoredEvent(t, agentevents.EventAcceptanceCompleted, agentevents.Payload{
				GovernanceReleaseDecisionPackageRef: packageID.String(),
				Status:                              "passed",
			}),
			code: "missing_agent_acceptance_refs",
		},
		{
			name: "completed event with failed status",
			event: agentAcceptanceEvidenceStoredEvent(t, agentevents.EventAcceptanceCompleted, agentevents.Payload{
				AcceptanceResultID:                  "acceptance-1",
				SessionID:                           "session-1",
				GovernanceReleaseDecisionPackageRef: packageID.String(),
				Status:                              "failed",
			}),
			code: "invalid_acceptance_status",
		},
	}
	cases[0].event.SourceService = "runtime-manager"
	cases[1].event.AggregateType = agentevents.AggregateRun

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := &fakeReleaseAgentEvidenceRecorder{pkg: entity.ReleaseDecisionPackage{VersionedBase: entity.VersionedBase{ID: packageID, Version: 1}}}
			handler := agentAcceptanceEvidenceEventHandler{recorder: recorder}
			result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: tc.event})
			if result.Status != eventconsumer.ResultPoison || result.Code != tc.code {
				t.Fatalf("HandleEvent() = %+v, want poison/%s", result, tc.code)
			}
			if len(recorder.recordInputs) != 0 {
				t.Fatalf("record calls = %d, want no mutation", len(recorder.recordInputs))
			}
		})
	}
}

func TestAgentAcceptanceEvidenceEventHandlerMapsDomainErrors(t *testing.T) {
	t.Parallel()

	packageID := uuid.New()
	cases := []struct {
		name      string
		getErr    error
		recordErr error
		status    eventconsumer.ResultStatus
		code      string
	}{
		{name: "unknown package", getErr: errs.ErrNotFound, status: eventconsumer.ResultPoison, code: "unknown_agent_acceptance_evidence_ref"},
		{name: "invalid", recordErr: errs.ErrInvalidArgument, status: eventconsumer.ResultPoison, code: "invalid_agent_acceptance_evidence"},
		{name: "conflict", recordErr: errs.ErrConflict, status: eventconsumer.ResultPoison, code: "conflicting_agent_acceptance_evidence"},
		{name: "forbidden", recordErr: errs.ErrForbidden, status: eventconsumer.ResultPoison, code: "forbidden_agent_acceptance_evidence"},
		{name: "stale", recordErr: errs.ErrPreconditionFailed, status: eventconsumer.ResultPoison, code: "stale_agent_acceptance_evidence"},
		{name: "temporary", recordErr: errors.New("database unavailable"), status: eventconsumer.ResultRetry, code: "retryable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := &fakeReleaseAgentEvidenceRecorder{
				pkg:       entity.ReleaseDecisionPackage{VersionedBase: entity.VersionedBase{ID: packageID, Version: 1}},
				getErr:    tc.getErr,
				recordErr: tc.recordErr,
			}
			handler := agentAcceptanceEvidenceEventHandler{recorder: recorder}
			result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: agentAcceptanceEvidenceStoredEvent(t, agentevents.EventAcceptanceCompleted, agentevents.Payload{
				AcceptanceResultID:                  "acceptance-1",
				SessionID:                           "session-1",
				GovernanceReleaseDecisionPackageRef: packageID.String(),
				Status:                              "passed",
			})})
			if result.Status != tc.status || result.Code != tc.code {
				t.Fatalf("HandleEvent() = %+v, want %s/%s", result, tc.status, tc.code)
			}
		})
	}
}

func TestAgentAcceptanceEvidenceIdempotencyKeyIncludesSafeOutcomeFingerprint(t *testing.T) {
	t.Parallel()

	packageID := uuid.New()
	event := agentAcceptanceEvidenceStoredEvent(t, agentevents.EventAcceptanceCompleted, agentevents.Payload{
		AcceptanceResultID: "acceptance-1",
		SessionID:          "session-1",
		Status:             "passed",
		Version:            1,
	})
	payload := agentevents.Payload{AcceptanceResultID: "acceptance-1", Status: "passed"}
	first := agentAcceptanceEvidenceIdempotencyKey(event, payload, packageID, "passed")
	replayed := agentAcceptanceEvidenceIdempotencyKey(event, payload, packageID, "passed")
	conflicting := agentAcceptanceEvidenceIdempotencyKey(event, payload, packageID, "failed")
	if first == "" || first != replayed || first == conflicting {
		t.Fatalf("idempotency keys first=%q replayed=%q conflicting=%q", first, replayed, conflicting)
	}
}

func agentAcceptanceEvidenceStoredEvent(t *testing.T, eventType string, payload agentevents.Payload) eventlog.StoredEvent {
	t.Helper()
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(): %v", err)
	}
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	return eventlog.StoredEvent{
		SequenceID: 1,
		Event: eventlog.Event{
			ID:            uuid.New(),
			SourceService: agentAcceptanceEvidenceSourceService,
			EventType:     eventType,
			SchemaVersion: agentevents.SchemaVersion,
			AggregateType: agentevents.AggregateAcceptance,
			AggregateID:   uuid.New(),
			Payload:       payloadBytes,
			OccurredAt:    now,
		},
		RecordedAt: now,
	}
}

func releaseIntegrationRefPresent(refs []value.ReleaseIntegrationRef, domain string, kind string, ref string, status string) bool {
	for _, item := range refs {
		if item.Domain == domain && item.Kind == kind && item.Ref == ref && item.Status == status {
			return true
		}
	}
	return false
}

type fakeReleaseAgentEvidenceRecorder struct {
	pkg          entity.ReleaseDecisionPackage
	getErr       error
	recordErr    error
	getInputs    int
	recordInputs []governanceservice.RecordReleaseAgentEvidenceInput
}

func (r *fakeReleaseAgentEvidenceRecorder) GetReleaseDecisionPackage(_ context.Context, input governanceservice.GetReleaseDecisionPackageInput) (entity.ReleaseDecisionPackage, error) {
	r.getInputs++
	if r.getErr != nil {
		return entity.ReleaseDecisionPackage{}, r.getErr
	}
	item := r.pkg
	item.ID = input.ReleaseDecisionPackageID
	return item, nil
}

func (r *fakeReleaseAgentEvidenceRecorder) RecordReleaseAgentEvidence(_ context.Context, input governanceservice.RecordReleaseAgentEvidenceInput) (entity.ReleaseDecisionPackage, error) {
	r.recordInputs = append(r.recordInputs, input)
	if r.recordErr != nil {
		return entity.ReleaseDecisionPackage{}, r.recordErr
	}
	item := r.pkg
	item.ID = input.ReleaseDecisionPackageID
	item.AgentContext = input.AgentContext
	item.EvidenceRefs = input.EvidenceRefs
	item.IntegrationRefs = input.IntegrationRefs
	return item, nil
}
