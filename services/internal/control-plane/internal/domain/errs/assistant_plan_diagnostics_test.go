package errs

import (
	"errors"
	"strings"
	"testing"
)

func TestAssistantPlanDiagnosticsPreserveClosedClassAndIndex(t *testing.T) {
	for _, stage := range []string{AssistantPlanHydrate, AssistantPlanNormalize, AssistantPlanBind, AssistantPlanAuthorize, AssistantPlanEmpty} {
		for _, cause := range []error{ErrConflict, ErrVersionMismatch} {
			index := 32
			if stage == AssistantPlanEmpty {
				index = 0
			}
			failure := WithAssistantPlanStage(errors.Join(cause, errors.New("PRIVATE_OPERATION_PAYLOAD")), stage, index)
			gotStage, category, gotIndex, ok := AssistantPlanDiagnostic(failure)
			want := "CONFLICT"
			if cause == ErrVersionMismatch {
				want = "VERSION"
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
	for _, cause := range []error{nil, ErrNotFound, ErrForbidden, ErrInvalid, ErrUnavailable} {
		if WithAssistantPlanStage(cause, AssistantPlanHydrate, 1) != cause {
			t.Fatal("plan diagnostic changed another error boundary")
		}
	}
}
