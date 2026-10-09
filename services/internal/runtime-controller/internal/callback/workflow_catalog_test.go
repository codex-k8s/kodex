package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
)

type workflowCatalogClient struct {
	cp.RuntimeWorkServiceClient
	request  *cp.GetExecutionWorkflowCatalogRequest
	response *cp.GetExecutionWorkflowCatalogResponse
	calls    int
}

func (client *workflowCatalogClient) GetExecutionWorkflowCatalog(_ context.Context, request *cp.GetExecutionWorkflowCatalogRequest, _ ...grpc.CallOption) (*cp.GetExecutionWorkflowCatalogResponse, error) {
	client.calls++
	client.request = request
	return client.response, nil
}

func workflowCatalogFixture() *cp.GetExecutionWorkflowCatalogResponse {
	return &cp.GetExecutionWorkflowCatalogResponse{NextPageToken: "canonical-next", Items: []*cp.ExecutionWorkflowCatalogEntry{{WorkflowRef: "wfl_workflow01", Name: "Рабочий процесс", Purpose: "Безопасная проверка", WorkflowVersion: 7, PublishedRef: "wfv_workflow01", SpecDigest: strings.Repeat("a", 64), InputFields: []*cp.WorkflowInputField{{Key: "field-001", Label: "Задача", ValueType: "LONG_TEXT", Required: true}}, Readiness: &cp.WorkflowLaunchReadiness{AllowedToSubmit: true, Reason: "READY", WorkflowVersion: 7, RevisionRef: "wfv_workflow01", ContextDigest: strings.Repeat("b", 64), OperationalState: "READY"}}}}
}
func TestExecutionWorkflowCatalogClosedProjection(t *testing.T) {
	input := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, AssistantScope: runtimecontract.AssistantScopeNone, ProjectRef: "prj_project001", Capabilities: []string{"platform.run.launch"}, LeaseRef: "lse_origin001", LeaseFence: "fixture-fence", LeaseGeneration: 7}
	client := &workflowCatalogClient{response: workflowCatalogFixture()}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
	listRequest := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":"catalog","method":"tools/list","params":{}}`))
	listResponse := httptest.NewRecorder()
	server.serveMCP(listResponse, listRequest, input)
	if listResponse.Code != http.StatusOK || listResponse.Body.Len() > 32768 {
		t.Fatal("ordinary native catalog exceeds bounded wire")
	}
	t.Logf("ordinary tools/list: %d bytes", listResponse.Body.Len())
	result, err := server.workflowCatalog(t.Context(), input, map[string]any{"query": "Рабочий", "page_token": "canonical"})
	if err != nil || result == nil || client.calls != 1 || client.request.LeaseRef != input.LeaseRef || client.request.Fence != input.LeaseFence || client.request.Generation != 7 || client.request.PageToken != "canonical" {
		t.Fatalf("exact query: %v", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	wire := httptest.NewRecorder()
	server.writeMCPResult(wire, json.RawMessage(`"catalog"`), map[string]any{"content": []map[string]string{{"type": "text", "text": string(encoded)}}, "structuredContent": result, "isError": false})
	if wire.Body.Len() > 1<<20 {
		t.Fatal("full workflow catalog wire exceeds runner boundary")
	}
	t.Logf("workflow catalog full text/structuredContent wire: %d bytes", wire.Body.Len())
	for _, key := range []string{"actor_ref", "project_ref", "workflow_ref", "offset", "page_size"} {
		if _, err := server.workflowCatalog(t.Context(), input, map[string]any{key: "forged"}); err == nil {
			t.Fatal("caller authority or noncanonical pagination accepted")
		}
	}
	copy := input
	copy.Capabilities = nil
	if _, err := server.workflowCatalog(t.Context(), copy, nil); err == nil || client.calls != 1 {
		t.Fatal("catalog without launch authority reached RPC")
	}
	copy = input
	copy.AssistantScope = runtimecontract.AssistantScopeProject
	if _, err := server.workflowCatalog(t.Context(), copy, nil); err == nil || client.calls != 1 {
		t.Fatal("assistant scope catalog accepted")
	}
	projection, capability, grant, ok := safeToolCallParameters(input, "get_workflow_catalog", nil)
	if !ok || capability != "platform.run.launch" || grant != "" || len(projection) != 0 {
		t.Fatal("catalog safe activity boundary changed")
	}
	client.response = workflowCatalogFixture()
	client.response.Items[0].Readiness.OperationalState = "UNKNOWN"
	unknown, err := server.workflowCatalog(t.Context(), input, nil)
	if err != nil || unknown.(map[string]any)["items"].([]map[string]any)[0]["readiness"].(map[string]any)["operational_state"] != "UNKNOWN" {
		t.Fatal("owner readiness UNKNOWN was lost or fabricated")
	}
	for name, mutate := range map[string]func(*cp.GetExecutionWorkflowCatalogResponse){
		"missing pins":      func(r *cp.GetExecutionWorkflowCatalogResponse) { r.Items[0].PublishedRef = "" },
		"wrong revision":    func(r *cp.GetExecutionWorkflowCatalogResponse) { r.Items[0].Readiness.RevisionRef = "wfv_foreign001" },
		"unknown readiness": func(r *cp.GetExecutionWorkflowCatalogResponse) { r.Items[0].Readiness.Reason = "UNKNOWN" },
		"unknown type":      func(r *cp.GetExecutionWorkflowCatalogResponse) { r.Items[0].InputFields[0].ValueType = "OTHER" },
		"duplicate":         func(r *cp.GetExecutionWorkflowCatalogResponse) { r.Items = append(r.Items, r.Items[0]) },
		"oversize":          func(r *cp.GetExecutionWorkflowCatalogResponse) { r.Items[0].Purpose = strings.Repeat("a", 32768) },
	} {
		t.Run(name, func(t *testing.T) {
			client.response = workflowCatalogFixture()
			mutate(client.response)
			if _, err := server.workflowCatalog(t.Context(), input, nil); err == nil {
				t.Fatal("malformed owner projection accepted")
			}
		})
	}
}
