package callback

import (
	"context"
	"reflect"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
)

type assistantResourceSearchClient struct {
	controlplanev1.RuntimeWorkServiceClient
	request  *controlplanev1.SearchAssistantResourcesRequest
	response *controlplanev1.SearchAssistantResourcesResponse
}

func (client *assistantResourceSearchClient) SearchAssistantResources(_ context.Context, request *controlplanev1.SearchAssistantResourcesRequest, _ ...grpc.CallOption) (*controlplanev1.SearchAssistantResourcesResponse, error) {
	client.request = request
	return client.response, nil
}

func TestAssistantResourceSearchUsesOnlyVerifiedLeaseAndReturnsSafeRoutes(t *testing.T) {
	t.Parallel()
	client := &assistantResourceSearchClient{response: &controlplanev1.SearchAssistantResourcesResponse{
		Results: []*controlplanev1.SearchResult{
			{Kind: controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_PROJECT, Ref: "prj_target123", ProjectRef: "prj_target123", Title: "Marketplace"},
			{Kind: controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_AGENT, Ref: "agt_manager123", ProjectRef: "prj_target123", Title: "Project Manager"},
			{Kind: controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_ROLE_IMAGE, Ref: "imgrec_image123", ProjectRef: "prj_target123", Title: "Role image"},
			{Kind: controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_RUNTIME_ENVIRONMENT, Ref: "renv_test123", ProjectRef: "prj_target123", Title: "Runtime environment"},
			{Kind: controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_SCHEDULE, Ref: "sch_test123", ProjectRef: "prj_target123", Title: "Schedule"},
			{Kind: controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_SECRET, Ref: "sec_test123", ProjectRef: "prj_target123", Title: "Secret metadata"},
			{Kind: controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_INTEGRATION, Ref: "int_test123", Title: "Connection"},
		},
	}}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
	input := runtimecontract.RunnerInput{SystemAssistant: true, LeaseRef: "lse_current123", LeaseFence: "private-fence", LeaseGeneration: 4, ProjectRef: "prj_current123"}
	result, err := server.findPlatformResources(t.Context(), input, map[string]any{"query": "  Marketplace  "})
	if err != nil {
		t.Fatal(err)
	}
	if client.request.GetLeaseRef() != input.LeaseRef || client.request.GetFence() != input.LeaseFence ||
		client.request.GetGeneration() != input.LeaseGeneration || client.request.GetQuery() != "Marketplace" {
		t.Fatalf("resource search lost execution binding: %#v", client.request)
	}
	values := result.(map[string]any)
	items := values["results"].([]map[string]any)
	if len(items) != 7 || values["current_project_ref"] != input.ProjectRef ||
		!reflect.DeepEqual([]any{items[0]["route"], items[1]["route"], items[2]["route"], items[3]["route"], items[4]["route"], items[5]["route"], items[6]["route"]}, []any{
			"/projects/prj_target123", "/projects/prj_target123/agents/agt_manager123",
			"/projects/prj_target123/role-images/imgrec_image123", "/projects/prj_target123/environments/renv_test123",
			"/projects/prj_target123/automations?scheduleRef=sch_test123", "/projects/prj_target123/secrets", "/integrations?connectionRef=int_test123",
		}) || items[0]["requires_context_switch"] != true || items[1]["requires_context_switch"] != true || items[6]["requires_context_switch"] != true {
		t.Fatalf("resource search lost cross-project navigation hints: %#v", result)
	}
	if parameters, permission, _, ok := safeToolCallParameters(input, "find_platform_resources", map[string]any{"query": "Marketplace"}); !ok || permission != "platform.resources.search" || len(parameters) != 0 {
		t.Fatalf("resource search projected query or wrong permission: parameters=%#v permission=%q allowed=%v", parameters, permission, ok)
	}
}

func TestAssistantResourceSearchRejectsInvalidScopeAndResults(t *testing.T) {
	t.Parallel()
	input := runtimecontract.RunnerInput{SystemAssistant: true, LeaseRef: "lse_current123", LeaseFence: "private-fence", LeaseGeneration: 1}
	for _, test := range []struct {
		name      string
		input     runtimecontract.RunnerInput
		arguments map[string]any
		response  *controlplanev1.SearchAssistantResourcesResponse
	}{
		{"ordinary runtime", runtimecontract.RunnerInput{LeaseRef: input.LeaseRef, LeaseFence: input.LeaseFence, LeaseGeneration: 1}, map[string]any{"query": "market"}, &controlplanev1.SearchAssistantResourcesResponse{}},
		{"missing fence", runtimecontract.RunnerInput{SystemAssistant: true, LeaseRef: input.LeaseRef, LeaseGeneration: 1}, map[string]any{"query": "market"}, &controlplanev1.SearchAssistantResourcesResponse{}},
		{"unknown argument", input, map[string]any{"query": "market", "actor": "owner"}, &controlplanev1.SearchAssistantResourcesResponse{}},
		{"short query", input, map[string]any{"query": "a"}, &controlplanev1.SearchAssistantResourcesResponse{}},
		{"unknown kind", input, map[string]any{"query": "market"}, &controlplanev1.SearchAssistantResourcesResponse{Results: []*controlplanev1.SearchResult{{Kind: controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_UNSPECIFIED, Ref: "prj_target123", ProjectRef: "prj_target123"}}}},
		{"forged project route", input, map[string]any{"query": "market"}, &controlplanev1.SearchAssistantResourcesResponse{Results: []*controlplanev1.SearchResult{{Kind: controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_PROJECT, Ref: "prj_target123", ProjectRef: "prj_other123"}}}},
		{"unsafe ref", input, map[string]any{"query": "market"}, &controlplanev1.SearchAssistantResourcesResponse{Results: []*controlplanev1.SearchResult{{Kind: controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_AGENT, Ref: "../malicious", ProjectRef: "prj_target123"}}}},
		{"projectless secret", input, map[string]any{"query": "market"}, &controlplanev1.SearchAssistantResourcesResponse{Results: []*controlplanev1.SearchResult{{Kind: controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_SECRET, Ref: "sec_test123"}}}},
		{"integration with project", input, map[string]any{"query": "market"}, &controlplanev1.SearchAssistantResourcesResponse{Results: []*controlplanev1.SearchResult{{Kind: controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_INTEGRATION, Ref: "int_test123", ProjectRef: "prj_target123"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := &assistantResourceSearchClient{response: test.response}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			if result, err := server.findPlatformResources(t.Context(), test.input, test.arguments); err == nil {
				t.Fatalf("accepted invalid assistant search: %#v", result)
			}
		})
	}
}
