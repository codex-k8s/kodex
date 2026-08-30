package httptransport

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
)

type artifactQueryStub struct {
	controlplanev1.PlatformQueryServiceClient
	request *controlplanev1.ListArtifactsRequest
}

func (stub *artifactQueryStub) ListArtifacts(
	_ context.Context,
	request *controlplanev1.ListArtifactsRequest,
	_ ...grpc.CallOption,
) (*controlplanev1.ListArtifactsResponse, error) {
	stub.request = request
	return &controlplanev1.ListArtifactsResponse{Page: &controlplanev1.PageInfo{}}, nil
}

type launchRunCommandStub struct {
	controlplanev1.PlatformCommandServiceClient
	request *controlplanev1.LaunchRunRequest
}

func (stub *launchRunCommandStub) LaunchRun(
	_ context.Context,
	request *controlplanev1.LaunchRunRequest,
	_ ...grpc.CallOption,
) (*controlplanev1.LaunchRunResponse, error) {
	stub.request = request
	return &controlplanev1.LaunchRunResponse{}, nil
}

func TestListArtifactsMapsProjectFilters(t *testing.T) {
	t.Parallel()
	queryClient := &artifactQueryStub{}
	server := &Server{control: &controlplaneclient.Client{Query: queryClient}}
	lifecycle := generated.ListArtifactsParamsLifecycleState("DELETED")
	artifactType := generated.ListArtifactsParamsType("DOCUMENT")
	scanState := generated.ListArtifactsParamsScanState("QUARANTINED")
	sourceKind := generated.ListArtifactsParamsSourceKind("INTEGRATION_RESULT")
	runRef := generated.RunRefQuery("run_12345678")
	search := generated.Query("proposal")
	pageSize := generated.PageSize(17)
	pageToken := generated.PageToken("cursor")

	server.ListArtifacts(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), "prj_12345678", generated.ListArtifactsParams{
		RunRef: &runRef, LifecycleState: &lifecycle, Type: &artifactType, ScanState: &scanState,
		SourceKind: &sourceKind, Query: &search, PageSize: &pageSize, PageToken: &pageToken,
	})

	request := queryClient.request
	if request == nil || request.GetProjectRef() != "prj_12345678" || request.GetRunRef() != "run_12345678" ||
		request.GetLifecycleState() != controlplanev1.ArtifactLifecycleState_ARTIFACT_LIFECYCLE_STATE_DELETED ||
		request.GetType() != controlplanev1.ArtifactType_ARTIFACT_TYPE_DOCUMENT ||
		request.GetScanState() != controlplanev1.ArtifactScanState_ARTIFACT_SCAN_STATE_QUARANTINED ||
		request.GetSourceKind() != controlplanev1.ArtifactSource_ARTIFACT_SOURCE_INTEGRATION_RESULT ||
		request.GetQuery() != "proposal" || request.GetPage().GetPageSize() != 17 || request.GetPage().GetPageToken() != "cursor" {
		t.Fatalf("project artifact filters were not mapped: %#v", request)
	}
}

func TestListOrganizationArtifactsKeepsOrganizationScope(t *testing.T) {
	t.Parallel()
	queryClient := &artifactQueryStub{}
	server := &Server{control: &controlplaneclient.Client{Query: queryClient}}
	artifactType := generated.ListOrganizationArtifactsParamsType("TEXT")
	scanState := generated.ListOrganizationArtifactsParamsScanState("CLEAN")
	sourceKind := generated.ListOrganizationArtifactsParamsSourceKind("CONTROL_CENTER")

	server.ListOrganizationArtifacts(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), generated.ListOrganizationArtifactsParams{
		Type: &artifactType, ScanState: &scanState, SourceKind: &sourceKind,
	})

	request := queryClient.request
	if request == nil || request.GetProjectRef() != "" || request.GetRunRef() != "" ||
		request.GetType() != controlplanev1.ArtifactType_ARTIFACT_TYPE_TEXT ||
		request.GetScanState() != controlplanev1.ArtifactScanState_ARTIFACT_SCAN_STATE_CLEAN ||
		request.GetSourceKind() != controlplanev1.ArtifactSource_ARTIFACT_SOURCE_CONTROL_CENTER {
		t.Fatalf("organization artifact filters were not mapped: %#v", request)
	}
}

func TestListArtifactsRejectsUnknownClosedEnum(t *testing.T) {
	t.Parallel()
	queryClient := &artifactQueryStub{}
	server := &Server{control: &controlplaneclient.Client{Query: queryClient}}
	unknownType := generated.ListArtifactsParamsType("EXECUTABLE")
	response := httptest.NewRecorder()

	server.ListArtifacts(response, httptest.NewRequest("GET", "/", nil), "prj_12345678", generated.ListArtifactsParams{Type: &unknownType})

	if response.Code != 400 || queryClient.request != nil {
		t.Fatalf("unknown artifact type was not rejected before RPC: status=%d request=%#v", response.Code, queryClient.request)
	}
}

func TestCreateRunAcceptsMissingTitle(t *testing.T) {
	t.Parallel()
	commandClient := &launchRunCommandStub{}
	server := &Server{control: &controlplaneclient.Client{Command: commandClient}}
	body := `{"projectRef":"prj_12345678","targetRef":"agt_12345678","targetType":"AGENT","task":"Проверить заявку"}`
	response := httptest.NewRecorder()

	server.CreateRun(response, httptest.NewRequest("POST", "/", strings.NewReader(body)), generated.CreateRunParams{IdempotencyKey: "create-run-without-title"})

	if response.Code != 201 || commandClient.request == nil || commandClient.request.GetTitle() != "" {
		t.Fatalf("optional run title was not preserved: status=%d request=%#v", response.Code, commandClient.request)
	}
}
