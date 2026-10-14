package callback

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestAssistantArtifactSearchMetadataRoute(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		t.Run(string(scope), func(t *testing.T) {
			input, _, _ := assistantOwnCurrentFixture(scope)
			input.ProjectRef = "prj_current123"
			client := &assistantSearchDiagnosticClient{response: &cp.SearchAssistantResourcesResponse{Results: []*cp.SearchResult{{
				Kind: cp.SearchResultKind_SEARCH_RESULT_KIND_ARTIFACT, Ref: "art_notes123", ProjectRef: input.ProjectRef,
				Title: "Заметка проекта.md", Subtitle: "text/markdown", State: "ACTIVE",
			}}}}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			before := runtimecontract.RuntimeMCPToolNames(input)
			result, err := server.findPlatformResources(t.Context(), input, map[string]any{"query": "Заметка проекта"})
			if err != nil {
				t.Fatalf("artifact metadata route unavailable: %v", err)
			}
			items := result.(map[string]any)["results"].([]map[string]any)
			if len(items) != 1 || len(items[0]) != 8 || items[0]["kind"] != "ARTIFACT" || items[0]["title"] != "Заметка проекта.md" ||
				items[0]["route"] != "/projects/prj_current123/files?artifactRef=art_notes123" || items[0]["requires_context_switch"] != false ||
				client.request.Query != "Заметка проекта" || !reflect.DeepEqual(before, runtimecontract.RuntimeMCPToolNames(input)) {
				t.Fatal("artifact metadata changed its projection, query or runtime authority")
			}
			client.response.Results[0].ProjectRef = "prj_foreign123"
			result, err = server.findPlatformResources(t.Context(), input, map[string]any{"query": "Заметка проекта"})
			if err != nil || result.(map[string]any)["results"].([]map[string]any)[0]["requires_context_switch"] != true {
				t.Fatal("foreign project navigation lost its context-switch hint")
			}
		})
	}
}

func TestAssistantArtifactSearchRejectsInjectedIdentityAndUnknownKind(t *testing.T) {
	input, _, _ := assistantOwnCurrentFixture(runtimecontract.AssistantScopeProject)
	for _, item := range []*cp.SearchResult{
		{Kind: cp.SearchResultKind_SEARCH_RESULT_KIND_ARTIFACT, Ref: "art_notes123&other=1", ProjectRef: "prj_current123"},
		{Kind: cp.SearchResultKind_SEARCH_RESULT_KIND_ARTIFACT, Ref: "art_notes123#fragment", ProjectRef: "prj_current123"},
		{Kind: cp.SearchResultKind_SEARCH_RESULT_KIND_ARTIFACT, Ref: "art_notes123", ProjectRef: "prj_current123/../foreign"},
		{Kind: cp.SearchResultKind_SEARCH_RESULT_KIND_ARTIFACT, Ref: "art_notes123", ProjectRef: ""},
		{Kind: cp.SearchResultKind(999), Ref: "art_notes123", ProjectRef: "prj_current123"},
	} {
		client := &assistantSearchDiagnosticClient{response: &cp.SearchAssistantResourcesResponse{Results: []*cp.SearchResult{item}}}
		server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
		if result, err := server.findPlatformResources(t.Context(), input, map[string]any{"query": "Заметка"}); err == nil || result != nil {
			t.Fatal("unsafe identity or unknown kind produced a resource route")
		}
	}
	_, route := assistantResourceRoute(&cp.SearchResult{Kind: cp.SearchResultKind_SEARCH_RESULT_KIND_ARTIFACT, Ref: "art_notes123", ProjectRef: "prj_current123"})
	parsed, err := url.Parse(route)
	if err != nil || parsed.Path != "/projects/prj_current123/files" || parsed.Fragment != "" || len(parsed.Query()) != 1 || parsed.Query().Get("artifactRef") != "art_notes123" {
		t.Fatal("artifact route differs from the canonical file-screen locator")
	}
}

func TestAssistantArtifactSearchSchemaAndGrantGuidance(t *testing.T) {
	tool := assistantResourceSearchTool()
	output := tool["outputSchema"].(map[string]any)
	items := output["properties"].(map[string]any)["results"].(map[string]any)["items"].(map[string]any)
	properties := items["properties"].(map[string]any)
	kinds := properties["kind"].(map[string]any)["enum"].([]string)
	found := false
	for _, kind := range kinds {
		found = found || kind == "ARTIFACT"
	}
	if !found || len(properties) != 8 || output["additionalProperties"] != false {
		t.Fatal("closed metadata schema omitted artifacts or gained file content")
	}
	for _, guidance := range []string{"search_files", "PROJECT", "read_file", "file catalog", "does not grant", "cannot verify"} {
		if !strings.Contains(tool["description"].(string), guidance) {
			t.Fatalf("missing bounded artifact guidance: %s", guidance)
		}
	}
}
