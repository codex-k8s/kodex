package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type scheduleQueryStub struct {
	pb.PlatformQueryServiceClient
	request *pb.PreviewScheduleRequest
}

func (stub *scheduleQueryStub) PreviewSchedule(_ context.Context, request *pb.PreviewScheduleRequest, _ ...grpc.CallOption) (*pb.PreviewScheduleResponse, error) {
	stub.request = request
	return &pb.PreviewScheduleResponse{NormalizedCronExpression: "30 2 * * *", DstGapPolicy: "SHIFT_FORWARD",
		DstFoldPolicy: "RUN_ONCE_EARLIEST", MisfirePolicy: "COALESCE", OverlapPolicy: "FORBID",
		Occurrences: []*timestamppb.Timestamp{timestamppb.New(time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC))}}, nil
}

func TestSchedulePreviewMapsCanonicalPoliciesAndRejectsPayloadAuthority(t *testing.T) {
	stub := &scheduleQueryStub{}
	server := &Server{control: &controlplaneclient.Client{Query: stub}}
	body := `{"preset":"CUSTOM","cronExpression":"30 2 * * *","timezone":"Europe/Berlin","dstGapPolicy":"SHIFT_FORWARD","dstFoldPolicy":"RUN_ONCE_EARLIEST","misfirePolicy":"COALESCE","overlapPolicy":"FORBID","after":"2026-03-28T23:00:00Z","limit":1}`
	response := httptest.NewRecorder()
	server.PreviewSchedule(response, httptest.NewRequest(http.MethodPost, "/api/v1/schedules/preview", strings.NewReader(body)), generated.PreviewScheduleParams{})
	if response.Code != http.StatusOK || stub.request == nil || stub.request.Timezone != "Europe/Berlin" || stub.request.Limit != 1 || stub.request.CronExpression != "30 2 * * *" || stub.request.After.AsTime().Hour() != 23 {
		t.Fatal("preview mapping failed")
	}
	var preview generated.SchedulePreview
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil || len(preview.Occurrences) != 1 || preview.Occurrences[0].Hour() != 1 {
		t.Fatalf("preview response: %v", err)
	}
	stub.request = nil
	response = httptest.NewRecorder()
	server.PreviewSchedule(response, httptest.NewRequest(http.MethodPost, "/api/v1/schedules/preview", strings.NewReader(`{"organizationRef":"org_forged","preset":"HOURLY","timezone":"UTC"}`)), generated.PreviewScheduleParams{})
	if response.Code != http.StatusBadRequest || stub.request != nil {
		t.Fatal("payload authority reached RPC")
	}
}
