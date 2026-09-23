package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func projectTrashHandler(recorder *catalogRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{
		Query:   cp.NewPlatformQueryServiceClient(recorder),
		Command: cp.NewPlatformCommandServiceClient(recorder),
	}})
}

func projectTrashFixture() *cp.Project {
	deleted := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	return &cp.Project{
		Ref: "prj_fixture01", Version: 4, Name: "Synthetic project", Language: "en",
		Lifecycle: cp.EntityLifecycle_ENTITY_LIFECYCLE_TRASHED, IntegrationState: "UNKNOWN",
		DeletedAt: timestamppb.New(deleted), PurgeAfter: timestamppb.New(deleted.Add(30 * 24 * time.Hour)),
		NextActions: []cp.NextAction{cp.NextAction_NEXT_ACTION_RESTORE},
	}
}

func TestProjectTrashTypedRoutes(t *testing.T) {
	project := projectTrashFixture()
	for _, tc := range []struct {
		method, path, rpc string
		status            int
		response          proto.Message
	}{
		{http.MethodGet, "/api/v1/projects/trash?pageSize=20&pageToken=cursor", "ListTrashedProjects", http.StatusOK,
			&cp.ListTrashedProjectsResponse{Projects: []*cp.Project{project}, Page: &cp.PageInfo{NextPageToken: "next"}}},
		{http.MethodDelete, "/api/v1/projects/prj_fixture01", "TrashProject", http.StatusOK, &cp.TrashProjectResponse{Project: project}},
		{http.MethodPost, "/api/v1/projects/prj_fixture01/restore", "RestoreProject", http.StatusOK,
			&cp.RestoreProjectResponse{Project: &cp.Project{Ref: project.Ref, Version: 5, Name: project.Name,
				Language: "en", Lifecycle: cp.EntityLifecycle_ENTITY_LIFECYCLE_ACTIVE, IntegrationState: "UNKNOWN"}}},
		{http.MethodPost, "/api/v1/projects/prj_fixture01/purge", "PurgeProject", http.StatusAccepted,
			&cp.PurgeProjectResponse{Project: &cp.Project{Ref: project.Ref, Version: 5, Name: project.Name,
				Language: "en", Lifecycle: cp.EntityLifecycle_ENTITY_LIFECYCLE_PURGE_PENDING,
				IntegrationState: "UNKNOWN", DeletedAt: project.DeletedAt, PurgeAfter: project.PurgeAfter}}},
	} {
		t.Run(tc.rpc, func(t *testing.T) {
			client := &catalogRPCRecorder{response: tc.response}
			writer := httptest.NewRecorder()
			projectTrashHandler(client).ServeHTTP(writer, managedTestRequest(tc.method, tc.path, ""))
			if writer.Code != tc.status || !strings.HasSuffix(client.method, "/"+tc.rpc) {
				t.Fatalf("route status=%d method=%s", writer.Code, client.method)
			}
			if tc.rpc == "ListTrashedProjects" {
				request := client.request.(*cp.ListTrashedProjectsRequest)
				if request.GetPage().GetPageSize() != 20 || request.GetPage().GetPageToken() != "cursor" ||
					!strings.Contains(writer.Body.String(), `"nextPageToken":"next"`) ||
					!strings.Contains(writer.Body.String(), `"purgeAfter"`) {
					t.Fatalf("trash page lost retention metadata: %s", writer.Body.String())
				}
				return
			}
			var mutation *cp.MutationContext
			switch request := client.request.(type) {
			case *cp.TrashProjectRequest:
				mutation = request.GetMutation()
				if request.GetProjectRef() != project.Ref {
					t.Fatal("trash route changed project ref")
				}
			case *cp.RestoreProjectRequest:
				mutation = request.GetMutation()
				if request.GetProjectRef() != project.Ref {
					t.Fatal("restore route changed project ref")
				}
			case *cp.PurgeProjectRequest:
				mutation = request.GetMutation()
				if request.GetProjectRef() != project.Ref {
					t.Fatal("purge route changed project ref")
				}
			default:
				t.Fatalf("unexpected request %T", client.request)
			}
			if mutation.GetExpectedVersion() != 3 || mutation.GetIdempotencyKey() != "managed-fixture-01" {
				t.Fatal("mutation context lost OCC or idempotency")
			}
			for _, header := range []string{"If-Match", "Idempotency-Key"} {
				client.method = ""
				request := managedTestRequest(tc.method, tc.path, "")
				request.Header.Del(header)
				writer := httptest.NewRecorder()
				projectTrashHandler(client).ServeHTTP(writer, request)
				if writer.Code != http.StatusBadRequest || client.method != "" {
					t.Fatalf("missing %s reached owner", header)
				}
			}
		})
	}
}

func TestProjectTrashRejectsInvalidOwnerProjection(t *testing.T) {
	for _, mutate := range []func(*cp.Project){
		func(project *cp.Project) { project.PurgeAfter = nil },
		func(project *cp.Project) { project.PurgeAfter = project.DeletedAt },
		func(project *cp.Project) { project.Lifecycle = cp.EntityLifecycle_ENTITY_LIFECYCLE_ACTIVE },
		func(project *cp.Project) {
			project.Lifecycle = cp.EntityLifecycle_ENTITY_LIFECYCLE_ACTIVE
			project.DeletedAt, project.PurgeAfter = nil, nil
		},
	} {
		project := projectTrashFixture()
		mutate(project)
		client := &catalogRPCRecorder{response: &cp.ListTrashedProjectsResponse{Projects: []*cp.Project{project}}}
		writer := httptest.NewRecorder()
		projectTrashHandler(client).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/projects/trash", nil))
		if writer.Code != http.StatusBadGateway {
			t.Fatalf("invalid deletion metadata leaked: status=%d", writer.Code)
		}
	}
}

func TestProjectTrashAcceptsPendingOwnerProjection(t *testing.T) {
	project := projectTrashFixture()
	project.Lifecycle = cp.EntityLifecycle_ENTITY_LIFECYCLE_PURGE_PENDING
	project.NextActions = nil
	client := &catalogRPCRecorder{response: &cp.ListTrashedProjectsResponse{Projects: []*cp.Project{project}}}
	writer := httptest.NewRecorder()
	projectTrashHandler(client).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/projects/trash", nil))
	if writer.Code != http.StatusOK {
		t.Fatalf("pending project trash status = %d", writer.Code)
	}
}
