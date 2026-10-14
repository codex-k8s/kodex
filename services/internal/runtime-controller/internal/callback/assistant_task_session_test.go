package callback

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneapi"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func callbackTaskSessionPage() *controlplanev1.AssistantTaskSessionPage {
	p := &controlplanev1.AssistantTaskSessionPage{RunRef: "run_selected123", ProjectRef: "prj_owned123", SessionRef: "ses_owned123", Title: "Предыдущая задача", State: controlplanev1.RunState_RUN_STATE_FAILED, RunVersion: 3, ResultSummary: "Публичный результат", SessionStorageState: "PURGED", SourceSha256: strings.Repeat("a", 64), Messages: []*controlplanev1.AssistantTaskPublishedMessage{{EventRef: "evt_public123", MessageRef: "msg_final123", Phase: 3, Origin: 1, Text: "Публичный итог", SourceRunRef: "run_selected123", SourceRunVersion: 3, SessionRef: "ses_owned123", NodeRef: "nod_source123", TurnRef: "trn_source123", TurnNumber: 1, Attempt: 1, EventSequence: 48, MessageRevision: 1}}}
	if controlplaneapi.SealTaskSessionPage(p) != nil {
		panic("invalid synthetic page")
	}
	return p
}

func TestTaskSessionToolCatalogPreservesAssistantOrdinaryPartition(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeNone, runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		input := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, AssistantScope: scope, ProjectRef: "prj_owned123", Capabilities: []string{"platform.run.launch"}}
		if scope == runtimecontract.AssistantScopeProject {
			input.AssistantProfileRef, input.AgentRef = "asstprof_owned123", "agt_owned123"
		}
		names := map[string]bool{}
		for _, tool := range tools(input) {
			names[tool["name"].(string)] = true
		}
		if names["read_task_session"] != (scope != runtimecontract.AssistantScopeNone) || names["launch_workflow"] != (scope == runtimecontract.AssistantScopeNone) {
			t.Fatal("task session read crossed ordinary assistant partition")
		}
	}
}
func TestReadTaskSessionUsesExactLeaseTypedSelectorAndSafeActivity(t *testing.T) {
	input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeProject, AssistantProfileRef: "asstprof_owned123", AgentRef: "agt_owned123", ProjectRef: "prj_owned123", LeaseRef: "lse_current123", LeaseFence: "PRIVATE_SYNTHETIC_FENCE", LeaseGeneration: 2}
	client := &assistantResourceSearchClient{response: &controlplanev1.SearchAssistantResourcesResponse{AssistantTaskSession: callbackTaskSessionPage()}}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
	value, err := server.readTaskSession(t.Context(), input, map[string]any{"run_ref": "run_selected123"})
	if err != nil {
		t.Fatal(err)
	}
	if client.request.AssistantTaskSessionRead.RunRef != "run_selected123" || client.request.Query != "" || client.request.AssistantConfigurationCatalog != nil || client.request.LeaseRef != input.LeaseRef || client.request.Fence != input.LeaseFence || client.request.Generation != 2 {
		t.Fatal("tool lost exact server execution binding")
	}
	raw, _ := json.Marshal(value)
	if !strings.Contains(string(raw), `"phase":"FINAL"`) || strings.Contains(string(raw), input.LeaseFence) {
		t.Fatal("public output lost typed message or leaked execution fence")
	}
	activity := safeToolCallResult("read_task_session", value, nil)
	if strings.Contains(activity, "Публичный") || !strings.Contains(activity, "projection_sha256") || strings.Contains(activity, "run_selected123") {
		t.Fatal("activity persisted body or locators instead of commitment")
	}
	if safeToolCallResult("read_task_session", map[string]any{"projection_sha256": "forged"}, nil) != "TOOL_UNAVAILABLE" {
		t.Fatal("caller map supplied private receipt")
	}
	parameters, capability, _, ok := safeToolCallParameters(input, "read_task_session", map[string]any{"run_ref": "run_selected123"})
	if !ok || capability != "platform.resources.search" || len(parameters) != 0 {
		t.Fatal("read changed capability or logged locator")
	}
}
func TestReadTaskSessionRejectsWrongModeScopePinsAndCursor(t *testing.T) {
	input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeProject, AssistantProfileRef: "asstprof_owned123", AgentRef: "agt_owned123", ProjectRef: "prj_owned123", LeaseRef: "lse_current123", LeaseFence: "fence", LeaseGeneration: 1}
	for _, test := range []struct {
		name   string
		input  runtimecontract.RunnerInput
		args   map[string]any
		mutate func(*controlplanev1.SearchAssistantResourcesResponse)
	}{
		{"ordinary", runtimecontract.RunnerInput{}, map[string]any{"run_ref": "run_selected123"}, nil},
		{"unknown payload authority", input, map[string]any{"run_ref": "run_selected123", "session_ref": "ses_owned123"}, nil},
		{"unsafe locator", input, map[string]any{"run_ref": "../target"}, nil},
		{"cursor type", input, map[string]any{"run_ref": "run_selected123", "cursor": 2}, nil},
		{"missing result", input, map[string]any{"run_ref": "run_selected123"}, func(r *controlplanev1.SearchAssistantResourcesResponse) { r.AssistantTaskSession = nil }},
		{"mixed search mode", input, map[string]any{"run_ref": "run_selected123"}, func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.Results = []*controlplanev1.SearchResult{{Ref: "run_foreign123"}}
		}},
		{"foreign selected run", input, map[string]any{"run_ref": "run_selected123"}, func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.AssistantTaskSession.RunRef = "run_foreign123"
			controlplaneapi.SealTaskSessionPage(r.AssistantTaskSession)
		}},
		{"foreign project", input, map[string]any{"run_ref": "run_selected123"}, func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.AssistantTaskSession.ProjectRef = "prj_foreign123"
			controlplaneapi.SealTaskSessionPage(r.AssistantTaskSession)
		}},
		{"unknown response field", input, map[string]any{"run_ref": "run_selected123"}, func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
		}},
		{"digest mismatch", input, map[string]any{"run_ref": "run_selected123"}, func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.AssistantTaskSession.Messages[0].Text = "forged"
		}},
		{"cursor no progress", input, map[string]any{"run_ref": "run_selected123"}, func(r *controlplanev1.SearchAssistantResourcesResponse) {
			p := r.AssistantTaskSession
			raw, _ := json.Marshal(controlplaneapi.TaskSessionCursor{Version: 1, Binding: strings.Repeat("b", 64), Source: p.SourceSha256, Offset: 2})
			p.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
			p.Truncated = true
			controlplaneapi.SealTaskSessionPage(p)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := &controlplanev1.SearchAssistantResourcesResponse{AssistantTaskSession: callbackTaskSessionPage()}
			if test.mutate != nil {
				test.mutate(response)
			}
			client := &assistantResourceSearchClient{response: response}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			if _, err := server.readTaskSession(t.Context(), test.input, test.args); err == nil {
				t.Fatal("invalid public read accepted")
			}
		})
	}
}
