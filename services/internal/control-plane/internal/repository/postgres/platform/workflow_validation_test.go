package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestValidWorkflowVersionAcceptsBoundedExecutionGraph(t *testing.T) {
	t.Parallel()

	version := validWorkflowFixture()
	if !validWorkflowVersion(version) {
		t.Fatal("ожидалась валидная версия workflow")
	}
}

func TestWorkflowCompletionCriteriaBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		criteria string
		valid    bool
	}{
		{name: "empty", valid: true},
		{name: "ascii-2000", criteria: strings.Repeat("x", 2000), valid: true},
		{name: "ascii-2001", criteria: strings.Repeat("x", 2001)},
		{name: "cyrillic-2000", criteria: strings.Repeat("я", 2000), valid: true},
		{name: "cyrillic-2001", criteria: strings.Repeat("я", 2001)},
		{name: "unicode-2000", criteria: strings.Repeat("😀", 2000), valid: true},
		{name: "unicode-2001", criteria: strings.Repeat("😀", 2001)},
		{name: "invalid-utf8", criteria: "result\xff"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			draft := validWorkflowFixture()
			draft.CompletionCriteria = test.criteria
			if got := validWorkflowVersion(draft); got != test.valid {
				t.Fatalf("проверка версии workflow: получено %v, ожидалось %v", got, test.valid)
			}

			create := entity.AssistantPlanOperation{Type: "CREATE_WORKFLOW", Summary: "Создать workflow", Input: map[string]any{
				"projectRef": "prj_12345678", "name": draft.Name, "purpose": draft.Purpose,
				"coordinatorAgentRef": draft.CoordinatorAgentRef, "completionCriteria": test.criteria,
				"steps": []any{map[string]any{
					"name": "Подготовка", "purpose": "Подготовить ответ", "agentRef": "agt_12345678",
					"parallel": false, "parallelGroup": float64(0), "timeoutSeconds": float64(600),
					"expectedResult": "Ответ", "humanGate": false,
					"gateDecisions": []any{}, "requiredCapabilityKeys": []any{},
				}},
			}}

			previous := validWorkflowFixture()
			fields, steps := assistantWorkflowGraphFields(previous)
			before := map[string]any{
				"workflowRef": "wfl_12345678", "projectRef": "prj_12345678",
				"name": previous.Name, "purpose": previous.Purpose,
				"coordinatorAgentRef": previous.CoordinatorAgentRef, "instructions": previous.Instructions,
				"completionCriteria": previous.CompletionCriteria, "inputFields": fields, "steps": steps,
				"maxConcurrency": float64(previous.Concurrency), "timeoutSeconds": float64(previous.TimeoutSeconds),
				"draft": previous,
			}
			t.Run("UPDATE_WORKFLOW_hydrate", func(t *testing.T) {
				_, err := hydrateAssistantWorkflowFields(before, 7, entity.AssistantPlanOperation{
					Type: "UPDATE_WORKFLOW", Parameters: map[string]any{
						"workflowRef": "wfl_12345678", "name": "Обновлённая версия", "completionCriteria": test.criteria,
					},
				})
				if !test.valid {
					if !errors.Is(err, errs.ErrInvalid) {
						t.Fatalf("некорректные критерии допущены в план обновления: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("допустимые критерии отклонены при подготовке плана: %v", err)
				}
			})
			input := cloneAssistantFields(before)
			delete(input, "draft")
			input["completionCriteria"], input["expectedVersion"] = test.criteria, float64(7)
			update := entity.AssistantPlanOperation{Type: "UPDATE_WORKFLOW", Summary: "Обновить workflow", Before: before, Input: input}

			for _, operation := range []entity.AssistantPlanOperation{create, update} {
				t.Run(operation.Type, func(t *testing.T) {
					mapped, err := assistantOperationCommand(operation)
					if !test.valid {
						if !errors.Is(err, errs.ErrInvalid) {
							t.Fatalf("некорректные критерии не отклонены: %v", err)
						}
						return
					}
					if err != nil {
						t.Fatalf("допустимые критерии отклонены: %v", err)
					}
					payload := mapped.Payload.(command.WorkflowInput)
					if payload.Draft == nil || payload.Draft.CompletionCriteria != test.criteria {
						t.Fatal("критерии завершения изменились при построении команды")
					}
				})
			}
		})
	}
}

func TestNormalizeWorkflowDraftIdentityReplacesLegacyLiteralDeterministically(t *testing.T) {
	t.Parallel()

	left := entity.WorkflowVersion{Ref: "draft"}
	right := entity.WorkflowVersion{}
	normalizeWorkflowDraftIdentity("wfl_fixture01", &left)
	normalizeWorkflowDraftIdentity("wfl_fixture01", &right)
	if left.Ref != right.Ref || !strings.HasPrefix(left.Ref, "wfv_") || len(left.Ref) < 12 || left.VersionNumber != 1 || right.VersionNumber != 1 {
		t.Fatalf("server-owned draft identity is unstable: left=%q right=%q", left.Ref, right.Ref)
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
