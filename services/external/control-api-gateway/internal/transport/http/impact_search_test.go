package httptransport

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestImpactSearchTypedMappingAndBounds(t *testing.T) {
	for _, route := range []struct {
		path, method string
		response     proto.Message
	}{
		{"/api/v1/runtime-environments/env_fixture01/versions/ever_fixture01/impact", cp.PlatformQueryService_GetRuntimeEnvironmentImpact_FullMethodName,
			&cp.GetRuntimeEnvironmentImpactResponse{EnvironmentRef: "env_fixture01", EnvironmentVersion: 3, TargetVersionRef: "ever_fixture01", TargetDigest: strings.Repeat("a", 64), Page: &cp.PageInfo{NextPageToken: "next-filtered"}}},
		{"/api/v1/runtime-secrets/sec_fixture01/revisions/2/impact", cp.PlatformQueryService_GetRuntimeSecretImpact_FullMethodName,
			&cp.GetRuntimeSecretImpactResponse{SecretRef: "sec_fixture01", SecretVersion: 3, TargetRevision: 2, Page: &cp.PageInfo{NextPageToken: "next-filtered"}}},
	} {
		t.Run(route.method, func(t *testing.T) {
			for _, query := range []string{"", "  Поиск %_  ", strings.Repeat("я", 200)} {
				client := &catalogRPCRecorder{response: route.response}
				w := httptest.NewRecorder()
				catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, route.path+"?query="+url.QueryEscape(query)+"&pageSize=7&pageToken=prior-filtered", nil))
				if w.Code != 200 || client.method != route.method {
					t.Fatalf("impact mapping status=%d", w.Code)
				}
				message := client.request.ProtoReflect()
				if got := message.Get(message.Descriptor().Fields().ByName("query")).String(); got != query {
					t.Fatal("search changed before owner normalization")
				}
				var page *cp.PageRequest
				switch request := client.request.(type) {
				case *cp.GetRuntimeEnvironmentImpactRequest:
					page = request.Page
				case *cp.GetRuntimeSecretImpactRequest:
					page = request.Page
				}
				if page.PageSize != 7 || page.PageToken != "prior-filtered" || !strings.Contains(w.Body.String(), `"nextPageToken":"next-filtered"`) || w.Header().Get("ETag") != `"3"` || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("search lost cursor or response metadata")
				}
			}
			for _, query := range []string{strings.Repeat("я", 201), "a\x00b", string([]byte{0xff})} {
				client := &catalogRPCRecorder{response: route.response}
				w := httptest.NewRecorder()
				catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, route.path+"?query="+url.QueryEscape(query), nil))
				if w.Code != 400 || client.method != "" {
					t.Fatal("malformed impact search reached CP")
				}
			}
			for code, want := range map[codes.Code]int{codes.InvalidArgument: 400, codes.PermissionDenied: 403, codes.NotFound: 404, codes.Unavailable: 503} {
				client := &catalogRPCRecorder{failure: status.Error(code, "private cursor or authority detail")}
				w := httptest.NewRecorder()
				catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, route.path+"?query=changed&pageToken=old-filter", nil))
				if w.Code != want || strings.Contains(w.Body.String(), "private cursor") {
					t.Fatal("unsafe impact error mapping")
				}
			}
		})
	}
}
