package httptransport

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
)

func promptPinFixture() *cp.PromptContextPin {
	return &cp.PromptContextPin{Digest: strings.Repeat("c", 64), AgentRef: "agent_fixture01", AgentVersion: 3}
}

func promptPreviewFixture() *cp.PreviewPromptTemplateResponse {
	digest := strings.Repeat("a", 64)
	return &cp.PreviewPromptTemplateResponse{SafePreview: "TYPE_текст", Complete: true, TemplateRef: "preview_fixture01", TemplateDigest: digest, MaterializationDigest: digest, ServiceTemplateRevision: "prompt.v1", ServiceTemplateDigest: digest, VariableSnapshotDigest: digest, Locale: "en", ContextPin: promptPinFixture(),
		Slots: []*cp.PromptSlotProvenance{{Slot: cp.PromptSemanticSlot_PROMPT_SEMANTIC_SLOT_PURPOSE, Source: cp.PromptSectionSource_PROMPT_SECTION_SOURCE_USER_TEMPLATE, Position: 1}, {Slot: cp.PromptSemanticSlot_PROMPT_SEMANTIC_SLOT_FILES, Source: cp.PromptSectionSource_PROMPT_SECTION_SOURCE_PLATFORM, Position: 2}},
		Sections: []*cp.PromptPreviewSection{
			{Source: cp.PromptSectionSource_PROMPT_SECTION_SOURCE_USER_TEMPLATE, Content: "before", UserKind: cp.PromptUserSectionKind_PROMPT_USER_SECTION_KIND_BASE_TEMPLATE, TemplateRef: "preview_fixture01", TemplateDigest: digest},
			{Source: cp.PromptSectionSource_PROMPT_SECTION_SOURCE_PLATFORM, Slot: cp.PromptSemanticSlot_PROMPT_SEMANTIC_SLOT_PURPOSE, Content: "[PURPOSE]"},
			{Source: cp.PromptSectionSource_PROMPT_SECTION_SOURCE_USER_TEMPLATE, Content: "after", UserKind: cp.PromptUserSectionKind_PROMPT_USER_SECTION_KIND_BASE_TEMPLATE, TemplateRef: "preview_fixture01", TemplateDigest: digest},
			{Source: cp.PromptSectionSource_PROMPT_SECTION_SOURCE_PLATFORM, Slot: cp.PromptSemanticSlot_PROMPT_SEMANTIC_SLOT_FILES, Content: "[FILES]"},
		}}
}

func TestPromptPreviewPreservesExecutedSlotsAndExactContext(t *testing.T) {
	client := &catalogRPCRecorder{response: promptPreviewFixture()}
	w := httptest.NewRecorder()
	body := `{"template":"{{slot \"PURPOSE\"}}","targetKind":"AGENT","targetRef":"agent_fixture01","context":{"expectedAgentVersion":3,"task":"TYPE_задача","input":{"answer":"i18n:значение"}},"expectedContextDigest":"` + strings.Repeat("c", 64) + `"}`
	catalogTestHandler(client).ServeHTTP(w, managedTestRequest("POST", "/api/v1/prompt-templates/preview", body))
	request, ok := client.request.(*cp.PreviewPromptTemplateRequest)
	if w.Code != 200 || !ok || request.GetContext().GetTask() != "TYPE_задача" || request.GetContext().GetExpectedAgentVersion() != 3 || request.GetContext().GetInput().AsMap()["answer"] != "i18n:значение" || request.GetExpectedContextDigest() != strings.Repeat("c", 64) {
		t.Fatalf("preview context: %d %s", w.Code, w.Body.String())
	}
	var result generated.PromptTemplatePreview
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.SafePreview != "TYPE_текст" || len(result.Sections) != 4 || len(result.Slots) != 2 || result.ContextPin == nil {
		t.Fatal("preview provenance lost")
	}
}

func TestPromptCatalogPostCarriesValuesOutsideURL(t *testing.T) {
	client := &catalogRPCRecorder{response: &cp.ListTemplateVariablesResponse{ContextPin: promptPinFixture()}}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, managedTestRequest("POST", "/api/v1/prompt-templates/catalog/query", `{"targetKind":"AGENT","targetRef":"agent_fixture01","query":"input","pageSize":7,"context":{"task":"catalog task"}}`))
	request, ok := client.request.(*cp.ListTemplateVariablesRequest)
	if w.Code != 200 || !ok || request.Query != "input" || request.GetPage().GetPageSize() != 7 || request.GetContext().GetTask() != "catalog task" || !strings.Contains(w.Body.String(), `"contextPin"`) {
		t.Fatalf("catalog context: %d %s", w.Code, w.Body.String())
	}
}

func TestPromptCatalogPreservesOwnerDenialReasons(t *testing.T) {
	for _, reason := range []cp.TemplateVariableAvailabilityReason{cp.TemplateVariableAvailabilityReason_TEMPLATE_VARIABLE_AVAILABILITY_REASON_PERMISSION_REQUIRED, cp.TemplateVariableAvailabilityReason_TEMPLATE_VARIABLE_AVAILABILITY_REASON_CAPABILITY_REQUIRED} {
		variable := variableFixture()
		variable.Name, variable.Reason, variable.Available = "project.files", reason, false
		client := &catalogRPCRecorder{response: &cp.ListTemplateVariablesResponse{Variables: []*cp.TemplateVariable{variable}, Total: 1, ContextPin: promptPinFixture()}}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, managedTestRequest("POST", "/api/v1/prompt-templates/catalog/query", `{"targetKind":"AGENT","targetRef":"agent_fixture01"}`))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"available":false`) || !strings.Contains(w.Body.String(), strings.TrimPrefix(reason.String(), "TEMPLATE_VARIABLE_AVAILABILITY_REASON_")) {
			t.Fatalf("owner reason lost: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestPromptWorkflowAndContinuationUseOwnerPins(t *testing.T) {
	for _, kind := range []string{"WORKFLOW_STAGE", "SESSION_CONTINUATION"} {
		t.Run(kind, func(t *testing.T) {
			response := promptPreviewFixture()
			ref, context := "workflow_fixture01", `{"workflowRevisionRef":"wrev_fixture01","workflowStageKey":"review","expectedWorkflowVersion":7,"expectedAgentVersion":3}`
			if kind == "WORKFLOW_STAGE" {
				response.ContextPin.WorkflowRef, response.ContextPin.WorkflowVersion = ref, 7
				response.ContextPin.WorkflowRevisionRef, response.ContextPin.WorkflowStageKey = "wrev_fixture01", "review"
			} else {
				ref, context = "session_fixture01", `{"task":"continue"}`
				response.ContextPin.PreviousRuntimeRevisionRef = "runtime_previous01"
				response.RuntimeDiff = &cp.PromptRuntimeDiff{PreviousRevisionRef: "runtime_previous01", SessionRef: ref, Digest: strings.Repeat("b", 64)}
			}
			client := &catalogRPCRecorder{response: response}
			w := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(w, managedTestRequest("POST", "/api/v1/prompt-templates/preview", `{"template":"","targetKind":"`+kind+`","targetRef":"`+ref+`","context":`+context+`}`))
			request, ok := client.request.(*cp.PreviewPromptTemplateRequest)
			if w.Code != 200 || !ok || request.TargetKind != kind || request.TargetRef != ref {
				t.Fatalf("target context: %d %s", w.Code, w.Body.String())
			}
			if kind == "SESSION_CONTINUATION" && (strings.Contains(w.Body.String(), `"currentRevisionRef"`) || strings.Contains(w.Body.String(), `"attempt"`) || !strings.Contains(w.Body.String(), `"runtimeDiff"`)) {
				t.Fatal("prospective context invented a runtime attempt")
			}
		})
	}
}

func TestPromptPreviewRejectsInvalidProvenanceWithoutEcho(t *testing.T) {
	for name, mutate := range map[string]func(*cp.PreviewPromptTemplateResponse){
		"unknown slot":   func(v *cp.PreviewPromptTemplateResponse) { v.Slots[0].Slot = 999 },
		"duplicate slot": func(v *cp.PreviewPromptTemplateResponse) { v.Slots[1].Slot = v.Slots[0].Slot },
		"wrong position": func(v *cp.PreviewPromptTemplateResponse) { v.Slots[1].Position = 1 },
		"false user source": func(v *cp.PreviewPromptTemplateResponse) {
			v.Sections[0].Slot = cp.PromptSemanticSlot_PROMPT_SEMANTIC_SLOT_FILES
		},
		"unknown user kind": func(v *cp.PreviewPromptTemplateResponse) { v.Sections[0].UserKind = 999 },
		"missing block":     func(v *cp.PreviewPromptTemplateResponse) { v.Sections = v.Sections[:3] },
		"extra full body":   func(v *cp.PreviewPromptTemplateResponse) { v.FullMaterializedPrompt = "private-fixture-value" },
		"unsafe pin":        func(v *cp.PreviewPromptTemplateResponse) { v.ContextPin.AgentVersion = maximumSafeJSONInteger + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			v := promptPreviewFixture()
			mutate(v)
			if _, ok := promptPreviewView(v, false); ok {
				t.Fatal("invalid provenance accepted")
			}
		})
	}
}

func TestPromptSelectionRejectsPayloadAuthorityAndMalformedContext(t *testing.T) {
	for _, body := range []string{
		`{"template":"text","targetKind":"AGENT","targetRef":"agent_fixture01","context":{"actorRef":"actor_forged01"}}`,
		`{"template":"text","targetKind":"WORKFLOW_STAGE","targetRef":"workflow_fixture01"}`,
		`{"template":"text","targetKind":"AGENT","targetRef":"agent_fixture01","context":{"agentRef":"agent_other01"}}`,
	} {
		client := &catalogRPCRecorder{response: promptPreviewFixture()}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, managedTestRequest("POST", "/api/v1/prompt-templates/preview", body))
		if w.Code != 400 || client.request != nil {
			t.Fatalf("invalid context reached owner: %d", w.Code)
		}
	}
}

func TestPromptScopeIsPassedOnlyToPromptLifecycle(t *testing.T) {
	configuration, revision := managedFixture()
	configuration.Kind = cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_PROMPT_TEMPLATE
	scope := &cp.PromptTemplateScope{TargetKind: "AGENT", TargetRef: "agent_fixture01", TemplateKind: cp.PromptTemplateKind_PROMPT_TEMPLATE_KIND_CONTINUATION, ContextPin: promptPinFixture()}
	revision.PromptScope = scope
	client := &catalogRPCRecorder{response: &cp.CreatePromptTemplateDraftResponse{Configuration: configuration, Revision: revision}}
	handler := generated.Handler(&Server{control: &controlplaneclient.Client{Command: cp.NewPlatformCommandServiceClient(client)}})
	body := `{"name":"Template","contentFormat":"TEXT","content":"text","promptScope":{"targetKind":"AGENT","targetRef":"agent_fixture01","templateKind":"CONTINUATION"}}`
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, managedTestRequest("POST", "/api/v1/prompt-template-configurations/drafts", body))
	request, ok := client.request.(*cp.CreatePromptTemplateDraftRequest)
	if w.Code != 201 || !ok || request.GetPromptScope().GetTemplateKind() != scope.TemplateKind || !strings.Contains(w.Body.String(), `"promptScope"`) {
		t.Fatalf("scope lost: %d %s", w.Code, w.Body.String())
	}
	client.request = nil
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, managedTestRequest("POST", "/api/v1/role-image-configurations/drafts", body))
	if w.Code != 400 || client.request != nil {
		t.Fatal("prompt scope reached another lifecycle")
	}
}

func TestPromptProspectiveDiffDoesNotInventAttempt(t *testing.T) {
	v := &cp.PromptRuntimeDiff{PreviousRevisionRef: "runtime_previous01", SessionRef: "session_fixture01", Digest: strings.Repeat("a", 64)}
	if _, ok := promptRuntimeDiffView(v); !ok {
		t.Fatal("prospective diff rejected")
	}
	v.Attempt = 1
	if _, ok := promptRuntimeDiffView(v); ok {
		t.Fatal("invented attempt accepted")
	}
	copy := proto.Clone(promptPreviewFixture()).(*cp.PreviewPromptTemplateResponse)
	copy.ContextPin.AgentRef = "agent_foreign01"
	if validPromptContextReadback("AGENT", "agent_fixture01", "", copy.ContextPin) {
		t.Fatal("foreign context accepted")
	}
	copy = promptPreviewFixture()
	copy.ContextPin.PreviousRuntimeRevisionRef = "runtime_previous01"
	copy.RuntimeDiff = &cp.PromptRuntimeDiff{PreviousRevisionRef: "runtime_other01", SessionRef: "session_fixture01", Digest: strings.Repeat("b", 64)}
	if _, ok := promptPreviewView(copy, false); ok {
		t.Fatal("diff from another prior runtime accepted")
	}
}
