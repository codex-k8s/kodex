package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type catalogRPCRecorder struct {
	grpc.ClientConnInterface
	method   string
	request  proto.Message
	response proto.Message
	failure  error
}

func (client *catalogRPCRecorder) Invoke(_ context.Context, method string, request, response any, _ ...grpc.CallOption) error {
	client.method, client.request = method, proto.Clone(request.(proto.Message))
	if client.failure != nil {
		return client.failure
	}
	proto.Merge(response.(proto.Message), client.response)
	return nil
}

func catalogTestHandler(client *catalogRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{
		Query: controlplanev1.NewPlatformQueryServiceClient(client),
	}})
}

func TestOrganizationCatalogsForwardFiltersAndCursorWithoutProjectFanout(t *testing.T) {
	t.Parallel()
	next := &controlplanev1.PageInfo{NextPageToken: "next-page"}
	for _, tc := range []struct {
		path, rpc string
		response  proto.Message
	}{
		{"agents", "ListAgents", &controlplanev1.ListAgentsResponse{Page: next}},
		{"workflows", "ListWorkflows", &controlplanev1.ListWorkflowsResponse{Page: next}},
		{"schedules", "ListSchedules", &controlplanev1.ListSchedulesResponse{Page: next}},
		{"runtime-environments", "ListRuntimeEnvironmentSets", &controlplanev1.ListRuntimeEnvironmentSetsResponse{Page: next}},
		{"runtime-secrets", "ListRuntimeSecrets", &controlplanev1.ListRuntimeSecretsResponse{Page: next}},
		{"project-memberships", "ListProjectMemberships", &controlplanev1.ListProjectMembershipsResponse{Page: next}},
	} {
		for _, project := range []string{"", "prj_fixture01"} {
			t.Run(tc.path+"/"+project, func(t *testing.T) {
				client := &catalogRPCRecorder{response: tc.response}
				query := url.Values{"query": {"лид"}, "pageSize": {"30"}, "pageToken": {"cursor-1"}}
				if project != "" {
					query.Set("projectRef", project)
				}
				recorder := httptest.NewRecorder()
				catalogTestHandler(client).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/"+tc.path+"?"+query.Encode(), nil))
				if recorder.Code != http.StatusOK || !strings.HasSuffix(client.method, "/"+tc.rpc) {
					t.Fatalf("route status=%d method=%s body=%s", recorder.Code, client.method, recorder.Body.String())
				}
				request := client.request.(interface {
					GetProjectRef() string
					GetQuery() string
					GetPage() *controlplanev1.PageRequest
				})
				if request.GetProjectRef() != project || request.GetQuery() != "лид" ||
					request.GetPage().GetPageSize() != 30 || request.GetPage().GetPageToken() != "cursor-1" {
					t.Fatal("catalog filter or cursor changed")
				}
				var page struct {
					Items         []json.RawMessage `json:"items"`
					NextPageToken string            `json:"nextPageToken"`
				}
				if json.Unmarshal(recorder.Body.Bytes(), &page) != nil || page.Items == nil || page.NextPageToken != "next-page" {
					t.Fatalf("invalid empty page: %s", recorder.Body.String())
				}
				if tc.path == "runtime-secrets" && recorder.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("secret metadata became cacheable")
				}
			})
		}
	}
}

func TestProjectMembershipsForwardSearchAndPagination(t *testing.T) {
	client := &catalogRPCRecorder{response: &controlplanev1.ListProjectMembershipsResponse{Page: &controlplanev1.PageInfo{NextPageToken: "next"}}}
	recorder := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/projects/prj_fixture01/members?query=member&pageSize=20&pageToken=first", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	request := client.request.(*controlplanev1.ListProjectMembershipsRequest)
	if request.GetProjectRef() != "prj_fixture01" || request.GetQuery() != "member" || request.GetPage().GetPageSize() != 20 || request.GetPage().GetPageToken() != "first" || !strings.Contains(recorder.Body.String(), `"nextPageToken":"next"`) {
		t.Fatal("membership query or cursor changed")
	}
}

func TestManagedCatalogPreservesSummaryAndFilters(t *testing.T) {
	configuration, revision := managedFixture()
	configuration.CurrentRevision = &controlplanev1.ManagedConfigurationRevision{Ref: revision.Ref, Revision: revision.Revision, State: revision.State, Digest: revision.Digest}
	client := &catalogRPCRecorder{response: &controlplanev1.ListManagedConfigurationsResponse{Configurations: []*controlplanev1.ManagedConfigurationSet{configuration}, Total: 2, Page: &controlplanev1.PageInfo{NextPageToken: "next"}}}
	recorder := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/managed-configurations?kind=PROMPT_TEMPLATE&query=TYPE_&projectRef=prj_fixture01&pageSize=20&pageToken=first", nil))
	if recorder.Code != http.StatusOK || !strings.HasSuffix(client.method, "/ListManagedConfigurations") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	request := client.request.(*controlplanev1.ListManagedConfigurationsRequest)
	if request.GetProjectRef() != "prj_fixture01" || request.GetKind() != configuration.GetKind() || request.GetQuery() != "TYPE_" || request.GetPage().GetPageSize() != 20 || request.GetPage().GetPageToken() != "first" {
		t.Fatal("managed catalog filters changed")
	}
	var page generated.ManagedConfigurationPage
	if json.Unmarshal(recorder.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.Items[0].Name != configuration.Name || page.Items[0].CurrentRevision == nil || page.Items[0].CurrentRevision.Ref != revision.Ref || page.Total != 2 || page.NextPageToken == nil || *page.NextPageToken != "next" {
		t.Fatalf("summary changed: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `"content"`) || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("catalog exposed revision content or became cacheable")
	}
}

func TestManagedCatalogRejectsMalformedInputsAndProducer(t *testing.T) {
	for _, query := range []string{"kind=UNKNOWN", "kind=UNSPECIFIED", "kind=", "pageSize=4294967346", "pageToken=" + strings.Repeat("a", 513)} {
		client := &catalogRPCRecorder{}
		recorder := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/managed-configurations?"+query, nil))
		if recorder.Code != http.StatusBadRequest || client.method != "" {
			t.Fatalf("invalid catalog query reached RPC: status=%d method=%s", recorder.Code, client.method)
		}
	}
	configuration, revision := managedFixture()
	revision.State = controlplanev1.ManagedConfigurationState(999)
	configuration.CurrentRevision = revision
	for _, response := range []*controlplanev1.ListManagedConfigurationsResponse{
		{Configurations: []*controlplanev1.ManagedConfigurationSet{configuration}, Total: 1},
		{Configurations: []*controlplanev1.ManagedConfigurationSet{nil}, Total: 1},
		{Total: -1},
	} {
		client := &catalogRPCRecorder{response: response}
		recorder := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/managed-configurations", nil))
		if recorder.Code != http.StatusBadGateway {
			t.Fatalf("malformed producer accepted: %s", recorder.Body.String())
		}
	}
	client := &catalogRPCRecorder{response: &controlplanev1.ListManagedConfigurationsResponse{}}
	recorder := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/managed-configurations", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"items":[]`) {
		t.Fatal("empty managed catalog is not an array")
	}
}

func TestOrganizationCatalogRejectsInvalidBoundsBeforeRPC(t *testing.T) {
	t.Parallel()
	for _, query := range []string{
		"pageSize=0", "pageSize=101", "pageSize=4294967346", "projectRef=bad",
		"query=" + url.QueryEscape(strings.Repeat("я", 201)),
		"pageToken=" + strings.Repeat("a", 513),
	} {
		client := &catalogRPCRecorder{}
		recorder := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/agents?"+query, nil))
		if recorder.Code != http.StatusBadRequest || client.method != "" {
			t.Fatalf("invalid query reached RPC: status=%d method=%s", recorder.Code, client.method)
		}
	}
}

func TestOrganizationCatalogPropagatesAuthoritativeDenial(t *testing.T) {
	client := &catalogRPCRecorder{failure: status.Error(codes.PermissionDenied, "denied")}
	recorder := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/runtime-secrets", nil))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("denial status=%d", recorder.Code)
	}
}

func TestMembershipProjectGroupingUsesProducerReference(t *testing.T) {
	client := &catalogRPCRecorder{response: &controlplanev1.ListProjectMembershipsResponse{Memberships: []*controlplanev1.Membership{{
		Ref: "member_fixture01", ProjectRef: "prj_fixture01", Version: 1,
		PlatformRole: controlplanev1.PlatformRole_PLATFORM_ROLE_MEMBER,
	}}}}
	recorder := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/project-memberships", nil))
	var page struct {
		Items []generated.Membership `json:"items"`
	}
	if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &page) != nil || len(page.Items) != 1 ||
		page.Items[0].ProjectRef == nil || *page.Items[0].ProjectRef != "prj_fixture01" {
		t.Fatalf("membership lost project grouping: %s", recorder.Body.String())
	}
}

func TestVFSIncludesAutomationEnvironmentAndAvatarKinds(t *testing.T) {
	for _, kind := range []controlplanev1.VFSNodeKind{
		controlplanev1.VFSNodeKind_VFS_NODE_KIND_AUTOMATION,
		controlplanev1.VFSNodeKind_VFS_NODE_KIND_ENVIRONMENT,
		controlplanev1.VFSNodeKind_VFS_NODE_KIND_AVATAR,
	} {
		recorder := httptest.NewRecorder()
		writeVFSPage(recorder, []*controlplanev1.VFSNode{{
			Ref: "node_fixture01", Path: "/projects/prj_fixture01/node_fixture01", ParentPath: "/projects/prj_fixture01",
			Name: "TYPE_Источник", Kind: kind, Directory: kind != controlplanev1.VFSNodeKind_VFS_NODE_KIND_AVATAR,
			LifecycleState: "ACTIVE", SelectionReason: "DIRECTORY",
		}}, 1, "")
		var page generated.VFSNodePage
		if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &page) != nil || len(page.Items) != 1 ||
			string(page.Items[0].Kind) != strings.TrimPrefix(kind.String(), "VFS_NODE_KIND_") || page.Items[0].Name != "TYPE_Источник" {
			t.Fatalf("new VFS kind rejected or changed: %s", recorder.Body.String())
		}
	}
}

func TestVFSRoutesPreserveTypedSourceAndPagination(t *testing.T) {
	t.Parallel()
	node := &controlplanev1.VFSNode{Ref: "art_fixture01", Path: "/projects/prj_fixture01/results/art_fixture01",
		ParentPath: "/projects/prj_fixture01/results", Name: "TYPE_отчет.txt", Kind: controlplanev1.VFSNodeKind_VFS_NODE_KIND_RESULT,
		Version: 2, Revision: 1, RevisionRef: "arv_fixture01", LifecycleState: "ACTIVE", ScanState: "CLEAN", ResourceKind: "ARTIFACT", Selectable: true, SelectionReason: "AVAILABLE", NextActions: []string{"DOWNLOAD", "DELETE"},
		ProjectRef: "prj_fixture01", EntityRef: "art_fixture01", RunRef: "run_fixture01", SizeBytes: 12, Digest: "sha256:" + strings.Repeat("a", 64)}
	next := &controlplanev1.PageInfo{NextPageToken: "vfs-next"}
	for _, tc := range []struct {
		path, rpc string
		response  proto.Message
	}{
		{"/api/v1/vfs/nodes?path=/projects/prj_fixture01/results&pageSize=20&pageToken=vfs-first", "ListVFSNodes",
			&controlplanev1.ListVFSNodesResponse{Nodes: []*controlplanev1.VFSNode{node}, Total: 21, Page: next}},
		{"/api/v1/vfs/search?query=" + url.QueryEscape("отчет") + "&pageSize=20&pageToken=vfs-first", "SearchVFS",
			&controlplanev1.SearchVFSResponse{Nodes: []*controlplanev1.VFSNode{node}, Total: 21, Page: next}},
	} {
		t.Run(tc.rpc, func(t *testing.T) {
			client := &catalogRPCRecorder{response: tc.response}
			recorder := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if recorder.Code != http.StatusOK || !strings.HasSuffix(client.method, "/"+tc.rpc) {
				t.Fatalf("VFS status=%d method=%s body=%s", recorder.Code, client.method, recorder.Body.String())
			}
			request := client.request.(interface {
				GetPage() *controlplanev1.PageRequest
			})
			if request.GetPage().GetPageSize() != 20 || request.GetPage().GetPageToken() != "vfs-first" {
				t.Fatal("VFS cursor changed")
			}
			if list, ok := client.request.(*controlplanev1.ListVFSNodesRequest); ok && list.GetPath() != node.ParentPath {
				t.Fatal("VFS projection path changed")
			}
			var page generated.VFSNodePage
			if json.Unmarshal(recorder.Body.Bytes(), &page) != nil || len(page.Items) != 1 ||
				page.Items[0].Name != node.Name || page.Items[0].Kind != "RESULT" ||
				page.Items[0].Directory || page.Total != 21 || page.NextPageToken != "vfs-next" {
				t.Fatalf("VFS source changed: %s", recorder.Body.String())
			}
		})
	}
}

func TestVFSRejectsMalformedPathAndUnknownProducerKind(t *testing.T) {
	for _, path := range []string{"/api/v1/vfs/nodes?path=/projects/../secret", "/api/v1/vfs/search?query=x"} {
		client := &catalogRPCRecorder{}
		recorder := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusBadRequest || client.method != "" {
			t.Fatal("invalid VFS filter reached RPC")
		}
	}
	recorder := httptest.NewRecorder()
	writeVFSPage(recorder, []*controlplanev1.VFSNode{{Ref: "node", Path: "/projects", Kind: controlplanev1.VFSNodeKind(999)}}, 1, "")
	if recorder.Code != http.StatusBadGateway {
		t.Fatal("unknown VFS kind accepted")
	}
	recorder = httptest.NewRecorder()
	writeVFSPage(recorder, nil, 0, "")
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"items":[]`) {
		t.Fatal("empty VFS page is not an array")
	}
}

func TestVFSCursorCanCarryNestedProjectPath(t *testing.T) {
	token := strings.Repeat("a", 1200)
	client := &catalogRPCRecorder{response: &controlplanev1.ListVFSNodesResponse{
		Page: &controlplanev1.PageInfo{NextPageToken: token},
	}}
	recorder := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/vfs/nodes?pageToken="+token, nil))
	if recorder.Code != http.StatusOK || client.request.(*controlplanev1.ListVFSNodesRequest).GetPage().GetPageToken() != token {
		t.Fatal("bounded VFS cursor was truncated or rejected")
	}
	client = &catalogRPCRecorder{}
	recorder = httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/vfs/nodes?pageToken="+strings.Repeat("a", 2049), nil))
	if recorder.Code != http.StatusBadRequest || client.method != "" {
		t.Fatal("oversized VFS cursor reached RPC")
	}
}
