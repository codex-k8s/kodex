package errs

import (
	"errors"
	"strings"
	"testing"
)

// TestAssistantPlanDiagnosticsPreserveClosedClassAndIndex checks every plan stage across conflict, version and invalid causes, the accepted zero or thirty-two index, rejected stage/index examples and unchanged unrelated errors. Joined private payloads must not reach the wrapper's Error message.
// TestAssistantPlanDiagnosticsPreserveClosedClassAndIndex проверяет каждый этап плана с причинами конфликта, версии и некорректного ввода, допустимые номера ноль или тридцать два, примеры отклонённых пар и неизменность посторонних ошибок. Присоединённые закрытые данные не должны попадать в сообщение Error обёртки.
func TestAssistantPlanDiagnosticsPreserveClosedClassAndIndex(t *testing.T) {
	for _, stage := range []string{AssistantPlanHydrate, AssistantPlanNormalize, AssistantPlanBind, AssistantPlanAuthorize, AssistantPlanEmpty, AssistantPlanCommand} {
		for _, cause := range []error{ErrConflict, ErrVersionMismatch, ErrInvalid} {
			index := 32
			if stage == AssistantPlanEmpty {
				index = 0
			}
			failure := WithAssistantPlanStage(errors.Join(cause, errors.New("PRIVATE_OPERATION_PAYLOAD")), stage, index)
			gotStage, category, gotIndex, ok := AssistantPlanDiagnostic(failure)
			want := "CONFLICT"
			if cause == ErrVersionMismatch {
				want = "VERSION"
			} else if cause == ErrInvalid {
				want = "INVALID"
			}
			if !ok || gotStage != stage || gotIndex != index || category != want || !errors.Is(failure, cause) || strings.Contains(failure.Error(), "PRIVATE_OPERATION_PAYLOAD") {
				t.Fatal("plan diagnostic changed its class or exposed private data")
			}
		}
	}
	for _, test := range []struct {
		stage string
		index int
	}{{AssistantPlanHydrate, 0}, {AssistantPlanBind, 33}, {AssistantPlanEmpty, 1}, {"PRIVATE_OPERATION_PAYLOAD", 1}} {
		if WithAssistantPlanStage(ErrConflict, test.stage, test.index) != ErrConflict {
			t.Fatal("unknown diagnostic was not rejected")
		}
	}
	for _, cause := range []error{nil, ErrNotFound, ErrForbidden, ErrUnavailable} {
		if WithAssistantPlanStage(cause, AssistantPlanHydrate, 1) != cause {
			t.Fatal("plan diagnostic changed another error boundary")
		}
	}
}

// TestAssistantPlanInvalidFieldsAreClosedAndPreserveCause checks six allowed field labels through nested stage/field wrappers, cause preservation and sanitized messages. Four unknown or injected labels and a forbidden-error example must not acquire field-diagnostic authority.
// TestAssistantPlanInvalidFieldsAreClosedAndPreserveCause проверяет шесть допустимых меток полей через вложенные обёртки этапа и поля, сохранение причины и очищенные сообщения. Четыре неизвестные либо дополненные текстом метки и пример ошибки запрета не должны получать полномочие диагностики поля.
func TestAssistantPlanInvalidFieldsAreClosedAndPreserveCause(t *testing.T) {
	for _, field := range []string{"STEPS_INSTRUCTIONS", "STEPS_EXPECTED_RESULT", "WORKFLOW_INVARIANTS", "WORKFLOW_TEXT_BUDGET", "MAX_CONCURRENCY", "STEP_KEY"} {
		failure := WithAssistantPlanStage(WithAssistantPlanField(errors.Join(ErrInvalid, errors.New("PRIVATE_PAYLOAD")), field), AssistantPlanHydrate, 1)
		stage, category, index, ok := AssistantPlanDiagnostic(failure)
		if !ok || stage != AssistantPlanHydrate || category != "INVALID" || index != 1 || AssistantPlanField(failure) != field || !errors.Is(failure, ErrInvalid) || strings.Contains(failure.Error(), "PRIVATE_PAYLOAD") {
			t.Fatal("invalid diagnostic lost its rejecting guard or disclosed payload")
		}
	}
	for _, field := range []string{"", "steps.instructions", "PRIVATE_PAYLOAD", "STEPS_INSTRUCTIONS\nPRIVATE_PAYLOAD"} {
		if WithAssistantPlanField(ErrInvalid, field) != ErrInvalid || AssistantPlanField(WithAssistantPlanStage(WithAssistantPlanField(ErrInvalid, field), AssistantPlanHydrate, 1)) != "" {
			t.Fatal("unknown field gained diagnostic authority")
		}
	}
	if WithAssistantPlanField(ErrForbidden, "STEPS_INSTRUCTIONS") != ErrForbidden {
		t.Fatal("field changed another error boundary")
	}
}
