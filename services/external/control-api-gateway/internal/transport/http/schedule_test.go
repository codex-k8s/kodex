package httptransport

import (
	"context"
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
}
