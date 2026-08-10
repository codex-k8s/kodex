package httptransport

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type scheduleReadbackControl struct {
	ControlPlane
	occurrence *controlplanev1.ScheduleOccurrence
}

func (control *scheduleReadbackControl) RunScheduleNow(
	context.Context, *controlplanev1.RunScheduleNowRequest, ...grpc.CallOption,
) (*controlplanev1.RunScheduleNowResponse, error) {
	return &controlplanev1.RunScheduleNowResponse{Occurrence: control.occurrence}, nil
}

func TestRunScheduleNowReturnsTypedOwnerReadbackAndStableLocation(t *testing.T) {
	scheduleID, occurrenceID, targetID := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	control := &scheduleReadbackControl{occurrence: &controlplanev1.ScheduleOccurrence{
		ScheduleId: scheduleID.String(), OccurrenceId: occurrenceID.String(),
		ScheduledFor: timestamppb.New(now), AvailableAt: timestamppb.New(now),
		TargetResourceId: targetID.String(),
		TargetKind:       controlplanev1.ResourceKind_RESOURCE_KIND_ROLE, TargetVersion: 1,
		EffectiveInputSha256: strings.Repeat("a", 64),
		State:                controlplanev1.ScheduleOccurrenceState_SCHEDULE_OCCURRENCE_STATE_QUEUED,
		Attempt:              1, Version: 1,
		RecoveryEvidenceSha256: strings.Repeat("b", 64),
		RecoveryActions: []controlplanev1.ScheduleRecoveryAction{
			controlplanev1.ScheduleRecoveryAction_SCHEDULE_RECOVERY_ACTION_REPAIR,
			controlplanev1.ScheduleRecoveryAction_SCHEDULE_RECOVERY_ACTION_CANCEL,
			controlplanev1.ScheduleRecoveryAction_SCHEDULE_RECOVERY_ACTION_SKIP,
		},
	}}
	server := &Server{control: control, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	request := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/schedules/"+scheduleID.String()+"/run-now", nil)
	writer := httptest.NewRecorder()
	server.RunScheduleNow(writer, request, scheduleID, generated.RunScheduleNowParams{
		IdempotencyKey: uuid.New(), IfMatch: "\"1\"",
	})
	if writer.Code != stdhttp.StatusAccepted ||
		writer.Header().Get("Location") != "/api/v1/schedules/"+scheduleID.String()+"/occurrences" ||
		!strings.Contains(writer.Body.String(), occurrenceID.String()) ||
		strings.Contains(writer.Header().Get("Location"), "pageToken") {
		t.Fatalf("manual occurrence readback is incomplete: code=%d location=%q body=%s",
			writer.Code, writer.Header().Get("Location"), writer.Body.String())
	}
	var readback generated.ScheduleOccurrence
	if err := json.Unmarshal(writer.Body.Bytes(), &readback); err != nil {
		t.Fatalf("manual occurrence readback cannot be decoded: %v", err)
	}
	if len(readback.RecoveryActions) != 3 || readback.RecoveryActions[0] != generated.ScheduleRecoveryActionREPAIR ||
		readback.RecoveryActions[1] != generated.ScheduleRecoveryActionCANCEL ||
		readback.RecoveryActions[2] != generated.ScheduleRecoveryActionSKIP {
		t.Fatalf("authoritative recovery actions were lost: %v", readback.RecoveryActions)
	}
}

func TestScheduleFreshReadContainsCompleteEditableRoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	targetID, promptProfileID := uuid.New(), uuid.New()
	runtimeRevisionID, promptArtifactID := uuid.New(), uuid.New()
	roomID, executionSessionID := uuid.New(), uuid.New()
	source := &controlplanev1.ScheduleSpec{
		TargetResourceId: targetID.String(), Interval: durationpb.New(15 * time.Minute), Timezone: "Europe/Moscow",
		OverlapPolicy: controlplanev1.ScheduleOverlapPolicy_SCHEDULE_OVERLAP_POLICY_QUEUE,
		MisfirePolicy: controlplanev1.ScheduleMisfirePolicy_SCHEDULE_MISFIRE_POLICY_WITHIN_GRACE,
		MisfireGrace:  durationpb.New(90 * time.Second), NextRunAt: timestamppb.New(now.Add(time.Hour)),
		TargetKind: controlplanev1.ResourceKind_RESOURCE_KIND_ROLE, TargetVersion: 7,
		EffectiveInputSha256: strings.Repeat("a", 64), Calendar: "BUSINESS",
		DeliveryPolicy: "EXACTLY_ONCE_EFFECT", MaximumAttempts: 4,
		InitialBackoff: durationpb.New(5 * time.Second), MaximumBackoff: durationpb.New(time.Minute),
		DeadLetterAfter: durationpb.New(24 * time.Hour), PromptProfileId: promptProfileID.String(),
		PromptRevision: 8, SessionPolicy: controlplanev1.ScheduleSessionPolicy_SCHEDULE_SESSION_POLICY_PERSISTENT,
		RoomId: roomID.String(), NotificationPolicy: controlplanev1.ScheduleNotificationPolicy_SCHEDULE_NOTIFICATION_POLICY_ON_ACTION_OR_FAILURE,
		MaximumExecutionDuration: durationpb.New(30 * time.Minute), Coalesce: true,
		RuntimeRevisionId: runtimeRevisionID.String(), TargetType: controlplanev1.ScheduleTargetType_SCHEDULE_TARGET_TYPE_AGENT,
		PromptArtifactId: promptArtifactID.String(), ExecutionSessionId: executionSessionID.String(),
		Ownership: &controlplanev1.ConfigurationOwnership{ManagedBy: controlplanev1.ConfigurationManager_CONFIGURATION_MANAGER_UI,
			Drift: controlplanev1.ConfigurationDrift_CONFIGURATION_DRIFT_NOT_APPLICABLE},
	}
	converted, err := ConvertResource(&controlplanev1.Resource{
		Id: uuid.NewString(), Kind: controlplanev1.ResourceKind_RESOURCE_KIND_SCHEDULE,
		Name: "weekday", State: controlplanev1.LifecycleState_LIFECYCLE_STATE_ACTIVE, Version: 3,
		ProjectionSha256: strings.Repeat("f", 64),
		Spec:             &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_Schedule{Schedule: source}},
		CreatedAt:        timestamppb.New(now), UpdatedAt: timestamppb.New(now),
	})
	if err != nil {
		t.Fatalf("ConvertResource() error = %v", err)
	}
	raw, err := json.Marshal(converted)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var fresh generated.Resource
	if err := json.Unmarshal(raw, &fresh); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	projection := fresh.Spec.Schedule
	if projection == nil {
		t.Fatal("fresh read has no Schedule projection")
	}
	roundTrip, ok := scheduleSpec(generated.ScheduleInput{
		TargetResourceId: projection.TargetResourceId, Cron: projection.Cron,
		IntervalSeconds: projection.IntervalSeconds, Timezone: projection.Timezone,
		Calendar: generated.ScheduleCalendar(projection.Calendar), OverlapPolicy: projection.OverlapPolicy,
		MisfirePolicy: projection.MisfirePolicy, MisfireGraceSeconds: projection.MisfireGraceSeconds,
		DeliveryPolicy: generated.ScheduleDeliveryPolicy(projection.DeliveryPolicy), MaximumAttempts: projection.MaximumAttempts,
		InitialBackoffSeconds: projection.InitialBackoffSeconds, MaximumBackoffSeconds: projection.MaximumBackoffSeconds,
		DeadLetterAfterSeconds: projection.DeadLetterAfterSeconds, PromptProfileId: projection.PromptProfileId,
		PromptRevision: projection.PromptRevision, SessionPolicy: projection.SessionPolicy, RoomId: projection.RoomId,
		NotificationPolicy: projection.NotificationPolicy, MaximumExecutionSeconds: projection.MaximumExecutionSeconds,
		Coalesce: projection.Coalesce, RuntimeRevisionId: projection.RuntimeRevisionId, TargetType: projection.TargetType,
		PlaybookRef: projection.PlaybookRef, PlaybookVersion: projection.PlaybookVersion,
		PromptArtifactId: projection.PromptArtifactId, ExecutionSessionId: projection.ExecutionSessionId,
	})
	if !ok {
		t.Fatal("fresh projection cannot be submitted as ScheduleInput")
	}
	want := proto.Clone(source).(*controlplanev1.ScheduleSpec)
	want.TargetKind = controlplanev1.ResourceKind_RESOURCE_KIND_UNSPECIFIED
	want.TargetVersion, want.EffectiveInputSha256, want.NextRunAt = 0, "", nil
	// drift остаётся только server-authored readback и не принимается обратно
	// из browser payload.
	want.Ownership.Drift = controlplanev1.ConfigurationDrift_CONFIGURATION_DRIFT_UNSPECIFIED
	if !proto.Equal(roundTrip, want) {
		t.Fatalf("editable Schedule state changed during browser round trip:\n got: %v\nwant: %v", roundTrip, want)
	}
}

func TestOwnerGateProjectionRequiresProviderDeliveryProof(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	resource := &controlplanev1.Resource{
		State: controlplanev1.LifecycleState_LIFECYCLE_STATE_WAITING_OWNER,
	}
	spec := &controlplanev1.OwnerGateSpec{
		ProcessRunId: uuid.NewString(), SessionId: uuid.NewString(), TurnId: uuid.NewString(),
		Attempt: 1, ResultSha256: strings.Repeat("a", 64), ImmutableInputSha256: strings.Repeat("b", 64),
		ExpiresAt: timestamppb.New(now.Add(time.Hour)),
	}
	projection, err := ownerGateProjection(resource, spec)
	if err != nil || projection.OwnerGate == nil {
		t.Fatalf("ownerGateProjection() error = %v", err)
	}
	if projection.OwnerGate.DeliveryState != generated.OwnerGateDeliveryStateAWAITINGDELIVERYPROOF ||
		projection.OwnerGate.Resolvable || projection.OwnerGate.NextAction != generated.OwnerGateNextActionWAITFORDELIVERY {
		t.Fatalf("pre-delivery projection is unsafe: %+v", projection.OwnerGate)
	}
	spec.DeliveredAt = timestamppb.New(now)
	spec.DeliveryProviderReceiptSha256 = strings.Repeat("c", 64)
	projection, err = ownerGateProjection(resource, spec)
	if err != nil || projection.OwnerGate == nil ||
		projection.OwnerGate.DeliveryState != generated.OwnerGateDeliveryStateREADY ||
		!projection.OwnerGate.Resolvable || projection.OwnerGate.NextAction != generated.OwnerGateNextActionRESOLVE {
		t.Fatalf("provider-proven projection is not ready: projection=%+v error=%v", projection.OwnerGate, err)
	}
	resource.State = controlplanev1.LifecycleState_LIFECYCLE_STATE_EXPIRED
	projection, err = ownerGateProjection(resource, spec)
	if err != nil || projection.OwnerGate == nil ||
		projection.OwnerGate.DeliveryState != generated.OwnerGateDeliveryStateEXPIRED ||
		projection.OwnerGate.Resolvable || projection.OwnerGate.NextAction != generated.OwnerGateNextActionREADTERMINAL {
		t.Fatalf("expired projection is unsafe: projection=%+v error=%v", projection.OwnerGate, err)
	}
}
