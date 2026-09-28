package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type searchQueryStub struct {
	controlplanev1.PlatformQueryServiceClient
	request  *controlplanev1.SearchPlatformRequest
	response *controlplanev1.SearchPlatformResponse
}

func TestSearchRejectsMalformedInputBeforeRPC(t *testing.T) {
	for _, query := range []string{"query=x", "query=%20%20", "query=test&limit=0", "query=test&limit=51", "query=test&limit=4294967297", "query=test&pageToken=" + strings.Repeat("a", 513)} {
		client := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/search?"+query, nil))
		if w.Code != 400 || client.method != "" {
			t.Fatal("malformed search reached RPC")
		}
	}
}

func TestSearchRejectsCorruptTypedResults(t *testing.T) {
	for name, change := range map[string]func(*controlplanev1.SearchPlatformResponse){
		"unknown-kind":    func(r *controlplanev1.SearchPlatformResponse) { r.Results[0].Kind = 999 },
		"foreign-project": func(r *controlplanev1.SearchPlatformResponse) { r.Results[0].ProjectRef = "prj_foreign01" },
		"invalid-ref":     func(r *controlplanev1.SearchPlatformResponse) { r.Results[0].Ref = "../../private" },
		"nil":             func(r *controlplanev1.SearchPlatformResponse) { r.Results[0] = nil },
		"timestamp":       func(r *controlplanev1.SearchPlatformResponse) { r.Results[0].UpdatedAt = nil },
		"unsafe-total":    func(r *controlplanev1.SearchPlatformResponse) { r.Total = maximumSafeJSONInteger + 1 },
		"duplicate":       func(r *controlplanev1.SearchPlatformResponse) { r.Results = append(r.Results, r.Results[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			stub := &searchQueryStub{}
			r, _ := stub.SearchPlatform(t.Context(), &controlplanev1.SearchPlatformRequest{})
			change(r)
			client := &catalogRPCRecorder{response: r}
			w := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/search?query=test&projectRef=prj_project01", nil))
			if w.Code != 502 {
				t.Fatalf("corrupt search accepted: %d", w.Code)
			}
		})
	}
}

func TestVFSMismatchedProjectIsNotReturned(t *testing.T) {
	for _, path := range []string{"/api/v1/vfs/nodes?projectRef=prj_project01", "/api/v1/vfs/search?projectRef=prj_project01&query=test"} {
		node := &controlplanev1.VFSNode{Ref: "art_fixture01", Path: "/projects/prj_foreign01/result", ProjectRef: "prj_foreign01", Kind: controlplanev1.VFSNodeKind_VFS_NODE_KIND_RESULT}
		client := &catalogRPCRecorder{response: &controlplanev1.ListVFSNodesResponse{Nodes: []*controlplanev1.VFSNode{node}, Total: 1}}
		if strings.Contains(path, "/search") {
			client.response = &controlplanev1.SearchVFSResponse{Nodes: []*controlplanev1.VFSNode{node}, Total: 1}
		}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 502 || strings.Contains(w.Body.String(), "prj_foreign01") {
			t.Fatal("foreign VFS response returned")
		}
	}
}

func (stub *searchQueryStub) SearchPlatform(_ context.Context, request *controlplanev1.SearchPlatformRequest, _ ...grpc.CallOption) (*controlplanev1.SearchPlatformResponse, error) {
	stub.request = request
	if stub.response != nil {
		return stub.response, nil
	}
	return &controlplanev1.SearchPlatformResponse{
		Results: []*controlplanev1.SearchResult{{Kind: controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_AGENT, Ref: "agt_employee01", ProjectRef: "prj_project01", Title: "Сотрудник", State: "ACTIVE", UpdatedAt: timestamppb.New(time.Unix(100, 0))}},
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

func TestSearchPlatformReturnsAuthorizedArtifact(t *testing.T) {
	query := &searchQueryStub{}
	response, _ := query.SearchPlatform(t.Context(), &controlplanev1.SearchPlatformRequest{})
	response.Results[0].Kind = controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_ARTIFACT
	response.Results[0].Ref = "art_document01"
	response.Results[0].Title = "result.md"
	query.response = response
	server := &Server{control: &controlplaneclient.Client{Query: query}}
	limit := 5
	recorder := httptest.NewRecorder()

	server.SearchPlatform(recorder, httptest.NewRequest(http.MethodGet, "/", nil), generated.SearchPlatformParams{Query: "result", Limit: &limit})

	var body struct {
		Items []struct{ Kind, Ref, ProjectRef string } `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if recorder.Code != http.StatusOK || len(body.Items) != 1 || body.Items[0].Kind != "ARTIFACT" || body.Items[0].Ref != "art_document01" || body.Items[0].ProjectRef != "prj_project01" {
		t.Fatalf("artifact search response = status %d body %+v", recorder.Code, body)
	}
}
