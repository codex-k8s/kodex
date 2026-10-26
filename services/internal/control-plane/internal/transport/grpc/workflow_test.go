package grpc

import (
	"reflect"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// TestServerWorkflowProtoRoundtrip preserves a nonsequential DAG and typed results through the
// actual protobuf wire and both casters, retaining the pre-existing field13 owner-gate deadline.
// TestServerWorkflowProtoRoundtrip сохраняет непоследовательный DAG и типизированные результаты
// через настоящий protobuf и оба caster, включая прежний срок Human Gate в поле13.
func TestServerWorkflowProtoRoundtrip(t *testing.T) {
	ttl := int32(1200)
	input := &controlplanev1.WorkflowVersion{ExecutionMode: "SERVER_DAG_V1", MaxConcurrency: 2, TimeoutSeconds: 3600, Steps: []*controlplanev1.WorkflowStep{
		{Ref: "analysis", Position: 1, AgentRef: "agt_analyst01", TimeoutSeconds: 900, ResultRequirements: []*controlplanev1.WorkflowResultRequirement{{Key: "spec", Kind: "ARTIFACT", FileName: "spec.md", MediaType: "text/markdown"}}},
		{Ref: "architecture", Position: 2, AgentRef: "agt_architect", TimeoutSeconds: 900, DependsOn: []string{"analysis"}},
		{Ref: "review", Position: 3, AgentRef: "agt_reviewer01", TimeoutSeconds: 900, DependsOn: []string{"analysis"}, HumanGate: true, ApprovalTimeoutSeconds: &ttl, GateDecisions: []controlplanev1.OwnerGateDecision{controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_APPROVE}},
	}}
	raw, err := proto.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var received controlplanev1.WorkflowVersion
	if err := proto.Unmarshal(raw, &received); err != nil {
		t.Fatal(err)
	}
	domain := domainWorkflowVersion(&received)
	output := castWorkflowVersion(domain, "DRAFT", "")
	if output.ExecutionMode != input.ExecutionMode || !reflect.DeepEqual(domain.Steps[2].DependsOn, []string{"analysis"}) || len(output.Steps) != 3 {
		t.Fatal("explicit DAG was replaced by position-derived dependencies")
	}
	for index := range input.Steps {
		if !proto.Equal(input.Steps[index], output.Steps[index]) {
			t.Fatal("step policy or mandatory results changed during roundtrip")
		}
	}
	if received.Steps[2].ProtoReflect().Descriptor().Fields().ByName("approval_timeout_seconds").Number() != 13 || received.Steps[2].ProtoReflect().Descriptor().Fields().ByName("depends_on").Number() != 14 || received.Steps[2].ProtoReflect().Descriptor().Fields().ByName("result_requirements").Number() != 15 {
		t.Fatal("existing gate field number was reused")
	}
}

// TestWorkflowAttemptProjectionExcludesInternalRevision keeps owner evidence in the node wire
// while excluding the internal RuntimeRevision row identity from the public whitelist.
// TestWorkflowAttemptProjectionExcludesInternalRevision сохраняет доказательства в передаче узла,
// исключая внутренний идентификатор RuntimeRevision из публичного списка полей.
func TestWorkflowAttemptProjectionExcludesInternalRevision(t *testing.T) {
	node := castNode(entity.RunNode{WorkflowResults: []entity.WorkflowAttemptResult{{Attempt: 1, RunRef: "run_12345678", RuntimeRevisionID: "PRIVATE_REVISION_CANARY", Accepted: true, Results: []entity.WorkflowAcceptedResult{{Key: "spec", Kind: "ARTIFACT", ArtifactRef: "art_12345678", ArtifactRevision: "arv_12345678", Digest: "sha256:abc"}}}}})
	raw, err := protojson.Marshal(node)
	if err != nil || strings.Contains(string(raw), "PRIVATE_REVISION_CANARY") || strings.Contains(string(raw), "runtimeRevision") || len(node.WorkflowResults) != 1 || len(node.WorkflowResults[0].Results) != 1 {
		t.Fatal("public node evidence lost or internal revision exposed")
	}
}

// TestDomainWorkflowVersionBuildsAuthoritativeDependencies checks workflow metadata, generated
// step keys, sequential/parallel dependencies, capability requirements and human-gate decisions
// are materialized as expected.
// TestDomainWorkflowVersionBuildsAuthoritativeDependencies проверяет ожидаемую материализацию
// метаданных процесса, назначенных ключей шагов, последовательных и параллельных зависимостей,
// capability requirements и human-gate решений.
func TestDomainWorkflowVersionBuildsAuthoritativeDependencies(t *testing.T) {
	t.Parallel()

	input := &controlplanev1.WorkflowVersion{
		Ref: "wfv-input", Name: "Сверка документов", Purpose: "Проверить пакет документов",
		CoordinatorAgentRef: "agt-coordinator", Revision: 3, MaxConcurrency: 2,
		TimeoutSeconds: 1800, CompletionCriteria: "Все документы проверены",
		Steps: []*controlplanev1.WorkflowStep{
			{Position: 1, Name: "Получить", Purpose: "Получить документы", AgentRef: "agt-reader", TimeoutSeconds: 300},
			{Position: 2, Name: "Правила", Purpose: "Проверить правила", AgentRef: "agt-legal", Parallel: true, ParallelGroup: 1, TimeoutSeconds: 600, ExpectedResult: "Заключение", RequiredCapabilityKeys: []string{"storage.read"}},
			{Position: 3, Name: "Суммы", Purpose: "Проверить суммы", AgentRef: "agt-accounting", Parallel: true, ParallelGroup: 1, TimeoutSeconds: 600},
			{Position: 4, Name: "Решение", Purpose: "Собрать итог", AgentRef: "agt-coordinator", TimeoutSeconds: 300, HumanGate: true, GateDecisions: []controlplanev1.OwnerGateDecision{controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_APPROVE, controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_REQUEST_CHANGES}},
		},
	}

	got := domainWorkflowVersion(input)
	if got == nil || got.Name != input.Name || got.Purpose != input.Purpose || got.CoordinatorAgentRef != input.CoordinatorAgentRef {
		t.Fatalf("метаданные workflow потеряны: %#v", got)
	}
	if keys := []string{got.Steps[0].Key, got.Steps[1].Key, got.Steps[2].Key, got.Steps[3].Key}; !reflect.DeepEqual(keys, []string{"step-001", "step-002", "step-003", "step-004"}) {
		t.Fatalf("неожиданные server-owned keys: %v", keys)
	}
	if len(got.Steps[0].DependsOn) != 0 || !reflect.DeepEqual(got.Steps[1].DependsOn, []string{"step-001"}) || !reflect.DeepEqual(got.Steps[2].DependsOn, []string{"step-001"}) || !reflect.DeepEqual(got.Steps[3].DependsOn, []string{"step-002", "step-003"}) {
		t.Fatalf("неверно материализованы зависимости: %#v", got.Steps)
	}
	if got.Steps[1].ExpectedResult != "Заключение" || !reflect.DeepEqual(got.Steps[1].RequiredCapabilityKeys, []string{"storage.read"}) {
		t.Fatalf("поля шага потеряны: %#v", got.Steps[1])
	}
	if !got.Steps[3].HumanGateAfter || !reflect.DeepEqual(got.Steps[3].GateDecisions, []string{"APPROVE", "REQUEST_CHANGES"}) {
		t.Fatalf("Human Gate шага потерян: %#v", got.Steps[3])
	}
}

// TestDomainWorkflowVersionAssignsStableAvailableInputKeys checks explicit input keys are
// preserved, missing keys use an available stable identifier and select options survive domain
// conversion.
// TestDomainWorkflowVersionAssignsStableAvailableInputKeys проверяет сохранение явных input keys,
// назначение отсутствующим ключам свободного устойчивого идентификатора и сохранение select
// options при доменном преобразовании.
func TestDomainWorkflowVersionAssignsStableAvailableInputKeys(t *testing.T) {
	t.Parallel()

	got := domainWorkflowVersion(&controlplanev1.WorkflowVersion{InputFields: []*controlplanev1.WorkflowInputField{
		{Key: "field-002", Label: "Существующее", ValueType: "TEXT"},
		{Label: "Новое", ValueType: "SELECT", Required: true, Options: []string{"Да", "Нет"}},
	}})
	if got == nil || len(got.Inputs) != 2 {
		t.Fatalf("поля входа потеряны: %#v", got)
	}
	if got.Inputs[0].Key != "field-002" || got.Inputs[1].Key != "field-001" {
		t.Fatalf("server-owned keys назначены неверно: %#v", got.Inputs)
	}
	if !reflect.DeepEqual(got.Inputs[1].Options, []string{"Да", "Нет"}) {
		t.Fatalf("варианты выбора потеряны: %#v", got.Inputs[1])
	}
}
