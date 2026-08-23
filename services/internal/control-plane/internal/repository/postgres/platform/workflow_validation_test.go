package platform

import (
	"strings"
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

func TestValidBoundedRunInputRejectsOversizedPayload(t *testing.T) {
	t.Parallel()

	if !validBoundedRunInput(map[string]any{"task": "bounded"}) {
		t.Fatal("ограниченный input запуска был отклонён")
	}
	if validBoundedRunInput(map[string]any{"task": strings.Repeat("x", 65<<10)}) {
		t.Fatal("input запуска больше 64 KiB должен отклоняться")
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

func TestValidWorkflowVersionAllowsOneAgentInSeveralSteps(t *testing.T) {
	t.Parallel()

	version := validWorkflowFixture()
	version.Steps[1].AgentRef = version.Steps[0].AgentRef
	if !validWorkflowVersion(version) {
		t.Fatal("один ИИ-сотрудник должен иметь возможность выполнить несколько этапов")
	}
}

func TestValidWorkflowVersionRejectsInvalidInputFields(t *testing.T) {
	t.Parallel()

	for name, field := range map[string]entity.WorkflowInputField{
		"duplicate option": {Key: "priority", Label: "Приоритет", Type: "SELECT", Options: []string{"Высокий", "Высокий"}},
		"unsafe key":       {Key: "Priority!", Label: "Приоритет", Type: "TEXT"},
		"empty select":     {Key: "priority", Label: "Приоритет", Type: "SELECT"},
		"options for text": {Key: "priority", Label: "Приоритет", Type: "TEXT", Options: []string{"Высокий"}},
	} {
		t.Run(name, func(t *testing.T) {
			version := validWorkflowFixture()
			version.Inputs = []entity.WorkflowInputField{field}
			if validWorkflowVersion(version) {
				t.Fatal("некорректное поле входа не должно проходить проверку")
			}
		})
	}
}

func TestValidWorkflowRunInput(t *testing.T) {
	t.Parallel()

	fields := []entity.WorkflowInputField{
		{Key: "company", Label: "Компания", Type: "TEXT", Required: true},
		{Key: "priority", Label: "Приоритет", Type: "SELECT", Options: []string{"Обычный", "Высокий"}},
		{Key: "due_date", Label: "Срок", Type: "DATE"},
	}
	if !validWorkflowRunInput(fields, map[string]any{"company": "Север", "priority": "Высокий", "due_date": "2026-08-27"}) {
		t.Fatal("валидные входные данные workflow отклонены")
	}
	for name, input := range map[string]map[string]any{
		"missing required": {"priority": "Высокий"},
		"unknown":          {"company": "Север", "foreign": "value"},
		"invalid option":   {"company": "Север", "priority": "Критический"},
		"invalid date":     {"company": "Север", "due_date": "27.08.2026"},
	} {
		t.Run(name, func(t *testing.T) {
			if validWorkflowRunInput(fields, input) {
				t.Fatal("некорректные входные данные workflow не должны проходить проверку")
			}
		})
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
