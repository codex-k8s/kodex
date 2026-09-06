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

func TestArtifactSourceGroupRejectsAmbiguousFilters(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		values []string
		single controlplanev1.ArtifactSource
	}{
		{name: "duplicate", values: []string{"AGENT_RESULT", "AGENT_RESULT"}},
		{name: "unknown", values: []string{"FUTURE_SOURCE"}},
		{name: "unspecified", values: []string{"UNSPECIFIED"}},
		{name: "empty member", values: []string{""}},
		{name: "exclusive", values: []string{"AGENT_RESULT"}, single: controlplanev1.ArtifactSource_ARTIFACT_SOURCE_CONTROL_CENTER},
		{name: "oversized", values: []string{"CONTROL_CENTER", "AGENT_RESULT", "INTEGRATION_RESULT", "KNOWLEDGE_SOURCE", "INTERACTION_ATTACHMENT", "AGENT_RESULT"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, ok := artifactSourceGroupFilter(&test.values, test.single); ok {
				t.Fatal("ambiguous source filter was accepted")
			}
		})
	}
}

func TestArtifactSourceGroupUsesOneOwnerQuery(t *testing.T) {
	t.Parallel()
	for _, project := range []bool{false, true} {
		client := &artifactQueryStub{}
		server := &Server{control: &controlplaneclient.Client{Query: client}}
		group := generated.ArtifactSourceKindsQuery{"AGENT_RESULT", "INTEGRATION_RESULT"}
		query := generated.Query("literal_%")
		token := generated.PageToken("owner-cursor")
		response := httptest.NewRecorder()
		request := httptest.NewRequest("GET", "/", nil)
		if project {
			server.ListArtifacts(response, request, "prj_12345678", generated.ListArtifactsParams{SourceKinds: &group, Query: &query, PageToken: &token})
		} else {
			server.ListOrganizationArtifacts(response, request, generated.ListOrganizationArtifactsParams{SourceKinds: &group, Query: &query, PageToken: &token})
		}
		got := client.request
		if response.Code != 200 || got == nil || got.Query != string(query) || got.GetPage().GetPageToken() != string(token) || len(got.SourceKinds) != 2 || got.SourceKinds[0] != controlplanev1.ArtifactSource_ARTIFACT_SOURCE_AGENT_RESULT || got.SourceKinds[1] != controlplanev1.ArtifactSource_ARTIFACT_SOURCE_INTEGRATION_RESULT || got.SourceKind != 0 || (got.ProjectRef != "") != project {
			t.Fatalf("group scope or filters changed: status=%d request=%#v", response.Code, got)
		}
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
