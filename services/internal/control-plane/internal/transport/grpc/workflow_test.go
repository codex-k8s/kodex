package grpc

import (
	"reflect"
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
)

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
