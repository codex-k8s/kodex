package platform

import (
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
)

func TestValidWorkflowVersionAcceptsBoundedExecutionGraph(t *testing.T) {
	t.Parallel()

	version := validWorkflowFixture()
	if !validWorkflowVersion(version) {
		t.Fatal("ожидалась валидная версия workflow")
	}
}

func TestValidWorkflowVersionRejectsUnknownDependency(t *testing.T) {
	t.Parallel()

	version := validWorkflowFixture()
	version.Steps[1].DependsOn = []string{"step-missing"}
	if validWorkflowVersion(version) {
		t.Fatal("dependency на неизвестный или будущий шаг должна отклоняться")
	}
}

func TestValidWorkflowVersionRejectsGateWithoutDecisions(t *testing.T) {
	t.Parallel()

	version := validWorkflowFixture()
	version.Steps[1].GateDecisions = nil
	if validWorkflowVersion(version) {
		t.Fatal("Human Gate без допустимых решений должен отклоняться")
	}
}

func TestValidWorkflowVersionRejectsUnsafeCapabilityKey(t *testing.T) {
	t.Parallel()

	version := validWorkflowFixture()
	version.Steps[0].RequiredCapabilityKeys = []string{"crm.read;drop"}
	if validWorkflowVersion(version) {
		t.Fatal("небезопасный capability key должен отклоняться")
	}
}

func validWorkflowFixture() entity.WorkflowVersion {
	return entity.WorkflowVersion{
		Ref:                 "wfv-fixture",
		Name:                "Обработка обращения",
		Purpose:             "Подготовить и проверить ответ клиенту",
		CoordinatorAgentRef: "agt-coordinator",
		VersionNumber:       1,
		Concurrency:         2,
		TimeoutSeconds:      3600,
		Steps: []entity.WorkflowStep{
			{
				Key: "step-001", Position: 1, Name: "Подготовка", AgentRef: "agt-writer",
				Instructions: "Подготовить ответ", TimeoutSeconds: 600,
				RequiredCapabilityKeys: []string{"platform.artifacts.read"},
			},
			{
				Key: "step-002", Position: 2, Name: "Проверка", AgentRef: "agt-reviewer",
				Instructions: "Проверить ответ", TimeoutSeconds: 600, DependsOn: []string{"step-001"},
				HumanGateAfter: true, GateDecisions: []string{"APPROVE", "REQUEST_CHANGES"},
			},
		},
	}
}
