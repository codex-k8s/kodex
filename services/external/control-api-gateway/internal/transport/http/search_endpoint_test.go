package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
)

type searchQueryStub struct {
	controlplanev1.PlatformQueryServiceClient
	request *controlplanev1.SearchPlatformRequest
}

func (stub *searchQueryStub) SearchPlatform(_ context.Context, request *controlplanev1.SearchPlatformRequest, _ ...grpc.CallOption) (*controlplanev1.SearchPlatformResponse, error) {
	stub.request = request
	return &controlplanev1.SearchPlatformResponse{
		Results: []*controlplanev1.SearchResult{{Kind: controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_AGENT, Ref: "agt_employee01", ProjectRef: "prj_project01", Title: "Сотрудник", State: "ACTIVE"}},
		Total:   27, Page: &controlplanev1.PageInfo{NextPageToken: "next-page"},
	}, nil
}

func TestSearchPlatformForwardsFilterAndCursorAndPreservesPage(t *testing.T) {
	query := &searchQueryStub{}
	server := &Server{control: &controlplaneclient.Client{Query: query}}
	projectRef := generated.ProjectRefQuery("prj_project01")
	pageToken := generated.PageToken("opaque-page")
	limit := 7
	response := httptest.NewRecorder()

	server.SearchPlatform(response, httptest.NewRequest(http.MethodGet, "/", nil), generated.SearchPlatformParams{
		Query: "employee", Limit: &limit, ProjectRef: &projectRef, PageToken: &pageToken,
	})

	if query.request.GetProjectRef() != "prj_project01" || query.request.GetPage().GetPageToken() != "opaque-page" || query.request.GetLimit() != 7 {
		t.Fatalf("search request mapping = %v", query.request)
	}
	var body struct {
		Items         []struct{ Kind, Ref string } `json:"items"`
		Total         int64                        `json:"total"`
		NextPageToken string                       `json:"nextPageToken"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if response.Code != http.StatusOK || body.Total != 27 || body.NextPageToken != "next-page" || len(body.Items) != 1 || body.Items[0].Kind != "AGENT" {
		t.Fatalf("search response = status %d body %+v", response.Code, body)
	}
}
