package errs

import (
	"errors"
	"strings"
	"testing"
)

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

func TestAssistantPlanInvalidFieldsAreClosedAndPreserveCause(t *testing.T) {
	for _, field := range []string{"STEPS_INSTRUCTIONS", "STEPS_EXPECTED_RESULT", "WORKFLOW_INVARIANTS", "MAX_CONCURRENCY", "STEP_KEY"} {
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
