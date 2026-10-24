package httptransport

import (
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestRunGateStateFilters checks all known run and gate state filters, repeated run states,
// literal search and exact owner pagination survive HTTP mapping.
// TestRunGateStateFilters проверяет сохранение всех известных фильтров состояний выполнений и
// шлюзов, повторных параметров состояний, буквального поиска и точной пагинации владельца.
func TestRunGateStateFilters(t *testing.T) {
	for value := int32(1); value <= 7; value++ {
		state := cp.RunState(value)
		client := &catalogRPCRecorder{response: &cp.ListRunsResponse{Page: &cp.PageInfo{NextPageToken: "next"}}}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/runs?projectRef=prj_fixture01&query=search&pageSize=3&pageToken=first&states="+strings.TrimPrefix(state.String(), "RUN_STATE_"), nil))
		if w.Code != 200 || client.method != cp.PlatformQueryService_ListRuns_FullMethodName {
			t.Fatalf("run filter %s: %d", state, w.Code)
		}
		q := client.request.(*cp.ListRunsRequest)
		if len(q.States) != 1 || q.States[0] != state || q.ProjectRef != "prj_fixture01" || q.Query != "search" || q.Page.PageSize != 3 || q.Page.PageToken != "first" || !strings.Contains(w.Body.String(), `"nextPageToken":"next"`) {
			t.Fatal("run filter or cursor lost")
		}
	}
	client := &catalogRPCRecorder{response: &cp.ListRunsResponse{}}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/runs?states=QUEUED&states=RUNNING", nil))
	if w.Code != 200 || len(client.request.(*cp.ListRunsRequest).States) != 2 {
		t.Fatal("repeated state query lost")
	}
	for value := int32(1); value <= 6; value++ {
		state := cp.OwnerGateState(value)
		client := &catalogRPCRecorder{response: &cp.ListOwnerGatesResponse{Page: &cp.PageInfo{NextPageToken: "next"}}}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/owner-gates?projectRef=prj_fixture01&query=search&pageSize=3&pageToken=first&state="+strings.TrimPrefix(state.String(), "OWNER_GATE_STATE_"), nil))
		if w.Code != 200 || client.method != cp.PlatformQueryService_ListOwnerGates_FullMethodName {
			t.Fatalf("gate filter %s: %d", state, w.Code)
		}
		q := client.request.(*cp.ListOwnerGatesRequest)
		if q.State != state || q.Query != "search" || q.ProjectRef != "prj_fixture01" || q.Page.PageSize != 3 || q.Page.PageToken != "first" || !strings.Contains(w.Body.String(), `"nextPageToken":"next"`) {
			t.Fatal("gate filter or cursor lost")
		}
	}
}

// TestRunGateFiltersRejectMalformed checks unknown, unspecified, empty or duplicate states and
// invalid project, query or pagination bounds fail before run/gate catalog RPC.
// TestRunGateFiltersRejectMalformed проверяет отказ неизвестным, неопределённым, пустым либо
// повторным состояниям и неверным проекту, запросу либо пагинации до RPC каталогов выполнений и
// шлюзов.
func TestRunGateFiltersRejectMalformed(t *testing.T) {
	for _, path := range []string{
		"runs?states=UNKNOWN", "runs?states=UNSPECIFIED", "runs?states=", "runs?states=RUNNING&states=RUNNING", "runs?query=%00", "runs?projectRef=bad!", "runs?pageSize=101", "runs?pageToken=" + strings.Repeat("x", 513),
		"owner-gates?state=UNKNOWN", "owner-gates?state=UNSPECIFIED", "owner-gates?state=", "owner-gates?projectRef=bad!", "owner-gates?pageSize=0", "owner-gates?pageToken=" + strings.Repeat("x", 513),
	} {
		client := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/"+path, nil))
		if w.Code != 400 || client.method != "" {
			t.Fatalf("invalid filter reached RPC: %s %d", path, w.Code)
		}
	}
}

// TestRunGateFilterOwnerErrors checks authoritative run and gate filter failures map to concrete
// HTTP statuses without exposing private upstream details.
// TestRunGateFilterOwnerErrors проверяет отображение авторитетных ошибок фильтров выполнений и
// шлюзов в конкретные HTTP-статусы без раскрытия внутренних подробностей upstream.
func TestRunGateFilterOwnerErrors(t *testing.T) {
	for _, path := range []string{"runs?states=RUNNING", "owner-gates?state=OPEN", "owner-gates?query=review&states=APPROVED&states=REJECTED&pageToken=stale_snapshot"} {
		for code, want := range map[codes.Code]int{codes.PermissionDenied: 403, codes.NotFound: 404, codes.InvalidArgument: 400, codes.Unavailable: 503} {
			client := &catalogRPCRecorder{failure: status.Error(code, "private upstream detail")}
			w := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/"+path, nil))
			if w.Code != want || strings.Contains(w.Body.String(), "private upstream detail") {
				t.Fatalf("unsafe filter error: %s %d", path, w.Code)
			}
		}
	}
}
