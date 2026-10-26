package httptransport

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
)

// TestWorkflowDraftServerContractRoundTrip exercises JSON input, the gateway adapter and actual
// protobuf encoding together, retaining explicit dependencies, every predicate and optional TTL.
// TestWorkflowDraftServerContractRoundTrip проверяет JSON-вход, преобразователь шлюза и настоящее
// кодирование Protobuf вместе: зависимости, все условия и необязательный срок не теряются.
func TestWorkflowDraftServerContractRoundTrip(t *testing.T) {
	input := `{"name":"Релиз журнала","purpose":"Доставить проверенный сервис","coordinatorAgentRef":"agt_manager1","executionMode":"SERVER_DAG_V1","maxConcurrency":2,"steps":[{"ref":"deploy","position":4,"name":"Доставка","purpose":"Развернуть","agentRef":"agt_sre00001","parallel":false,"parallelGroup":0,"timeoutSeconds":600,"expectedResult":"Готовый сервис","humanGate":true,"approvalTimeoutSeconds":300,"gateDecisions":["APPROVE","CANCEL"],"requiredCapabilityKeys":[],"dependsOn":["merge","qa"],"resultRequirements":[{"key":"release","kind":"DEPLOYMENT","repository":"codex-k8s/test-repo","subjectStepKey":"merge","operation":"ssh.compose.deploy","targetRef":"ssh_fixture1"},{"key":"runbook","kind":"ARTIFACT","fileName":"runbook.md","mediaType":"text/markdown"}]}]}`
	var body generated.WorkflowInput
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || !validWorkflowGateTimeouts(body) {
		t.Fatal("valid workflow input rejected", err)
	}
	wire, err := proto.Marshal(workflowDraft(body))
	if err != nil {
		t.Fatal(err)
	}
	var got cp.WorkflowVersion
	if err := proto.Unmarshal(wire, &got); err != nil {
		t.Fatal(err)
	}
	ttl := int32(300)
	expected := &cp.WorkflowVersion{Name: body.Name, Purpose: body.Purpose, CoordinatorAgentRef: body.CoordinatorAgentRef, ExecutionMode: "SERVER_DAG_V1", MaxConcurrency: 2, Steps: []*cp.WorkflowStep{{
		Ref: "deploy", Position: 4, Name: "Доставка", Purpose: "Развернуть", AgentRef: "agt_sre00001", TimeoutSeconds: 600, ExpectedResult: "Готовый сервис", HumanGate: true, ApprovalTimeoutSeconds: &ttl,
		GateDecisions: []cp.OwnerGateDecision{cp.OwnerGateDecision_OWNER_GATE_DECISION_APPROVE, cp.OwnerGateDecision_OWNER_GATE_DECISION_CANCEL}, DependsOn: []string{"merge", "qa"},
		ResultRequirements: []*cp.WorkflowResultRequirement{{Key: "release", Kind: "DEPLOYMENT", Repository: "codex-k8s/test-repo", SubjectStepKey: "merge", Operation: "ssh.compose.deploy", TargetRef: "ssh_fixture1"}, {Key: "runbook", Kind: "ARTIFACT", FileName: "runbook.md", MediaType: "text/markdown"}},
	}}}
	if !proto.Equal(&got, expected) {
		t.Fatal("workflow input lost a predicate, dependency or approval timeout")
	}
	legacy := workflowDraft(generated.WorkflowInput{Name: "Прежний", Purpose: "Последовательный", CoordinatorAgentRef: "agt_manager1"})
	if legacy.ExecutionMode != "" || len(legacy.Steps) != 0 {
		t.Fatal("legacy workflow acquired server execution policy")
	}
}

// TestWorkflowModesFollowPublishedAndDraftPins prevents mode loss when flattening separate owner
// revisions; the published view and editable draft must retain their own execution policies.
// TestWorkflowModesFollowPublishedAndDraftPins исключает потерю режима при разворачивании разных
// ревизий владельца: опубликованный вид и редактируемый черновик сохраняют собственную политику.
func TestWorkflowModesFollowPublishedAndDraftPins(t *testing.T) {
	workflow := &cp.Workflow{Ref: "wfl_fixture1", Version: 3,
		PublishedVersion: &cp.WorkflowVersion{Ref: "wfv_published1", Version: 2, Revision: 2, State: cp.WorkflowState_WORKFLOW_STATE_PUBLISHED, ExecutionMode: "MANAGER_DRIVEN"},
		DraftVersion:     &cp.WorkflowVersion{Ref: "wfv_draft001", Version: 3, Revision: 3, State: cp.WorkflowState_WORKFLOW_STATE_DRAFT, ExecutionMode: "SERVER_DAG_V1", Steps: []*cp.WorkflowStep{{Ref: "qa", DependsOn: []string{"review"}, ResultRequirements: []*cp.WorkflowResultRequirement{{Key: "tested", Kind: "QA", SubjectStepKey: "review"}}}}},
	}
	workflowReadinessFixture(workflow)
	view, err := messageMap(&cp.GetWorkflowResponse{Workflow: workflow})
	if err != nil {
		t.Fatal(err)
	}
	public := view["workflow"].(map[string]any)
	draft := public["draft"].(map[string]any)
	if public["executionMode"] != "MANAGER_DRIVEN" || draft["executionMode"] != "SERVER_DAG_V1" || draft["ref"] != "wfv_draft001" {
		t.Fatal("published and draft execution modes were mixed")
	}
	step := draft["steps"].([]any)[0].(map[string]any)
	if !reflect.DeepEqual(step["dependsOn"], []any{"review"}) || len(step["resultRequirements"].([]any)) != 1 {
		t.Fatal("draft predicates lost during public projection")
	}
}

// TestWorkflowRejectedAttemptRetainsFalseAndEmptyCollections verifies required zero values in
// the actual public JSON and generated OpenAPI model without creating successful evidence.
// TestWorkflowRejectedAttemptRetainsFalseAndEmptyCollections проверяет обязательные нулевые поля
// настоящего публичного JSON и OpenAPI-модели без создания успешного доказательства.
func TestWorkflowRejectedAttemptRetainsFalseAndEmptyCollections(t *testing.T) {
	view, err := messageMap(&cp.RunNode{Ref: "nod_fixture1", RunRef: "run_fixture1", WorkflowResults: []*cp.WorkflowAttemptResult{{Attempt: 1, RunRef: "run_fixture1"}}})
	if err != nil {
		t.Fatal(err)
	}
	attempt := view["workflowResults"].([]any)[0].(map[string]any)
	expected := map[string]any{"attempt": float64(1), "runRef": "run_fixture1", "accepted": false, "reason": "", "results": []any{}}
	if !reflect.DeepEqual(attempt, expected) {
		t.Fatal("required rejected attempt fields were omitted or invented")
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var node generated.RunNode
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&node); err != nil || node.WorkflowResults == nil || (*node.WorkflowResults)[0].Accepted {
		t.Fatal("OpenAPI model cannot represent the rejected attempt", err)
	}
}

// TestWorkflowPublicProjectionRejectsUnknownEvidence requires closed failure for malformed
// owner snapshots rather than forwarding unknown enum values, extra history or unsafe integers.
// TestWorkflowPublicProjectionRejectsUnknownEvidence требует закрытого отказа для неверных
// снимков вместо выдачи неизвестных enum, лишней истории или неточных целочисленных значений.
func TestWorkflowPublicProjectionRejectsUnknownEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*cp.RunNode){
		"unknown kind":         func(v *cp.RunNode) { v.WorkflowResults[0].Results[0].Kind = "UNVERIFIED" },
		"unknown verdict":      func(v *cp.RunNode) { v.WorkflowResults[0].Results[0].Verdict = "MAYBE" },
		"empty key":            func(v *cp.RunNode) { v.WorkflowResults[0].Results[0].Key = "" },
		"digest newline":       func(v *cp.RunNode) { v.WorkflowResults[0].Results[0].Digest += "\n" },
		"unsafe number":        func(v *cp.RunNode) { v.WorkflowResults[0].Results[0].Number = maximumSafeJSONInteger + 1 },
		"foreign ref shape":    func(v *cp.RunNode) { v.WorkflowResults[0].Results[0].ArtifactRef = "private/path" },
		"invalid image digest": func(v *cp.RunNode) { v.WorkflowResults[0].Results[0].ImageDigest = "sha256:unknown" },
		"attempt zero":         func(v *cp.RunNode) { v.WorkflowResults[0].Attempt = 0 },
		"attempt five":         func(v *cp.RunNode) { v.WorkflowResults[0].Attempt = 5 },
		"history overflow": func(v *cp.RunNode) {
			for len(v.WorkflowResults) < 5 {
				v.WorkflowResults = append(v.WorkflowResults, v.WorkflowResults[0])
			}
		},
		"results overflow": func(v *cp.RunNode) {
			for len(v.WorkflowResults[0].Results) < 5 {
				v.WorkflowResults[0].Results = append(v.WorkflowResults[0].Results, v.WorkflowResults[0].Results[0])
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			node := &cp.RunNode{Ref: "nod_fixture1", RunRef: "run_fixture1", WorkflowResults: []*cp.WorkflowAttemptResult{{Attempt: 1, RunRef: "run_fixture1", Accepted: true, Results: []*cp.WorkflowAcceptedResult{{Key: "review", Kind: "REVIEW", Digest: strings.Repeat("a", 64), Verdict: "PASS", HeadSha: strings.Repeat("b", 40)}}}}}
			if _, err := messageMap(node); err != nil {
				t.Fatal("valid evidence projection rejected", err)
			}
			mutate(node)
			if view, err := messageMap(node); err == nil || view != nil {
				t.Fatal("invalid evidence projection accepted")
			}
		})
	}
	for _, message := range []proto.Message{
		&cp.WorkflowVersion{ExecutionMode: "AUTOMATIC"},
		&cp.WorkflowResultRequirement{Key: "output", Kind: "ARTIFACT", Operation: "ssh.arbitrary"},
		&cp.WorkflowResultRequirement{Key: "output", Kind: "UNKNOWN"},
		&cp.WorkflowResultRequirement{Key: "output", Kind: "REVIEW", SubjectStepKey: "qa\n"},
		&cp.WorkflowStep{DependsOn: []string{""}},
		&cp.WorkflowStep{DependsOn: []string{"qa", "qa"}},
		&cp.WorkflowStep{DependsOn: []string{"qa/other"}},
	} {
		if _, err := messageMap(message); err == nil {
			t.Fatal("unknown workflow policy accepted")
		}
	}
}
