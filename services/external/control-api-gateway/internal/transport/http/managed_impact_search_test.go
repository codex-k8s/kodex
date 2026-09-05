package httptransport

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const managedImpactPath = "/api/v1/managed-configurations/mcfg_fixture01/revisions/mrev_fixture01/impact"

func TestManagedImpactSearchPageAndReadback(t *testing.T) {
	fixture := func() *cp.GetManagedConfigurationImpactResponse {
		return &cp.GetManagedConfigurationImpactResponse{Impact: &cp.ManagedConfigurationImpact{ConfigurationRef: "mcfg_fixture01", TargetRevisionRef: "mrev_fixture01", Digest: strings.Repeat("a", 64), Total: 12, Page: &cp.PageInfo{NextPageToken: "next-filter"}}}
	}
	for _, query := range []string{"", "  Поиск %_  ", strings.Repeat("я", 200)} {
		client := &catalogRPCRecorder{response: fixture()}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", managedImpactPath+"?query="+url.QueryEscape(query)+"&pageSize=7&pageToken=prior-filter", nil))
		if w.Code != 200 || client.method != cp.PlatformQueryService_GetManagedConfigurationImpact_FullMethodName {
			t.Fatalf("impact status=%d", w.Code)
		}
		r := client.request.(*cp.GetManagedConfigurationImpactRequest)
		if r.Query != query || r.Page.PageSize != 7 || r.Page.PageToken != "prior-filter" || r.ConfigurationRef != "mcfg_fixture01" || r.RevisionRef != "mrev_fixture01" || !strings.Contains(w.Body.String(), `"total":12`) || !strings.Contains(w.Body.String(), `"nextPageToken":"next-filter"`) {
			t.Fatal("managed impact fields lost")
		}
	}
	for _, query := range []string{"pageSize=0", "pageSize=101", "pageToken=" + strings.Repeat("x", 513), "query=a%00b", "query=%FF", "query=" + url.QueryEscape(strings.Repeat("я", 201))} {
		client := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", managedImpactPath+"?"+query, nil))
		if w.Code != 400 || client.method != "" {
			t.Fatal("invalid impact filter reached CP")
		}
	}
	for _, corrupt := range []func(*cp.ManagedConfigurationImpact){
		func(i *cp.ManagedConfigurationImpact) { i.ConfigurationRef = "mcfg_foreign01" },
		func(i *cp.ManagedConfigurationImpact) { i.TargetRevisionRef = "mrev_foreign01" },
		func(i *cp.ManagedConfigurationImpact) { i.Total = -1 },
		func(i *cp.ManagedConfigurationImpact) { i.Total = maximumSafeJSONInteger + 1 },
		func(i *cp.ManagedConfigurationImpact) { i.Digest = "invalid" },
		func(i *cp.ManagedConfigurationImpact) { i.Page.NextPageToken = strings.Repeat("x", 513) },
		func(i *cp.ManagedConfigurationImpact) { i.Page.NextPageToken = string([]byte{0xff}) },
		func(i *cp.ManagedConfigurationImpact) { i.Consumers = make([]*cp.ManagedConfigurationConsumer, 8) },
	} {
		response := fixture()
		corrupt(response.Impact)
		client := &catalogRPCRecorder{response: response}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", managedImpactPath+"?pageSize=7", nil))
		if w.Code != 502 {
			t.Fatal("corrupt managed impact accepted")
		}
	}
	for code, want := range map[codes.Code]int{codes.InvalidArgument: 400, codes.PermissionDenied: 403, codes.NotFound: 404, codes.Unavailable: 503} {
		client := &catalogRPCRecorder{failure: status.Error(code, "private filter/owner")}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", managedImpactPath+"?query=changed&pageToken=stale", nil))
		if w.Code != want || strings.Contains(w.Body.String(), "private filter") {
			t.Fatal("unsafe impact failure")
		}
	}
}
