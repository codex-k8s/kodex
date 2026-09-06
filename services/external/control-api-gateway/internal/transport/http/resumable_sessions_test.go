package httptransport

import (
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func resumableRunFixture(ref, session string) *cp.Run {
	return &cp.Run{Ref: ref, Version: 3, ProjectRef: "prj_fixture01", SessionRef: session,
		State: cp.RunState_RUN_STATE_SUCCEEDED, NextActions: []cp.NextAction{cp.NextAction_NEXT_ACTION_OPEN, cp.NextAction_NEXT_ACTION_ADD_TURN},
		Target: &cp.RunTarget{Target: &cp.RunTarget_AgentRef{AgentRef: "agent_fixture01"}}}
}

func TestResumableSessionsForwardOwnerCatalog(t *testing.T) {
	for _, target := range []string{"", "&targetType=AGENT&targetRef=agent_fixture01", "&targetType=WORKFLOW&targetRef=workflow_fixture01"} {
		item := resumableRunFixture("run_fixture01", "session_fixture01")
		if strings.Contains(target, "WORKFLOW") {
			item.Target = &cp.RunTarget{Target: &cp.RunTarget_WorkflowRef{WorkflowRef: "workflow_fixture01"}}
		}
		client := &catalogRPCRecorder{response: &cp.ListRunsResponse{Runs: []*cp.Run{item}, Total: 43, Page: &cp.PageInfo{NextPageToken: "next_snapshot"}}}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/runs?resumableSessionsOnly=true&projectRef=prj_fixture01&query=review&pageSize=2&pageToken=original_snapshot"+target, nil))
		if w.Code != 200 || client.method != cp.PlatformQueryService_ListRuns_FullMethodName {
			t.Fatalf("catalog rejected: %d", w.Code)
		}
		q := client.request.(*cp.ListRunsRequest)
		if !q.ResumableSessionsOnly || q.ProjectRef != "prj_fixture01" || q.Query != "review" || q.Page.PageSize != 2 || q.Page.PageToken != "original_snapshot" || len(q.States) != 0 ||
			(target == "") != (q.TargetType == "" && q.TargetRef == "") || !strings.Contains(w.Body.String(), `"total":43`) || !strings.Contains(w.Body.String(), `"nextPageToken":"next_snapshot"`) {
			t.Fatal("owner catalog fields lost")
		}
	}
}

func TestResumableSessionsRejectInvalidInput(t *testing.T) {
	for _, query := range []string{
		"resumableSessionsOnly=invalid", "resumableSessionsOnly=true&states=SUCCEEDED",
		"targetType=AGENT&targetRef=agent_fixture01", "resumableSessionsOnly=false&targetType=AGENT&targetRef=agent_fixture01",
		"resumableSessionsOnly=true&targetType=AGENT", "resumableSessionsOnly=true&targetRef=agent_fixture01",
		"resumableSessionsOnly=true&targetType=UNKNOWN&targetRef=agent_fixture01", "resumableSessionsOnly=true&targetType=AGENT&targetRef=",
		"resumableSessionsOnly=true&targetType=AGENT&targetRef=bad!", "resumableSessionsOnly=true&targetType=AGENT&targetRef=" + strings.Repeat("a", 97),
	} {
		client := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/runs?"+query, nil))
		if w.Code != 400 || client.method != "" {
			t.Fatalf("invalid catalog input reached owner: %s %d", query, w.Code)
		}
	}
}

func TestResumableSessionsEmptyAndOrdinaryModes(t *testing.T) {
	for _, query := range []string{"resumableSessionsOnly=true", "resumableSessionsOnly=false&states=RUNNING"} {
		client := &catalogRPCRecorder{response: &cp.ListRunsResponse{}}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/runs?"+query, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":0`) {
			t.Fatalf("empty catalog lost: %d", w.Code)
		}
		q := client.request.(*cp.ListRunsRequest)
		if strings.Contains(query, "false") && (q.ResumableSessionsOnly || len(q.States) != 1 || q.States[0] != cp.RunState_RUN_STATE_RUNNING) {
			t.Fatal("ordinary run catalog changed")
		}
	}
}

func TestResumableSessionsRejectInvalidOwnerPage(t *testing.T) {
	for _, mutate := range []func(*cp.ListRunsResponse){
		func(p *cp.ListRunsResponse) {
			p.Runs = append(p.Runs, resumableRunFixture("run_fixture02", "session_fixture01"))
		},
		func(p *cp.ListRunsResponse) { p.Runs[0].State = cp.RunState_RUN_STATE_RUNNING },
		func(p *cp.ListRunsResponse) { p.Runs[0].NextActions = nil },
		func(p *cp.ListRunsResponse) { p.Runs[0].ProjectRef = "prj_foreign01" },
		func(p *cp.ListRunsResponse) {
			p.Runs[0].Target = &cp.RunTarget{Target: &cp.RunTarget_AgentRef{AgentRef: "agent_foreign01"}}
		},
		func(p *cp.ListRunsResponse) { p.Page = &cp.PageInfo{NextPageToken: "original_snapshot"} },
		func(p *cp.ListRunsResponse) { p.Total = 0 },
	} {
		response := &cp.ListRunsResponse{Runs: []*cp.Run{resumableRunFixture("run_fixture01", "session_fixture01")}, Total: 43}
		mutate(response)
		client := &catalogRPCRecorder{response: response}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/runs?resumableSessionsOnly=true&projectRef=prj_fixture01&targetType=AGENT&targetRef=agent_fixture01&pageToken=original_snapshot", nil))
		if w.Code != 502 {
			t.Fatalf("invalid owner page accepted: %d", w.Code)
		}
	}
}

func TestResumableSessionsOwnerErrors(t *testing.T) {
	for code, want := range map[codes.Code]int{codes.PermissionDenied: 403, codes.NotFound: 404, codes.InvalidArgument: 400, codes.Aborted: 412, codes.FailedPrecondition: 409, codes.Unavailable: 503, codes.DeadlineExceeded: 504} {
		client := &catalogRPCRecorder{failure: status.Error(code, "private owner detail")}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/runs?resumableSessionsOnly=true", nil))
		if w.Code != want || strings.Contains(w.Body.String(), "private owner detail") {
			t.Fatalf("unsafe owner error mapping: %s %d", code, w.Code)
		}
	}
}
