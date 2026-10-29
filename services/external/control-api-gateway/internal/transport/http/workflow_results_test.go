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

// TestWorkflowPDLCPublicContract exercises JSON input, actual Proto bytes and the shared HTTP/WS
// projection. New fields survive closed decoding and cannot be smuggled into an old profile.
// TestWorkflowPDLCPublicContract проверяет JSON input, actual Proto bytes и общий HTTP/WS projection.
// Новые поля сохраняются при closed decoding и не переносятся скрыто в прежний профиль.
func TestWorkflowPDLCPublicContract(t *testing.T) {
	input := `{"name":"Документы","purpose":"Уточнить журнал обращений","coordinatorAgentRef":"agt_manager1","executionMode":"SERVER_DAG_V1","resultEvidenceProfile":"GITHUB_PDLC_V1","maxConcurrency":2,"steps":[{"ref":"author","position":1,"name":"Требования","purpose":"Сохранить документ","agentRef":"agt_author01","parallel":false,"parallelGroup":0,"timeoutSeconds":120,"expectedResult":"Требования","humanGate":false,"gateDecisions":[],"requiredCapabilityKeys":[],"resultRequirements":[{"key":"document","kind":"REPOSITORY_FILE","repository":"owner/repo","targetRef":"3","branch":"feature/journal","fileName":"docs/requirements.md"},{"key":"comment","kind":"PR_COMMENT","repository":"owner/repo","targetRef":"3"}]}]}`
	var body generated.WorkflowInput
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		t.Fatal("PDLC OpenAPI input unavailable", err)
	}
	raw, err := proto.Marshal(workflowDraft(body))
	if err != nil {
		t.Fatal("marshal PDLC gateway command")
	}
	var version cp.WorkflowVersion
	if err := proto.Unmarshal(raw, &version); err != nil || version.ResultEvidenceProfile != "GITHUB_PDLC_V1" || version.Steps[0].ResultRequirements[0].Branch != "feature/journal" {
		t.Fatal("gateway dropped exact PDLC branch/profile", err)
	}
	version.Ref, version.Version, version.Revision, version.State = "wfv_pdlc0001", 1, 1, cp.WorkflowState_WORKFLOW_STATE_DRAFT
	if _, err := messageMap(&version); err != nil {
		t.Fatal("valid PDLC owner version rejected by HTTP/WS projection", err)
	}
	for _, old := range []string{"", "GITHUB_REVIEW_QA_V1", "GITHUB_PDLC_V2"} {
		version.ResultEvidenceProfile = old
		if _, err := messageMap(&version); err == nil {
			t.Fatal("old or unknown profile accepted PDLC public predicates")
		}
	}
	proof := &cp.WorkflowAcceptedResult{Key: "document", Kind: "REPOSITORY_FILE", Repository: "owner/repo", Number: 3, HeadSha: strings.Repeat("a", 40), FileName: "docs/requirements.md", Branch: "feature/journal", Digest: strings.Repeat("b", 64), ReceiptRef: "erc_file_write000", HeadReceiptRef: "erc_file_head0000", ContentReceiptRef: "erc_file_content0"}
	for name, mutate := range map[string]func(*cp.WorkflowAcceptedResult){
		"valid":             func(*cp.WorkflowAcceptedResult) {},
		"missing-head":      func(v *cp.WorkflowAcceptedResult) { v.HeadReceiptRef = "" },
		"duplicate-receipt": func(v *cp.WorkflowAcceptedResult) { v.ContentReceiptRef = v.ReceiptRef },
		"path-traversal":    func(v *cp.WorkflowAcceptedResult) { v.FileName = "../requirements.md" },
		"branch-alias":      func(v *cp.WorkflowAcceptedResult) { v.Branch = "refs/heads/feature/journal" },
		"cross-kind":        func(v *cp.WorkflowAcceptedResult) { v.Kind = "ARTIFACT" },
		"invented-verdict":  func(v *cp.WorkflowAcceptedResult) { v.Verdict = "PASS" },
	} {
		t.Run(name, func(t *testing.T) {
			current := proto.Clone(proof).(*cp.WorkflowAcceptedResult)
			mutate(current)
			view, err := messageMap(current)
			if name != "valid" {
				if err == nil || view != nil {
					t.Fatal("malformed file evidence crossed public projection")
				}
				return
			}
			if err != nil {
				t.Fatal("valid file proof public projection unavailable", err)
			}
			raw, err := json.Marshal(view)
			if err != nil {
				t.Fatal("encode public file proof")
			}
			var accepted generated.WorkflowAcceptedResult
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&accepted); err != nil || accepted.Branch == nil || *accepted.Branch != proof.Branch || accepted.HeadReceiptRef == nil || *accepted.HeadReceiptRef != proof.HeadReceiptRef || accepted.ContentReceiptRef == nil || *accepted.ContentReceiptRef != proof.ContentReceiptRef {
				t.Fatal("OpenAPI model dropped native file proof pins", err)
			}
		})
	}
}

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

// TestWorkflowReworkPolicyReadbackPreservesRevisionBoundaries checks the actual owner-message
// projection and OpenAPI JSON round trip without inferring policy from another workflow revision.
// TestWorkflowReworkPolicyReadbackPreservesRevisionBoundaries проверяет проекцию сообщения
// владельца и JSON OpenAPI без вывода политики из другой редакции процесса.
func TestWorkflowReworkPolicyReadbackPreservesRevisionBoundaries(t *testing.T) {
	const upstream = "OWNER_APPROVED_UPSTREAM_V1"
	for _, scenario := range []struct {
		name, publishedMode, publishedPolicy, draftMode, draftPolicy string
	}{
		{name: "draft only", draftMode: "SERVER_DAG_V1", draftPolicy: upstream},
		{name: "published only", publishedMode: "SERVER_DAG_V1", publishedPolicy: upstream},
		{name: "new draft opt in", publishedMode: "MANAGER_DRIVEN", draftMode: "SERVER_DAG_V1", draftPolicy: upstream},
		{name: "draft leaves policy unset", publishedMode: "SERVER_DAG_V1", publishedPolicy: upstream, draftMode: "SERVER_DAG_V1"},
		{name: "legacy without policy", publishedMode: "MANAGER_DRIVEN", draftMode: "MANAGER_DRIVEN"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			workflow := &cp.Workflow{Ref: "wfl_fixture1", Version: 3}
			if scenario.publishedMode != "" {
				workflow.PublishedVersion = &cp.WorkflowVersion{Ref: "wfv_published1", Version: 2, Revision: 2,
					State: cp.WorkflowState_WORKFLOW_STATE_PUBLISHED, ExecutionMode: scenario.publishedMode, ReworkPolicy: scenario.publishedPolicy}
			}
			if scenario.draftMode != "" {
				workflow.DraftVersion = &cp.WorkflowVersion{Ref: "wfv_draft001", Version: 3, Revision: 3,
					State: cp.WorkflowState_WORKFLOW_STATE_DRAFT, ExecutionMode: scenario.draftMode, ReworkPolicy: scenario.draftPolicy}
			}
			workflowReadinessFixture(workflow)
			view, err := messageMap(&cp.GetWorkflowResponse{Workflow: workflow})
			if err != nil {
				t.Fatal(err)
			}
			public := view["workflow"].(map[string]any)
			expected := scenario.publishedPolicy
			if workflow.PublishedVersion == nil {
				expected = scenario.draftPolicy
			}
			if policy, present := public["reworkPolicy"]; present != (expected != "") || present && policy != expected {
				t.Fatalf("selected revision policy changed: value=%v present=%t expected=%q", policy, present, expected)
			}
			if workflow.DraftVersion != nil {
				draft := public["draft"].(map[string]any)
				if policy, present := draft["reworkPolicy"]; present != (scenario.draftPolicy != "") || present && policy != scenario.draftPolicy {
					t.Fatal("draft policy was lost or copied from the published revision")
				}
			}
			raw, err := json.Marshal(public)
			if err != nil {
				t.Fatal(err)
			}
			var decoded generated.Workflow
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&decoded); err != nil {
				t.Fatal("workflow readback is incompatible with the public contract", err)
			}
			if decoded.ReworkPolicy == nil && expected != "" || decoded.ReworkPolicy != nil && string(*decoded.ReworkPolicy) != expected {
				t.Fatal("public JSON round trip lost the selected policy")
			}
		})
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

// TestWorkflowNegativeEvidenceProjection exercises the shared actual HTTP/WS message writer:
// verified negative facts survive, but accepted results and private owner coordinates do not.
// TestWorkflowNegativeEvidenceProjection проверяет actual общий HTTP/WS message writer:
// negative facts сохраняются, accepted results и private owner coordinates не выдаются.
func TestWorkflowNegativeEvidenceProjection(t *testing.T) {
	attempt := &cp.WorkflowAttemptResult{Attempt: 1, RunRef: "run_fixture1", Accepted: false, Reason: "CHANGES_REQUIRED", Evidence: []*cp.WorkflowAcceptedResult{{Key: "review", Kind: "REVIEW", Digest: strings.Repeat("a", 64), ReceiptRef: "erc_12345678", Repository: "owner/repo", Number: 42, HeadSha: strings.Repeat("b", 40), Verdict: "CHANGES_REQUIRED"}}}
	node := &cp.RunNode{Ref: "nod_fixture1", RunRef: "run_fixture1", WorkflowResults: []*cp.WorkflowAttemptResult{attempt}}
	view, err := messageMap(node)
	if err != nil {
		t.Fatal("negative evidence unavailable", err)
	}
	row := view["workflowResults"].([]any)[0].(map[string]any)
	if row["accepted"] != false || len(row["results"].([]any)) != 0 || len(row["evidence"].([]any)) != 1 {
		t.Fatal("negative evidence became acceptance")
	}
	for _, change := range []func(*cp.WorkflowAttemptResult){
		func(v *cp.WorkflowAttemptResult) { v.Accepted = true },
		func(v *cp.WorkflowAttemptResult) { v.Reason = "RESULT_INVALID" },
		func(v *cp.WorkflowAttemptResult) { v.Results = v.Evidence },
		func(v *cp.WorkflowAttemptResult) { v.Evidence[0].ReceiptRef = "" },
		func(v *cp.WorkflowAttemptResult) { v.Evidence[0].Verdict = "PASS" },
	} {
		changed := proto.Clone(node).(*cp.RunNode)
		change(changed.WorkflowResults[0])
		if _, err := messageMap(changed); err == nil {
			t.Fatal("invalid negative envelope projected")
		}
	}
}

// TestWorkflowPublicGeneratedHistoryRemainsTyped detects successful codegen that accidentally
// replaces a closed history DTO with interface{} through partial schema compositions.
// TestWorkflowPublicGeneratedHistoryRemainsTyped ловит успешный codegen, который случайно
// заменил закрытый DTO истории на interface{} из-за частичных schema compositions.
func TestWorkflowPublicGeneratedHistoryRemainsTyped(t *testing.T) {
	wire := reflect.TypeFor[generated.WorkflowAttemptResult]()
	if wire.Kind() != reflect.Struct {
		t.Fatal("workflow history DTO lost its closed typed object")
	}
	for _, field := range []string{"Attempt", "RunRef", "Accepted", "Reason", "Results", "Evidence"} {
		if _, exists := wire.FieldByName(field); !exists {
			t.Fatal("generated workflow history lost required typed field", field)
		}
	}
}

// TestWorkflowPublicEvidenceSubjectRelations exercises the actual shared HTTP/WS projection;
// the public graph cannot advertise an opt-in relation rejected by canonical owner SQL.
// TestWorkflowPublicEvidenceSubjectRelations проверяет actual общий HTTP/WS projection:
// public graph не объявляет opt-in relation, которую canonical owner SQL отвергнет.
func TestWorkflowPublicEvidenceSubjectRelations(t *testing.T) {
	version := &cp.WorkflowVersion{Ref: "wfv_evidence1", Version: 1, Revision: 1, State: cp.WorkflowState_WORKFLOW_STATE_PUBLISHED, ExecutionMode: "SERVER_DAG_V1", Steps: []*cp.WorkflowStep{
		{Ref: "author", AgentRef: "agt_author123", ResultRequirements: []*cp.WorkflowResultRequirement{{Key: "pr", Kind: "PULL_REQUEST", Repository: "owner/repo"}}},
		{Ref: "review", AgentRef: "agt_review123", DependsOn: []string{"author"}, ResultRequirements: []*cp.WorkflowResultRequirement{{Key: "review", Kind: "REVIEW", SubjectStepKey: "author"}}},
		{Ref: "checker", AgentRef: "agt_checker123", DependsOn: []string{"review"}, ResultRequirements: []*cp.WorkflowResultRequirement{{Key: "checked", Kind: "REVIEW", SubjectStepKey: "review"}}},
	}}
	if _, err := messageMap(version); err != nil {
		t.Fatal("legacy public relation changed", err)
	}
	version.ResultEvidenceProfile = "GITHUB_REVIEW_QA_V1"
	if _, err := messageMap(version); err == nil {
		t.Fatal("public wire admitted REVIEW-only evidence subject")
	}
	version.Steps[2].DependsOn = []string{"author"}
	version.Steps[2].ResultRequirements[0].SubjectStepKey = "author"
	if _, err := messageMap(version); err != nil {
		t.Fatal("public direct PR subject rejected", err)
	}
	version.Steps[2].DependsOn = []string{"review"}
	if _, err := messageMap(version); err == nil {
		t.Fatal("public wire admitted a non-direct PR evidence subject")
	}
	version.Steps[2].DependsOn = []string{"author"}
	version.Steps[0].ResultRequirements = append(version.Steps[0].ResultRequirements, &cp.WorkflowResultRequirement{Key: "other_pr", Kind: "PULL_REQUEST", Repository: "owner/repo"})
	if _, err := messageMap(version); err == nil {
		t.Fatal("public wire admitted ambiguous PR evidence subject")
	}
}

// TestWorkflowPublicOptionalEvidenceAbsence preserves the same HTTP/WS shape for nil and empty
// repeated evidence while retaining mandatory false acceptance, empty results and owner bytes.
// TestWorkflowPublicOptionalEvidenceAbsence сохраняет HTTP/WS форму для nil и empty repeated
// evidence с обязательными false acceptance, empty results и неизменёнными owner bytes.
func TestWorkflowPublicOptionalEvidenceAbsence(t *testing.T) {
	for _, evidence := range [][]*cp.WorkflowAcceptedResult{nil, {}} {
		attempt := &cp.WorkflowAttemptResult{Attempt: 1, RunRef: "run_fixture1", Reason: "RESULT_INVALID", Evidence: evidence}
		before := proto.Clone(attempt)
		view, err := ProtoMap(attempt)
		if err != nil {
			t.Fatal("legacy history unavailable", err)
		}
		if _, exists := view["evidence"]; exists {
			t.Fatal("optional empty evidence was invented")
		}
		if view["accepted"] != false || !reflect.DeepEqual(view["results"], []any{}) || view["reason"] != "RESULT_INVALID" {
			t.Fatal("mandatory rejected history defaults changed")
		}
		if !proto.Equal(attempt, before) {
			t.Fatal("public projection mutated owner history")
		}
	}
}

// TestWorkflowMixedNegativeEvidenceWriter preserves data-only reviewed artifacts and native
// COMMENT facts together, including the owner's historical effectiveness discriminator.
// TestWorkflowMixedNegativeEvidenceWriter сохраняет data-only reviewed artifacts вместе с native
// COMMENT facts и историческим discriminator эффективности, не превращая их в accepted results.
func TestWorkflowMixedNegativeEvidenceWriter(t *testing.T) {
	attempt := &cp.WorkflowAttemptResult{Attempt: 1, RunRef: "run_fixture1", Reason: "CHANGES_REQUIRED", EffectiveState: "REWORK_PENDING", Evidence: []*cp.WorkflowAcceptedResult{
		{Key: "design", Kind: "ARTIFACT_REVIEW", ArtifactRef: "art_fixture1", ArtifactRevision: "arev_fixture1", Digest: strings.Repeat("a", 64), Verdict: "CHANGES_REQUIRED"},
		{Key: "comment", Kind: "REVIEW", ReceiptRef: "erc_fixture1", Repository: "owner/repo", Number: 42, HeadSha: strings.Repeat("b", 40), Digest: strings.Repeat("c", 64), Verdict: "PASS"},
	}}
	node := &cp.RunNode{Ref: "nod_fixture1", RunRef: "run_fixture1", WorkflowResults: []*cp.WorkflowAttemptResult{attempt}}
	view, err := messageMap(node)
	if err != nil {
		t.Fatal("mixed negative evidence unavailable", err)
	}
	row := view["workflowResults"].([]any)[0].(map[string]any)
	if row["accepted"] != false || row["effectiveState"] != "REWORK_PENDING" || len(row["results"].([]any)) != 0 || len(row["evidence"].([]any)) != 2 {
		t.Fatal("mixed negative facts changed")
	}
	for _, change := range []func(*cp.WorkflowAttemptResult){
		func(v *cp.WorkflowAttemptResult) { v.Accepted = true },
		func(v *cp.WorkflowAttemptResult) { v.Evidence[0].ArtifactRevision = "" },
		func(v *cp.WorkflowAttemptResult) { v.Evidence[1].ReceiptRef = "" },
		func(v *cp.WorkflowAttemptResult) { v.Evidence[0].Verdict = "PASS" },
	} {
		changed := proto.Clone(node).(*cp.RunNode)
		change(changed.WorkflowResults[0])
		if _, err := messageMap(changed); err == nil {
			t.Fatal("invalid mixed negative projection accepted")
		}
	}
}
