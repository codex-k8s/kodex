package callback

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneapi"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func publicationReadFixture(t *testing.T) (*Server, runtimecontract.RunnerInput, *workflowCatalogClient, []byte, string) {
	t.Helper()
	model := controlplaneapi.WorkflowPublication{Version: 1, WorkflowRef: "wfl_workflow01", ProjectRef: "prj_project001", PublishedRef: "wfv_workflow01", SpecDigest: strings.Repeat("a", 64), WorkflowVersion: 7, PublishedVersion: 3, Name: "Процесс", CoordinatorAgentRef: "agt_coordinator", Instructions: strings.Repeat("Работа ё🙂 <> ", 2000), Concurrency: 1, TimeoutSeconds: 3600, Steps: []controlplaneapi.WorkflowPublicationStep{{Key: "step-001", Position: 1, AgentRef: "agt_specialist", Instructions: "Инструкция", TimeoutSeconds: 900}}}
	raw, digest, err := controlplaneapi.EncodeWorkflowPublication(model)
	if err != nil {
		t.Fatal(err)
	}
	client := &workflowCatalogClient{response: &cp.GetExecutionWorkflowCatalogResponse{Read: &cp.GetExecutionWorkflowCatalogResponse_Publication{Publication: &cp.ExecutionWorkflowPublication{ConfigurationJson: raw, ConfigurationSha256: digest}}}}
	input := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, AssistantScope: runtimecontract.AssistantScopeNone, ProjectRef: model.ProjectRef, Capabilities: []string{"platform.run.launch"}, LeaseRef: "lse_origin001", LeaseFence: "fixture-fence", LeaseGeneration: 7}
	return &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}, input, client, raw, digest
}
func publicationPins() map[string]any {
	return map[string]any{"workflow_ref": "wfl_workflow01", "published_ref": "wfv_workflow01", "spec_digest": strings.Repeat("a", 64), "workflow_version": float64(7)}
}
func TestExecutionWorkflowPublicationUTF8MultiEOF(t *testing.T) {
	server, input, client, source, digest := publicationReadFixture(t)
	selector := publicationPins()
	var assembled []byte
	for {
		value, err := server.workflowCatalog(t.Context(), input, map[string]any{"publication_read": selector})
		if err != nil {
			t.Fatal(err)
		}
		result := value.(workflowCatalogToolResult).wire
		page := result["configuration_page"].(map[string]any)
		part := []byte(page["text"].(string))
		if len(part) > maximumAssistantConfigurationPageBytes || page["offset_bytes"].(int64) != int64(len(assembled)) {
			t.Fatal("page bound or continuity")
		}
		assembled = append(assembled, part...)
		wire := bytes.NewBuffer(nil)
		encoder := json.NewEncoder(wire)
		_ = encoder.Encode(result)
		if wire.Len() > 200000 {
			t.Fatal("page wire budget")
		}
		if page["eof"].(bool) {
			break
		}
		selector["offset_bytes"] = page["next_offset_bytes"]
		selector["configuration_sha256"] = digest
	}
	if !bytes.Equal(source, assembled) || client.request.Query != "" || client.request.PageToken != "" || client.request.GetPublicationRead().Pins.WorkflowRef != "wfl_workflow01" || client.request.LeaseRef != input.LeaseRef {
		t.Fatal("whole source or server binding changed")
	}
	selector["offset_bytes"] = int64(len(source))
	selector["configuration_sha256"] = digest
	value, err := server.workflowCatalog(t.Context(), input, map[string]any{"publication_read": selector})
	if err != nil || !value.(workflowCatalogToolResult).wire["configuration_page"].(map[string]any)["eof"].(bool) {
		t.Fatal("empty exact EOF")
	}
	if _, err := server.workflowCatalog(t.Context(), input, map[string]any{"query": "x"}); err == nil {
		t.Fatal("cross-mode owner response accepted")
	}
}
func TestExecutionWorkflowPublicationClosedFailures(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"unknown":                   func(v map[string]any) { v["credential"] = "CANARY" },
		"fraction":                  func(v map[string]any) { v["workflow_version"] = 1.5 },
		"empty pins":                func(v map[string]any) { v["published_ref"] = "" },
		"offset without commitment": func(v map[string]any) { v["offset_bytes"] = 1 },
		"null size":                 func(v map[string]any) { v["maximum_bytes"] = nil },
		"oversize page":             func(v map[string]any) { v["maximum_bytes"] = 16385 },
		"bad commitment":            func(v map[string]any) { v["configuration_sha256"] = strings.Repeat("b", 64) },
		"beyond EOF": func(v map[string]any) {
			v["offset_bytes"] = int64(controlplaneapi.WorkflowPublicationMaximumBytes)
			v["configuration_sha256"] = strings.Repeat("b", 64)
		},
	} {
		t.Run(name, func(t *testing.T) {
			server, input, _, _, _ := publicationReadFixture(t)
			v := publicationPins()
			mutate(v)
			if _, err := server.workflowCatalog(t.Context(), input, map[string]any{"publication_read": v}); err == nil {
				t.Fatal("invalid read accepted")
			}
		})
	}
	server, input, client, _, _ := publicationReadFixture(t)
	for _, args := range []map[string]any{
		{"publication_read": publicationPins(), "query": "x"},
		{"publication_read": publicationPins(), "page_token": "x"},
		{"publication_read": publicationPins(), "active_runs_read": publicationPins()},
		{"active_runs_read": publicationPins(), "query": "x"},
		{"publication_read": nil},
	} {
		if _, err := server.workflowCatalog(t.Context(), input, args); err == nil {
			t.Fatal("mixed modes accepted")
		}
	}
	if client.calls != 0 {
		t.Fatal("bad args reached owner")
	}
	for name, mutate := range map[string]func(*cp.GetExecutionWorkflowCatalogResponse){
		"mixed": func(r *cp.GetExecutionWorkflowCatalogResponse) { r.Items = workflowCatalogFixture().Items },
		"digest": func(r *cp.GetExecutionWorkflowCatalogResponse) {
			r.GetPublication().ConfigurationSha256 = strings.Repeat("b", 64)
		},
		"unknown proto": func(r *cp.GetExecutionWorkflowCatalogResponse) { r.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01}) },
		"missing":       func(r *cp.GetExecutionWorkflowCatalogResponse) { r.Read = nil },
	} {
		t.Run(name, func(t *testing.T) {
			server, input, client, _, _ := publicationReadFixture(t)
			mutate(client.response)
			if _, err := server.workflowCatalog(t.Context(), input, map[string]any{"publication_read": publicationPins()}); err == nil {
				t.Fatal("bad owner response accepted")
			}
		})
	}
}
func activeRunResponse() *cp.GetExecutionWorkflowCatalogResponse {
	return &cp.GetExecutionWorkflowCatalogResponse{Read: &cp.GetExecutionWorkflowCatalogResponse_ActiveRuns{ActiveRuns: &cp.ExecutionWorkflowActiveRuns{Pins: &cp.ExecutionWorkflowReadPins{WorkflowRef: "wfl_workflow01", PublishedRef: "wfv_workflow01", SpecDigest: strings.Repeat("a", 64), WorkflowVersion: 7}, Items: []*cp.ExecutionWorkflowActiveRun{{RunRef: "run_workflow01", WorkflowRef: "wfl_workflow01", PublishedRef: "wfv_previous01", SpecDigest: strings.Repeat("c", 64), PublishedVersion: 2, RunVersion: 4, State: "WAITING_HUMAN", Title: "Задача", CreatedAt: timestamppb.New(time.Unix(100, 0))}}}}}
}
func TestExecutionWorkflowActiveRunsClosedProjection(t *testing.T) {
	server, input, client, _, _ := publicationReadFixture(t)
	client.response = activeRunResponse()
	result, err := server.workflowCatalog(t.Context(), input, map[string]any{"active_runs_read": publicationPins()})
	if err != nil || result.(workflowCatalogToolResult).wire["duplicate_check"] != "ADVISORY" || client.request.GetActiveRunsRead() == nil {
		t.Fatal("active roots or actual historical publication lost")
	}
	for name, mutate := range map[string]func(*cp.ExecutionWorkflowActiveRuns){
		"terminal":  func(p *cp.ExecutionWorkflowActiveRuns) { p.Items[0].State = "SUCCEEDED" },
		"foreign":   func(p *cp.ExecutionWorkflowActiveRuns) { p.Items[0].WorkflowRef = "wfl_foreign01" },
		"duplicate": func(p *cp.ExecutionWorkflowActiveRuns) { p.Items = append(p.Items, p.Items[0]) },
		"pin":       func(p *cp.ExecutionWorkflowActiveRuns) { p.Pins.SpecDigest = strings.Repeat("d", 64) },
		"time":      func(p *cp.ExecutionWorkflowActiveRuns) { p.Items[0].CreatedAt = nil },
		"overflow": func(p *cp.ExecutionWorkflowActiveRuns) {
			for len(p.Items) < 11 {
				p.Items = append(p.Items, p.Items[0])
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			client.response = activeRunResponse()
			mutate(client.response.GetActiveRuns())
			if _, err := server.workflowCatalog(t.Context(), input, map[string]any{"active_runs_read": publicationPins()}); err == nil {
				t.Fatal("bad active read accepted")
			}
		})
	}
}
